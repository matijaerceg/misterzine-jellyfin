# Jellyfin distribution

Jellyfin owns `misterzine-jellyfin` on the SD card, its own Scripts entries,
main-menu launcher and download staging directory. It does not install into or
update MisterZine Plex. Application settings, calibration, cached artwork and
saved beta access live outside the versioned application payload so updates and
rollback preserve them.

## Build channels

Public and development ARM builds use `release/build_app.py`. They contain no
supporter gate and need no access code. Official beta builds are assembled by
the private companion repository, which supplies the supporter components and
code verifier. See [the repository split](../docs/SUPPORTER_BUILDS.md).

Packaging validates the application build stamp against its binary hash,
release ID and channel. Public releases must be rebuilt for the public channel,
not made by renaming a beta ZIP. Never include raw codes in release assets.

## Package inputs

Build the Go ARMv7 application and `jfplay` using the documented toolchains.
Supply the FFmpeg executable used by the current toolchain and the qualified
H.264 RBF with its original generated manifest. An upstream checked-in RBF is
not a substitute for the qualified custom image. Keep the original manifest
unchanged.

The core in `third_party/MiSTer_h264_decoder` is this project's own fork and
follows only this project's rules. Upstream changes are never merged
automatically; a wanted fix is cherry-picked and reviewed like any other change.
Before the build that will ship, set `CORE_VERSION` in its `h264/emu.sv` to
`jf-` plus the app version it first ships in, and commit it. Then build with
`build_release.sh --compile --release` from that clean commit. The packager
refuses a core that is marginal, failed timing, was built from a dirty tree, or
carries a version without the `jf-` prefix. An app-only release reuses the last
packaged core and its manifest.

The runtime payload is `plexcrt`, `jfplay`, `ffmpeg`, and the H.264 RBF.
Fonts and UI assets are embedded in the application. HPS tools and static
libraries are build inputs rather than runtime dependencies.

Include matching source and build instructions for the core and worker,
component notices, and the applicable FFmpeg, OpenSSL, C library and compiler
runtime source/relink materials. Keep source assets beside the binary release
and identify them by hash. Exclude account files, caches, logs, raw codes,
receipts, local paths and Git history from distributable archives.

Run `python3 release/build_app.py --help` and
`python3 release/build_package.py --help` for the available build inputs.

## Prepare and publish

Prepare an immutable package, review its manifest and release notes, then run:

```sh
python3 release/publish.py --package "$PACKAGE" --tag "$TAG" --notes "$NOTES"
```

Without `--publish`, this prepares local distribution assets only. Publishing
requires an authenticated GitHub CLI and a reviewed source tag in
`matijaerceg/misterzine-jellyfin`. Add `--publish` only for an approved release.
Use unique release IDs and tags. Never replace a previously published package
at the same URL: clients verify its hash.

The distribution catalogue lists only channels that have published releases.
An exact-version installer selects that immutable release; a latest-channel
installer follows the channel catalogue. Do not create a placeholder stable
release while only a beta exists.

## Catalogue slots

`catalogue.json` holds two entries per channel. `current` is what the app's
updater and the channel installer scripts install. `releases` is the legacy
slot, read by cards whose updater predates `current`. Readers take a channel
from `current` when it is there and fall back to `releases` otherwise, so a
catalogue written before `current` existed stays valid. The same release ID
may appear in both slots, but not twice in one.

Each publish writes its release to `current` and, unless the channel is
listed in `legacy_pin`, to `releases` too. Publishing with `--pin-legacy`
freezes that release's `releases` entry and adds the channel to `legacy_pin`,
for when older cards must keep installing one particular release. The frozen
entry points at that release's own `release-db.json.zip` asset rather than
the channel database, which every later publish rewrites. Never edit or
delete a pinned legacy entry, by hand or with the tools: old cards may update
from it at any time, and publishing refuses a pinned channel whose entry is
missing.

## Memos

`catalogue.json` may hold `memos`, a list of short notes from the developers,
each `{"id", "title", "text"}`. The app shows the first note not yet dismissed
over Home, once per id, and remembers dismissed ids in its config; a published
note with the id of one compiled into the app replaces that note's text. So a
note can be added, reworded or withdrawn between releases:

```
python3 release/publish_memos.py --show
python3 release/publish_memos.py --memos notes.json            # inspect dist/distribution/catalogue.json
python3 release/publish_memos.py --memos notes.json --publish
```

The list replaces the catalogue's notes whole and touches nothing else; an
empty list withdraws them all. A reworded note keeps its id and reaches only
cards that have not dismissed it; a note meant for everyone again takes a new
id. Ids are up to 64 characters of letters, digits, `.`, `_` and `-`; titles up
to 80 bytes, texts up to 600, at most 20 notes. A release publish carries the
notes over unchanged unless given `--memos`. Cards validate the release entries
only and the app drops a note it cannot show, so a mistake in a note never
stops updates; the publishing tools refuse one.

## Validation

Run the Go suite, ARM vet/build, release Python tests and public-content check.
Use synthetic codes and fixtures in tests. Before a release that changes the
updater or its Downloader catalogue, run
`python3 tools/check_downloader_integration.py /path/to/downloader_latest.zip`
on Linux: it runs the official Downloader against a synthetic card under
`/media` and checks staging, filters, repair and that other files stay
untouched. Verify locked Play and Resume,
incorrect-code rejection, successful unlock, saved access after restart,
upgrade and rollback, and ungated public builds.

Exercise the actual package on the intended device: fresh install, main-menu
launch, repair/update, failed-start recovery and rollback. Preserve the user's
settings and other applications. Only use video modes supported by the
connected display. Unit tests and cross-builds do not establish hardware
compatibility or visual playback quality.

Keep private test journals and device-specific deployment details outside the
repository. Public release notes should distinguish tested behavior from known
beta limitations.
