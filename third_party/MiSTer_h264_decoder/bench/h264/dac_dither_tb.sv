// bench/h264/dac_dither_tb.sv -- h264/dac_dither.sv (docs/analog.md S12): the ordered dither to the
// I/O board's 6-bit DAC. A small raster (4 active clocks a line, 4 lines a field, 2 fields), every
// 8-bit level on all three channels at once. Arms:
//   [mean]   v <= 252: over a field's 16 positions the 6-bit codes floor(out / 4) sum to 4 v -- the
//            DAC's average is the 8-bit value exactly (the point of it: an external fact, not the RTL's
//            table -- for ANY 16 thresholds holding 0..3 four times each)
//   [lines]  v <= 252: every line's 4 codes sum to v -- each line's own mean exact (a line seen as its
//            mean, the pattern being far above the video bandwidth along it; the first matrix, Bayer's
//            top bits, put alternate lines half a DAC step apart at v % 4 = 1 or 3: a line texture)
//   [cols]   v <= 252: every column's 4 codes (the field's 4 lines) sum to v -- no column off (the same
//            ramp on every line is vertical stripes)
//            ([mean], [lines] and [cols] are scored in that order at the field's end, so a defect is
//            caught by the first property it breaks)
//   [mult4]  v a multiple of 4: every 6-bit code is v / 4 (black, YPbPr's 128, ...: unchanged)
//   [sat]    v > 252: out never below v, never above 255 (no wrap)
//   [blank]  DE low: out == in, bit-exact (sync levels, the burst)
//   [field]  the second field's thresholds are 3 - the first's at every position (two fields average flat)
//   [off]    en low: out == in during DE
// PASS marker: "PASS: dac_dither_tb". Every failure is $fatal.
`timescale 1ns/1ps
`default_nettype none
module dac_dither_tb;

`define FAIL(arm, msg) begin $display("FAIL [%0s] %0s", arm, msg); $fatal(1); end

reg clk = 0;
always #5 clk = ~clk;
reg en = 1, hs = 0, vs = 0, de = 0;
reg [23:0] din = 0;
wire [23:0] dout;
dac_dither u (.clk(clk), .en(en), .hs(hs), .vs(vs), .de(de), .din(din), .dout(dout));

integer t0 [0:15], t1 [0:15];          // the thresholds seen (out - in), per position, fields 0 / 1
integer code [0:2][0:15];              // the 6-bit codes, per channel and position (line * 4 + clock)
integer v, f, ln, c, ch, o, n_pos, s6;
reg [7:0] ob;

// one line: hs pulse, then 4 DE clocks sampled (the dither is combinational on din: sample before the edge)
task automatic line(input integer fld, input integer row, input integer val, input integer score);
    integer cc, k;
    begin
        @(negedge clk) hs = 1; @(negedge clk) hs = 0;
        repeat (2) @(negedge clk);
        for (cc = 0; cc < 4; cc++) begin
            de = 1; din = {val[7:0], val[7:0], val[7:0]};
            #1;
            for (k = 0; k < 3; k++) begin
                ob = dout[8*k +: 8];
                if (score) begin
                    if (val > 252) begin
                        if (ob < val || ob > 255) `FAIL("sat", $sformatf("v %0d: out %0d", val, ob))
                    end else begin
                        if (ob < val || ob > val + 3) `FAIL("mean", $sformatf("v %0d: out %0d (a threshold outside 0..3)", val, ob))
                        if (val % 4 == 0 && (ob >> 2) !== (val >> 2)) `FAIL("mult4", $sformatf("v %0d: the 6-bit code %0d", val, ob >> 2))
                    end
                    code[k][row * 4 + cc] = ob >> 2;
                    if (k == 0) begin
                        if (fld == 0) t0[row * 4 + cc] = ob - val; else t1[row * 4 + cc] = ob - val;
                    end
                end
            end
            @(negedge clk);
        end
        de = 0;
        // [blank]: outside DE the word passes
        din = 24'hA5C3_7E ^ {val[7:0], val[7:0], val[7:0]};
        #1;
        if (dout !== din) `FAIL("blank", $sformatf("v %0d: out %06h in %06h with DE low", val, dout, din))
        repeat (2) @(negedge clk);
    end
endtask

task automatic field_start;
    begin
        @(negedge clk) vs = 1; @(negedge clk) vs = 0;
    end
endtask

initial begin
    repeat (4) @(negedge clk);
    // align: a field start, then fields of 4 lines (the first lines' rows are 1..4 = 1,2,3,0 after the vs)
    for (v = 0; v < 256; v++) begin
        for (f = 0; f < 2; f++) begin
            field_start();
            for (ln = 0; ln < 4; ln++) line(f, ln, v, 1);
            if (v <= 252) begin
                for (ch = 0; ch < 3; ch++) begin
                    s6 = 0;
                    for (o = 0; o < 16; o++) s6 = s6 + code[ch][o];
                    if (s6 !== 4 * v)
                        `FAIL("mean", $sformatf("v %0d field %0d ch %0d: the 6-bit codes sum to %0d over the 16 positions, want %0d", v, f, ch, s6, 4 * v))
                end
                for (ch = 0; ch < 3; ch++)
                    for (ln = 0; ln < 4; ln++)
                        if (code[ch][ln * 4] + code[ch][ln * 4 + 1] + code[ch][ln * 4 + 2] + code[ch][ln * 4 + 3] !== v)
                            `FAIL("lines", $sformatf("v %0d field %0d ch %0d line %0d: codes %0d %0d %0d %0d, want a sum of %0d", v, f, ch, ln, code[ch][ln * 4], code[ch][ln * 4 + 1], code[ch][ln * 4 + 2], code[ch][ln * 4 + 3], v))
                for (ch = 0; ch < 3; ch++)
                    for (c = 0; c < 4; c++)
                        if (code[ch][c] + code[ch][4 + c] + code[ch][8 + c] + code[ch][12 + c] !== v)
                            `FAIL("cols", $sformatf("v %0d field %0d ch %0d column %0d: codes %0d %0d %0d %0d, want a sum of %0d", v, f, ch, c, code[ch][c], code[ch][4 + c], code[ch][8 + c], code[ch][12 + c], v))
            end
        end
        if (v == 100) begin
            // [field] at a level away from the edges: field 1's threshold = 3 - field 0's, every position
            // (the two fields are consecutive here: f 0 then f 1 at the same rows and columns)
            for (o = 0; o < 16; o++)
                if (t0[o] + t1[o] !== 3) `FAIL("field", $sformatf("position %0d: thresholds %0d then %0d", o, t0[o], t1[o]))
        end
    end
    // [off]
    en = 0;
    field_start();
    for (ln = 0; ln < 4; ln++) begin
        @(negedge clk) hs = 1; @(negedge clk) hs = 0;
        for (c = 0; c < 4; c++) begin
            de = 1; din = 24'h123456 + c + 4 * ln; #1;
            if (dout !== din) `FAIL("off", $sformatf("en 0: out %06h in %06h", dout, din))
            @(negedge clk);
        end
        de = 0;
    end
    $display("dac_dither_tb: every level's 6-bit average exact (0..252) over the field, every line and every column, multiples of 4 unchanged, saturating above, blanking and the bypass bit-exact, the fields' thresholds complementary");
    $display("PASS: dac_dither_tb");
    $finish;
end

endmodule
`default_nettype wire
