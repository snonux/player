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
from contextlib import contextmanager
from types import SimpleNamespace
from podcast_sync_e2e import (require_audio, require_resume, register_installation,
                              select_row_action, PlayerProxy, PublisherProxy, Run,
                              feed_label_pattern, require_publisher_response,
                              require_download_state, require_seek_jump, seek_bar_node,
                              seek_track_swipe, wait_seek_bar)


class PodcastNativeOracleTests(unittest.TestCase):
    def test_offline_seek_checks_durable_beta_copy_before_touch_navigation(self):
        with tempfile.TemporaryDirectory() as directory:
            run = self.run_without_device(directory)
            beta = {'key': 'guid:beta-002', 'title': 'Beta Episode 002'}
            run.state['feeds'] = [{}, {'title': 'Acceptance Beta',
                                      'episodes': [{}, beta]}]
            run.snapshot = MagicMock(return_value={'podcast_downloads': [
                {'episode_key': beta['key'], 'state': 'available',
                 'actual_sha256': 'publisher-bytes'}]})
            run.wait_downloaded_row = MagicMock()
            with patch('podcast_sync_e2e.checked_adb',
                       side_effect=RuntimeError('stop before offline mapping')):
                with self.assertRaisesRegex(RuntimeError, 'stop before offline mapping'):
                    run.offline_seek()
            run.snapshot.assert_called_once_with('before-offline-seek')
            run.episodes.assert_called_once_with(combined=False, filter_name='All',
                                                  feed_title='Acceptance Beta')
            run.wait_downloaded_row.assert_not_called()

    def test_download_setup_skips_absent_delete_controls_and_uses_short_feed_views(self):
        with tempfile.TemporaryDirectory() as directory:
            for rows, expected_feeds, delete_count in (
                    ([{'episode_key': 'guid:beta-003', 'state': 'missing'},
                      {'episode_key': 'guid:alpha-003', 'state': 'failed'}],
                     ['Acceptance Beta'], 0),
                    ([{'episode_key': 'guid:beta-003', 'state': 'available'},
                      {'episode_key': 'guid:alpha-003', 'state': 'available'}],
                     ['Acceptance Beta', 'Acceptance Alpha', 'Acceptance Beta'], 2)):
                run = self.run_without_device(directory)
                run.state.update(database='unused', feeds=[
                    {'title': 'Acceptance Alpha', 'episodes': [{}, {},
                        {'key': 'guid:alpha-003', 'title': 'Alpha Episode 003'}]},
                    {'title': 'Acceptance Beta', 'episodes': [{}, {},
                        {'key': 'guid:beta-003', 'title': 'Beta Episode 003'}]},
                ])
                run.snapshot = MagicMock(return_value={'podcast_downloads': rows})
                run.publisher_proxy = MagicMock(side_effect=RuntimeError('stop before device'))
                with patch('podcast_sync_e2e.podcast_media', return_value={}):
                    with self.assertRaisesRegex(RuntimeError, 'stop before device'):
                        run.download_negatives()
                run.snapshot.assert_called_once_with('before-download-negatives')
                self.assertEqual([call.kwargs['feed_title'] for call in run.episodes.call_args_list],
                                 expected_feeds)
                self.assertTrue(all(call.kwargs['combined'] is False
                                    for call in run.episodes.call_args_list))
                self.assertEqual(run.row.call_count, delete_count)
                self.assertTrue(all(call.args[0] == 'Delete downloaded file'
                                    for call in run.row.call_args_list))

    def test_publisher_proxy_holds_real_bytes_and_switches_error_to_retry(self):
        class Upstream(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def do_GET(self):
                self.send_response(200)
                self.send_header('Content-Type', 'audio/wav')
                self.send_header('Content-Length', '5')
                self.end_headers()
                self.wfile.write(b'audio')

        upstream = ThreadingHTTPServer(('127.0.0.1', 0), Upstream)
        proxy = PublisherProxy(upstream.server_port, hold_tag='beta-003',
                               fail_tag='alpha-003')
        front = ThreadingHTTPServer(('127.0.0.1', 0), proxy.handler())
        workers = [threading.Thread(target=s.serve_forever, daemon=True)
                   for s in (upstream, front)]
        for worker in workers:
            worker.start()

        def request(tag):
            with closing(http.client.HTTPConnection('127.0.0.1', front.server_port,
                                                    timeout=3)) as connection:
                connection.request('GET', '/audio/end.wav?episode=' + tag)
                response = connection.getresponse()
                return response.status, response.read()

        try:
            result = []
            worker = threading.Thread(target=lambda: result.append(request('beta-003')))
            worker.start()
            self.assertTrue(proxy.held.wait(2))
            held = require_publisher_response(proxy.trace, 'beta-003', 200)
            self.assertNotIn('delivered_ms', held)
            proxy.release.set()
            worker.join(3)
            self.assertFalse(worker.is_alive())
            self.assertEqual(result, [(200, b'audio')])
            self.assertEqual(request('alpha-003'), (503, b''))
            require_publisher_response(proxy.trace, 'alpha-003', 503)
            proxy.fail_tag = None
            self.assertEqual(request('alpha-003'), (200, b'audio'))
            require_publisher_response(proxy.trace, 'alpha-003', 200)
            with self.assertRaisesRegex(AssertionError, 'credential-free'):
                require_publisher_response([{
                    'episode_tag': 'alpha-003', 'method': 'GET', 'status': 200,
                    'player_credentials_present': True}], 'alpha-003', 200)
        finally:
            proxy.release.set()
            for server in (front, upstream):
                server.shutdown()
                server.server_close()
            for worker in workers:
                worker.join()

    def test_publisher_mapping_is_restored_after_native_stage_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            run = self.run_without_device(directory)
            run.state['publisher_port'] = 18776
            run.publisher_proxy = Run.publisher_proxy.__get__(run)
            calls = []
            def adb(*args):
                calls.append(args)
                if args == ('reverse', '--list'):
                    return 'emulator-5554 tcp:18776 tcp:18776\n'
                return ''
            with patch('podcast_sync_e2e.checked_adb', side_effect=adb):
                with self.assertRaisesRegex(RuntimeError, 'test failure'):
                    with run.publisher_proxy():
                        raise RuntimeError('test failure')
            self.assertEqual(calls[-1], ('reverse', 'tcp:18776', 'tcp:18776'))
            self.assertTrue((run.output / 'publisher-proxy-trace.json').exists())

    def test_negative_download_oracles_reject_playable_or_missing_state(self):
        episode = {'key': 'guid:test', 'title': 'Beta Episode 003'}
        def snapshot(state, **extra):
            return {'podcast_downloads': [{'episode_key': episode['key'],
                      'state': state, **extra}]}
        require_download_state(snapshot('missing', size_bytes=0), episode, 'missing')
        require_download_state(snapshot('failed', error='Retry download'), episode, 'failed')
        require_download_state(snapshot('available', actual_sha256='abc'), episode, 'available')
        with self.assertRaises(AssertionError):
            require_download_state(snapshot('available', actual_sha256='abc'), episode, 'missing')
        with self.assertRaises(AssertionError):
            require_download_state(snapshot('available'), episode, 'available')
        with self.assertRaises(AssertionError):
            require_download_state(snapshot('failed', actual_sha256='abc'), episode, 'failed')

    def test_offline_seek_uses_thumb_and_full_track_geometry(self):
        root = {'cls': 'FrameLayout', 'b': [0, 0, 1080, 2220]}
        good = {'cls': 'SeekBar', 'b': [75, 1400, 207, 1532],
                'desc': '1%', 'text': ''}
        nodes = [root, good]
        self.assertIs(seek_bar_node(nodes), good)
        self.assertEqual(seek_track_swipe(nodes, good), (141, 1466, 638))
        self.assertEqual(seek_track_swipe(nodes, good, fraction=.02),
                         (141, 1466, 149))
        finished = {**good, 'b': [873, 1400, 1005, 1532], 'desc': '100%'}
        self.assertEqual(seek_track_swipe([root, finished], finished),
                         (939, 1466, 636))
        middle = {**good, 'b': [474, 1400, 606, 1532], 'desc': '50%'}
        self.assertEqual(seek_track_swipe([root, middle], middle),
                         (540, 1466, 638))
        with patch('podcast_sync_e2e.adrv.nodes',
                   side_effect=[[root, middle], nodes]), \
                patch('podcast_sync_e2e.time.sleep'):
            self.assertEqual(wait_seek_bar(timeout=2, max_percent=15),
                             (nodes, good))
        with patch('podcast_sync_e2e.adrv.nodes',
                   side_effect=[RuntimeError('route changing'), [], nodes]), \
                patch('podcast_sync_e2e.time.sleep'):
            self.assertEqual(wait_seek_bar(timeout=2), (nodes, good))
        with patch('podcast_sync_e2e.adrv.nodes', return_value=[]):
            with self.assertRaisesRegex(AssertionError, 'did not appear'):
                wait_seek_bar(timeout=0)
        for invalid in ([], [good, dict(good)],
                        [{'cls': 'SeekBar', 'b': [0, 0, 0, 30]}]):
            with self.assertRaisesRegex(AssertionError, 'seek bar'):
                seek_bar_node(invalid)
        with self.assertRaisesRegex(AssertionError, 'full-screen geometry'):
            seek_track_swipe([good], good)

    def test_offline_seek_requires_verified_reset_then_large_touch_jump(self):
        title = 'Beta Episode 002'
        reset = {'title': title, 'position_ms': 240}
        target = {'title': title, 'position_ms': 7456}
        require_seek_jump(reset, target, title)
        for start, end in ((7456, 7456), (2000, 7456), (240, 4000),
                           (240, 11000)):
            with self.assertRaisesRegex(AssertionError, 'reset=.*target='):
                require_seek_jump({'title': title, 'position_ms': start},
                                  {'title': title, 'position_ms': end}, title)

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
        run.episode_visible = MagicMock(return_value=True)
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
            available = [{'desc': 'Alpha Episode 001 / Available on this device', 'text': '',
                          'cls': 'Button', 'b': [0, 0, 1, 1], 'click': True}]
            with patch('podcast_sync_e2e.adrv.wait', return_value=True), \
                    patch('podcast_sync_e2e.adrv.nodes', return_value=available), \
                    patch('podcast_sync_e2e.adrv.label',
                          side_effect=lambda n: (n.get('desc') or n.get('text') or '')), \
                    patch('podcast_sync_e2e.app_ui.back'), \
                    patch('podcast_sync_e2e.checked_adb'), \
                    patch('podcast_sync_e2e.time.sleep'), \
                    patch('podcast_sync_e2e.audio_trace.analyze', return_value=analysis), \
                    patch('podcast_sync_e2e.session_state', return_value={
                        'state': 'PLAYING', 'title': run.episode['title'], 'position_ms': 0}):
                with self.assertRaisesRegex(AssertionError, 'Resume differs'):
                    run.resume()
                run.snapshot.assert_not_called()
            with patch('podcast_sync_e2e.adrv.wait', return_value=True), \
                    patch('podcast_sync_e2e.adrv.nodes', return_value=available), \
                    patch('podcast_sync_e2e.adrv.label',
                          side_effect=lambda n: (n.get('desc') or n.get('text') or '')), \
                    patch('podcast_sync_e2e.app_ui.back'), \
                    patch('podcast_sync_e2e.checked_adb'), \
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
        identity = {'serial': 'emulator-5554', 'android_id': 'one',
                    'installed': 'versionCode=4022\nfirstInstallTime=original'}
        ledger = {}
        register_installation(ledger, 'A', identity)
        register_installation(ledger, 'A', identity)
        with self.assertRaises(AssertionError):
            register_installation(ledger, 'B', identity)
        with self.assertRaises(AssertionError):
            register_installation(ledger, 'A', {**identity,
                'installed': 'versionCode=4023\nfirstInstallTime=reinstalled'})
        register_installation(ledger, 'A', {**identity,
            'installed': 'versionCode=4023\nfirstInstallTime=original'})
        register_installation(ledger, 'B', {'serial': 'emulator-5556', 'android_id': 'two',
            'installed': 'versionCode=4022\nfirstInstallTime=other'})

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
        # In-progress rows expose Resume and Play from beginning together.
        resume = node('Resume', 140)
        restart = node('Play from beginning', 160)
        dual = [node('Alpha Episode 001', 100), resume, restart,
                node('Alpha Episode 002', 200), node('Download', 250)]
        self.assertIs(
            select_row_action(dual, 'Alpha Episode 001', r'(?:Play|Resume|Play from beginning)'),
            resume)
        with self.assertRaises(AssertionError):
            select_row_action(nodes, 'Beta Episode 001', 'Download')

    def test_clipped_episode_action_scrolls_above_fixed_sync_footer_before_tap(self):
        def node(text, top, bottom, click=True):
            return {'desc': text, 'text': '', 'cls': 'Button',
                    'b': [0, top, 500, bottom], 'click': click}
        footer = node('Sync now', 2011, 2143)
        clipped = node('Download', 2100, 2230)
        visible = node('Download', 900, 1030)
        screens = [
            [node('Beta Episode 002', 1600, 1700), clipped, footer],
            [node('Beta Episode 002', 500, 600), visible, footer],
        ]
        with tempfile.TemporaryDirectory() as directory:
            run = self.run_without_device(directory)
            run.row = Run.row.__get__(run)
            run.shot = MagicMock()
            with patch('podcast_sync_e2e.adrv.nodes', side_effect=screens), \
                    patch('podcast_sync_e2e.adrv.tap_node') as tap, \
                    patch('podcast_sync_e2e.app_ui.swipe') as swipe:
                run.row('Download', title='Beta Episode 002')
            swipe.assert_called_once()
            tap.assert_called_once_with(visible)

            run.wait_downloaded_row = Run.wait_downloaded_row.__get__(run)
            screens = [
                [node('Beta Episode 002', 1600, 1700),
                 node('Delete downloaded file', 2100, 2230), footer],
                [node('Beta Episode 002', 500, 600),
                 node('Delete downloaded file', 900, 1030), footer],
            ]
            with patch('podcast_sync_e2e.adrv.nodes', side_effect=screens), \
                    patch('podcast_sync_e2e.app_ui.swipe') as swipe:
                run.wait_downloaded_row('Beta Episode 002', timeout=2)
            swipe.assert_called_once()

    def test_held_account_releases_only_after_logout_and_rejects_cross_scope_state(self):
        with tempfile.TemporaryDirectory() as directory:
            run = self.run_without_device(directory)
            run.state.update(origin='http://fixture', accounts={'admin': 1, 'other': 3})
            run.feed = {'key': 'feed:alpha'}
            run.root = MagicMock()
            run.switch_account = MagicMock()
            trace = [{'path': '/api/v1/podcasts/sync', 'scoped': True, 'status': 200,
                      'received_ms': 10}]
            control = SimpleNamespace(held=threading.Event(), release=threading.Event(), trace=trace)
            control.held.set()

            @contextmanager
            def proxy(**options):
                self.assertTrue(options['delay_scope'])
                self.assertGreater(options['hold_seconds'], 90)
                yield control

            run.proxy = proxy
            def login(kind):
                self.assertEqual(kind, 'other')
                self.assertTrue(control.release.is_set())
                trace[0]['delivered_ms'] = 20
            run.login.side_effect = login
            run.snapshot = MagicMock(return_value={
                'podcast_episode_state': [], 'podcast_subscriptions': []})
            with patch('podcast_sync_e2e.adrv.find', side_effect=lambda pattern:
                       pattern == '^Log Out$'), \
                    patch('podcast_sync_e2e.adrv.wait', return_value=True):
                run.held_account()
                run.record.assert_called_with('held scoped response cannot cross native account logout')
                trace[0].pop('delivered_ms')
                control.release.clear()
                run.snapshot.return_value['podcast_episode_state'] = [{
                    'scope': 'account:http://fixture#3', 'episode_key': 'guid:one'}]
                with self.assertRaisesRegex(AssertionError, 'other account'):
                    run.held_account()

    def test_end_outage_next_requires_persisted_completion_next_audio_and_failed_sync(self):
        with tempfile.TemporaryDirectory() as directory:
            run = self.run_without_device(directory)
            first = {'key': 'guid:alpha-2', 'title': 'Alpha Episode 002'}
            second = {'key': 'guid:beta-2', 'title': 'Beta Episode 002'}
            run.state['feeds'] = [{'episodes': [{}, first]}, {'episodes': [{}, second]}]
            run.state['audio']['end'] = {'duration_s': 12, 'tone_hz': 882}
            run.wait_downloaded_row = MagicMock()
            run.snapshot = MagicMock(side_effect=[
                {'podcast_downloads': [{'episode_key': first['key'], 'state': 'available'},
                                       {'episode_key': second['key'], 'state': 'available'}]},
                {'podcast_episode_state': [{'scope': run.scope, 'episode_key': first['key'],
                                            'completed': 1, 'completion_source': 'auto'}]},
            ])
            control = SimpleNamespace(trace=[])

            @contextmanager
            def proxy(**_):
                yield control

            run.proxy = proxy
            run.audio.save.return_value = run.output / 'not-platform-proof.wav'
            first_play = {'title': first['title'], 'state': 'PLAYING', 'position_ms': 1000}
            first_end = {**first_play, 'position_ms': 12000}
            next_play = {'title': second['title'], 'state': 'PLAYING', 'position_ms': 1000}
            states = [first_play, *([first_end] * 4), next_play]
            def state():
                item = states.pop(0)
                if len(states) == 3:
                    control.trace.append({'path': '/api/v1/podcasts/sync',
                                          'transport_failed': True})
                return item
            analysis = {'audible_s': 3, 'segments': [{'duration_s': 3, 'tone_hz': 882}]}
            with patch('podcast_sync_e2e.session_state', side_effect=state), \
                    patch('podcast_sync_e2e.time.sleep'), \
                    patch('podcast_sync_e2e.audio_trace.analyze', return_value=analysis), \
                    patch('podcast_sync_e2e.app_ui.back'), \
                    patch('podcast_sync_e2e.checked_adb'):
                run.end_outage_next()
            run.record.assert_called_with(
                'failed native end sync leaves completion durable and next source audible')
            self.assertEqual(run.row.call_args_list[-1].kwargs['title'], second['title'])


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
