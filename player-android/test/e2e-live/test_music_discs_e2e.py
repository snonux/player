"""M04 native combined-row identity oracle; no device required."""
import re
import unittest
from unittest.mock import patch

import music_discs_e2e as discs


class DiscRowTest(unittest.TestCase):
    def test_complete_actual_row_matches(self):
        pattern = discs.song_row_pattern('1-1', 'M04 Album 1-1')
        self.assertRegex('1-1 / M04 Album 1-1 / 0:18', pattern)
        self.assertRegex('2-1 / M04 Compilation 2-1 / 0:18',
                         discs.song_row_pattern('2-1', 'M04 Compilation 2-1'))

    def test_wrong_or_partial_identity_is_rejected(self):
        pattern = discs.song_row_pattern('2-1', 'M04 Album 2-1')
        for label in [
            '1-2 / M04 Album 2-1 / 0:18',
            '2-1 / M04 Compilation 2-1 / 0:18',
            '2-1 / M04 Album 2-1 / 0:12',
            '2-1', 'M04 Album 2-1', '2-1 / M04 Album 2-1',
            'prefix 2-1 / M04 Album 2-1 / 0:18',
            '2-1 / M04 Album 2-1 / 0:18 suffix',
        ]:
            with self.subTest(label=label):
                self.assertIsNone(re.search(pattern, label))

    def test_song_lookup_returns_the_actual_combined_row_node(self):
        node = {'b': [0, 100, 900, 250], 'text': '1-2 / M04 Album 1-2 / 0:18'}
        with patch.object(discs, 'visible', return_value=node) as visible:
            self.assertIs(discs.song_row('Album', '1-2'), node)
        visible.assert_called_once_with(discs.song_row_pattern('1-2', 'M04 Album 1-2'))

    def test_playback_touches_the_actual_rows_leading_column(self):
        row = {'b': [24, 100, 984, 340], 'text': '2-1 / M04 Album 2-1 / 0:18'}
        with patch.object(discs, 'song_row', return_value=row) as lookup, patch.object(discs.adrv, 'tap_node') as tap:
            discs.tap_song('Album', '2-1')
        lookup.assert_called_once_with('Album', '2-1')
        target = tap.call_args.args[0]
        self.assertEqual(target['b'], [24, 100, 104, 340])
        self.assertEqual(row['b'], [24, 100, 984, 340])

    def test_exact_placeholder_token_accepts_standalone_and_actual_merged_label(self):
        pattern = discs.artwork_placeholder_pattern()
        for label in ['No album artwork', 'No album artwork / M04 Missing Art / 1 songs · 0:12',
                      'M04 Missing Art / No album artwork / 1 songs · 0:12']:
            with self.subTest(label=label):
                self.assertRegex(label, pattern)
        for label in ['No disc artwork', 'No album artworks', 'Wrong No album artwork',
                      'No album artwork unavailable', 'Artwork for M04 Missing Art']:
            with self.subTest(label=label):
                self.assertIsNone(re.search(pattern, label))

    def test_broken_image_label_requires_correct_disc_and_complete_subtitle(self):
        actual = 'No album artwork / Disc 2 — ' + discs.SUBTITLE.strip()
        pattern = discs.broken_disc_pattern()
        self.assertRegex(actual, pattern)
        for wrong in [actual.replace('Disc 2', 'Disc 1'), actual.replace('Encore 東京', 'Other'),
                      actual.replace('No album artwork / ', ''), actual[:-10], actual + ' trailing']:
            with self.subTest(label=wrong):
                self.assertIsNone(re.search(pattern, wrong))


if __name__ == '__main__':
    unittest.main()
