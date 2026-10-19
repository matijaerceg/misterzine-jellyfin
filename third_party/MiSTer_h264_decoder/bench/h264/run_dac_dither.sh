#!/usr/bin/env bash
# run_dac_dither.sh -- h264/dac_dither.sv, the ordered dither to the analog DAC's 6 bits (docs/analog.md
# S12), against bench/h264/dac_dither_tb.sv (Icarus). GREEN: every arm; --red: each mutation, on a COPY
# compiled from inside it, must be caught by its own arm:
#   T1  the thresholds 0..1 only (a dither too weak to average)     -> [mean]
#   T2  the row never advances (one row of thresholds on every line:
#       each line still exact, the columns not)                     -> [cols]
#   T3  the field's inversion removed                               -> [field]
#   T4  no saturation (v + t wraps past 255)                        -> [sat]
#   T5  dithered in the blanking too                                -> [blank]
#   T6  en ignored                                                  -> [off]
#   T7  one channel's threshold one too high (up to 4)               -> [mean]
#   T8  the first matrix, a 4 x 4 Bayer's top two bits (= 2 x 2 {0 2; 3 1} tiled: exact over the
#       field, alternate lines half a DAC step apart)               -> [lines]
#   T9  the same ramp 3 2 1 0 on every line (exact lines, a column's offset the same on every line:
#       vertical stripes)                                           -> [cols]
set -u
cd "$(dirname "$0")/../.."
ROOT=$(pwd)
W=.sim/dac_dither
mkdir -p $W
red=0
for a in "$@"; do case $a in --red) red=1;; esac; done
RTL="h264/dac_dither.sv"
TB="bench/h264/dac_dither_tb.sv"
build() {   # src-root out -> 0 on success (deletes the old sim first: iverilog leaves it on a failure)
  rm -f "$2"
  (cd "$1" && iverilog -g2012 -o "$2" $RTL "$ROOT/$TB") > "$2.log" 2>&1 && [ -f "$2" ]
}
fail=0
echo "== GREEN =="
if build "$ROOT" "$ROOT/$W/sim" && vvp -n "$W/sim" > "$W/green.log" 2>&1 && grep -q "^PASS: dac_dither_tb" "$W/green.log"; then
  echo "  PASS: $(grep -m1 '^dac_dither_tb:' "$W/green.log" | sed 's/^dac_dither_tb: //')"
else
  echo "  FAIL"; grep -m3 "FAIL\|rror" "$W/green.log" "$W/sim.log" 2>/dev/null | sed 's/^/    /'; fail=1
fi
if [ $red = 1 ]; then
  echo "== MUTATIONS (each must be caught by its own arm) =="
  mut() {   # name old new arm
    local name=$1 old=$2 new=$3 arm=$4 d
    d=$(mktemp -d)
    mkdir -p "$d/h264"; cp $RTL "$d/h264/"
    python3 - "$d/h264/dac_dither.sv" "$old" "$new" <<'PYEOF' || { echo "  $name: anchor not unique/found"; fail=1; rm -rf "$d"; return; }
import sys
p, old, new = sys.argv[1:4]
s = open(p).read()
assert s.count(old) == 1
open(p, "w").write(s.replace(old, new))
PYEOF
    if ! build "$d" "$d/sim"; then echo "  $name: build failed"; fail=1; rm -rf "$d"; return; fi
    vvp -n "$d/sim" > "$d/log" 2>&1
    if grep -q "^PASS:" "$d/log"; then echo "  $name: SURVIVED -- the bench cannot see this defect"; fail=1
    elif grep -Eq "FAIL \[($arm)\]" "$d/log"; then echo "  $name: caught by $(grep -Em1 "FAIL \[($arm)\]" "$d/log" | cut -c1-110)"
    else echo "  $name: failed but NOT in [$arm]: $(grep -m1 FAIL "$d/log")"; fail=1; fi
    rm -rf "$d"
  }
  mut T1 "wire [1:0] t = fld ? (2'd3 - t0) : t0;" "wire [1:0] t = {1'b0, fld ? !t0[1] : t0[1]};" mean
  mut T2 "if (hs && !hs_q) row <= row + 2'd1;" "if (1'b0) row <= row + 2'd1;" cols
  mut T3 "wire [1:0] t = fld ? (2'd3 - t0) : t0;" "wire [1:0] t = t0;" field
  mut T4 "s2[8] ? 8'hFF : s2[7:0], s1[8] ? 8'hFF : s1[7:0], s0[8] ? 8'hFF : s0[7:0]" "s2[7:0], s1[7:0], s0[7:0]" sat
  mut T5 "if (!en_s2 || !de) dout = din;" "if (!en_s2) dout = din;" blank
  mut T6 "if (!en_s2 || !de) dout = din;" "if (!de) dout = din;" off
  mut T7 "s2 = {1'b0, din[23:16]} + {7'd0, t};" "s2 = {1'b0, din[23:16]} + {7'd0, t} + 9'd1;" mean
  # the matrix rewritten: its four case lines (rows = lines, top to bottom; columns = clocks)
  matrix() {   # 16 thresholds -> the case body, in the RTL's layout
    local i=0 out="" r c sel
    for r in 0 1 2 3; do
      out+="           "
      for c in 0 1 2 3; do
        sel=$(printf "4'h%X" $((4 * r + c))); [ $r$c = 33 ] && sel=default
        out+=" $sel: t0 = 2'd${@:$((i + 1)):1};"; i=$((i + 1))
      done
      out+=$'\n'
    done
    printf '%s' "$out"
  }
  LATIN=$(matrix 3 0 2 1  2 1 3 0  0 3 1 2  1 2 0 3)
  mut T8 "$LATIN" "$(matrix 0 2 0 2  3 1 3 1  0 2 0 2  3 1 3 1)" lines
  mut T9 "$LATIN" "$(matrix 3 2 1 0  3 2 1 0  3 2 1 0  3 2 1 0)" cols
fi
[ $fail -eq 0 ] && echo "== ALL GREEN ==" || echo "== FAILURES =="
exit $fail
