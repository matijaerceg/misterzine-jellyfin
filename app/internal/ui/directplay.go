package ui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"plexcrt/internal/jellyfin"
)

// Direct-stream eligibility: whether a source the server would send as stored
// is one the H.264 core can certainly play in real time. The decision uses the
// probed metadata Jellyfin returns with PlaybackInfo, the core's own limits
// and the user's bitrate setting: a source above the chosen cap is transcoded
// to it, as the cap is there for a link that cannot carry more.
//
// Jellyfin's metadata cannot say CAVLC or CABAC, MBAFF or PAFF; jfplay checks
// the SPS/PPS before the first picture (--direct) and the app falls back to a
// transcode when it says no. Values the server does not know are therefore
// not held against a source; only known out-of-range values are.
//
// The thresholds are first estimates and will be tuned on hardware.
const (
	// directMaxKbpsHD is the highest average bitrate accepted for a direct
	// stream wider than directSDWidth (720p class). The core sustains about
	// 8 Mbit/s at 720p; the margin covers peaks above the average.
	directMaxKbpsHD = 6000
	// directMaxKbpsSD is the cap for directSDWidth wide and narrower.
	directMaxKbpsSD = 8000
	// directSDWidth is the widest picture the core decodes at 60 frames/s.
	directSDWidth = 854
	// Match the worker: at most 80 blocks wide and 3,600 16x16 blocks per picture.
	// Cropped dimensions are rounded up; jfplay checks the actual SPS before decoding.
	directMaxWidth, directMaxBlocks = 1280, 3600
	directMaxBlockRate              = 108000
	// directMaxFPSSD / directMaxFPSHD are the frame-rate limits for the two
	// size classes; directFPSSlack absorbs 29.97-style rounding in metadata.
	directMaxFPSSD, directMaxFPSHD, directFPSSlack = 60.0, 30.0, 0.5
	// directMaxLevel is H.264 level 4.1, as Jellyfin reports it (41).
	directMaxLevel = 41
	// directProbeBitrate is the MaxStreamingBitrate sent when asking whether
	// a source can stream as stored: high enough that the server never
	// rejects direct play for bitrate, which this client decides itself.
	directProbeBitrate = 120_000_000
)

// directEligible reports whether src may be streamed untranscoded under a
// video cap in kbit/s (the user's setting; 0 for none), and why not when it
// may not.
func directEligible(src jellyfin.MediaSource, capKbps int) (bool, string) {
	var video *jellyfin.MediaStream
	for i := range src.MediaStreams {
		if strings.EqualFold(src.MediaStreams[i].Type, "Video") {
			video = &src.MediaStreams[i]
			break
		}
	}
	if video == nil {
		// No probe data: trust the server's profile match; jfplay's check
		// and the fallback still protect playback.
		return true, ""
	}
	if video.Codec != "" && !strings.EqualFold(video.Codec, "h264") {
		return false, "codec " + video.Codec
	}
	if p := strings.ToLower(strings.TrimSpace(video.Profile)); p != "" && p != "high" && p != "main" {
		return false, "profile " + video.Profile
	}
	level := video.Level
	if level > 0 && level < 10 { // some servers report 4.1 instead of 41
		level *= 10
	}
	if level > directMaxLevel+0.01 {
		return false, fmt.Sprintf("level %g", video.Level)
	}
	if video.BitDepth != 0 && video.BitDepth != 8 {
		return false, fmt.Sprintf("bit depth %d", video.BitDepth)
	}
	blocks := ((int64(video.Width) + 15) / 16) * ((int64(video.Height) + 15) / 16)
	if video.Width > directMaxWidth || blocks > directMaxBlocks {
		return false, fmt.Sprintf("size %dx%d", video.Width, video.Height)
	}
	hd := video.Width > directSDWidth
	fps := video.RealFrameRate
	if fps <= 0 {
		fps = video.AverageFrameRate
	}
	maxFPS, maxKbps := directMaxFPSSD, directMaxKbpsSD
	if hd {
		maxFPS, maxKbps = directMaxFPSHD, directMaxKbpsHD
	}
	if fps > maxFPS+directFPSSlack {
		return false, fmt.Sprintf("%.3g frames/s at %d wide", fps, video.Width)
	}
	if float64(blocks)*fps > directMaxBlockRate*1.01 {
		return false, fmt.Sprintf("picture block rate %.0f exceeds %d", float64(blocks)*fps, directMaxBlockRate)
	}
	bitrate := video.BitRate
	if bitrate <= 0 {
		bitrate = src.Bitrate // the container's total: a conservative stand-in
	}
	if bitrate > maxKbps*1000 {
		return false, fmt.Sprintf("bitrate %d kbit/s above %d", bitrate/1000, maxKbps)
	}
	if capKbps > 0 && bitrate > capKbps*1000 {
		return false, fmt.Sprintf("bitrate %d kbit/s above the %d chosen", bitrate/1000, capKbps)
	}
	return true, ""
}

