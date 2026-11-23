package ui

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"plexcrt/internal/jellyfin"
)

// waitingPlayer is f's player with a worker that stays in its open for
// seconds, as jfplay does on a stream that does not come, then exits 0.
func (f *fallbackServer) waitingPlayer(t *testing.T, seconds string) *Player {
	t.Helper()
	p := f.player(t, nil, "0")
	p.stopGrace = 50 * time.Millisecond
	p.exec = func(_ string, args ...string) *exec.Cmd {
		f.mu.Lock()
		f.launches = append(f.launches, append([]string(nil), args...))
		f.mu.Unlock()
		return exec.Command("sh", "-c", "exec sleep "+seconds)
	}
	return p
}

// workerGet asks the newest worker's gateway URL as jfplay would.
func (f *fallbackServer) workerGet(t *testing.T) int {
	t.Helper()
	f.mu.Lock()
	args := f.launches[len(f.launches)-1]
	f.mu.Unlock()
	for i, arg := range args {
		if arg == "--gateway" && i+1 < len(args) {
			resp, err := http.Get(args[i+1])
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			return resp.StatusCode
		}
	}
	t.Fatal("worker was not given a gateway URL")
	return 0
}

func TestRefusedTranscodeEndsPlaybackAtOnce(t *testing.T) {
	f := newFallbackServer(t, eligibleDirect)
	f.media = func(int) int { return 500 }
	p := f.waitingPlayer(t, "30")
	p.DirectPlay = func() bool { return false }
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	if status := f.workerGet(t); status != 502 {
		t.Fatalf("worker answer %d, want 502", status)
	}
	err = waitDone(t, s)
	var refused transcodeRefusedError
	if !errors.As(err, &refused) || err.Error() != "The server could not transcode this title (HTTP 500)" {
		t.Fatalf("session ended with %v", err)
	}
	if took := time.Since(began); took > 3*time.Second {
		t.Fatalf("playback ended after %v, not at once", took)
	}
	data, err := os.ReadFile(p.LogTo + ".failed")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"app: gateway: media request failed before any media was delivered: the server answered HTTP 500\n",
		"app: transcode refused (HTTP 500)",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("playback log lacks %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "127.0.0.1") || strings.Contains(string(data), "synthetic-token") {
		t.Errorf("playback log names an address or the token:\n%s", data)
	}
	select {
	case <-s.ReportsDone():
	case <-time.After(3 * time.Second):
		t.Fatal("no stopped report")
	}
	f.mu.Lock()
	reports := strings.Join(f.reports, "\n")
	f.mu.Unlock()
	if !strings.Contains(reports, "/Sessions/Playing/Stopped transcode-session") {
		t.Errorf("the server was not told the transcode stopped: %s", reports)
	}
}

// A refusal the app does not end playback for is still in the log: a stream
// played as stored keeps the worker's handling, and so does one that was
// already playing.
func TestOtherGatewayRefusalsAreOnlyLogged(t *testing.T) {
	for _, test := range []struct {
		name     string
		direct   bool
		media    func(int) int
		requests int
		want     string
	}{
		{"direct stream", true, func(int) int { return 404 }, 1, "app: gateway: media request failed before any media was delivered: the server answered HTTP 404\n"},
		{"playing transcode", false, func(n int) int {
			if n == 1 {
				return 200
			}
			return 500
		}, 2, "app: gateway: media request failed after media was delivered: the server answered HTTP 500\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFallbackServer(t, eligibleDirect)
			f.media = test.media
			p := f.waitingPlayer(t, "1")
			p.DirectPlay = func() bool { return test.direct }
			s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 0)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < test.requests; i++ {
				f.workerGet(t)
			}
			data, err := os.ReadFile(p.LogTo)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), test.want) || strings.Contains(string(data), "transcode refused") {
				t.Errorf("playback log:\n%s", data)
			}
			if err := waitDone(t, s); err != nil {
				t.Fatalf("session ended with %v, want the worker's own clean end", err)
			}
		})
	}
}

