"""Build an allowlisted Jellyfin release ZIP from verified build outputs."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parent.parent
HASH = re.compile(r'[0-9a-f]{64}')
IDENT = re.compile(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,95}')
VERSION = re.compile(r'v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?')
PAYLOAD_NAMES = ('plexcrt', 'jfplay', 'ffmpeg', 'H264.rbf')


def sha(path):
    h = hashlib.sha256()
    with path.open('rb') as source:
        for block in iter(lambda: source.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def read_build_metadata(path, ident, version, binary_hash):
    if not path.is_file():
        raise ValueError('Application build metadata is required')
    info = json.loads(path.read_text(encoding='utf-8'))
    if info.get('id') != ident or info.get('version') != version or info.get('binary_sha256') != binary_hash:
        raise ValueError('Application metadata does not match the selected package and binary')
    channel = info.get('channel')
    if channel not in ('development', 'public', 'beta'):
        raise ValueError('Invalid application release channel')
    batch, verifier = info.get('batch', ''), info.get('code_sha256', '')
    if channel == 'beta':
        if '-' not in version:
            raise ValueError('Beta builds must use a prerelease version')
        if not isinstance(batch, str) or not __import__('re').fullmatch(r'[a-z0-9][a-z0-9-]{0,47}', batch):
            raise ValueError('Beta build is missing valid batch metadata')
        if not isinstance(verifier, str) or not HASH.fullmatch(verifier):
            raise ValueError('Beta build is missing a valid code verifier')
        if info.get('access') not in (None, {'batch': batch}):
            raise ValueError('Beta access metadata does not match the batch')
        return {'channel': channel, 'access': {'batch': batch}}
    if batch or verifier or info.get('access') not in (None, {}):
        raise ValueError('Non-beta build contains beta gate metadata')
    if channel == 'public' and '-' in version:
        raise ValueError('Public builds must use a stable version')
    return {'channel': channel, 'access': None}


def build(out, ident, version, worker=None, ffmpeg=None, rbf=None, rbf_manifest=None, source_archive=None):
    if not isinstance(ident, str) or not IDENT.fullmatch(ident):
        raise ValueError('Invalid release identifier')
    if not isinstance(version, str) or not VERSION.fullmatch(version):
        raise ValueError('Invalid release version')
    out.mkdir(parents=True, exist_ok=True)
    worker = Path(worker or ROOT / 'app/dist/jfplay')
    ffmpeg = Path(ffmpeg or ROOT / 'app/dist/ffmpeg')
    rbf = Path(rbf or ROOT / 'app/dist/H264.rbf')
    rbf_manifest = Path(rbf_manifest or ROOT / 'app/dist/quartus-rbf-manifest.json')
    inputs = {'plexcrt': ROOT / 'app/dist/plexcrt', 'jfplay': worker,
              'ffmpeg': ffmpeg, 'H264.rbf': rbf}
    for name, path in inputs.items():
        if not path.is_file():
            raise FileNotFoundError('Required release input is missing: ' + name)
    if not rbf_manifest.is_file():
        raise FileNotFoundError('Qualified Quartus RBF manifest is required')
    provenance = json.loads(rbf_manifest.read_text(encoding='utf-8'))
    rbf_info = provenance.get('rbf', {})
    if rbf_info.get('name') != rbf.name or rbf_info.get('sha256') != sha(rbf) or rbf_info.get('marginal') is not False:
        raise ValueError('RBF does not match a non-marginal qualified build manifest')
    if provenance.get('timing', {}).get('pass') is not True:
        raise ValueError('RBF build manifest does not record a timing pass')
    metadata = read_build_metadata(ROOT / 'app/dist/plexcrt.build.json', ident, version, sha(inputs['plexcrt']))
    core_provenance = {'rbf': rbf.name, 'sha256': sha(rbf),
                       'quartus_manifest_sha256': sha(rbf_manifest),
                       'core_version': provenance.get('core_version'),
                       'build_date': provenance.get('build_date'),
                       'git': provenance.get('git'), 'fit': provenance.get('fit'),
                       'timing': provenance.get('timing'), 'toolchain': provenance.get('toolchain')}

    if source_archive is None:
        raise ValueError('A matching corresponding-source archive is required')
    source_archive = Path(source_archive)
    if not source_archive.is_file():
        raise FileNotFoundError('Corresponding-source archive is required')

    with tempfile.TemporaryDirectory(prefix='misterzine-jellyfin-package-') as tmp:
        stage = Path(tmp)
        package = stage / 'misterzine-jellyfin-beta'
        payload = package / 'payload'
        payload.mkdir(parents=True)
        hashes = {}
        for name, src in inputs.items():
            shutil.copyfile(src, payload / name)
            hashes[name] = sha(src)
        source_name = 'MisterZine-Jellyfin-Source-' + ident + '.zip'
        source_meta = {'filename': source_name, 'size': source_archive.stat().st_size,
                       'sha256': sha(source_archive)}
        manifest = {'id': ident, 'version': version, **metadata, 'core': core_provenance,
                    'source': source_meta, 'files': hashes}
        (package / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n', encoding='utf-8')
        prov = package / 'provenance'
        prov.mkdir()
        shutil.copyfile(rbf_manifest, prov / 'quartus-rbf-manifest.json')
        for name in ('manager.py', 'update_service.py', 'catalogue.py', 'menu_launcher.py', 'README.md', 'THIRD_PARTY_NOTICES.md', 'BETA_ACCESS.md'):
            shutil.copyfile(ROOT / 'release' / name, package / name)
        shutil.copytree(ROOT / 'release/licenses', package / 'licenses')
        with zipfile.ZipFile(source_archive) as archive:
            if archive.testzip() is not None:
                raise ValueError('Corresponding-source archive is corrupt')
        shutil.copyfile(source_archive, out / source_name)
        script = stage / 'Scripts/MisterZine-Jellyfin-Install.sh'
        script.parent.mkdir()
        script.write_text('#!/bin/bash\nset -e\npython3 /media/fat/misterzine-jellyfin-beta/manager.py install\npython3 /media/fat/misterzine-jellyfin/manager.py run\n', newline='\n')
        zip_path = out / ('MisterZine-Jellyfin-' + ident + '.zip')
        with zipfile.ZipFile(zip_path, 'w', zipfile.ZIP_DEFLATED) as archive:
            for path in sorted(stage.rglob('*')):
                if path.is_file():
                    archive.write(path, path.relative_to(stage).as_posix())
        (out / (zip_path.name + '.sha256')).write_text(sha(zip_path) + '  ' + zip_path.name + '\n')
        print(str(zip_path))
        print('SHA-256: ' + sha(zip_path))
        print('Payloads: ' + ', '.join(sorted(hashes)))
        return zip_path


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--out', type=Path, default=ROOT / 'release/dist')
    parser.add_argument('--id', required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--worker', type=Path)
    parser.add_argument('--ffmpeg', type=Path)
    parser.add_argument('--rbf', type=Path)
    parser.add_argument('--rbf-manifest', type=Path)
    parser.add_argument('--source-archive', type=Path, required=True)
    args = parser.parse_args()
    build(args.out, args.id, args.version, args.worker, args.ffmpeg, args.rbf, args.rbf_manifest, args.source_archive)
