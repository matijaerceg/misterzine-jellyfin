# What you need

- A MiSTer (DE10-Nano) with an up-to-date MiSTer menu.
- An HDMI display, or an analog display through a MiSTer I/O board: a CRT TV at 15 kHz, a
  VGA monitor, or an external scaler ([Video output](../video/output.md)).
- **A program that feeds the core.** The core decodes; it does not fetch anything from
  the network or read files by itself. Programs that use it run on the MiSTer's ARM side.

The core comes with four programs. Each one is started from MiSTer's **Scripts** menu,
loads the core by itself, and is driven with a gamepad or a keyboard:

| Program | What it plays | You also need |
|---|---|---|
| **Moonlight** ([Streaming from a PC](../using/moonlight.md)) | A PC's desktop or its games, with their sound. The MiSTer's keyboard, mouse and gamepads control the PC, and rumble comes back. | A PC running [Sunshine](https://github.com/LizardByte/Sunshine), on the same network |
| **Jellyfin** ([Watching from a Jellyfin server](../using/jellyfin.md)) | The films and shows on your Jellyfin server, with their sound and subtitles. You pick them in any Jellyfin app (a phone, a browser) and play them on the MiSTer. | A [Jellyfin](https://jellyfin.org) server, and a phone or computer to pick from |
| **Video player** ([Playing video files](../using/player.md)) | Video files from the SD card, a USB drive or a network share, picked from a list on screen. | -- |
| **Camera viewer** ([Showing a USB camera](../using/camera.md)) | A USB webcam or capture device: the MiSTer as a screen for another source. | A USB camera that sends Motion JPEG |

A MiSTer with a network connection (wired or Wi-Fi) is needed for Moonlight and Jellyfin.
[Install the core](install.md) puts all four on the SD card.

Other programs can use the core too: it takes video from any program written for its
interface. If you want to write one, see [Integrating a program](../integrating/overview.md).
