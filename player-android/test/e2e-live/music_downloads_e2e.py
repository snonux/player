#!/usr/bin/env python3
"""M46 native acceptance on a leased, already running disposable app/emulator.

No install, reset, runtime restart or fixture metadata writes. A client-local
HTTP proxy slows/truncates genuine server media; the normal adb reverse mapping
is restored on exit. Each phase leaves saved copies for inspection. --scenario
allows small recoveries; a saved run-manifest.json supplies offline targets.
Requires baseline.Run's optional evidence_dir keyword (parent-owned helper patch).
"""
import argparse
import hashlib
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import io
import json
from pathlib import Path
import re
import sqlite3
import sys
import tarfile
import tempfile
import threading
import time
import traceback
from urllib.parse import urlsplit

import adrv
import app_ui
import music_e2e as baseline
import music_albums_e2e as albums
import music_playlists_e2e as playlists
import music_queue_recovery_e2e as recovery
from fixture_server import Client


MEDIA_PATH = re.compile(r'^/api/(?:v1/)?media/(\d+)/(download|compat)$')
PRIVATE = f'/data/user/0/{app_ui.PKG}/databases/offline-downloads'


def media_id(path):
    match = MEDIA_PATH.fullmatch(urlsplit(path).path)
    return int(match.group(1)) if match else None


def original_rows(rows, origin, user_id, ids):
    """Require one complete, correctly scoped Original row per requested ID."""
    scoped = [row for row in rows if row['origin'] == origin and row['user_id'] == user_id
              and row['media_id'] in ids]
    assert sorted(row['media_id'] for row in scoped) == sorted(set(ids)), scoped
    for row in scoped:
        assert row['download_profile'] == 'original', row
        assert row['size_bytes'] > 0 and re.fullmatch(r'[a-f0-9]{64}', row['key']), row
    return scoped


def assert_audio(analysis, expected):
    # The short QEMU capture's FFT lands the fixture's 440 Hz MP3 at 453 Hz;
    # neighboring fixture tones are more than 100 Hz apart.
    matching = [s for s in analysis['segments'] if s['tone_hz'] is not None
                and abs(s['tone_hz'] - expected) < 18 and s['duration_s'] >= 1]
    assert matching, {'expected_hz': expected, 'analysis': analysis}
    return matching


class TransferProxy:
    """Client-scoped real HTTP forwarding; artifacts contain paths, never cookies."""
    def __init__(self, origin, port=0):
        self.origin = urlsplit(origin)
        assert self.origin.scheme == 'http' and self.origin.hostname in ('127.0.0.1', 'localhost')
        self.events = []
        self.fail_once = None
        self.hold_id = None
        self.release = threading.Event()
        self.release.set()
        self.started = threading.Event()
        self.lock = threading.Lock()
        owner = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = 'HTTP/1.1'

            def log_message(self, *_):
                pass

            def forward(self):
                ident = media_id(self.path)
                with owner.lock:
                    fail = ident is not None and ident == owner.fail_once
                    if fail:
                        owner.fail_once = None
                    hold = ident is not None and ident == owner.hold_id
                conn = http.client.HTTPConnection(owner.origin.hostname, owner.origin.port, timeout=45)
                record = {'method': self.command, 'path': urlsplit(self.path).path,
                          'media_id': ident, 'fault': 'truncate' if fail else 'hold' if hold else '',
                          'bytes': 0, 'complete': False}
                try:
                    body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
                    headers = {k: v for k, v in self.headers.items()
                               if k.lower() not in ('host', 'connection', 'accept-encoding')}
                    headers['Accept-Encoding'] = 'identity'
                    conn.request(self.command, self.path, body, headers)
                    response = conn.getresponse()
                    record['status'] = response.status
                    owner.events.append(record)
                    self.send_response(response.status)
                    # Close-delimited responses are valid when upstream uses chunking.
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
                        record['bytes'] += len(block)
                        if hold:
                            owner.started.set()
                            if not owner.release.wait(20):
                                return
                            hold = False
                        if fail and record['bytes'] >= 8192:
                            return  # Preserve real Content-Length; native detects the short body.
                    record['complete'] = True
                except (OSError, http.client.HTTPException) as error:
                    record['error'] = type(error).__name__
                finally:
                    conn.close()

            do_GET = do_HEAD = do_POST = do_PUT = do_PATCH = do_DELETE = forward

        self.server = ThreadingHTTPServer(('127.0.0.1', port), Handler)
        self.server.daemon_threads = True
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    @property
    def port(self):
        return self.server.server_address[1]

    def start(self):
        self.thread.start()

    def hold(self, ident):
        self.hold_id = ident
        self.release.clear()
        self.started.clear()

    def unblock(self):
        self.hold_id = None
        self.release.set()

    def close(self):
        self.unblock()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=3)


