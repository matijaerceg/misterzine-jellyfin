# Third-party components and licenses

This notice describes the components used by the source tree and binary release
packages. Source checkouts omit generated ARM executables and libraries. Binary
packages include the application, `jfplay`, FFmpeg and the qualified H.264 FPGA
image, with corresponding source/build materials supplied alongside them.
Component licenses and upstream notices are retained with their source; license
texts referenced below are in `licenses/` in a package or `release/licenses/`
in a source checkout.

This notice does not grant a license to other project code. The repository has
no root `LICENSE` file.

## MiSTer H.264 core and HPS library

The core source in `third_party/MiSTer_h264_decoder/` is GPL-3.0-or-later. Its
upstream `LICENSE` and detailed per-file `NOTICE` remain in that directory. The
notice identifies MiSTer framework files with GPL-2.0-or-later or
GPL-3.0-or-later terms; the combined core is GPL-3.0-or-later. The associated
HPS client library under `hps/` is separately MIT-licensed; see
`hps/LICENSE`. The GPL texts are in `release/licenses/GPL-2.0.txt` and
`GPL-3.0.txt`.

## ARM playback worker

`arm/jfplay.c` is GPL-3.0-or-later and links the MIT-licensed HPS library.
The retained upstream Jellyfin client source under
`third_party/MiSTer_h264_decoder/clients/jellyfin/` vendors cJSON 1.7.19 under
MIT; it is not used by the current Go application or `jfplay` build. Its
license is included beside the source.

The ARM build scripts fetch FFmpeg 7.1.1 and OpenSSL 3.0.15 from their upstream
source releases. FFmpeg is configured without `--enable-gpl` or
`--enable-nonfree`, and the playback worker statically links its libavformat,
libavcodec, libswresample, and libavutil libraries. FFmpeg's applicable license
is LGPL-2.1-or-later; the source tree and exact configuration are documented in
`toolchain/build_ffmpeg.sh`. OpenSSL is configured static-only and is licensed
under Apache-2.0. The ARM executable is linked statically, including the GNU C
Library supplied by the ARM cross toolchain. The LGPL text is included in
`release/licenses/LGPL-2.1.txt`; OpenSSL's Apache-2.0 text is in
`release/licenses/Apache-2.0.txt`.

The source and build scripts are provided here, but this source publication
does not include those generated libraries or binaries. Any separate binary
distribution must include the exact applicable notices and source materials,
including those for the toolchain's GNU C Library, and meet the terms that apply
to its static LGPL and OpenSSL links.

## Application assets and Go runtime

The application includes the Go runtime and standard library under the Go
BSD-style license, reproduced in `release/licenses/Go.txt`.

The bitmap font atlases are generated from Roboto and Roboto Condensed fonts,
version 2.137. Those fonts are licensed under Apache-2.0; see
`release/licenses/Apache-2.0.txt`. The generated atlases are rasterized and
resized for the display.

## Installer

The optional MiSTer Downloader bootstrap is fetched by the installer from its
pinned upstream revision. It is not part of the playback worker. Direct package
download is also supported; release package hashes are verified before install.
