package ui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func TestConfigRecoveryAndSignOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	c := LoadConfig(path)
	if c.LoadError != nil {
		t.Fatal(c.LoadError)
	}
	id := c.ClientID
	c.UserID, c.UserName = "user-1", "Alice"
	c.ServerToken = "test-server"
	c.ServerURL = "http://test"
	c.Bitrate = 1000
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c.Bitrate = 2000
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("{broken"), 0600)
	c = LoadConfig(path)
	if c.LoadError != nil || c.LoadWarning == "" || c.Bitrate != 1000 || c.ClientID != id {
		t.Fatal("valid backup not recovered")
	}
	if err := c.SignOut(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, path + ".bak"} {
		var saved Config
		b, _ := os.ReadFile(p)
		if json.Unmarshal(b, &saved) != nil || saved.ServerToken != "" || saved.UserID != "" || saved.UserName != "" {
			t.Fatal("sign-out left recovery credentials")
		}
	}
}

func TestSubtitleSizeDefaultChoicesAndPersistence(t *testing.T) {
	c := LoadConfig(filepath.Join(t.TempDir(), "settings.json"))
	if c.SubtitleSizeValue() != 100 || c.subtitleSizeIndex() != 0 {
		t.Fatalf("missing setting should mean Normal: %d / %d", c.SubtitleSizeValue(), c.subtitleSizeIndex())
	}
	for i, want := range []int{100, 150, 200} {
		c.SubtitleSize = want
		if got := c.subtitleSizeIndex(); got != i {
			t.Fatalf("size %d has index %d, want %d", want, got, i)
		}
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	loaded := LoadConfig(c.path)
	if loaded.SubtitleSizeValue() != 200 {
		t.Fatalf("saved size loaded as %d", loaded.SubtitleSizeValue())
	}
	loaded.SubtitleSize = 999
	if loaded.SubtitleSizeValue() != 100 || loaded.subtitleSizeIndex() != 0 {
		t.Fatal("unknown setting did not safely fall back to Normal")
	}
}

func TestDeFlickerPreferencePersists(t *testing.T) {
	c := LoadConfig(filepath.Join(t.TempDir(), "settings.json"))
	if c.DeFlicker {
		t.Fatal("de-flicker should default off")
	}
	c.DeFlicker = true
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if loaded := LoadConfig(c.path); !loaded.DeFlicker {
		t.Fatal("saved de-flicker choice was not restored")
	}
}
func TestConfigFailedSavePreservesPrimary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	c := LoadConfig(path)
	original, _ := os.ReadFile(path)
	os.Mkdir(path+".bak", 0700)
	c.Bitrate = 9000
	if c.Save() == nil {
		t.Fatal("expected backup write failure")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Fatal("failed save changed primary")
	}
}
func TestConfigMalformedWithoutBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"server_token":"partial",`), 0600)
	c := LoadConfig(path)
	if c.LoadError != nil || c.ServerToken != "" || c.LoadWarning == "" {
		t.Fatal("partial account survived damaged JSON")
	}
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatal("damaged file not preserved")
	}
}
func TestOwnedPlayerRequiresExactMarker(t *testing.T) {
	for _, env := range []string{"PATH=/usr/bin\x00", "OTHER=ffmpeg\x00", "MISTERZINE_PLEX_OWNER=/other/plexplay.py\x00"} {
		if ownedPlayer([]byte(env), "/app/plexplay.py") {
			t.Fatal("unrelated process matched")
		}
	}
	if !ownedPlayer([]byte("MISTERZINE_PLEX_OWNER=/app/plexplay.py\x00"), "/app/plexplay.py") {
		t.Fatal("owned child not recognized")
	}
}

func TestReapLeavesUnrelatedProcessRunning(t *testing.T) {
	owner := "/test/misterzine/plexplay.py"
	owned := exec.Command("sleep", "30")
	owned.Env = append(os.Environ(), "MISTERZINE_PLEX_OWNER="+owner)
	unrelated := exec.Command("sleep", "30")
	for _, cmd := range []*exec.Cmd{owned, unrelated} {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer cmd.Process.Kill()
	}
	(&Player{Script: owner}).Reap()
	if err := owned.Wait(); err == nil {
		t.Fatal("owned child was not terminated")
	}
	if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("cleanup terminated an unrelated child")
	}
	unrelated.Process.Kill()
	unrelated.Wait()
}

func TestBitrateClampsSavedValuesToOfferedSteps(t *testing.T) {
	for _, tc := range []struct{ saved, want int }{{0, 8000}, {100, 400}, {400, 400}, {500, 400}, {1000, 1000}, {1500, 1000}, {2000, 2000}, {2500, 2000}, {3000, 3000}, {4000, 4000}, {5000, 4000}, {6000, 6000}, {7000, 6000}, {8000, 8000}, {20000, 8000}} {
		c := Config{Bitrate: tc.saved}
		if c.BitrateKbps() != tc.want {
			t.Fatalf("saved %d: got %d, want %d", tc.saved, c.BitrateKbps(), tc.want)
		}
	}
}

func TestBitrateOptionShowsAndStepsOfferedCaps(t *testing.T) {
	for _, tc := range []struct {
		saved int
		label string
	}{{0, "8 Mbps (max)"}, {12000, "8 Mbps (max)"}, {5000, "4 Mbps"}, {4000, "4 Mbps"}, {3000, "3 Mbps"}, {2000, "2 Mbps"}, {1000, "1 Mbps"}, {400, "0.4 Mbps, low res"}, {50, "0.4 Mbps, low res"}} {
		c := Config{Bitrate: tc.saved}
		if got := Bitrates[c.bitrateIndex()].Label; got != tc.label {
			t.Fatalf("saved %d: shows %q, want %q", tc.saved, got, tc.label)
		}
	}
	a := &App{Cfg: &Config{}}
	o := &Options{app: a}
	var row option
	for _, it := range o.items() {
		if it.label == "Video bitrate" {
			row = it
		}
	}
	row.step(+1)
	if a.Cfg.Bitrate != 8000 || row.val() != "8 Mbps (max)" {
		t.Fatalf("stepping up from the default: %d %q", a.Cfg.Bitrate, row.val())
	}
	row.step(-1)
	if a.Cfg.Bitrate != 6000 || row.val() != "6 Mbps" {
		t.Fatalf("stepping down from the default: %d %q", a.Cfg.Bitrate, row.val())
	}
	for i := 0; i < len(Bitrates)+1; i++ {
		row.step(-1)
	}
	if a.Cfg.Bitrate != 400 || row.val() != "0.4 Mbps, low res" {
		t.Fatalf("stepping down to the end: %d %q", a.Cfg.Bitrate, row.val())
	}
}

// A choice saved by an older build, which offered 3 Mbps at the lowest,
// loads as the same step.
func TestBitrateSavedByOlderBuildLoads(t *testing.T) {
	for _, kbps := range []int{3000, 4000, 6000, 8000} {
		path := filepath.Join(t.TempDir(), "settings.json")
		os.WriteFile(path, []byte(`{"client_id":"misterzine-test","bitrate":`+strconv.Itoa(kbps)+`}`), 0600)
		c := LoadConfig(path)
		if c.LoadError != nil || c.LoadWarning != "" || c.BitrateKbps() != kbps {
			t.Fatalf("saved %d: loaded %d (%v %q)", kbps, c.BitrateKbps(), c.LoadError, c.LoadWarning)
		}
	}
}
