package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"plexcrt/internal/jellyfin"
)

func TestPlayerLaunchesAndReports(t *testing.T) {
	for _, direct := range []bool{true, false} {
		t.Run(map[bool]string{true: "direct", false: "transcode"}[direct], func(t *testing.T) {
			var mu sync.Mutex
			var reports []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/PlaybackInfo") {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["StartTimeTicks"] != float64(120*jellyfin.TicksPerSecond) {
						t.Errorf("start ticks = %v", body["StartTimeTicks"])
					}
					w.Header().Set("Content-Type", "application/json")
					if direct {
						_, _ = w.Write([]byte(`{"PlaySessionId":"session","MediaSources":[{"Id":"source","Container":"mkv","SupportsDirectStream":true}]}`))
					} else {
						_, _ = w.Write([]byte(`{"PlaySessionId":"session","MediaSources":[{"Id":"source","TranscodingUrl":"/stream.ts?ApiKey=synthetic-token&x=1","TranscodingSubProtocol":"http"}]}`))
					}
					return
				}
				mu.Lock()
				reports = append(reports, r.URL.Path)
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()
			store := &jellyfin.MemoryStore{}
			store.SetCredentials(jellyfin.Credentials{UserID: "user", Token: "synthetic-token"})
			client := jellyfin.New(srv.URL, "", "device", store)
			client.SetHTTPClient(srv.Client())
			dir := t.TempDir()
			status := dir + "/status"
			logPath := dir + "/log"
			var args []string
			p := &Player{Client: client, Binary: "jfplay", Fifo: dir + "/fifo", Status: status, LogTo: logPath, Kbps: func() int { return 3000 }, Crop: func() Crop { return Crop14x9 }, Geometry: func() string { return "12,8,10,6,940" }}
			p.exec = func(_ string, a ...string) *exec.Cmd {
				args = append([]string(nil), a...)
				script := "printf 'state=playing\\nposition=121\\nduration=300\\n' > '" + status + "'; echo synthetic-token >&2; sleep 1"
				return exec.Command("sh", "-c", script)
			}
			s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 120)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-s.Done:
			case <-time.After(4 * time.Second):
				t.Fatal("player did not exit")
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "--analog-aspect 4") {
				t.Fatalf("missing crop argument: %s", joined)
			}
			if strings.Contains(joined, "--anti-twitter") {
				t.Fatalf("player overrides the core's OSD filter setting: %s", joined)
			}
			if !strings.Contains(joined, "--geometry 12,8,10,6,940") {
				t.Fatalf("missing saved calibration argument: %s", joined)
			}
			if direct {
				if !strings.Contains(joined, "--base 0 --start-in-file 120") || !strings.Contains(joined, "/Videos/item/stream.mkv") {
					t.Fatalf("direct args: %s", joined)
				}
			} else {
				if !strings.Contains(joined, "--base 120") || strings.Contains(joined, "--start-in-file") || strings.Contains(joined, "ApiKey") {
					t.Fatalf("transcode args: %s", joined)
				}
			}
			logData, _ := os.ReadFile(logPath)
			if strings.Contains(string(logData), "synthetic-token") {
				t.Fatal("token reached log")
			}
			mu.Lock()
			got := strings.Join(reports, ",")
			mu.Unlock()
			if !strings.Contains(got, "/Sessions/Playing") || !strings.Contains(got, "/Sessions/Playing/Stopped") {
				t.Fatalf("reports: %s", got)
			}
		})
	}
}

