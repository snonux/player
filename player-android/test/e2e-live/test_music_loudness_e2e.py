"""Negative waveform oracles; these tests are not native feature acceptance."""
from array import array
import math
from pathlib import Path
import tempfile
import unittest
import wave

from music_loudness_e2e import clipping_waveform


class ClippingOracleTest(unittest.TestCase):
    def check_signal(self, amplitude):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'signal.wav'
            values = array('h', (round(max(-1, min(1, amplitude * math.sin(
                2 * math.pi * 1000 * i / 44100))) * 32767) for i in range(3 * 44100)))
            with wave.open(str(path), 'wb') as output:
                output.setnchannels(1)
                output.setsampwidth(2)
                output.setframerate(44100)
                output.writeframes(values.tobytes())
            return clipping_waveform(path, {'start_s': 0, 'end_s': 3})

    def test_full_scale_sine_is_safe(self):
        self.assertAlmostEqual(self.check_signal(1)['crest_db'], 3.0103, places=2)

    def test_hard_clipped_overdrive_is_rejected(self):
        with self.assertRaises(AssertionError):
            self.check_signal(2.5)

    def test_unamplified_sine_cannot_pass_peak_ceiling_check(self):
        with self.assertRaises(AssertionError):
            self.check_signal(.5)


if __name__ == '__main__':
    unittest.main()
