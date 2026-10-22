# Build from source

This fork uses the third-party H.264 FPGA decoder in
`third_party/MiSTer_h264_decoder`. The Go app and C playback worker build on Linux
(or WSL) for MiSTer's ARMv7 processor.

## Dependencies and playback worker

Install the prerequisites described in [the toolchain guide](../toolchain/README.md).
From the repository root, build the HPS ring library, FFmpeg/OpenSSL libraries,
and the worker:

```sh
bash toolchain/build_hps.sh
bash toolchain/build_ffmpeg.sh
make -C arm jfplay
python3 arm/test_jfplay_contract.py
```

The outputs are `arm/jfplay`, `build/arm/ffmpeg`, and the HPS tools and libraries
under `build/arm/`. The worker is statically linked against the libraries built
by these scripts. Consult the [component notices](../release/THIRD_PARTY_NOTICES.md)
before distributing binaries.

Saved video geometry is sent only to a core advertising HPS interface 1.2 or
later. The core shipped with the app (interface 1.4) applies saved geometry;
older H.264 cores keep calibration preview-only. Fit/crop telemetry has been
verified on Mister Pi with DeFlicker off and on; visual CRT confirmation
remains pending.

The app's 14:9 crop uses the same custom geometry interface. On older cores it
falls back to Off, while the core receives its valid Letterbox setting.

The core OSD's **Output Mode**, **Refresh Rate Match**, **Display Shape**,
**De-flicker**, **240p Playback**, and **Play 24fps at 25fps** controls use
HPS interface 1.3. Interface 1.4 exposes the selected refresh-match mode in
status word 87 bits 16–17; supported choices are Off and 50/60. Interface 1.3
uses its existing Off and 50/60 status bit. Ordinary playback remains
available with older cores.

The display options live in the core OSD, not the Jellyfin app. Output Mode
selects 480i60, 480p60, 576i50, 576p50, or Native HDMI. Display Shape (4:3 or
16:9) shapes every fitted raster and the aspect HDMI is told; in Native HDMI a
playing video takes the HDMI mode's own shape. Refresh Rate Match
offers Off and 50/60. Matching can switch the refresh family while a picture
plays, then restore the selected Output Mode when playback ends. Native HDMI
and the progressive Output Mode choices remain selectable. 240p Playback otherwise
changes playback on the interlaced profiles to progressive 240p or 288p; menus
and idle screens keep their normal mode.

Film Speed can speed eligible 23.976/24 fps video to 25 fps only on fixed-50-Hz
576i/576p profiles when refresh matching is Off. It shortens runtime and raises
audio pitch because pitch preservation is not implemented; a 25 fps source is
never sped up again. Exact 24/30/60 fps sources are slowed by 1000/1001 when
the output uses the NTSC family: on fitted 480-line profiles, or when refresh
matching selects NTSC rates. Audio is resampled with the same clock conversion.
Fractional NTSC sources are unchanged, and Native HDMI does not receive the
fitted 480-line slowdown. The worker selects its clock when it opens a stream,
based on the core setting, refresh mode, output family, and source frame rate.
Stop and restart playback after changing a setting that changes the playback
clock. Progress and seek targets remain in source seconds.

240p and 288p use constant line lengths, at approximately 59.914 and 49.993 Hz.
Their scheduling grid follows those actual frame periods. The small difference
from nominal cadence can still require occasional frame corrections; these
modes do not use a variable-line-length approximation of exact refresh.

These display modes remain experimental. Review the validation instructions
below and in [Contributing](../CONTRIBUTING.md) before using a custom build.

## Application

With Go 1.27, from the repository root:

```sh
cd app
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -trimpath -o ../build/arm/plexcrt ./cmd/plexcrt
cd ..
```

The executable retains its historical `plexcrt` name. It runs on MiSTer Linux;
it is not a desktop media player. Unit tests can run on a Linux desktop.

The font atlases in `app/internal/gfx/fonts` are committed and embedded in the
build. `app/tools/genatlas.ps1` generates them on Windows from the installed
Roboto and Roboto Medium fonts
(`powershell -File app\tools\genatlas.ps1`); run it only to change a face or
size, and keep every atlas the app loads by name.

## Official beta builds

Official supporter builds combine this source with a private companion
repository. The beta gate, code registry and beta builder live there. See
[Public source and supporter builds](SUPPORTER_BUILDS.md).

For a standalone public ARM build with packaging metadata:

```sh
python3 release/build_app.py --channel public --version 0.1.0 --id local-public
```

Source builds remain code-free. Public packaging and the installer are described
in [distribution](../release/DISTRIBUTION.md).

## Development install

Use a separate directory on the MiSTer's SD card. Copy `plexcrt`, `jfplay`, and
`ffmpeg` into it and make them executable. `ffmpeg` decodes show theme music, so
the preflight requires it alongside the app.

Install an H.264 core built from this repository's fork in
`third_party/MiSTer_h264_decoder` (see [distribution](../release/DISTRIBUTION.md))
in the SD card's `_Other` folder as `H264.rbf`. See its [launch instructions](../third_party/MiSTer_h264_decoder/site/content/integrating/launch.md).
By default the app loads the `_Other/H264` core through an MGL. An optional
`MISTERZINE_H264_RBF` override is an MGL core name such as `_Other/H264`, relative
to the SD root and without the `.rbf` extension.

Run `./plexcrt -check` on the device to check the application files, then
`./plexcrt` to load the core and sign in on screen. Configuration and cached
artwork are stored alongside the executable. Keep account files private.

Switching to another core stops the app and playback worker. Their shared-memory
mappings outlive FPGA reconfiguration, so they check MiSTer's core name and the
raster heartbeat before writing. After ownership is lost, shutdown releases
process resources without resetting the decoder or repainting the framebuffer.
These checks reduce the changeover window; they are not an atomic handoff with
MiSTer Main. Test switches from both browsing and active or paused playback,
and verify that neither process remains in the next core.

Official supporter packages are built with the private companion repository.

## Validation

See [Contributing](../CONTRIBUTING.md) for the Linux unit tests, ARM build checks,
and public-content scan. Playback changes also need real hardware tests covering
both direct streams and transcodes, pause/resume, repeated seeks, stop, saved
resume position, and the absence of leftover player processes.

The worker's C helpers, the pure-C video geometry helper among them, are
tested on a native Linux or WSL host without the ARM toolchain or device:

```sh
make -C arm test
```

The geometry test reads the shared expected rectangles in
`tools/testdata/fit_cases.txt` and checks NTSC/PAL alignment, crop and fit
behavior, and invalid inputs.