class Run(baseline.Run):
    def __init__(self, args):
        state = json.loads(Path(args.fixture_dir, 'state.json').read_text())
        endpoint = urlsplit(state['origin'])
        assert endpoint.scheme == 'http' and endpoint.hostname in ('127.0.0.1', 'localhost'), 'Disposable local server required'
        # Keyword binding on an older helper fails before any Evidence write.
        super().__init__(args, evidence_dir=args.evidence_dir)
        self.args = args
        self.failures = 0
        self.viewer = Client(self.state['origin']).login(**self.auth['user'])
        self.viewer_id = next(u['id'] for u in self.admin.request('GET', '/api/v1/admin/users')
                              if u['username'] == self.auth['user']['username'])
        assert self.audio.available(), 'Owned QEMU WAV is required'
        self.proxy = TransferProxy(self.state['origin'], args.proxy_port)
        self.created_playlists = []
        self.targets = []
        self.manifest_path = self.evidence.path('run-manifest.json')
        if args.targets:
            saved = json.loads(Path(args.targets).read_text())
            assert saved['origin'] == self.base and saved['user_id'] == self.viewer_id, saved
            self.targets = saved['targets']

    def record(self, scenario, ok=True, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M46', scenario, result, detail, data, artifacts,
                             proof='none' if result == 'gap' else 'real')
        print(result.upper(), scenario, detail, flush=True)
        if result == 'fail':
            self.failures += 1

    def pause(self):
        if baseline.session_state()['state'] == 'PLAYING':
            adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
        deadline = time.monotonic() + 10
        while baseline.session_state()['state'] == 'PLAYING' and time.monotonic() < deadline:
            time.sleep(.2)
        assert baseline.session_state()['state'] != 'PLAYING'

    def touch(self, pattern):
        # Existing helpers scroll within the actual native view and use semantic bounds.
        playlists.tap_visible(pattern)
        time.sleep(.3)

    def select_album(self, title):
        pattern = '^Select ' + re.escape(title) + '$'
        node = baseline.find_visible(pattern)
        assert node, (pattern, app_ui.labels())
        for _ in range(3):
            if node['b'][1] <= 1650:
                adrv.tap_node(node)
                time.sleep(.3)
                return
            app_ui.swipe(up=True)
            node = adrv.find(pattern)
            assert node, (pattern, app_ui.labels())
        raise AssertionError((pattern, node))

    def open_playlist(self, name):
        playlists.catalog()
        pattern = r'(^| / )' + re.escape(name)
        node = app_ui.find_scrolling(pattern)
        assert node, app_ui.labels()
        if node['b'][1] > 1500:
            adrv.adb('shell', 'input', 'swipe', '540', '1600', '540', '1250', '300')
            node = adrv.find(pattern)
            assert node and node['b'][1] < 1800, app_ui.labels()
            adrv.adb('shell', 'input', 'tap',
                     str((node['b'][0] + node['b'][2]) // 2), str(node['b'][1] + 45))
        else:
            adrv.tap_node(node)
        playlists.wait_detail(name)

    def rows(self):
        # Quiescent batch checkpoints only. Capture main DB + WAL together; never
        # open/write the device DB or checkpoint it behind the app's connection.
        names = adrv.adb('shell', 'ls', PRIVATE).splitlines()
        files = [n for n in ('index.db', 'index.db-wal', 'index.db-shm') if n in names]
        if not files:
            return []
        archive = adrv.adb('exec-out', 'tar', '-C', PRIVATE, '-cf', '-', *files, binary=True)
        with tempfile.TemporaryDirectory(prefix='player-m46-db-') as directory:
            with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
                for name in files:
                    stream = tar.extractfile(name)
                    assert stream is not None, name
                    Path(directory, name).write_bytes(stream.read())
            with sqlite3.connect(f'file:{directory}/index.db?mode=ro', uri=True) as db:
                db.row_factory = sqlite3.Row
                return [dict(row) for row in db.execute('SELECT * FROM downloads')]

    def require_fresh(self, ids):
        existing = [row['media_id'] for row in self.rows() if row['origin'] == self.base
                    and row['user_id'] == self.viewer_id and row['media_id'] in ids]
        assert not existing, ('Selected copies already exist; choose a focused recovery or remove only these through the UI first', existing)

    def verify(self, ids):
        rows = original_rows(self.rows(), self.base, self.viewer_id, ids)
        proof = []
        for row in rows:
            relative = next(path for path, ident in self.state['media_ids'].items() if ident == row['media_id'])
            expected = Path(self.state['media_root'], relative).read_bytes()
            actual = adrv.adb('exec-out', 'cat', f'{PRIVATE}/{row["key"]}.media', binary=True)
            assert len(actual) == row['size_bytes'] == len(expected), row
            assert hashlib.sha256(actual).digest() == hashlib.sha256(expected).digest(), row
            item = {'id': row['media_id'], 'title': row['title'], 'key': row['key'],
                    'path': relative, 'sha256': hashlib.sha256(actual).hexdigest(), 'size': len(actual)}
            proof.append(item)
            self.targets = [old for old in self.targets if old['id'] != item['id']] + [item]
        self.manifest_path.parent.mkdir(parents=True, exist_ok=True)
        self.manifest_path.write_text(json.dumps({'origin': self.base, 'user_id': self.viewer_id,
                                                  'targets': self.targets}, indent=2))
        return proof

    def album(self, title):
        self.pause()
        albums.album(title)
        entry = next(a for a in self.viewer.request('GET', '/api/v1/music/albums') if a['title'] == title)
        return self.viewer.request('GET', '/api/v1/music/albums/' + entry['album_key'])['songs']

    def dialog(self):
        self.touch('^Keep offline$')
        assert adrv.wait('^Keep music offline$'), app_ui.labels()
        self.touch('^Download quality /')
        self.touch('^Original \\(no conversion\\)$')
        assert adrv.wait('^Keep offline$'), app_ui.labels()

    def start(self):
        self.touch('^Keep offline$')

    def finished(self, count, failed=0):
        pattern = rf'^{count} of {count + failed} saved · {failed} failed · Finished$'
        assert adrv.wait(pattern, timeout=90), app_ui.labels()
        return self.shot(f'finished-{count}-{failed}-{time.time_ns()}')

    def downloads(self):
        self.pause()
        assert app_ui.go_home(), app_ui.labels()
        assert adrv.tap('^Open navigation menu$')
        assert adrv.tap('^Downloads$')
        assert adrv.wait('^Downloads$'), app_ui.labels()

    def open_download(self, title):
        pattern = '^' + re.escape(title) + r'(?: /|$)'
        for upward in (True, False):
            for _ in range(20):
                node = adrv.find(pattern)
                if node and node['b'][3] - node['b'][1] > 70:
                    if 0 <= node['b'][1] < 1900:
                        # Downloads has no mini player; the row above the
                        # bottom sync banner is still directly touchable.
                        adrv.adb('shell', 'input', 'tap', '400',
                                 str(min(node['b'][1] + 45, 1920)))
                        return
                    adrv.adb('shell', 'input', 'swipe', '540', '1500', '540', '1280', '300')
                else:
                    start, end = ('1500', '1000') if upward else ('1000', '1500')
                    adrv.adb('shell', 'input', 'swipe', '540', start, '540', end, '300')
                time.sleep(.3)
        raise AssertionError((pattern, app_ui.labels()))


def clear_saved_queue(run):
    """Drop the portable Music session so browser proof does not block native play."""
    import uuid
    import urllib.request
    queue = run.viewer.request('GET', '/api/v1/queue')
    body = json.dumps({
        'mutation_id': str(uuid.uuid4()), 'entries': [], 'current_id': 0,
        'position_seconds': 0, 'label': '', 'repeat': 'off', 'shuffle': False,
        'canonical': [], 'up_next': [],
    }).encode()
    req = urllib.request.Request(
        run.base + '/api/v1/queue', data=body, method='PUT',
        headers={'Content-Type': 'application/json', 'If-Match': f'"{queue["revision"]}"'})
    with run.viewer.opener.open(req) as response:
        assert 200 <= response.status < 300, response.status


def resolve_session_prompt():
    """Dismiss multi-client saved-session prompts without leaving the app."""
    for _ in range(4):
        labels = app_ui.labels()
        if 'Replace' in labels and any('Replace saved session?' in l for l in labels):
            adrv.tap('^Replace$'); time.sleep(1); continue
        if 'Replace saved session' in labels:
            adrv.tap('^Replace saved session$'); time.sleep(1); continue
        if 'Continue session' in labels:
            adrv.tap('^Continue session$'); time.sleep(1); continue
        if 'Keep this queue' in labels:
            adrv.tap('^Keep this queue$'); time.sleep(1); continue
        break


def use_proxy(run):
    adrv.adb('reverse', f'tcp:{run.state["port"]}', f'tcp:{run.proxy.port}')


def use_origin(run):
    adrv.adb('reverse', f'tcp:{run.state["port"]}', f'tcp:{run.state["port"]}')


def happy(run):
    artifacts = []
    clear_saved_queue(run)
    songs = run.album('Night Transit')
    ids = [s['id'] for s in songs]
    run.require_fresh(ids)
    # Short fixture tracks left at their end finish immediately on Play album.
    for song in songs:
        baseline.reset_progress(run.viewer, song['id'])
    resolve_session_prompt()
    # Seed listening on the real fixture origin. The transfer proxy is only for
    # download fault injection; streaming through it left the session in NONE.
    port = run.state['port']
    adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
    run.touch('^Play album$')
    resolve_session_prompt()
    deadline = time.monotonic() + 25
    while baseline.session_state()['state'] != 'PLAYING' and time.monotonic() < deadline:
        # A prepared but idle session often needs an explicit media play.
        if baseline.session_state()['state'] in (None, 'NONE', 'PAUSED'):
            adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PLAY')
        time.sleep(.4)
    assert baseline.session_state()['state'] == 'PLAYING', baseline.session_state()
    run.pause()
    if not adrv.find('^Play album$'):
        app_ui.back()
    if not adrv.find('^Play album$'):
        # A screen or search route may be above the album detail; re-enter it
        # from Library only when Back did not reveal the action.
        albums.album('Night Transit', fresh=True)
    before = recovery.snapshot(run)
    run.dialog()
    for theme, mode in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
        time.sleep(.7)
        artifacts.append(run.shot('dialog-' + theme))
    adrv.adb('reverse', f'tcp:{port}', f'tcp:{run.proxy.port}')
    run.proxy.hold(ids[0])
    try:
        run.start()
        assert run.proxy.started.wait(20), run.proxy.events
        assert adrv.wait(r'^0 of 3 saved · 0 failed$'), app_ui.labels()
        source = baseline.session_state()
        mark = run.audio.mark()
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PLAY')
        deadline = time.monotonic() + 10
        while baseline.session_state()['state'] != 'PLAYING' and time.monotonic() < deadline:
            time.sleep(.2)
        playing = baseline.session_state()
        assert playing['state'] == 'PLAYING' and playing['title'] == source['title'], (source, playing)
        time.sleep(2)
        during = baseline.session_state()
        # MediaSession publishes sparse position anchors on this emulator;
        # sustained playback is proved below by captured QEMU audio.
        assert during['state'] == 'PLAYING' and during['title'] == playing['title'], (playing, during)
        run.pause()
        wav = run.audio.save(mark, run.evidence.path('listening-during-held-download.wav'))
        analysis = baseline.audio_trace.analyze(wav)
        assert_audio(analysis, run.track(next(path for path, ident in run.state['media_ids'].items() if ident == ids[0]))['tone_hz'][0])
        artifacts.append(wav)
    finally:
        run.proxy.unblock()
    artifacts.append(run.finished(len(ids)))
    proof = run.verify(ids)
    app_ui.back()
    assert adrv.wait('^Play album$'), app_ui.labels()
    after = recovery.snapshot(run)
    assert (before or {}).get('entries') == (after or {}).get('entries'), (before, after)
    assert (before or {}).get('index') == (after or {}).get('index'), (before, after)
    run.record('album aggregate saves exact originals without replacing queue; touch/Back and themes', data={'copies': proof}, artifacts=artifacts)

    happy_tail(run)


def happy_tail(run):
    unicode_songs = [run.track(t['path']) for t in run.manifest['tracks']
                     if t['path'].startswith('Music/Ünïcødé Artïst - 東京 Nights/')]
    assert len(unicode_songs) == 2
    ids = [s['id'] for s in unicode_songs]
    run.require_fresh(ids)
    name = 'M46 duplicate playlist ' + str(time.time_ns())
    playlist = run.viewer.request('POST', '/api/v1/playlists', {'name': name})
    pid = playlist['playlist_id']
    try:
        run.viewer.request('POST', f'/api/v1/playlists/{pid}/songs', {'action': 'add', 'media_ids': [*ids, ids[0]]})
        run.open_playlist(name)
        run.dialog()
        assert adrv.wait('^2 unique songs · Saved to Downloads$'), app_ui.labels()
        start = len(run.proxy.events)
        use_proxy(run)
        run.start()
        shot = run.finished(2)
        use_origin(run)
        proof = run.verify(ids)
        requests = [x for x in run.proxy.events[start:]
                    if x['method'] == 'GET' and x['media_id'] in ids]
        assert sorted(x['media_id'] for x in requests) == sorted(ids), requests
        app_ui.back()
        run.record('playlist duplicate occurrence saves each entry once', data={'copies': proof, 'requests': requests}, artifacts=[shot])
    finally:
        run.viewer.request('DELETE', '/api/v1/playlists/' + pid)

    happy_selection(run)


def happy_selection(run):
    songs = run.album('Synth Compilation')
    ids = [s['id'] for s in songs]
    run.require_fresh(ids)
    albums.catalog()
    if adrv.find('^View: Grid$'):
        run.touch('^View: Grid$')
    run.touch('^Select albums$')
    run.select_album('Night Transit')
    run.select_album('Synth Compilation')
    selection_finish(run, ids)


def selection_finish_current(run):
    ids = [run.track(track['path'])['id'] for track in run.manifest['tracks']
           if track['path'].startswith('Music/Various Artists - Synth Compilation/')]
    assert len(ids) == 3, ids
    run.require_fresh(ids)
    selection_finish(run, ids)


def selection_finish(run, ids):
    assert adrv.find('^2 selected$'), app_ui.labels()
    run.dialog()
    assert adrv.wait('^6 unique songs · Saved to Downloads$'), app_ui.labels()
    start = len(run.proxy.events)
    use_proxy(run)
    run.start()
    shot = run.finished(6)
    use_origin(run)
    proof = run.verify(ids)
    requests = [x for x in run.proxy.events[start:]
                if x['method'] == 'GET' and x['media_id'] is not None]
    assert sorted(x['media_id'] for x in requests) == sorted(ids), requests
    app_ui.back()
    app_ui.back()  # Cancel catalog selection through native Back.
    assert adrv.wait('^Select albums$'), app_ui.labels()
    run.record('selected album batch saves every entry and Back restores catalog', data={'copies': proof}, artifacts=[shot])


def catalog_playlist(run, numbers, label):
    # Existing short, uniquely titled songs keep fault phases independent of
    # successful earlier downloads; no fixture sources are generated/replaced.
    ids = [run.track(f'Music/Catalog Position/Release {n:02}/01 - Position.mp3')['id'] for n in numbers]
    run.require_fresh(ids)
    name = 'M46 ' + label + ' ' + str(time.time_ns())
    pid = run.viewer.request('POST', '/api/v1/playlists', {'name': name})['playlist_id']
    run.created_playlists.append(pid)
    run.viewer.request('POST', '/api/v1/playlists/' + pid + '/songs', {'action': 'add', 'media_ids': ids})
    run.pause()
    run.open_playlist(name)
    return ids


def interruption(run):
    use_proxy(run)
    ids = catalog_playlist(run, [1, 2, 3], 'interrupted')
    run.proxy.fail_once = ids[1]
    run.dialog()
    start = len(run.proxy.events)
    run.start()
    shot = run.finished(2, 1)
    retained = run.verify([ids[0], ids[2]])
    assert not any(row['media_id'] == ids[1] and row['user_id'] == run.viewer_id for row in run.rows())
    run.touch('^Retry$')
    run.finished(1)
    proof = run.verify(ids)
    requests = [x for x in run.proxy.events[start:]
                if x['method'] == 'GET' and x['media_id'] in ids]
    assert [x['media_id'] for x in requests].count(ids[0]) == 1, requests
    assert [x['media_id'] for x in requests].count(ids[1]) == 2, requests
    assert [x['media_id'] for x in requests].count(ids[2]) == 1, requests
    app_ui.back()
    run.record('genuine short transfer retains successes; Retry fetches only the failed song', data={'retained': retained, 'copies': proof, 'requests': requests}, artifacts=[shot])


def cancellation(run):
    use_proxy(run)
    ids = catalog_playlist(run, [4, 5, 6], 'cancel')
    run.proxy.hold(ids[1])
    try:
        run.dialog()
        start = len(run.proxy.events)
        run.start()
        assert run.proxy.started.wait(20), run.proxy.events
        assert adrv.wait(r'^1 of 3 saved · 0 failed$'), app_ui.labels()
        shot = run.shot('aggregate-progress-before-cancel')
        run.touch('^Cancel downloads$')
        assert adrv.wait(r'^1 of 3 saved · 0 failed · Canceled$'), app_ui.labels()
    finally:
        run.proxy.unblock()
    retained = run.verify([ids[0]])
    assert not any(row['media_id'] in ids[1:] and row['user_id'] == run.viewer_id for row in run.rows())
    run.touch('^Retry$')
    run.finished(3)
    proof = run.verify(ids)
    requests = [x for x in run.proxy.events[start:]
                if x['method'] == 'GET' and x['media_id'] in ids]
    assert [x['media_id'] for x in requests].count(ids[0]) == 1, requests
    app_ui.back()
    run.record('aggregate progress cancellation retains completed copy; Retry skips it', data={'retained': retained, 'copies': proof, 'requests': requests}, artifacts=[shot])


def permission(run):
    use_proxy(run)
    ids = catalog_playlist(run, [7, 8], 'permission')
    run.dialog()
    grant = {'set_id': run.state['set_ids']['Music'], 'user_id': run.viewer_id, 'role': 'viewer'}
    run.admin.request('DELETE', '/api/v1/admin/permissions', grant)
    start = len(run.proxy.events)
    try:
        run.start()
        shot = run.finished(0, 2)
        denied = [x for x in run.proxy.events[start:]
                  if x['method'] == 'GET' and x['media_id'] in ids]
        # Resolution was allowed; the real download route must now deny both.
        assert len(denied) == 2 and all(x['status'] == 403 for x in denied), denied
        assert not any(row['media_id'] in ids and row['user_id'] == run.viewer_id for row in run.rows())
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', grant)
    run.touch('^Retry$')
    run.finished(2)
    proof = run.verify(ids)
    app_ui.back()
    run.record('actual permission denial saves no files; restored Retry saves exact originals', data={'denied': denied, 'copies': proof}, artifacts=[shot])


def offline(run):
    assert run.targets, 'Run a saving phase or supply --targets with its run-manifest.json'
    run.downloads()
    # Only this client's reverse route is removed. Server and other clients stay live.
    port = run.state['port']
    adrv.adb('reverse', '--remove', f'tcp:{port}')
    proof = []
    try:
        for item in run.targets:
            row = next(row for row in run.rows() if row['key'] == item['key'])
            assert row['origin'] == run.base and row['user_id'] == run.viewer_id
            run.open_download(item['title'])
            # Saved tracks open paused at their local position; reset with the visible skip.
            run.pause()
            assert adrv.tap('^Skip back 15 seconds$'), app_ui.labels()
            mark = run.audio.mark()
            # Seeking back from a saved terminal position can resume the
            # current local source. The player control shows its actual state.
            if adrv.find('^Play$'):
                assert adrv.tap('^Play$'), app_ui.labels()
            else:
                assert adrv.find('^Pause$'), app_ui.labels()
            deadline = time.monotonic() + 20
            before = baseline.session_state()
            while before['state'] != 'PLAYING' and time.monotonic() < deadline:
                time.sleep(.2)
                before = baseline.session_state()
            assert before['state'] == 'PLAYING' and before['title'] == item['title'], before
            time.sleep(2)
            after = baseline.session_state()
            assert after['state'] == 'PLAYING' and after['title'] == item['title'], (before, after)
            run.pause()
            wav = run.audio.save(mark, run.evidence.path(f'offline-{item["id"]}.wav'))
            analysis = baseline.audio_trace.analyze(wav)
            song = run.track(item['path'])
            matching = assert_audio(analysis, song['tone_hz'][0])
            # Local progress belongs to the exact private key, independently of
            # server progress and the streaming cache. Observe its persisted write.
            deadline = time.monotonic() + 5
            saved = next(r for r in run.rows() if r['key'] == item['key'])
            while saved['position_seconds'] < .8 and time.monotonic() < deadline:
                time.sleep(.3)
                saved = next(r for r in run.rows() if r['key'] == item['key'])
            assert saved['position_seconds'] >= .8, saved
            proof.append({'id': item['id'], 'sha256': item['sha256'], 'before': before, 'after': after,
                          'audio': matching, 'offline_position': saved['position_seconds']})
            app_ui.back()
            assert adrv.wait('^Downloads$'), app_ui.labels()
        run.record('every saved bulk entry decodes and advances with real source audio while disconnected', data={'copies': proof}, artifacts=[run.evidence.path(f'offline-{item["id"]}.wav') for item in run.targets])
    finally:
        use_origin(run)


def account(run):
    use_proxy(run)
    ids = catalog_playlist(run, [9, 10], 'account')
    run.proxy.hold(ids[0])
    try:
        run.dialog()
        run.start()
        assert run.proxy.started.wait(20), run.proxy.events
        # Native Back hides progress without canceling. Logout is a real
        # Settings action while that original-user transfer remains held.
        app_ui.back()
        recovery.login_other(run, run.auth['admin'])
        run.proxy.unblock()
        time.sleep(1)
        rows = run.rows()
        assert not any(row['media_id'] in ids and row['origin'] == run.base for row in rows), rows
        requests = [x for x in run.proxy.events
                    if x['method'] == 'GET' and x['media_id'] in ids]
        assert len(requests) == 1 and requests[0]['media_id'] == ids[0], requests
        run.record('Back hides batch; real account switch cancels held transfer and prevents later saves', data={'requests': requests}, artifacts=[run.shot('changed-account')])
    finally:
        run.proxy.unblock()
        recovery.login_other(run, run.auth['user'])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--evidence-dir', required=True)
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--apk', help='Read-only APK identity; never installed')
    parser.add_argument('--proxy-port', type=int, default=0)
    parser.add_argument('--targets', help='Prior saving phase run-manifest.json for focused offline replay')
    parser.add_argument('--scenario', choices=['happy', 'happy-tail', 'selection-only', 'selection-finish', 'interruption', 'cancel', 'permission', 'offline', 'account', 'all'], default='happy')
    args = parser.parse_args(argv)
    args.host_alias = False
    args.evidence_dir = str(Path(args.evidence_dir) / ('m46-' + str(time.time_ns())))
    # No prepare_device/connect_and_login/teardown: those can install/reset data.
    run = Run(args)
    initial_night = adrv.adb('shell', 'cmd', 'uimode', 'night').strip().rsplit(' ', 1)[-1]
    port = run.state['port']
    run.proxy.start()
    # Keep the fixture origin mapped directly for login/catalog/playback.
    # Individual download-fault steps remap through the proxy when needed.
    adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
    try:
        assert app_ui.foreground(), 'Parent must foreground and sign in the existing app before the lease'
        # The existing UI login helper logs out through Settings; it never resets app data.
        if args.scenario != 'selection-finish':
            recovery.login_other(run, run.auth['user'])
            resolve_session_prompt()
            clear_saved_queue(run)
        for name, step in [('happy', happy), ('happy-tail', happy_tail),
                           ('selection-only', happy_selection),
                           ('selection-finish', selection_finish_current),
                           ('interruption', interruption), ('cancel', cancellation),
                           ('permission', permission), ('account', account), ('offline', offline)]:
            if name in ('happy-tail', 'selection-only', 'selection-finish') and args.scenario == 'all':
                continue
            if args.scenario not in (name, 'all'):
                continue
            try:
                step(run)
            except Exception:
                trace = run.evidence.path(name + '-failure.txt')
                trace.write_text(traceback.format_exc())
                run.record(name, False, traceback.format_exc(), artifacts=[trace, run.shot(name + '-failure')])
                break  # Stop the dependent stages at the first failure; preserve state.
        run.record('large real collection stress', result='gap', detail='No large real native collection exercised. The web/server 500-song/512 MiB ZIP limits do not apply to native sequential batches.')
    finally:
        run.proxy.unblock()
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
        adrv.adb('shell', 'cmd', 'uimode', 'night', initial_night)
        run.proxy.close()
        run.evidence.path('transfers.json').write_text(json.dumps(run.proxy.events, indent=2))
        for pid in run.created_playlists:
            run.viewer.request('DELETE', '/api/v1/playlists/' + pid)
        print('Evidence:', run.evidence.directory, flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
