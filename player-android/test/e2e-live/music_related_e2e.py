#!/usr/bin/env python3
"""M41 native related music against a disposable music fixture and running emulator.

The caller owns the emulator, APK and fixture. This runner never starts or stops
the emulator or fixture server. It can install the supplied APK and add adb
reverse, but leaves both in place for the integrated run unless --cleanup-device
is requested.
"""
import argparse
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import re
import socket
import sys
import threading
import time
import traceback

import adrv
import app_ui
import music_e2e as baseline
import music_albums_e2e as albums
import music_artists_e2e as artists
import music_sequence_e2e as sequence
from fixture_server import Client


class Run(albums.Run):
    failures = 0

    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M41', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def labels():
    return ' | '.join(app_ui.labels())


def scroll_content(up=True):
    # Stay above the mini player and transient snackbar after a Play action.
    start, end = ('1300', '450') if up else ('450', '1300')
    adrv.adb('shell', 'input', 'swipe', '540', start, '540', end, '420')
    time.sleep(.8)


def visible(pattern, swipes=8):
    for move in (None, *([True] * swipes), *([False] * (2 * swipes))):
        if move is not None:
            scroll_content(move)
        node = adrv.find(pattern)
        if node:
            return node
    raise AssertionError((pattern, labels()))


def result_action(title, action):
    """Tap the named action within the visible related card for this title."""
    for move in (None, *([True] * 10), *([False] * 20)):
        if move is not None:
            scroll_content(move)
        nodes = adrv.nodes()
        titles = [n for n in nodes if re.search(r'^' + re.escape(title) + r'($| / )', adrv.label(n))]
        actions = [n for n in nodes if adrv.label(n) == action and n['click']]
        mini_player = any(adrv.label(n).startswith('Open player:') for n in nodes)
        pairs = [(a['b'][1] - t['b'][1], a) for t in titles for a in actions
                 if 0 <= a['b'][1] - t['b'][1] <= 420
                 and (not mini_player or a['b'][3] < 1450)]
        if pairs:
            _, target = min(pairs, key=lambda pair: pair[0])
            adrv.tap_node(target)
            return
    raise AssertionError(('Related action missing', title, action, labels()))


def output(run, song, label):
    sequence.wait_track(song['title'])
    # Volume keys target music only after Player owns an active audio session.
    for _ in range(15):
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_VOLUME_UP')
    sequence.ensure_music_volume(run)
    sequence.key('PAUSE')
    time.sleep(.5)
    mark = run.audio.mark()
    sequence.key('PLAY')
    sequence.wait_track(song['title'])
    time.sleep(4)
    sequence.key('PAUSE')
    wav = run.audio.save(mark, run.evidence.path(label + '.wav'))
    analysis = baseline.audio_trace.analyze(wav)
    segment = baseline.main_segment(analysis)
    assert segment and segment['duration_s'] > 2 and baseline.tone_matches(segment, song), analysis
    return wav, {'session': sequence.state(), 'audio': analysis, 'media_id': song['id']}


def core(run):
    night = next(a for a in run.viewer.request('GET', '/api/v1/music/albums')
                 if a['title'] == 'Night Transit')
    matching = run.viewer.request('GET', '/api/v1/music/related/album/' + night['album_key'])
    assert [m['title'] for m in matching][:2] == ['Synth Compilation', '東京 Nights (夜)'], matching
    assert all(m['key'] != night['album_key'] and m['basis'] for m in matching), matching
    tokyo = run.track('Music/Ünïcødé Artïst - 東京 Nights/01 - Café Ñoño.mp3')
    albums.album()
    visible('^More like this$')
    visible('From your library')
    for theme, mode in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
        time.sleep(2)
        visible('^More like this$')
        run.record('album related cards ' + theme, True, artifacts=[run.shot('m41-album-' + theme)])
    result_action('東京 Nights (夜)', 'Open album')
    assert adrv.wait('^Play album$', timeout=25), labels()
    assert '東京 Nights (夜)' in labels(), labels()
    app_ui.back()
    assert 'Night Transit' in labels(), labels()
    result_action('東京 Nights (夜)', 'Play')
    wav, proof = output(run, tokyo, 'm41-album-source')
    run.record('touch album suggestion Open/Back/Play and original output', True,
               data=proof, artifacts=[wav, run.shot('m41-album-play')])
    app_ui.back()

    people = run.viewer.request('GET', '/api/v1/music/artists')
    aurora = next(a for a in people if a['name'] == 'Aurora Lane')
    neighbours = run.viewer.request('GET', '/api/v1/music/related/artist/' + aurora['artist_key'])
    assert any(a['title'] == 'Dusk Choir' for a in neighbours), neighbours
    artists.catalog()
    artists.open_visible('Aurora Lane')
    visible('^More like this$')
    result_action('Dusk Choir', 'Open artist')
    assert adrv.wait('^Dusk Choir$', timeout=25), labels()
    app_ui.back()
    assert 'Aurora Lane' in labels(), labels()
    afterglow = run.track('Music/Various Artists - Synth Compilation/Disc 2/01 - Afterglow.ogg')
    result_action('Dusk Choir', 'Play')
    wav, proof = output(run, afterglow, 'm41-artist-source')
    run.record('touch artist suggestion Open/Back/Play and original output', True,
               data=proof, artifacts=[wav, run.shot('m41-artist-play')])
    app_ui.back()

    song(run)


def song(run):

    first = run.track('Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3')
    road = run.track('Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3')
    assert baseline.open_track('Music', baseline.ALBUM, first['title']), labels()
    visible('^More like this$')
    result_action('Long Road Home', 'Open song')
    assert 'Long Road Home' in labels(), labels()
    app_ui.back()
    assert first['title'] in labels(), labels()
    result_action('Long Road Home', 'Play')
    wav, proof = output(run, road, 'm41-song-source')
    run.record('touch song suggestion Open/Back/Play and original output', True,
               data=proof, artifacts=[wav, run.shot('m41-song-play')])
    app_ui.back()
    adrv.adb('shell', 'cmd', 'uimode', 'night', 'no')


