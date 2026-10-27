# Playback

The Go app handles the Jellyfin session and the on-screen controls. It starts
`jfplay`, a small playback worker that sends video and audio to the MiSTer
H.264 core. The file is streamed untranscoded when the core can certainly
play it, and Jellyfin transcodes it to HLS otherwise.

## Starting playback

When playback starts, the app requests Jellyfin `PlaybackInfo` with its device
profile, the item's selected audio and subtitle tracks, the known media source,
and the resume position. Unless a subtitle must be burned in, the first request lets
the server offer the file as stored without applying the configured bitrate
limit. The app then checks the returned video details against the core's own
limits: H.264 High or Main, level 4.1 or lower, 8-bit, at most 1280 pixels
wide and 3,600 rounded-up 16x16 blocks per picture, at
most 30 frames per second above 854 pixels wide (60 at or below), and an
average bitrate of at most 6 Mbit/s above 854 pixels wide (8 Mbit/s at or
below). It also checks the video bitrate against the **Video bitrate**
setting: the setting is there for a link that cannot carry more, so a file
whose video is above it is never streamed as stored. A file that fits both is
streamed directly. Otherwise the app makes a second PlaybackInfo request for a
transcode with the configured bitrate limit (two requests for a transcoded
item, one for a direct one).

The device profile advertises supported audio codecs and at most two audio
channels. Surround, unknown channel counts and unsupported audio codecs need
server conversion to stereo AAC. If video passes the checks above and no
subtitle needs burning in, the app requests video copy in the offered HLS
stream while requiring audio conversion. The worker still checks the original
video's parameter sets and can fall back once to a bounded video transcode.

With the **Direct play** setting off, none of this applies: every start asks
the server for a transcode outright, within the Video bitrate.
The audio conversion is triggered by capabilities, not a low bitrate target.
The picture block budget permits shapes such as 968x722, but not 1280x722 or
1920x1080; the block-rate limit is 108,000 per second with 1% rounding slack.

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

The app downloads media through a checked local gateway and gives `jfplay` a
temporary loopback URL. The decoder receives no token; remote certificate validation,
redirect rules and local HTTP consent apply to direct streams and HLS alike.
The decoder rejects URLs outside that gateway session. It hides the app overlay while
the video is playing. The worker status file supplies playback position and
state; the app reports start, periodic progress, and stop to Jellyfin.

For HTTPS media, the gateway uses the worker's OpenSSL in a separate, private
stdio connection. This avoids expensive generic encryption on ARMv7. It checks
the certificate chain, expiry and DNS/IP subject alternative name before sending
HTTP bytes, using the system trust store and TLS 1.2 or later. The app still owns
HTTP redirects and playlist validation; this helper never touches the decoder.

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

The playback **More > Crop** row steps with Left and Right through **Off**,
**14:9**, and **Fill** for
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
output it softens thin horizontal lines in frame-based video and in the app's
screens so they flicker less between fields. It is On by default. Video recorded
as interlaced fields is not filtered. The app does not pass `jfplay` an
Anti-Twitter request, so the OSD choice stays in force during playback.

## Audio and subtitles

The device profile offers H.264 High or Main, 8-bit 4:2:0 video up to 1280x720.
The core supports up to 60 frames per second at 854 pixels wide or less, and up
to 30 frames per second above that width. The profile caps the configured video
bitrate at 8 Mbit/s for transcodes. The bitrate steps below 3 Mbit/s also make
the transcode smaller, so a low bitrate is spent on fewer pixels: at most
854x480 at 1 and 2 Mbit/s, and 640x360 at 0.4 Mbit/s. Surround audio is converted
to stereo by the server. Supported mono/stereo audio is decoded by `jfplay` to
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

Text subtitle tracks (SRT, ASS/SSA, WebVTT and other formats Jellyfin reports as
text it can deliver separately) are drawn by the app. It fetches the whole track
as SRT from `/Videos/{id}/{source}/Subtitles/{index}/Stream.srt`, keeps the cue
text only (styling, positioning and ASS effects are dropped) and draws the cues as
white type on a translucent box, centred along the bottom of the safe area. The
stream is requested without subtitles, so it can still play as stored, and
choosing another text track or Off changes the overlay without restarting the
stream. Cue times follow the worker's reported position and run on from its
last report while playing. The subtitles show while nothing else is on the
overlay: the controls, a list, the seek strip, the skip button and the wait dots
replace them while they are up. They are drawn over the picture, so cropping
does not cut them.

**Subtitle size** in Options and playback More offers Normal, Large (150%) and
Extra large (200%) for the drawn subtitles. Normal is 22-pixel type on the
480-line overlay; long lines wrap within the safe area, and at most six lines
show at once. A change applies from the next subtitle painted, without a
restart.

Picture subtitles (PGS, VobSub, DVB) are burned into the video by Jellyfin. The
app requests a video transcode with the chosen subtitle index; this requires
server-side video transcoding permission. The overlay fonts cover ASCII; Latin
letters with accents (Latin-1 and Latin Extended-A) are drawn as their base
letters. A text track in which more than one letter in twenty falls outside them (Cyrillic, Greek, Arabic, CJK and
other scripts) is handed back to Jellyfin to burn in as well, with one restart,
and is burned in from the start for the rest of the app's run. Jellyfin offers
no per-request subtitle size or style for burn-in, and its burned subtitles
scale with the picture, so the size setting does not change them, and cropping
can cut them. Changing to or from a burned-in track restarts the stream; track
changes preserve the position and pause state.

## Worker interface

`jfplay` accepts a direct stream or HLS URL, request headers, an optional base
time and in-file seek, volume, audio stream index, control FIFO, status file,
analog aspect, and (on capable core versions) saved video geometry. The app
passes saved geometry only when the running core reports interface 1.2 or
later. Its full command line and status/control contract are
documented in [`arm/jfplay.md`](../arm/jfplay.md). Seeks go to the running
worker as `seek` commands; when it cannot seek in place, the app restarts it
with an in-file seek for direct streams and full-timeline HLS playlists.