func TestHLSFullTimelineSeekUsesInFileOffsetAndZeroBase(t *testing.T) {
	type request struct{ startTicks float64 }
	var mu sync.Mutex
	var requests []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/PlaybackInfo") {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			got := request{}
			if value, ok := body["StartTimeTicks"].(float64); ok {
				got.startTicks = value
			}
			mu.Lock()
			requests = append(requests, got)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"PlaySessionId":"session","MediaSources":[{"Id":"source","TranscodingUrl":"/master.m3u8?ApiKey=synthetic-token","TranscodingSubProtocol":"hls"}]}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	store := &jellyfin.MemoryStore{}
	store.SetCredentials(jellyfin.Credentials{UserID: "user", Token: "synthetic-token"})
	client := jellyfin.New(srv.URL, "", "device", store)
	client.SetHTTPClient(srv.Client())
	dir := t.TempDir()
	status := dir + "/status"
	p := &Player{Client: client, Binary: "jfplay", Fifo: dir + "/fifo", Status: status, LogTo: dir + "/log"}
	var cmdMu sync.Mutex
	var child *exec.Cmd
	var launches [][]string
	p.exec = func(_ string, args ...string) *exec.Cmd {
		mu.Lock()
		launches = append(launches, append([]string(nil), args...))
		mu.Unlock()
		state := "playing"
		if hasArg(args, "--hold") {
			state = "paused"
		}
		cmd := exec.Command("sh", "-c", "printf 'state="+state+"\\nposition=4937\\nduration=6192.256\\n' > '"+status+"'; exec sleep 30")
		cmdMu.Lock()
		child = cmd
		cmdMu.Unlock()
		return cmd
	}
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 4937)
	if err != nil {
		t.Fatal(err)
	}
	s.writeCommand = func(line string) {
		if line == "toggle" {
			if err := os.WriteFile(status, []byte("state=playing\nposition=5100\nduration=6192.256\n"), 0o600); err != nil {
				t.Error(err)
			}
			return
		}
		if line != "stop" {
			return
		}
		cmdMu.Lock()
		defer cmdMu.Unlock()
		if child != nil && child.Process != nil {
			_ = child.Process.Kill()
		}
	}
	// Keep the worker's status stale after Pause to model its reporting lag.
	s.Send("pause")
	s.Send("seek 5100")
	if !s.state.Paused {
		t.Fatal("seek restart did not preserve paused state")
	}
	s.Send("toggle")
	if s.state.Paused {
		t.Fatal("toggle after held seek restart did not resume")
	}
	s.Stop()

	mu.Lock()
	gotRequests := append([]request(nil), requests...)
	gotLaunches := append([][]string(nil), launches...)
	mu.Unlock()
	// The start probes for direct play, gets a transcode and asks again with
	// the user's bitrate; the seek restart skips the probe.
	if len(gotRequests) != 3 || gotRequests[0].startTicks != 4937*jellyfin.TicksPerSecond || gotRequests[1].startTicks != 4937*jellyfin.TicksPerSecond || gotRequests[2].startTicks != 5100*jellyfin.TicksPerSecond {
		t.Fatalf("HLS PlaybackInfo offsets = %#v", gotRequests)
	}
	if len(gotLaunches) != 2 {
		t.Fatalf("player launches = %d, want 2", len(gotLaunches))
	}
	for i, args := range gotLaunches {
		if !containsArgPair(args, "--base", "0") || !containsArgPair(args, "--start-in-file", []string{"4937", "5100"}[i]) {
			t.Fatalf("HLS launch %d args = %#v", i, args)
		}
		if strings.Contains(strings.Join(args, " "), "ApiKey") {
			t.Fatalf("HLS launch %d leaked its stream token", i)
		}
	}
	if hasArg(gotLaunches[0], "--hold") || !hasArg(gotLaunches[1], "--hold") {
		t.Fatalf("seek restart hold arguments = %#v", gotLaunches)
	}
}

func TestHLSDetectionUsesProtocolOrPlaylistPath(t *testing.T) {
	for _, tc := range []struct {
		protocol, url string
		want          bool
	}{
		{protocol: "HLS", url: "/stream.ts", want: true},
		{protocol: "http", url: "/master.m3u8?token=synthetic", want: true},
		{protocol: "http", url: "/stream.ts?token=synthetic", want: false},
	} {
		if got := hasFullTimelineHLS(tc.protocol, tc.url); got != tc.want {
			t.Errorf("hasFullTimelineHLS(%q, %q) = %v, want %v", tc.protocol, tc.url, got, tc.want)
		}
	}
}

