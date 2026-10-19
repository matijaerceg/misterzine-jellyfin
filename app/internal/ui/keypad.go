package ui

import (
	"time"

	"plexcrt/internal/gfx"
	"plexcrt/internal/input"
)

// keyPad is the on-screen keyboard of the sign-in screens: Search's six
// columns of keys, with lower case, upper case and a page of digits and
// the symbols addresses and passwords need. A USB keyboard types into the
// same text (Keyboard).
type keyPad struct {
	text   string
	key    int
	page   int // 0 lower case, 1 upper case, 2 digits and symbols
	max    int // longest text accepted
	secret bool
	done   string // the wide key's label
}

const (
	padCols   = 6
	padKeys   = 30 // the grid; the wide key under it is padKeys
	padLetter = 26
)

var padPages = [3]string{
	"abcdefghijklmnopqrstuvwxyz",
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	"0123456789.-_:/@!#$%&*+=?~",
}

// padAction is what a key press asks of the screen.
type padAction int

const (
	padNone padAction = iota
	padDone           // the wide key, or Enter on a keyboard
)

func newKeyPad(text, done string, secret bool) *keyPad {
	return &keyPad{text: text, max: 256, secret: secret, done: done}
}

// keys is the current page's labels, the wide key last.
func (k *keyPad) keys() []string {
	keys := make([]string, padKeys+1)
	for i, r := range padPages[k.page] {
		keys[i] = string(r)
	}
	shift, symbols := "ABC", "123"
	if k.page == 1 {
		shift = "abc"
	}
	if k.page == 2 {
		shift, symbols = "abc", "abc"
	}
	keys[26], keys[27], keys[28], keys[29] = shift, symbols, "Delete", "Clear"
	keys[padKeys] = k.done
	return keys
}

func (k *keyPad) add(s string) {
	if len(k.text)+len(s) <= k.max {
		k.text += s
	}
}

func (k *keyPad) del() {
	if len(k.text) > 0 {
		k.text = k.text[:len(k.text)-1]
	}
}

// Key moves over the grid and types; OK on the wide key is padDone.
func (k *keyPad) Key(ev input.Event) padAction {
	if ev.Release {
		return padNone
	}
	switch ev.Key {
	case input.Left:
		if k.key < padKeys && k.key%padCols > 0 {
			k.key--
		}
	case input.Right:
		if k.key < padKeys && k.key%padCols < padCols-1 {
			k.key++
		}
	case input.Up:
		if k.key == padKeys {
			k.key = padKeys - padCols
		} else {
			k.key = max(0, k.key-padCols)
		}
	case input.Down:
		k.key = min(padKeys, k.key+padCols)
	case input.Enter:
		if ev.Repeat {
			return padNone
		}
		switch label := k.keys()[k.key]; {
		case k.key == padKeys:
			return padDone
		case k.key == 26: // shift
			if k.page == 0 {
				k.page = 1
			} else {
				k.page = 0
			}
		case k.key == 27: // digits and symbols, and back
			if k.page == 2 {
				k.page = 0
			} else {
				k.page = 2
			}
		case label == "Delete":
			k.del()
		case label == "Clear":
			k.text = ""
		default:
			k.add(label)
		}
	}
	return padNone
}

// Keyboard types from a USB keyboard: characters, Backspace, Enter. It
// reports whether it took the key, and padDone for Enter.
func (k *keyPad) Keyboard(ev input.Event) (bool, padAction) {
	if ev.Text != 0 {
		if !ev.Release {
			k.add(string(ev.Text))
		}
		return true, padNone
	}
	if ev.ScanCode == 0x66 || ev.ScanCode == 0x171 { // Backspace, Delete
		if !ev.Release {
			k.del()
		}
		return true, padNone
	}
	if ev.Key == input.Enter { // Enter, keypad Enter
		if !ev.Release && !ev.Repeat {
			return true, padDone
		}
		return true, padNone
	}
	return false, padNone
}

// shown is the text as drawn: a password as dots.
func (k *keyPad) shown() string {
	if !k.secret {
		return k.text
	}
	s := make([]byte, len(k.text))
	for i := range s {
		s[i] = '*'
	}
	return string(s)
}

// Draw paints the text line at y and the grid under it, as Search does.
func (k *keyPad) Draw(a *App, c *gfx.Canvas, y int, now time.Time) {
	f := a.F
	shown := k.shown()
	for f.Body.Width(shown) > SafeW-12 && len(shown) > 0 {
		shown = shown[1:] // the end of a long address stays in view
	}
	a.text(c, SafeX, y, f.Body, gfx.White, shown)
	c.Fill(SafeX+f.Body.Width(shown)+2, y+2, 2, f.Body.Height()-4, gfx.Purple)
	c.Fill(SafeX, y+f.Body.Height()+4, SafeW, 2, gfx.Purple)
	top := y + f.Body.Height() + 18
	for i, label := range k.keys() {
		x, ky, w := SafeX+(i%padCols)*48, top+(i/padCols)*42, 44
		if i == padKeys {
			x, ky, w = SafeX, top+5*42, 284
		}
		col := gfx.GreyHi
		if i == k.key {
			c.Fill(x, ky, w, 2, gfx.Purple)
			c.Fill(x, ky+34, w, 2, gfx.Purple)
			c.Fill(x, ky, 2, 36, gfx.Purple)
			c.Fill(x+w-2, ky, 2, 36, gfx.Purple)
			col = gfx.Purple
		}
		font := f.Body
		switch label {
		case "Delete":
			label = "DEL"
		case "Clear":
			label = "CLR"
		}
		if len(label) > 1 {
			font = f.Small
		}
		a.text(c, x+(w-font.Width(label))/2, ky+(36-font.Height())/2, font, col, label)
	}
}
