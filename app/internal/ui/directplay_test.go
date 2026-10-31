package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"plexcrt/internal/jellyfin"
)

func videoSource(v jellyfin.MediaStream, bitrate int) jellyfin.MediaSource {
	v.Type = "Video"
	return jellyfin.MediaSource{ID: "source", Bitrate: bitrate, MediaStreams: []jellyfin.MediaStream{v, {Type: "Audio", Index: 1}}}
}

func TestDirectEligible(t *testing.T) {
	ok720 := jellyfin.MediaStream{Codec: "h264", Profile: "High", Level: 41, BitDepth: 8, Width: 1280, Height: 720, RealFrameRate: 23.976, BitRate: 5_000_000}
	ok480 := jellyfin.MediaStream{Codec: "h264", Profile: "Main", Level: 31, BitDepth: 8, Width: 854, Height: 480, RealFrameRate: 59.94, BitRate: 7_500_000}
	with := func(base jellyfin.MediaStream, f func(*jellyfin.MediaStream)) jellyfin.MediaStream {
		f(&base)
		return base
	}
	cases := []struct {
		name string
		src  jellyfin.MediaSource
		want bool
	}{
		{"720p high", videoSource(ok720, 0), true},
		{"narrow 722 lines", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.Width, v.Height, v.RealFrameRate = 968, 722, 29.97 }), 0), true},
		{"rounded block boundary", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.Width, v.Height, v.RealFrameRate = 1280, 721, 29.97 }), 0), false},
		{"narrow tall within budget", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.Width, v.Height, v.RealFrameRate = 960, 960, 29.97 }), 0), true},
		{"narrow tall over rate", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.Width, v.Height, v.RealFrameRate = 848, 1024, 60 }), 0), false},
		{"padded 720p within block budget", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.Width, v.Height, v.RealFrameRate = 1280, 736, 24 }), 0), true},
		{"over block budget", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.Width, v.Height, v.RealFrameRate = 1280, 832, 24 }), 0), false},
		{"480p60 main", videoSource(ok480, 0), true},
		{"no probe data trusts server", jellyfin.MediaSource{ID: "source"}, true},
		{"unknown values pass", videoSource(jellyfin.MediaStream{Codec: "h264"}, 0), true},
		{"level as decimal", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.Level = 4.1 }), 0), true},
		{"720p bitrate over HD cap", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.BitRate = 6_500_000 }), 0), false},
		{"480p bitrate within SD cap", videoSource(with(ok480, func(v *jellyfin.MediaStream) { v.BitRate = 8_000_000 }), 0), true},
		{"480p bitrate over SD cap", videoSource(with(ok480, func(v *jellyfin.MediaStream) { v.BitRate = 8_100_000 }), 0), false},
		{"container bitrate stands in", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.BitRate = 0 }), 7_000_000), false},
		{"720p within HD cap", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.BitRate = 5_900_000 }), 0), true},
		{"720p60", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.RealFrameRate = 59.94 }), 0), false},
		{"720p29.97", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.RealFrameRate = 0; v.AverageFrameRate = 29.97 }), 0), true},
		{"480p over 60", videoSource(with(ok480, func(v *jellyfin.MediaStream) { v.RealFrameRate = 120 }), 0), false},
		{"level 4.2", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.Level = 42 }), 0), false},
		{"10-bit", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.BitDepth = 10 }), 0), false},
		{"High 10 profile", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.Profile = "High 10" }), 0), false},
		{"constrained baseline", videoSource(with(ok480, func(v *jellyfin.MediaStream) { v.Profile = "Constrained Baseline" }), 0), false},
		{"1080p", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.Width, v.Height = 1920, 1080 }), 0), false},
		{"hevc", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.Codec = "hevc" }), 0), false},
		{"interlaced is only a hint", videoSource(with(ok480, func(v *jellyfin.MediaStream) { v.IsInterlaced = true; v.RealFrameRate = 29.97 }), 0), true},
	}
	for _, c := range cases {
		got, why := directEligible(c.src, MaxVideoKbps)
		if got != c.want {
			t.Errorf("%s: eligible = %v (%s), want %v", c.name, got, why, c.want)
		}
		if !got && why == "" {
			t.Errorf("%s: no reason given", c.name)
		}
	}
}

