package ui

import (
	"encoding/json"
	"strconv"
)

// The Jellyfin device profile for the H.264 decoder core. The FPGA decodes
// H.264 High or Main profile, 8-bit 4:2:0, up to 1280x720 at 30 frames/s or
// 854x480 at 60, and falls behind above about 8 Mbit/s at 720p; jfplay
// decodes the audio and downmixes it to stereo. Everything else is an HLS
// transcode to the same limits. Selected subtitles use Jellyfin's Encode
// delivery method, which burns them into the transcoded video.
type deviceProfile struct {
	Name                string               `json:"Name"`
	MaxStreamingBitrate int                  `json:"MaxStreamingBitrate"`
	DirectPlayProfiles  []directPlayProfile  `json:"DirectPlayProfiles"`
	TranscodingProfiles []transcodingProfile `json:"TranscodingProfiles"`
	CodecProfiles       []codecProfile       `json:"CodecProfiles"`
	SubtitleProfiles    []subtitleProfile    `json:"SubtitleProfiles"`
}
type directPlayProfile struct {
	Container  string `json:"Container"`
	Type       string `json:"Type"`
	VideoCodec string `json:"VideoCodec"`
	AudioCodec string `json:"AudioCodec"`
}
type transcodingProfile struct {
	Container           string `json:"Container"`
	Type                string `json:"Type"`
	Protocol            string `json:"Protocol"`
	VideoCodec          string `json:"VideoCodec"`
	AudioCodec          string `json:"AudioCodec"`
	MaxAudioChannels    string `json:"MaxAudioChannels"`
	BreakOnNonKeyFrames bool   `json:"BreakOnNonKeyFrames"`
}

// codecProfile's Conditions apply to a stream (and cap a transcode) when every
// ApplyCondition holds; with none they always apply.
type codecProfile struct {
	Type            string           `json:"Type"`
	Codec           string           `json:"Codec"`
	ApplyConditions []codecCondition `json:"ApplyConditions,omitempty"`
	Conditions      []codecCondition `json:"Conditions"`
}
type codecCondition struct {
	Condition  string `json:"Condition"`
	Property   string `json:"Property"`
	Value      string `json:"Value"`
	IsRequired bool   `json:"IsRequired"`
}
type subtitleProfile struct {
	Format string `json:"Format"`
	Method string `json:"Method"`
}

// MaxVideoKbps is the most the core decodes comfortably at 720p.
const MaxVideoKbps = 8000

// containerHeadroom is added to the video cap for the container limit, so a
// file whose video fits is not transcoded for its audio and overhead.
const containerHeadroom = 2_000_000

// transcodeSize is the largest picture a transcode at a video cap in kbit/s
// is made at. The output is at most 576 lines, so a low cap spends its bits
// on fewer pixels rather than on a blocky 720p: 480p below 3 Mbit/s, 360p
// below 1 Mbit/s. At 854 wide or less the core keeps 60 frames/s.
func transcodeSize(videoKbps int) (w, h int) {
	switch {
	case videoKbps < 1000:
		return 640, 360
	case videoKbps < 3000:
		return 854, 480
	}
	return 1280, 720
}

// playbackDeviceProfile is the profile for a video cap in kbit/s (the
// user's bitrate setting, never above MaxVideoKbps).
func playbackDeviceProfile(videoKbps int) deviceProfile {
	if videoKbps <= 0 || videoKbps > MaxVideoKbps {
		videoKbps = MaxVideoKbps
	}
	video := videoKbps * 1000
	w, h := transcodeSize(videoKbps)
	base := []codecCondition{
		{"EqualsAny", "VideoProfile", "high|main", true},
		{"LessThanEqual", "VideoLevel", "41", false},
		{"LessThanEqual", "Width", strconv.Itoa(w), true},
		{"LessThanEqual", "Height", strconv.Itoa(h), true},
		{"Equals", "VideoBitDepth", "8", true},
		{"LessThanEqual", "VideoFramerate", "60", true},
		{"LessThanEqual", "VideoBitrate", strconv.Itoa(video), true},
	}
	// above 854 wide the core manages 30 frames/s, not 60
	hd := codecProfile{Type: "Video", Codec: "h264",
		ApplyConditions: []codecCondition{{"GreaterThanEqual", "Width", "855", true}},
		Conditions:      []codecCondition{{"LessThanEqual", "VideoFramerate", "30", true}}}
	var subs []subtitleProfile
	for _, f := range []string{"srt", "subrip", "ass", "ssa", "vtt", "webvtt", "ttml", "smi", "sub", "idx", "dvdsub", "dvbsub", "vobsub", "pgs", "pgssub"} {
		subs = append(subs, subtitleProfile{Format: f, Method: "Encode"})
	}
	return deviceProfile{
		Name:                "MiSTer H.264",
		MaxStreamingBitrate: video + containerHeadroom,
		DirectPlayProfiles: []directPlayProfile{{"mkv,mp4,m4v,ts,mpegts", "Video", "h264",
			"aac,ac3,eac3,dts,mp3,flac,opus,vorbis,pcm"}},
		TranscodingProfiles: []transcodingProfile{{"ts", "Video", "hls", "h264", "aac", "2", true}},
		CodecProfiles:       []codecProfile{{Type: "Video", Codec: "h264", Conditions: base}, hd},
		SubtitleProfiles:    subs,
	}
}

// DeviceProfileJSON is the profile for PlaybackInfo, as JSON.
func DeviceProfileJSON(videoKbps int) (json.RawMessage, error) {
	b, err := json.Marshal(playbackDeviceProfile(videoKbps))
	return json.RawMessage(b), err
}
