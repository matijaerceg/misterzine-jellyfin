package ui

import (
	"time"

	"plexcrt/internal/access"
	"plexcrt/internal/gfx"
	"plexcrt/internal/input"
)

// CodeEntry is the supporter-code entry: a MisterZine code covering the
// extras (package access), saved for good beside the settings. It keeps
// the page it opened over, darkened, under its panel, and runs then once
// a code has been accepted and the success has shown for a moment. The
// beta's playback code entry is a separate screen that official builds
// add; this one only takes supporter codes.
type CodeEntry struct {
	app       *App
	titleFont *gfx.Font
	lineFont  *gfx.Font
	motion    [6]digitMotion
	digitCell *gfx.Canvas
	pageState codeEntryState
	pageReady bool
	page      *gfx.Canvas
	back      *gfx.Canvas
	digits    [6]byte
	selected  int
	message   string
	opened    time.Time // when a code was accepted
	then      func()
}

const (
	codeEntryUnlocked           = 1800 * time.Millisecond // the success shows this long
	codeEntryDigit              = 80 * time.Millisecond   // a digit rolls this long
	codeEntrySurface  gfx.Color = 0x292633
)

type digitMotion struct {
	previous  byte
	direction int
	at        time.Time
}

type codeEntryState struct {
	selected int
	message  string
}

// The entry's title and the paragraph under it, wrapped to the panel: one
// code, entered once, keeps the features of its day for good; later ones
// come with a new code, and nobody has to stay subscribed.
const (
	codeEntryTitle = "UNLOCK FOREVER"
	codeEntryText  = "A code unlocks today's supporter features for good on this card. " +
		"Later features come with a new code. No need to stay subscribed."
	codeEntryLineH = 22
)

// NewCodeEntry is the supporter-code entry over the page on top. then
// runs once a code is accepted.
func NewCodeEntry(a *App, then func()) *CodeEntry {
	b := &CodeEntry{app: a, then: then, back: gfx.NewCanvas(720, 480), titleFont: gfx.Load("bold22"), lineFont: gfx.Load("reg18")}
	if len(a.stack) > 0 {
		a.top().Draw(b.back, time.Now())
	}
	image := &gfx.Image{W: 720, H: 480, Pix: b.back.Pix}
	b.back.BlendSolidClip(0, 0, image, 0, 256-112, 0, 0, 720, 480)
	return b
}

func (b *CodeEntry) Back() bool {
	if !b.opened.IsZero() {
		return true
	}
	b.then = nil
	b.app.Pop()
	return true
}

// Keyboard takes digits and Backspace, keeping the code out of the
// settings and the logs; Escape still cancels.
func (b *CodeEntry) Keyboard(ev input.Event, now time.Time) bool {
	if ev.Text >= '0' && ev.Text <= '9' {
		if !ev.Release && !ev.Repeat && b.opened.IsZero() {
			b.setDigit(byte(ev.Text-'0'), 1, now)
			b.selected = min(5, b.selected+1)
			b.message = ""
		}
		return true
	}
	if ev.ScanCode == 0x66 { // Backspace
		if !ev.Release && b.opened.IsZero() {
			b.selected = max(0, b.selected-1)
			b.setDigit(0, -1, now)
			b.message = ""
		}
		return true
	}
	return ev.Text != 0 // no Space=submit or WASD aliases while typing
}

func (b *CodeEntry) Key(ev input.Event, now time.Time) {
	if ev.Release || !b.opened.IsZero() {
		return
	}
	switch ev.Key {
	case input.Left:
		b.selected = max(0, b.selected-1)
	case input.Right:
		b.selected = min(5, b.selected+1)
	case input.Up:
		b.setDigit((b.digits[b.selected]+1)%10, 1, now)
		b.message = ""
	case input.Down:
		b.setDigit((b.digits[b.selected]+9)%10, -1, now)
		b.message = ""
	case input.Enter:
		if ev.Repeat {
			return
		}
		code := make([]byte, 6)
		for i, d := range b.digits {
			code[i] = '0' + d
		}
		if b.app.accessDir() == "" {
			b.message = "Could not save access. Check storage and retry."
			return
		}
		switch _, err := access.Unlock(b.app.accessDir(), string(code)); err {
		case nil:
			b.opened = now
			b.message = "Unlocked"
			b.app.accessChanged()
		case access.ErrCode:
			b.message = "Invalid code."
		default:
			b.message = "Could not save access. Check storage and retry."
		}
	}
}