// A source above the user's bitrate cap is transcoded to it, even one the
// core plays: the cap is for a link that cannot carry more.
func TestDirectEligibleRespectsUserCap(t *testing.T) {
	sd := jellyfin.MediaStream{Codec: "h264", Profile: "Main", Level: 30, BitDepth: 8, Width: 720, Height: 480, RealFrameRate: 29.97}
	for _, c := range []struct {
		name         string
		video, total int // bit/s; 0 for unknown
		capKbps      int
		want         bool
	}{
		{"within the lowest step", 380_000, 0, 400, true},
		{"above the lowest step", 900_000, 0, 400, false},
		{"at the 1 Mbps step", 1_000_000, 0, 1000, true},
		{"just above the 1 Mbps step", 1_001_000, 0, 1000, false},
		{"above 2 Mbps", 2_500_000, 0, 2000, false},
		{"container total stands in", 0, 2_500_000, 2000, false},
		{"no cap is the core's", 7_500_000, 0, 0, true},
	} {
		v := sd
		v.BitRate = c.video
		got, why := directEligible(videoSource(v, c.total), c.capKbps)
		if got != c.want {
			t.Errorf("%s: eligible = %v (%s), want %v", c.name, got, why, c.want)
		}
		if !got && !strings.Contains(why, "chosen") {
			t.Errorf("%s: reason %q does not name the chosen cap", c.name, why)
		}
	}
}

func TestWorkerNeedsTranscodeCodes(t *testing.T) {
	for code, want := range map[int]bool{0: false, 3: false, 4: false, 5: true, 6: false, 7: true} {
		if workerNeedsTranscode(code) != want {
			t.Errorf("exit %d: needs transcode = %v", code, !want)
		}
	}
}

// fallbackServer answers a direct stream unless the request disables direct
// play, then a transcode; it records each PlaybackInfo body and report path.
type fallbackServer struct {
	mu       sync.Mutex
	bodies   []map[string]any
	reports  []string
	direct   string // the direct answer's MediaSources JSON
	srv      *httptest.Server
	client   *jellyfin.Client
	launches [][]string
}

