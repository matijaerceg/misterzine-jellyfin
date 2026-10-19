# Contributing

See [Build from source](docs/BUILD.md) for application and worker setup, and
[distribution](release/DISTRIBUTION.md) for official beta builds and packaging.
Development builds remain ungated; release builds select their channel explicitly.

Report bugs with the app revision, MiSTer hardware, output cable/profile,
connection type, direct-stream or transcode mode, and reproduction steps.
Review diagnostics before sharing them; do not attach account files or card images.

Keep changes focused and retain the applicable
[component licenses](release/THIRD_PARTY_NOTICES.md) and
[application terms](release/TERMS.md). No additional license grant
is implied by source availability.

On Linux with Go 1.27 and Python 3:

```sh
cd app
go test ./...
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go vet ./...
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -o ../build/arm/plexcrt ./cmd/plexcrt
cd ..
python3 arm/test_jfplay_contract.py
make -C arm audio-boost-test seek-cache-test seek-protocol-test h264-idr-test direct-check-test
make -C arm film-speed-test
python3 tools/check_public_content.py
```

Run the standalone geometry test from the repository root as well; it compares
the C helper with the shared fit fixtures and checks calibrated crop/fit,
480/576-line alignment, and invalid inputs:

```sh
cc -std=c99 -Wall -Wextra -Werror -Iarm arm/test_video_geometry.c -o /tmp/test_video_geometry
/tmp/test_video_geometry
```

Playback calibration is capability-gated on HPS interface 1.2. Older cores
retain the preview-only calibration screen. A custom interface 1.2 core build
is required to apply saved geometry. Fit/crop telemetry has been verified on
Mister Pi with DeFlicker off and on; visual CRT confirmation remains pending.

The core's display controls and `jfplay` film-speed selection use HPS interface
1.3; interface 1.4 exposes the selected Off/50-60 refresh-match mode in status
word 87. The application accepts later 1.x interfaces, and the HPS library
checks the required version separately for each newer API call. Validate
source/output clock conversion with `make -C arm film-speed-test`. RTL timing
and mode-transition benches provide software coverage for 240p and PAL
switching; these checks do not replace hardware testing. On the target, verify
the selected mode, refresh switching, pause/resume, A/V sync, and the analog
picture. Film Speed intentionally raises pitch; do not describe it as
pitch-preserving.

Run the standalone raster bench from the vendored core directory:

```sh
cd third_party/MiSTer_h264_decoder
iverilog -g2012 -I. -o /tmp/sync_gen_240_tb h264/sync_gen.sv bench/h264/sync_gen_240_tb.sv
vvp /tmp/sync_gen_240_tb
```

Hardware-dependent changes also need a MiSTer test. Verify direct streaming and
transcoding, continuous audio/video, pause/resume, repeated seeking, stop/restart,
and Jellyfin's saved position. Confirm that only one `jfplay` runs during playback
and none remain after stop. Test crop on an analog display when changing it.

Playback can update watched state. Keep raw test journals, credentials, deployment
logs, and session notes in a private directory outside Git. Public docs should
describe reproducible behavior using parameters and synthetic examples.
