package ui

import (
	"slices"
	"sync"
	"testing"
	"time"

	"plexcrt/internal/gfx"
	"plexcrt/internal/input"
	"plexcrt/internal/jellyfin"
)

// The VCR panel fits the safe area on the standard and the wide canvas,
// with the sprites' places inside the controls' rows, on a flat blue band.
func TestVCRPanelFitsAndKeepsTheSpritesPlaces(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a := betaTestApp(t)
		a.Out = &fakeOut{c: gfx.NewCanvas(720, 480)}
		if wide {
			a.Out = &wideFakeOut{&fakeOut{c: gfx.NewCanvas(720, 480)}}
		}
		a.osd = &painterTestOSD{}
		a.Cfg.PlaybackLook = "vcr"
		it := &jellyfin.Item{Type: "episode", SeriesTitle: "A show with a rather long name indeed", Title: "An episode", Season: 2, Index: 7, Duration: 1460}
		p := NewPlaying(a, it, nil, 0, func(string) {})
		p.compose(false)
		left, right := p.btnX[0], p.btnX[len(p.btnX)-1]+p.btnW[len(p.btnW)-1]
		if left < p.safeX() || right > p.safeX()+p.safeW() {
			t.Fatalf("wide %v: keys %d..%d outside the safe area %d..%d", wide, left, right, p.safeX(), p.safeX()+p.safeW())
		}
		if p.pillY+PillH > p.btnY || p.btnY+BarW > OsdControlH {
			t.Fatalf("wide %v: blocks at %d, focus bar at %d, controls end at %d", wide, p.pillY, p.btnY, OsdControlH)
		}
		if p.alpha[0] != OsdAlpha {
			t.Fatalf("wide %v: the band's top edge is eased (alpha %d)", wide, p.alpha[0])
		}
		if got := gfx.Color(p.static.Pix[2])<<16 | gfx.Color(p.static.Pix[1])<<8 | gfx.Color(p.static.Pix[0]); got != vcrBlue {
			t.Fatalf("wide %v: band colour %06x, want %06x", wide, got, vcrBlue)
		}
		p.compose(true)
		if p.peekH <= 0 || p.peekH > p.btnY {
			t.Fatalf("wide %v: peek strip %d rows", wide, p.peekH)
		}
		<-p.closePainter(a.osd)
	}
}

// shakeOSD is coreOSD that can shake, recording what it was told.
type shakeOSD struct {
	coreOSD
	mu     sync.Mutex
	jitter []bool
	shakes int
}

func (s *shakeOSD) SetJitter(on bool) { s.mu.Lock(); s.jitter = append(s.jitter, on); s.mu.Unlock() }
func (s *shakeOSD) Shake()            { s.mu.Lock(); s.shakes++; s.mu.Unlock() }

func (s *shakeOSD) seen() ([]bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.jitter), s.shakes
}

// In the VCR look the controls shake while they are up, a pattern every
// ShakeFields fields, and stop when they go; the standard look never asks.
func TestVCRControlsShakeWhileUp(t *testing.T) {
	for _, look := range []string{"vcr", ""} {
		p := waitPlaying(t)
		p.app.Cfg.PlaybackLook = look
		o := &shakeOSD{}
		at := time.Now()
		fields := func(n int) {
			for range n {
				p.framed(at)
				o.vsync()
				p.Tick(at, o, true)
				p.painter.flush()
				at = at.Add(16683 * time.Microsecond)
			}
		}
		fields(1) // the first frame: the wait is over
		if p.Key(input.Event{Key: input.Enter}, at) || !p.visible {
			t.Fatal("OK did not open the controls")
		}
		fields(4 * ShakeFields)
		jitter, shakes := o.seen()
		if look == "" {
			if len(jitter) != 0 || shakes != 0 {
				t.Fatalf("standard look: jitter %v, %d shakes", jitter, shakes)
			}
			<-p.closePainter(o)
			continue
		}
		if !slices.Equal(jitter, []bool{true}) || shakes < 3 {
			t.Fatalf("controls up: jitter %v, %d shakes; want [true] and a shake every %d fields", jitter, shakes, ShakeFields)
		}
		if p.Key(input.Event{Key: input.Back}, at) || p.visible {
			t.Fatal("Back did not close the controls")
		}
		fields(1)
		jitter, shakes = o.seen()
		fields(4 * ShakeFields)
		if j, s := o.seen(); !slices.Equal(j, []bool{true, false}) || s != shakes {
			t.Fatalf("controls gone: jitter %v, shakes %d then %d", j, shakes, s)
		}
		<-p.closePainter(o)
	}
}

func TestOptionsStepThePlaybackLook(t *testing.T) {
	a := betaTestApp(t)
	o := NewOptions(a)
	a.Push(o)
	row := func() option { return o.items()[optionRow(o, "Playback controls")] }
	if optionRow(o, "Playback controls") < 0 || row().val() != "Standard" {
		t.Fatal("no Playback controls row at Standard")
	}
	pressOption(t, o, "Playback controls", input.Right)
	if a.Cfg.PlaybackLook != "vcr" || row().val() != "VCR" {
		t.Fatalf("right stepped to %q (%q)", a.Cfg.PlaybackLook, row().val())
	}
	pressOption(t, o, "Playback controls", input.Right)
	pressOption(t, o, "Playback controls", input.Left)
	if a.Cfg.PlaybackLook != "" {
		t.Fatalf("left stepped to %q, want Standard", a.Cfg.PlaybackLook)
	}
}
