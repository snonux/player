import unittest
from unittest.mock import MagicMock, patch
import tempfile
from pathlib import Path
import json
import re
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import threading
from contextlib import closing
from podcast_sync_e2e import require_audio, require_resume, register_installation, select_row_action, PlayerProxy, Run, feed_label_pattern


class PodcastNativeOracleTests(unittest.TestCase):
    def test_feed_title_boundary_accepts_actual_merged_url_but_not_another_show(self):
        pattern = feed_label_pattern('Acceptance Alpha')
        self.assertIsNotNone(re.match(pattern, 'Acceptance Alpha / http://127.0.0.1:18776/feeds/alpha.xml'))
        self.assertIsNotNone(re.match(pattern, 'Acceptance Alpha'))
        self.assertIsNone(re.match(pattern, 'Acceptance Alphabet / another URL'))

    def run_without_device(self, directory):
        run = object.__new__(Run)
        run.directory = run.output = Path(directory)
        run.scope = 'account:http://fixture#1'
        run.episode = {'key': 'guid:one', 'title': 'Alpha Episode 001'}
        run.state = {'audio': {'resume': {'duration_s': 60, 'tone_hz': 441}}}
        run.audio = MagicMock()
        run.audio.available.return_value = True
        run.audio.save.return_value = run.output / 'not-platform-proof.wav'
        for name in ('login', 'touch', 'podcasts', 'episodes', 'row', 'no_server_audio', 'publish_transfer', 'record'):
            setattr(run, name, MagicMock())
        return run

    def test_production_resume_checks_first_native_position_despite_stale_saved_history(self):
        with tempfile.TemporaryDirectory() as directory:
            run = self.run_without_device(directory)
            (run.directory / 'transfer-A.json').write_text(json.dumps({'position_seconds': 12}))
            run.snapshot = MagicMock(return_value={'podcast_episode_state': [{
                'scope': run.scope, 'episode_key': 'guid:one', 'position_seconds': 12}]})
            analysis = {'audible_s': 3, 'segments': [{'duration_s': 3, 'tone_hz': 441}]}
            # Exercise Run.resume -> unmocked Run.playback, not only the scalar
            # helper: a zero player anchor with stale saved 12 must fail.
            with patch('podcast_sync_e2e.adrv.wait', return_value=True), \
                    patch('podcast_sync_e2e.app_ui.back'), \
                    patch('podcast_sync_e2e.time.sleep'), \
                    patch('podcast_sync_e2e.audio_trace.analyze', return_value=analysis), \
                    patch('podcast_sync_e2e.session_state', return_value={
                        'state': 'PLAYING', 'title': run.episode['title'], 'position_ms': 0}):
                with self.assertRaisesRegex(AssertionError, 'Resume differs'):
                    run.resume()
                run.snapshot.assert_not_called()
            with patch('podcast_sync_e2e.adrv.wait', return_value=True), \
                    patch('podcast_sync_e2e.app_ui.back'), \
                    patch('podcast_sync_e2e.time.sleep'), \
                    patch('podcast_sync_e2e.audio_trace.analyze', return_value=analysis), \
                    patch('podcast_sync_e2e.session_state', return_value={
                        'state': 'PLAYING', 'title': run.episode['title'], 'position_ms': 12000}):
                run.resume()
                run.snapshot.assert_called_once_with('resumed')
                playback = run.record.call_args_list[-2]
                self.assertEqual(playback.kwargs['first_playing']['position_ms'], 12000)

    def test_production_uncompletion_requires_position_register_to_survive(self):
        with tempfile.TemporaryDirectory() as directory:
            run = self.run_without_device(directory)
            before = {'scope': run.scope, 'episode_key': 'guid:one', 'position_seconds': 60,
                      'position_ms': 100, 'completed': 1, 'completion_source': 'auto'}
            after = {**before, 'completed': 0, 'completion_source': 'manual'}
            for seconds, valid in ((60, True), (0, False)):
                run.snapshot = MagicMock(side_effect=[
                    {'podcast_episode_state': [before]},
                    {'podcast_episode_state': [{**after, 'position_seconds': seconds}]}])
                with patch('podcast_sync_e2e.adrv.wait', return_value=True):
                    if valid:
                        run.uncomplete()
                    else:
                        with self.assertRaisesRegex(AssertionError, 'changed the saved position'):
                            run.uncomplete()

    def test_resume_requires_real_partial_baseline_and_retained_offset(self):
        require_resume(12, 16, 5, 60)
        for expected, actual in [(0, 4), (58, 0), (12, 0), (12, 50)]:
            with self.assertRaises(AssertionError):
                require_resume(expected, actual, 5, 60)

    def test_silence_wrong_source_and_short_output_cannot_pass(self):
        require_audio({'audible_s': 3, 'segments': [{'duration_s': 3, 'tone_hz': 441}]}, 441)
        for data in ({'audible_s': 0, 'segments': []},
                     {'audible_s': 3, 'segments': [{'duration_s': 3, 'tone_hz': 882}]},
                     {'audible_s': .5, 'segments': [{'duration_s': .5, 'tone_hz': 441}]}):
            with self.assertRaises(AssertionError):
                require_audio(data, 441)

    def test_two_clients_reject_same_device_or_changed_installation(self):
        identity = {'serial': 'emulator-5554', 'android_id': 'one', 'installed': 'first'}
        ledger = {}
        register_installation(ledger, 'A', identity)
        register_installation(ledger, 'A', identity)
        with self.assertRaises(AssertionError):
            register_installation(ledger, 'B', identity)
        with self.assertRaises(AssertionError):
            register_installation(ledger, 'A', {**identity, 'installed': 'reinstalled'})
        register_installation(ledger, 'B', {'serial': 'emulator-5556', 'android_id': 'two', 'installed': 'second'})

    def test_native_row_actions_must_belong_to_exact_episode(self):
        def node(text, top, click=True):
            return {'desc': '', 'text': text, 'b': [0, top, 500, top + 30], 'click': click}
        first = node('Download', 150)
        nodes = [node('Alpha Episode 001', 100), first,
                 node('Alpha Episode 002', 200), node('Download', 250)]
        self.assertIs(select_row_action(nodes, 'Alpha Episode 001', 'Download'), first)
        # Merged list semantics prefix progress, e.g. "0, Alpha Episode 001 / …".
        prefixed = [node('0, Alpha Episode 001 / Acceptance Alpha / Unplayed', 100), first,
                    node('0, Alpha Episode 002 / Acceptance Alpha / Unplayed', 200), node('Download', 250)]
        self.assertIs(select_row_action(prefixed, 'Alpha Episode 001', 'Download'), first)
        # FutureBuilder leaves Download non-clickable until copy() settles.
        with self.assertRaises(AssertionError):
            select_row_action([node('Alpha Episode 001', 100), node('Download', 150, click=False)],
                              'Alpha Episode 001', 'Download')
        with self.assertRaises(AssertionError):
            select_row_action(nodes + [node('Download', 170)], 'Alpha Episode 001', 'Download')
        with self.assertRaises(AssertionError):
            select_row_action(nodes, 'Beta Episode 001', 'Download')


