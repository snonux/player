#!/usr/bin/env python3
"""M45 installed Android cache journey on an already running emulator.

Use an exclusively leased disposable music fixture. No APK install, app-data
reset, or emulator restart is performed. The optional --augment writes two
69 MiB decodable WAVs to that fixture and removes them on exit. The fixture's
server remains running; only this test's adb reverse mapping is changed.

python3 -u player-android/test/e2e-live/music_playback_cache_e2e.py \
    /tmp/player-m45-fixture --evidence-dir /tmp/player-m45-android \
    --emulator-wav /tmp/emulator-output.wav --augment
"""
import argparse
import hashlib
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import re
import sys
import threading
import time
import traceback
import wave
from urllib.parse import urlsplit

import adrv
import app_ui
import music_e2e as baseline
import music_playlists_e2e as playlists
import music_queue_recovery_e2e as recovery
from fixture_server import Client


FOLDER = 'M45 Cache Acceptance'
NAMES = ['01 - Cache A.wav', '02 - Cache B.wav']
LABELS = ['01 - Cache A', '02 - Cache B']
PRIVATE = f'/data/user/0/{app_ui.PKG}/databases/playback-cache'


def make_wav(path, hz):
    if path.exists():
        return False
    samples = (69 * 1048576 - 44) // 2
    with wave.open(str(path), 'wb') as out:
        out.setnchannels(1)
        out.setsampwidth(2)
        out.setframerate(16000)
        # A 1-second whole-cycle tone can be repeated without discontinuity.
        import math
        block = b''.join(int(8000 * math.sin(2 * math.pi * hz * n / 16000)).to_bytes(
            2, 'little', signed=True) for n in range(16000))
        left = samples * 2
        while left:
            piece = block[:min(left, len(block))]
            out.writeframesraw(piece)
            left -= len(piece)
    return True


class TransferProxy:
    """Forward genuine Player responses, holding one uncached full stream."""
    def __init__(self, origin):
        upstream = urlsplit(origin)
        assert upstream.scheme == 'http' and upstream.hostname in ('127.0.0.1', 'localhost')
        self.upstream = upstream
        self.hold_id = None
        self.started = threading.Event()
        self.release = threading.Event()
        self.events = []
        owner = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = 'HTTP/1.1'

            def log_message(self, *_):
                pass

            def forward(self):
                path = urlsplit(self.path).path
                match = re.fullmatch(r'/api/(?:v1/)?media/(\d+)/stream', path)
                held = (self.command == 'GET' and match and
                        int(match.group(1)) == owner.hold_id and
                        'Range' not in self.headers)
                conn = http.client.HTTPConnection(upstream.hostname, upstream.port, timeout=75)
                event = {'method': self.command, 'path': path, 'held': bool(held),
                         'bytes': 0, 'complete': False}
                owner.events.append(event)
                try:
                    body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
                    headers = {k: v for k, v in self.headers.items()
                               if k.lower() not in ('host', 'connection', 'accept-encoding')}
                    headers['Accept-Encoding'] = 'identity'
                    conn.request(self.command, self.path, body, headers)
                    response = conn.getresponse()
                    event['status'] = response.status
                    self.send_response(response.status)
                    for key, value in response.getheaders():
                        if key.lower() not in ('transfer-encoding', 'connection'):
                            self.send_header(key, value)
                    self.send_header('Connection', 'close')
                    self.end_headers()
                    self.close_connection = True
                    if self.command == 'HEAD':
                        return
                    while True:
                        block = response.read(4096)
                        if not block:
                            break
                        self.wfile.write(block)
                        self.wfile.flush()
                        event['bytes'] += len(block)
                        if held:
                            owner.started.set()
                            if not owner.release.wait(75):
                                return
                            held = False
                    event['complete'] = True
                except (OSError, http.client.HTTPException) as error:
                    event['error'] = type(error).__name__
                finally:
                    conn.close()

            do_GET = do_HEAD = do_POST = do_PUT = do_PATCH = do_DELETE = forward

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.server.daemon_threads = True
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    @property
    def port(self):
        return self.server.server_address[1]

    def close(self):
        self.release.set()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=3)


