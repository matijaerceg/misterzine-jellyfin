# Watching from a Jellyfin server

The core comes with a Jellyfin client: the films and shows on your
[Jellyfin](https://jellyfin.org) server, shown by the core on HDMI and on the analog output, a
CRT included, with their sound and subtitles. You pick what to watch in any Jellyfin app (on
your phone, in a web browser) and play it on the MiSTer, the way you would play it on a TV.

!!! warning "Early"
    The client is new. It has no menu of its own to browse your library: you choose what
    to play in another Jellyfin app, and the MiSTer plays it. Stereo sound.

## What plays

Anything on your server. When a file already suits the core -- H.264 at 10 Mbit/s or less, up
to 1280x720 at 30 frames per second or up to 854x480 at 60 -- the MiSTer plays the file itself.
Anything else -- 1080p and 4K films, HEVC, interlaced recordings -- the server converts as it
sends it (a **transcode**) to a picture the core plays in real time: **1280x720** when Video
Output is **Native (HDMI)** (what Auto picks on a MiSTer used with HDMI only), so a 1080p film
keeps its detail on an HD display; **854x480** on the Interlaced and Progressive outputs, which
are 480 lines (576 at 50 Hz) and would show no more.

Films and shows made at 25 frames per second (PAL) switch the output to 50 Hz when
**Refresh Rate** is **Auto** ([Settings](settings.md)).

## Installing

Copy the release's `Scripts/jellyfin.sh` and `jellyfin/` folder to the root of the SD card
(`/media/fat/`), beside the core itself ([Install the core](../getting-started/install.md)).

## First run: the settings

Run **Scripts** → **jellyfin** from the MiSTer menu. The first time, it creates
`/media/fat/jellyfin/jellyfin.ini` and stops. Edit that file (from your PC over the network, or
on the SD card) and set `server=` to your server's address, as a web browser opens it:

```ini
server=https://jellyfin.example.com
name=MiSTer
user=
password=
```

| Setting | Meaning |
|---|---|
| `server` | your Jellyfin server: `https://...`, or `http://` and its address and port (`http://jellyfin.local:8096`) |
| `name` | how the MiSTer appears in Jellyfin's list of devices to play on |
| `user`, `password` | only for a server with **Quick Connect** turned off: the account to sign in with. They are kept in the file as written; prefer Quick Connect |
| `screensaver_dim`, `screensaver_black` | optional: the minutes before the screen dims, then goes black, while nothing plays (5 and 30; 0 turns that step off): [The screensaver](#the-screensaver) |

## Signing in

Run **jellyfin** again. The core loads and shows a six-digit code. In any Jellyfin app where you
are signed in, open **Settings** → **Quick Connect** and enter the code. The MiSTer signs in to
your account and remembers it; you do this once.

## Playing something

When the MiSTer shows **Ready to play**, open Jellyfin on your phone or in a browser, choose a
film or an episode, and pick the MiSTer (the `name` from the settings) as the device to play on
-- the **cast** / **Play on** button in the Jellyfin apps. It starts playing on the MiSTer; the
app becomes its remote: pause, seek, stop, volume, the audio and subtitle tracks.

When something starts, the MiSTer shows its title and progress for a few seconds; the panel
stays up while the film is paused.

**Subtitles** in text formats (SRT, ASS, WebVTT) are drawn by the MiSTer over the picture.
Picture subtitles (DVD and Blu-ray subtitles) are burned into the picture by your server, which
makes it convert the film.

## Controls on the MiSTer

A gamepad or the keyboard controls the film playing:

| Gamepad | Keyboard | |
|---|---|---|
| A, Start | Space, Enter, K | pause / play |
| Left / Right | Left / Right | back 10 seconds / forward 30 seconds |
| LB, Down / RB, Up | Page Down / Page Up | back / forward 5 minutes |
| X | S | the next subtitle track (or off) |
| Y | A | the next audio track |
| B | I | show the title and progress |
| Back + Start | Esc, Q | stop -- and, on the **Ready to play** screen, quit to the MiSTer menu |

**Seeking** adds up: tap Right three times, or hold it, and the panel shows where you are going;
the film jumps there once you stop pressing. The picture stays on screen, frozen, while the film
starts again at the new place (the panel says **Loading**): about two seconds for a file the MiSTer
plays directly, about three for a converted one (a transcode) while your server restarts its
conversion.

Switching **subtitles** between text tracks (or off) is instant. A picture subtitle the server must
burn in restarts the film where it was.

**Send Home to the program** in the MiSTer menu stops what plays; **Program menu** shows the
title and progress ([Settings](settings.md)).

**To quit:** on the **Ready to play** screen press Back + Start (or Esc), or load another core
from the MiSTer menu. The MiSTer returns to its menu.

## The screensaver

After 5 minutes while nothing plays (the **Ready to play** screen, the sign-in code, or a film
paused), the screen dims. A line of text fades in and out at different places, saying what is
up: **Ready to play**, the sign-in code, or the paused film and where it stopped. After 30
minutes the screen goes black, with the same line. This protects OLED, plasma and CRT screens
from burn-in.

A button, or playing something from a Jellyfin app, brings the screen back; a button press that
wakes it does nothing else. To change the times, add these lines to `jellyfin.ini` (minutes;
0 turns a step off):

```ini
screensaver_dim=5
screensaver_black=30
```

## If it does not work

The client writes what it did to `/media/fat/jellyfin/jellyfin.log`. See
[Troubleshooting](../reference/troubleshooting.md#jellyfin).