func newFallbackServer(t *testing.T, direct string) *fallbackServer {
	t.Helper()
	f := &fallbackServer{direct: direct}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/PlaybackInfo") {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			f.mu.Lock()
			f.bodies = append(f.bodies, body)
			f.mu.Unlock()
			if body["EnableDirectPlay"] == false {
				_, _ = w.Write([]byte(`{"PlaySessionId":"transcode-session","MediaSources":[{"Id":"source","TranscodingUrl":"/stream.ts?x=1","TranscodingSubProtocol":"http"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"PlaySessionId":"direct-session","MediaSources":[` + f.direct + `]}`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.reports = append(f.reports, r.URL.Path+" "+toString(body["PlaySessionId"]))
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(f.srv.Close)
	store := &jellyfin.MemoryStore{}
	store.SetCredentials(jellyfin.Credentials{UserID: "user", Token: "synthetic-token"})
	f.client = testJellyfinClient(f.srv.URL, "", "device", store)
	f.client.SetHTTPClient(f.srv.Client())
	return f
}

func toString(v any) string { s, _ := v.(string); return s }

// player whose workers write a playing status at position, then exit with
// the next code from codes (0 once they run out). A worker whose stream does
// not open (exit 4) writes no status, as jfplay is still in its open then.
// Its bitrate cap is 6 Mbit/s, above eligibleDirect's 4.
func (f *fallbackServer) player(t *testing.T, codes []int, position string) *Player {
	t.Helper()
	dir := t.TempDir()
	status := dir + "/status"
	p := &Player{Client: f.client, Binary: "jfplay", Fifo: dir + "/fifo", Status: status, LogTo: dir + "/log", Kbps: func() int { return 6000 }}
	p.exec = func(_ string, args ...string) *exec.Cmd {
		f.mu.Lock()
		n := len(f.launches)
		f.launches = append(f.launches, append([]string(nil), args...))
		f.mu.Unlock()
		code := 0
		if n < len(codes) {
			code = codes[n]
		}
		state := "playing"
		if code == workerExitOpenTimeout {
			state = "opening" // the stream never came: no picture was played
		}
		script := "printf 'state=" + state + "\\nposition=" + position + "\\nduration=300\\n' > '" + status + "'; "
		if code == workerExitOpenFailed {
			script = ""
		}
		script += "sleep 0.4; exit " + strconv.Itoa(code)
		return exec.Command("sh", "-c", script)
	}
	return p
}

// recordSleeps replaces noPictureSleep for the test with one that does not
// wait and records each wait with the worker launch it came before.
func (f *fallbackServer) recordSleeps(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var sleeps []string
	noPictureSleep = func(d time.Duration) {
		f.mu.Lock()
		next := len(f.launches) + 1
		f.mu.Unlock()
		mu.Lock()
		defer mu.Unlock()
		sleeps = append(sleeps, d.String()+" before launch "+strconv.Itoa(next))
	}
	t.Cleanup(func() { noPictureSleep = time.Sleep })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sleeps...)
	}
}

// transcodeBodies is every PlaybackInfo body that asked for a transcode, in
// order.
func (f *fallbackServer) transcodeBodies() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, b := range f.bodies {
		if b["EnableDirectPlay"] == false {
			out = append(out, b)
		}
	}
	return out
}

// profileHeight is the Height cap of a PlaybackInfo body's device profile.
func profileHeight(b map[string]any) string {
	raw, _ := json.Marshal(b["DeviceProfile"])
	var p deviceProfile
	if json.Unmarshal(raw, &p) != nil || len(p.CodecProfiles) == 0 {
		return ""
	}
	for _, c := range p.CodecProfiles[0].Conditions {
		if c.Property == "Height" {
			return c.Value
		}
	}
	return ""
}

const eligibleDirect = `{"Id":"source","Container":"mkv","SupportsDirectStream":true,"MediaStreams":[{"Type":"Video","Codec":"h264","Profile":"High","Level":40,"BitDepth":8,"Width":1280,"Height":720,"RealFrameRate":23.976,"BitRate":4000000}]}`

func TestDirectProbeUsesCoreCapNotUserBitrate(t *testing.T) {
	f := newFallbackServer(t, eligibleDirect)
	p := f.player(t, nil, "10")
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 1 {
		t.Fatalf("PlaybackInfo calls = %d, want 1", len(f.bodies))
	}
	if f.bodies[0]["MaxStreamingBitrate"] != float64(directProbeBitrate) || f.bodies[0]["EnableDirectPlay"] != nil {
		t.Fatalf("probe body = %#v", f.bodies[0])
	}
	if !hasArg(f.launches[0], "--direct") {
		t.Fatalf("direct launch lacks --direct: %v", f.launches[0])
	}
}

func TestIneligibleDirectRequestsTranscodeAtUserBitrate(t *testing.T) {
	ineligible := strings.Replace(eligibleDirect, `"BitRate":4000000`, `"BitRate":7000000`, 1)
	f := newFallbackServer(t, ineligible)
	p := f.player(t, nil, "10")
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 2 {
		t.Fatalf("PlaybackInfo calls = %d, want probe then transcode", len(f.bodies))
	}
	b := f.bodies[1]
	if b["EnableDirectPlay"] != false || b["EnableDirectStream"] != false || b["MaxStreamingBitrate"] != float64(6000*1000+containerHeadroom) {
		t.Fatalf("transcode body = %#v", b)
	}
	if hasArg(f.launches[0], "--direct") {
		t.Fatalf("transcode launch has --direct: %v", f.launches[0])
	}
}