class Run(baseline.Run):
    def __init__(self, args):
        args.host_alias = False
        super().__init__(args, evidence_dir=args.evidence_dir)
        self.args = args
        self.viewer = Client(self.state['origin']).login(**self.auth['user'])
        self.user_id = next(u['id'] for u in self.admin.request('GET', '/api/v1/admin/users')
                            if u['username'] == self.auth['user']['username'])
        self.admin_id = self.admin.request('GET', '/api/config')['user_id']
        self.failed = False
        self.created = []
        self.tracks = []
        assert (self.fixture_dir / '.player-music-fixture').exists(), 'Disposable fixture required'
        assert self.audio.available(), 'QEMU WAV output required for native audio proof'

    def record(self, scenario, detail='', data=None, artifacts=(), result='pass'):
        self.evidence.record('M45', scenario, result, detail, data or {}, artifacts,
                             proof='real' if result != 'gap' else 'none')
        print(result.upper(), scenario, detail, flush=True)
        self.failed |= result == 'fail'

    def touch(self, pattern):
        playlists.tap_visible(pattern)
        time.sleep(.4)

    def prepare(self):
        media_root = Path(self.state['media_root'])
        directory = media_root / 'Music' / FOLDER
        directory.mkdir(parents=True, exist_ok=True)
        for name, hz in zip(NAMES, (440, 550)):
            path = directory / name
            if not path.exists() and not self.args.augment:
                raise AssertionError(f'{path} missing; rerun with --augment on an exclusively leased fixture')
            if make_wav(path, hz):
                self.created.append(path)
        if self.created:
            self.rescan()
        media = self.admin.request('GET', '/api/v1/media?limit=1000')
        for name, label, hz in zip(NAMES, LABELS, (440, 550)):
            path = directory / name
            found = next((m for m in media if m['file_name'] == name and
                          m['set_id'] == self.state['set_ids']['Music']), None)
            assert found, (name, 'media scan missing')
            self.tracks.append({'id': found['id'], 'name': name, 'label': label,
                                'hz': hz, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
                                'size': path.stat().st_size})

    def rescan(self):
        self.admin.request('POST', '/api/v1/admin/rescan', {})
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            if not self.admin.request('GET', '/api/v1/admin/scan-progress')['running']:
                return
            time.sleep(.4)
        raise AssertionError('Fixture rescan did not finish')

    def index(self):
        raw = adrv.adb('exec-out', 'cat', f'{PRIVATE}/index.json', binary=True)
        if not raw:
            return {}
        data = json.loads(raw)
        assert isinstance(data, dict), data
        return data

    def entry(self, media_id, user_id=None):
        user_id = self.user_id if user_id is None else user_id
        matches = [(key, row) for key, row in self.index().items()
                   if row['origin'] == self.base and row['user'] == user_id and
                   row['media_id'] == media_id]
        assert len(matches) <= 1, matches
        return matches[0] if matches else None

    def await_entry(self, media_id, present=True, timeout=75):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            row = self.entry(media_id)
            if bool(row) == present:
                return row
            time.sleep(.5)
        raise AssertionError((media_id, present, self.index()))

    def verify_bytes(self, track):
        key, row = self.await_entry(track['id'])
        assert row['size'] == track['size'], (row, track)
        bytes_ = adrv.adb('exec-out', 'cat', f'{PRIVATE}/{key}.audio', binary=True)
        digest = hashlib.sha256(bytes_).hexdigest()
        assert digest == track['sha256'], (digest, track['sha256'])
        return {'id': track['id'], 'key': key, 'sha256': digest, 'size': row['size']}

    def settings(self):
        assert app_ui.go_home(), app_ui.labels()
        self.touch('^Settings$')
        self.touch('Playback cache')
        assert adrv.wait('Cache budget'), app_ui.labels()

    def set_buffer(self, enabled):
        node = baseline.find_visible('Cache whole song before playing')
        assert node, app_ui.labels()
        if (node['checked'] == 'true') != enabled:
            adrv.tap_node(node)
            time.sleep(.5)

    def set_prefetch(self, enabled):
        node = baseline.find_visible('Prefetch next queued song')
        assert node, app_ui.labels()
        if (node['checked'] == 'true') != enabled:
            adrv.tap_node(node)
            time.sleep(.5)

    def configure(self):
        self.settings()
        self.touch('Cache budget')
        self.touch('^128 MB$')
        self.set_prefetch(False)
        self.set_buffer(True)
        for theme, mode in [('dark', 'yes'), ('light', 'no')]:
            adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
            time.sleep(.6)
            shot = self.shot(f'settings-{theme}')
            assert adrv.find('Kept downloads are separate'), app_ui.labels()
            self.record(f'{theme} cache settings, disposable copy explanation and touch controls',
                        artifacts=[shot])
        app_ui.back()

    def pause(self):
        if baseline.session_state()['state'] == 'PLAYING':
            adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
        deadline = time.monotonic() + 10
        while baseline.session_state()['state'] == 'PLAYING' and time.monotonic() < deadline:
            time.sleep(.2)

    def play(self, track):
        self.pause()
        assert baseline.open_track('Music', FOLDER, track['label']), app_ui.labels()
        baseline.reset_progress(self.viewer, track['id'])
        mark = self.audio.mark()
        self.touch('^Play Audio$')
        deadline = time.monotonic() + 75
        while baseline.session_state()['state'] != 'PLAYING' and time.monotonic() < deadline:
            time.sleep(.3)
        playing = baseline.session_state()
        assert playing['state'] == 'PLAYING' and playing['title'] == track['label'], playing
        time.sleep(2)
        after = baseline.session_state()
        assert after['position_ms'] > playing['position_ms'] + 700, (playing, after)
        self.pause()
        wav = self.audio.save(mark, self.evidence.path(f'played-{track["id"]}.wav'))
        analysis = baseline.audio_trace.analyze(wav)
        segment = baseline.main_segment(analysis)
        assert segment and abs(segment['tone_hz'] - track['hz']) < 6, analysis
        return {'session': after, 'segment': segment, 'wav': wav}

    def offline(self, track):
        self.pause()
        assert app_ui.go_home(), app_ui.labels()
        self.touch('^Open navigation menu$')
        self.touch('^Downloads$')
        self.touch('^Browse offline music$')
        assert adrv.wait('^Offline music$'), app_ui.labels()
        assert adrv.find('Cached copies are disposable'), app_ui.labels()
        port = self.state['port']
        adrv.adb('reverse', '--remove', f'tcp:{port}')
        try:
            self.touch('^' + re.escape(track['label']) + '$')
            assert adrv.wait(r'Cached \(disposable\)'), app_ui.labels()
            mark = self.audio.mark()
            # Song row in the bottom sheet starts the actual offline source.
            self.touch('^' + re.escape(track['label']) + '$')
            deadline = time.monotonic() + 20
            while baseline.session_state()['state'] != 'PLAYING' and time.monotonic() < deadline:
                time.sleep(.3)
            before = baseline.session_state()
            assert before['title'] == track['label'] and before['state'] == 'PLAYING', before
            time.sleep(2)
            after = baseline.session_state()
            assert after['position_ms'] > before['position_ms'] + 700, (before, after)
            self.pause()
            wav = self.audio.save(mark, self.evidence.path('offline-replay.wav'))
            segment = baseline.main_segment(baseline.audio_trace.analyze(wav))
            assert segment and abs(segment['tone_hz'] - track['hz']) < 6, segment
            shot = self.shot('offline-replay')
            self.record('cached WAV replays with real output after adb reverse removed',
                        data={'before': before, 'after': after, 'segment': segment}, artifacts=[wav, shot])
        finally:
            adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')

    def clear(self):
        self.pause()
        self.settings()
        self.touch('^Clear cache$')
        assert adrv.wait('Cache cleared'), app_ui.labels()
        app_ui.back()

    def prefetch(self, a, b):
        self.pause()
        self.settings()
        self.set_buffer(False)
        self.set_prefetch(True)
        self.touch('Music network')
        self.touch('^Network$')
        self.touch(r'^Wi-Fi \(manual\)$')
        app_ui.back()
        assert baseline.open_track('Music', FOLDER, a['label']), app_ui.labels()
        self.touch('^Play now$')
        deadline = time.monotonic() + 20
        while baseline.session_state()['state'] != 'PLAYING' and time.monotonic() < deadline:
            time.sleep(.3)
        assert baseline.session_state()['title'] == a['label'], baseline.session_state()
        assert baseline.open_track('Music', FOLDER, b['label']), app_ui.labels()
        self.touch('^Play next$')
        proof = self.verify_bytes(b)
        assert baseline.session_state()['title'] == a['label'], baseline.session_state()
        self.pause()
        self.record('visible Play next prefetched complete upcoming original without switching playback',
                    data={'prefetched': proof, 'playing': baseline.session_state()},
                    artifacts=[self.shot('prefetch-next')])
        self.settings()
        self.set_prefetch(False)
        self.touch('^Network$')
        self.touch('^Auto$')
        app_ui.back()

    def cancel(self, track):
        self.clear()
        assert self.entry(track['id']) is None
        self.settings()
        self.set_buffer(False)
        app_ui.back()
        assert baseline.open_track('Music', FOLDER, track['label']), app_ui.labels()
        proxy = TransferProxy(self.state['origin'])
        port = self.state['port']
        proxy.hold_id = track['id']
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{proxy.port}')
        try:
            self.touch('^Play Audio$')
            assert proxy.started.wait(30), proxy.events
            self.pause()
            self.settings()
            self.touch('^Cancel transfer$')
            assert adrv.wait('Transfer cancelled'), app_ui.labels()
            proxy.release.set()
            self.await_entry(track['id'], False)
            assert not any(e['held'] and e['complete'] for e in proxy.events), proxy.events
            self.record('Cancel transfer aborts an incomplete real server stream',
                        data={'transfers': proxy.events}, artifacts=[self.shot('cancelled-transfer')])
            app_ui.back()
        finally:
            proxy.release.set()
            adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
            proxy.close()

    def storage(self):
        self.pause()
        self.settings()
        node = baseline.find_visible('Use app external storage')
        assert node, app_ui.labels()
        if node['checked'] != 'true':
            adrv.tap_node(node)
            assert adrv.wait('Cache settings saved'), app_ui.labels()
        external = f'/storage/emulated/0/Android/data/{app_ui.PKG}/files/playback-cache'
        assert adrv.adb('shell', f'test -d {external} && echo present').strip() == 'present'
        shot = self.shot('external-storage')
        node = baseline.find_visible('Use app external storage')
        assert node and node['checked'] == 'true', node
        adrv.tap_node(node)
        assert adrv.wait('Cache settings saved'), app_ui.labels()
        assert baseline.find_visible('Use app external storage')['checked'] != 'true'
        self.record('native app external storage selected and restored to internal',
                    data={'external_directory': external}, artifacts=[shot])
        app_ui.back()

    def cleanup(self):
        for path in self.created:
            path.unlink(missing_ok=True)
        if self.created:
            self.rescan()


