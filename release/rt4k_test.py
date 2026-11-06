"""Entry point inside the self-contained DV1 tester script."""
import argparse
import contextlib
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import zipfile

import manager


BETA = 'beta-0.1.0-beta.14'
CORE_HASH = 'c7b839acdc039e66687160be97c4decca2458648f48a61b229122cfae0e4e3fb'


def run(card, archive):
    root = card.resolve() / 'misterzine-jellyfin'
    if not (root / 'active.json').is_file():
        raise ValueError('Install Jellyfin beta.14 on this card first.')
    with manager.no_update_running(root) as clear:
        if not clear:
            raise ValueError('Finish the Jellyfin update and return to the MiSTer menu first.')
        with manager.locked(root):
            current = manager.read_state(root)['current']
            if current != BETA:
                raise ValueError('This RT4K test requires the installed Jellyfin beta.14.')
            folder = root / 'releases' / current
            manifest = json.loads((folder / 'manifest.json').read_text())
            if manifest.get('id') != BETA or manifest.get('channel') != 'beta':
                raise ValueError('The selected release does not match Jellyfin beta.14.')
            with zipfile.ZipFile(archive) as payload:
                core = payload.read('H264.rbf')
            if hashlib.sha256(core).hexdigest() != CORE_HASH:
                raise ValueError('The RT4K test download is damaged. Download the script again.')
            test_folder = root / 'rt4k-dv1-test'
            target = test_folder / 'H264.rbf'
            if test_folder.is_symlink() or target.is_symlink():
                raise ValueError('Unexpected link at the RT4K test location.')
            manager.write_if_changed(target, core)
            if manager.digest(target) != CORE_HASH:
                raise ValueError('The test core failed verification on the SD card.')
            print('Opening Jellyfin with the RT4K test core dev-dv1-crop.\n'
                  'To use the stock beta again, exit to the menu and open Jellyfin normally.', flush=True)
            # Scripts' terminal can disappear during the core switch. The
            # detached session must still be able to write its launch trace.
            with open('/tmp/misterzine-jellyfin-rt4k-test.log', 'w', buffering=1) as log:
                with contextlib.redirect_stdout(log), contextlib.redirect_stderr(log):
                    try:
                        manager.run_with_core(root, folder, test_folder)
                    except Exception as exc:
                        print('Launch failed:', exc)
                        raise


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Open Jellyfin beta.14 with the RT4K DV1 test core.')
    parser.add_argument('--card', type=Path, default=Path('/media/fat'))
    try:
        run(parser.parse_args().card, sys.argv[0])
    except (KeyError, OSError, ValueError, RuntimeError, zipfile.BadZipFile, subprocess.SubprocessError) as exc:
        sys.exit('RT4K test: ' + str(exc))
