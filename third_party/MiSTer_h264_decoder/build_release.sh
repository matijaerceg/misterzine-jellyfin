#!/bin/bash
# build_release.sh — Compile the core and/or pack a MiSTer-loadable .rbf.
#
# MiSTer's HPS FPGA loader expects a COMPRESSED Raw Binary File. A plain
# `quartus_cpf -c x.sof x.rbf` produces an UNCOMPRESSED bitstream (~7 MB) that
# fails to configure the FPGA — you get no video on HDMI or analog. The correct
# output is ~2.9 MB. This script always passes `-o bitstream_compression=on`.
#
# Usage:
#   ./build_release.sh                 # convert existing .sof -> releases/<name>_<date>.rbf
#   ./build_release.sh --compile       # run the full Quartus compile first, then convert
#   ./build_release.sh --name H264_foo  # override the release base name
#   ./build_release.sh --release      # PUBLIC RELEASE build. Two things:
#                                     #  (1) HARD timing gate — refuse to pack if clk_dec
#                                     #      Fmax is below tools/fmax_check.sh's threshold
#                                     #      at either slow corner (the chroma-fringe
#                                     #      lottery; currently 101.4 MHz). Default
#                                     #      (testing) builds only WARN and tag the pack
#                                     #      MARGINAL — fringe is acceptable while testing
#                                     #      features unrelated to video quality.
#                                     #  (2) MiSTer release NAMING — packs as
#                                     #      releases/H264_YYYYMMDD.rbf (the convention every
#                                     #      other core and the MiSTer update scripts use),
#                                     #      ignoring --name. Feature builds keep the
#                                     #      <name>_<date>_<time> form so they stay
#                                     #      distinguishable on an SD card; a release is
#                                     #      the one artifact a user installs and cites in
#                                     #      a bug report, so it gets the plain name.
#
# Requires quartus_sh / quartus_cpf on PATH (Quartus 17.0.2). Or run the whole thing
# in the pinned Quartus container with USE_DOCKER=1 (see tools/docker_reexec.sh):
#   USE_DOCKER=1 ./build_release.sh --compile

set -euo pipefail

# Optional: re-exec inside the pinned Quartus 17.0.2 Docker image when USE_DOCKER=1
# (no-op otherwise; never returns when it re-execs). Must run before any quartus call.
source "$(dirname "$0")/tools/docker_reexec.sh"
maybe_reexec_in_docker "$0" "$@"

PROJECT="H264"                       # Quartus revision -> output_files/${PROJECT}.sof
NAME="H264_scaffold"                  # release base name
NAME_SET=0                          # 1 = caller passed --name explicitly
DO_COMPILE=0
RELEASE_GATE=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --compile) DO_COMPILE=1; shift ;;
        --release) RELEASE_GATE=1; shift ;;
        --name)    NAME="$2"; NAME_SET=1; shift 2 ;;
        -h|--help) sed -n '2,18p' "$0"; exit 0 ;;
        *) echo "Unknown argument: $1" >&2; exit 1 ;;
    esac
done

