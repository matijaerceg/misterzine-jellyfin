# MisterZine Jellyfin beta help

## Install and launch

Download the Jellyfin installer attached to the release you want from
[GitHub releases](https://github.com/matijaerceg/misterzine-jellyfin/releases).
Copy it into the SD card's `Scripts` folder and run it from MiSTer Scripts.
A release candidate is not downloadable until its release has been published.
Current MiSTer Linux with Python 3.9 or newer and a network connection are required.

The installer uses `misterzine-jellyfin` on the SD card and adds **MisterZine
Jellyfin** to the MiSTer menu. It has separate settings and updates from Plex.
Open that menu entry to launch the app. Sign in using Jellyfin Quick Connect or
your server address, username and password. Your server needs transcoding
support for unsupported video formats and burned-in subtitles.

Browse freely; official beta builds ask for the shared MisterZine supporter code
when you start playback. See [beta access](BETA_ACCESS.md) and the
[terms of use](TERMS.md).

The screen stays black until the app draws its first menu. No background
animation or separate loading screen runs.

## Controls and picture

Use the D-pad to navigate, OK to select, and Back to return. During playback,
OK opens the controls. Back closes the controls; Back with the controls hidden
stops playback. Choose **Exit to MiSTer menu** to leave the app.

Start with the core OSD's **480i60** output, **Refresh Rate Match: Off**, and
**240p Playback: Off**. Use **576i50** only with a PAL-capable display.
Progressive 480p/576p requires a display that supports those modes; a normal
15 kHz CRT does not. Refresh Rate Match can switch between the 50 and 60 Hz
families during playback, so enable it only if your display supports both.
There is no 48 Hz mode.

**Display Shape** in the core OSD selects the shared 4:3 or 16:9 output shape.
In Native HDMI, a playing video takes the HDMI mode's own shape instead.
Use in-app video calibration to adjust the picture to your screen, and **Video
crop** to choose how much of a film's edges to crop. **De-flicker** in the core
OSD softens vertical detail on interlaced output while preserving horizontal
detail. **240p Playback** keeps menus interlaced
and switches only video to 240p, or 288p in the PAL family.

**Play 24fps at 25fps** is available with fixed PAL output and refresh matching
Off. It slightly speeds up films and raises audio pitch. Stop and restart
playback after changing settings that affect playback speed.

## Network and playback

Start with a low video bitrate on Wi-Fi and raise it if playback remains smooth.
A high bitrate needs adequate sustained throughput, not just a strong signal.
The buffered section of the playback bar shows the available seek range;
seeks outside it may require a fresh server request. Subtitles are burned into
the video by Jellyfin and require server-side transcoding. Turning them on
restarts the stream, and the server can take tens of seconds to resume.

Files the decoder supports play directly. Others are transcoded from the
start. If a direct stream falls behind, playback switches to a transcode once
at the same position.

This is beta software. Playback updates watched state and resume positions in
your Jellyfin library. Network interruptions, server behavior and different
MiSTer setups still need feedback.

## Updates and recovery

Use **Options > Updates** to prepare an update, then restart when ready.
Settings, calibration, sign-in and saved beta access are kept. The Jellyfin
rollback script returns to a previous installed release. Run repair or rollback
from MiSTer Menu after leaving the app.

## Reporting a problem

Report the version, board type, output mode, connection type, bitrate, whether
Jellyfin was transcoding, and the steps that reproduce the problem. Mention
whether switching to another core or rebooting changes it. A photo of a display
issue can help. Do not attach account files, tokens, supporter codes or whole
SD-card images. Review logs before sharing them.
