"""Regression tests for the native runner's saved-login acceptance oracle."""
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import music_e2e


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