# The OSD version string, read from the single source of truth. It is part of
# CONF_STR = part of the synthesis netlist, so it changes at most ONCE PER BRANCH
# (see CLAUDE.md "Versioning"). Recorded in the provenance manifest below, and
# gated for shape further down.
HERE="$(cd "$(dirname "$0")" && pwd)"
CORE_VERSION="$(sed -n 's/.*`define CORE_VERSION "\([^"]*\)".*/\1/p' "$HERE/h264/emu.sv" | head -1)"
if [[ -z "$CORE_VERSION" ]]; then
    echo "ERROR: no \`define CORE_VERSION found in h264/emu.sv." >&2
    exit 1
fi

# --- Version identity gate --------------------------------------------------
# Runs BEFORE the compile so a wrong version costs a second, not 40 minutes.
# The invariant (h264/emu.sv, CLAUDE.md "Versioning"): a bare semver lives in
# exactly ONE commit per release; every other commit carries dev-<slug>.
#
# "Publishable" is --release WITHOUT --name — the same condition that selects the
# H264_YYYYMMDD.rbf naming below. tools/seed_sweep.sh passes --release WITH --name
# purely for the hard timing gate, so it lands in the dev arm and is unaffected;
# that is the one non-obvious caller.
if [[ ${#CORE_VERSION} -gt 18 ]]; then
    echo "ERROR: CORE_VERSION '$CORE_VERSION' is ${#CORE_VERSION} chars; the limit is 18." >&2
    echo "       stock menu.cpp (MENU_ABOUT2) truncates the whole 'H264 <ver> <date>'" >&2
    echo "       About line at 30 chars, and 'H264 ' + ' ' + 'yymmdd' already uses 12." >&2
    exit 1
fi
case "$CORE_VERSION" in
    *,*|*\;*|*\"*)
        echo "ERROR: CORE_VERSION '$CORE_VERSION' contains , ; or \" — all three break CONF_STR." >&2
        echo "       ';' ends the entry and user_io.cpp's V arm splits on commas, so the" >&2
        echo "       version would be silently TRUNCATED rather than rejected." >&2
        exit 1 ;;
esac

if [[ "$RELEASE_GATE" -eq 1 && "$NAME_SET" -eq 0 ]]; then
    if [[ ! "$CORE_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "ERROR: a publishable --release build needs the release semver in h264/emu.sv," >&2
        echo "       e.g. \`define CORE_VERSION \"v0.4.0\"  — found \"$CORE_VERSION\"." >&2
        echo "       Set it on the release commit, together with the mkdocs.yml and manual" >&2
        echo "       updates, so one commit and one compile carry the whole release." >&2
        exit 1
    fi
else
    if [[ ! "$CORE_VERSION" =~ ^dev-[a-z0-9][a-z0-9.-]*$ ]]; then
        echo "ERROR: dev builds must carry a feature-identifying CORE_VERSION 'dev-<slug>'," >&2
        echo "       e.g. \`define CORE_VERSION \"dev-seekrealign\"  — found \"$CORE_VERSION\"." >&2
        echo "       A bare semver on a dev build is exactly the bug this gate exists to" >&2
        echo "       stop: testers quote the OSD line and report it as a released version." >&2
        exit 1
    fi
    # Advisory only — a slug and a branch name legitimately differ, but a slug left
    # over from ANOTHER branch is a real and easy mistake.
    _br="${GIT_BRANCH:-$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '')}"
    _brslug="$(printf '%s' "${_br##*/}" | tr -cd 'a-z0-9')"
    _vslug="$(printf '%s' "${CORE_VERSION#dev-}" | tr -cd 'a-z0-9')"
    if [[ -n "$_brslug" && "$_brslug" != "main" && "$_brslug" != *"$_vslug"* && "$_vslug" != *"$_brslug"* ]]; then
        echo "NOTE: CORE_VERSION '$CORE_VERSION' does not resemble branch '$_br' — left over from another branch?" >&2
    fi
fi

# The .rbf, the zip and the OSD should all name the same thing, so a dev build's
# name FOLLOWS the version slug instead of needing --name every time. An explicit
# --name still wins.
if [[ "$NAME_SET" -eq 0 && "$RELEASE_GATE" -eq 0 ]]; then
    NAME="${PROJECT}_${CORE_VERSION#dev-}"
fi

SOF="output_files/${PROJECT}.sof"
mkdir -p releases
# Release builds take the MiSTer convention (CoreName_YYYYMMDD.rbf); feature builds keep
# the name+timestamp form so parallel experiments stay distinguishable on the SD card.
# An EXPLICIT --name always wins: tools/seed_sweep.sh passes --release purely for the hard
# timing gate and still needs its own name, and without this it (a) packed sweep artifacts
# under the release name, which reads as an official build, and (b) then failed to find
# them via `ls releases/${NAME}_*.rbf` and reported a STALE marginal rbf as the result.
if [[ "$RELEASE_GATE" -eq 1 && "$NAME_SET" -eq 0 ]]; then
    OUT="releases/${PROJECT}_$(date +%Y%m%d).rbf"
else
    OUT="releases/${NAME}_$(date +%Y%m%d_%H%M).rbf"
fi

notify() { "$(dirname "$0")/tools/notify.sh" "$1" || true; }

# Notify on failure (compile can't route / any error) so a walked-away build still
# pings. `set -e` triggers this on the first failing command; the success path sends
# its own notification below and exits 0, so this fires only for rc != 0.
# Quartus can rewrite the project file: in MiSTer_UMD_Video (2026-09-24) a quartus_map that
# died of low memory left UMD.qsf with sys/sys.tcl's assignments inlined (pins, device) and the hand-written
# comment lines -- the seed ledger -- gone, and it was committed so. The compile keeps a
# copy and puts it back if the file returns with sys.tcl inlined (a SEED write-back by
# quartus_fit alone is not an inlining and is kept).
QSF_COPY=""
qsf_guard() {
    if [[ -n "$QSF_COPY" && -f "$QSF_COPY" ]] && grep -q '^set_location_assignment' "${PROJECT}.qsf" \
       && ! grep -q '^set_location_assignment' "$QSF_COPY"; then
        echo "WARNING: Quartus rewrote ${PROJECT}.qsf (sys/sys.tcl inlined, comments lost): restored the pre-build copy" >&2
        cp "$QSF_COPY" "${PROJECT}.qsf"
    fi
}
trap 'rc=$?; qsf_guard; [[ $rc -ne 0 ]] && notify "❌ MiSTer_H264 build FAILED (${NAME}, exit $rc) — check log"; exit $rc' EXIT

if [[ "$DO_COMPILE" -eq 1 ]]; then
    echo ">> Compiling ${PROJECT} (this takes ~15-40 min)..."
    QSF_COPY=$(mktemp); cp "${PROJECT}.qsf" "$QSF_COPY"
    quartus_sh --flow compile "$PROJECT"
    qsf_guard
fi

# --- Implicit-net gate (instituted 2026-08-26, closed-captions round 3) -----
# An edit deleted a wire declaration; Verilog silently created an undriven
# implicit net, Quartus tied it to GROUND (Warning 10236) and the feature went
# dead on hardware while every testbench still passed. iverilog does not catch
# this either unless the file uses `default_nettype none. Zero tolerance: any
# 10236 in the map report fails the build. If one is ever intentional, declare
# the wire instead.
MAP_RPT="output_files/${PROJECT}.map.rpt"
if [[ -f "$MAP_RPT" ]] && grep -q "Warning (10236)" "$MAP_RPT"; then
    echo "ERROR: implicit net(s) created during synthesis — every one of these is an undeclared/undriven wire bug:" >&2
    grep "Warning (10236)" "$MAP_RPT" | sed 's/^/    /' >&2
    exit 1
fi

if [[ ! -f "$SOF" ]]; then
    echo "ERROR: $SOF not found. Run with --compile, or compile in Quartus first." >&2
    exit 1
fi

# clk_dec timing gate (tools/fmax_check.sh): the decoder compute domain must close at
# BOTH slow corners. The threshold lives in fmax_check.sh (do not quote a number here, it
# goes stale). Verify every pack; --release refuses a marginal fit, default warns + tags
# the name so a marginal rbf on a tester's card is instantly explainable.
FMAX_TAG=""
FMAX_LINE=""
VMAX_LINE=""      # (hd_raster) clk_vid, the raster's 81 MHz: gated by fmax_check.sh too
if FMAX_OUT=$("$(dirname "$0")/tools/fmax_check.sh" "output_files/${PROJECT}.sta.rpt" 2>&1); then
    echo "$FMAX_OUT"
    FMAX_LINE=$(echo "$FMAX_OUT" | grep -m1 'clk_dec Restricted Fmax' || true)
    VMAX_LINE=$(echo "$FMAX_OUT" | grep -m1 'clk_vid Restricted Fmax' || true)
else
    rc=$?
    echo "$FMAX_OUT"
    FMAX_LINE=$(echo "$FMAX_OUT" | grep -m1 'clk_dec Restricted Fmax' || true)
    VMAX_LINE=$(echo "$FMAX_OUT" | grep -m1 'clk_vid Restricted Fmax' || true)
    if [[ "$RELEASE_GATE" -eq 1 ]]; then
        echo "ERROR: --release build refused: clk_dec timing gate failed (rc=$rc). Re-sweep seeds (tools/seed_sweep.sh) or retime." >&2
        notify "❌ MiSTer_H264 RELEASE build REFUSED — ${FMAX_LINE:-no STA data} (marginal fit)"
        trap - EXIT
        exit 1
    fi
    echo "WARNING: packing a TIMING-MARGINAL build (testing mode) — timing failures on HW are the first suspect." >&2
    FMAX_TAG="_MARGINAL"
    OUT="releases/${NAME}${FMAX_TAG}_$(date +%Y%m%d_%H%M).rbf"
fi

# Undriven-signal gate (tools/lint_undriven.sh): a declared, consumed signal with no
# driver. Quartus ties it low and everything downstream keeps working, so it survives
# synthesis, fit, and every module bench that drives the input directly. Two shipped
# in one feature (dec_pts_in's missing CDC, frame_late's deleted always block) and
# both survived two HW rounds. Runs BEFORE the compile so it costs seconds, not 40
# minutes. Advisory on a dev build, fatal on --release.
if LINT_OUT=$("$(dirname "$0")/tools/lint_undriven.sh" 2>&1); then
    echo "$LINT_OUT"
else
    echo "$LINT_OUT" >&2
    if [[ "$RELEASE_GATE" -eq 1 ]]; then
        echo "ERROR: --release build refused: an undriven signal means a dead data path." >&2
        trap - EXIT
        exit 1
    fi
    echo "WARNING: packing a build with an UNDRIVEN signal (see above)." >&2
fi

# Netlist canary (tools/netlist_canary.sh): a wide data register that Quartus
# constant-folded is a data path that is DEAD in silicon and healthy in every
# simulation. This gate exists because dec_pts_in/dec_pts_in_valid were connected
# but never DRIVEN -- no missing declaration, so no implicit-net warning; no
# emu-level bench, so nothing in sim; and the telemetry read clean because it was
# derived from the same clock the dead path was supposed to steer. The netlist
# said it in one line. Advisory on a dev build, fatal on --release.
if CANARY_OUT=$("$(dirname "$0")/tools/netlist_canary.sh" "output_files/${PROJECT}.map.rpt" 2>&1); then
    echo "$CANARY_OUT"
else
    echo "$CANARY_OUT" >&2
    if [[ "$RELEASE_GATE" -eq 1 ]]; then
        echo "ERROR: --release build refused: a load-bearing data path is constant in the netlist." >&2
        trap - EXIT
        exit 1
    fi
    echo "WARNING: packing a build with a DEAD data path (see above) — it will look correct in sim." >&2
fi

# HDMI clock gate (tools/hdmi_pll_check.sh): pll_hdmi's output on the physical counter Main
# reprograms. A fit with it elsewhere closes timing and shows 1080p, and sends every other HDMI
# mode at the wrong pixel clock (720x480 at 135 MHz: a black TV) -- three SEED 2 builds shipped
# to the rig so (docs/history.md S47). sys/sys.tcl pins it; this proves the pin held.
# Advisory on a dev build, fatal on --release.
if HPLL_OUT=$("$(dirname "$0")/tools/hdmi_pll_check.sh" "output_files/${PROJECT}.fit.rpt" 2>&1); then
    echo "$HPLL_OUT"
else
    echo "$HPLL_OUT" >&2
    if [[ "$RELEASE_GATE" -eq 1 ]]; then
        echo "ERROR: --release build refused: the HDMI pixel clock would be wrong in every mode but 1080p." >&2
        trap - EXIT
        exit 1
    fi
    echo "WARNING: packing a build whose HDMI clock is WRONG below 1080p (see above)." >&2
fi

echo ">> Packing compressed .rbf -> ${OUT}"
quartus_cpf -c -o bitstream_compression=on "$SOF" "$OUT"

SIZE=$(stat -c%s "$OUT")
echo ">> Done: ${OUT} ($((SIZE/1024)) KB)"
# A compressed pack of this device is ~3-4.5 MB depending on how much BRAM is
# initialised. An UNCOMPRESSED pack is ~7 MB and silently fails to configure the FPGA
# (the core "loads" with no video on any output) -- flag anything past 4.5 MB so it is
# not discovered as a dead screen on hardware.
if [[ "$SIZE" -gt 4500000 ]]; then
    echo "WARNING: .rbf is larger than expected (> 4.5 MB). Compression may not have applied." >&2
fi

# --- Build provenance manifest ---------------------------------------------
# One JSON sidecar per .rbf, "<rbf>.json". releases/ and build_id.v are BOTH
# git-ignored, so without this an .rbf found on a tester's SD card months from
# now is unidentifiable — the filename is its only metadata. Three consumers:
#   * .github/workflows/package.yml validates the uploaded release asset against
#     it, and learns WHICH COMMIT to check out (a draft release has no git tag).
#   * a mystery build: which tree, which seed, what timing.
#   * H264.qsf's hand-written seed ledger — its entries quote exactly these
#     numbers, so this is machine-readable input for that paragraph.
# Everything here comes from files this script has already produced; nothing is
# recomputed and there is no python3 dependency inside the Quartus container.
MANIFEST="${OUT}.json"
FIT_SUMMARY="output_files/${PROJECT}.fit.summary"
# `Key : value` lookup in the fitter summary. Keys contain ( ) which are literal in BRE.
fitval() { sed -n "s/^$1 : *//p" "$FIT_SUMMARY" 2>/dev/null | head -1 | sed 's/[[:space:]]*$//' || true; }
# The pinned fitter seed. quartus_fit --seed=N writes this assignment back into the
# qsf, so the LAST one is what actually built this netlist -- on a compile. A pack of
# tools/seed_sweep.sh's winner runs after the sweep restored the qsf (its old SEED line),
# so the sweep passes the seed that built the .sof as BUILD_SEED (from UMD P6b7: the
# manifests of swept builds recorded the qsf's seed, not the fit's; docs/harvest.md).
SEED_V=$(sed -n 's/^set_global_assignment -name SEED  *//p' "$HERE/${PROJECT}.qsf" 2>/dev/null | tail -1 || true)
[ -n "${BUILD_SEED:-}" ] && SEED_V=$BUILD_SEED
# From "clk_dec Restricted Fmax: 94.06 MHz @100C, 93.11 MHz @-40C (target 86.0)".
F100=$(printf '%s' "$FMAX_LINE" | sed -n 's/.*Fmax: *\([0-9.]*\) MHz @100C.*/\1/p')
FM40=$(printf '%s' "$FMAX_LINE" | sed -n 's/.*, *\([0-9.]*\) MHz @-40C.*/\1/p')
FTGT=$(printf '%s' "$FMAX_LINE" | sed -n 's/.*(target *\([0-9.]*\)).*/\1/p')
V100=$(printf '%s' "$VMAX_LINE" | sed -n 's/.*Fmax: *\([0-9.]*\) MHz @100C.*/\1/p')
VM40=$(printf '%s' "$VMAX_LINE" | sed -n 's/.*, *\([0-9.]*\) MHz @-40C.*/\1/p')
VTGT=$(printf '%s' "$VMAX_LINE" | sed -n 's/.*(target *\([0-9.]*\)).*/\1/p')
BDATE=$(sed -n 's/.*`define BUILD_DATE "\([^"]*\)".*/\1/p' "$HERE/build_id.v" 2>/dev/null || true)
RBF_SHA=$(sha256sum "$OUT" 2>/dev/null | cut -d' ' -f1 || true)
# GIT_* normally arrive from tools/docker_reexec.sh, which resolves them on the
# host. This fallback covers a direct (non-Docker) run.
G_BRANCH="${GIT_BRANCH:-$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)}"
G_SHA="${GIT_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
G_DIRTY="${GIT_DIRTY:-$([ -n "$(git -C "$HERE" status --porcelain --untracked-files=no 2>/dev/null)" ] && echo true || echo false)}"
[[ "$G_DIRTY" == "true" || "$G_DIRTY" == "false" ]] || G_DIRTY=null

cat > "$MANIFEST" <<EOF
{
  "schema": 1,
  "core_version": "${CORE_VERSION}",
  "build_date": "${BDATE}",
  "built_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "rbf": {
    "name": "$(basename "$OUT")",
    "bytes": ${SIZE},
    "sha256": "${RBF_SHA}",
    "marginal": $([ -n "$FMAX_TAG" ] && echo true || echo false)
  },
  "git": {
    "branch": "${G_BRANCH}",
    "sha": "${G_SHA}",
    "dirty": ${G_DIRTY}
  },
  "fit": {
    "seed": ${SEED_V:-null},
    "alm": "$(fitval 'Logic utilization (in ALMs)')",
    "registers": "$(fitval 'Total registers')",
    "ram_blocks": "$(fitval 'Total RAM Blocks')",
    "dsp_blocks": "$(fitval 'Total DSP Blocks')"
  },
  "timing": {
    "clk_dec_100c_mhz": ${F100:-null},
    "clk_dec_m40c_mhz": ${FM40:-null},
    "threshold_mhz": ${FTGT:-null},
    "clk_vid_100c_mhz": ${V100:-null},
    "clk_vid_m40c_mhz": ${VM40:-null},
    "clk_vid_threshold_mhz": ${VTGT:-null},
    "pass": $([ -z "$FMAX_TAG" ] && echo true || echo false)
  },
  "toolchain": {
    "quartus": "$(fitval 'Quartus Prime Version')",
    "image": "${QUARTUS_DOCKER_IMAGE:-raetro/quartus:mister}"
  }
}
EOF
echo ">> Manifest: ${MANIFEST}"

# Phone notification (no-op if tools/.notify_url / $NOTIFY_URL unset; NOTIFY_SILENT=1
# suppresses it, e.g. when seed_sweep.sh sends its own summary instead).
if [[ -n "$FMAX_TAG" ]]; then
    notify "⚠️ MiSTer_H264 packed MARGINAL $(basename "$OUT") ($((SIZE/1024)) KB) — ${FMAX_LINE:-no STA data}"
else
    notify "✅ MiSTer_H264 packed $(basename "$OUT") ($((SIZE/1024)) KB) — ${FMAX_LINE:-no STA data}"
fi
