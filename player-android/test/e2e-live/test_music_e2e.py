"""Regression tests for the native runner's saved-login acceptance oracle."""
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import music_e2e


class MediaTitleTest(unittest.TestCase):
    def test_tagged_and_old_audio_labels_preserve_filename_identity(self):
        original = {'id': 7, 'type': 'audio', 'file_name': 'sample-mp3.mp3'}
        self.assertEqual(music_e2e.app_ui.media_title(original), 'sample-mp3')
        tagged = dict(original, music={'title': ' 東京 Tagged Song '})
        self.assertEqual(music_e2e.app_ui.media_title(tagged), '東京 Tagged Song')
        self.assertEqual(tagged['file_name'], original['file_name'])
        self.assertEqual(tagged['id'], original['id'])

    def test_blank_tags_and_non_audio_keep_correct_fallbacks(self):
        self.assertEqual(music_e2e.app_ui.media_title({'type':'audio', 'file_name':'01-name.flac', 'music':{'title':' \t '}}), '01-name')
        self.assertEqual(music_e2e.app_ui.media_title({'type':'video', 'file_name':'clip.mp4'}), 'clip.mp4')
        self.assertEqual(music_e2e.app_ui.media_title({'type':'audio', 'file_name':'.mp3'}), '.mp3')


class FlattenedFolderNavigationTest(unittest.TestCase):
    def test_missing_single_file_folder_requires_visible_target(self):
        target = {'b': [0, 200, 900, 500]}
        with patch.object(music_e2e.app_ui, 'go_home', return_value=True), \
                patch.object(music_e2e.adrv, 'tap'), \
                patch.object(music_e2e.adrv, 'find', return_value=None), \
                patch.object(music_e2e.app_ui, 'find_scrolling', side_effect=[None, target]), \
                patch.object(music_e2e, 'find_visible', side_effect=[target, target]), \
                patch.object(music_e2e.adrv, 'tap_node') as tap, \
                patch.object(music_e2e.time, 'sleep'):
            self.assertTrue(music_e2e.open_track('Music', 'Untagged', 'track_0042'))
        tap.assert_called_once_with(target)

    def test_missing_folder_without_target_remains_failure(self):
        with patch.object(music_e2e.app_ui, 'go_home', return_value=True), \
                patch.object(music_e2e.adrv, 'tap'), \
                patch.object(music_e2e.adrv, 'find', return_value=None), \
                patch.object(music_e2e.app_ui, 'find_scrolling', return_value=None), \
                patch.object(music_e2e, 'find_visible', return_value=None), \
                patch.object(music_e2e.adrv, 'tap_node') as tap, \
                patch.object(music_e2e.time, 'sleep'):
            self.assertFalse(music_e2e.open_track('Music', 'Untagged', 'track_0042'))
        tap.assert_not_called()


class MetadataPlaybackOracleTest(unittest.TestCase):
    def test_failed_restored_title_navigation_cannot_skip_audio_acceptance(self):
        records = []
        run = SimpleNamespace(record=lambda name, passed, *args, **kwargs: records.append((name, passed)))
        with patch.object(music_e2e, 'open_track', return_value=False), \
                patch.object(music_e2e, 'play_and_capture') as play:
            self.assertFalse(music_e2e.step_metadata_audio(run, {'title': 'Restored title'}))
        self.assertEqual(len(records), 2)
        self.assertTrue(all(not passed for _, passed in records))
        self.assertIn('session', records[0][0])
        self.assertIn('tone', records[1][0])
        play.assert_not_called()


class ConnectivityPreflightTest(unittest.TestCase):
    def test_connected_android_network_passes(self):
        self.assertTrue(music_e2e.has_online_network(
            '  NetworkAgentInfo{network{101} ni{WIFI CONNECTED extra: } '
            'nc{[ Transports: WIFI Capabilities: INTERNET&VALIDATED]}}'))

    def test_no_network_or_disconnected_agent_fails(self):
        for status in ('', 'NetworkRequest [ Capabilities: INTERNET ]',
                       'NetworkAgentInfo{ ni{WIFI DISCONNECTED} INTERNET }'):
            with self.subTest(status=status):
                self.assertFalse(music_e2e.has_online_network(status))

    def test_offline_preflight_stops_before_install_or_reverse(self):
        with patch.object(music_e2e.adrv, 'adb', return_value='') as adb:
            with self.assertRaisesRegex(SystemExit, 'Online emulator required'):
                music_e2e.prepare_device(SimpleNamespace(apk='fixture.apk'), 18776)
        self.assertEqual(adb.call_count, 1)


