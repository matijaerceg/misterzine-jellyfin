// h264/dac_dither.sv -- an ordered dither for the analog DAC (2026-10-06, docs/analog.md §12).
//
// The MiSTer I/O board's VGA DAC takes the top 6 bits of each 8-bit channel (sys_top: VGA_R =
// vga_o[23:18]): 64 steps from black to white, and film's smooth gradients contour on the analog
// output -- measured on a CRT over S-Video: a Y 16 -> 64 ramp showed 14 bands (RGB 0 -> 56, / 4). This
// adds a 4 x 4 threshold t of 0..3 LSB to every channel before the truncation. The matrix is a Latin
// square (every line and every column holds 0..3 once), so by Hermite's identity, sum over t = 0..3 of
// floor((v + t) / 4) = v, the DAC's output averages to the 8-bit value exactly (v <= 252) over every 4
// clocks of a line and every 4 lines of a column. The per-LINE mean is the point: the pattern is far
// above the video bandwidth along the line, so a line is seen as its mean. (The first version took the
// top two bits of a 4 x 4 Bayer matrix, which reduce to a 2 x 2 {0 2; 3 1}: exact over 4 x 4 but not per
// line, so v % 4 = 1 or 3 showed alternate lines half a DAC step apart -- a line texture. Found in
// MiSTer_DVD's copy, its PR #164; docs/analog.md S12 "The matrix".)
//
//   the place    the core-raster VGA path's last 24-bit word (vga_o: RGB, YPbPr or S-Video's Y / C), an
//                H264-FORK edit in sys/sys_top.v -- HDMI never sees it
//   the pattern  keyed on CLOCKS (clk_vid, 81 MHz: a pixel is held 3 or 6 of them), not pixels -- column
//                (the clock in the line) mod 4, row (the line) mod 4: horizontally ~20 MHz, far above
//                the video bandwidth, so the DAC and the TV low-pass it into the missing two bits; every
//                other field (frame) the thresholds are inverted (3 - t): two fields average flat
//   only DE      blanking, sync levels and S-Video's burst are untouched; a value that is a multiple of
//                4 is unchanged at 6 bits whatever the threshold (black 0, YPbPr's 128 and the rest)
//   saturating   v + t is clamped at 255
//   8-bit DACs   (boards taking the low bits on SDIO) see v + 0..3: a 1.5 LSB mean, invisible -- but a
//                SuperStation's analog output (24-bit) shows no banding at all, measured: so the OSD's
//                Analog Dither (emu's VGA_DITHER, status[19]) turns it on, Off by default
//
// No `function`, no N'(expr) cast (Quartus 17).

`default_nettype none

module dac_dither (
    input  wire        clk,
    input  wire        en,                 // 0: din passes through, bit-exact (a level from clk_sys: synchronised here)
    input  wire        hs,                 // the word's own syncs and DE (vga_out / yc_out's outputs)
    input  wire        vs,
    input  wire        de,
    input  wire [23:0] din,
    output logic [23:0] dout
);

    // no reset (sys_top's VGA path has none): power-up values, as the framework's own registers
    logic [1:0] col = 2'd0, row = 2'd0;
    logic       fld = 1'b0, hs_q = 1'b0, vs_q = 1'b0;
    logic       en_s1 = 1'b0, en_s2 = 1'b0;     // the OSD's level (h264.sdc: a false path into en_s1)
    always_ff @(posedge clk) begin
        en_s1 <= en; en_s2 <= en_s1;
        hs_q <= hs; vs_q <= vs;
        col <= de ? col + 2'd1 : 2'd0;
        if (hs && !hs_q) row <= row + 2'd1;
        if (vs && !vs_q) begin row <= 2'd0; fld <= !fld; end
    end

    // the threshold matrix, a 4 x 4 Latin square (rows the lines, columns the clocks): every line and
    // every column holds 0..3 once, so each averages to v / 4 on its own, whatever its alignment against
    // DE and vs. MiSTer_DVD's matrix, taken as it is; its choice among the Latin squares (high and low
    // thresholds alternating clock by clock, the 3s stepping between pixel halves) is its 27 MHz case,
    // two clocks a 480i pixel -- here a pixel is 3 or 6 clocks and the pattern ~20 MHz, so any Latin
    // square would do. 3 - t of a Latin square is one too, so the field's inversion keeps it.
    logic [1:0] t0;
    always_comb
        case ({row, col})
            4'h0: t0 = 2'd3; 4'h1: t0 = 2'd0; 4'h2: t0 = 2'd2; 4'h3: t0 = 2'd1;
            4'h4: t0 = 2'd2; 4'h5: t0 = 2'd1; 4'h6: t0 = 2'd3; 4'h7: t0 = 2'd0;
            4'h8: t0 = 2'd0; 4'h9: t0 = 2'd3; 4'hA: t0 = 2'd1; 4'hB: t0 = 2'd2;
            4'hC: t0 = 2'd1; 4'hD: t0 = 2'd2; 4'hE: t0 = 2'd0; default: t0 = 2'd3;
        endcase
    wire [1:0] t = fld ? (2'd3 - t0) : t0;

    logic [8:0] s2, s1, s0;
    always_comb begin
        s2 = {1'b0, din[23:16]} + {7'd0, t};
        s1 = {1'b0, din[15:8]}  + {7'd0, t};
        s0 = {1'b0, din[7:0]}   + {7'd0, t};
    end
    always_comb begin
        if (!en_s2 || !de) dout = din;
        else dout = {s2[8] ? 8'hFF : s2[7:0], s1[8] ? 8'hFF : s1[7:0], s0[8] ? 8'hFF : s0[7:0]};
    end

endmodule

`default_nettype wire
