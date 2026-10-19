import hashlib
import io
import json
import tempfile
import unittest
import zipfile
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

import build_package


class PackageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.dist = self.root / 'app/dist'
        self.dist.mkdir(parents=True)
        self.core = self.root / 'private/H264_crt_qualified.rbf'
        self.core.parent.mkdir(parents=True)
        self.core.write_bytes(b'qualified rbf fixture')
        self.rbf_manifest = self.root / 'private/quartus-rbf-manifest.json'
        self.rbf_manifest.write_text(json.dumps({
            'schema': 1, 'core_version': 'qualified-test', 'build_date': '261018',
            'rbf': {'name': self.core.name, 'bytes': self.core.stat().st_size,
                    'sha256': build_package.sha(self.core), 'marginal': False},
            'git': {'branch': 'fixture', 'sha': 'abc123', 'dirty': True},
            'fit': {'seed': 7}, 'timing': {'pass': True, 'clk_dec_100c_mhz': 103.67},
            'toolchain': {'quartus': '17.0.2'}}))
        self.worker = self.root / 'jfplay'
        self.worker.write_bytes(b'ARM worker')
        self.ffmpeg = self.root / 'ffmpeg'
        self.ffmpeg.write_bytes(b'ARM static ffmpeg')
        self.app = self.dist / 'plexcrt'
        self.app.write_bytes(b'ARM app')
        self.verifier = hashlib.sha256(b'synthetic verifier').hexdigest()
        (self.dist / 'plexcrt.build.json').write_text(json.dumps({
            'id': 'fixture', 'version': '1.0.0-beta.1', 'channel': 'beta',
            'access': {'batch': 'fixture-batch'}, 'batch': 'fixture-batch',
            'code_sha256': self.verifier, 'binary_sha256': build_package.sha(self.app)}))
        self.source = self.root / 'corresponding-source.zip'
        with zipfile.ZipFile(self.source, 'w') as z:
            z.writestr('BUILD.md', 'fixture source and exact build recipe')
            z.writestr('H264/rtl/fixture.sv', 'source')
        release = self.root / 'release'
        release.mkdir()
        for name in ('manager.py', 'update_service.py', 'catalogue.py', 'menu_launcher.py', 'README.md', 'THIRD_PARTY_NOTICES.md', 'BETA_ACCESS.md'):
            (release / name).write_text('# fixture')
        (release / 'licenses').mkdir()
        (release / 'licenses/NOTICE.txt').write_text('notice')
        self.release_dir = release

    def build(self):
        with patch.object(build_package, 'ROOT', self.root), redirect_stdout(io.StringIO()):
            return build_package.build(self.root / 'out', 'fixture', '1.0.0-beta.1',
                                       self.worker, self.ffmpeg, self.core,
                                       self.rbf_manifest, self.source)

    def test_beta_package_is_allowlisted_keeps_core_provenance_and_hides_verifier(self):
        path = self.build()
        with zipfile.ZipFile(path) as archive:
            files = set(archive.namelist())
            payload = {name.removeprefix('misterzine-jellyfin-beta/payload/')
                       for name in files if name.startswith('misterzine-jellyfin-beta/payload/')}
            self.assertEqual(payload, {'plexcrt', 'jfplay', 'ffmpeg', 'H264.rbf'})
            manifest = json.loads(archive.read('misterzine-jellyfin-beta/manifest.json'))
            self.assertEqual(manifest['access'], {'batch': 'fixture-batch'})
            self.assertNotIn('code_sha256', manifest)
            self.assertEqual(manifest['core']['sha256'], build_package.sha(self.core))
            self.assertEqual(manifest['core']['quartus_manifest_sha256'], build_package.sha(self.rbf_manifest))
            self.assertEqual(manifest['source']['sha256'], build_package.sha(self.source))
            self.assertEqual(manifest['source']['size'], self.source.stat().st_size)
            self.assertEqual(archive.read('misterzine-jellyfin-beta/provenance/quartus-rbf-manifest.json'),
                             self.rbf_manifest.read_bytes())
            self.assertNotIn(self.verifier.encode(), b''.join(archive.read(name) for name in files))
            installer = archive.read('Scripts/MisterZine-Jellyfin-Install.sh').decode()
            self.assertIn('/media/fat/misterzine-jellyfin-beta/manager.py install', installer)
            self.assertIn('/media/fat/misterzine-jellyfin/manager.py run', installer)
        self.assertEqual((self.root / 'out' / manifest['source']['filename']).read_bytes(), self.source.read_bytes())

    def test_beta_build_fails_closed_on_missing_or_malformed_gate_metadata(self):
        metadata = self.dist / 'plexcrt.build.json'
        original = json.loads(metadata.read_text())
        for changes in ({'batch': ''}, {'code_sha256': 'bad'}, {'binary_sha256': '0' * 64},
                        {'channel': 'public', 'access': None, 'batch': '', 'code_sha256': ''}):
            with self.subTest(changes=changes):
                bad = dict(original, **changes)
                metadata.write_text(json.dumps(bad))
                with patch.object(build_package, 'ROOT', self.root), self.assertRaises(ValueError):
                    build_package.build(self.root / 'out', 'fixture', '1.0.0-beta.1',
                                        self.worker, self.ffmpeg, self.core,
                                        self.rbf_manifest, self.source)
        metadata.write_text(json.dumps(original))

    def test_rejects_unqualified_or_mismatched_core(self):
        provenance = json.loads(self.rbf_manifest.read_text())
        provenance['rbf']['marginal'] = True
        self.rbf_manifest.write_text(json.dumps(provenance))
        with patch.object(build_package, 'ROOT', self.root), self.assertRaises(ValueError):
            build_package.build(self.root / 'out', 'fixture', '1.0.0-beta.1',
                                self.worker, self.ffmpeg, self.core,
                                self.rbf_manifest, self.source)


if __name__ == '__main__':
    unittest.main()
