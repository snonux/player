"""Device-free M46 oracle checks. These never stand in for platform acceptance."""
import copy
import unittest
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
from unittest.mock import patch

import music_downloads_e2e as downloads


class DownloadOracles(unittest.TestCase):
    def row(self, ident=3, **updates):
        return dict({'origin': 'http://127.0.0.1:18766', 'user_id': 2, 'media_id': ident,
                     'key': 'a' * 64, 'download_profile': 'original', 'size_bytes': 45}, **updates)

    def verify(self, rows, ids=(3,)):
        return downloads.original_rows(rows, 'http://127.0.0.1:18766', 2, ids)

    def test_saved_row_oracle_rejects_wrong_account_origin_quality_missing_duplicate_and_empty(self):
        good = self.row()
        self.assertEqual(self.verify([good]), [good])
        for rows in [[], [self.row(user_id=1)], [self.row(origin='http://elsewhere')],
                     [self.row(download_profile='aac128')], [self.row(size_bytes=0)],
                     [self.row(key='../../source')], [good, copy.copy(good)]]:
            with self.subTest(rows=rows), self.assertRaises(AssertionError):
                self.verify(rows)
        self.assertEqual(self.verify([self.row(4), good], (3, 4)), [self.row(4), good])
        self.assertEqual(self.verify([self.row(4), good]), [good])
        with self.assertRaises(AssertionError):
            self.verify([good], (3, 4))

    def test_only_actual_download_paths_are_fault_targets(self):
        for path in ['/api/media/12/download', '/api/v1/media/12/download?profile=original', '/api/v1/media/12/compat']:
            self.assertEqual(downloads.media_id(path), 12)
        for path in ['/api/v1/media/12/stream', '/api/v1/media/12', '/s/secret/download',
                     '/api/music/downloads', '/api/v1/media/12/download/extra']:
            self.assertIsNone(downloads.media_id(path))

    def test_audio_oracle_requires_matching_advancing_audible_source(self):
        good = {'segments': [{'tone_hz': 439.9, 'duration_s': 1.2}]}
        self.assertEqual(downloads.assert_audio(good, 440), good['segments'])
        for segments in [[], [{'tone_hz': 220, 'duration_s': 3}],
                         [{'tone_hz': 440, 'duration_s': .2}], [{'tone_hz': None, 'duration_s': 5}]]:
            with self.subTest(segments=segments), self.assertRaises(AssertionError):
                downloads.assert_audio({'segments': segments}, 440)

    def test_private_evidence_preserves_shared_versions_and_default_caller_still_uses_fixture_root(self):
        with tempfile.TemporaryDirectory(prefix='player-m46-evidence-test-') as directory:
            root = Path(directory)
            fixture = root / 'fixture'
            fixture.mkdir()
            shared = root / 'shared'
            (shared / 'android').mkdir(parents=True)
            sentinel = shared / 'android' / 'versions.json'
            sentinel.write_text('{"M08":"sentinel"}')
            private = root / 'm46-private'
            state = {'manifest_file': str(fixture / 'manifest.json'),
                     'android': {'adb_reverse_origin': 'http://127.0.0.1:18766'},
                     'server': {'version': '0.8.2', 'build_revision': 'test-only'},
                     'run_id': 'test-only', 'evidence_dir': str(shared), 'origin': 'http://127.0.0.1:18766'}
            (fixture / 'state.json').write_text(json.dumps(state))
            (fixture / 'manifest.json').write_text('{}')
            (fixture / 'auth.json').write_text(json.dumps({'admin': {'username': 'test', 'password': 'test-only'}}))
            args = SimpleNamespace(fixture_dir=str(fixture), host_alias=False, emulator_wav=None)
            with patch.object(downloads.baseline, 'device_versions', return_value={'test': 'device mocked'}), patch.object(downloads.baseline, 'Client'):
                isolated = downloads.baseline.Run(args, evidence_dir=private)
                self.assertEqual(isolated.evidence.directory, private / 'android')
                self.assertEqual(sentinel.read_text(), '{"M08":"sentinel"}')
                self.assertTrue((private / 'android' / 'versions.json').exists())
                default = downloads.baseline.Run(args)
                self.assertEqual(default.evidence.directory, shared / 'android')
                self.assertEqual(json.loads(sentinel.read_text())['test'], 'device mocked')

    def test_old_helper_rejects_evidence_keyword_before_any_evidence_write(self):
        entered = []
        def old_init(self, args):
            entered.append(True)
        with tempfile.TemporaryDirectory(prefix='player-m46-old-helper-') as directory:
            Path(directory, 'state.json').write_text(json.dumps({'origin': 'http://127.0.0.1:18766'}))
            with patch.object(downloads.baseline.Run, '__init__', old_init):
                with self.assertRaises(TypeError):
                    downloads.Run(SimpleNamespace(fixture_dir=directory, evidence_dir='/unused'))
        self.assertEqual(entered, [])


if __name__ == '__main__':
    unittest.main()