func TestPlayerTrackChangeRequestsNewDirectStreamAndSelectsAudioIndex(t *testing.T) {
	type request struct {
		audio, subtitle float64
	}
	var mu sync.Mutex
	var requests []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/PlaybackInfo") {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			got := request{audio: -999, subtitle: -999}
			if v, ok := body["AudioStreamIndex"].(float64); ok {
				got.audio = v
			}
			if v, ok := body["SubtitleStreamIndex"].(float64); ok {
				got.subtitle = v
			}
			mu.Lock()
			requests = append(requests, got)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"PlaySessionId":"session","MediaSources":[{"Id":"source","Container":"mkv","SupportsDirectStream":true}]}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	store := &jellyfin.MemoryStore{}
	store.SetCredentials(jellyfin.Credentials{UserID: "user", Token: "synthetic-token"})
	client := jellyfin.New(srv.URL, "", "device", store)
	client.SetHTTPClient(srv.Client())
	dir := t.TempDir()
	status := dir + "/status"
	p := &Player{Client: client, Binary: "jfplay", Fifo: dir + "/fifo", Status: status, LogTo: dir + "/log"}
	var cmdMu sync.Mutex
	var child *exec.Cmd
	var launches [][]string
	p.exec = func(_ string, args ...string) *exec.Cmd {
		mu.Lock()
		launches = append(launches, append([]string(nil), args...))
		mu.Unlock()
		state := "playing"
		if hasArg(args, "--hold") {
			state = "paused"
		}
		cmd := exec.Command("sh", "-c", "printf 'state="+state+"\\nposition=12\\nduration=300\\n' > '"+status+"'; exec sleep 30")
		cmdMu.Lock()
		child = cmd
		cmdMu.Unlock()
		return cmd
	}
	// The first item snapshot has no tracks; NewPlaying can receive them later
	// from its asynchronous metadata request.
	item := &jellyfin.Item{ID: "item", SourceID: "source"}
	s, err := p.Start(item, 10)
	if err != nil {
		t.Fatal(err)
	}
	s.writeCommand = func(line string) {
		if line == "toggle" {
			if err := os.WriteFile(status, []byte("state=playing\nposition=25\nduration=300\n"), 0o600); err != nil {
				t.Error(err)
			}
			return
		}
		if line != "stop" {
			return
		}
		cmdMu.Lock()
		defer cmdMu.Unlock()
		if child != nil && child.Process != nil {
			_ = child.Process.Kill()
		}
	}
	// The status file still says playing when the pause and track change are
	// issued back to back.
	s.Send("pause")
	s.Send("tracks 3 -1 25")
	if !s.state.Paused {
		t.Fatal("track restart did not preserve paused state")
	}
	s.Send("toggle")
	if s.state.Paused {
		t.Fatal("toggle after held track restart did not resume")
	}
	s.Stop()

	mu.Lock()
	gotRequests := append([]request(nil), requests...)
	gotLaunches := append([][]string(nil), launches...)
	mu.Unlock()
	if len(gotRequests) != 2 || gotRequests[0] != (request{audio: -999, subtitle: -1}) || gotRequests[1] != (request{audio: 3, subtitle: -1}) {
		t.Fatalf("PlaybackInfo stream selections = %#v", gotRequests)
	}
	if len(gotLaunches) != 2 {
		t.Fatalf("player launches = %d, want 2", len(gotLaunches))
	}
	if containsArgPair(gotLaunches[0], "--audio", "1") || !containsArgPair(gotLaunches[1], "--audio", "3") {
		t.Fatalf("direct audio arguments = %#v", gotLaunches)
	}
	if hasArg(gotLaunches[0], "--hold") || !hasArg(gotLaunches[1], "--hold") {
		t.Fatalf("track restart hold arguments = %#v", gotLaunches)
	}
}

