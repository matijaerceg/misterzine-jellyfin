package ui

import (
	"io"
	"log"
	"testing"
	"time"

	"plexcrt/internal/gfx"
	"plexcrt/internal/jellyfin"
)

type resizeOut struct{ *fakeOut }

func (*resizeOut) LayoutCanvasSize() (int, int) { return 960, 480 }

func TestHomeRebuildsPageAfterCanvasResize(t *testing.T) {
	a := &App{
		Cfg: &Config{NoTheme: true},
		F:   Fonts{Title: gfx.Load("med22"), Body: gfx.Load("med18"), Small: gfx.Load("reg16"), SmallBold: gfx.Load("med16"), Big: gfx.Load("bold28")},
		Art: NewArt(nil, 0, nil),
		Log: log.New(io.Discard, "", 0),
	}
	a.T = gfx.NewTextCache(1, nil)
	a.Mark = NewWordmark()
	a.Out = &resizeOut{fakeOut: &fakeOut{c: gfx.NewCanvas(720, 480)}}
	h := &Home{app: a, home: true, preloadAfter: time.Now().Add(time.Hour)}
	h.setHubs([]*jellyfin.Hub{{Title: "Recently Added", Items: []*jellyfin.Item{{ID: "one", Type: "movie", Title: "A title"}}}})
	now := time.Now()
	h.Draw(gfx.NewCanvas(720, 480), now)
	if h.page == nil || h.page.W != 720 {
		t.Fatalf("initial page size = %v, want 720 wide", h.page)
	}
	h.Draw(gfx.NewCanvas(960, 480), now.Add(time.Second))
	if h.page == nil || h.page.W != 960 || h.page.H != 480 {
		t.Fatalf("resized page is %v, want 960x480", h.page)
	}
}

func TestFadePoolResizesWithCanvas(t *testing.T) {
	warmFades(720, 480)
	f := &Fade{}
	f.Start(gfx.NewCanvas(960, 480), gfx.NewCanvas(960, 480), time.Now())
	t.Cleanup(f.Done)
	if len(f.steps) == 0 || f.steps[0].W != 960 || f.steps[0].H != 480 {
		t.Fatalf("fade step canvas = %v, want 960x480", f.steps[0])
	}
}
