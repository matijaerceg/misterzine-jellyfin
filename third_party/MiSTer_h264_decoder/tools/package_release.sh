#!/usr/bin/env bash
# package_release.sh -- stage the SD-card tree for a release and zip it.
#
# The zip exists so a user extracts one thing to the SD root and gets the core under
# _Other/, the Moonlight client (Scripts/moonlight.sh, moonlight/: clients/moonlight/build.sh's
# build/dist, the manual's using/moonlight.md), the Jellyfin client (Scripts/jellyfin.sh, jellyfin/:
# clients/jellyfin/build.sh's build/dist, using/jellyfin.md), the video player (Scripts/h264player.sh,
# h264player/: clients/player/build.sh's build/dist, using/player.md), the camera viewer (Scripts/h264cam.sh,
# h264cam/: clients/uvc/build.sh's build/dist, using/camera.md) and a short H264_INSTALL.txt. (The HPS client
# library ships with the programs that link it, not here: docs/architecture.md.)
#
#   ./tools/package_release.sh [--rbf releases/H264_YYYYMMDD.rbf] [--no-moonlight] [--no-jellyfin] [--no-player]
#                              [--no-cam]
#     -> releases/MiSTer_h264_decoder_<version>.zip
# Build the clients first (USE_DOCKER=1 clients/moonlight/build.sh, clients/jellyfin/build.sh,
# clients/player/build.sh, clients/uvc/build.sh) from the same commit; without one the script
# refuses, unless --no-moonlight / --no-jellyfin / --no-player / --no-cam says the release goes
# without it.
#
# <version> is CORE_VERSION from h264/emu.sv (v0.1.0 on a release commit, dev-<slug> on a
# feature branch), so the zip names the same build the OSD and the .rbf name.
set -euo pipefail
cd "$(dirname "$0")/.."

RBF=""
MOONLIGHT=1
JELLYFIN=1
PLAYER=1
CAM=1
while [[ $# -gt 0 ]]; do
  case "$1" in
    --rbf) RBF="$2"; shift 2 ;;
    --no-moonlight) MOONLIGHT=0; shift ;;
    --no-jellyfin) JELLYFIN=0; shift ;;
    --no-player) PLAYER=0; shift ;;
    --no-cam) CAM=0; shift ;;
    -h|--help) sed -n '2,21p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

