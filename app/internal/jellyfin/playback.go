package jellyfin

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// Playback: which stream to fetch (PlaybackInfo) and reporting what plays
// (/Sessions/Playing*). The device profile is the playback design's
// business; it is passed through as JSON.

// PlaybackRequest is the body of POST /Items/{id}/PlaybackInfo.
type PlaybackRequest struct {
	DeviceProfile       json.RawMessage // a Jellyfin DeviceProfile, as JSON; nil for the server's default
	MaxStreamingBitrate int             // bits per second, 0 for none
	StartSeconds        float64
	AudioStream         string // a Stream.ID, "" for the default
	SubtitleStream      string // a Stream.ID, "" for the default, "-1" for none
	// AlwaysBurnInSubtitleWhenTranscoding makes Jellyfin render selected
	// subtitles into the video when preparing a transcode.
	AlwaysBurnInSubtitleWhenTranscoding bool
	MediaSourceID                       string // Item.SourceID, "" for the first
}

// MediaSource is one way the server offers to play an item.
type MediaSource struct {
	ID                   string        `json:"Id"`
	MediaStreams         []MediaStream `json:"MediaStreams"`
	Container            string        `json:"Container"`
	Bitrate              int           `json:"Bitrate"`
	RunTimeTicks         int64         `json:"RunTimeTicks"`
	SupportsDirectPlay   bool          `json:"SupportsDirectPlay"`
	SupportsDirectStream bool          `json:"SupportsDirectStream"`
	SupportsTranscoding  bool          `json:"SupportsTranscoding"`
	ETag                 string        `json:"ETag"`
	// TranscodingURL is a server path with its query. Jellyfin puts the
	// session token in it (ApiKey=); never log it. StreamURL strips it.
	TranscodingURL       string `json:"TranscodingUrl"`
	TranscodingContainer string `json:"TranscodingContainer"`
	TranscodingProtocol  string `json:"TranscodingSubProtocol"`
	DefaultAudioStream   *int   `json:"DefaultAudioStreamIndex"`
	DefaultSubtitle      *int   `json:"DefaultSubtitleStreamIndex"`
}

// PlaybackInfo is the server's answer.
type PlaybackInfo struct {
	PlaySessionID string        `json:"PlaySessionId"`
	MediaSources  []MediaSource `json:"MediaSources"`
	ErrorCode     string        `json:"ErrorCode"`
}

// PlaybackInfo asks how to play an item (POST /Items/{id}/PlaybackInfo).
func (c *Client) PlaybackInfo(ctx context.Context, id string, r PlaybackRequest) (*PlaybackInfo, error) {
	body := map[string]any{"UserId": c.UserID(), "AutoOpenLiveStream": false}
	if len(r.DeviceProfile) > 0 {
		body["DeviceProfile"] = r.DeviceProfile
	}
	if r.MaxStreamingBitrate > 0 {
		body["MaxStreamingBitrate"] = r.MaxStreamingBitrate
	}
	if r.StartSeconds > 0 {
		body["StartTimeTicks"] = Ticks(r.StartSeconds)
	}
	if n, ok := parseIndex(r.AudioStream); ok {
		body["AudioStreamIndex"] = n
	}
	if r.SubtitleStream == "-1" {
		body["SubtitleStreamIndex"] = -1
	} else if n, ok := parseIndex(r.SubtitleStream); ok {
		body["SubtitleStreamIndex"] = n
	}
	selectedSubtitle := r.SubtitleStream != "" && r.SubtitleStream != "-1"
	if r.AlwaysBurnInSubtitleWhenTranscoding || selectedSubtitle {
		body["AlwaysBurnInSubtitleWhenTranscoding"] = true
	}
	if selectedSubtitle || r.AlwaysBurnInSubtitleWhenTranscoding {
		// The subtitle renderer needs a video transcode. Ask Jellyfin not to
		// return a direct-play or remux stream for this request.
		body["EnableDirectPlay"] = false
		body["EnableDirectStream"] = false
	}
	if r.MediaSourceID != "" {
		body["MediaSourceId"] = r.MediaSourceID
	}
	var info PlaybackInfo
	if err := c.post(ctx, "/Items/"+url.PathEscape(id)+"/PlaybackInfo", c.userQuery(), body, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// StreamURL is the absolute URL of a transcode, with the token the server
// embedded (ApiKey/api_key) removed: the caller sends AuthHeader instead.
func (c *Client) StreamURL(transcodingURL string) string {
	path, raw, _ := strings.Cut(transcodingURL, "?")
	q, _ := url.ParseQuery(raw)
	for k := range q {
		if strings.EqualFold(k, "ApiKey") || strings.EqualFold(k, "api_key") {
			delete(q, k)
		}
	}
	return c.URL(path, q)
}

// DirectStreamURL is the URL of an item's file as stored (static), for a
// source that SupportsDirectStream; send AuthHeader with it.
func (c *Client) DirectStreamURL(id string, src MediaSource, playSession string) string {
	q := url.Values{"static": {"true"}, "mediaSourceId": {src.ID}, "deviceId": {c.DeviceID}}
	if playSession != "" {
		q.Set("playSessionId", playSession)
	}
	if src.ETag != "" {
		q.Set("tag", src.ETag)
	}
	ext := ""
	if src.Container != "" && !strings.Contains(src.Container, ",") {
		ext = "." + src.Container
	}
	return c.URL("/Videos/"+url.PathEscape(id)+"/stream"+ext, q)
}

// PlayState is a playback report. Position is in seconds; the reports send
// ticks.
type PlayState struct {
	ItemID         string
	MediaSourceID  string
	PlaySessionID  string
	Position       float64
	Paused         bool
	AudioStream    string
	SubtitleStream string
	PlayMethod     string // Transcode, DirectStream, DirectPlay; "" is Transcode
}

func (s PlayState) body(progress bool) map[string]any {
	b := map[string]any{"ItemId": s.ItemID, "PositionTicks": Ticks(s.Position)}
	if s.MediaSourceID != "" {
		b["MediaSourceId"] = s.MediaSourceID
	}
	if s.PlaySessionID != "" {
		b["PlaySessionId"] = s.PlaySessionID
	}
	if progress {
		m := s.PlayMethod
		if m == "" {
			m = "Transcode"
		}
		b["PlayMethod"], b["IsPaused"], b["CanSeek"] = m, s.Paused, true
		if n, ok := parseIndex(s.AudioStream); ok {
			b["AudioStreamIndex"] = n
		}
		if n, ok := parseIndex(s.SubtitleStream); ok {
			b["SubtitleStreamIndex"] = n
		}
	}
	return b
}

// ReportStart says playback began (POST /Sessions/Playing).
func (c *Client) ReportStart(ctx context.Context, s PlayState) error {
	return c.post(ctx, "/Sessions/Playing", nil, s.body(true), nil)
}

// ReportProgress updates the position, about every ten seconds while
// playing and on pause or seek (POST /Sessions/Playing/Progress). The
// server keeps the resume point from these.
func (c *Client) ReportProgress(ctx context.Context, s PlayState) error {
	return c.post(ctx, "/Sessions/Playing/Progress", nil, s.body(true), nil)
}

// ReportStopped ends playback (POST /Sessions/Playing/Stopped); the server
// marks the item watched past its played threshold and stops the transcode.
func (c *Client) ReportStopped(ctx context.Context, s PlayState) error {
	return c.post(ctx, "/Sessions/Playing/Stopped", nil, s.body(false), nil)
}
