package gfx

import (
	"strconv"
	"testing"
	"time"
)

// getNow waits for the cache to have rendered s.
func getNow(t testing.TB, tc *TextCache, f *Font, s string) *Image {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if img := tc.Get(f, s, 0xffffff, 0); img != nil {
			return img
		}
		if time.Now().After(deadline) {
			t.Fatalf("line %q never rendered", s)
		}
		time.Sleep(50 * time.Microsecond)
	}
}

// A line drawn every frame stays cached however many other lines come
// and go; the cache used to drop everything at 600 lines.
func TestTextCacheKeepsLinesInUse(t *testing.T) {
	f := Load("med18")
	tc := NewTextCache(1, make(chan struct{}, 1))
	getNow(t, tc, f, "Continue Watching")
	for i := 0; i < 2000; i++ {
		getNow(t, tc, f, "line "+strconv.Itoa(i))
		if tc.Get(f, "Continue Watching", 0xffffff, 0) == nil {
			t.Fatalf("the line in use was dropped after %d others", i+1)
		}
	}
	tc.mu.Lock()
	n := len(tc.cur) + len(tc.old)
	tc.mu.Unlock()
	if n > 2*tc.gen {
		t.Fatalf("%d lines cached, want at most %d", n, 2*tc.gen)
	}
}

func BenchmarkTextCacheHit(b *testing.B) {
	f := Load("med18")
	tc := NewTextCache(1, make(chan struct{}, 1))
	lines := make([]string, 40) // a screenful
	for i := range lines {
		lines[i] = "Season " + strconv.Itoa(i) + "  The Long Way Round"
		getNow(b, tc, f, lines[i])
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if tc.Get(f, lines[i%len(lines)], 0xffffff, 0) == nil {
			b.Fatal("miss")
		}
	}
}
