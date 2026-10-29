# MisterZine Jellyfin 0.1.0-beta.9

- A resume, a seek or Skip intro that the server never answers is retried
  instead of failing after a minute: a converted stream started part-way
  through is given 20 seconds to arrive, then asked for again, then played
  from the beginning, with a line on the playback panel. The playback log
  names each attempt for Send a report. This targets servers whose
  hardware-accelerated conversion stalls on a seek; playing from the start
  was unaffected.
- Failure reports now show which playback startup stage stalled and how long
  it took. The most recent failed playback log survives playing another video
  before sending a report.

- Movies and episodes stored as several files now ask which version to play
  when you start from the beginning. The version best suited to the MiSTer is
  selected: the largest that plays directly, otherwise the smallest of at
  least 720p. Jellyfin 12 and later keep each version's own resume point, and
  Resume plays the version in progress without asking. With older servers the
  app remembers the version last played on this MiSTer and asks when it
  cannot tell.
- The next episode, the L/R episode hold and Shuffle never ask: they play the
  version the show last played, or the one best suited to the MiSTer.
- A Version row beside Audio and Subtitles changes the version for the next
  play, Playback Info names the version playing, and Mark watched marks every
  version.
- A saved audio or subtitle choice carries over to another file only when the
  track's language matches.

Uses the same qualified FPGA core as beta 7, with an updated playback worker. Settings and
saved supporter access are preserved. Update through **Options > Updates**, or
use the beta installer.

# MisterZine Jellyfin 0.1.0-beta.7

- Direct play can be switched off in Options or the playback More menu to
  ask the server to convert every video. Changing it during playback keeps
  your position.
- Hold L or R for two seconds during an episode to play the previous or next
  episode. A progress indicator shows the hold; releasing early cancels it.
- HTTPS is preferred for server discovery and sign-in. Plain HTTP is limited to
  local destinations and requires a clear warning and approval for that server.
  Existing unapproved HTTP sessions return to sign-in after updating.
- Server identity is checked without credentials before a saved session is used.
  Unsafe redirects are refused.
- Video downloads now use the app's certificate and connection checks. The
  decoder receives a temporary local URL rather than the Jellyfin token, and
  external playlist destinations are refused. Update the app and playback worker
  together. HTTPS servers need valid certificates and the MiSTer's clock set.
- Verified HTTPS media uses the worker's optimized OpenSSL connection so the
  security checks do not burden ARM playback with generic software encryption.
  DE10 checks covered 720p H.264 direct play and HLS transcoding at 23.976 fps,
  480i output, pause/resume, repeated seeks, Direct play changes and episode holds.

Known beta limitation: seeking and playback mode changes can land several seconds
from the requested position at keyframes or stream segment boundaries.

Uses the same qualified FPGA core as beta 6. Settings and saved supporter access
are preserved. Update through **Options > Updates**, or use the beta installer.

# MisterZine Jellyfin 0.1.0-beta.6

- Playback Info fits in one wider, compact panel with no row selection.
- Video and audio separately explain whether the server converted them.
  Original and received formats are compared when the source details are known.
- Plain-language conversion reasons explain format, channel, subtitle and
  playback-limit requirements. Local stereo output is shown separately.
- Measured current and average video bitrate, the applicable bitrate limit,
  MiSTer output and effective de-flicker state are visible together.
- Recent late frames and decoder errors are separated from playback totals;
  the sound buffer is shown in seconds.
- Arrow keys leave Playback Info open. Left/Right on its More-menu row has no
  effect; OK and Back return from Info to More.
- Original video whose file carries wrong or missing frame timing in its H.264
  header no longer judders. The player corrects the timing to the file's real
  frame rate before the core schedules pictures; a steady late-frame count on
  an otherwise smooth title was this.
- Send a report now uploads the report and shows a short code to post where
  you asked for help, as the Plex app does. A copy still stays on the card.
- Reports include the H.264 stream headers as received, the demuxer's frame
  rates and the core's output changes, for diagnosing wrong output rates.

Uses the same qualified FPGA core as the preceding release. Existing settings,
calibration and saved access are preserved. Rollback restores the previous release.
