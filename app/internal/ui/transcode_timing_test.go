package ui

import (
	"context"
	"math"
	"net/url"
	"testing"

	"plexcrt/internal/jellyfin"
)

func TestTranscodeFrameRate(t *testing.T) {
	for _, tc := range []struct {
		name                string
		w, h                int
		rate, average, want float64
	}{
		{"film", 1280, 720, 24000.0 / 1001, 0, 24000.0 / 1001},
		{"PAL", 720, 576, 25, 0, 25},
		{"PAL50", 720, 576, 50, 0, 50},
		{"SD beyond block rate", 854, 576, 60, 0, 30},
		{"SD60", 854, 480, 60000.0 / 1001, 0, 60000.0 / 1001},
		{"HD60", 1280, 720, 60, 0, 30},
		{"bad high rate", 712, 480, 120, 0, 60},
		{"unknown", 0, 0, 0, 0, 30},
		{"average only", 720, 480, 0, 30000.0 / 1001, 30000.0 / 1001},
		{"invalid", 720, 480, math.NaN(), 0, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			streams := []jellyfin.MediaStream{{Type: "Video", Width: tc.w, Height: tc.h, RealFrameRate: tc.rate, AverageFrameRate: tc.average}}
			if got := transcodeFrameRate(streams, nil); got != tc.want {
				t.Fatalf("rate %g, want %g", got, tc.want)
			}
		})
	}
}

func TestFallbackRequestsVideoEncodingAndConstantTiming(t *testing.T) {
	f := newFallbackServer(t, eligibleDirect)
	p := f.player(t, nil, "42")
	s := &Session{p: p, item: &jellyfin.Item{ID: "item", SourceID: "source", SourceStreams: []jellyfin.MediaStream{{Type: "Video", Width: 712, Height: 480, RealFrameRate: 30000.0 / 1001}}}}
	info, err := s.playbackInfo(context.Background(), 8000, 42, "1", "-1", true, false)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(info.MediaSources[0].TranscodingURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("VideoCodec") != "h264" || q.Get("AllowVideoStreamCopy") != "false" || q.Get("Framerate") != "29.970030" || q.Get("x") != "1" {
		t.Fatalf("fallback query %v", q)
	}
	if f.bodies[0]["AllowVideoStreamCopy"] != false || f.bodies[0]["StartTimeTicks"] != float64(42*jellyfin.TicksPerSecond) {
		t.Fatalf("fallback request %v", f.bodies[0])
	}
}
