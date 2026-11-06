"""Pack the qualified DV1 diagnostic core and session launcher into one Scripts entry."""
import argparse
import base64
import hashlib
import io
import json
from pathlib import Path
import zipfile

from rt4k_test import CORE_HASH


HERE = Path(__file__).resolve().parent


def build(rbf, out):
    core = rbf.read_bytes()
    manifest = json.loads(rbf.with_suffix('.rbf.json').read_text())
    if (hashlib.sha256(core).hexdigest() != CORE_HASH or manifest['rbf']['sha256'] != CORE_HASH
            or manifest['rbf']['name'] != rbf.name or manifest['rbf']['bytes'] != len(core)
            or manifest['rbf']['marginal'] is not False or manifest['timing']['pass'] is not True
            or manifest['fit']['seed'] != 5 or manifest['core_version'] != 'dev-dv1-crop'):
        raise ValueError('Expected the qualified seed 5 DV1 diagnostic core and its original manifest')
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, 'w') as bundle:
        for name, data in [('__main__.py', (HERE / 'rt4k_test.py').read_bytes()),
                           ('manager.py', (HERE / 'manager.py').read_bytes()), ('H264.rbf', core)]:
            bundle.writestr(zipfile.ZipInfo(name), data, compress_type=zipfile.ZIP_DEFLATED)
    archive = buf.getvalue()
    script = '''#!/bin/bash
set -eu
work=$(mktemp /tmp/jellyfin-rt4k.XXXXXX)
trap 'rm -f -- "$work"' EXIT
echo "Checking the Jellyfin RT4K test build..."
base64 -d > "$work" <<'RT4K_PAYLOAD'
''' + base64.encodebytes(archive).decode('ascii') + '''RT4K_PAYLOAD
printf '%s  %s\\n' 'DIGEST' "$work" | sha256sum -c - >/dev/null
# Keep the app alive when loading its core restarts MiSTer and closes Scripts.
python3 -c 'import subprocess, sys; sys.exit(subprocess.call([sys.executable] + sys.argv[1:], start_new_session=True))' "$work" "$@"
'''.replace('DIGEST', hashlib.sha256(archive).hexdigest())
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_bytes(script.encode('ascii'))
    out.chmod(0o755)


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('rbf', type=Path)
    p.add_argument('out', type=Path)
    args = p.parse_args()
    build(args.rbf, args.out)