def journey(run):
    a, b = run.tracks
    recovery.login_other(run, run.auth['user'])
    run.configure()
    first = run.play(a)
    proof_a = run.verify_bytes(a)
    run.record('complete first streamed original and measured native output',
               data={'source': proof_a, 'audio': first['segment']}, artifacts=[first['wav']])
    run.offline(a)
    second = run.play(b)
    proof_b = run.verify_bytes(b)
    run.await_entry(a['id'], False)
    assert len([r for r in run.index().values() if r['user'] == run.user_id and
                r['origin'] == run.base]) == 1
    run.record('128 MB budget evicts LRU after two complete 69 MB originals',
               data={'evicted': a['id'], 'retained': proof_b, 'audio': second['segment']},
               artifacts=[second['wav']])
    # Logging back into the same fixture account releases the active file;
    # Clear cache then has no protected current song to skip.
    recovery.login_other(run, run.auth['user'])
    run.clear()
    run.await_entry(b['id'], False)
    run.record('Clear cache removes disposable bytes; kept downloads are separate')
    run.prefetch(a, b)
    run.cancel(b)
    # Refill one copy before switching identity, so the isolation oracle has
    # real bytes owned by the original account.
    run.play(b)
    run.verify_bytes(b)
    recovery.login_other(run, run.auth['admin'])
    assert run.entry(b['id'], run.admin_id) is None
    assert run.entry(b['id'], run.user_id) is not None
    assert baseline.open_track('Music', FOLDER, b['label']), app_ui.labels()
    port = run.state['port']
    adrv.adb('reverse', '--remove', f'tcp:{port}')
    try:
        run.touch('^Play Audio$')
        time.sleep(8)
        session = baseline.session_state()
        assert not (session['state'] == 'PLAYING' and session['title'] == b['label']), session
        assert run.entry(b['id'], run.admin_id) is None
        run.record('other account cannot replay original user cache while disconnected',
                   data={'original_user': run.user_id, 'other_user': run.admin_id,
                         'media_id': b['id'], 'session': session},
                   artifacts=[run.shot('other-account-offline')])
    finally:
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
    run.storage()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--evidence-dir', required=True)
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--apk', help='Read-only APK identity; never installed')
    parser.add_argument('--augment', action='store_true', help='Create removable 69 MiB songs in the leased fixture')
    args = parser.parse_args(argv)
    args.evidence_dir = str(Path(args.evidence_dir) / ('m45-' + str(time.time_ns())))
    Path(args.evidence_dir).mkdir(parents=True)
    run = Run(args)
    original_night = adrv.adb('shell', 'cmd', 'uimode', 'night').strip().rsplit(' ', 1)[-1]
    port = run.state['port']
    try:
        assert app_ui.foreground(), 'Parent must foreground/sign in the installed app before lease'
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
        run.prepare()
        journey(run)
    except Exception:
        path = run.evidence.path('failure.txt')
        path.write_text(traceback.format_exc())
        run.record('native cache journey', traceback.format_exc(), artifacts=[path], result='fail')
    finally:
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
        adrv.adb('shell', 'cmd', 'uimode', 'night', original_night)
        run.cleanup()
        print('Evidence:', run.evidence.directory, flush=True)
    return 1 if run.failed else 0


if __name__ == '__main__':
    sys.exit(main())