// A file the core plays but above the chosen bitrate is transcoded to that
// bitrate, at the smaller picture a low cap gets.
func TestDirectAboveUserBitrateRequestsTranscode(t *testing.T) {
	f := newFallbackServer(t, eligibleDirect)
	p := f.player(t, nil, "10")
	p.Kbps = func() int { return 2000 }
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 2 {
		t.Fatalf("PlaybackInfo calls = %d, want probe then transcode", len(f.bodies))
	}
	b := f.bodies[1]
	if b["EnableDirectPlay"] != false || b["MaxStreamingBitrate"] != float64(2000*1000+containerHeadroom) {
		t.Fatalf("transcode body = %#v", b)
	}
	profile, _ := json.Marshal(b["DeviceProfile"])
	if !strings.Contains(string(profile), `"Property":"Height","Value":"480"`) {
		t.Fatalf("transcode profile not capped at 480 lines: %s", profile)
	}
	if hasArg(f.launches[0], "--direct") {
		t.Fatalf("transcode launch has --direct: %v", f.launches[0])
	}
}

func TestDirectFallsBackToTranscodeOnce(t *testing.T) {
	for _, code := range []int{workerExitNeedsTranscode, workerExitRefused} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			f := newFallbackServer(t, eligibleDirect)
			// the direct worker plays to 42 s and asks for a transcode; the
			// transcode worker asks again, which must not loop
			p := f.player(t, []int{code, code}, "42")
			s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 10)
			if err != nil {
				t.Fatal(err)
			}
			err = waitDone(t, s)
			if err == nil {
				t.Fatal("second refusal did not end playback with an error")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.launches) != 2 {
				t.Fatalf("launches = %d, want direct then one transcode", len(f.launches))
			}
			if !hasArg(f.launches[0], "--direct") || hasArg(f.launches[1], "--direct") {
				t.Fatalf("launch args = %v", f.launches)
			}
			if !containsArgPair(f.launches[1], "--base", "42") {
				t.Fatalf("transcode did not resume at the direct position: %v", f.launches[1])
			}
			if len(f.bodies) != 2 || f.bodies[1]["EnableDirectPlay"] != false || f.bodies[1]["StartTimeTicks"] != float64(42*jellyfin.TicksPerSecond) {
				t.Fatalf("PlaybackInfo bodies = %#v", f.bodies)
			}
			want := []string{
				"/Sessions/Playing direct-session",
				"/Sessions/Playing/Stopped direct-session",
				"/Sessions/Playing transcode-session",
				"/Sessions/Playing/Stopped transcode-session",
			}
			if strings.Join(f.reports, ",") != strings.Join(want, ",") {
				t.Fatalf("reports = %v, want %v", f.reports, want)
			}
		})
	}
}