def boundaries(run):
    empty = next(a for a in run.viewer.request('GET', '/api/v1/music/albums')
                 if a['title'] == 'Partial Metadata')
    assert run.viewer.request('GET', '/api/v1/music/related/album/' + empty['album_key']) == []
    albums.album('Partial Metadata')
    visible('No related music found')
    run.record('untagged album has explicit empty state', True,
               artifacts=[run.shot('m41-empty')])
    hidden = run.track('Restricted/Admin Only - Hidden Album/01 - Hidden Track.mp3')
    status, denied = run.viewer.call('GET', f'/api/v1/music/related/song/{hidden["id"]}')
    assert status == 404 and 'Hidden Track' not in json.dumps(denied), (status, denied)
    user_id = next(u['id'] for u in run.admin.request('GET', '/api/v1/admin/users')
                   if u['username'] == run.auth['user']['username'])
    permission = {'set_id': run.state['set_ids']['Music'], 'user_id': user_id}
    source = run.track('Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3')
    run.admin.request('DELETE', '/api/v1/admin/permissions', permission)
    try:
        status, _ = run.viewer.call('GET', f'/api/v1/music/related/song/{source["id"]}')
        assert status == 404, status
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', dict(permission, role='viewer'))
    assert run.viewer.request('GET', f'/api/v1/music/related/song/{source["id"]}')
    run.record('actual set ACL hides source and restores related results', True,
               data={'hidden_status': 404, 'revoked_status': status})


def related_failure_proxy(origin_port):
    """Forward normal fixture traffic while dropping only related reads.

    The connection closes without a response, matching a real transport
    interruption. The direct adb reverse mapping is restored before Retry.
    """
    class Handler(BaseHTTPRequestHandler):
        protocol_version = 'HTTP/1.1'

        def log_message(self, *_):
            pass  # Never log cookies or credentials from the disposable account.

        def do_GET(self):
            if self.path.startswith(('/api/music/related/', '/api/v1/music/related/')):
                try:
                    self.connection.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass
                self.close_connection = True
                return
            self.forward()

        def do_POST(self):
            self.forward()

        do_PUT = do_POST
        do_PATCH = do_POST
        do_DELETE = do_POST

        def forward(self):
            length = int(self.headers.get('Content-Length', '0'))
            body = self.rfile.read(length) if length else None
            headers = {key: value for key, value in self.headers.items()
                       if key.lower() not in ('host', 'connection', 'transfer-encoding')}
            upstream = http.client.HTTPConnection('127.0.0.1', origin_port, timeout=20)
            try:
                upstream.request(self.command, self.path, body=body, headers=headers)
                response = upstream.getresponse()
                payload = response.read()
                self.send_response(response.status)
                for key, value in response.getheaders():
                    if key.lower() not in ('connection', 'transfer-encoding', 'content-length'):
                        self.send_header(key, value)
                self.send_header('Content-Length', str(len(payload)))
                self.send_header('Connection', 'close')
                self.end_headers()
                self.wfile.write(payload)
                self.close_connection = True
            finally:
                upstream.close()

    server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread


def errors(run):
    assert run.base == run.state['android']['adb_reverse_origin'], 'Use adb reverse for related-read failure'
    fixture_port = run.state['port']
    proxy, thread = related_failure_proxy(fixture_port)
    try:
        adrv.adb('reverse', '--remove', f'tcp:{fixture_port}')
        adrv.adb('reverse', f'tcp:{fixture_port}', f'tcp:{proxy.server_port}')
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'],
                                        run.auth['user']['password'], run.record, run.shot)
        albums.album()
        visible('Could not load related music')
        visible('^Retry$')
        shot = run.shot('m41-related-transport-failure')
        run.record('related-only transport failure keeps album detail and visible Retry', True,
                   detail='Local forwarder dropped only related HTTP response', artifacts=[shot])
    finally:
        adrv.adb('reverse', '--remove', f'tcp:{fixture_port}')
        adrv.adb('reverse', f'tcp:{fixture_port}', f'tcp:{fixture_port}')
        proxy.shutdown()
        proxy.server_close()
        thread.join(timeout=5)
    visible('^Retry$')
    assert adrv.tap('^Retry$'), labels()
    visible('Synth Compilation')
    run.record('related Retry after direct fixture reconnect shows deterministic results', True,
               artifacts=[run.shot('m41-related-retry')])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--cleanup-device', action='store_true')
    parser.add_argument('--scenario', choices=['all', 'core', 'song', 'boundaries', 'errors'], default='all')
    args = parser.parse_args(argv)
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    run = None
    try:
        baseline.prepare_device(args, port)
        run = Run(args)
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        assert run.audio.available(), 'QEMU output WAV required for actual native output proof'
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'],
                                        run.auth['user']['password'], run.record, run.shot)
        for name, step in [('core', core), ('song', song), ('boundaries', boundaries), ('errors', errors)]:
            if name == 'song' and args.scenario != 'song':
                continue  # core already includes the song journey.
            if args.scenario in ('all', name):
                step(run)
    except Exception:
        if run is None:
            raise
        path = run.evidence.path('m41-native-failure.txt')
        path.write_text(traceback.format_exc())
        run.record('native related journey', False, traceback.format_exc(),
                   artifacts=[run.shot('m41-failure'), path])
    finally:
        if args.cleanup_device:
            baseline.teardown(run, None if args.host_alias else port)
    return 1 if run is None or run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
