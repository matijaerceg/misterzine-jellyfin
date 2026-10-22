package ui

import (
	"testing"
	"time"

	"plexcrt/internal/gfx"
)

// A presenter that stops calling Frame mid-fade leaves the worker parked
// before a canvas whose step was never handed out; Done (and halt, which
// the transition trace uses) must still stop it at once.
func TestFadeParkedWorkerStopsPromptly(t *testing.T) {
	for _, name := range []string{"Done", "halt"} {
		from, to := gfx.NewCanvas(8, 4), gfx.NewCanvas(8, 4)
		var f Fade
		start := time.Now().Add(-time.Hour) // every step is due: only the ring holds the worker
		f.StartFor(from, to, start, time.Second)
		if f.n <= len(f.steps)+2 {
			t.Fatalf("a %d-step fade in %d canvases does not reuse them", f.n, len(f.steps))
		}
		for f.ready.Load() < 1 {
			time.Sleep(time.Millisecond)
		}
		// one Frame hands out step 1, then the presenter falls silent
		if page, running := f.Frame(start.Add(f.dur * 3 / (2 * time.Duration(f.n)))); !running || page != f.step(1) {
			t.Fatalf("%s: the first frame was not step 1", name)
		}
		// step len+1 takes step 1's canvas, which Frame last handed out
		parked := len(f.steps)
		deadline := time.Now().Add(2 * time.Second)
		for int(f.ready.Load()) < parked {
			if time.Now().After(deadline) {
				t.Fatalf("%s: the worker reached step %d, want %d", name, f.ready.Load(), parked)
			}
			time.Sleep(time.Millisecond)
		}
		time.Sleep(20 * time.Millisecond)
		if got := int(f.ready.Load()); got != parked {
			t.Fatalf("%s: the worker went on to step %d over a canvas still on screen", name, got)
		}
		done := make(chan struct{})
		go func() {
			if name == "Done" {
				f.Done()
			} else {
				f.halt()
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("%s did not return with the worker parked", name)
		}
	}
}
