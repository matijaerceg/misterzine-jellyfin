package ui

import (
	"net/url"
	"plexcrt/internal/jellyfin"
	"strings"
	"testing"
)

const surroundSource = `{"Id":"source","Container":"mkv","TranscodingUrl":"/master.m3u8?VideoCodec=h264&AudioCodec=aac&ApiKey=synthetic-token","TranscodingSubProtocol":"hls","DefaultAudioStreamIndex":1,"MediaStreams":[{"Type":"Video","Codec":"h264","Profile":"High","Level":40,"BitDepth":8,"Width":968,"Height":722,"RealFrameRate":29.97,"BitRate":3000000},{"Type":"Audio","Index":1,"Codec":"aac","Channels":6},{"Type":"Audio","Index":2,"Codec":"aac","Channels":2}]}`

func TestAudioConversionKeepsOriginalVideoGuardAndFallback(t *testing.T) {
	f := newFallbackServer(t, surroundSource)
	p := f.player(t, []int{workerExitNeedsTranscode, 0}, "42")
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.launches) != 2 || !hasArg(f.launches[0], "--direct") || hasArg(f.launches[1], "--direct") {
		t.Fatalf("copy guard/fallback: %v", f.launches)
	}
	args := f.launches[0]
	u, _ := url.Parse(args[len(args)-1])
	q := u.Query()
	if q.Get("VideoCodec") != "copy" || q.Get("AudioCodec") != "aac" || q.Get("MaxAudioChannels") != "2" || q.Get("AllowAudioStreamCopy") != "false" || q.Has("ApiKey") {
		t.Fatalf("audio-only URL: %v", q)
	}
	if !containsArgPair(args, "--base", "0") || !containsArgPair(args, "--start-in-file", "10") || hasArg(args, "--audio") {
		t.Fatalf("wrong HLS timeline/track: %v", args)
	}
	if len(f.bodies) != 2 || f.bodies[1]["EnableDirectStream"] != false || f.bodies[1]["StartTimeTicks"] != float64(42*jellyfin.TicksPerSecond) {
		t.Fatalf("fallback: %v", f.bodies)
	}
}

func TestAudioConversionCannotBypassVideoCap(t *testing.T) {
	f := newFallbackServer(t, strings.Replace(surroundSource, `"Width":968,"Height":722`, `"Width":1920,"Height":1080`, 1))
	p := f.player(t, nil, "10")
	s, err := p.Start(&jellyfin.Item{ID: "item", SourceID: "source"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	if len(f.launches) != 1 || hasArg(f.launches[0], "--direct") {
		t.Fatalf("unsafe video copy: %v", f.launches)
	}
	if len(f.bodies) != 2 || f.bodies[1]["EnableDirectStream"] != false {
		t.Fatalf("video encode not forced: %v", f.bodies)
	}
}

func TestStereoPolicyUsesSelectedAudioTrack(t *testing.T) {
	index := 1
	src := jellyfin.MediaSource{DefaultAudioStream: &index, MediaStreams: []jellyfin.MediaStream{{Type: "Audio", Index: 1, Codec: "aac", Channels: 6}, {Type: "Audio", Index: 2, Codec: "aac", Channels: 2}, {Type: "Audio", Index: 3, Codec: "aac", Channels: 1}}}
	if !needsStereoConversion(src, "") || !needsStereoConversion(src, "1") || needsStereoConversion(src, "2") || needsStereoConversion(src, "3") {
		t.Fatal("selected/default audio policy")
	}
	src.MediaStreams[1].Channels = 0
	if !needsStereoConversion(src, "2") {
		t.Fatal("unknown channels bypassed stereo conversion")
	}
}

func TestStereoPolicyRecognizesPlannedOutputChannels(t *testing.T) {
	src := jellyfin.MediaSource{TranscodingURL: "/master.m3u8?TranscodeReasons=AudioChannelsNotSupported", MediaStreams: []jellyfin.MediaStream{{Type: "Audio", Index: 1, Codec: "aac", Channels: 2}}}
	if !needsStereoConversion(src, "1") {
		t.Fatal("planned stereo output mistaken for original stereo source")
	}
	src.VideoCopy = true
	if !mediaSourceSurround(src, &jellyfin.Item{Audio: []jellyfin.Stream{{ID: "1", Channels: 6}}}, "1") {
		t.Fatal("server conversion lost the source surround boost marker")
	}
}
