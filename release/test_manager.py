import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import manager

# Tests never talk to the live report service.
manager.REPORT_SERVICE = ''


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.card = self.base / 'card'
        self.package = self.base / 'package'
        (self.package / 'payload').mkdir(parents=True)
        self.root = self.card / 'misterzine-jellyfin'

    def package_version(self, ident):
        files = {}
        for name in manager.PAYLOAD:
            data = (ident + name).encode()
            (self.package / 'payload' / name).write_bytes(data)
            files[name] = hashlib.sha256(data).hexdigest()
        manager.write_json(self.package / 'manifest.json', {'id': ident, 'files': files})

    def test_install_update_rollback_preserve_settings(self):
        self.package_version('alpha-1')
        manager.install(self.card, self.package)
        account = self.root / 'plexcrt.json'
        account.write_text('{"token":"TEST-ONLY-secret"}')
        key = self.root / 'beta-keys/2026-09.key'
        key.parent.mkdir()
        key.write_bytes(b'patron-test-key')
        receipt = self.root / 'beta-unlocks/fixture.receipt'
        receipt.parent.mkdir()
        receipt.write_text('unlocked\n')
        self.package_version('alpha-2')
        manager.install(self.card, self.package)
        self.assertEqual(manager.read_state(self.root), {'current': 'alpha-2', 'previous': 'alpha-1'})
        manager.rollback(self.root)
        self.assertEqual(manager.read_state(self.root)['current'], 'alpha-1')
        manager.remove(self.card)
        self.assertTrue((self.card / 'Scripts/MisterZine-Jellyfin-Rollback.sh.disabled').is_file())
        self.assertFalse((self.card / 'Scripts/MisterZine-Jellyfin-Run.sh').exists())
        self.assertEqual(account.read_text(), '{"token":"TEST-ONLY-secret"}')
        self.assertEqual(key.read_bytes(), b'patron-test-key')
        self.assertEqual(receipt.read_text(), 'unlocked\n')

    def test_install_after_remove_restores_the_scripts_entries(self):
        self.package_version('alpha-1')
        manager.install(self.card, self.package)
        manager.remove(self.card)
        manager.install(self.card, self.package)
        scripts = self.card / 'Scripts'
        self.assertTrue((scripts / 'MisterZine-Jellyfin-Rollback.sh').is_file())
        # A set-aside copy left behind would keep the boot check switched off.
        self.assertEqual(list(scripts.glob('*.disabled')), [])

    def test_runtime_preflight_finishes_before_core_switch_and_app_launch(self):
        self.root.mkdir(parents=True)
        manager.write_json(self.root / 'active.json', {'current': 'fixture', 'previous': None})
        events = []
        child = mock.Mock(pid=123)
        child.wait.return_value = 0
        def preflight(args, check=False):
            events.append('preflight')
            self.assertTrue(check)
            self.assertEqual(args[-1], '-check')
        def select(card, root, folder):
            events.append('core')
            return True
        def display(card):
            events.append('display')
            self.assertEqual(card, self.root.parent)
            return '{"ini": "MiSTer.ini", "vrr": "off"}'
        def launch(*args, **kwargs):
            events.append('app')
            self.assertEqual(kwargs['env']['MISTERZINE_PLEX_DISPLAY'], '{"ini": "MiSTer.ini", "vrr": "off"}')
            return child
        with mock.patch.object(manager, 'recover_activation', return_value=True), \
                mock.patch.object(manager, 'ensure_selected_core', side_effect=select), \
                mock.patch.object(manager, 'display_env', side_effect=display), \
                mock.patch.object(manager.subprocess, 'run', side_effect=preflight), \
                mock.patch.object(manager.subprocess, 'Popen', side_effect=launch), \
                mock.patch.object(manager, 'rotate_log'), \
                mock.patch('builtins.open', mock.mock_open()):
            manager.run(self.root)
        # the INI check once main has read the INI for the core, before the app
        self.assertEqual(events, ['preflight', 'core', 'display', 'app'])

    def test_bad_package_never_changes_active_release(self):
        self.package_version('alpha-1')
        manager.install(self.card, self.package)
        self.package_version('alpha-2')
        (self.package / 'payload/plexcrt').write_bytes(b'broken')
        with self.assertRaises(ValueError):
            manager.install(self.card, self.package)
        self.assertEqual(manager.read_state(self.root)['current'], 'alpha-1')

    def test_install_keeps_beta_help_and_core_build_provenance_with_release(self):
        self.package_version('beta-1')
        (self.package / 'BETA_ACCESS.md').write_text('access help')
        (self.package / 'TERMS.md').write_text('terms')
        (self.package / 'provenance').mkdir()
        (self.package / 'provenance/quartus-rbf-manifest.json').write_text('{"fixture":true}')
        manager.install(self.card, self.package)
        installed = self.root / 'releases/beta-1'
        self.assertEqual((installed / 'BETA_ACCESS.md').read_text(), 'access help')
        self.assertEqual((self.root / 'TERMS.md').read_text(), 'terms')
        self.assertEqual((installed / 'provenance/quartus-rbf-manifest.json').read_text(), '{"fixture":true}')

    def test_interrupted_activation_keeps_previous_release(self):
        self.package_version('alpha-1')
        manager.install(self.card, self.package)
        self.package_version('alpha-2')
        real_atomic = manager.atomic
        def fail_active(path, data):
            if path.name == 'active.json':
                raise OSError('simulated full card')
            real_atomic(path, data)
        with mock.patch.object(manager, 'atomic', side_effect=fail_active):
            with self.assertRaises(OSError):
                manager.install(self.card, self.package)
        self.assertEqual(manager.read_state(self.root)['current'], 'alpha-1')

    def test_menu_entry_follows_the_selected_release_and_the_installed_watcher(self):
        import xml.etree.ElementTree as ET
        entry = self.card / 'MisterZine Jellyfin.mgl'
        self.root.mkdir(parents=True)
        (self.root / 'menu_launcher.py').write_text("SELECTIONS = ('MisterZine Jellyfin',)\n")
        self.package_version('alpha-1')
        manager.install(self.card, self.package)
        self.assertEqual(ET.parse(entry).findtext('rbf'), 'misterzine-jellyfin/releases/alpha-1/H264')
        self.package_version('alpha-2')
        manager.install(self.card, self.package)
        self.assertEqual(ET.parse(entry).findtext('rbf'), 'misterzine-jellyfin/releases/alpha-2/H264')
        manager.rollback(self.root)
        self.assertEqual(ET.parse(entry).findtext('rbf'), 'misterzine-jellyfin/releases/alpha-1/H264')
        # A watcher from before beta.4 only reacts to the menu bounce.
        (self.root / 'menu_launcher.py').write_text("SELECTION = 'misterzine-jellyfin'\n")
        manager.rollback(self.root)
        self.assertEqual(entry.read_bytes(), manager.LEGACY_ENTRY)
        (self.root / 'active.json').unlink()
        manager.repair_menu_entry(self.card)
        self.assertFalse(entry.exists())

    def test_core_launch_keeps_browser_at_card_root(self):
        import xml.etree.ElementTree as ET
        folder = self.root / 'releases/alpha-1'
        folder.mkdir(parents=True)
        core = folder / 'H264.rbf'
        core.write_bytes(b'core')
        entry = manager.core_launch_entry(self.root, folder)
        self.assertEqual(entry.parent, self.card)
        self.assertEqual(ET.parse(entry).findtext('rbf'),
                         'misterzine-jellyfin/releases/alpha-1/H264')

    def test_run_core_selection_reloads_when_same_h264_name_is_from_another_release(self):
        folder = self.root / 'releases/alpha-2'
        folder.mkdir(parents=True)
        (folder / 'H264.rbf').write_bytes(b'new core')
        old = {40: str(self.root / 'releases/alpha-1/H264.rbf').encode()}
        selected = {41: str(folder / 'H264.rbf').encode()}
        corename = self.base / 'CORENAME'
        corename.write_text('H264\n')
        command = self.base / 'MiSTer_cmd'
        with mock.patch.object(manager, 'mister_processes', side_effect=[old, old, selected]), \
                mock.patch.object(manager.time, 'sleep'):
            self.assertTrue(manager.ensure_selected_core(self.card, self.root, folder, wait=1,
                                                         corename=corename, cmd=command))
        self.assertEqual(command.read_text(), 'load_core ' + str(self.card / '.misterzine-jellyfin-core.mgl') + '\n')

    def test_run_core_selection_accepts_exact_release_without_reload(self):
        folder = self.root / 'releases/alpha-2'
        folder.mkdir(parents=True)
        (folder / 'H264.rbf').write_bytes(b'new core')
        corename = self.base / 'CORENAME'
        corename.write_text('H264\n')
        command = self.base / 'MiSTer_cmd'
        proc = {41: str(folder / 'H264.rbf').encode()}
        with mock.patch.object(manager, 'mister_processes', return_value=proc):
            self.assertTrue(manager.ensure_selected_core(self.card, self.root, folder, wait=0,
                                                         corename=corename, cmd=command))
        self.assertFalse(command.exists())

    def test_only_the_exit_status_asks_for_the_menu(self):
        self.assertTrue(manager.app_finished(manager.EXIT_TO_MENU))
        self.assertFalse(manager.app_finished(0))
        self.assertFalse(manager.app_finished(None))    # stopped here: the core went
        for status in (1, 2, -15):
            with self.assertRaises(RuntimeError):
                manager.app_finished(status)

    def test_exit_loads_the_menu_core(self):
        cmd = self.base / 'MiSTer_cmd'
        self.card.mkdir()
        plex, menu = {10: b'/media/fat/plex.rbf'}, {11: b'/media/fat/menu.rbf'}
        with mock.patch.object(manager, 'trace'):
            self.assertFalse(manager.load_menu(self.card, cmd))
            self.assertFalse(cmd.exists())
            (self.card / 'menu.rbf').write_bytes(b'rbf')
            # MiSTer restarts its main process to load the menu
            with mock.patch.object(manager, 'mister_processes', side_effect=[plex, plex, menu]):
                self.assertTrue(manager.load_menu(self.card, cmd))
            self.assertEqual(cmd.read_text(), 'load_core ' + str(self.card / 'menu.rbf') + '\n')
            # Zaparoo Frontend's main comes back on its own menu core
            with mock.patch.object(manager, 'mister_processes', side_effect=[plex, {12: b'zaparoo/menu_zaparoo.rbf'}]):
                self.assertTrue(manager.load_menu(self.card, cmd))
            # the same main process all along, or a new one on another core
            for after in (plex, {13: b'/media/fat/_Console/NES.rbf'}):
                with mock.patch.object(manager, 'mister_processes', side_effect=[plex] + [after] * 100):
                    self.assertFalse(manager.load_menu(self.card, cmd, wait=.1))

    def test_run_loads_the_menu_after_an_exit(self):
        folder = self.root / 'releases/alpha-1'
        folder.mkdir(parents=True)
        manager.write_json(self.root / 'active.json', {'current': 'alpha-1', 'previous': None})

        def launch(status, menu):
            app = mock.Mock(pid=1)
            app.wait.return_value = status
            with mock.patch.object(manager, 'recover_activation', return_value=True), \
                    mock.patch.object(manager.subprocess, 'run'), \
                    mock.patch.object(manager.subprocess, 'Popen', return_value=app), \
                    mock.patch.object(manager, 'ensure_selected_core', return_value=True), \
                    mock.patch.object(manager, 'display_env', return_value=''), \
                    mock.patch.object(manager, 'rotate_log'), \
                    mock.patch.object(manager, 'open', mock.mock_open(), create=True), \
                    mock.patch.object(manager, 'trace'), \
                    mock.patch.object(manager, 'load_menu', return_value=menu) as load_menu:
                return manager.run(self.root), load_menu
        # Exit to the MiSTer menu: the app only says so, run() loads it.
        import io
        for loaded in (True, False):
            with mock.patch.object(manager.sys, 'stderr', new_callable=io.StringIO) as err:
                to_menu, load_menu = launch(manager.EXIT_TO_MENU, loaded)
            # the Exit status either way: a failure would read as a failed start to the updater
            self.assertTrue(to_menu)
            load_menu.assert_called_once_with(self.card)
            if loaded:
                self.assertEqual(err.getvalue(), '')
            else:
                self.assertIn('Could not return to the MiSTer menu. Use the OSD to leave MisterZine Jellyfin.', err.getvalue())
        to_menu, load_menu = launch(0, True)
        self.assertFalse(to_menu)
        load_menu.assert_not_called()
        with self.assertRaisesRegex(RuntimeError, 'status 1'):
            launch(1, True)

    def test_a_lock_held_for_a_moment_is_waited_for(self):
        import fcntl
        import threading
        self.root.mkdir(parents=True)
        holder = (self.root / 'manager.lock').open('a')
        fcntl.flock(holder, fcntl.LOCK_EX)
        with self.assertRaises(RuntimeError):
            with manager.locked(self.root):
                pass
        # the watcher's entry check letting go while the launch waits
        threading.Timer(.3, holder.close).start()
        with manager.locked(self.root, patience=5):
            pass

    def test_exit_lingers_with_the_lock_let_go(self):
        import fcntl
        self.root.mkdir(parents=True)
        free = []

        def linger():
            # a new pick of Plex meanwhile must be able to launch
            with (self.root / 'manager.lock').open('a') as lock:
                try:
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    free.append(True)
                except BlockingIOError:
                    free.append(False)
        with mock.patch.object(manager, 'run', return_value=True), \
                mock.patch.object(manager, 'linger_for_start_check', side_effect=linger), \
                mock.patch.object(manager.sys, 'argv', ['manager.py', 'run', '--card', str(self.card)]):
            self.assertEqual(manager.main(), manager.EXIT_TO_MENU)
        self.assertEqual(free, [True])
        with mock.patch.object(manager, 'run', return_value=False), \
                mock.patch.object(manager, 'linger_for_start_check', side_effect=self.fail), \
                mock.patch.object(manager.sys, 'argv', ['manager.py', 'run', '--card', str(self.card)]):
            self.assertEqual(manager.main(), 0)

    def test_exit_after_an_update_start_outlasts_its_check(self):
        ready = self.base / 'started'
        ready.write_text('ready')
        up = ready.stat().st_mtime
        slept = []
        # the app came up half a second ago: stay until 3 s after it
        self.assertAlmostEqual(manager.linger_for_start_check(str(ready), now=lambda: up + .5, sleep=slept.append), 2.5)
        self.assertAlmostEqual(slept[0], 2.5)
        # long up, no updater, or no marker: no wait
        self.assertEqual(manager.linger_for_start_check(str(ready), now=lambda: up + 10, sleep=self.fail), 0)
        self.assertEqual(manager.linger_for_start_check('', sleep=self.fail), 0)
        self.assertEqual(manager.linger_for_start_check(str(self.base / 'missing'), sleep=self.fail), 0)

    def test_bundled_ffmpeg_is_required_and_no_second_network_fetch_occurs(self):
        self.root.mkdir(parents=True)
        ffmpeg = self.root / 'ffmpeg'
        ffmpeg.write_bytes(b'ARM static decoder')
        ffmpeg.chmod(0o755)
        with mock.patch.object(manager.urllib.request, 'urlopen', side_effect=AssertionError('unexpected network fetch')):
            manager.decoder(self.root)
        ffmpeg.unlink()
        with self.assertRaisesRegex(ValueError, 'bundled FFmpeg'):
            manager.decoder(self.root)

    def test_reject_path_escape(self):
        self.root.mkdir(parents=True)
        manager.write_json(self.root / 'active.json', {'current': '../elsewhere'})
        with self.assertRaises(ValueError):
            manager.read_state(self.root)

    def test_redaction(self):
        value = manager.safe_log('token=TESTsecret&v=1\nAuthorization: Bearer OTHERsecret\nhttps://private.local/path\nTESTsecret', ['TESTsecret'])
        self.assertNotIn('TESTsecret', value)
        self.assertNotIn('OTHERsecret', value)
        self.assertNotIn('private.local', value)
        self.assertIn('&v=1', value)

    def test_names_are_redacted_as_words_and_short_ones_kept(self):
        text = 'log /tmp/misterzine-jellyfin.log\nuser tv signed in on the tv\nRosalind, Rosalinds, rosalind\nGET /x?k=TESTsecret9&v=1'
        value = manager.safe_log(text, [('tv', True), ('Rosalind', True), 'TESTsecret9'])
        self.assertIn('/tmp/misterzine-jellyfin.log', value)
        self.assertIn('user tv signed in on the tv', value)
        self.assertIn('[redacted], Rosalinds, [redacted]', value)
        self.assertNotIn('TESTsecret9', value)
        self.assertIn('&v=1', value)
        # a name that is also a path segment leaves the path alone
        self.assertIn('/media/fat/x', manager.safe_log('/media/fat/x on media', [('media', True)]))
        self.assertNotIn(' media', manager.safe_log('/media/fat/x on media', [('media', True)]))


class ReportTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.card = self.base / 'card'
        self.root = self.card / 'misterzine-jellyfin'
        (self.root / 'releases/beta-9').mkdir(parents=True)
        manager.write_json(self.root / 'active.json', {'current': 'beta-9', 'previous': None})
        manager.write_json(self.root / 'releases/beta-9/manifest.json', {'id': 'beta-9', 'version': '0.1.0-beta.9', 'files': {}})
        (self.root / 'plexcrt.json').write_text('{"server_token":"TESTtoken","server_url":"https://jellyfin.home.example:8096"}')
        (self.card / 'MiSTer.ini').write_text('[MiSTer]\nvideo_mode=8\nfb_terminal=1\n[Menu]\nmain=zaparoo/MiSTer_Zaparoo\n')
        (self.card / 'linux').mkdir()
        (self.card / 'linux/user-startup.sh').write_text('#!/bin/bash\nzaparoo.sh\n')
        self.proc = self.base / 'proc/40'
        self.proc.mkdir(parents=True)
        (self.proc / 'comm').write_text('MiSTer_Zaparoo\n')
        (self.proc / 'cmdline').write_bytes(b'/media/fat/zaparoo/MiSTer_Zaparoo\0')
        exe = self.base / 'MiSTer_Zaparoo'
        exe.write_bytes(b'not really main')
        (self.proc / 'exe').symlink_to(exe)
        self.tmp = self.base / 'tmp'
        self.tmp.mkdir()
        (self.tmp / 'misterzine-jellyfin.log').write_text('watch: header wiped 3 times\nGET https://jellyfin.home.example:8096/x?api_key=TESTtoken\n')
        (self.tmp / 'misterzine-jellyfin.log.1').write_text('ring: no core\n')
        (self.tmp / 'misterzine-jellyfin-menu-run.log').write_text('12:00:00 launch: CORENAME MENU\n')
        (self.tmp / 'misterzine-jellyfin-update-run.log.1').write_text('MisterZine Jellyfin: operation failed\n')

    def report(self):
        return manager.build_report(self.root, self.proc.parent, self.tmp, now=0)

    def test_report_names_the_main_binary_and_keeps_secrets_out(self):
        text = self.report()
        self.assertTrue(text.startswith(manager.REPORT_MAGIC + '\nApp: MisterZine Jellyfin 0.1.0-beta.9\nCreated: 1970-01-01T00:00:00Z\n'))
        self.assertIn('main_binaries: ["MiSTer_Zaparoo ', text)
        self.assertIn(hashlib.sha256(b'not really main').hexdigest()[:16], text)
        self.assertIn('"fb_terminal": "1"', text)
        self.assertIn('"main": "zaparoo/MiSTer_Zaparoo"', text)
        self.assertIn('startup_hooks: ["zaparoo"]', text)
        self.assertIn('== LOG misterzine-jellyfin.log.1', text)
        self.assertIn('== LOG misterzine-jellyfin-menu-run.log', text)
        self.assertIn('== LOG misterzine-jellyfin-update-run.log.1', text)
        self.assertIn('watch: header wiped 3 times', text)
        self.assertNotIn('TESTtoken', text)
        self.assertNotIn('jellyfin.home', text)
        self.assertLessEqual(len(text.encode()), manager.REPORT_MAX_BYTES)

    def test_connection_diagnostics_survive_report_redaction(self):
        evidence = ('server connection: probing 2 advertised connections\n'
                    'server connection: candidate 1 local HTTPS: DNS name not found (2 ms)\n'
                    'server connection: candidate 2 remote HTTP: HTTP 401 (3 ms)\n'
                    'server connection: no reachable advertised connection\n')
        with (self.tmp / 'misterzine-jellyfin.log').open('a') as log:
            log.write(evidence)
        text = self.report()
        self.assertIn(evidence, text)
        self.assertNotIn('TESTtoken', text)
        self.assertNotIn('jellyfin.home', text)

    def test_report_redacts_names_as_words(self):
        (self.root / 'plexcrt.json').write_text(json.dumps({'server_token': 'TESTtoken', 'server_url': 'https://jellyfin.home.example:8096',
                                                            'user_name': 'tv', 'server_name': 'Rosalind'}))
        with (self.tmp / 'misterzine-jellyfin.log').open('a') as log:
            log.write('signed in to Rosalind as tv\nRosalinds stays\nGET https://x.invalid/y?api_key=TESTtoken\nraw TESTtoken\n')
        text = self.report()
        self.assertIn('== LOG misterzine-jellyfin.log', text)
        self.assertIn('signed in to [redacted] as tv', text)
        self.assertIn('Rosalinds stays', text)
        self.assertNotIn('TESTtoken', text)

    def test_plain_http_server_address_stays_out_of_the_report(self):
        lan = '10.0.0.2'
        (self.root / 'plexcrt.json').write_text(json.dumps({'server_token': 'TESTtoken', 'server_url': 'http://%s:8096' % lan}))
        with (self.tmp / 'misterzine-jellyfin.log').open('a') as log:
            log.write('home: dial tcp %s:8096: connect: connection refused\n' % lan)
        text = self.report()
        self.assertIn('connect: connection refused', text)
        self.assertIn('"scheme": "http"', text)
        self.assertNotIn(lan, text)

    def test_report_trims_long_logs_to_the_limit(self):
        (self.tmp / 'jfplay.log').write_text('x' * 200 + '\n' + 'a line of playback\n' * 20000)
        text = self.report()
        self.assertLessEqual(len(text.encode()), manager.REPORT_MAX_BYTES)
        self.assertIn('a line of playback', text)
        self.assertIn('watch: header wiped', text)

    @mock.patch.object(manager, 'REPORT_SERVICE', 'https://reports.invalid')
    def test_send_report_reads_the_code_and_words_failures(self):
        import io
        import urllib.error

        class Answer(io.BytesIO):
            def __enter__(self):
                return self

            def __exit__(self, *a):
                pass
        seen = {}

        def ok(req, timeout):
            seen['body'] = req.data
            seen['type'] = req.get_header('Content-type')
            return Answer(b'{"code":"K7M4"}')
        self.assertEqual(manager.send_report('MisterZine report v1\n', '0.1.0-beta.9', ok), 'K7M4')
        self.assertEqual(seen['body'], b'MisterZine report v1\n')
        self.assertEqual(seen['type'], 'text/plain; charset=utf-8')

        def busy(req, timeout):
            raise urllib.error.HTTPError(req.full_url, 429, 'busy', {}, None)
        with self.assertRaisesRegex(manager.ReportError, 'Too many reports'):
            manager.send_report('x', opener=busy)

        def offline(req, timeout):
            raise urllib.error.URLError('no route')
        with self.assertRaisesRegex(manager.ReportError, 'offline'):
            manager.send_report('x', opener=offline)
        with self.assertRaisesRegex(manager.ReportError, 'no code'):
            manager.send_report('x', opener=lambda req, timeout: Answer(b'{"code":"0O"}'))

    def test_diagnostics_saves_the_report_without_uploading(self):
        with mock.patch.object(manager, 'build_report', return_value='MisterZine report v1\nApp: x\n'), \
                mock.patch.object(manager, 'send_report', side_effect=AssertionError('uploaded')):
            out, code, problem = manager.diagnostics(self.root)
        self.assertEqual(out, self.root / 'report.txt')
        self.assertEqual(out.read_text(), 'MisterZine report v1\nApp: x\n')
        self.assertEqual((code, problem), ('', ''))

    def test_report_service_off_is_worded(self):
        with self.assertRaisesRegex(manager.ReportError, 'unavailable for MisterZine Jellyfin'):
            manager.send_report('x')

    def test_account_name_never_reaches_the_report(self):
        for key in ('user_name', 'account_name'):       # account_name: older files
            with self.subTest(key=key):
                (self.root / 'plexcrt.json').write_text(json.dumps({'server_token': 'TESTtoken', key: 'TESTaccount'}))
                (self.tmp / 'misterzine-jellyfin.log').write_text('signed in as TESTaccount\n')
                self.assertNotIn('TESTaccount', self.report())

    def test_diagnostics_entry_always_waits_for_a_key(self):
        manager.wrappers(self.card)
        diag = (self.card / 'Scripts/MisterZine-Jellyfin-Diagnostics.sh').read_text()
        self.assertIn('if true; then read -r -p', diag)
        rollback = (self.card / 'Scripts/MisterZine-Jellyfin-Rollback.sh').read_text()
        self.assertIn('if [ "$result" -ne 0 ]; then read -r -p', rollback)

    def test_report_reads_the_active_alternative_ini_by_number(self):
        (self.card / 'MiSTer_Bench.ini').write_text('[MiSTer]\nvrr_mode=2\n[MisterZine Jellyfin]\nvideo_mode=8\n')
        facts = manager.system_facts(self.root, [], self.proc.parent, altcfg=lambda: 1)
        self.assertEqual(facts['ini'], 'alternative 1')
        self.assertEqual(facts['video_settings'], {'MiSTer': {'vrr_mode': '2'}, 'MisterZine Jellyfin': {'video_mode': '8'}})
        self.assertEqual(facts['ini_video'], {'vrr_mode': '2', 'video_mode': '8', 'vrr': 'forced', 'hdmi_hz': None,
                                               'hdmi_mode': '', 'dvi': False, 'overridden': [], 'conditional': [],
                                               'own_section': True, 'section_in': ''})
        self.assertNotIn('Bench', json.dumps(facts))
        facts = manager.system_facts(self.root, [], self.proc.parent, altcfg=lambda: None)
        self.assertEqual(facts['ini'], 'unknown')
        self.assertEqual(facts['ini_video']['hdmi_hz'], 60.0)          # MiSTer.ini stands in
        self.assertEqual(facts['ini_video']['section_in'], 'alternative 1')
        self.assertNotIn('Bench', json.dumps(facts))


class DisplayCheckTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.card = Path(self.temp.name)

    def check(self, text, altcfg=lambda: 0):
        (self.card / 'MiSTer.ini').write_text(text)
        return manager.display_check(self.card, altcfg)

    def assertFound(self, check, **expected):
        self.assertEqual({key: check.get(key) for key in expected}, expected)

    def test_the_plex_section_overrides_the_global_setting(self):
        self.assertFound(self.check('[MiSTer]\nvrr_mode=2\n'), ini='MiSTer.ini', vrr='forced', vrr_mode=2)
        self.assertEqual(self.check('[MiSTer]\nvrr_mode=2\n[MisterZine Jellyfin]\nvrr_mode=0\n')['vrr'], 'off')
        # main reads top to bottom: a [MiSTer] section after the core's wins
        self.assertEqual(self.check('[misterzine plex core]\nvrr_mode=0\n[MiSTer]\nvrr_mode=3\n')['vrr'], 'forced')
        self.assertEqual(self.check('[SNES]\nvrr_mode=2\n[MiSTer]\nvideo_mode=8\n')['vrr'], 'off')
        # what the app's notes suggest: a second Plex section at the end
        self.assertEqual(self.check('[MisterZine Jellyfin]\nvideo_mode=8\n[MiSTer]\nvrr_mode=2\n'
                                    '[MisterZine Jellyfin]\nvrr_mode=0\n')['vrr'], 'off')

    def test_main_drops_vrr_with_vsync_adjust_or_direct_video(self):
        self.assertEqual(self.check('[MiSTer]\nvrr_mode=2\nvsync_adjust=1\n')['vrr'], 'off')
        self.assertEqual(self.check('[MiSTer]\nvrr_mode=4\ndirect_video=1\n')['vrr'], 'off')
        # direct_video=2 is direct video only for a VGA converter main recognises at start-up
        self.assertEqual(self.check('[MiSTer]\nvrr_mode=2\ndirect_video=2\n')['vrr'], 'unknown')
        self.assertEqual(self.check('[MiSTer]\nvrr_mode=2\ndirect_video=2\nvsync_adjust=1\n')['vrr'], 'off')
        self.assertEqual(self.check('[MiSTer]\nvrr_mode=1\n')['vrr'], 'auto')
        self.assertEqual(self.check('[MiSTer]\nvideo_mode=8\n')['vrr'], 'off')

    def test_lines_are_read_as_main_reads_them(self):
        self.assertEqual(self.check('﻿[MiSTer]\r\n  vrr_mode = 2 ; forced\r\n')['vrr_mode'], 2)
        self.assertEqual(self.check('vrr_mode=2\n[MiSTer]\n')['vrr'], 'off')           # before any section
        self.assertEqual(self.check('[MiSTer]\n;vrr_mode=2\n')['vrr'], 'off')
        self.assertEqual(self.check('[MisterZine*]\nvrr_mode=2\n')['vrr'], 'forced')
        self.assertEqual(self.check('[Genesis]\n+MisterZine Jellyfin\nvrr_mode=2\n')['vrr'], 'forced')
        self.assertEqual(self.check('[MiSTer]\nvrr_mode=9\n')['vrr_mode'], 4)            # clamped
        self.assertEqual(self.check('[MiSTer]\nvrr_mode=0x2\n')['vrr_mode'], 2)
        self.assertEqual(self.check('[MiSTer]\nvrr_mode=on\n')['vrr'], 'off')

    def test_the_ini_main_chose_is_the_one_read(self):
        for name in ('MiSTer_zeta.ini', 'MiSTer_Alpha.ini', 'notes.ini'):
            (self.card / name).write_text('[MiSTer]\nvrr_mode=2\n')
        self.assertFound(self.check('[MiSTer]\n', altcfg=lambda: 2), ini='MiSTer_zeta.ini', vrr='forced', vrr_mode=2)
        self.assertEqual(self.check('[MiSTer]\n', altcfg=lambda: 0)['vrr'], 'off')
        self.assertEqual(self.check('[MiSTer]\n', altcfg=lambda: 7)['ini'], 'MiSTer.ini')
        self.assertEqual(self.check('[MiSTer]\n', altcfg=lambda: 3), {'ini': '', 'vrr': 'unknown'})
        self.assertEqual(self.check('[MiSTer]\n', altcfg=lambda: None), {'ini': '', 'vrr': 'unknown'})

    def test_without_alternatives_main_memory_is_not_read(self):
        self.assertEqual(self.check('[MiSTer]\nvrr_mode=2\n', altcfg=mock.Mock(side_effect=AssertionError))['vrr'], 'forced')
        (self.card / 'MiSTer.ini').unlink()
        self.assertEqual(manager.display_check(self.card, lambda: 0),
                         {'ini': 'MiSTer.ini', 'vrr': 'off', 'vrr_mode': 0, 'vsync_adjust': 0, 'hdmi_hz': None,
                          'hdmi_mode': '', 'dvi': False, 'overridden': [], 'conditional': []})

    def test_video_mode_refresh_follows_main_parsing(self):
        refresh = manager.video_mode_refresh
        self.assertEqual([refresh(v) for v in ('8', '9', '3', '7', '12', '99', '0x9', '9,pr')],
                         [60.0, 50.0, 50.0, 50.0, 60.0, 60.0, 50.0, 50.0])
        self.assertEqual(refresh('1920,1080,50'), 50.0)
        self.assertEqual(refresh('1920,1080,59.94,cvt'), 59.94)
        self.assertEqual(refresh('1280,110,40,220,720,5,5,20,74250,+hsync,-vsync'), 60.0)
        self.assertEqual(refresh('1920,528,44,148,1080,4,5,36,148500'), 50.0)
        self.assertEqual(refresh('9,oops'), 60.0)           # main rejects it and falls back to 60 Hz
        self.assertEqual(refresh('1920,1080'), 60.0)
        self.assertIsNone(refresh(','.join(['1'] * 21)))
        self.assertEqual(manager.parse_video_mode(''), (False, None))
        self.assertEqual(manager.parse_video_mode('9,oops'), (False, None))

    def test_fixed_hdmi_refresh(self):
        hz = lambda text: self.check('[MiSTer]\n' + text)['hdmi_hz']
        self.assertFound(self.check('[MiSTer]\nvideo_mode=9\n'), hdmi_hz=50.0, hdmi_mode='video_mode=9', vsync_adjust=0)
        self.assertFound(self.check('[MiSTer]\nvideo_mode=8\nvsync_adjust=1\nvideo_mode_pal=9\n'),
                         hdmi_hz=50.0, hdmi_mode='video_mode_pal=9', vsync_adjust=1)
        self.assertEqual(self.check('[MiSTer]\nvideo_mode=9\n[MisterZine Jellyfin]\nvideo_mode=8\n')['hdmi_hz'], 60.0)
        # HDMI follows the core with vsync_adjust or forced VRR; direct video is the core's own timing
        for extra in ('vsync_adjust=1', 'direct_video=1', 'vrr_mode=2'):
            self.assertIsNone(hz('video_mode=9\n' + extra + '\n'), extra)
        self.assertIsNone(hz('video_mode=9\ndirect_video=2\n'))            # decided at start-up
        self.assertEqual(hz('video_mode=9\nvrr_mode=1\n'), 50.0)             # the note hedges this one
        self.assertIsNone(hz('vscale_mode=0\n'))                             # the display's own mode
        self.assertEqual(hz('video_mode_ntsc=8\n'), 60.0)                    # main's default mode
        # vsync_adjust gives up when the refresh bounds leave out 59.94 Hz
        self.assertEqual(hz('video_mode=9\nvsync_adjust=1\nrefresh_max=55\n'), 50.0)
        self.assertEqual(hz('video_mode=9\nvsync_adjust=1\nrefresh_min=60.5\n'), 50.0)
        self.assertIsNone(hz('video_mode=9\nvsync_adjust=1\nrefresh_min=50\nrefresh_max=61\n'))
        # a PAL mode without an NTSC one is used for a 60 Hz core as it is
        self.assertEqual(hz('video_mode=8\nvsync_adjust=1\nvideo_mode_pal=9\n'), 50.0)
        self.assertIsNone(hz('video_mode=8\nvsync_adjust=1\nvideo_mode_pal=9\nvideo_mode_ntsc=8\n'))
        self.assertEqual(hz('video_mode=8\nvsync_adjust=2\nvideo_mode_ntsc=7\nrefresh_max=55\n'), 50.0)
        # or when the pixel clock 59.94 Hz would take leaves 2-300 MHz: 2560 x 2031 lines needs 311.7 MHz
        found = self.check('[MiSTer]\nvsync_adjust=1\nvideo_mode=1920,100,100,440,1800,50,50,131,259968\n')
        self.assertFound(found, hdmi_hz=50.0, hdmi_mode='video_mode=1920,100,100,440,1800,50,50,131,259968')
        self.assertIsNone(hz('vsync_adjust=1\nvideo_mode=1920,528,44,148,1080,4,5,36,148500\n'))
        self.assertIsNone(hz('vsync_adjust=1\nvideo_mode=3840,2160,50\n'))   # CVT timing: not worked out here

    def test_video_mode_sections_make_findings_unknown(self):
        # main applies [video=...] by the core's measured mode, not known before its video runs
        found = self.check('[MiSTer]\nvrr_mode=2\nvideo_mode=9\ndvi_mode=1\n[video=720x480@59.9]\nvrr_mode=0\n')
        self.assertFound(found, vrr='unknown', hdmi_hz=None, dvi=True, conditional=['vrr_mode'])
        found = self.check('[MiSTer]\ndvi_mode=1\nvideo_mode=9\n[Video=720x480]\ndvi_mode=0\n')
        self.assertFound(found, dvi=None, hdmi_hz=50.0, conditional=['dvi_mode'])
        # a later line outside video sections settles the value whether or not the section applies
        found = self.check('[video=720x480]\nvrr_mode=0\ndvi_mode=0\n[MiSTer]\nvrr_mode=2\ndvi_mode=1\n')
        self.assertFound(found, vrr='forced', dvi=True, conditional=[])
        # only the settings the checks use count, so junk cannot swell what the app is handed
        found = self.check('[MiSTer]\nvrr_mode=2\n[video=720x480]\n' + 'k' * 5000 + '=1\nbootscreen=0\n')
        self.assertFound(found, vrr='forced', conditional=[])
        self.assertEqual(len(manager.ini_line('k' * 5000 + '=1')), 1023)          # main's line limit
        self.assertEqual(manager.section_scope('vid=640x480]'), 'video')     # strncasecmp up to the '='
        self.assertIsNone(manager.section_scope('videos=640x480]'))
        self.assertIsNone(manager.section_scope('SNES]'))

    def test_dvi_mode(self):
        self.assertTrue(self.check('[MiSTer]\ndvi_mode=1\n')['dvi'])
        self.assertFalse(self.check('[MiSTer]\ndvi_mode=1\n[MisterZine Jellyfin]\ndvi_mode=0\n')['dvi'])
        self.assertFalse(self.check('[MiSTer]\n;dvi_mode=1\n')['dvi'])

    def test_plex_settings_a_later_mister_section_undoes(self):
        text = ('[MisterZine Jellyfin]\nvideo_mode=8\nvrr_mode=0\nvscale_mode=0\n'
                '[MiSTer]\nvideo_mode=9\nvrr_mode=0\nvrr_mode=2\n')
        self.assertFound(self.check(text), overridden=['video_mode', 'vrr_mode'], hdmi_hz=None, vrr='forced')
        self.assertEqual(self.check('[MiSTer]\nvideo_mode=9\n[MisterZine Jellyfin]\nvideo_mode=8\n')['overridden'], [])
        # the same value again is no override, however it is spelled
        self.assertEqual(self.check('[MisterZine Jellyfin]\nvideo_mode=8\n[MiSTer]\nvideo_mode=8\n')['overridden'], [])
        self.assertEqual(self.check('[MisterZine Jellyfin]\nvrr_mode=0x2\nrefresh_max=61\n'
                                    '[MiSTer]\nvrr_mode=2\nrefresh_max=61.0\n')['overridden'], [])
        self.assertEqual(self.check('[MisterZine Jellyfin]\nvrr_mode=9\n[MiSTer]\nvrr_mode=4\n')['overridden'], [])
        self.assertEqual(self.check('[MisterZine Jellyfin]\nvideo_mode=0x9\nvideo_mode_ntsc=1920,1080,60,CVT\n'
                                    '[MiSTer]\nvideo_mode=9\nvideo_mode_ntsc=1920,1080,60.0,cvt\n')['overridden'], [])
        self.assertEqual(self.check('[MisterZine Jellyfin]\nvideo_mode=99\n[MiSTer]\nvideo_mode=0\n')['overridden'], [])
        # flags as main leaves them: the last of a pair wins; a numbered mode ignores blanking flags
        for own, later in (('9,+hsync,+vsync', '9,+vsync,+hsync'), ('9,-hsync,+hsync', '9,+hsync'), ('9,cvt,pr', '9'),
                           ('1280,110,40,220,720,5,5,20,74250,1,1,-vsync', '1280,110,40,220,720,5,5,20,74250,1,1')):
            self.assertEqual(self.check('[MisterZine Jellyfin]\nvideo_mode=%s\n[MiSTer]\nvideo_mode=%s\n' % (own, later))
                             ['overridden'], [], (own, later))
        self.assertEqual(self.check('[MisterZine Jellyfin]\nvideo_mode=9,+hsync\n[MiSTer]\nvideo_mode=9\n')['overridden'],
                         ['video_mode'])
        # a timing keeps its blanking flag, which vscale_mode 4 and 5 use
        timing = '1280,110,40,220,720,5,5,20,74250'
        self.assertEqual(self.check('[MisterZine Jellyfin]\nvscale_mode=4\nvideo_mode=%s,cvt\n[MiSTer]\nvideo_mode=%s,cvtrb\n'
                                    % (timing, timing))['overridden'], ['video_mode'])
        self.assertEqual(self.check('[MisterZine Jellyfin]\nvideo_mode=1920,1080,60\n'
                                    '[MiSTer]\nvideo_mode=1920,1080,50\n')['overridden'], ['video_mode'])
        # settings main does not know, or these checks do not use, are not flagged
        self.assertEqual(self.check('[MisterZine Jellyfin]\nvideo_mdoe=8\nbootscreen=0\n'
                                    '[MiSTer]\nvideo_mdoe=9\nbootscreen=1\n')['overridden'], [])
        # nor ones a video mode section may change again
        self.assertEqual(self.check('[MisterZine Jellyfin]\nvideo_mode=8\n[MiSTer]\nvideo_mode=9\n'
                                    '[video=720x480]\nvideo_mode=8\n')['overridden'], [])
        # but a video section in between changes nothing about a later [MiSTer] line
        self.assertEqual(self.check('[MisterZine Jellyfin]\nvideo_mode=8\n[video=720x480]\nvideo_mode=7\n'
                                    '[MiSTer]\nvideo_mode=9\n')['overridden'], ['video_mode'])

    def test_plex_section_in_another_ini_is_for_reports_only(self):
        # a CRT INI and an HDMI INI often differ on purpose: Options are not told
        (self.card / 'MiSTer_crt.ini').write_text('[MiSTer]\nvideo_mode=9\n')
        (self.card / 'MiSTer.ini').write_text('[MiSTer]\n[MisterZine Jellyfin]\nvideo_mode=8\n')
        self.assertEqual(manager.section_elsewhere(self.card, self.card / 'MiSTer_crt.ini'), 'MiSTer.ini')
        self.assertNotIn('section_in', manager.display_check(self.card, lambda: 1))
        (self.card / 'MiSTer_crt.ini').write_text('[MiSTer]\n[MisterZine Jellyfin]\nvideo_mode=8\n')
        (self.card / 'MiSTer.ini').write_text('[MiSTer]\n')
        self.assertEqual(manager.section_elsewhere(self.card, self.card / 'MiSTer.ini'), 'MiSTer_crt.ini')
        self.assertEqual(manager.ini_label(self.card, 'MiSTer_crt.ini'), 'alternative 1')

    def test_altcfg_reads_main_signature(self):
        mem = self.card / 'mem'
        page = bytearray(4096)
        mem.write_bytes(bytes(page))
        self.assertEqual(manager.read_altcfg(str(mem), address=0), 0)
        page[0xF04:0xF08] = b'\x34\x99\xba\x02'
        mem.write_bytes(bytes(page))
        self.assertEqual(manager.read_altcfg(str(mem), address=0), 2)
        self.assertIsNone(manager.read_altcfg(str(self.card / 'missing')))

    def test_display_env_never_fails(self):
        with mock.patch.object(manager, 'display_check', side_effect=RuntimeError('boom')), \
                mock.patch.object(manager, 'trace') as trace:
            self.assertEqual(manager.display_env(self.card), '')
        trace.assert_called_once_with('display check failed: RuntimeError')
        (self.card / 'MiSTer.ini').write_text('[MiSTer]\nvrr_mode=2\n')
        with mock.patch.object(manager, 'trace') as trace:
            self.assertFound(json.loads(manager.display_env(self.card)), ini='MiSTer.ini', vrr='forced', vrr_mode=2)
        trace.assert_called_once_with('display check: vrr forced, hdmi None Hz, dvi False, overridden none')
        with mock.patch.object(manager, 'trace') as trace, mock.patch.object(manager, 'DISPLAY_ENV_MAX', 20):
            self.assertEqual(manager.display_env(self.card), '')
        self.assertTrue(trace.call_args[0][0].startswith('display check left out: '))


if __name__ == '__main__':
    unittest.main()