VER="$(sed -n 's/.*`define CORE_VERSION "\([^"]*\)".*/\1/p' h264/emu.sv | head -1)"
[ -n "$VER" ] || { echo "no CORE_VERSION in h264/emu.sv" >&2; exit 1; }

if [ -z "$RBF" ]; then
  RBF="$(ls -t releases/H264_*.rbf 2>/dev/null | grep -v MARGINAL | head -1 || true)"
  [ -n "$RBF" ] || { echo "no releases/H264_*.rbf -- build one first" >&2; exit 1; }
fi
[ -f "$RBF" ] || { echo "not found: $RBF" >&2; exit 1; }
case "$(basename "$RBF")" in *MARGINAL*) echo "refusing to package a _MARGINAL_ build" >&2; exit 1 ;; esac

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/_Other"
cp "$RBF" "$STAGE/_Other/"
ML=clients/moonlight/build/dist
if [ "$MOONLIGHT" = 1 ]; then
  [ -x "$ML/moonlight/moonlight" ] && [ -f "$ML/Scripts/moonlight.sh" ] ||
    { echo "no Moonlight client in $ML -- USE_DOCKER=1 clients/moonlight/build.sh (or --no-moonlight)" >&2; exit 1; }
  cmp -s "$ML/Scripts/moonlight.sh" clients/moonlight/mister/moonlight.sh ||
    { echo "$ML's launcher differs from clients/moonlight/mister/: rebuild it" >&2; exit 1; }
  for src in clients/moonlight/src/mister/* clients/moonlight/patches/* hps/lib/* hps/include/*; do
    [ "$ML/moonlight/moonlight" -nt "$src" ] ||
      { echo "$ML/moonlight/moonlight is older than $src: rebuild it" >&2; exit 1; }
  done
  cp -r "$ML/Scripts" "$ML/moonlight" "$STAGE/"
fi
JF=clients/jellyfin/build/dist
if [ "$JELLYFIN" = 1 ]; then
  [ -x "$JF/jellyfin/jellyfin" ] && [ -f "$JF/Scripts/jellyfin.sh" ] && [ -f "$JF/jellyfin/cacert.pem" ] ||
    { echo "no Jellyfin client in $JF -- USE_DOCKER=1 clients/jellyfin/build.sh (or --no-jellyfin)" >&2; exit 1; }
  cmp -s "$JF/Scripts/jellyfin.sh" clients/jellyfin/mister/jellyfin.sh ||
    { echo "$JF's launcher differs from clients/jellyfin/mister/: rebuild it" >&2; exit 1; }
  for src in clients/jellyfin/src/* clients/jellyfin/third_party/*/* clients/common/*.[ch] \
             clients/common/third_party/*/* hps/lib/* hps/include/*; do
    [ "$JF/jellyfin/jellyfin" -nt "$src" ] ||
      { echo "$JF/jellyfin/jellyfin is older than $src: rebuild it" >&2; exit 1; }
  done
  mkdir -p "$STAGE/Scripts"
  cp "$JF/Scripts/jellyfin.sh" "$STAGE/Scripts/"
  cp -r "$JF/jellyfin" "$STAGE/"
fi
PL=clients/player/build/dist
if [ "$PLAYER" = 1 ]; then
  [ -x "$PL/h264player/h264player" ] && [ -f "$PL/Scripts/h264player.sh" ] ||
    { echo "no video player in $PL -- USE_DOCKER=1 clients/player/build.sh (or --no-player)" >&2; exit 1; }
  cmp -s "$PL/Scripts/h264player.sh" clients/player/mister/h264player.sh ||
    { echo "$PL's launcher differs from clients/player/mister/: rebuild it" >&2; exit 1; }
  for src in clients/player/src/* clients/common/*.[ch] clients/common/third_party/*/* hps/lib/* hps/include/*; do
    [ "$PL/h264player/h264player" -nt "$src" ] ||
      { echo "$PL/h264player/h264player is older than $src: rebuild it" >&2; exit 1; }
  done
  mkdir -p "$STAGE/Scripts"
  cp "$PL/Scripts/h264player.sh" "$STAGE/Scripts/"
  cp -r "$PL/h264player" "$STAGE/"
fi
CM=clients/uvc/build/dist
if [ "$CAM" = 1 ]; then
  [ -x "$CM/h264cam/h264cam" ] && [ -f "$CM/Scripts/h264cam.sh" ] ||
    { echo "no camera viewer in $CM -- USE_DOCKER=1 clients/uvc/build.sh (or --no-cam)" >&2; exit 1; }
  cmp -s "$CM/Scripts/h264cam.sh" clients/uvc/mister/h264cam.sh ||
    { echo "$CM's launcher differs from clients/uvc/mister/: rebuild it" >&2; exit 1; }
  for src in clients/uvc/src/* clients/common/*.[ch] clients/common/third_party/*/* hps/lib/* hps/include/*; do
    [ "$CM/h264cam/h264cam" -nt "$src" ] ||
      { echo "$CM/h264cam/h264cam is older than $src: rebuild it" >&2; exit 1; }
  done
  mkdir -p "$STAGE/Scripts"
  cp "$CM/Scripts/h264cam.sh" "$STAGE/Scripts/"
  cp -r "$CM/h264cam" "$STAGE/"
fi
cat > "$STAGE/H264_INSTALL.txt" <<TXT
MiSTer H264 core -- $VER

1. Copy the _Other/ folder to the root of your MiSTer SD card (/media/fat).
   MiSTer's core browser lists the newest H264_YYYYMMDD.rbf under Other.
2. The core plays what a program on the MiSTer's ARM side sends it. On its own
   it shows a test pattern.
3. Moonlight (stream a PC running Sunshine): the Scripts/ and moonlight/ folders
   go to the SD root too. Run Scripts -> moonlight from the MiSTer menu; the first
   run creates moonlight/moonlight.ini (set host= to the PC), the next pairs (a PIN
   for Sunshine's web page), then each run streams.
4. Jellyfin (watch your Jellyfin server's films and shows): the Scripts/ and
   jellyfin/ folders go to the SD root too. Run Scripts -> jellyfin; the first run
   creates jellyfin/jellyfin.ini (set server= to your server), the next shows a
   Quick Connect code to sign in with, then play from any Jellyfin app to the MiSTer.
5. The video player (files from the SD card, a USB drive or a network share): the
   Scripts/ and h264player/ folders go to the SD root too. Put videos in
   /media/fat/video and run Scripts -> h264player. It plays H.264 (High or Main,
   8-bit) up to 1280x720 at 30 frames/s, and MPEG-2, DivX / Xvid, WMV, Theora and
   Motion JPEG up to DVD size; anything else is named on screen, with the manual's
   Compatibility page saying how to convert it.
6. The camera viewer (a USB webcam or capture device): the Scripts/ and h264cam/
   folders go to the SD root too. Run Scripts -> h264cam and plug the camera in.

Manual: https://owenb321.github.io/MiSTer_h264_decoder/
TXT

OUT="releases/MiSTer_h264_decoder_${VER}.zip"
rm -f "$OUT"
python3 - "$STAGE" "$OUT" <<'PY'
import os, sys, zipfile
stage, out = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED) as z:
    for root, _, files in os.walk(stage):
        for f in sorted(files):
            p = os.path.join(root, f)
            z.write(p, os.path.relpath(p, stage))
print('packaged', out, os.path.getsize(out), 'bytes')
PY
