import tempfile
import unittest
import wave
from pathlib import Path

from music_filters import MIB, make_wave, augment


class ExactFilterFixtureTest(unittest.TestCase):
    def test_real_wave_preserves_duration_with_exact_binary_size(self):
        with tempfile.TemporaryDirectory() as directory:
            for seconds, mib in [(60, 1), (120, 2)]:
                path = Path(directory) / f'{seconds}.wav'
                make_wave(path, seconds, mib)
                self.assertEqual(path.stat().st_size, mib * MIB)
                with wave.open(str(path)) as stream:
                    self.assertEqual(stream.getnframes() / stream.getframerate(), seconds)
                    self.assertGreater(max(stream.readframes(400)), 0)

    def test_augmentation_rejects_unmarked_directory_before_writing(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(ValueError, 'marked disposable'):
                augment(directory)
            self.assertEqual(list(Path(directory).iterdir()), [])


if __name__ == '__main__':
    unittest.main()
