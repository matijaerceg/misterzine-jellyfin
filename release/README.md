# MisterZine Jellyfin beta help

[Downloads](https://github.com/matijaerceg/misterzine-jellyfin/releases) ·
[Installation](https://github.com/matijaerceg/misterzine-jellyfin#install) ·
[Full reference](https://github.com/matijaerceg/misterzine-jellyfin/blob/main/docs/REFERENCE.md)

### Before you start

This is unstable beta software. It talks to your real Jellyfin account:
playback, watched marks and resume points change your library, and a bug could
change them wrongly. Use it at your own risk; no responsibility is taken for
what it does to your library or your MiSTer.

## Install and launch

Download `MisterZine-Jellyfin-Install-Beta.sh` from the release you want on
[GitHub releases](https://github.com/matijaerceg/misterzine-jellyfin/releases).
Copy it into the SD card's `Scripts` folder and run it from MiSTer Scripts.
Current MiSTer Linux with Python 3.9 or newer and a network connection are
required.

The installer uses `misterzine-jellyfin` on the SD card and adds **MisterZine
Jellyfin** to the MiSTer menu. Its settings and updates are separate from
MisterZine Plex. Open that menu entry to launch the app. Choose your server from
those found on your network, or type its address, then choose your user or
sign in with Jellyfin Quick Connect or your username and password. Your server
needs transcoding support for unsupported video formats and picture subtitles.

Browse freely; official beta builds ask for the shared MisterZine supporter code
when you start playback. See [beta access](BETA_ACCESS.md) and the
[terms of use](TERMS.md).

The screen stays black until the app draws its first menu. No background
animation or separate loading screen runs.

## Controls and picture

Use the D-pad to navigate, OK to select, and Back to return. During playback,
OK opens the controls. The first Back closes whatever is on screen; Back with
nothing on screen stops playback. Choose **Exit to MiSTer menu** to leave the
app. Hold L or R for two seconds during an episode to play the previous or
next episode; releasing early cancels the change.

Start with the core OSD's **480i60** output, **Refresh Rate Match: Off**, and
**240p Playback: Off**. Use **576i50** only with a PAL-capable display.
Progressive 480p/576p requires a display that supports those modes; a normal
15 kHz CRT does not. Refresh Rate Match can switch between the 50 and 60 Hz
families during playback, so enable it only if your display supports both.
There is no 48 Hz mode.

**Display Shape** in the core OSD selects the shared 4:3 or 16:9 output shape.
In Native HDMI, a playing video takes the HDMI mode's own shape instead.
HDMI scaling follows your MiSTer.ini. If it turns on integer scaling
(`vscale_mode`) for every core, the picture may not fill the screen; add a
section for this core to MiSTer.ini to fill it:

```ini
[MisterZine Jellyfin]
vscale_mode=0
```

Use in-app video calibration to adjust the picture to your screen, and **Video
crop** to choose how much of a film's edges to crop. **De-flicker** in the core
OSD softens thin horizontal lines so they flicker less on interlaced output.
**240p Playback** keeps menus interlaced and switches only video to 240p, or
288p in the PAL family.

**Play 24fps at 25fps** is available with fixed PAL output and refresh matching
Off. It slightly speeds up films and raises audio pitch. Stop and restart
playback after changing settings that affect playback speed.

## Network and playback

Start with a low video bitrate on Wi-Fi and raise it if playback remains smooth.
A high bitrate needs adequate sustained throughput, not just a strong signal.
The buffered section of the playback bar shows the available seek range;
seeks outside it may require a fresh server request.

Text subtitles (SRT, ASS/SSA, WebVTT) are drawn by the app, so the video needs
no transcode for them and changing them does not restart it. **Subtitle size**
in Options, or under **More** during playback, makes them larger. Picture
subtitles (PGS, VobSub) are burned into the video by Jellyfin and require
server-side transcoding. Turning them on restarts the stream, and the server
can take tens of seconds to resume.

Files the decoder supports play directly. Others are transcoded from the
start. If a direct stream falls behind, playback switches to a transcode once
at the same position. **Direct play** in Options or playback **More** can be
switched off to ask the server to convert every video. Seeking and playback
mode changes can land several seconds from the requested position because
streams resume at keyframes or segment boundaries.

This is beta software. Playback updates watched state and resume positions in
your Jellyfin library. Network interruptions, server behavior and different
MiSTer setups still need feedback.

## Troubleshooting

### Installation or sign-in isn't working

Check the network connection, SD card space and that MiSTer Linux is current
(Python 3.9 or newer). Running the installer again repairs the installed
release's files, or installs a newer release; settings are kept.

Sign-in lists the Jellyfin servers that answer on your local network. If yours
is missing, check that auto discovery is on in the server's settings (it is by
default) and that nothing blocks UDP port 7359 to it. A server on another
network, or in a container without host networking, may not answer either way.
Choose **Enter address** and type the address instead.

To type it, enter the server's address as you would in a web browser. A bare
name such as `jellyfin.example.com` is tried over HTTPS first. Only local
servers can fall back to plain HTTP on port 8096; type `http://` for another
local HTTP port. Plain HTTP sends sign-in details and media unencrypted and
requires your approval for that server. Public HTTP addresses are refused.
Existing HTTP sessions without approval return to sign-in after this update.
HTTPS needs a valid server certificate and the correct time on your MiSTer.
What the screen says next:

- **No Jellyfin server at** the address: something answered, but not
  Jellyfin. Check the address, port and any path your reverse proxy uses.
- **The server's address was not found**: the name does not exist. Check it,
  or use the server's IP address.
- **Could not reach** the address: nothing answered. Check that the server is
  running and that the MiSTer is on a network that can reach it.
- **Quick Connect** is missing from the list: it is turned off on the server.
  Turn it on in the server's dashboard, or use your user name and password.
- Your user is not listed: many servers hide their users. Choose **Other
  user** (or **User name and password**), or use Quick Connect.
- **Wrong user name or password**, or **Wrong password** for a user chosen
  from the list: try again, or use Quick Connect.

Right after power-on the app may show **Connecting** for up to a minute while
the network comes up or, on a MiSTer without a clock chip, while the clock is
set from the internet. After that it shows the error and tries again every 30
seconds; OK tries at once. If your session is ended on the server, the app
returns to sign-in with the address kept.

### Playback stutters

Try a lower **Video bitrate** in Options, and check that your Jellyfin server
can transcode the video in real time: its dashboard shows the transcode's
speed. The steps below 3 Mbps (2, 1 and 0.4 Mbps) are for weak Wi-Fi; they
also make the transcoded picture smaller, which costs little on a CRT. A file
whose video is above the chosen bitrate is transcoded rather than streamed as
stored.

On HDMI, if films stutter or motion judders, look in **Options** below
**Video crop**: the app adds rows for MiSTer INI settings that hurt playback.
**HDMI refresh** means HDMI is held at a fixed rate that the core's Output Mode
or Refresh Rate Match does not keep to, **HDMI sound** means DVI mode (no sound
over HDMI), and **INI section** means a later `[MiSTer]` section overrides the
app's `[MisterZine Jellyfin]` section. Select a row for the lines to add. See
[HDMI setup](https://github.com/matijaerceg/misterzine-jellyfin/blob/main/docs/HDMI.md).

### Subtitles are missing or slow to appear

The first time a subtitle track embedded in a large file is asked for, the
server extracts it from the file, which can take a while; the video plays
without subtitles meanwhile. The app waits 30 seconds for an answer and tries
twice more, then gives up and shows none. Turn subtitles off and on again to ask
once more. The server's **Allow subtitle extraction on the fly** setting, under
Dashboard > Playback > Transcoding, decides how it handles embedded subtitles,
and so affects how long they take to start.

Picture subtitles need a transcode. If choosing one fails, check that the
server allows your account to transcode.

### Updates or downloads fail

Use **Options > Updates** to install a release, then choose **Restart now** when
ready. Sign-in and settings are kept. If the new version does not start, the
previous one is put back.

Downloads go through MiSTer Downloader, and fall back to a direct fetch of the
release file when that fails. If both fail, the message under **Options >
Updates** says whether your MiSTer's connection, its clock, its SD card or the
release is the problem. A board whose clock is far off cannot verify any secure
site, so set the time in MiSTer Menu or run Downloader once so it syncs over the
network. Wait for any running Downloader or Update All to finish before trying
again. The full reasons are kept in
`/media/fat/misterzine-jellyfin/updates/last-error.log` and in the report.

### How do I go back or remove it?

Return to MiSTer Menu, then use the entries in **Scripts**:

- **MisterZine-Jellyfin-Rollback** returns to the previous installed release.
- **MisterZine-Jellyfin-Uninstall** removes the app. It keeps your sign-in,
  settings and saved beta access by default; removing all of the app's data
  requires typing REMOVE.

### Still stuck?

**Options > Send a report** in the app writes a report to
`/media/fat/misterzine-jellyfin/report.txt`. It holds the app and launcher logs,
the reasons for connection and update failures (including `last-error.log`),
the video settings read from the MiSTer INI, which MiSTer main is running and
the framebuffer state. It can name media titles and playback details; sign-in
tokens, the server address and account files are never included. This app
uploads the report and shows a short code to post where you asked for help.
A copy remains on the card; if upload fails, attach that file yourself.

If the app does not start, **MisterZine-Jellyfin-Diagnostics** in MiSTer
Scripts writes the same file.

Report problems on
[GitHub](https://github.com/matijaerceg/misterzine-jellyfin/issues) with the
report file, the version from Options, your MiSTer model, the output mode and
display, the connection type, the bitrate, whether Jellyfin was transcoding,
and the steps that reproduce the problem. Mention whether switching to another
core or rebooting changes it. A photo of a display issue can help. Review the
report before sharing it, and never share account files, tokens, supporter
codes or whole SD-card images.
