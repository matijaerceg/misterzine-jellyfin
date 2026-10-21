# Playback

The Go app handles the Jellyfin session and the on-screen controls. It starts
`jfplay`, a small playback worker that sends video and audio to the MiSTer
H.264 core. The file is streamed untranscoded when the core can certainly
play it, and Jellyfin transcodes it to HLS otherwise.

## Starting playback

When playback starts, the app requests Jellyfin `PlaybackInfo` with its device
profile, the item's selected audio and subtitle tracks, the known media source,
and the resume position. Unless a subtitle is selected, the first request lets
the server offer the file as stored without applying the configured bitrate
limit. The app then checks the returned video details against the core's own
limits: H.264 High or Main, level 4.1 or lower, 8-bit, at most 1280x720, at
most 30 frames per second above 854 pixels wide (60 at or below), and an
average bitrate of at most 6 Mbit/s above 854 pixels wide (8 Mbit/s at or
below). A file that fits is streamed directly whatever the bitrate setting.
Otherwise the app makes a second PlaybackInfo request for a transcode with the
configured bitrate limit (two requests for a transcoded item, one for a direct
one), as before; the bitrate setting only applies to transcodes.

Jellyfin cannot report every property the core depends on (CABAC versus
CAVLC entropy coding, MBAFF versus field-coded interlace). MBAFF is refused and
field-coded interlace is accepted, but Jellyfin's own `IsInterlaced` flag cannot
tell the two apart, so the app only logs it and does not decide on it. `jfplay` checks
a direct stream's parameter sets before the first picture is shown. If the
check fails, the core refuses the stream, or the core keeps falling behind for
several seconds, the app restarts the item once as a transcode at the current
position. The switch is recorded in the playback log and is not shown as an
error; the direct play session is reported stopped and the transcode is
reported as a new session. The playback position is carried over to the
transcode.

It uses the first media source returned:

- For direct play or direct stream, `jfplay` reads the item's stream and seeks
  within it to the requested position.
- For an HLS transcode, the app seeks within Jellyfin's full-movie playlist.
  A requested resume position does not mean the returned playlist starts there.
  The worker uses the movie's timeline without adding the resume offset again.

The app passes its authorization header to `jfplay` for stream requests and
redacts the account token from the playback log. It hides the app overlay while
the video is playing. The worker status file supplies playback position and
state; the app reports start, periodic progress, and stop to Jellyfin.

## Controls and seek

Pause and resume are sent to `jfplay` through its control FIFO. For direct streams
and full-timeline HLS, a capable worker can seek without reopening the player.
The app waits for a matching seek acknowledgement while input and rendering
continue. Older workers, offset transcodes, and failed or timed-out seeks use
the process-restart fallback: stop and reap the worker, then start at the target.
A direct restart reuses its URL; a transcode restart requests fresh PlaybackInfo.
Container start timestamps are included when seeking. Seeks can still interrupt
the picture while the decoder starts from a keyframe.

The compressed forward queue is bounded by 64 MiB, 8,192 packets, and a
45-second read-ahead horizon when timestamps are known. A nearby forward seek
can reuse queued packets when both streams cover a confirmed H.264 IDR within
one second of the target, before or after it. The closest eligible IDR is used,
and must advance beyond the current position. Other seeks use the demuxer; backward history
is not retained. Network speed, server transcoding speed, and keyframe spacing
determine whether a fast-path hit is possible. Neither path promises frame-exact
seeking or instant startup.

The playback progress bar shows verified buffered audio/video coverage in pale
purple, with played video in the normal purple. It does not infer a video range
from the audio-only queue, and clears unavailable coverage during seeking.
Buffered coverage indicates downloaded media, not a guarantee that every point
within it has a suitable keyframe for a fast seek.
Seeking or changing audio while paused keeps the replacement paused. Resume
continues playback; it does not automatically start because a stream restarted.

On initial playback and Resume, the loading UI covers the retained previous
picture with opaque black until a new picture is shown. The session handshake
keeps that cover in place until the core acknowledges its display reset.

Stopping also asks the worker to exit and reports the final position to
Jellyfin. If the stream ends normally, the app treats it as completed and can
continue to the next episode. Worker errors are shown as a short playback
notice.

The playback **More > Crop** setting offers **Off**, **14:9**, and **Fill** for
applicable wide-screen content. Off asks the core to letterbox; 14:9 zooms
wide pictures toward a 14:9 window, trimming less of the sides than Fill and
leaving small bars; pictures at or narrower than 14:9 stay whole. Fill crops
the picture to fill the analog display. The choice is sent when a worker starts and can be
changed live while playback continues. The app keeps the selected value for
the current playback. 14:9 uses custom geometry on cores with HPS interface
1.2 or later; older cores safely show the full picture as Off does.

**Options > Video calibration** saves the screen edges and width correction for
the current display. Playback uses those values only when the running H.264
core advertises the additive 1.2 geometry interface, as the core shipped with
the app does. On older cores the screen is labeled **Video calibration
(preview only)** and playback remains unchanged; the saved values are
retained. Geometry is separate from the crop setting and is limited to fitted
480- or 576-line output; Native HDMI output is not supported.

De-flicker is an option of the core's OSD, not of the app: on interlaced
output it softens thin horizontal detail in frame-based video and in the app's
screens so it flickers less between fields. It is On by default. Video recorded
as interlaced fields is not filtered. The app does not pass `jfplay` an
Anti-Twitter request, so the OSD choice stays in force during playback.

## Audio and subtitles

The device profile offers H.264 High or Main, 8-bit 4:2:0 video up to 1280x720.
The core supports up to 60 frames per second at 854 pixels wide or less, and up
to 30 frames per second above that width. The profile caps the configured video
bitrate at 8 Mbit/s for transcodes. Supported audio is decoded by `jfplay` and downmixed to
stereo PCM for the core. Other video is requested as an HLS H.264/AAC
transcode.

Audio changes during playback restart at the current position with explicit
track indices. Direct streams pass the selected source audio index to the worker.
**Surround downmix boost** is available in Options and playback More, with Off,
Small (175%), and Large (300%) settings. The worker boosts only a known surround
source, including a source Jellyfin has already downmixed to stereo, and soft-limits
peaks before output. Stereo sources are unchanged. Changing boost during playback
does not restart the stream; audio already queued may retain its previous gain
briefly. The separate worker volume control only attenuates sound.

Selected subtitle tracks are burned into the video by Jellyfin. The app requests
a video transcode with the chosen subtitle index; this requires server-side video
transcoding permission and support for the subtitle format. Selecting Off removes
subtitles on the replacement stream. Track changes preserve the position and
pause state. Cropping can cut off burned-in text. There is no subtitle size
setting: the server burns subtitles in at its own size.

## Worker interface

`jfplay` accepts a direct stream or HLS URL, request headers, an optional base
time and in-file seek, volume, audio stream index, control FIFO, status file,
analog aspect, and (on capable core versions) saved video geometry. The app
passes saved geometry only when the running core reports interface 1.2 or
later. Its full command line and status/control contract are
documented in [`arm/jfplay.md`](../arm/jfplay.md). Seeks go to the running
worker as `seek` commands; when it cannot seek in place, the app restarts it
with an in-file seek for direct streams and full-timeline HLS playlists.
