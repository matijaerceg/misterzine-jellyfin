import hashlib
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest import mock

import manager
import rt4k_test


class RT4KSessionTests(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.card = Path(tmp.name)
        self.root = self.card / 'misterzine-jellyfin'
        self.release = self.root / 'releases' / rt4k_test.BETA
        self.release.mkdir(parents=True)
        (self.root / 'updates').mkdir()
        self.stock = self.release / 'H264.rbf'
        self.stock.write_bytes(b'stock')
        self.trial = self.root / 'rt4k-dv1-test'
        self.state = {'current': rt4k_test.BETA, 'previous': 'previous'}
        manager.write_json(self.root / 'active.json', self.state)
        manager.write_json(self.release / 'manifest.json', {'id': rt4k_test.BETA, 'channel': 'beta'})
        self.archive = self.card / 'payload.zip'
        with zipfile.ZipFile(self.archive, 'w') as archive:
            archive.writestr('H264.rbf', b'trial')

    def test_trial_and_regular_launch_use_their_own_core(self):
        chosen = []
        def select(card, root, folder):
            chosen.append(folder)
            return True
        child = mock.Mock(pid=123)
        child.wait.return_value = 0
        with mock.patch.object(rt4k_test, 'CORE_HASH', hashlib.sha256(b'trial').hexdigest()), \
                mock.patch.object(manager, 'ensure_selected_core', side_effect=select), \
                mock.patch.object(manager, 'recover_activation', return_value=True), \
                mock.patch.object(manager, 'display_env', return_value=''), \
                mock.patch.object(manager, 'rotate_log'), mock.patch.object(manager, 'open', mock.mock_open()), \
                mock.patch.object(manager.subprocess, 'run'), \
                mock.patch.object(manager.subprocess, 'Popen', return_value=child) as spawn:
            rt4k_test.run(self.card, self.archive)
            manager.run(self.root)
        self.assertEqual(chosen, [self.trial, self.release])
        for call, folder in zip(spawn.call_args_list, chosen):
            self.assertEqual(call.args[0][0], str(self.release / 'plexcrt'))
            self.assertIn(str(self.root / 'plexcrt.json'), call.args[0])
            self.assertEqual(call.kwargs['env']['MISTERZINE_H264_RBF'],
                             (folder / 'H264').relative_to(self.card).as_posix())
        self.assertEqual(manager.read_state(self.root), self.state)
        self.assertEqual(self.stock.read_bytes(), b'stock')

    def test_damaged_core_wrong_release_and_busy_app_stop_before_writing(self):
        with self.assertRaisesRegex(ValueError, 'damaged'):
            rt4k_test.run(self.card, self.archive)
        with manager.locked(self.root), self.assertRaisesRegex(RuntimeError, 'running'):
            rt4k_test.run(self.card, self.archive)
        journal = self.root / 'updates/activation.json'
        journal.touch()
        with self.assertRaisesRegex(ValueError, 'Finish the Jellyfin update'):
            rt4k_test.run(self.card, self.archive)
        journal.unlink()
        manager.write_json(self.root / 'active.json', {'current': 'another-beta'})
        with self.assertRaisesRegex(ValueError, 'requires'):
            rt4k_test.run(self.card, self.archive)
        self.assertFalse(self.trial.exists())
        self.assertEqual(self.stock.read_bytes(), b'stock')
