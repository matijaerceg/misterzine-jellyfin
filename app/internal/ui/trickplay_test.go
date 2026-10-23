package ui

import (
	"fmt"
	"image"
	"testing"
	"time"

	"plexcrt/internal/gfx"
	"plexcrt/internal/jellyfin"
)

// four frames of 32x18 a sheet, one a second, fourteen in all: four sheets,
// the last holding two
var testTP = jellyfin.Trickplay{Width: 32, Height: 18, TileWidth: 2, TileHeight: 2, Count: 14, Interval: 1000}

func TestTrickplayFindsTheTile(t *testing.T) {
	if n := trickplaySheets(testTP); n != 4 {
		t.Fatalf("%d sheets", n)
	}
	for _, c := range []struct {
		t                  float64
		thumb, sheet, x, y int
	}{{0, 0, 0, 0, 0}, {0.9, 0, 0, 0, 0}, {1, 1, 0, 1, 0}, {3, 3, 0, 1, 1}, {9.5, 9, 2, 1, 0}, {13, 13, 3, 1, 0}, {500, 13, 3, 1, 0}, {-3, 0, 0, 0, 0}} {
		thumb, sheet, x, y := trickplayAt(testTP, c.t)
		if thumb != c.thumb || sheet != c.sheet || x != c.x || y != c.y {
			t.Fatalf("at %v: %d %d %d,%d", c.t, thumb, sheet, x, y)
		}
	}
	if thumb, _, _, _ := trickplayAt(jellyfin.Trickplay{}, 5); thumb != -1 {
		t.Fatal("an empty trickplay has a frame")
	}
}

func TestTrickplayCutsEachTile(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 64, 36))
	if f := trickplayCut(img, testTP, 0, 18, 9); len(f) != 4 || f[3].W != 18 || f[3].H != 9 {
		t.Fatalf("cut %d frames", len(f))
	}
	if f := trickplayCut(img, testTP, 3, 18, 9); len(f) != 2 {
		t.Fatalf("the last sheet cut %d frames", len(f))
	}
	if f := trickplayCut(image.NewGray(image.Rect(0, 0, 64, 20)), testTP, 0, 18, 9); len(f) != 2 {
		t.Fatalf("a short sheet cut %d frames", len(f))
	}
}

func sheetOf(n int) []*gfx.Image {
	f := make([]*gfx.Image, n)
	for i := range f {
		f[i] = &gfx.Image{W: 1, H: 1, Pix: make([]byte, 4)}
	}
	return f
}

// The decoder keeps three sheets: the one under the dot and both
// neighbours, fetched in that order with the one it moves towards first;
// moving on drops only what is none of the three.
func TestTrickplayKeepsThreeSheets(t *testing.T) {
	b := newTrickplayPreview(testTP, 18, 9, 0)
	if n := b.next(); n != 0 {
		t.Fatalf("first fetch is sheet %d", n)
	}
	b.add(0, sheetOf(4))
	if n := b.next(); n != 1 {
		t.Fatalf("then sheet %d", n)
	}
	b.add(1, sheetOf(4))
	b.add(2, sheetOf(4)) // not wanted: not kept
	if len(b.sheets) != 2 || b.next() != -1 {
		t.Fatalf("kept %d sheets, next %d", len(b.sheets), b.next())
	}
	b.Near(5) // sheet 1, heading on into 2: 0 stays as the one behind
	if b.wanted() != [3]int{1, 2, 0} || len(b.sheets) != 2 || b.next() != 2 {
		t.Fatalf("wanted %v, kept %d, next %d", b.wanted(), len(b.sheets), b.next())
	}
	b.add(2, sheetOf(4))
	b.Near(9) // sheet 2, heading on into 3: 0 is dropped
	if b.wanted() != [3]int{2, 3, 1} || len(b.sheets) != 2 || b.sheets[0] != nil || b.next() != 3 {
		t.Fatalf("wanted %v, kept %d, next %d", b.wanted(), len(b.sheets), b.next())
	}
	b.Near(6) // back into sheet 1: 0 comes first, then 2
	if b.wanted() != [3]int{1, 0, 2} || len(b.sheets) != 2 || b.next() != 0 {
		t.Fatalf("going back: wanted %v, kept %d, next %d", b.wanted(), len(b.sheets), b.next())
	}
	b.failed[0] = trickplayTries
	if n := b.next(); n != -1 {
		t.Fatalf("a sheet tried twice is asked again: next %d", n)
	}
}

// The worker prepares from its creation, with no Near: the sheet under
// the start, then the one after, then the one before.
func TestTrickplayPreparesFromTheStart(t *testing.T) {
	b := newTrickplayPreview(testTP, 18, 9, 5)
	var got []int
	for n := b.next(); n >= 0; n = b.next() {
		got = append(got, n)
		b.add(n, sheetOf(4))
	}
	if fmt.Sprint(got) != "[1 2 0]" {
		t.Fatalf("prepared %v", got)
	}
}

// A run widens Frame's look to the frames the dot passes in a decode.
func TestTrickplayFrameDuringARun(t *testing.T) {
	b := newTrickplayPreview(testTP, 18, 9, 0)
	b.add(0, sheetOf(4))
	if b.Frame(11) != nil {
		t.Fatal("a frame from far away at rest")
	}
	b.speed, b.dec, b.movedAt = 4, 1, time.Now()
	if b.Frame(11) == nil {
		t.Fatal("no frame within the run's lead")
	}
}

// Frame never waits: nothing before a sheet is in, then the nearest
// ready frame within a few while the one under the dot is not.
func TestTrickplayFrameBeforeItsSheet(t *testing.T) {
	b := newTrickplayPreview(testTP, 18, 9, 0)
	if b.Frame(0) != nil {
		t.Fatal("a frame before any sheet")
	}
	b.add(0, sheetOf(4))
	if b.Frame(2) == nil || b.Frame(5) == nil {
		t.Fatal("no frame from the ready sheet or near it")
	}
	if b.Frame(12) != nil {
		t.Fatal("a frame from far away")
	}
	b.Close()
	if b.Frame(0) != nil {
		t.Fatal("frames outlived Close")
	}
}

// A run crossing into a sheet still decoding keeps the last frame it was
// given until that sheet lands.
func TestTrickplayFrameAcrossASheetStillDecoding(t *testing.T) {
	b := newTrickplayPreview(testTP, 18, 9, 0)
	b.add(0, sheetOf(4))
	last := b.Frame(3)
	if last == nil {
		t.Fatal("no frame from the ready sheet")
	}
	if b.Frame(11) != nil {
		t.Fatal("a frame from far away at rest")
	}
	b.speed, b.dec, b.movedAt = 2, 0.5, time.Now() // a run past the near look
	if b.Frame(11) != last {
		t.Fatal("the next sheet's frame blinked out before it landed")
	}
}
