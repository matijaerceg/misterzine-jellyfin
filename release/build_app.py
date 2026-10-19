#!/usr/bin/env python3
"""Build a public or development ARM application without supporter components."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def build(channel, version, ident, go='go'):
    if channel not in ('development', 'public'):
        raise ValueError('Source builds support development and public channels')
    for value in (version, ident):
        if not re.fullmatch(r'[A-Za-z0-9_.-]+', value):
            raise ValueError('Invalid version or build identifier')
    app = ROOT / 'app'
    (app / 'dist').mkdir(exist_ok=True)
    flags = ['-s', '-w', '-X', 'plexcrt/internal/updates.Channel=' + channel,
             '-X', 'main.version=' + version, '-X', 'main.build=' + ident]
    env = dict(os.environ, GOOS='linux', GOARCH='arm', GOARM='7', CGO_ENABLED='0')
    subprocess.run([go, 'build', '-trimpath', '-ldflags', ' '.join(flags),
                    '-o', 'dist/plexcrt', './cmd/plexcrt'], cwd=app, env=env, check=True)
    digest = hashlib.sha256((app / 'dist/plexcrt').read_bytes()).hexdigest()
    (app / 'dist/plexcrt.build.json').write_text(json.dumps({
        'id': ident, 'version': version, 'channel': channel, 'access': None,
        'binary_sha256': digest}, indent=2) + '\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--channel', choices=('development', 'public'), required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--id', required=True)
    parser.add_argument('--go', default='go')
    args = parser.parse_args()
    build(args.channel, args.version, args.id, args.go)
