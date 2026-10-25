package ui

import (
	"encoding/json"
	"strconv"
	"testing"
)

// The profile Jellyfin is sent: one H.264 profile with the core's limits and
// the user's video cap, a 30 frames/s rule above 854 wide, an HLS transcode,
// selected subtitles encoded into the video (which forces a transcode).
func TestDeviceProfileJSON(t *testing.T) {
	b, err := DeviceProfileJSON(4000)
	if err != nil {
		t.Fatal(err)
	}
	var got deviceProfile
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.MaxStreamingBitrate != 6_000_000 {
		t.Fatalf("container limit = %d", got.MaxStreamingBitrate)
	}
	if len(got.DirectPlayProfiles) == 0 || got.DirectPlayProfiles[0].VideoCodec != "h264" {
		t.Fatal("missing H.264 direct play")
	}
	if len(got.TranscodingProfiles) != 1 || got.TranscodingProfiles[0].Protocol != "hls" || !got.TranscodingProfiles[0].BreakOnNonKeyFrames {
		t.Fatal("invalid transcode profile")
	}
	if len(got.SubtitleProfiles) == 0 {
		t.Fatal("subtitle formats must be declared")
	}
	for _, sub := range got.SubtitleProfiles {
		if sub.Method != "Encode" {
			t.Fatalf("subtitle %q method = %q, want Encode", sub.Format, sub.Method)
		}
	}
	if len(got.CodecProfiles) != 3 {
		t.Fatalf("codec profiles = %d", len(got.CodecProfiles))
	}
	audio := got.CodecProfiles[2]
	if audio.Type != "VideoAudio" || len(audio.Conditions) != 1 || audio.Conditions[0].Property != "AudioChannels" || audio.Conditions[0].Value != "2" || !audio.Conditions[0].IsRequired {
		t.Fatalf("missing stereo constraint: %+v", audio)
	}
	want := map[string]string{"Width": "1280", "Height": "720", "VideoFramerate": "60", "VideoBitrate": "4000000", "VideoBitDepth": "8"}
	if len(got.CodecProfiles[0].ApplyConditions) != 0 {
		t.Fatal("the base profile must always apply")
	}
	for _, c := range got.CodecProfiles[0].Conditions {
		if want[c.Property] == c.Value {
			delete(want, c.Property)
		}
	}
	if len(want) != 0 {
		t.Fatalf("base profile missing conditions: %v", want)
	}
	hd := got.CodecProfiles[1]
	if len(hd.ApplyConditions) != 1 || hd.ApplyConditions[0].Property != "Width" || len(hd.Conditions) != 1 || hd.Conditions[0].Value != "30" {
		t.Fatalf("HD rule = %+v", hd)
	}
	// no cap, or one past the core's limit, is the core's limit
	for _, kbps := range []int{0, 20000} {
		if p := playbackDeviceProfile(kbps); p.MaxStreamingBitrate != MaxVideoKbps*1000+containerHeadroom {
			t.Fatalf("cap %d: container limit %d", kbps, p.MaxStreamingBitrate)
		}
	}
}

// A low cap transcodes to a smaller picture: 480p below 3 Mbit/s, 360p below
// 1; from 3 Mbit/s up, and with no cap, the core's 720p.
func TestDeviceProfileSizeFollowsCap(t *testing.T) {
	for _, tc := range []struct{ kbps, w, h int }{
		{400, 640, 360}, {999, 640, 360}, {1000, 854, 480}, {2000, 854, 480},
		{3000, 1280, 720}, {8000, 1280, 720}, {0, 1280, 720},
	} {
		got := map[string]string{}
		for _, c := range playbackDeviceProfile(tc.kbps).CodecProfiles[0].Conditions {
			got[c.Property] = c.Value
		}
		if got["Width"] != strconv.Itoa(tc.w) || got["Height"] != strconv.Itoa(tc.h) {
			t.Errorf("cap %d: %sx%s, want %dx%d", tc.kbps, got["Width"], got["Height"], tc.w, tc.h)
		}
	}
	// the lowest step offered gets the smallest picture
	if w, _ := transcodeSize(Bitrates[0].Kbps); w != 640 {
		t.Fatalf("lowest step %d kbit/s is %d wide", Bitrates[0].Kbps, w)
	}
}

func TestDirectProbeLeavesHeightToBlockCheck(t *testing.T) {
	b, err := directProbeProfileJSON()
	if err != nil {
		t.Fatal(err)
	}
	var p deviceProfile
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	for _, profile := range p.CodecProfiles {
		for _, c := range profile.Conditions {
			if c.Property == "Height" {
				t.Fatal("probe imposes a rectangular height cap")
			}
		}
	}
	// Offering a tall source must not loosen the eventual transcode target.
	found := false
	for _, c := range playbackDeviceProfile(8000).CodecProfiles[0].Conditions {
		if c.Property == "Height" && c.Value == "720" {
			found = true
		}
	}
	if !found {
		t.Fatal("transcode height cap changed")
	}
}