// A playing stream whose every remaining segment is refused does not fill
// the log with one line each.
func TestGatewayFailureLogIsBounded(t *testing.T) {
	f := newFallbackServer(t, eligibleDirect)
	f.media = func(n int) int {
		if n == 1 {
			return 200
		}
		return 500
	}
	p := f.waitingPlayer(t, "1")
	p.DirectPlay = func() bool { return false }
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		f.workerGet(t)
	}
	data, err := os.ReadFile(p.LogTo)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "the server answered HTTP 500\n"); n != gatewayLogLimit {
		t.Errorf("%d failure lines, want %d", n, gatewayLogLimit)
	}
	if n := strings.Count(string(data), "app: gateway: more requests of this stream failed; not logged\n"); n != 1 {
		t.Errorf("%d closing notes, want 1:\n%s", n, data)
	}
	if err := waitDone(t, s); err != nil {
		t.Fatalf("session ended with %v", err)
	}
}

// A stream the server stops sending part-way ends as jfplay's clean exit;
// the session reports it as lost unless it was at its end or recovered.
func TestStreamTheServerStoppedSendingIsAFailure(t *testing.T) {
	refuseAfterFirst := func(n int) int {
		if n == 1 {
			return 200
		}
		return 500
	}
	for _, test := range []struct {
		name     string
		position string
		media    func(int) int
		requests int
		want     string
	}{
		{"lost mid-way", "111", refuseAfterFirst, 4, "The server stopped sending video (HTTP 500)"},
		{"refused at the very end", "290", refuseAfterFirst, 4, ""},
		{"recovered", "111", func(n int) int {
			if n == 2 {
				return 500
			}
			return 200
		}, 3, ""},
		{"no failure", "111", func(int) int { return 200 }, 2, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFallbackServer(t, eligibleDirect)
			f.media = test.media
			p := f.player(t, nil, test.position)
			p.DirectPlay = func() bool { return false }
			status := p.Status
			p.exec = func(_ string, args ...string) *exec.Cmd {
				f.mu.Lock()
				f.launches = append(f.launches, append([]string(nil), args...))
				f.mu.Unlock()
				return exec.Command("sh", "-c", "printf 'state=ended\\nposition="+test.position+"\\nduration=300\\n' > '"+status+"'; exec sleep 1")
			}
			s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 0)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < test.requests; i++ {
				f.workerGet(t)
			}
			err = waitDone(t, s)
			if test.want == "" {
				if err != nil {
					t.Fatalf("session ended with %v, want a clean end", err)
				}
				return
			}
			var lost streamLostError
			if !errors.As(err, &lost) || err.Error() != test.want {
				t.Fatalf("session ended with %v, want %q", err, test.want)
			}
			data, err := os.ReadFile(p.LogTo + ".failed")
			if err != nil {
				t.Fatal(err)
			}
			if want := "app: stream lost: the last 3 media requests failed (the server answered HTTP 500) and playback ended at 111 of 300 s\n"; !strings.Contains(string(data), want) {
				t.Errorf("playback log lacks %q:\n%s", want, data)
			}
		})
	}
}

func TestStreamLostWording(t *testing.T) {
	if got := (streamLostError{0}).Error(); got != "The server stopped sending video (connection lost)" {
		t.Error(got)
	}
}

// A stop that arrives with the refusal wins: no failure is shown for a
// playback the user already left.
func TestRefusalAfterStopIsNotAFailure(t *testing.T) {
	f := newFallbackServer(t, eligibleDirect)
	f.media = func(int) int { return 500 }
	p := f.waitingPlayer(t, "30")
	p.DirectPlay = func() bool { return false }
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.stopRequested = true
	s.mu.Unlock()
	f.workerGet(t)
	s.Stop()
	if err := waitDone(t, s); err != nil {
		t.Fatalf("stopped session ended with %v", err)
	}
}
