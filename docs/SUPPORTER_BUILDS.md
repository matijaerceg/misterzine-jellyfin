# Public source and supporter builds

This repository contains the standalone Jellyfin app, playback worker, FPGA
sources, installer and updater. A source build needs no supporter code.

The supporter-code entry and the `internal/access` package are public. The
official app adds optional components from a separate private repository:
the beta playback lock and its code entry, the registry of issued MisterZine
codes and the features those codes unlock, such as the screensavers. The
playback UI themes other than Standard are gated in the public app itself
(`access.PlaybackTheme`): a public build shows the row locked, and only a
build whose registry has a code covering October 2026 can unlock it.
Official beta builds are made with that repository's tooling. New
supporter-only features belong there.

The October 2026 beta batch's code is registered as the first supporter
code, so a card already unlocked for that beta has both features without
entering anything again.

The private build exports a specific public commit and adds files marked with
the `premium` Go build tag. It may only add files; it cannot replace public
ones. Small extension points in the public app have no-op implementations in
`premium_free.go`, and their names and signatures are what the private files
build against.
Compiling this public repository alone with `-tags premium` is unsupported;
the matching private components are required.

Public source archives are built from this checkout. They include the source
and notices for the public application, worker, core and dependencies, but
exclude the private additions and code registry. The public-content check
rejects private registries and premium implementation files.

Moving a file out of the current source tree does not remove older public
commits or revoke any rights already granted under its existing license.
