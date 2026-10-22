package ui

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"plexcrt/internal/gfx"
	"plexcrt/internal/jellyfin"
)

// resumeRig is an app whose player plays an item from wherever it is asked
// and stops at stopAt, against a server that holds each stopped report
// until release and answers the item with serverAt until then, after with
// laterAt.
type resumeRig struct {
	a        *App
	logs     *syncBuffer
	release  chan struct{}
	released atomic.Bool
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func newResumeRig(t *testing.T, stopAt float64, serverAt, laterAt int) *resumeRig {
	t.Helper()
	r := &resumeRig{release: make(chan struct{}), logs: &syncBuffer{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/PlaybackInfo"):
			_, _ = w.Write([]byte(`{"PlaySessionId":"session","MediaSources":[{"Id":"source","Container":"mkv","SupportsDirectStream":true}]}`))
		case strings.HasSuffix(req.URL.Path, "/Sessions/Playing/Stopped"):
			<-r.release
			w.WriteHeader(http.StatusNoContent)
		case req.URL.Path == "/Items/item":
			at := serverAt
			if r.released.Load() {
				at = laterAt
			}
			fmt.Fprintf(w, `{"Id":"item","Type":"Movie","Name":"Film","RunTimeTicks":%d,"UserData":{"PlayCount":0,"PlaybackPositionTicks":%d}}`,
				int64(1000)*jellyfin.TicksPerSecond, int64(at)*jellyfin.TicksPerSecond)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(func() {
		r.free()
		srv.Close()
	})
	store := &jellyfin.MemoryStore{}
	store.SetCredentials(jellyfin.Credentials{UserID: "user", Token: "synthetic-token"})
	client := jellyfin.New(srv.URL, "", "device", store)
	client.SetHTTPClient(srv.Client())
	dir := t.TempDir()
	status := dir + "/status"
	p := &Player{Client: client, Binary: "jfplay-test", Fifo: dir + "/fifo", Status: status, LogTo: dir + "/log"}
	p.exec = func(string, ...string) *exec.Cmd {
		script := fmt.Sprintf("printf 'state=playing\\nposition=%g\\nduration=1000\\n' > '%s'", stopAt, status)
		return exec.Command("sh", "-c", script)
	}
	r.a = &App{Log: log.New(r.logs, "", 0), Out: &foreignPresenter{gfx.NewCanvas(8, 4)}, Client: client, Player: p,
		Cfg: &Config{NoTheme: true}, Wake: make(chan struct{}, 1), later: make(chan func(), 32)}
	return r
}

func (r *resumeRig) free() {
	if r.released.CompareAndSwap(false, true) {
		close(r.release)
	}
}

// settle runs the render thread's queued work until done says the
// server's answer after the release has been taken.
func (r *resumeRig) settle(t *testing.T, done func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		r.a.runLater()
		if done() {
			return
		}
	}
	t.Fatal("the server's record was never asked for")
}

func filmCopy() *jellyfin.Item {
	return &jellyfin.Item{ID: "item", Type: "movie", Title: "Film", Duration: 1000, ViewOffset: 702, SourceID: "source"}
}

// Back pressed at 714: the item and every screen's copy of it resume from
// 714 at once, while the stopped report is still on its way and the server
// still says 702, and take the server's record once that is in.
func TestResumePointFollowsPlaybackBeforeTheServer(t *testing.T) {
	r := newResumeRig(t, 714.4, 702, 716)
	it, row, ep := filmCopy(), filmCopy(), filmCopy()
	home := &Home{app: r.a, home: true, hubs: []*jellyfin.Hub{{Title: "Continue Watching", Items: []*jellyfin.Item{row}}}}
	season := &Season{app: r.a, eps: []*jellyfin.Item{ep}}
	r.a.stack = []Screen{home, season}

	r.a.PlayQueue(it, it.ViewOffset, nil, 0, nil)
	for name, c := range map[string]*jellyfin.Item{"played": it, "home row": row, "season": ep} {
		if c.ViewOffset != 714 {
			t.Errorf("%s copy resumes at %d before the report, want 714", name, c.ViewOffset)
		}
	}
	if home.refreshAt.IsZero() {
		t.Error("Home's rows are asked for before the stopped report is in")
	}

	go func() { time.Sleep(200 * time.Millisecond); r.free() }()
	r.a.PlayQueue(it, it.ViewOffset, nil, 0, nil)
	if !strings.Contains(r.logs.String(), "play Film (item) at 714") {
		t.Fatalf("second play did not resume at 714:\n%s", r.logs.String())
	}
	r.settle(t, func() bool { return it.ViewOffset == 716 })
	for name, c := range map[string]*jellyfin.Item{"played": it, "home row": row, "season": ep} {
		if c.ViewOffset != 716 {
			t.Errorf("%s copy at %d after the server's record, want its 716", name, c.ViewOffset)
		}
	}
	if !home.refreshAt.IsZero() {
		t.Error("Home's rows were not asked for again once the report was in")
	}
}

// A playback stopped in its last tenth is watched: no resume point, the
// check mark, in every copy.
func TestResumePointClearsWhenPlaybackEnds(t *testing.T) {
	r := newResumeRig(t, 990, 702, 0)
	it, row := filmCopy(), filmCopy()
	r.a.stack = []Screen{&Home{app: r.a, home: true, hubs: []*jellyfin.Hub{{Items: []*jellyfin.Item{row}}}}}
	r.a.PlayQueue(it, it.ViewOffset, nil, 0, nil)
	for name, c := range map[string]*jellyfin.Item{"played": it, "home row": row} {
		if c.ViewOffset != 0 || c.ViewCount != 1 || !c.Played {
			t.Errorf("%s copy: offset %d, count %d, played %v; want 0, 1, true", name, c.ViewOffset, c.ViewCount, c.Played)
		}
	}
}