// A stream the core decoded but never showed (exit 9) is played again at the
// same position as a transcode of at most 854x480, once: the second exit 9
// ends playback with the no-picture error, which carries the worker's exit.
// After the direct fallback the small retry is still available. The server
// is given noPictureRestartDelay before the small transcode, and the direct
// fallback no wait.
func TestNoPictureRetriesSmallTranscodeOnce(t *testing.T) {
	for name, tc := range map[string]struct {
		source  string
		codes   []int
		heights []string // each transcode PlaybackInfo's profile height
		sleeps  []string
	}{
		"transcode": {ineligibleDirect, []int{workerExitNoPicture, workerExitNoPicture}, []string{"720", "480"}, []string{"1.5s before launch 2"}},
		"direct":    {eligibleDirect, []int{workerExitNeedsTranscode, workerExitNoPicture, workerExitNoPicture}, []string{"720", "480"}, []string{"1.5s before launch 3"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFallbackServer(t, tc.source)
			p := f.player(t, tc.codes, "42")
			sleeps := f.recordSleeps(t)
			s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 10)
			if err != nil {
				t.Fatal(err)
			}
			err = waitDone(t, s)
			if err == nil || !strings.Contains(err.Error(), "showed no picture") {
				t.Fatalf("playback ended with %v, want the no-picture error", err)
			}
			var ee *exec.ExitError
			if !errors.As(err, &ee) || ee.ExitCode() != workerExitNoPicture {
				t.Fatalf("error %v does not carry the worker's exit", err)
			}
			f.mu.Lock()
			l := append([][]string(nil), f.launches...)
			f.mu.Unlock()
			if len(l) != len(tc.codes) || !containsArgPair(l[len(l)-1], "--base", "42") {
				t.Fatalf("launches %v", l)
			}
			transcodes := f.transcodeBodies()
			if len(transcodes) != len(tc.heights) {
				t.Fatalf("transcode PlaybackInfo bodies %#v", transcodes)
			}
			for i, want := range tc.heights {
				if got := profileHeight(transcodes[i]); got != want {
					t.Errorf("transcode %d capped at %s lines, want %s", i, got, want)
				}
			}
			last := transcodes[len(transcodes)-1]
			if last["StartTimeTicks"] != float64(42*jellyfin.TicksPerSecond) || last["MaxStreamingBitrate"] != float64(6000*1000+containerHeadroom) {
				t.Fatalf("small transcode body %#v", last)
			}
			log, _ := os.ReadFile(p.LogTo)
			if !strings.Contains(string(log), "restarting at 42.000 s as a transcode of at most 854x480") || !strings.Contains(string(log), "small_transcode=true") {
				t.Fatalf("log does not say so: %s", log)
			}
			if got := sleeps(); strings.Join(got, ",") != strings.Join(tc.sleeps, ",") {
				t.Fatalf("waits %v, want %v", got, tc.sleeps)
			}
		})
	}
}

// A small transcode whose stream the server closes unopened (exit 4) soon
// after the no-picture restart is opened once more, after the same wait, at
// the same position and still small; a second exit 4 ends playback with the
// worker's exit. An ordinary transcode that does not open is not retried.
func TestSmallTranscodeReopensOnce(t *testing.T) {
	small := []string{"1.5s before launch 2", "1.5s before launch 3"}
	for name, tc := range map[string]struct {
		codes   []int
		heights []string // each transcode PlaybackInfo's profile height
		sleeps  []string
		fails   bool
	}{
		"plays":    {[]int{workerExitNoPicture, workerExitOpenFailed, 0}, []string{"720", "480", "480"}, small, false},
		"fails":    {[]int{workerExitNoPicture, workerExitOpenFailed, workerExitOpenFailed}, []string{"720", "480", "480"}, small, true},
		"ordinary": {[]int{workerExitOpenFailed}, []string{"720"}, nil, true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFallbackServer(t, ineligibleDirect)
			p := f.player(t, tc.codes, "42")
			sleeps := f.recordSleeps(t)
			s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 10)
			if err != nil {
				t.Fatal(err)
			}
			err = waitDone(t, s)
			if !tc.fails && err != nil {
				t.Fatalf("playback ended with %v, want a clean end", err)
			}
			if tc.fails {
				var ee *exec.ExitError
				if !errors.As(err, &ee) || ee.ExitCode() != workerExitOpenFailed {
					t.Fatalf("playback ended with %v, want the worker's open failure", err)
				}
			}
			f.mu.Lock()
			l := append([][]string(nil), f.launches...)
			f.mu.Unlock()
			if len(l) != len(tc.codes) {
				t.Fatalf("launches %v, want %d", l, len(tc.codes))
			}
			for i := 1; i < len(l); i++ {
				if !containsArgPair(l[i], "--base", "42") {
					t.Fatalf("launch %d did not resume at the first worker's position: %v", i+1, l[i])
				}
			}
			transcodes := f.transcodeBodies()
			if len(transcodes) != len(tc.heights) {
				t.Fatalf("transcode PlaybackInfo bodies %#v", transcodes)
			}
			for i, want := range tc.heights {
				if got := profileHeight(transcodes[i]); got != want {
					t.Errorf("transcode %d capped at %s lines, want %s", i, got, want)
				}
				if i > 0 && transcodes[i]["StartTimeTicks"] != float64(42*jellyfin.TicksPerSecond) {
					t.Errorf("transcode %d at %v ticks, want 42 s", i, transcodes[i]["StartTimeTicks"])
				}
			}
			if got := sleeps(); strings.Join(got, ",") != strings.Join(tc.sleeps, ",") {
				t.Fatalf("waits %v, want %v", got, tc.sleeps)
			}
			log, _ := os.ReadFile(p.LogTo)
			want := 0
			if len(tc.codes) > 1 {
				want = 1
			}
			if n := strings.Count(string(log), "small transcode did not open (jfplay exit 4)"); n != want || (want == 1 && !strings.Contains(string(log), "starting it once more at 42.000 s")) {
				t.Fatalf("log has %d re-open lines, want %d: %s", n, want, log)
			}
		})
	}
}