class CaptureTimingTest(unittest.TestCase):
    def test_buffering_does_not_consume_requested_playback_duration(self):
        run = SimpleNamespace(audio=SimpleNamespace(available=lambda: False))
        states = [{'state': 'CONNECTING'}, {'state': 'PLAYING'}, {'state': 'PLAYING'}]
        with patch.object(music_e2e, 'reset_progress'), \
                patch.object(music_e2e.adrv, 'tap'), \
                patch.object(music_e2e, 'session_state', side_effect=states), \
                patch.object(music_e2e.time, 'monotonic', side_effect=[100, 119]), \
                patch.object(music_e2e.time, 'sleep') as sleep:
            sessions, analysis, wav = music_e2e.play_and_capture(run, None, {'id': 1}, 'probe', seconds=7)
        self.assertEqual(sessions[-1]['state'], 'PLAYING')
        self.assertEqual(sleep.call_args_list[-1].args, (7,))
        self.assertIsNone(analysis)
        self.assertIsNone(wav)

    def test_buffering_timeout_is_not_reported_as_playing(self):
        run = SimpleNamespace(audio=SimpleNamespace(available=lambda: False))
        with patch.object(music_e2e, 'reset_progress'), \
                patch.object(music_e2e.adrv, 'tap'), \
                patch.object(music_e2e, 'session_state', return_value={'state': 'CONNECTING'}), \
                patch.object(music_e2e.time, 'monotonic', side_effect=[100, 121]), \
                patch.object(music_e2e.time, 'sleep') as sleep:
            sessions, _, _ = music_e2e.play_and_capture(run, None, {'id': 1}, 'probe')
        self.assertEqual(sessions[-1]['state'], 'CONNECTING')
        sleep.assert_not_called()


class ColdRelaunchTest(unittest.TestCase):
    def run_flow(self, *, chooser=True, saved=True, credential=True, signed_in=True):
        records = []
        server = {'text': 'http://127.0.0.1:18776'}
        fields = [{'cls': 'EditText', 'text': 'music-admin', 'desc': ''},
                  {'cls': 'EditText', 'text': '', 'desc': ''}]
        run = SimpleNamespace(base=server['text'], auth={'admin': {
            'username': 'music-admin', 'password': 'fixture-password'}},
            record=lambda name, ok, *args, **kwargs: records.append((name, bool(ok))),
            shot=lambda name: name + '.png')

        def find(pattern):
            if pattern == '^Library$':
                return None
            if pattern == '^Unlock saved login with fingerprint$':
                return server if credential else None
            return server if saved else None

        with patch.object(music_e2e.adrv, 'adb'), \
                patch.object(music_e2e.app_ui, 'launch'), \
                patch.object(music_e2e.time, 'sleep'), \
                patch.object(music_e2e.adrv, 'wait', side_effect=lambda pattern, **_: server if chooser else None), \
                patch.object(music_e2e.adrv, 'find', side_effect=find), \
                patch.object(music_e2e.adrv, 'nodes', return_value=fields), \
                patch.object(music_e2e.adrv, 'tap_node'), \
                patch.object(music_e2e.app_ui, 'sign_in', return_value=signed_in) as sign_in, \
                patch.object(music_e2e, 'labels', return_value=['Music', 'Restricted']):
            music_e2e.step_cold_relaunch(run)
            calls = sign_in.call_count
        return [ok for _, ok in records], calls

    def test_remembered_locked_login_requires_explicit_sign_in(self):
        self.assertEqual(self.run_flow(), ([True, True, True], 1))

    def test_direct_entry_or_missing_saved_server_fails_without_sign_in(self):
        for args in ({'chooser': False}, {'saved': False}):
            with self.subTest(args=args):
                self.assertEqual(self.run_flow(**args), ([False], 0))

    def test_lost_saved_credential_is_not_hidden_by_password_recovery(self):
        self.assertEqual(self.run_flow(credential=False), ([True, False], 0))

    def test_password_failure_stays_failed(self):
        self.assertEqual(self.run_flow(signed_in=False), ([True, True, False], 1))


if __name__ == '__main__':
    unittest.main()
