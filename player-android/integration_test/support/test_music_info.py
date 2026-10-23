"""Lightweight refusal checks; never generate files or start a server."""
import json
from pathlib import Path
import tempfile
import unittest

from music_info import guarded_fixture


class FixtureGuards(unittest.TestCase):
    def test_refuses_unmarked_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(ValueError, 'marked disposable'):
                guarded_fixture(directory)

    def test_refuses_remote_origin_and_unconfined_media(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / '.player-music-fixture').touch()
            (root / 'media').mkdir()
            for origin, media, message in [
                ('https://example.com', str(root / 'media'), 'loopback'),
                ('http://127.0.0.1:18816', str(root.parent), 'confined'),
                ('http://127.0.0.1:18816', str(root), 'confined'),
            ]:
                (root / 'state.json').write_text(json.dumps({'origin':origin, 'media_root':media}))
                with self.assertRaisesRegex(ValueError, message):
                    guarded_fixture(root)

    def test_accepts_confined_marked_loopback_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            media = root / 'media'
            media.mkdir()
            (root / '.player-music-fixture').touch()
            (root / 'state.json').write_text(json.dumps({'origin':'http://127.0.0.1:18816','media_root':str(media)}))
            self.assertEqual(guarded_fixture(root)[2], media.resolve())


if __name__ == '__main__':
    unittest.main()
