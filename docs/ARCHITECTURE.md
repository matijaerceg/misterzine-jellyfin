# Architecture

MisterZine Jellyfin is a Jellyfin client for the MiSTer FPGA. It forks the
MisterZine Plex app but does **not** ship its own FPGA core: video decoding and
output come from the third-party H.264 decoder core vendored at
[`third_party/MiSTer_h264_decoder`](../third_party/MiSTer_h264_decoder)
(GPL-3.0-or-later core, MIT client library). Its integration manual is under
`third_party/MiSTer_h264_decoder/site/content/integrating/` (overview, overlay,
launch) and the C interface is `hps/include/h264ring.h`.

## Target layout

```
 Jellyfin server ──HTTP──► Go app (plexcrt) ──draws 720x480 frames──► /dev/fb0 overlay page ─┐
                              ▲                                                               │
                              │ input region (pads, keys, OSD open)                           ▼
                              └──────────────────────────── H.264 core (FPGA) ◄── ring ── C player
                                                             composites overlay        (libh264ring)
                                                             over decoded video
```

- **The core** (`_Other/H264`) decodes H.264 access units and plays 48 kHz PCM
  against one clock, outputs 480i/576i/480p/576p or native HDMI, and composites
  a page of the Linux framebuffer (`/dev/fb0`) over the video. With nothing
  playing it shows that overlay over black, so the browse UI needs no video.
  It is loaded through an MGL and `/dev/MiSTer_cmd` (`h264ring_launch_core`).
- **The Go app** (`app/`) keeps the Plex app's UI, rendering, input handling,
  art cache, updater and installer. It is retargeted so its 720x480 frames go
  into an overlay page in `/dev/fb0` described to the core
  (`h264ring_overlay_show`), sized to the core's raster (word 80), and its
  input comes from the core's input region (MiSTer main holds the real input
  devices while a core runs). It talks to Jellyfin over its REST API.
- **The C player** (`arm/jfplay.c`) replaces the Plex project's `plexplay.py`,
  FFmpeg pipeline and `plexfb` presenter. It reads the direct stream or transcode URL supplied by the Go app,
  demuxes H.264 video, decodes audio to stereo PCM, and writes both through
  `libh264ring`. The Go app requests streams and reports progress to Jellyfin.
- **Installer and updater** (`release/manager.py`, `update_service.py`,
  `catalogue.py`, `build_package.py`) keep the versioned-release, hash-checked,
  rollback-capable layout of the Plex app. Payload names still describe the
  Plex build; adapting packaging to the Jellyfin worker is still pending.

Release channels (`public`, `beta`) remain; the channel is set at link time
with `-X plexcrt/internal/updates.Channel=...`. There is no access gating:
every build plays.

## What is still Plex

These parts still carry Plex behaviour and have to be rewritten or retargeted:

- Done: `app/internal/plex` is replaced by `app/internal/jellyfin` (header
  token auth, Quick Connect, views, items, resume, search, images, PlaybackInfo,
  play reports). `ui/login.go` is the new sign-in flow (server address on an
  on-screen keyboard, then Quick Connect or user name and password);
  `ui/connect.go` checks one server; `ui/config.go` holds the server, user,
  token and the chosen audio/subtitle tracks; Plex Home user switching is gone.
  An unauthorized answer drops the app back to sign-in with the address kept.
- Done: `app/internal/ring` and `app/internal/input` speak the H.264 core (next
  section).
- Done: the playback launcher (`ui/player.go`, `docs/PLAYBACK.md`) asks
  PlaybackInfo with the core's device profile (`ui/deviceprofile.go`), runs
  `jfplay` on the direct stream or the HLS transcode, reports progress, seeks
  by restarting the player, and sets the core's Analog Aspect for the crop
  choice. Subtitles are not rendered yet.
- `ui/config.go` still carries Plex-era comments and values around bitrate,
  audio boost and subtitle size; `seasonsOf` in `ui/app.go` still skips Plex's
  `/allLeaves` seasons, which Jellyfin never returns.
- The Collections tab lists every box set on the server in every film or show
  library, because Jellyfin's box sets do not belong to a library.
- The installer payload list (`release/manager.py` `PAYLOAD`,
  `release/build_package.py`) still names `plexplay.py`, `plexfb` and the Plex
  core `.rbf`.
- Branding strings (MisterZine Plex Core) throughout, unchanged for now.

## The ring and input packages on the H.264 core (as built, 2026-10-15)

`app/internal/ring` and `app/internal/input` now speak the H.264 core's
interface 1.x (`third_party/MiSTer_h264_decoder/hps/include/h264ring_layout.h`),
reimplemented in Go without cgo. Their exported API is the old one, so the UI
builds unchanged.

