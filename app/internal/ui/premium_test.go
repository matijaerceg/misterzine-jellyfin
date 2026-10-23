package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"plexcrt/internal/access"
	"plexcrt/internal/gfx"
	"plexcrt/internal/input"
)

// registerTestGrant registers one code for the tests. The registry keeps
// it for the process, and the official build's tests run with the real
// registry in place, so each test here uses its own far-future month and a
// code no real grant has.
func registerTestGrant(t *testing.T, month access.Month, code, legacy string) {
	t.Helper()
	sum := sha256.Sum256([]byte(code))
	access.Register(access.Grant{Month: month, SHA256: hex.EncodeToString(sum[:]), LegacyBatch: legacy})
}

func TestGatedRowLockedUntilCovered(t *testing.T) {
	registerTestGrant(t, 999901, "246810", "")
	a := betaTestApp(t)
	a.loadAccess()
	f := access.Feature{Premium: true, Since: 202610}
	row, ok := a.gated(option{label: "Screensaver", val: func() string { return "DVD" }}, f)
	if !ok || !row.locked || !row.supporter || row.val() != "Unlock forever" || row.label != "Screensaver" {
		t.Fatalf("uncovered row: %+v %v", row, ok)
	}
	row.do()
	if _, isEntry := a.top().(*CodeEntry); !isEntry {
		t.Fatalf("OK on a locked row opened %T", a.top())
	}
	for _, ch := range "246810" {
		a.key(input.Event{Keyboard: true, Text: ch, Key: input.None}, time.Now())
	}
	a.key(input.Event{Key: input.Enter}, time.Now())
	if a.Access() != 999901 {
		t.Fatalf("access after the code: %v", a.Access())
	}
	row, ok = a.gated(option{label: "Screensaver", val: func() string { return "DVD" }}, f)
	if !ok || row.locked || !row.supporter || row.val() != "DVD" {
		t.Fatalf("covered row: %+v %v", row, ok)
	}
	// the code is saved: a fresh start keeps it
	a.access = 0
	a.loadAccess()
	if a.Access() != 999901 {
		t.Fatalf("access after a reload: %v", a.Access())
	}
	// a beta extra hides until the toggle is on, code or not
	beta := access.Feature{Premium: true, Beta: true, Since: 202610}
	if _, ok := a.gated(option{label: "Beta thing"}, beta); ok {
		t.Fatal("beta row shown with the toggle off")
	}
	a.Cfg.ShowBeta = true
	if row, ok := a.gated(option{label: "Beta thing"}, beta); !ok || row.locked {
		t.Fatalf("beta row with the toggle on: %+v %v", row, ok)
	}
}

// A wrong code changes nothing and says so; the entry stays.
func TestCodeEntryRefusesAWrongCode(t *testing.T) {
	a := betaTestApp(t)
	b := NewCodeEntry(a, func() { t.Fatal("a wrong code ran the action") })
	a.Push(b)
	for _, ch := range "000000" {
		a.key(input.Event{Keyboard: true, Text: ch, Key: input.None}, time.Now())
	}
	a.key(input.Event{Key: input.Enter}, time.Now())
	if b.message != "Invalid code." || a.Access() != 0 || a.top() != b {
		t.Fatalf("after a wrong code: %q, access %v, top %T", b.message, a.Access(), a.top())
	}
}

func TestPremiumSettingsRoundTrip(t *testing.T) {
	a := betaTestApp(t)
	a.setPremiumSetting("screensaver", "dvd")
	if err := a.Cfg.Save(); err != nil {
		t.Fatal(err)
	}
	saved := LoadConfig(a.Cfg.path)
	if saved.Premium["screensaver"] != "dvd" {
		t.Fatalf("premium settings saved as %v", saved.Premium)
	}
	a.setPremiumSetting("screensaver", "")
	if a.premiumSetting("screensaver") != "" || len(a.PremiumSettings()) != 0 {
		t.Fatal("setting not removed")
	}
}

// drawnText reports whether c shows s in font f and colour col over bg
// with its glyph origin at x, y, as Text draws it: the rows of its ink,
// across its width and a little either side.
func drawnText(c *gfx.Canvas, x, y int, f *gfx.Font, col, bg gfx.Color, s string) bool {
	ref := gfx.NewCanvas(f.Width(s)+4, f.Height())
	ref.Fill(0, 0, ref.W, ref.H, bg)
	ref.Text(2, 0, f, col, s)
	_, top, _, h := f.InkBounds(s)
	for yy := top; yy < top+h; yy++ {
		for xx := 0; xx < ref.W; xx++ {
			if optionPixel(ref, xx, yy) != optionPixel(c, x-2+xx, y+yy) {
				return false
			}
		}
	}
	return true
}

// The supporter-code entry asks to unlock forever and says how, in full
// and inside its panel; a good code says "Unlocked", and the entry leaves
// and runs its action once that has shown.
func TestCodeEntrySaysUnlockForever(t *testing.T) {
	registerTestGrant(t, 999904, "864209", "")
	a := betaTestApp(t)
	ran := 0
	b := NewCodeEntry(a, func() { ran++ })
	a.Push(b)
	c := gfx.NewCanvas(720, 480)
	b.Draw(c, time.Now())
	centred := func(y int, f *gfx.Font, col gfx.Color, s string) bool {
		return drawnText(c, (c.W-f.Width(s))/2, y, f, col, codeEntrySurface, s)
	}
	if !centred(128, b.titleFont, gfx.Purple, "UNLOCK FOREVER") {
		t.Fatal("the code entry's title is not UNLOCK FOREVER")
	}
	// the paragraph, whole, in at most three lines that fit the panel and
	// end above the digits
	lines := wrap(a.F.Body, codeEntryText, 536-16, 3)
	if strings.Join(lines, " ") != codeEntryText || len(lines) > 3 {
		t.Fatalf("the paragraph wraps to %q", lines)
	}
	for i, s := range lines {
		if w := a.F.Body.Width(s); w > 536-16 {
			t.Errorf("%q is %d wide, more than the panel holds", s, w)
		}
		if !centred(196+i*codeEntryLineH, a.F.Body, gfx.GreyHi, s) {
			t.Errorf("line %d under the title is not %q", i+1, s)
		}
	}
	if 196+len(lines)*codeEntryLineH > 268 {
		t.Fatalf("%d lines reach the digits", len(lines))
	}
	for _, ch := range "864209" {
		a.key(input.Event{Keyboard: true, Text: ch, Key: input.None}, time.Now())
	}
	now := time.Now()
	a.key(input.Event{Key: input.Enter}, now)
	if b.message != "Unlocked" || a.Access() != 999904 {
		t.Fatalf("after a good code: %q, access %v", b.message, a.Access())
	}
	b.pollRefresh(now.Add(codeEntryUnlocked - time.Millisecond))
	if a.top() != b || ran != 0 {
		t.Fatal("the entry left before the success showed")
	}
	b.pollRefresh(now.Add(codeEntryUnlocked))
	if a.top() == b || ran != 1 {
		t.Fatalf("after the success: top %T, action ran %d times", a.top(), ran)
	}
}
