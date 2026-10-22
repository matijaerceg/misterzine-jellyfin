import unittest
from check_public_content import issues


class ContentTests(unittest.TestCase):
    def test_private_overlay_is_rejected(self):
        for name, data in [('access-codes.json', b'{}'), ('PRIVATE_ASSEMBLY.json', b'{}'),
                           ('app/extra.go', b'//go:build premium\n\npackage app')]:
            self.assertIn('private supporter implementation or registry', issues(name, data))
        self.assertEqual(issues('app/free.go', b'//go:build !premium\n\npackage app'), [])

    def test_internal_state_rejected(self):
        for name in ('HANDOFF.md', 'docs/M2_RESULTS.md', 'release/private-beta/key.zip', 'beta.key', 'plexcrt.json.bak', '.env'):
            self.assertTrue(issues(name, b''), name)

    def test_safe_examples_and_source_allowed(self):
        self.assertEqual(issues('.env.example', b'JELLYFIN_TOKEN=example'), [])
        self.assertEqual(issues('app/player.go', b'// frame ownership'), [])
        for address in (b'http://127.0.0.1:8096', b'http://192.0.2.5:8096',
                        b'http://198.51.100.5:8096', b'http://203.0.113.5:8096'):
            self.assertEqual(issues('docs/example.md', address), [], address)

    def test_home_directories_stay_private(self):
        for path in (b'/home/' + b'example/project', b'/home/' + b'users/private'):
            self.assertIn('personal machine path or device address', issues('notes.txt', path))

    def test_private_content_rejected_without_printing_it(self):
        path = b'C:' + b'/Users/' + b'example/project'
        self.assertIn('personal machine path or device address', issues('README.md', path))
        token = b'ghp_' + b'a' * 36
        self.assertIn('possible credential', issues('settings.txt', token))
        server = b'http://' + b'8.8.8.8:8096'
        self.assertIn('literal server address; use configuration', issues('client.py', server))
        self.assertIn('personal machine path or device address',
                      issues('settings.ini', b'192.168.' + b'1.10'))


if __name__ == '__main__':
    unittest.main()
