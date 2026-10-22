"""M05 multiline native exact-label oracle regressions, no device required."""
import re
import unittest
from unittest.mock import patch, MagicMock

import music_release_e2e as release


class ReleaseLabelTest(unittest.TestCase):
    def test_multiline_unicode_uses_exact_android_label_convention(self):
        pattern = release.field_pattern('tape op', 'A; B\n東京 / C')
        self.assertRegex('tape op: A; B / 東京 / C', pattern)
        for wrong in ['tape op: A; B', 'tape op: A; B / WRONG', 'tape op: A; B / 東京 / C trailing', 'Other: A; B / 東京 / C']:
            self.assertIsNone(re.search(pattern, wrong))

    def test_missing_participant_is_a_failure_not_a_partial_pass(self):
        with patch.object(release.adrv, 'find', return_value=None), patch.object(release.app_ui, 'swipe'), patch.object(release.app_ui, 'labels', return_value=['tape op: A; B']):
            with self.assertRaises(AssertionError):
                release.fields({'tape op': 'A; B\n東京 / C'})

    def test_visible_complete_participants_do_not_need_swipes(self):
        node = {'b':[0,200,900,400]}
        with patch.object(release.adrv, 'find', return_value=node) as find, patch.object(release.app_ui, 'swipe') as swipe:
            release.fields({'Label':'A; B\n東京'})
            self.assertEqual(find.call_args.args[0], release.field_pattern('Label', 'A; B\n東京'))
            swipe.assert_not_called()

    def test_capture_uses_visible_play_and_real_pause_without_animated_dump(self):
        run = MagicMock()
        item = {'id': 1, 'title': 'Release Metadata 東京'}
        playing = {'state':'PLAYING', 'title':item['title']}
        paused = dict(playing, state='PAUSED')
        with patch.object(release.baseline, 'reset_progress'), \
                patch.object(release.baseline, 'session_state', side_effect=[playing, playing, paused, paused]), \
                patch.object(release.baseline.audio_trace, 'analyze', return_value={'segments':[]}), \
                patch.object(release.adrv, 'tap', return_value=True) as tap, \
                patch.object(release.adrv, 'adb') as adb, \
                patch.object(release.app_ui, 'labels', return_value=[]), \
                patch.object(release.time, 'sleep'):
            release.capture_source(run, item)
        tap.assert_called_once_with('^Play Audio$')
        adb.assert_called_once_with('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')


if __name__ == '__main__':
    unittest.main()