// A stop asked for while the app waits before the small transcode ends the
// session there: no transcode is asked for and no worker started.
func TestStopDuringNoPictureWaitStartsNothing(t *testing.T) {
	f := newFallbackServer(t, ineligibleDirect)
	p := f.player(t, []int{workerExitNoPicture}, "42")
	var mu sync.Mutex
	var s *Session
	noPictureSleep = func(time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		s.mu.Lock()
		s.stopRequested = true // what StopUI does before its Stop
		s.mu.Unlock()
	}
	t.Cleanup(func() { noPictureSleep = time.Sleep })
	mu.Lock()
	started, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 10)
	s = started
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		stopping := s.stopRequested
		s.mu.Unlock()
		if stopping || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.Stop()
	if err := waitDone(t, s); err != nil {
		t.Fatalf("playback ended with %v, want a clean end", err)
	}
	f.mu.Lock()
	n := len(f.launches)
	f.mu.Unlock()
	if n != 1 {
		t.Fatalf("launches %d, want only the first", n)
	}
	if d := f.transcodeBodies(); len(d) != 1 {
		t.Fatalf("transcode PlaybackInfo bodies %+v, want only the first", d)
	}
}

func TestOtherWorkerErrorsDoNotFallBack(t *testing.T) {
	f := newFallbackServer(t, eligibleDirect)
	p := f.player(t, []int{6}, "42")
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if waitDone(t, s) == nil {
		t.Fatal("stream failure was not reported")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.launches) != 1 {
		t.Fatalf("launches = %d, want 1", len(f.launches))
	}
}

func waitDone(t *testing.T, s *Session) error {
	t.Helper()
	select {
	case err := <-s.Done:
		return err
	case <-time.After(6 * time.Second):
		t.Fatal("session did not end")
		return nil
	}
}

func TestTallDirectSourceUsesWorkerChecks(t *testing.T) {
	source := strings.Replace(strings.Replace(eligibleDirect, `"Width":1280`, `"Width":968`, 1), `"Height":720`, `"Height":722`, 1)
	f := newFallbackServer(t, source)
	p := f.player(t, nil, "10")
	session, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, session)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 1 || !hasArg(f.launches[0], "--direct") {
		t.Fatal("tall eligible source did not use checked direct playback")
	}
}

// ineligibleDirect is a source the core cannot play as stored (HEVC): the
// probe's answer sends the session to a transcode.
const ineligibleDirect = `{"Id":"source","Container":"mkv","SupportsDirectStream":true,"MediaStreams":[{"Type":"Video","Codec":"hevc","Profile":"Main","Level":40,"BitDepth":8,"Width":1280,"Height":720,"RealFrameRate":23.976,"BitRate":4000000}]}`

// transcodeStarts is the StartTimeTicks of every PlaybackInfo that asked
// for a transcode, in seconds, in order.
func (f *fallbackServer) transcodeStarts() []float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []float64
	for _, b := range f.bodies {
		if b["EnableDirectPlay"] == false {
			ticks, _ := b["StartTimeTicks"].(float64)
			out = append(out, ticks/float64(jellyfin.TicksPerSecond))
		}
	}
	return out
}