- **Memory.** The control block (`0x3140_0000`, 4 KB of 64-bit words) is mapped
  through `/dev/mem` `O_RDWR|O_SYNC` after checking `/proc/iomem` (the library's
  rule); every access is one aligned 32-bit atomic load or store (on ARMv7 Go's
  `atomic.Store` is `DMB; STR; DMB`, `atomic.Load` is `LDR; DMB`, never
  LDREX/STREX). The 8 MB ring is not mapped: only the player writes it.
  `/dev/fb0` is mapped whole (its `smem_start` from `FBIOGET_FSCREENINFO` must lie
  in `0x2200_0000`+8 MB); two 720x480 ARGB8888 pages sit at its start
  (1,382,400 bytes each, stride 2,880). A framebuffer mode that maps less than
  two pages (a 480p HDMI mode) is set to 1920x1080 after the core is up, and
  Watch puts it back when MiSTer main shrinks it.
- **Frames.** `Begin` hands out an ordinary-memory canvas; `End`/`Present` wait
  until the core's `ovl_gen` (word 46) has reached the last generation written
  (at most 60 ms), copy the frame into the page not on screen with alpha forced
  to 0xFF (the app draws xRGB with 0 in the top byte, which the core shows
  transparent) and the software brightness applied, then write words 79, 73-78,
  72 under a new generation. `EndBand`/`PresentBand` declare a band; `Hide`
  clears the show bit. The generation is kept ahead of word 72 and `ovl_gen`,
  because the player's library numbers its own descriptors from 1.
- **The overlay only during a session** (the vendored `docs/jellyfin.md` F1). The
  library's lock is an advisory exclusive `flock(LOCK_EX|LOCK_NB)` on
  `/tmp/h264ring.lock`, held by the player from `h264ring_open_devmem` to
  `h264ring_close`. The Go app never holds it while the player may run: when no
  session is open (a fresh core, the menu's Reset) and the lock is free, it takes
  the lock for the handshake only (pause, settings and rumble words zeroed, the
  write index rounded to 16, its descriptor, then the nonce alone, the ack),
  and releases it at once. Closing a library handle does not end a session, so
  the overlay stays up after the player exits. The player's session start writes
  its own descriptor (none) over ours; Watch notices (words 72-79 differ from what
  we wrote) and writes ours again. If the player ever draws its own overlay the
  two would fight: a contract between them is needed then.
- **The playback overlay** (`ring.Overlay`: a rectangle with alpha, the dot, the
  bar, formerly the PlexCRT plane and sprites) is composed in software into the
  same pages, transparent outside its lines, band = the lines it covers. The
  core-driven dot run is emulated by a goroutine redrawing each heartbeat.
  Unchanged overlay calls skip redraws only while the published descriptor still
  matches. A replaced descriptor or framebuffer presentation forces restoration;
  changed pixels retain the normal acknowledged page-flip path.
- **Playback controls.** Startup and commands run on session workers so network
  requests and player restarts do not block input and drawing. Pending seeks
  coalesce, stop cancels startup, and child replacement remains serialized.
  Jellyfin reports use a separate ordered worker; metadata refreshes wait for
  the stopped report without holding up the UI.
- **Status.** `Status()` reads the status block's seqlock (word 31, the words,
  word 3): heartbeat, state, live flags (OSD open, session), shown-picture count,
  overlay generation/state/error, raster (word 80), aspect (word 47).
  `Field()` is the heartbeat; `Foreign`/`Published` follow the display's
  `fetch_changes` (word 13) while a picture is shown. The CRT video lease,
  hardware brightness and audio tap of the PlexCRT core are stubs.
- **Launch.** `ring.Open` calls `LaunchCore` (MGL in `/tmp/h264ring.mgl`,
  `load_core` written once to `/dev/MiSTer_cmd`, only when `/tmp/CORENAME` is
  not `H264`; rbf `_Other/H264`, or `$MISTERZINE_H264_RBF`) and waits for the
  heartbeat.
- **Input.** `input.Poll` reads the whole input region (words 48-71) as a seqlock
  (in_tail first, in_head last) when the source offers `InputRegion`, keeps its
  own event count against `ev_count` (more than 32 between reads: the oldest are
  lost, everything is released), and maps the pads (all six OR'd, the left stick
  past 96/127 as the d-pad) to the legacy layout the UI uses: bits 0-3 d-pad
  (same bits), 4 OK <- B, 5 Back <- A, 6 L <- LB, 7 R <- RB. Key events feed
  the existing PS/2 set-2 decoder (the event's [9:0] is the PlexCRT key word's
  layout); since MiSTer sends no typematic, the last key held repeats in software.
  MiSTer's menu opening releases everything held and the pads read released
  until it closes (buttons held across it are ignored until let go); a release
  with no press is dropped. `PollAll` also delivers `Special` events (Key None):
  `MenuOpened`, `MenuClosed`, `ActionHome` ("Send Home to the program"),
  `ActionMenu` ("Program menu"), `InputLost`, `MouseButton`; `Poll` keeps them
  to itself so the current UI sees only keys.
