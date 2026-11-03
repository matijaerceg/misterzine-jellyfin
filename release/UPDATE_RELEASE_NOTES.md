# MisterZine Jellyfin 0.1.0-beta.12

- Optional supporter chapter navigation: enable **Options > Chapters** to browse a six-thumbnail grid from the playback controls or the movie/episode page. Thumbnails appear when the server provides them. Chapters starts Off, and existing supporter codes cover it.
- Chapter browsing pauses the picture and only moves the selection. **OK** confirms the jump; **Back** cancels. Only one confirmed jump can load at a time, and Back during loading stops playback and returns to the details page.
- Video geometry calibration now gives clearer edge-by-edge instructions and keeps the square, circle and measuring guidance together on the shape step.
- Stopping or switching cores while video is paused now exits the playback worker promptly.
- Episodes now finish when audio continues past the last picture. The playback log also records the end-of-stream stages to help diagnose stuck endings.
- Send a report includes private network/subnet clues and the composite/S-Video setup, while omitting server names, MAC addresses and public addresses from the new network diagnostics.

Reuses the previous release’s qualified FPGA core with updated app and playback worker. Settings, calibration and saved access are preserved. Update through **Options > Updates**, or use the beta installer.

# MisterZine Jellyfin 0.1.0-beta.11

- Composite and S-Video colour: the core now reports its video clock count
  over a whole frame to MiSTer main (words 19/20 of the current framework),
  so main computes the subcarrier exactly on every start instead of from a
  one-line measurement that landed on 80.99 or 81.01 MHz. The yc.txt lines
  added in beta.10 stay as a fallback for MiSTer releases before August 2026.
  The core also picks up the framework's Y/C chroma and NTSC timing fixes,
  registered analog audio pins and a scaler state reset at VSync.
- 720p transcodes from Jellyfin servers that use Intel Quick Sync hardware encoding now show a picture; before, the screen stayed black while the sound played. Intel's encoder codes 720p as 1280x736 with a note to show only 720 lines, and the core silently refused anything larger than 1280x720. It now accepts pictures up to 4,096 blocks of 16x16 pixels, which its frame memory always had room for, so hardware encoding can stay on.
- If the core still decodes a stream but shows none of it, or drops its pictures as too large, the app now notices (within a second for pictures that are too large, a few seconds otherwise) and restarts the item once at the same point as a transcode of at most 854x480, instead of leaving the spinner up over the sound. A file played as stored falls back to an ordinary transcode first.
- Files coded at 1280x736 and similar sizes just over 720 lines can now play as stored, at up to 29.6 frames per second.

Ships a new qualified FPGA core. Settings and saved supporter access are
preserved. Update through **Options > Updates**, or use the beta installer.

# MisterZine Jellyfin 0.1.0-beta.10

- Video crop set to Fill applies when playback starts again. Since the
  decoder-core release it only took effect after changing Crop under More
  during playback; a saved Video geometry calibration hid the fault.
- Theme music fades out over its last seconds instead of stopping dead.
  The clips servers hold are mostly 30 s long and cut hard at the end,
  which sounded like the app cutting the music off.
- Direct Video works again through HDMI-to-VGA adapters, such as a DAC
  feeding a 15 kHz CRT. Since the decoder-core release the core clocked the
  HDMI transmitter at 81 MHz on Direct Video and put a broadcast-style
  composite sync with equalizing pulses on the sync line; adapters dropped
  lock and the screen stayed black. The transmitter now runs at 27 MHz there
  and sends the plain composite sync the other cores use. Confirmed on the
  reporter's adapter with a Plex Core test build of the same core.
- Composite and S-Video colour (MiSTer.ini vga_mode=svideo or cvbs) is now
  pinned: the installer and the app add this core's lines to the SD card's
  yc.txt, as the other cores have, so it applies from the first start. This
  fixes the occasional wrong colours after an OSD Reset or a new start, and
  the dot crawl that came with them.
- The 4:3 filter now ignores season 0 specials when judging a show, and
  re-checks the TV libraries once a day while the menus sit dimmed.

Ships a new qualified FPGA core (the Direct Video fix is in the core) with an
updated playback worker. Settings and saved supporter access are preserved.
Update through **Options > Updates**, or use the beta installer.

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
