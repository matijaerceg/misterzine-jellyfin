# MisterZine Jellyfin

Browse and play a Jellyfin library on MiSTer, with a controller-driven interface
for CRT displays. The Go application handles browsing and playback controls;
`jfplay` sends compressed H.264 video and decoded stereo audio to Ben Owen's
[MiSTer H.264 decoder](third_party/MiSTer_h264_decoder/README.md).

This is beta software for CRT-first Jellyfin playback. Official beta builds let
you sign in and browse before unlocking playback with the shared MisterZine
supporter code. Stable releases and development builds do not require a beta
code. See [beta access](release/BETA_ACCESS.md).

Release candidates are being prepared; installation assets are available only
once a release is published. Use the Jellyfin installer attached to that
release, then launch **MisterZine Jellyfin** from the MiSTer menu. Jellyfin has its
own install directory and updater, separate from MisterZine Plex.
See [installation and help](release/README.md).

## Current capabilities

- Jellyfin sign-in using Quick Connect or a username and password.
- Library browsing, direct streaming, and server-side HLS transcoding.
- Pause, resume, in-process seeking with restart fallback, and playback progress reporting.
- Server-burned subtitles and surround downmix boost with peak limiting.
- Display modes and refresh matching are selected in the H.264 core's OSD;
  optional 240p/288p playback and 23.976/24-to-25 film speed-up require a core
  with HPS interface 1.3. Film speed-up raises the audio pitch; it does not
  preserve pitch.
- Analog letterbox and Fill crop controls through the H.264 core.
- H.264 High/Main video up to 1280x720, with a configurable video bitrate cap
  up to 8 Mbit/s. Higher frame rates are limited to lower widths; see
  [playback details](docs/PLAYBACK.md).

Selected subtitles use Jellyfin video transcoding to burn text into the picture.
Seek interruptions, network buffering, and hardware-specific
behavior remain under active testing. Playback changes watched state and resume
positions in the connected Jellyfin library.

## Build and run

You need MiSTer hardware, its H.264 decoder core, a Jellyfin server, Go 1.27,
and an ARM Linux cross compiler. The server must be able to transcode media
that falls outside the decoder's supported formats.

Follow [Build from source](docs/BUILD.md) for the application and worker, and
[the toolchain guide](toolchain/README.md) for the native dependencies.

The application executable is currently called `plexcrt`; some internal names
and older documents still refer to Plex. Use the build and playback guides for
this fork's current behavior.

## Development and licenses

[Contributing](CONTRIBUTING.md) · [Architecture](docs/ARCHITECTURE.md) ·
[Player contract](arm/jfplay.md) · [Component licenses](release/THIRD_PARTY_NOTICES.md)

Component licenses apply separately; source availability does not grant additional
rights. Retained license texts and upstream notices remain with their components.
This project is independent of Jellyfin and MiSTer.
