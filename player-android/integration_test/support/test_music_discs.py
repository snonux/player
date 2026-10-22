"""Focused M04 fixture guard and real FLAC authoring checks; no server runtime."""
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

import music_discs


class DiscFixtureTest(unittest.TestCase):
    def test_augmentation_rejects_non_fixture_before_writes(self):
        with tempfile.TemporaryDirectory() as path:
            with self.assertRaises(ValueError):
                music_discs.augment(path)
            self.assertEqual(list(Path(path).iterdir()), [])

    def test_existing_namespace_is_never_overwritten(self):
        with tempfile.TemporaryDirectory() as path:
            root = Path(path)
            original = root / 'Music/M04/kept'
            original.parent.mkdir(parents=True)
            original.write_text('existing')
            with self.assertRaises(ValueError):
                music_discs.make_tracks(root)
            self.assertEqual(original.read_text(), 'existing')

    @unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'), 'Requires real fixture encoders')
    def test_real_generation_preserves_repeated_artists_and_embedded_picture(self):
        with tempfile.TemporaryDirectory() as path:
            tracks = music_discs.make_tracks(Path(path))
            self.assertEqual(len(tracks), 8)
            compilation = [entry for entry in tracks if entry['tags'].get('album') == music_discs.COMPILATION]
            self.assertEqual([entry['tags']['artists'] for entry in compilation], [['M04 A / B', 'M04 東京'], ['M04 Guest'], ['M04 A / B', 'M04 東京']])
            guest = Path(path) / 'Music/M04/Compilation/Disc 1/2.flac'
            self.assertIn(b'ARTIST=M04 Guest', guest.read_bytes())
            self.assertNotIn(b'ARTIST=M04 A / B', guest.read_bytes())
            source = Path(path) / music_discs.RETAG_PATH
            probe = json.loads(subprocess.check_output(['ffprobe', '-v', 'error', '-show_streams', '-show_format', '-of', 'json', str(source)]))
            self.assertTrue(any(s.get('disposition', {}).get('attached_pic') for s in probe['streams']))
            self.assertEqual(probe['format']['tags']['discsubtitle'].strip(), music_discs.SUBTITLE.strip())
            self.assertIn(b'ARTIST=M04 A / B', source.read_bytes())
            self.assertIn('ARTIST=M04 東京'.encode(), source.read_bytes())
            root = Path(path)
            (root / '.player-music-fixture').write_text('test')
            (root / 'state.json').write_text(json.dumps({'media_root': str(root / 'media')}))
            # Rehome generated media under the marker's confined media root.
            media = root / 'media'
            media.mkdir()
            for name in ('Music', 'Restricted'):
                (root / name).rename(media / name)
            source = media / music_discs.RETAG_PATH
            music_discs.retag(root, 'changed')
            music_discs.retag(root, 'broken')
            self.assertEqual(source.read_bytes()[:4], b'fLaC')
            music_discs.retag(root, 'cleared')
            probe = json.loads(subprocess.check_output(['ffprobe', '-v', 'error', '-show_format', '-of', 'json', str(source)]))
            self.assertNotIn('discsubtitle', probe['format']['tags'])
            music_discs.retag(root, 'initial')
            self.assertIn('ARTIST=M04 東京'.encode(), source.read_bytes())


if __name__ == '__main__':
    unittest.main()