// A transcode started at an offset is given the open timeout; when its
// worker sees no stream (exit 8) playback ends with the no-video error,
// which carries the worker's exit. A start from the beginning and a direct
// stream get no open timeout but end the same way.
func TestOpenTimeoutEndsPlaybackWithNoVideoError(t *testing.T) {
	for name, tc := range map[string]struct {
		source  string
		offset  int
		timeout string
	}{"resume": {ineligibleDirect, 375, "20"}, "from-start": {ineligibleDirect, 0, ""}, "direct": {eligibleDirect, 10, ""}} {
		t.Run(name, func(t *testing.T) {
			f := newFallbackServer(t, tc.source)
			p := f.player(t, []int{workerExitOpenTimeout}, "1")
			s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, tc.offset)
			if err != nil {
				t.Fatal(err)
			}
			err = waitDone(t, s)
			if err == nil || !strings.Contains(err.Error(), "sent no video") {
				t.Fatalf("playback ended with %v, want the no-video error", err)
			}
			var ee *exec.ExitError
			if !errors.As(err, &ee) || ee.ExitCode() != workerExitOpenTimeout {
				t.Fatalf("error %v does not carry the worker's exit", err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.launches) != 1 {
				t.Fatalf("launches %v, want one", f.launches)
			}
			if hasArg(f.launches[0], "--open-timeout") != (tc.timeout != "") || (tc.timeout != "" && !containsArgPair(f.launches[0], "--open-timeout", tc.timeout)) {
				t.Fatalf("open timeout: %v", f.launches[0])
			}
		})
	}
}

// Jellyfin numbers a subtitle file beside the video before the file's own
// streams, so the selected audio track's Jellyfin index is one more than its
// index in the file; jfplay is asked for the file's. A source without
// external files passes the index through; an external audio track cannot
// play from the direct stream.
func TestDirectAudioIndexDiscountsExternalStreams(t *testing.T) {
	streams := []jellyfin.MediaStream{
		{Type: "Subtitle", Index: 0, IsExternal: true, Codec: "subrip"},
		{Type: "Video", Index: 1, Codec: "h264"},
		{Type: "Audio", Index: 2, Codec: "aac", IsDefault: true},
		{Type: "Audio", Index: 3, Codec: "ac3"},
		{Type: "Audio", Index: 4, Codec: "aac", IsExternal: true},
	}
	for _, tc := range []struct {
		index, want int
		fails       bool
	}{{2, 1, false}, {3, 2, false}, {4, 0, true}, {1, 0, true}, {9, 9, false}} {
		got, err := fileAudioIndex(streams, tc.index)
		if (err != nil) != tc.fails || got != tc.want {
			t.Errorf("fileAudioIndex(%d) = %d, %v; want %d, fails=%v", tc.index, got, err, tc.want, tc.fails)
		}
	}
	if got, err := fileAudioIndex(streams[1:4], 2); err != nil || got != 2 {
		t.Errorf("without external streams: %d, %v", got, err)
	}

	f := newFallbackServer(t, eligibleDirect)
	p := f.player(t, nil, "1")
	item := &jellyfin.Item{ID: "item", SourceID: "source", SourceStreams: streams,
		Audio: []jellyfin.Stream{{ID: "2", Selected: true}, {ID: "3"}}}
	s, err := p.Start(item, 0)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.launches) != 1 || !containsArgPair(f.launches[0], "--audio", "1") {
		t.Fatalf("direct launch %v, want --audio 1 for Jellyfin stream 2", f.launches)
	}
	if len(f.bodies) != 1 || f.bodies[0]["AudioStreamIndex"] != 2.0 {
		t.Fatalf("PlaybackInfo bodies %v, want AudioStreamIndex 2", f.bodies)
	}
}
