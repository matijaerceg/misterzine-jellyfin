package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		{"480p60 main", videoSource(ok480, 0), true},
		{"no probe data trusts server", jellyfin.MediaSource{ID: "source"}, true},
		{"unknown values pass", videoSource(jellyfin.MediaStream{Codec: "h264"}, 0), true},
		{"level as decimal", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.Level = 4.1 }), 0), true},
		{"720p bitrate over HD cap", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.BitRate = 6_500_000 }), 0), false},
		{"480p bitrate within SD cap", videoSource(with(ok480, func(v *jellyfin.MediaStream) { v.BitRate = 8_000_000 }), 0), true},
		{"480p bitrate over SD cap", videoSource(with(ok480, func(v *jellyfin.MediaStream) { v.BitRate = 8_100_000 }), 0), false},
		{"container bitrate stands in", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.BitRate = 0 }), 7_000_000), false},
		{"user bitrate setting is irrelevant", videoSource(with(ok720, func(v *jellyfin.MediaStream) { v.BitRate = 5_900_000 }), 0), true},
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
		got, why := directEligible(c.src)
		if got != c.want {
			t.Errorf("%s: eligible = %v (%s), want %v", c.name, got, why, c.want)
		}
		if !got && why == "" {
			t.Errorf("%s: no reason given", c.name)
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
	f.client = jellyfin.New(f.srv.URL, "", "device", store)
	f.client.SetHTTPClient(f.srv.Client())
	return f
}

func toString(v any) string { s, _ := v.(string); return s }

// player whose workers write a playing status at position, then exit with
// the next code from codes (0 once they run out).
func (f *fallbackServer) player(t *testing.T, codes []int, position string) *Player {
	t.Helper()
	dir := t.TempDir()
	status := dir + "/status"
	p := &Player{Client: f.client, Binary: "jfplay", Fifo: dir + "/fifo", Status: status, LogTo: dir + "/log", Kbps: func() int { return 3000 }}
	p.exec = func(_ string, args ...string) *exec.Cmd {
		f.mu.Lock()
		n := len(f.launches)
		f.launches = append(f.launches, append([]string(nil), args...))
		f.mu.Unlock()
		code := 0
		if n < len(codes) {
			code = codes[n]
		}
		script := "printf 'state=playing\\nposition=" + position + "\\nduration=300\\n' > '" + status + "'; sleep 0.4; exit " + strconv.Itoa(code)
		return exec.Command("sh", "-c", script)
	}
	return p
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
	if b["EnableDirectPlay"] != false || b["EnableDirectStream"] != false || b["MaxStreamingBitrate"] != float64(3000*1000+containerHeadroom) {
		t.Fatalf("transcode body = %#v", b)
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