// workerNeedsTranscode reports whether a jfplay exit status asks for the same
// item to be played again as a server transcode.
func workerNeedsTranscode(code int) bool {
	return code == workerExitNeedsTranscode || code == workerExitRefused
}

// jfplay exit codes the app acts on (arm/jfplay.md).
const (
	workerExitRefused        = 5 // the core refused the stream
	workerExitNeedsTranscode = 7 // the pre-check or the watchdog said transcode
)

// playbackInfo asks Jellyfin how to play the session's item. Unless a
// transcode is already required (a subtitle to burn in, or force), the first
// request lets the server offer the file as stored, with a bitrate cap no
// direct-eligible file exceeds; the client then applies the core's limits and
// the user's cap (directEligible). A transcode is always requested with the
// user's bitrate setting, so a second request follows when the first answer
// was a transcode at the probe's cap or an ineligible direct stream.
func (s *Session) playbackInfo(ctx context.Context, kbps, offset int, audio, subtitle string, force bool) (*jellyfin.PlaybackInfo, error) {
	p := s.p
	req := jellyfin.PlaybackRequest{StartSeconds: float64(offset), AudioStream: audio, SubtitleStream: subtitle, MediaSourceID: s.item.SourceID, AlwaysBurnInSubtitleWhenTranscoding: subtitle != "-1"}
	key := audio + "|" + subtitle
	s.mu.Lock()
	known := s.probedTranscode == key
	s.mu.Unlock()
	// A restart (a seek) skips the probe when the server already chose a
	// transcode for these tracks: the answer would be the same.
	if subtitle == "-1" && !force && !known {
		probe := req
		profile, err := directProbeProfileJSON()
		if err != nil {
			return nil, err
		}
		probe.DeviceProfile, probe.MaxStreamingBitrate = profile, directProbeBitrate
		info, err := p.Client.PlaybackInfo(ctx, s.item.ID, probe)
		if err != nil {
			return nil, err
		}
		if len(info.MediaSources) == 0 {
			return nil, fmt.Errorf("Jellyfin offered no playable media source")
		}
		src := info.MediaSources[0]
		ok, why := directEligible(src, kbps)
		if ok && src.TranscodingURL != "" && needsStereoConversion(src, audio) {
			// Preserve the client-validated original video. The worker still
			// checks its SPS and can fall back to a bounded video transcode.
			u, err := url.Parse(src.TranscodingURL)
			if err != nil {
				return nil, fmt.Errorf("invalid stereo stream URL")
			}
			q := u.Query()
			for name, value := range map[string]string{"VideoCodec": "copy", "AudioCodec": "aac", "AudioChannels": "2", "MaxAudioChannels": "2", "AllowAudioStreamCopy": "false"} {
				for key := range q {
					if strings.EqualFold(key, name) {
						q.Del(key)
					}
				}
				q.Set(name, value)
			}
			u.RawQuery = q.Encode()
			info.MediaSources[0].TranscodingURL = u.String()
			info.MediaSources[0].VideoCopy = true
			s.logf("audio-only conversion: original video, server stereo AAC")
			return info, nil
		}
		if ok && (src.SupportsDirectPlay || src.SupportsDirectStream) && src.TranscodingURL == "" {
			if needsStereoConversion(src, audio) {
				return nil, fmt.Errorf("Jellyfin did not offer stereo audio conversion; check this user's transcoding permissions")
			}
			if interlacedVideo(src) {
				s.logf("direct stream is interlaced; jfplay checks for MBAFF before playing")
			}
			return info, nil
		}
		if !ok {
			s.logf("direct stream not eligible (%s); requesting a transcode", why)
		}
		s.mu.Lock()
		s.probedTranscode = key
		s.mu.Unlock()
	}
	profile, err := DeviceProfileJSON(kbps)
	if err != nil {
		return nil, err
	}
	req.DeviceProfile, req.MaxStreamingBitrate, req.ForceTranscode = profile, playbackDeviceProfile(kbps).MaxStreamingBitrate, true
	info, err := p.Client.PlaybackInfo(ctx, s.item.ID, req)
	if err != nil {
		return nil, err
	}
	if len(info.MediaSources) == 0 {
		return nil, fmt.Errorf("Jellyfin offered no playable media source")
	}
	return info, nil
}

