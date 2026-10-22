package ui

import (
	"bytes"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"plexcrt/internal/gfx"
)

// foreignPresenter is a presenter whose picture is another process's from
// the start: playLoop sees the playback up at once and draws nothing.
type foreignPresenter struct{ c *gfx.Canvas }

func (p *foreignPresenter) Begin() *gfx.Canvas                   { return p.c }
func (p *foreignPresenter) End()                                 {}
func (p *foreignPresenter) WaitField(time.Duration)              {}
func (p *foreignPresenter) Missed() bool                         { return false }
func (p *foreignPresenter) Foreign() bool                        { return true }
func (p *foreignPresenter) Late() (time.Duration, time.Duration) { return 0, 0 }

// More background results than the queue holds arrive during one playback
// (an autoplay posts one per finished episode): none is dropped, none runs
// while the playback has the screen, and all run in order afterwards.
func TestLaterKeepsEveryPostDuringPlayback(t *testing.T) {
	a := &App{Log: log.New(io.Discard, "", 0), Out: &foreignPresenter{gfx.NewCanvas(8, 4)},
		Wake: make(chan struct{}, 1), later: make(chan func(), 32)}
	ctl := &Playing{painter: newPainter()}
	s := &Session{Done: make(chan error, 1)}
	const posts = 40
	var ran []int
	go func() {
		for i := range posts {
			a.Later(func() { ran = append(ran, i) })
			if i%10 == 9 { // a batch at a time, as finished episodes come
				for wait := time.Now().Add(time.Second); len(a.later) != 0 && time.Now().Before(wait); {
					time.Sleep(time.Millisecond)
				}
			}
		}
		s.Done <- nil
	}()
	a.playLoop(s, ctl)
	if len(ran) != 0 {
		t.Fatalf("%d closures ran while the playback had the screen", len(ran))
	}
	a.runLater()
	if len(ran) != posts {
		t.Fatalf("%d of %d closures ran", len(ran), posts)
	}
	for i, v := range ran {
		if v != i {
			t.Fatalf("closure %d ran in place %d", v, i)
		}
	}
}

// A post dropped for a full queue is logged, once until the queue drains.
func TestLaterLogsAFullQueueOnce(t *testing.T) {
	var out bytes.Buffer
	a := &App{Log: log.New(&out, "", 0), Wake: make(chan struct{}, 1), later: make(chan func(), 2)}
	for range 5 {
		a.Later(func() {})
	}
	if n := strings.Count(out.String(), "queue full"); n != 1 {
		t.Fatalf("logged a full queue %d times, want once:\n%s", n, out.String())
	}
	a.runLater()
	for range 3 {
		a.Later(func() {})
	}
	if n := strings.Count(out.String(), "queue full"); n != 2 {
		t.Fatalf("a full queue after a drain logged %d times in all, want 2", n)
	}
}