class ProxyBoundaryTests(unittest.TestCase):
    def setUp(self):
        class Upstream(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def serve(self):
                self.rfile.read(int(self.headers.get('Content-Length', 0)))
                body = b'{"episodes":[]}'
                status = 200
                if self.headers.get('Range') == 'bytes=0-3':
                    body, status = body[:4], 206
                self.send_response(status)
                self.send_header('Content-Length', str(len(body)))
                if status == 206:
                    self.send_header('Content-Range', 'bytes 0-3/15')
                self.end_headers()
                if self.command != 'HEAD':
                    self.wfile.write(body)
            do_GET = do_POST = do_HEAD = serve
        self.upstream = ThreadingHTTPServer(('127.0.0.1', 0), Upstream)
        self.control = PlayerProxy(self.upstream.server_port)
        self.proxy = ThreadingHTTPServer(('127.0.0.1', 0), self.control.handler())
        self.threads = [threading.Thread(target=s.serve_forever, daemon=True) for s in (self.upstream, self.proxy)]
        for thread in self.threads:
            thread.start()

    def tearDown(self):
        self.control.release.set()
        for server in (self.proxy, self.upstream):
            server.shutdown()
            server.server_close()
        for thread in self.threads:
            thread.join()

    def request(self, method, path, body=None, headers=None):
        with closing(http.client.HTTPConnection('127.0.0.1', self.proxy.server_port, timeout=3)) as client:
            client.request(method, path, body=body, headers=headers or {})
            response = client.getresponse()
            return response.status, dict(response.getheaders()), response.read()

    def test_real_head_range_and_transport_failure_are_preserved(self):
        status, headers, body = self.request('HEAD', '/audio')
        self.assertEqual((status, body), (200, b''))
        self.assertEqual(int(headers['Content-Length']), 15)
        status, headers, body = self.request('GET', '/audio', headers={'Range': 'bytes=0-3'})
        self.assertEqual((status, body, headers['Content-Range']), (206, b'{"ep', 'bytes 0-3/15'))
        self.control.fail_progress = True
        with self.assertRaises(http.client.RemoteDisconnected):
            self.request('POST', '/api/v1/progress', '{}')
        self.assertTrue(self.control.trace[-1]['transport_failed'])

    def test_scoped_reply_is_held_after_real_upstream_response(self):
        self.control.delay_scope = True
        replies = []
        thread = threading.Thread(target=lambda: replies.append(
            self.request('POST', '/api/v1/podcasts/sync', '{"scope":{"episode_key":"guid:one"}}')))
        thread.start()
        self.assertTrue(self.control.held.wait(2))
        self.assertFalse(replies)
        self.assertIn('received_ms', self.control.trace[-1])
        self.assertNotIn('delivered_ms', self.control.trace[-1])
        self.control.release.set()
        thread.join(3)
        self.assertEqual(replies[0][::2], (200, b'{"episodes":[]}'))


if __name__ == '__main__':
    unittest.main()
