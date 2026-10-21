# MisterZine Jellyfin

Watch your Jellyfin library on MiSTer. Built for CRTs, with controller
navigation and HDMI support. The Go application handles browsing and playback
controls; `jfplay` sends compressed H.264 video and decoded stereo audio to this
project's fork of Ben Owen's
[MiSTer H.264 decoder](third_party/MiSTer_h264_decoder/README.md) core, which
ships with the app.

## Install

The current release is a beta, published as a GitHub pre-release. Beta builds
let you sign in and browse freely, but playback needs the shared MisterZine
supporter code, available to supporters on
[Patreon](https://www.patreon.com/MisterZine). Stable releases, once published,
need no code. See [beta access](release/BETA_ACCESS.md).

You'll need a networked MiSTer running current MiSTer Linux with
Python 3.9 or newer, some free space on the SD card (each installed release
takes about 14 MB, and two are kept), and a Jellyfin server you can sign in to.
The server must be allowed to transcode for your account: anything the decoder
cannot play directly, and every video with subtitles on, is converted by the
server. Run **Update All** or **Downloader** first so MiSTer Linux is current.

1. Download `MisterZine-Jellyfin-Install-Beta.sh` from
   [Releases](https://github.com/matijaerceg/misterzine-jellyfin/releases) and
   put it in the SD card's `Scripts` folder
2. Run it from **Scripts**
3. Open **MisterZine Jellyfin** from the MiSTer main menu
4. Enter your server's address, then sign in with Quick Connect or your user
   name and password

Enter the supporter code when you first press Play. It is saved for next time.

If the installer or an update reports a download failure, check whether
**Update All** or **Downloader** works on the same MiSTer. If they fail too, the
problem is the board's connection or clock, not this app: run Downloader once so
the board syncs its time, or set the clock in MiSTer Menu, then try again. If
Downloader works and this app still cannot download, the on-screen message says
why; [report it](https://github.com/matijaerceg/misterzine-jellyfin/issues) with
that message.

CRT output is 480i or PAL 576i, with optional 240p/288p playback. HDMI supports
480p, 576p and native output. Choose them in the core's OSD.

This is unstable beta software. It talks to your real Jellyfin account:
playback, watched marks and resume points change your library, and a bug could
change them wrongly. Use it at your own risk; no responsibility is taken for
what it does to your library or your MiSTer.

[Need help?](release/README.md) · [Full reference](docs/REFERENCE.md) ·
[HDMI setup](docs/HDMI.md)

## What it does

- Jellyfin sign-in using Quick Connect or a user name and password.
- Home rows, library browsing by name, date added, release date, folders and
  collections, search, and resume positions and watched marks kept in step
  with Jellyfin.
- Direct streaming of files the decoder supports; the server transcodes the
  rest. H.264 High/Main video up to 1280x720, with a video bitrate cap for
  transcodes of up to 8 Mbit/s. Higher frame rates are limited to lower widths;
  see [playback details](docs/PLAYBACK.md).
- Pause, seeking, audio and subtitle track choice, skip intro and credits,
  autoplay and shuffle.
- Subtitles burned into the picture by the server, and a surround downmix
  boost with peak limiting.
- Display modes and refresh matching are selected in the core's OSD, including
  240p/288p playback and 23.976/24-to-25 film speed-up on PAL output. Film
  speed-up raises the audio pitch; it does not preserve pitch.
- Letterbox, 14:9 and Fill crop, and on-screen calibration of the picture area.
- In-app updates with rollback to the previous release.

Seek interruptions, network buffering, and hardware-specific behavior remain
under active testing.

## Build and run

You need MiSTer hardware, a Jellyfin server, Go 1.27, and an ARM Linux cross
compiler. Follow [Build from source](docs/BUILD.md) for the application and
worker, and [the toolchain guide](toolchain/README.md) for the native
dependencies.

The app is derived from the MisterZine Plex app. Its executable is still called
`plexcrt`, and some internal names still refer to Plex.

## Development and licenses

[Contributing](CONTRIBUTING.md) · [Architecture](docs/ARCHITECTURE.md) ·
[Player contract](arm/jfplay.md) · [Supporter builds](docs/SUPPORTER_BUILDS.md) ·
[Terms](release/TERMS.md) · [Component licenses](release/THIRD_PARTY_NOTICES.md)

Component licenses apply separately; source availability does not grant additional
rights. Retained license texts and upstream notices remain with their components.
Movie artwork belongs to its owners. This project is independent of Jellyfin and
MiSTer.
