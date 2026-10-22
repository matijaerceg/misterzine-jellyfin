package ui

import (
	"strings"

	"plexcrt/internal/gfx"
)

// The VCR look of the playback controls (Options > Playback controls): an
// old deck's on-screen display, white block capitals with a hard shadow on
// a flat blue band, the progress as a row of blocks, and every line of it
// shaking sideways a little as worn tape did (ring.Overlay.SetJitter). The
// controls work as in the standard look; only the drawing differs. The
// subtitles and the wait dots keep their own look and stay still.
var (
	vcrBig   = gfx.PixelFont{SX: 3, SY: 4} // 15x28: titles, the counter, the keys
	vcrSmall = gfx.PixelFont{SX: 2, SY: 3} // 10x21: the episode line, the lists
)

const (
	vcrBlue      gfx.Color = 0x10208C // the band behind the controls
	vcrWhite     gfx.Color = 0xF0F0F0
	vcrShadow    gfx.Color = 0x000000
	vcrGrey      gfx.Color = 0xA8B0D0 // keys and values not under the cursor
	vcrOff       gfx.Color = 0x56609A // keys with nothing to do
	vcrYellow    gfx.Color = 0xF0D040 // Play while paused
	vcrBlockOff  gfx.Color = 0x2C3CA8 // progress still to come
	vcrBuffered  gfx.Color = 0x5868C8 // downloaded ahead of the playhead
	vcrMarkerCol gfx.Color = 0x8C98E8 // an intro or credits marker
	vcrBarH                = 10       // the progress blocks' height
	vcrBlock               = 6        // a block's width
	vcrBlockGap            = 2
	vcrKeyGap              = 22 // between the keys
	// ShakeFields is how many fields each pattern of the shake holds.
	ShakeFields = 3
)

// vcrKeys are the keys' labels in osdButtons' order: the deck's symbols
// for the ten-second steps and Pause, words for the rest.
var vcrKeys = map[string]string{
	"-10": string([]rune{gfx.PixelRewind, gfx.PixelRewind}), "Pause": string(gfx.PixelPause),
	"Play": string(gfx.PixelPlay), "+10": string([]rune{gfx.PixelPlay, gfx.PixelPlay}),
}

func vcrKey(b string) string {
	if k, ok := vcrKeys[b]; ok {
		return k
	}
	return strings.ToUpper(b)
}

// jitterer is an OSD whose rectangle can shake (ring.Overlay).
type jitterer interface {
	SetJitter(on bool)
	Shake()
}

// setJitter says whether what goes up next shakes: the controls in the
// VCR look do, the subtitles and the wait dots do not. It goes through the
// painter, so it lands in order with the panels.
func (p *Playing) setJitter(osd OSD, on bool) {
	on = on && p.app.Cfg.vcr()
	j, ok := osd.(jitterer)
	if !ok || on == p.jittering {
		return
	}
	p.jittering = on
	p.painter.do(func() { j.SetJitter(on) })
}

// shake moves the shake on every ShakeFields fields while shaking
// controls are up, one shake waiting at most: a painter behind on its
// writes skips patterns rather than queueing them.
func (p *Playing) shake(osd OSD, field bool) {
	if !field || !p.jittering || p.last == "" {
		return
	}
	if p.shakeTick++; p.shakeTick%ShakeFields != 0 {
		return
	}
	j, ok := osd.(jitterer)
	if !ok || !p.shaking.CompareAndSwap(false, true) {
		return
	}
	p.painter.do(func() {
		j.Shake()
		p.shaking.Store(false)
	})
}

