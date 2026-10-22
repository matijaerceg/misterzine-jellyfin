package ring

import (
	"slices"
	"testing"
	"unsafe"

	"plexcrt/internal/gfx"
)

func TestShiftRowContinuesTheEdge(t *testing.T) {
	src := []uint32{1, 2, 3, 4, 5, 6, 7, 8}
	for _, c := range []struct {
		k    int
		want []uint32
	}{
		{0, []uint32{1, 2, 3, 4, 5, 6, 7, 8}},
		{2, []uint32{1, 1, 1, 2, 3, 4, 5, 6}},
		{-3, []uint32{4, 5, 6, 7, 8, 8, 8, 8}},
		{20, []uint32{1, 1, 1, 1, 1, 1, 1, 1}},
		{-20, []uint32{8, 8, 8, 8, 8, 8, 8, 8}},
	} {
		dst := make([]uint32, len(src))
		shiftRow(dst, src, c.k)
		if !slices.Equal(dst, c.want) {
			t.Fatalf("shift %d = %v, want %v", c.k, dst, c.want)
		}
	}
}

// The shake stays within a few pixels, repeats for a pattern, and moves on
// from one pattern to the next.
func TestJitterShiftsStaySmall(t *testing.T) {
	a, b := make([]int, 208), make([]int, 208)
	changed := false
	for gen := uint32(0); gen < 2000; gen++ {
		jitterShifts(a, gen, 1)
		jitterShifts(b, gen, 1)
		if !slices.Equal(a, b) {
			t.Fatalf("pattern %d is not repeatable", gen)
		}
		for y, k := range a {
			if k < -9 || k > 9 {
				t.Fatalf("pattern %d shifts line %d by %d", gen, y, k)
			}
		}
		jitterShifts(b, gen+1, 1)
		changed = changed || !slices.Equal(a, b)
	}
	if !changed {
		t.Fatal("the patterns never change")
	}
}

// With the shake on, each of the panel's lines lands where the pattern
// puts it; Shake shows the next pattern; off, the lines are back in place.
func TestOverlayShakesThePanelsLines(t *testing.T) {
	f := newFake(t, false)
	o := f.r.Overlay()
	const top, h = 100, 40
	panel := gfx.NewCanvas(W, h)
	px := unsafe.Slice((*uint32)(unsafe.Pointer(&panel.Pix[0])), W*h)
	for i := range px {
		px[i] = uint32(i % W) // each pixel holds its column
	}
	column := func(y int) int { return int(f.r.pages[f.r.cur][(top+y)*W+360] & 0xFFFFFF) }
	check := func(gen uint32) {
		t.Helper()
		want := make([]int, h)
		jitterShifts(want, gen, 1)
		moved := false
		for y, k := range want {
			if got := column(y); got != 360-k {
				t.Fatalf("pattern %d line %d shows column %d at 360, want %d", gen, y, got, 360-k)
			}
			moved = moved || k != 0
		}
		if !moved {
			t.Fatalf("pattern %d moved no line", gen)
		}
	}
	o.SetJitter(true)
	o.Show(0, top, panel, nil)
	check(0)
	o.Shake()
	check(1)
	o.SetJitter(false)
	o.Show(0, top, panel, nil)
	for y := 0; y < h; y++ {
		if got := column(y); got != 360 {
			t.Fatalf("still shaking: line %d shows column %d", y, got)
		}
	}
}