// pollRefresh leaves once the success has shown, and runs then.
func (b *CodeEntry) pollRefresh(now time.Time) {
	if b.opened.IsZero() || now.Sub(b.opened) < codeEntryUnlocked {
		return
	}
	then := b.then
	b.then = nil
	b.opened = time.Time{}
	b.app.Pop()
	if then != nil {
		then()
	}
}

// setDigit changes the selected digit at once; its roll only shows it.
// A repeat replaces the roll before it with the newest value.
func (b *CodeEntry) setDigit(value byte, direction int, now time.Time) {
	i := b.selected
	if value == b.digits[i] {
		return
	}
	b.motion[i] = digitMotion{previous: b.digits[i], direction: direction, at: now}
	b.digits[i] = value
}

func (b *CodeEntry) Draw(c *gfx.Canvas, now time.Time) bool {
	if b.page == nil {
		b.page = gfx.NewCanvas(c.W, c.H)
	}
	state := codeEntryState{b.selected, b.message}
	if !b.pageReady || state != b.pageState {
		b.compose(b.page)
		b.pageState = state
		b.pageReady = true
	}
	moving := !b.opened.IsZero()
	b.page.Fill(210, 266, 300, 47, codeEntrySurface)
	if b.digitCell == nil {
		b.digitCell = gfx.NewCanvas(40, 36)
	}
	big := b.app.F.Big
	for i, d := range b.digits {
		dx := 218 + i*48
		col := gfx.GreyHi
		if i == b.selected {
			col = gfx.White
			b.page.Fill(dx+5, 306, 30, 3, gfx.Purple)
		}
		cell := b.digitCell
		cell.Fill(0, 0, 40, 36, codeEntrySurface)
		motion := b.motion[i]
		age := now.Sub(motion.at)
		offset := 0
		if !motion.at.IsZero() && age < codeEntryDigit && age >= 0 {
			remaining := 1 - float64(age)/float64(codeEntryDigit)
			offset = round(32*remaining*remaining) * motion.direction
			moving = true
			cell.Text((40-big.Width("0"))/2, 4+offset-32*motion.direction, big, col, string(rune('0'+motion.previous)))
		}
		cell.Text((40-big.Width("0"))/2, 4+offset, big, col, string(rune('0'+d)))
		b.page.Blit(dx, 268, &gfx.Image{W: 40, H: 36, Pix: cell.Pix})
	}
	c.Blit(0, 0, &gfx.Image{W: b.page.W, H: b.page.H, Pix: b.page.Pix})
	return moving
}

// compose draws the page but for the digits: the darkened page under it,
// the panel, its text and the message.
func (b *CodeEntry) compose(c *gfx.Canvas) {
	c.Blit(0, 0, &gfx.Image{W: b.back.W, H: b.back.H, Pix: b.back.Pix})
	const x, y, w, h = 92, 108, 536, 312
	// lift the surface above dark artwork without a heavy lock-style frame
	c.Fill(x+5, y+5, w, h, 0x08090D)
	c.Fill(x, y, w, h, codeEntrySurface)
	f := b.app.F
	center := func(yy int, font *gfx.Font, color gfx.Color, s string) {
		c.Text((c.W-font.Width(s))/2, yy, font, color, s)
	}
	center(128, b.titleFont, gfx.Purple, codeEntryTitle)
	center(158, b.lineFont, gfx.White, "Your access: "+b.app.Access().Covering())
	for i, line := range wrap(f.Body, codeEntryText, w-16, 3) {
		center(196+i*codeEntryLineH, f.Body, gfx.GreyHi, line)
	}
	for i, line := range wrap(f.SmallBold, b.message, w-32, 2) {
		center(321+i*19, f.SmallBold, gfx.Purple, line)
	}
	center(374, f.Body, gfx.Purple, patreonAddress)
}