// composeStaticVCR is composeStatic in the VCR look, with the same places
// for the sprites: the title, the episode line, the blocks, PLAY or PAUSE
// beside the counter (or, with timed, room for the running time at timeY),
// and the keys.
func (p *Playing) composeStaticVCR(c *gfx.Canvas, peek, timed bool) {
	c.Fill(0, 0, c.W, c.H, vcrBlue)
	x0, w := p.safeX(), p.safeW()
	title, sub := heading(p.item)
	y := 12
	c.PixelText(x0, y, vcrBig, vcrWhite, vcrShadow, vcrBig.Fit(title, w))
	y += vcrBig.Height() + 4
	if sub != "" {
		c.PixelText(x0, y, vcrSmall, vcrGrey, vcrShadow, vcrSmall.Fit(sub, w))
	}
	y += vcrSmall.Height() + 10
	p.pillY = y + (vcrBarH-PillH)/2 // the dot rides the blocks' middle
	p.vcrBlocks(c, y)
	y += vcrBarH + 10
	status := string(gfx.PixelPlay) + " PLAY"
	if p.paused {
		status = string(gfx.PixelPause) + " PAUSE"
	}
	c.PixelText(x0, y, vcrBig, vcrWhite, vcrShadow, status)
	if timed {
		p.timeY = y
	} else {
		t := clock(int(p.pos))
		if p.dur > 0 {
			t += " / " + clock(int(p.dur))
		}
		c.PixelText(x0+w-vcrBig.Width(t), y, vcrBig, vcrWhite, vcrShadow, t)
	}
	y += vcrBig.Height() + 14
	if peek {
		p.peekH = y
		return
	}
	labels := make([]string, len(osdButtons))
	total := vcrKeyGap * (len(labels) - 1)
	for i, b := range osdButtons {
		if b == "Pause" && p.paused {
			b = "Play"
		}
		labels[i] = vcrKey(b)
		total += vcrBig.Width(labels[i])
	}
	x := (c.W - total) / 2
	for i, b := range labels {
		col := vcrGrey
		if i == p.focus && !p.scrub {
			col = vcrWhite
		}
		if p.paused && osdButtons[i] == "Pause" {
			col = vcrYellow
		}
		if p.dimmed(i) {
			col = vcrOff
		}
		p.btnX[i], p.btnW[i], p.btnY = x, vcrBig.Width(b), y+vcrBig.Height()+3
		c.PixelText(x, y, vcrBig, col, vcrShadow, b)
		x += vcrBig.Width(b) + vcrKeyGap
	}
}

// vcrBlocks draws the progress as blocks across the safe width, each
// coloured by the film time at its middle: played, in a marker, buffered,
// or still to come.
func (p *Playing) vcrBlocks(c *gfx.Canvas, y int) {
	x0, w := p.safeX(), p.safeW()
	for x := 0; x+vcrBlock <= w; x += vcrBlock + vcrBlockGap {
		col := vcrBlockOff
		if p.dur > 0 {
			t := float64(x+vcrBlock/2) * p.dur / float64(w)
			switch {
			case t <= p.pos:
				col = vcrWhite
			case p.inMarker(t):
				col = vcrMarkerCol
			case t >= p.bufferStart && t < p.bufferEnd:
				col = vcrBuffered
			}
		}
		c.Fill(x0+x, y, vcrBlock, vcrBarH, col)
	}
}

func (p *Playing) inMarker(t float64) bool {
	for _, m := range p.item.Markers {
		if t >= m.Start && t < m.End {
			return true
		}
	}
	return false
}

// vcrRunningTime draws the time the scrub dot stands for, centred in the
// room composeStaticVCR left for it.
func (p *Playing) vcrRunningTime(c *gfx.Canvas) {
	t := clock(int(p.scrubTo))
	c.PixelText(p.safeX()+(p.safeW()-vcrBig.Width(t))/2, p.timeY, vcrBig, vcrWhite, vcrShadow, t)
}

// composeSkipVCR is composeSkip's button in the VCR look.
func (p *Playing) composeSkipVCR(c *gfx.Canvas, label string) {
	c.Fill(0, 0, c.W, c.H, vcrBlue)
	label = strings.ToUpper(label)
	c.PixelText((c.W-vcrBig.Width(label))/2, (c.H-vcrBig.Height())/2, vcrBig, vcrWhite, vcrShadow, label)
}

// drawListVCR is uploadList's column in the VCR look, from column row base.
func (p *Playing) drawListVCR(c *gfx.Canvas, base int) {
	l := p.list
	c.Fill(0, 0, c.W, c.H, vcrBlue)
	x := p.menuX()
	if base == 0 {
		title := l.kind
		if l.hint != "" {
			title += " (" + l.hint + ")"
		}
		f := vcrBig
		if f.Width(title) > OsdListW-x-OsdListPad {
			f = vcrSmall
		}
		c.PixelText(x, 10, f, vcrWhite, vcrShadow, f.Fit(title, OsdListW-x-OsdListPad))
	}
	for i, item := range l.items {
		y := OsdListTop + i*OsdListRowH - base
		if y+OsdListRowH <= 0 || y >= c.H {
			continue
		}
		c.PixelText(x, y+2, vcrSmall, vcrWhite, vcrShadow, vcrSmall.Fit(item, OsdListW-x-52))
		if i == l.set {
			c.PixelText(OsdListW-34, y+2, vcrSmall, vcrWhite, vcrShadow, string(gfx.PixelTick))
		}
		if i < len(l.vals) {
			v := strings.ToUpper(l.vals[i])
			c.PixelText(OsdListW-OsdListPad-vcrSmall.Width(v), y+2, vcrSmall, vcrGrey, vcrShadow, v)
		}
	}
}
