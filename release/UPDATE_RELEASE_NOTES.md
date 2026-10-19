# MisterZine Jellyfin 0.1.0-beta.1

First packaged beta of the CRT-first Jellyfin application.

- Sign in and browse with a controller; enter the shared MisterZine supporter
  code to start playback. Access is remembered across updates and rollback.
- Separate Jellyfin installer, main-menu entry and updater; Plex installs keep
  their own settings and release channel.
- A static, faint-logo loading screen replaces the FPGA test pattern as soon
  as the app can draw. A brief pattern during FPGA startup can still appear.
- Direct streaming and Jellyfin transcoding, burned-in subtitles, pause/resume,
  seeking, buffered-range display and surround downmix boost.
- 4:3/16:9 display shape, saved calibration and crop, vertical de-flicker,
  menu/paused dimming and playback-only 240p/288p.
- NTSC/PAL output with optional 50/60 refresh matching. No 48 Hz mode.
  Optional 24-to-25 film speed-up requires fixed PAL and raises audio pitch.

Playback updates Jellyfin watched state and resume positions. Network seeking,
server compatibility and different hardware setups remain beta test areas.
The included FPGA image reports `dev-crt`; its original qualification manifest
is included so the image remains traceable.

Installers and update notices become available when this release and its
catalogue are published. See README.md for setup and feedback instructions.