// Match the chosen track, including the server's default when none was named.
func needsStereoConversion(src jellyfin.MediaSource, audio string) bool {
	// PlaybackInfo can replace the selected stream's channel count with the
	// planned output count. Its conversion reason still describes the source.
	if u, err := url.Parse(src.TranscodingURL); err == nil {
		for key, values := range u.Query() {
			if strings.EqualFold(key, "TranscodeReasons") {
				for _, value := range values {
					for _, reason := range strings.Split(value, ",") {
						if reason == "AudioChannelsNotSupported" || reason == "AudioCodecNotSupported" {
							return true
						}
					}
				}
			}
		}
	}
	index, err := strconv.Atoi(audio)
	if err != nil && src.DefaultAudioStream != nil {
		index, err = *src.DefaultAudioStream, nil
	}
	for _, st := range src.MediaStreams {
		if !strings.EqualFold(st.Type, "Audio") || (err == nil && st.Index != index) {
			continue
		}
		if st.Channels < 1 || st.Channels > 2 {
			return true
		}
		switch strings.ToLower(st.Codec) {
		case "aac", "ac3", "eac3", "dts", "dca", "mp3", "flac", "opus", "vorbis", "pcm":
			return false
		default:
			return true
		}
	}
	return false
}

func interlacedVideo(src jellyfin.MediaSource) bool {
	for _, st := range src.MediaStreams {
		if strings.EqualFold(st.Type, "Video") {
			return st.IsInterlaced
		}
	}
	return false
}

// logf writes an app line into the playback log, beside jfplay's output.
func (s *Session) logf(format string, args ...any) {
	if s.lf != nil {
		_, _ = fmt.Fprintf(s.lf, "app: "+format+"\n", args...)
	}
}

// fallbackLocked replaces a direct stream whose worker asked for a transcode
// (the pre-check failed, the core refused it, or it could not keep up) with a
// server transcode at the same position. It runs at most once a session, so
// a transcode that fails in turn ends playback with its error. It reports
// whether it took over the worker's exit; opMu is held.
func (s *Session) fallbackLocked(err error) bool {
	code := -1
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	if !workerNeedsTranscode(code) {
		return false
	}
	s.mu.Lock()
	if (!s.direct && !s.videoCopy) || s.fellBack || s.closing || s.stopRequested {
		s.mu.Unlock()
		return false
	}
	s.fellBack = true
	s.mu.Unlock()
	s.updateLocked(false) // the worker's final position
	s.mu.Lock()
	ps, started := s.state, s.started
	s.started = false
	s.forceTranscode = true
	s.cmd, s.cmdDone = nil, nil
	s.mu.Unlock()
	s.logf("direct stream needs a transcode (jfplay exit %d); restarting at %.3f s", code, ps.Position)
	if started && ps.PlaySessionID != "" {
		// end the direct play session; the transcode reports its own start
		if s.async {
			s.p.reportAsync(func() { _ = s.p.Client.ReportStopped(context.Background(), ps) }, false)
		} else {
			_ = s.p.Client.ReportStopped(context.Background(), ps)
		}
	}
	_ = os.Remove(s.p.Status)
	// startContext takes whole seconds: round to nearest rather than truncate
	if err := s.startContext(int(math.Round(ps.Position)), false, s.sessionContext()); err != nil {
		s.finishLocked(s.suppressStopCancellation(err))
	}
	return true
}