func containsArgPair(args []string, name, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name && args[i+1] == value {
			return true
		}
	}
	return false
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestTrackRestartCommandCarriesSelectedStreamIDs(t *testing.T) {
	item := &jellyfin.Item{
		Audio: []jellyfin.Stream{{ID: "1"}, {ID: "4", Selected: true}},
		Subs:  []jellyfin.Stream{{ID: "7"}},
	}
	if got := trackRestartCommand(item, 33); got != "tracks 4 -1 33" {
		t.Fatalf("track restart command = %q", got)
	}
}

func TestPlayerSeekCoalescingKeepsTrackChangeOrder(t *testing.T) {
	s := &Session{async: true, uiWake: make(chan struct{}, 1)}
	s.SendUI("seek 10")
	s.SendUI("tracks 4 -1 10")
	s.SendUI("seek 20")
	s.SendUI("seek 21")
	want := []string{"seek 10", "tracks 4 -1 10", "seek 21"}
	if strings.Join(s.uiQueue, "|") != strings.Join(want, "|") {
		t.Fatalf("queued commands = %#v, want %#v", s.uiQueue, want)
	}
}

func TestPlayerChildWait(t *testing.T) {
	if testingValue("run") == "^TestPlayerChildWait$" {
		time.Sleep(time.Minute)
	}
}

func TestPlayerChildExit(t *testing.T) {}

func testingValue(name string) string {
	for i, arg := range os.Args {
		if arg == "-test."+name && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if strings.HasPrefix(arg, "-test."+name+"=") {
			return strings.TrimPrefix(arg, "-test."+name+"=")
		}
	}
	return ""
}

func lifecyclePlayer(t *testing.T) (*Player, *atomic.Int32, *atomic.Int32, *atomic.Bool) {
	t.Helper()
	var playbackInfo, stopped atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/PlaybackInfo"):
			playbackInfo.Add(1)
			_, _ = w.Write([]byte(`{"PlaySessionId":"session","MediaSources":[{"Id":"source","Container":"mkv","SupportsDirectStream":true}]}`))
		case strings.HasSuffix(r.URL.Path, "/Stopped"):
			stopped.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	store := &jellyfin.MemoryStore{}
	store.SetCredentials(jellyfin.Credentials{UserID: "user", Token: "synthetic-token"})
	client := jellyfin.New(srv.URL, "", "device", store)
	client.SetHTTPClient(srv.Client())
	dir := t.TempDir()
	p := &Player{Client: client, Binary: "jfplay", Fifo: dir + "/fifo", Status: dir + "/status", LogTo: dir + "/log"}
	var launches atomic.Int32
	var firstMu sync.Mutex
	var first *exec.Cmd
	var replacementAfterReap atomic.Bool
	p.exec = func(_ string, args ...string) *exec.Cmd {
		launch := launches.Add(1)
		testName := "TestPlayerChildExit"
		if launch == 1 {
			testName = "TestPlayerChildWait"
		}
		cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
		firstMu.Lock()
		if launch == 1 {
			first = cmd
		} else if first != nil && first.ProcessState != nil {
			replacementAfterReap.Store(true)
		}
		firstMu.Unlock()
		return cmd
	}
	t.Cleanup(srv.Close)
	return p, &playbackInfo, &stopped, &replacementAfterReap
}

func TestPlayerSeekWaitsForForcedKillAndReportsStoppedOnce(t *testing.T) {
	p, _, stopped, replacementAfterReap := lifecyclePlayer(t)
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	s.Send("seek 25")
	select {
	case <-s.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("player did not finish after the replacement process exited")
	}
	if got := stopped.Load(); got != 1 {
		t.Fatalf("stopped reports = %d, want 1", got)
	}
	if !replacementAfterReap.Load() {
		t.Fatal("replacement process launched before the previous process was reaped")
	}
}

