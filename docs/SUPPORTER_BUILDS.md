# Public source and supporter builds

This repository contains the standalone Jellyfin app, playback worker, FPGA
sources, installer and updater. A source build needs no supporter code.

The official app adds optional components from a separate private repository.
That repository owns the beta playback lock, code-entry screen, issued code
verifiers, and official beta build tooling. New supporter-only features belong
there. There are currently no additional supporter-only features in Jellyfin.

The private build exports a specific public commit and adds files marked with
the `premium` Go build tag. It cannot replace public files. Small extension
points in the public app have no-op implementations in `premium_free.go`.
Compiling this public repository alone with `-tags premium` is unsupported;
the matching private components are required.

Public source archives are built from this checkout. They include the source
and notices for the public application, worker, core and dependencies, but
exclude the private additions and code registry. The public-content check
rejects private registries and premium implementation files.

Moving a file out of the current source tree does not remove older public
commits or revoke any rights already granted under its existing license.
