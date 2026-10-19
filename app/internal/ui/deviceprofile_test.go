package ui

import (
	"encoding/json"
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
	if len(got.CodecProfiles) != 2 {
		t.Fatalf("codec profiles = %d", len(got.CodecProfiles))
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