func TestPlayerConcurrentStopAndSeekReportsStoppedOnce(t *testing.T) {
	p, playbackInfo, stopped, _ := lifecyclePlayer(t)
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var ops sync.WaitGroup
	ops.Add(2)
	go func() { defer ops.Done(); <-start; s.Send("seek 25") }()
	go func() { defer ops.Done(); <-start; s.Stop() }()
	close(start)
	done := make(chan struct{})
	go func() { ops.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent stop and seek did not finish")
	}
	select {
	case <-s.Done:
	case <-time.After(time.Second):
		t.Fatal("session did not finish")
	}
	s.Stop()
	s.Send("seek 40")
	if got := stopped.Load(); got != 1 {
		t.Fatalf("stopped reports = %d, want 1", got)
	}
	if got := playbackInfo.Load(); got != 1 {
		t.Fatalf("PlaybackInfo calls = %d, want 1", got)
	}
}

func TestPlayerStopReportsFinalPositionAfterGracefulExit(t *testing.T) {
	var stoppedTicks atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/PlaybackInfo") {
			_, _ = w.Write([]byte(`{"PlaySessionId":"session","MediaSources":[{"Id":"source","Container":"mkv","SupportsDirectStream":true}]}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/Stopped") {
			var body struct {
				PositionTicks int64
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			stoppedTicks.Store(body.PositionTicks)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	store := &jellyfin.MemoryStore{}
	store.SetCredentials(jellyfin.Credentials{UserID: "user", Token: "synthetic-token"})
	client := jellyfin.New(srv.URL, "", "device", store)
	client.SetHTTPClient(srv.Client())
	dir := t.TempDir()
	fifo, status, ready := dir+"/fifo", dir+"/status", dir+"/ready"
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	p := &Player{Client: client, Binary: "jfplay", Fifo: fifo, Status: status, LogTo: dir + "/log"}
	p.exec = func(_ string, _ ...string) *exec.Cmd {
		script := fmt.Sprintf("printf 'state=playing\\nposition=10\\nduration=300\\n' > %s; exec 3<>%s; printf ready > %s; IFS= read -r line <&3; printf 'state=playing\\nposition=77\\nduration=300\\n' > %s", shellQuote(status), shellQuote(fifo), shellQuote(ready), shellQuote(status))
		return exec.Command("sh", "-c", script)
	}
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("player helper did not open its FIFO")
		}
		time.Sleep(5 * time.Millisecond)
	}
	started := time.Now()
	s.Stop()
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("graceful stop took %s, want less than 2 seconds", elapsed)
	}
	if got, want := stoppedTicks.Load(), int64(77*jellyfin.TicksPerSecond); got != want {
		t.Fatalf("stopped position ticks = %d, want %d", got, want)
	}
}

func TestPlayerToggleReportsIntendedStateWhenStatusUpdatesImmediately(t *testing.T) {
	var reportedPaused atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Progress") {
			var body struct {
				IsPaused bool
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			reportedPaused.Store(body.IsPaused)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	store := &jellyfin.MemoryStore{}
	store.SetCredentials(jellyfin.Credentials{UserID: "user", Token: "synthetic-token"})
	client := jellyfin.New(srv.URL, "", "device", store)
	client.SetHTTPClient(srv.Client())
	status := t.TempDir() + "/status"
	if err := os.WriteFile(status, []byte("state=playing\nposition=12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Session{
		p:       &Player{Client: client, Status: status},
		state:   jellyfin.PlayState{ItemID: "item", Position: 12},
		started: true,
		writeCommand: func(line string) {
			if line != "toggle" {
				t.Errorf("command = %q, want toggle", line)
			}
			if err := os.WriteFile(status, []byte("state=paused\nposition=12\n"), 0o600); err != nil {
				t.Error(err)
			}
		},
	}
	s.Send("toggle")
	if !reportedPaused.Load() {
		t.Fatal("progress report missed the intended paused state")
	}
}

func TestPlayerStartAsyncCanBeCanceledDuringPlaybackInfo(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var launches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/PlaybackInfo") {
			close(entered)
			<-release
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer func() {
		close(release)
		srv.Close()
	}()
	store := &jellyfin.MemoryStore{}
	store.SetCredentials(jellyfin.Credentials{UserID: "user", Token: "synthetic-token"})
	client := jellyfin.New(srv.URL, "", "device", store)
	client.SetHTTPClient(srv.Client())
	dir := t.TempDir()
	p := &Player{Client: client, Binary: "jfplay", Fifo: dir + "/fifo", Status: dir + "/status", LogTo: dir + "/log"}
	p.exec = func(_ string, _ ...string) *exec.Cmd {
		launches.Add(1)
		return exec.Command("sh", "-c", "sleep 30")
	}
	started := time.Now()
	s, err := p.StartAsync(&jellyfin.Item{ID: "item", SourceID: "source"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("StartAsync took %s; want an immediate return", elapsed)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("PlaybackInfo did not start in background")
	}
	s.StopUI()
	select {
	case err := <-s.Done:
		if err != nil {
			t.Fatalf("canceled startup returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceling startup did not finish promptly")
	}
	if got := launches.Load(); got != 0 {
		t.Fatalf("launched %d child processes after startup was canceled", got)
	}
}

func TestPlayerSuppressesCanceledStartupOnlyAfterStopRequest(t *testing.T) {
	s := &Session{}
	wrapped := fmt.Errorf("PlaybackInfo failed: %w", context.Canceled)
	if got := s.suppressStopCancellation(wrapped); got != wrapped {
		t.Fatalf("unexpected cancellation without StopUI request: %v", got)
	}
	s.mu.Lock()
	s.stopRequested = true
	s.mu.Unlock()
	if got := s.suppressStopCancellation(wrapped); got != nil {
		t.Fatalf("cancellation after StopUI request = %v, want nil", got)
	}
	unrelated := fmt.Errorf("server rejected playback")
	if got := s.suppressStopCancellation(unrelated); got != unrelated {
		t.Fatalf("unrelated error after StopUI request = %v, want %v", got, unrelated)
	}
}

func TestPlayerImmediateSeekWaitsForInitialStartup(t *testing.T) {
	initialEntered := make(chan struct{})
	releaseInitial := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseInitial) }) }
	var playbackInfo, launches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/PlaybackInfo") {
			if playbackInfo.Add(1) == 1 {
				close(initialEntered)
				<-releaseInitial
			}
			_, _ = w.Write([]byte(`{"PlaySessionId":"session","MediaSources":[{"Id":"source","TranscodingUrl":"/stream.m3u8"}]}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer func() {
		release()
		srv.Close()
	}()
	store := &jellyfin.MemoryStore{}
	store.SetCredentials(jellyfin.Credentials{UserID: "user", Token: "synthetic-token"})
	client := jellyfin.New(srv.URL, "", "device", store)
	client.SetHTTPClient(srv.Client())
	dir := t.TempDir()
	status := dir + "/status"
	p := &Player{Client: client, Binary: "jfplay", Fifo: dir + "/fifo", Status: status, LogTo: dir + "/log"}
	var childMu sync.Mutex
	var first, current *exec.Cmd
	var replacementReaped atomic.Bool
	p.exec = func(_ string, _ ...string) *exec.Cmd {
		cmd := exec.Command("sh", "-c", "printf 'state=playing\\nposition=12\\nduration=300\\n' > '"+status+"'; exec sleep 30")
		childMu.Lock()
		if launches.Add(1) == 1 {
			first = cmd
		} else if first != nil && first.ProcessState != nil {
			replacementReaped.Store(true)
		}
		current = cmd
		childMu.Unlock()
		return cmd
	}
	s, err := p.StartAsync(&jellyfin.Item{ID: "item", SourceID: "source"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.StopUI)
	s.writeCommand = func(line string) {
		if line != "stop" {
			return
		}
		childMu.Lock()
		cmd := current
		childMu.Unlock()
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	select {
	case <-initialEntered:
	case <-time.After(time.Second):
		t.Fatal("initial PlaybackInfo did not start")
	}
	s.SendUI("seek 25")
	time.Sleep(100 * time.Millisecond)
	if got := playbackInfo.Load(); got != 1 {
		t.Fatalf("PlaybackInfo calls before initial response = %d, want 1", got)
	}
	if got := launches.Load(); got != 0 {
		t.Fatalf("children launched before initial response = %d, want 0", got)
	}
	release()
	deadline := time.Now().Add(3 * time.Second)
	for (playbackInfo.Load() < 3 || launches.Load() < 2) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// probe + transcode request at start, one transcode request for the seek
	if got := playbackInfo.Load(); got != 3 {
		t.Fatalf("PlaybackInfo calls = %d, want initial start then seek restart", got)
	}
	if got := launches.Load(); got != 2 {
		t.Fatalf("child launches = %d, want exactly two serialized launches", got)
	}
	if !replacementReaped.Load() {
		t.Fatal("replacement launch happened before the initial child was reaped")
	}
	s.StopUI()
	select {
	case <-s.Done:
	case <-time.After(time.Second):
		t.Fatal("session did not stop after replacement launch")
	}
}

func TestPlayerUISeekAndStopDoNotWaitForDelayedReports(t *testing.T) {
	enteredReport := make(chan struct{})
	releaseReport := make(chan struct{})
	var playbackInfo atomic.Int32
	var reportEntered atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/PlaybackInfo"):
			playbackInfo.Add(1)
			_, _ = w.Write([]byte(`{"PlaySessionId":"session","MediaSources":[{"Id":"source","Container":"mkv","SupportsDirectStream":true}]}`))
		case strings.HasSuffix(r.URL.Path, "/Sessions/Playing"):
			if reportEntered.CompareAndSwap(false, true) {
				close(enteredReport)
			}
			<-releaseReport
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer func() {
		close(releaseReport)
		srv.Close()
	}()
	store := &jellyfin.MemoryStore{}
	store.SetCredentials(jellyfin.Credentials{UserID: "user", Token: "synthetic-token"})
	client := jellyfin.New(srv.URL, "", "device", store)
	client.SetHTTPClient(srv.Client())
	dir := t.TempDir()
	status := dir + "/status"
	p := &Player{Client: client, Binary: "jfplay", Fifo: dir + "/fifo", Status: status, LogTo: dir + "/log"}
	var launches atomic.Int32
	var stopCommands atomic.Int32
	var childMu sync.Mutex
	var child *exec.Cmd
	p.exec = func(_ string, _ ...string) *exec.Cmd {
		launches.Add(1)
		cmd := exec.Command("sh", "-c", "printf 'state=playing\\nposition=15\\nduration=300\\n' > '"+status+"'; exec sleep 30")
		childMu.Lock()
		child = cmd
		childMu.Unlock()
		return cmd
	}
	s, err := p.StartAsync(&jellyfin.Item{ID: "item", SourceID: "source"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.StopUI)
	s.writeCommand = func(line string) {
		if line != "stop" {
			return
		}
		stopCommands.Add(1)
		childMu.Lock()
		cmd := child
		childMu.Unlock()
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	select {
	case <-enteredReport:
	case <-time.After(time.Second):
		t.Fatal("playing report did not reach delayed endpoint")
	}
	s.SendUI("seek 25")
	deadline := time.Now().Add(time.Second)
	for launches.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := launches.Load(); got < 2 {
		t.Fatalf("seek did not launch replacement while HTTP report was blocked (launches=%d, stop commands=%d)", got, stopCommands.Load())
	}
	if got := playbackInfo.Load(); got != 1 {
		t.Fatalf("direct seek PlaybackInfo calls = %d, want 1", got)
	}
	s.StopUI()
	select {
	case <-s.Done:
	case <-time.After(time.Second):
		t.Fatal("stop waited for the blocked reporting endpoint")
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
