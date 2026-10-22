"""Cheap specification/encoder-input checks; real duration proof uses ffprobe."""
import tempfile
from pathlib import Path
import unittest
from unittest.mock import patch
import music_fixture
import music_library as library


class ShuffleFixtureTest(unittest.TestCase):
    def test_flag_is_optional_and_forwarded_to_same_encoder_manifest_specs(self):
        self.assertFalse(music_fixture.parse_args(['up','/tmp/player']).shuffle_playback)
        self.assertTrue(music_fixture.parse_args(['up','/tmp/player','--shuffle-playback']).shuffle_playback)
        original = [dict(spec) for spec in library.TRACKS]
        for enabled in [False,True]:
            encoded = []
            def track(root, spec):
                encoded.append(spec)
                return library._manifest_entry(spec,None)
            with tempfile.TemporaryDirectory() as directory, \
                    patch.object(library,'_make_track',side_effect=track), \
                    patch.object(library,'_make_gapless',return_value=[]), \
                    patch.object(library,'_make_gap_calibration',return_value=[]), \
                    patch.object(library,'_make_cover'), patch.object(library,'_write_playlist'):
                manifest=library.generate(Path(directory)/'media',shuffle_playback=enabled)
            night=[s for s in encoded if s['path'].startswith('Music/Aurora Lane - Night Transit (2021)/')]
            self.assertEqual(len(night),3)
            self.assertEqual([s.get('seconds',library.TRACK_SECONDS) for s in night],[45 if enabled else 12]*3)
            for spec in encoded:
                entry=next(t for t in manifest['tracks'] if t['path']==spec['path'])
                self.assertEqual(entry['duration_s'],spec.get('seconds',library.TRACK_SECONDS))
            self.assertEqual(library.TRACKS,original)

    def test_encoder_input_uses_duration_instead_of_only_changing_manifest(self):
        spec=dict(library.TRACKS[0],seconds=45)
        with tempfile.TemporaryDirectory() as directory, patch.object(library,'ffmpeg') as encode, \
                patch.object(library,'_write_lyrics'), patch.object(library,'set_flac_artists'):
            result=library._make_track(Path(directory),spec)
        arguments=encode.call_args.args
        self.assertTrue(any(':d=45' in str(value) for value in arguments),arguments)
        self.assertEqual(result['duration_s'],45)


if __name__=='__main__':
    unittest.main()
