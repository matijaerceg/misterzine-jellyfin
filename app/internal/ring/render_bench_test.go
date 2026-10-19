package ring

import (
	"testing"
	"unsafe"

	"plexcrt/internal/gfx"
)

func benchmarkRing(b *testing.B, w, h int, native, wide bool) *Ring {
	b.Helper()
	words := make([]uint32, ctrlLen/4)
	put := func(word int, lo, hi uint32) {
		words[2*word], words[2*word+1] = lo, hi
	}
	put(wVersion, 0x103, 0)
	put(wSeqHead, 1, 0)
	put(wSeqTail, 1, 0)
	put(wRaster, uint32(w)|uint32(h)<<12|uint32(map[bool]int{true: 1}[native])<<24, 0)
	put(wAspect, 16|9<<12, 0)
	if wide {
		put(wDisplay, 1<<6, 0)
	}
	fb := make([]byte, NPage*maxPageSize)
	r := attach(words, fb, fbByte)
	r.setOutput(w, h)
	r.setPages()
	return r
}

func benchmarkFrame(b *testing.B, w, h int, native, wide bool) {
	r := benchmarkRing(b, w, h, native, wide)
	cw, ch := W, H
	if wide {
		cw = 960
	}
	c := gfx.NewCanvas(cw, ch)
	px := unsafe.Slice((*uint32)(unsafe.Pointer(&c.Pix[0])), cw*ch)
	for i := range px {
		px[i] = uint32(i) * 2654435761 & 0xFFFFFF
	}
	b.SetBytes(int64(cw * ch * 4))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.mu.Lock()
		r.publishFrame(c.Pix, 0, ch-1)
		r.pending = false // benchmark pixel work; do not wait on a simulated core
		r.mu.Unlock()
	}
}

func BenchmarkPublishFrame(b *testing.B) {
	b.Run("720x480", func(b *testing.B) { benchmarkFrame(b, 720, 480, false, false) })
	b.Run("720x480-wide", func(b *testing.B) { benchmarkFrame(b, 720, 480, false, true) })
	b.Run("720x576", func(b *testing.B) { benchmarkFrame(b, 720, 576, false, false) })
	b.Run("720x576-wide", func(b *testing.B) { benchmarkFrame(b, 720, 576, false, true) })
	b.Run("1280x720", func(b *testing.B) { benchmarkFrame(b, 1280, 720, true, true) })
}

func BenchmarkMovingPlaybackPanel(b *testing.B) {
	r := benchmarkRing(b, 1280, 720, true, true)
	o := r.Overlay()
	o.SetCanvasSize(960, 480)
	panel := gfx.NewCanvas(360, 100)
	px := unsafe.Slice((*uint32)(unsafe.Pointer(&panel.Pix[0])), panel.W*panel.H)
	for i := range px {
		px[i] = 0x00203040 | uint32(i&0xFF)
	}
	alpha := make([]byte, panel.W*panel.H)
	for i := range alpha {
		alpha[i] = 220
	}
	b.SetBytes(int64(panel.W * panel.H * 5))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		o.Show((i%180)-40, 330, panel, alpha)
		r.mu.Lock()
		r.pending = false // no hardware or core wait in this benchmark
		r.mu.Unlock()
	}
}
