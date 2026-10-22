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

// The waver stays within a pixel (two at twice the scale), moves from one
// line to the next by a pixel at most, leaves most lines in place, repeats
// for a step and moves on from one step to the next.
func TestJitterShiftsStaySubtle(t *testing.T) {
	a, b := make([]int, 208), make([]int, 208)
	changed, moved, lines := false, 0, 0
	for gen := uint32(0); gen < 2000; gen++ {
		jitterShifts(a, gen, 1)
		jitterShifts(b, gen, 1)
		if !slices.Equal(a, b) {
			t.Fatalf("step %d is not repeatable", gen)
		}
		for y, k := range a {
			if k < -1 || k > 1 || y > 0 && (k-a[y-1] > 1 || a[y-1]-k > 1) {
				t.Fatalf("step %d shifts line %d by %d after %d", gen, y, k, a[max(0, y-1)])
			}
			if k != 0 {
				moved++
			}
			lines++
		}
		jitterShifts(b, gen, 2)
		for y, k := range b {
			if k < -2 || k > 2 {
				t.Fatalf("step %d at scale 2 shifts line %d by %d", gen, y, k)
			}
		}
		jitterShifts(b, gen+1, 1)
		changed = changed || !slices.Equal(a, b)
	}
	if !changed || moved == 0 || moved*2 > lines {
		t.Fatalf("changed %v; %d of %d lines moved, want some and under half", changed, moved, lines)
	}
}

// With the waver on, each of the panel's lines lands where the step puts
// it; Shake shows the next step; off, the lines are back in place.
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
	want := make([]int, h)
	moves := func(gen uint32) bool {
		jitterShifts(want, gen, 1)
		return slices.ContainsFunc(want, func(k int) bool { return k != 0 })
	}
	check := func(gen uint32) {
		t.Helper()
		jitterShifts(want, gen, 1)
		for y, k := range want {
			if got := column(y); got != 360-k {
				t.Fatalf("step %d line %d shows column %d at 360, want %d", gen, y, got, 360-k)
			}
		}
	}
	for !moves(o.jitterGen) {
		o.jitterGen++ // a step that moves a line, so the check tells
	}
	o.SetJitter(true)
	o.Show(0, top, panel, nil)
	check(o.jitterGen)
	o.Shake()
	check(o.jitterGen)
	o.SetJitter(false)
	o.Show(0, top, panel, nil)
	for y := 0; y < h; y++ {
		if got := column(y); got != 360 {
			t.Fatalf("still wavering: line %d shows column %d", y, got)
		}
	}
}
