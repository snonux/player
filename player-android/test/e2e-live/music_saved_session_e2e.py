#!/usr/bin/env python3
"""M17 live saved-session checks on an already signed-in disposable emulator.

Never clears data or installs/restarts the emulator. Uses native touch/Back,
reads only scoped queue preferences, and temporarily routes adb reverse through
a transparent local proxy to delay/fail real queue responses. Credentials and
complete app preferences are never written to evidence.
"""
import argparse
import copy
import http.client
import json
from pathlib import Path
import re
import sys
import threading
import time
from types import SimpleNamespace
import urllib.request
import uuid
import xml.etree.ElementTree as ET
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import adrv
import app_ui
import music_e2e
from fixture_server import Client


def wait_for(read, predicate, timeout=20):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        value = read()
        if predicate(value):
            return value
        time.sleep(.2)
    raise AssertionError(('condition not reached', value))


def snapshot(origin, uid):
    root = ET.fromstring(adrv.adb('exec-out', 'cat',
        f'/data/user/0/{app_ui.PKG}/shared_prefs/FlutterSharedPreferences.xml'))
    key = f'flutter.player.queue.{origin}.{uid}'
    for node in root:
        if node.attrib.get('name') == key:
            return json.loads(node.text)
    return None


class Proxy(BaseHTTPRequestHandler):
    upstream_port = 18766
    offline = False
    delay = False
    started = threading.Event()
    release = threading.Event()
    requests = []

    def log_message(self, *_):
        pass

    def relay(self):
        queue = self.path.split('?')[0] in ('/api/v1/queue', '/api/queue')
        if queue and Proxy.offline:
            self.connection.shutdown(2)
            self.connection.close()
            return
        connection = http.client.HTTPConnection('127.0.0.1', self.upstream_port, timeout=30)
        body = self.rfile.read(int(self.headers.get('Content-Length', 0))) or None
        headers = {k: v for k, v in self.headers.items() if k.lower() not in ('host', 'connection')}
        try:
            connection.request(self.command, self.path, body=body, headers=headers)
            response = connection.getresponse()
            data = response.read()
            Proxy.requests.append({'method': self.command, 'path': self.path.split('?')[0], 'status': response.status, 'bytes': len(data)})
            if queue and self.command == 'GET' and Proxy.delay:
                Proxy.delay = False
                Proxy.started.set()
                if not Proxy.release.wait(30):
                    raise TimeoutError('saved queue response not released')
            self.send_response(response.status)
            for key, value in response.getheaders():
                if key.lower() not in ('transfer-encoding', 'connection', 'content-length'):
                    self.send_header(key, value)
            self.send_header('Content-Length', response.getheader('Content-Length', '0') if self.command == 'HEAD' else str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except (BrokenPipeError, ConnectionResetError):
            pass
        finally:
            connection.close()

    do_GET = do_POST = do_PUT = do_DELETE = do_PATCH = do_HEAD = relay


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture', type=Path)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--proxy-port', type=int, default=18770)
    parser.add_argument('--scenario', choices=('core', 'acl', 'restart'), default='core')
    args = parser.parse_args()
    args.evidence.mkdir(parents=True, exist_ok=True)
    state = json.loads((args.fixture / 'state.json').read_text())
    auth = json.loads((args.fixture / 'auth.json').read_text())
    origin = state['origin']
    versions = music_e2e.device_versions(SimpleNamespace(apk=None, emulator_wav=None), origin)
    versions['fixture_server_metadata'] = state['server']
    (args.evidence / 'versions.json').write_text(json.dumps(versions, indent=2))
    port = int(origin.rsplit(':', 1)[1])
    original_night = adrv.adb('shell', 'cmd', 'uimode', 'night').strip().rsplit(' ', 1)[-1]
    Proxy.upstream_port = port
    client = Client(origin).login(**auth['admin'])
    uid = client.request('GET', '/api/v1/config')['user_id']
    read = lambda: snapshot(origin, uid)
    server = lambda: client.request('GET', '/api/v1/queue')
    results = []

    def record(name, **data):
        results.append({'scenario': name, 'result': 'pass', **data})
        (args.evidence / 'results.json').write_text(json.dumps(results, indent=2))
        print('PASS', name, flush=True)

    def shot(name):
        adrv.shot(str(args.evidence / (name + '.png')))

    def queue():
        assert app_ui.go_home(), app_ui.labels()
        assert adrv.tap('^Now Playing queue$'), app_ui.labels()
        assert adrv.wait('^Now Playing$'), app_ui.labels()
        assert adrv.tap('^More options$'), app_ui.labels()
        assert adrv.wait('^Continue session$'), app_ui.labels()

    def confirm(action, replace=True):
        if not adrv.find('^' + re.escape(action) + '$'):
            assert adrv.tap('^More options$'), app_ui.labels()
        assert adrv.tap('^' + re.escape(action) + '$'), app_ui.labels()
        assert adrv.wait('^Replace$'), app_ui.labels()
        if replace:
            assert adrv.tap('^Replace$'), app_ui.labels()
        else:
            app_ui.back()

    def same(actual, portable):
        assert actual is not None
        assert actual['entries'] == [{'id': e['id'], 'mediaId': e['media_id']} for e in portable['entries']], (actual, portable)
        assert actual['entries'][actual['index']]['id'] == portable['current_id'], (actual, portable)
        assert abs(actual['position'] - portable['position_seconds']) < .15, (actual, portable)

    def login(account):
        assert app_ui.go_home(), app_ui.labels()
        assert adrv.tap('^Settings$'), app_ui.labels()
        assert adrv.tap('^Log Out$'), app_ui.labels()
        assert adrv.wait('^Welcome back$', timeout=25), app_ui.labels()
        fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
        assert len(fields) == 2, app_ui.labels()
        adrv.tap_node(fields[0]); app_ui.clear_field(); adrv.type_text(account['username'])
        adrv.tap_node(fields[1]); app_ui.clear_field()
        assert app_ui.sign_in(account['password']), app_ui.labels()
        assert adrv.wait('^Library$', timeout=30), app_ui.labels()

    if args.scenario == 'acl':
        viewer = Client(origin).login(**auth['user'])
        viewer_id = viewer.request('GET', '/api/v1/config')['user_id']
        expected = viewer.request('GET', '/api/v1/queue')
        assert [entry['id'] for entry in expected['entries']] == [101, 102, 103], expected
        try:
            login(auth['user'])
            queue()
            assert adrv.tap('^Continue session$')  # menu already open from queue()
            time.sleep(.5)
            if adrv.find('^Replace$'):
                assert adrv.tap('^Replace$')
            filtered = wait_for(lambda: snapshot(origin, viewer_id), lambda q: q is not None and [e['id'] for e in q['entries']] == [101, 103])
            assert filtered['position'] == 0, filtered
            assert music_e2e.session_state()['state'] in ('PAUSED', 'NONE', None)
            assert viewer.request('GET', '/api/v1/queue')['revision'] == expected['revision']
            shot('acl-filtered-native')
            record('Native Continue revalidates revoked current media and keeps remaining order paused without rewriting server', saved=expected, filtered=filtered)
        finally:
            login(auth['admin'])
        return

    if args.scenario == 'restart':
        before = read()
        adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
        app_ui.launch()
        assert adrv.wait('^Choose a server$', timeout=40), app_ui.labels()
        saved_server = adrv.find(re.escape(origin) + '$')
        assert saved_server, app_ui.labels()
        adrv.tap_node(saved_server)
        assert adrv.wait('^Welcome back$', timeout=25), app_ui.labels()
        fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
        assert any(auth['admin']['username'] in (n['text'], n['desc']) for n in fields)
        adrv.tap_node(fields[-1])
        assert app_ui.sign_in(auth['admin']['password']), app_ui.labels()
        assert adrv.wait('^Library$', timeout=30), app_ui.labels()
        after = wait_for(read, lambda q: q is not None and q['entries'] == before['entries'])
        assert after['index'] == before['index'] and abs(after['position'] - before['position']) < .15
        assert music_e2e.session_state()['state'] in ('PAUSED', 'NONE', None)
        queue()
        assert adrv.wait('^Resume queue$'), app_ui.labels()
        shot('native-restored-after-cold-restart')
        record('Native cold restart preserves saved continuation queue/current/offset with locked login and paused recovery', before=before, after=after)
        app_ui.back()
        return

    proxy = ThreadingHTTPServer(('127.0.0.1', args.proxy_port), Proxy)
    thread = threading.Thread(target=proxy.serve_forever, daemon=True)
    thread.start()
    adrv.adb('reverse', f'tcp:{port}', f'tcp:{args.proxy_port}')
    try:
        expected = server()
        assert len(expected['entries']) == 3, expected
        before = read()
        queue()
        confirm('Continue session', replace=False)
        assert read() == before
        record('Native Continue confirmation Back preserves existing queue', before=before)
        confirm('Continue session')
        restored = wait_for(read, lambda q: q is not None and q['entries'] == [{'id': e['id'], 'mediaId': e['media_id']} for e in expected['entries']])
        same(restored, expected)
        settled = music_e2e.session_state()
        assert settled['state'] in ('PAUSED', 'NONE', None), settled
        time.sleep(.7)
        assert music_e2e.session_state()['state'] in ('PAUSED', 'NONE', None)
        for name, mode in [('light', 'no'), ('dark', 'yes')]:
            adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
            time.sleep(1)
            shot('continued-' + name)
        record('Browser to native exact repeated occurrence/current/offset transfer stays paused', expected=expected, restored=restored)

        resume = music_e2e.find_visible('^Resume queue$')
        assert resume, app_ui.labels()
        adrv.tap_node(resume)
        playing = wait_for(music_e2e.session_state, lambda s: s['state'] == 'PLAYING')
        time.sleep(.7)
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
        paused = wait_for(music_e2e.session_state, lambda s: s['state'] == 'PAUSED')
        progressed = wait_for(read, lambda q: q is not None and q['position'] > restored['position'] + .4)
        assert progressed['index'] == restored['index']
        assert playing['title'] == paused['title'] == 'First Light', (playing, paused)
        record('Native Resume touch starts exact source and advances authoritative offset; native Pause', playing=playing, paused=paused, progressed=progressed)

        # A failed network refresh remains an explicit error and cannot replace
        # the local paused queue. Another actual client saves a competing revision.
        queue()
        Proxy.offline = True
        confirm('Continue session')
        assert adrv.wait('Session.*(?:unavailable|connection)|load saved session', timeout=20), app_ui.labels()
        failed = read()
        assert failed['entries'] == progressed['entries'] and failed['index'] == progressed['index']
        shot('offline-error')
        Proxy.offline = False

        # Native repeat is a structural queue edit. Save a newer browser/client
        # revision before the pending native mutation can replace it.
        Proxy.offline = True
        repeat = adrv.find('^Repeat:')
        assert repeat
        adrv.tap_node(repeat)
        wait_for(read, lambda q: q['repeat'] != progressed['repeat'])
        competing = copy.deepcopy(server())
        old_revision = competing.pop('revision')
        competing.pop('updated_at', None)
        competing['mutation_id'] = str(uuid.uuid4())
        competing['label'] = 'Other client M17 continuation'
        req = urllib.request.Request(origin + '/api/v1/queue', method='PUT', data=json.dumps(competing).encode(),
            headers={'Content-Type': 'application/json', 'If-Match': f'"{old_revision}"'})
        with client.opener.open(req) as response:
            competing_saved = json.load(response)
        Proxy.offline = False
        confirm('Continue session', replace=False)
        # Explicit Continue refresh discovers the competing queue without
        # applying it if the user cancels. A later refresh is allowed below.
        confirm('Replace saved session', replace=False)
        assert server()['revision'] == competing_saved['revision']
        confirm('Continue session')
        wait_for(read, lambda q: q['label'] == competing_saved['label'])
        same(read(), competing_saved)
        record('Native transport-offline refresh error; local edit and competing revision remain durable; cancelled replace does not write; explicit Continue adopts', competing=competing_saved, restored=read())

        # Cache actual accessible row bounds while paused, before the progress
        # animation prevents uiautomator's idle detection during playback.
        target = music_e2e.find_visible(r'^3 / Long Road Home /')
        assert target, app_ui.labels()
        # Scroll back to saved controls, retaining the row's current screen only
        # after the confirmation dialog closes. The three-row queue fits after
        # restoring the top; find target once more before starting the delay.
        for _ in range(3):
            app_ui.swipe(up=False)
        confirm_button = adrv.find('^Continue session$')
        assert confirm_button
        nodes = adrv.nodes()
        target = next((n for n in nodes if re.search(r'^3 / Long Road Home /', adrv.label(n))), None)
        assert target, 'Three-row fixture must fit below the saved-session controls'
        Proxy.started.clear(); Proxy.release.clear(); Proxy.delay = True
        confirm('Continue session')
        assert Proxy.started.wait(10), 'native refresh did not reach delay proxy'
        # The sheet can scroll while refreshing; use actual touch target from a
        # fresh settled pre-request layout. Queue starts playing by native touch.
        adrv.tap_node(target)
        late_playing = wait_for(music_e2e.session_state, lambda s: s['state'] == 'PLAYING' and s['title'] == 'Long Road Home', timeout=8)
        Proxy.release.set()
        time.sleep(.7)
        assert music_e2e.session_state()['title'] == 'Long Road Home'
        assert music_e2e.session_state()['state'] == 'PLAYING'
        active = read()
        assert active['entries'][active['index']]['mediaId'] == expected['entries'][2]['media_id']
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
        wait_for(music_e2e.session_state, lambda s: s['state'] == 'PAUSED')
        record('Delayed native Continue response cannot jump newer touch playback', playing=late_playing, active=active)
        queue()
        confirm('Replace saved session')
        final = wait_for(server, lambda q: q['current_id'] == active['entries'][active['index']]['id'])
        shot('native-saved-final')
        record('Native Replace saves current ordered queue for browser continuation', saved=final, local=read())
        app_ui.back()
    finally:
        Proxy.release.set()
        if original_night in ('yes', 'no', 'auto'):
            adrv.adb('shell', 'cmd', 'uimode', 'night', original_night)
        (args.evidence / 'requests.json').write_text(json.dumps(Proxy.requests, indent=2))
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
        proxy.shutdown()
        proxy.server_close()


if __name__ == '__main__':
    main()
