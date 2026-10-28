#!/usr/bin/env python3
"""Staged native podcast acceptance on TWO persistent, parent-owned installs.

No install, pm clear, emulator/server lifecycle or preference writes. Run A
standalone, A associate, B resume, browser resume stages, B finish, each client's
observe-completed, one client's uncomplete, then observe-uncompleted and cleanup.
Parent supplies both per-device reverse mappings and an installed accessibility
probe (music_accessibility_probe.py). --serial selects exactly one installation.
Audio stages require its real QEMU output WAV. Results are stage-specific; no
single stage claims all of task 5i3. Failed attempts remain in separate folders.
"""
import argparse
from contextlib import contextmanager
from datetime import datetime, timezone
import hashlib
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import sys
import threading
import time
from urllib.parse import parse_qs, urlsplit

import adrv
import app_ui
import music_accessibility_probe as accessibility
from music_e2e import EmulatorAudio, session_state, open_track

SUPPORT = Path(__file__).resolve().parents[2] / 'integration_test/support'
sys.path.insert(0, str(SUPPORT))
import audio_trace
from podcast_fixture import load, podcast_media
from fixture_server import Client


FOOTER = re.compile(r'^(Syncing…|Retry|Sync now|Sign in required)$')


def wait_footer_clear(timeout=20):
    """The sync footer is hidden once everything is synced and fresh."""
    end = time.time() + timeout
    while time.time() < end:
        if not any(FOOTER.search(adrv.label(node)) for node in adrv.nodes()):
            return True
        time.sleep(1)
    return False


PKG = app_ui.PKG


def feed_label_pattern(title):
    # Flutter merges a feed ListTile's title and URL into one accessibility
    # label. Keep an exact title boundary rather than a loose substring match.
    return '^' + re.escape(title) + r'(?: /|$)'


class PlayerProxy:
    """Small real-HTTP fault boundary; credentials are relayed, never logged."""
    def __init__(self, port, *, fail_progress=False, fail_podcasts=False, delay_scope=False,
                 strip_auth=False, hold_seconds=15):
        self.port = port
        self.fail_progress = fail_progress
        self.fail_podcasts = fail_podcasts
        self.delay_scope = delay_scope
        self.strip_auth = strip_auth
        self.hold_seconds = hold_seconds
        self.held = threading.Event()
        self.release = threading.Event()
        self.trace = []

    def handler(self):
        proxy = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def relay(self):
                path = self.path.split('?')[0]
                body = self.rfile.read(int(self.headers.get('Content-Length', 0))) or None
                if ((proxy.fail_progress and re.match(r'^/api/(v1/)?progress(?:/|$)', path)) or
                        (proxy.fail_podcasts and path.endswith('/podcasts/sync'))):
                    proxy.trace.append({'path': path, 'method': self.command, 'transport_failed': True})
                    self.connection.shutdown(2)
                    self.connection.close()
                    return
                connection = http.client.HTTPConnection('127.0.0.1', proxy.port, timeout=20)
                try:
                    headers = {k: v for k, v in self.headers.items() if k.lower() not in ('host', 'connection')}
                    if proxy.strip_auth and path.endswith('/podcasts/sync'):
                        headers = {k: v for k, v in headers.items() if k.lower() not in ('authorization', 'cookie')}
                    connection.request(self.command, self.path, body=body, headers=headers)
                    response = connection.getresponse()
                    data = response.read()
                    scoped = False
                    if path.endswith('/podcasts/sync') and body:
                        scoped = bool(json.loads(body).get('scope'))
                    event = {'path': path, 'method': self.command, 'status': response.status,
                             'scoped': scoped, 'received_ms': round(time.monotonic() * 1000)}
                    proxy.trace.append(event)
                    if scoped and proxy.delay_scope and not proxy.held.is_set():
                        proxy.held.set()
                        if not proxy.release.wait(proxy.hold_seconds):
                            event['hold_timed_out'] = True
                            raise TimeoutError('Bounded scoped-response hold expired')
                    event['delivered_ms'] = round(time.monotonic() * 1000)
                    self.send_response(response.status)
                    for key, value in response.getheaders():
                        if key.lower() not in ('transfer-encoding', 'connection', 'content-length'):
                            self.send_header(key, value)
                    self.send_header('Content-Length', response.getheader('Content-Length', '0')
                                     if self.command == 'HEAD' else str(len(data)))
                    self.end_headers()
                    if self.command != 'HEAD':
                        self.wfile.write(data)
                except (BrokenPipeError, ConnectionResetError):
                    pass
                finally:
                    connection.close()
            do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = do_HEAD = relay
        return Handler


class PublisherProxy:
    """Forward actual publisher bytes with one controlled device-side boundary."""

    def __init__(self, port, *, hold_tag=None, fail_tag=None):
        self.port = port
        self.hold_tag = hold_tag
        self.fail_tag = fail_tag
        self.held = threading.Event()
        self.release = threading.Event()
        self.trace = []

    def handler(self):
        proxy = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def serve(self):
                url = urlsplit(self.path)
                tag = parse_qs(url.query).get('episode', [''])[0]
                event = {'path': url.path, 'episode_tag': tag, 'method': self.command,
                         'player_credentials_present': bool(
                             self.headers.get('Authorization') or self.headers.get('Cookie'))}
                proxy.trace.append(event)
                if self.command == 'GET' and tag == proxy.fail_tag:
                    event['status'] = 503
                    self.send_response(503)
                    self.send_header('Content-Length', '0')
                    self.end_headers()
                    return
                connection = http.client.HTTPConnection('127.0.0.1', proxy.port, timeout=20)
                try:
                    headers = {k: v for k, v in self.headers.items()
                               if k.lower() not in ('host', 'connection')}
                    connection.request(self.command, self.path, headers=headers)
                    response = connection.getresponse()
                    data = response.read()
                    event['status'] = response.status
                    event['received_ms'] = round(time.monotonic() * 1000)
                    if (self.command == 'GET' and tag == proxy.hold_tag
                            and not proxy.held.is_set()):
                        proxy.held.set()
                        if not proxy.release.wait(25):
                            event['hold_timed_out'] = True
                            return
                    event['delivered_ms'] = round(time.monotonic() * 1000)
                    self.send_response(response.status)
                    for key, value in response.getheaders():
                        if key.lower() not in ('transfer-encoding', 'connection', 'content-length'):
                            self.send_header(key, value)
                    self.send_header('Content-Length', response.getheader('Content-Length', '0')
                                     if self.command == 'HEAD' else str(len(data)))
                    self.end_headers()
                    if self.command != 'HEAD':
                        self.wfile.write(data)
                except (BrokenPipeError, ConnectionResetError):
                    pass  # The expected result when native Cancel closes its socket.
                finally:
                    connection.close()

            do_GET = do_HEAD = serve

        return Handler


def checked_adb(*args, binary=False, optional=False):
    result = subprocess.run(['adb', *args], capture_output=True, timeout=30)
    if result.returncode and not optional:
        raise AssertionError(f'adb {args[0]} failed ({result.returncode})')
    if result.returncode:
        return None
    return result.stdout if binary else result.stdout.decode().strip()


def require_resume(expected, actual, elapsed, duration):
    if not 0 < expected < duration - 5:
        raise AssertionError('Resume proof requires an established nonterminal saved position')
    if actual < expected - 1 or actual > expected + elapsed + 3:
        raise AssertionError(f'Resume differs from saved history: expected {expected}, got {actual}')


def require_audio(analysis, frequency):
    segments = analysis.get('segments', [])
    best = max(segments, key=lambda item: item['duration_s'], default={})
    heard = best.get('tone_hz')
    if analysis.get('audible_s', 0) < 2 or not heard or abs(heard - frequency) > frequency * .02:
        raise AssertionError('Actual native output did not contain the expected publisher tone')


def register_installation(ledger, name, identity):
    other = ledger.get('B' if name == 'A' else 'A')
    if other and (other['serial'] == identity['serial'] or other['android_id'] == identity['android_id']):
        raise AssertionError('A and B must be separate persistent Android installations')
    previous = ledger.get(name)
    def first_install(item):
        match = re.search(r'^firstInstallTime=(.+)$', item['installed'], re.MULTILINE)
        return match.group(1) if match else None
    if previous and (previous['serial'] != identity['serial'] or
                     previous['android_id'] != identity['android_id'] or
                     not first_install(previous) or
                     first_install(previous) != first_install(identity)):
        raise AssertionError('Installation identity changed between stages; preserve its app data')
    ledger[name] = identity


def episode_title_label(node):
    """Strip list prefixes such as '0, ' from the merged episode a11y label."""
    head = adrv.label(node).split(' / ')[0]
    return re.sub(r'^\d+,\s*', '', head)


def select_row_action(nodes, title, action):
    """Choose an actual labeled action inside the visible episode's extent."""
    titles = [node for node in nodes
              if re.match(r'^(?:\d+,\s*)?(Alpha|Beta) Episode \d{3}(?:$| /)', adrv.label(node))]
    target = [node for node in titles if episode_title_label(node) == title]
    if len(target) != 1:
        raise AssertionError('Expected exactly one visible target episode title')
    top = target[0]['b'][1]
    bottom = min((node['b'][1] for node in titles if node['b'][1] > top), default=10**6)
    actions = [node for node in nodes if re.fullmatch(action, adrv.label(node))
               and top <= node['b'][1] < bottom and node['b'][3] > node['b'][1]
               and node['click']]
    if not actions:
        raise AssertionError('Expected exactly one visible clickable action for the target episode')
    if len(actions) > 1:
        # Alternation (Play|Resume|Play from beginning) matches sibling controls;
        # prefer resume/continue over restart. Duplicate identical labels stay an error.
        labels = {adrv.label(node) for node in actions}
        if len(labels) == 1:
            raise AssertionError('Expected exactly one visible clickable action for the target episode')
        rank = {'Resume': 0, 'Play': 1, 'Play from beginning': 2}
        actions.sort(key=lambda node: (rank.get(adrv.label(node), 99), node['b'][1]))
    return actions[0]


def episode_viewport_bottom(nodes):
    """The fixed sync footer covers episode controls exposed in semantics."""
    screen_bottom = max((node['b'][3] for node in nodes if node['cls'] == 'FrameLayout'
                         and node['b'][1] == 0), default=max(node['b'][3] for node in nodes))
    footer = [node['b'][1] for node in nodes if node['click']
              and adrv.label(node) in ('Sync now', 'Retry')
              and node['b'][1] >= screen_bottom * .8]
    return max(footer) if footer else None


def action_above_footer(node, bottom):
    return bottom is None or node['b'][3] <= bottom - 8


def require_publisher_response(trace, tag, status):
    matches = [event for event in trace if event.get('episode_tag') == tag
               and event.get('method') == 'GET' and event.get('status') == status]
    if not matches or any(event.get('player_credentials_present') for event in matches):
        raise AssertionError(f'Expected credential-free publisher GET {status} for {tag}')
    return matches[-1]


def require_download_state(snapshot, episode, state):
    rows = [row for row in snapshot['podcast_downloads']
            if row['episode_key'] == episode['key']]
    if len(rows) != 1 or rows[0]['state'] != state:
        raise AssertionError(f'Expected one {state} device copy for {episode["title"]}')
    if state == 'available' and not rows[0].get('actual_sha256'):
        raise AssertionError('Available device copy has no verified bytes')
    if state in ('missing', 'failed') and rows[0].get('actual_sha256'):
        raise AssertionError('Incomplete device copy was exposed as playable bytes')
    return rows[0]


def seek_bar_node(nodes):
    candidates = [node for node in nodes if node['cls'] == 'SeekBar'
                  and node['b'][2] > node['b'][0]
                  and node['b'][3] > node['b'][1]]
    if len(candidates) != 1:
        raise AssertionError('Expected one visible native playback seek bar')
    return candidates[0]


def seek_track_swipe(nodes, bar, fraction=.62):
    """Map the accessibility thumb to the full horizontal slider track."""
    screens = [node['b'] for node in nodes if node['cls'] == 'FrameLayout'
               and node['b'][1] == 0 and node['b'][2] > node['b'][0]]
    if not screens:
        raise AssertionError('Native player has no full-screen geometry for seek')
    left, _, right, _ = max(screens, key=lambda bounds: bounds[2] - bounds[0])
    width = right - left
    if width < 400 or not 0 < fraction < 1:
        raise AssertionError('Invalid native slider track geometry')
    thumb_x = (bar['b'][0] + bar['b'][2]) // 2
    y = (bar['b'][1] + bar['b'][3]) // 2
    match = re.fullmatch(r'(\d{1,3})%', adrv.label(bar))
    progress = int(match.group(1)) / 100 if match else .5
    # Flutter exposes the thumb (132 px), not the full track. With symmetric
    # insets, the thumb position and its percent estimate the track endpoint.
    inset = ((thumb_x - left - progress * width) / (1 - 2 * progress)
             if abs(1 - 2 * progress) >= .25 else width * .12)
    if not width * .06 <= inset <= width * .22:
        inset = width * .12
    track_left, track_right = left + inset, right - inset
    if not track_left - 12 <= thumb_x <= track_right + 12:
        raise AssertionError('Seek thumb is outside inferred playback track')
    target_x = round(track_left + fraction * (track_right - track_left))
    return thumb_x, y, target_x


def require_seek_jump(reset, target, title):
    start = reset.get('position_ms')
    end = target.get('position_ms')
    if (reset.get('state') != 'PAUSED' or target.get('state') != 'PAUSED'
            or reset.get('title') != title or target.get('title') != title
            or not isinstance(start, int) or not isinstance(end, int)
            or not 0 <= start <= 1500 or not 5500 <= end <= 10000
            or end - start < 4500):
        raise AssertionError(f'Actual native offline seek was not a large touch jump: '
                             f'reset={start}, target={end}, '
                             f'reset_title={reset.get("title")!r}, '
                             f'target_title={target.get("title")!r}, '
                             f'reset_state={reset.get("state")!r}, '
                             f'target_state={target.get("state")!r}')


def require_seek_silence(analysis):
    if analysis.get('audible_s', 0) > .35:
        raise AssertionError(f'Podcast remained audible during paused touch seek: '
                             f'{analysis["audible_s"]}s')


def wait_paused_session(title, timeout=4, reader=None, max_position=None):
    """Require an actual stable paused media session before touching the slider."""
    reader = reader or session_state
    deadline = time.monotonic() + timeout
    samples = []
    while True:
        first = reader()
        samples.append((first.get('state'), first.get('position_ms')))
        if first.get('state') == 'PAUSED' and first.get('title') == title:
            time.sleep(.65)
            second = reader()
            samples.append((second.get('state'), second.get('position_ms')))
            before, after = first.get('position_ms'), second.get('position_ms')
            if (second.get('state') == 'PAUSED' and second.get('title') == title
                    and isinstance(before, int) and isinstance(after, int)
                    and abs(after - before) <= 200
                    and (max_position is None or 0 <= after <= max_position)):
                return second
        if time.monotonic() >= deadline:
            raise AssertionError(f'Native playback was not stably paused: samples={samples}')
        time.sleep(.2)


def wait_seek_jump(reset, title, timeout=20, reader=None, clock=None):
    """Wait for serialized touch seeks to settle at a stable paused target."""
    reader = reader or session_state
    clock = clock or time.monotonic
    started = clock()
    samples = []
    candidate = None
    candidate_at = None
    while True:
        target = reader()
        sample = {'elapsed_ms': round((clock() - started) * 1000),
                  'position_ms': target.get('position_ms'),
                  'state': target.get('state')}
        samples.append(sample)
        if target.get('state') not in ('PAUSED', 'BUFFERING'):
            raise AssertionError(f'Native playback started during touch seek: samples={samples}')
        if target.get('state') == 'PAUSED':
            try:
                require_seek_jump(reset, target, title)
            except AssertionError:
                candidate = candidate_at = None
            else:
                if (candidate is None or abs(target['position_ms'] -
                                             candidate['position_ms']) > 200):
                    candidate, candidate_at = target, sample['elapsed_ms']
                elif sample['elapsed_ms'] - candidate_at >= 700:
                    return target, samples
        else:
            candidate = candidate_at = None
        if sample['elapsed_ms'] >= timeout * 1000:
            raise AssertionError(f'No stable paused touch-seek destination: '
                                 f'reset={reset.get("position_ms")}, samples={samples}')
        time.sleep(.2)


def wait_seek_bar(timeout=12, min_percent=None, max_percent=None):
    """The player route can appear after its media session starts playing."""
    deadline = time.monotonic() + timeout
    while True:
        try:
            nodes = adrv.nodes()
            bar = seek_bar_node(nodes)
            if min_percent is not None or max_percent is not None:
                match = re.fullmatch(r'(\d{1,3})%', adrv.label(bar))
                if (not match or (min_percent is not None
                                  and int(match.group(1)) < min_percent)
                        or (max_percent is not None
                            and int(match.group(1)) > max_percent)):
                    raise AssertionError('Seek thumb has not settled at the expected position')
            return nodes, bar
        except (AssertionError, RuntimeError) as error:
            if time.monotonic() >= deadline:
                raise AssertionError(f'Native player seek bar did not appear: {error}')
            time.sleep(.25)


class Run:
    def __init__(self, args):
        self.args = args
        os.environ['ANDROID_SERIAL'] = args.serial
        self.directory, self.state = load(args.fixture)
        self.auth = json.loads((self.directory / 'auth.json').read_text())
        self.output = self.directory / 'evidence' / f'native-{args.client}' / (
            datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S.%fZ') + '-' + args.stage)
        self.output.mkdir(parents=True)
        self.audio = EmulatorAudio(args.emulator_wav)
        self.results = []
        self.scope = f'account:{self.state["origin"]}#{self.state["accounts"]["admin"]}'
        self.feed = self.state['feeds'][0]
        self.episode = self.feed['episodes'][0]
        identity = {'serial': checked_adb('get-serialno'),
                    'android_id': checked_adb('shell', 'settings', 'get', 'secure', 'android_id'),
                    'installed': checked_adb('shell', 'dumpsys', 'package', PKG)}
        if not identity['android_id'] or 'versionName=' not in identity['installed']:
            raise AssertionError('A current normal Player APK must already be installed')
        identity['installed'] = '\n'.join(line.strip() for line in identity['installed'].splitlines()
            if re.search(r'versionName=|versionCode=|firstInstallTime=', line))
        ledger_file = self.directory / 'native-installations.json'
        ledger = json.loads(ledger_file.read_text()) if ledger_file.exists() else {}
        register_installation(ledger, args.client, identity)
        ledger_file.write_text(json.dumps(ledger, indent=1))
        (self.output / 'installation.json').write_text(json.dumps(identity, indent=1))

    def record(self, name, **data):
        self.results.append({'name': name, 'result': 'pass', 'data': data})
        print('PASS ' + name, flush=True)

    def shot(self, name):
        path = self.output / f'{name}.png'
        adrv.shot(str(path))
        return str(path)

    def touch(self, pattern, cls=None):
        node = adrv.wait(pattern, timeout=15, cls=cls) or adrv.reveal(pattern, cls)
        if not node:
            raise AssertionError('Missing labeled native control: ' + pattern)
        # AppBar titles reuse action labels (e.g. Sign In); prefer a clickable hit.
        if not node.get('click') and cls is None:
            clickable = [n for n in adrv.nodes()
                         if re.search(pattern, adrv.label(n)) and n.get('click')]
            if clickable:
                node = clickable[0]
        self.shot(f'touch-{len(list(self.output.glob("*.png"))):03}')
        adrv.tap_node(node)

    def field(self, hint, value):
        nodes = accessibility.snapshot(self.output / f'field-{hint.replace(" ", "-")}-{time.time_ns()}.json')
        target = accessibility.editable_field(nodes, hint)
        adrv.tap_node({'b': target['bounds']})
        app_ui.clear_field(160)
        adrv.type_text(value)
        # Refuse silent mangling (device sh historically turned http:// into //…).
        # Password fields expose same-length bullets, not plaintext.
        texts = [node['text'] or '' for node in adrv.nodes() if node['cls'] == 'EditText']
        masked = '•' * len(value)
        if value not in texts and masked not in texts:
            raise AssertionError(f'Native field {hint!r} did not retain typed value {value!r}; saw {texts!r}')
        checked_adb('shell', 'input', 'keyevent', 'KEYCODE_BACK')
    def root(self):
        app_ui.launch()
        for _ in range(12):
            labels = app_ui.labels()
            if 'On this device' in labels or 'Open navigation menu' in labels or 'Welcome back' in labels:
                return
            # Cold start can land on the server chooser. Prefer the fixture
            # origin so account stages stay out of empty local-only mode.
            origin = self.state.get('origin')
            if origin and adrv.find('^' + re.escape(origin) + '$'):
                self.touch('^' + re.escape(origin) + '$')
                continue
            if adrv.find(r'^Use files on this device$'):
                self.touch(r'^Use files on this device$')
                continue
            app_ui.back()
        raise AssertionError('Native app did not return to an established root')

    def podcasts(self):
        self.root()
        if adrv.find('^Welcome back$'):
            self.login()
        # Account library exposes the drawer; local-only mode uses the
        # overflow menu (or a leftover direct tile on older builds).
        if adrv.find('^Open navigation menu$'):
            self.touch('^Open navigation menu$')
            self.touch('^Podcasts$')
        elif adrv.find('^Podcasts on this device$'):
            # Older builds still place Podcasts on this device on the toolbar.
            self.touch('^Podcasts on this device$')
        elif adrv.find('^More options$'):
            self.touch('^More options$')
            self.touch('^Podcasts on this device$')
        else:
            raise AssertionError('No podcasts entry from the established root')
        if not adrv.wait('^All podcast episodes$'):
            raise AssertionError('Podcasts destination not visible')

    def episodes(self, combined=True, filter_name='Uncompleted',
                 feed_title='Acceptance Alpha'):
        self.podcasts()
        self.touch('^All podcast episodes$' if combined else feed_label_pattern(feed_title))
        if filter_name == 'Uncompleted':
            chips = [node for node in adrv.nodes() if adrv.label(node) == 'Uncompleted']
            if len(chips) != 1 or not (chips[0]['sel'] == 'true' or chips[0]['checked'] == 'true'):
                raise AssertionError('Fresh native episode view did not default to Uncompleted')
        self.touch('^' + filter_name + '$')
        selected = [node for node in adrv.nodes() if adrv.label(node) == filter_name]
        if not any(node.get('sel') == 'true' or node.get('checked') == 'true' for node in selected):
            raise AssertionError(f'Episode filter {filter_name!r} did not become selected')

    def row(self, action, title=None, required=True):
        title = title or self.episode['title']
        for _ in range(80):
            nodes = adrv.nodes()
            bottom = episode_viewport_bottom(nodes)
            try:
                target = select_row_action(nodes, title, action)
                if not action_above_footer(target, bottom):
                    app_ui.swipe()
                    continue
                self.shot(f'row-{len(list(self.output.glob("*.png"))):03}')
                adrv.tap_node(target)
                return True
            except AssertionError:
                if any(title in adrv.label(node) for node in nodes):
                    titles = [node for node in nodes if episode_title_label(node) == title]
                    if bottom is not None and any(node['b'][1] > bottom * .6 for node in titles):
                        app_ui.swipe()
                        continue
                    # Optional cleanup: title visible without the action means absent.
                    if not required:
                        return False
                    # Title is on-screen: Download stays non-clickable until
                    # PodcastDownloadControls' FutureBuilder settles — wait, do
                    # not page away. Swipe only when the row is clipped.
                    time.sleep(0.8)
                    continue
                # Combined catalogs page newest-first; advance when the title is absent.
                nxt = next((node for node in nodes
                            if adrv.label(node) == 'Next page' and node['click']), None)
                if nxt:
                    adrv.tap_node(nxt)
                    time.sleep(0.8)
                    continue
                app_ui.swipe()
        if not required:
            return False
        raise AssertionError('Could not reach native episode action by label')

    def wait_downloaded_row(self, title, timeout=90):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            nodes = adrv.nodes()
            bottom = episode_viewport_bottom(nodes)
            try:
                action = select_row_action(nodes, title, 'Delete downloaded file')
                if action_above_footer(action, bottom):
                    return
            except AssertionError:
                pass
            titles = [node for node in nodes if episode_title_label(node) == title]
            if bottom is not None and any(node['b'][1] > bottom * .6 for node in titles):
                app_ui.swipe()
            else:
                time.sleep(.5)
        raise AssertionError(f'Device copy for {title} did not become available')

    def wait_row_action(self, title, action, timeout=30):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            nodes = adrv.nodes()
            bottom = episode_viewport_bottom(nodes)
            try:
                target = select_row_action(nodes, title, action)
                if action_above_footer(target, bottom):
                    return target
            except AssertionError:
                pass
            titles = [node for node in nodes if episode_title_label(node) == title]
            if bottom is not None and any(node['b'][1] > bottom * .6 for node in titles):
                app_ui.swipe()
            else:
                time.sleep(.5)
        raise AssertionError(f'Native {action} action did not appear for {title}')

    def no_server_audio(self):
        counts = podcast_media(self.state['database'])
        if any(counts.values()):
            raise AssertionError('Android/state sync unexpectedly created server podcast audio/media')
        self.record('no server podcast copy', **counts)

    @contextmanager
    def proxy(self, **options):
        mapping = f'tcp:{self.state["port"]}'
        mappings = checked_adb('reverse', '--list').splitlines()
        original = next((line.split()[-1] for line in mappings if line.split()[-2] == mapping), None)
        if original is None:
            raise AssertionError('Parent must establish the selected device Player reverse mapping')
        proxy = PlayerProxy(self.state['port'], **options)
        server = ThreadingHTTPServer(('127.0.0.1', 0), proxy.handler())
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            checked_adb('reverse', mapping, f'tcp:{server.server_port}')
            yield proxy
        finally:
            proxy.release.set()
            try:
                checked_adb('reverse', mapping, original)
            finally:
                server.shutdown()
                server.server_close()
                thread.join()
                (self.output / 'proxy-trace.json').write_text(json.dumps(proxy.trace, indent=1))

    @contextmanager
    def publisher_proxy(self, *, evidence_name='publisher-proxy-trace', **options):
        mapping = f'tcp:{self.state["publisher_port"]}'
        mappings = checked_adb('reverse', '--list').splitlines()
        original = next((line.split()[-1] for line in mappings
                         if line.split()[-2] == mapping), None)
        if original is None:
            raise AssertionError('Parent must establish the selected device publisher reverse mapping')
        proxy = PublisherProxy(self.state['publisher_port'], **options)
        server = ThreadingHTTPServer(('127.0.0.1', 0), proxy.handler())
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            checked_adb('reverse', mapping, f'tcp:{server.server_port}')
            yield proxy
        finally:
            proxy.release.set()
            try:
                checked_adb('reverse', mapping, original)
            finally:
                server.shutdown()
                server.server_close()
                thread.join()
                (self.output / f'{evidence_name}.json').write_text(
                    json.dumps(proxy.trace, indent=1))

    def _device_bytes(self, path, optional=False):
        """Read a private app path; non-root emulators need su 0."""
        data = checked_adb('exec-out', 'cat', path, binary=True, optional=True)
        if data and not data.startswith(b'cat: '):
            return data
        data = checked_adb('exec-out', 'su', '0', 'cat', path, binary=True, optional=True)
        if data and not data.startswith(b'cat: '):
            return data
        if optional:
            return b''
        raise AssertionError('Could not read device path ' + path)

    def snapshot(self, name):
        """Pause first; force-stop owned app so DB+WAL copies form one snapshot."""
        checked_adb('shell', 'am', 'force-stop', PKG)
        time.sleep(1.5)
        folder = self.output / name
        folder.mkdir()
        root = f'/data/user/0/{PKG}/databases'
        db_path = folder / 'podcasts.db'
        for attempt in range(5):
            for suffix in ('', '-wal', '-shm'):
                data = self._device_bytes(root + '/podcasts.db' + suffix, optional=True)
                if data:
                    (folder / ('podcasts.db' + suffix)).write_bytes(data)
            if db_path.exists() and db_path.read_bytes()[:15] == b'SQLite format 3':
                break
            time.sleep(0.8)
        else:
            raise AssertionError('Could not copy a readable podcasts.db snapshot from the device')
        with sqlite3.connect(db_path.as_uri() + '?mode=ro', uri=True) as db:
            db.row_factory = sqlite3.Row
            keys = [feed['key'] for feed in self.state['feeds']]
            out = {}
            for table in ('podcast_subscriptions', 'podcast_episode_state', 'podcast_downloads'):
                out[table] = [dict(row) for row in db.execute(
                    f'SELECT * FROM {table} WHERE feed_key IN (?,?)', keys)]
            out['podcast_scopes'] = [dict(row) for row in db.execute(
                'SELECT * FROM podcast_scopes WHERE scope IN (?,?)', ('local', self.scope))]
        # SQLite files may contain unrelated scoped metadata: keep only the
        # sanitized scoped JSON, never ship full app databases or preferences.
        for path in folder.iterdir():
            path.unlink()
        folder.rmdir()
        for copy in out['podcast_downloads']:
            if copy['state'] == 'available':
                data = self._device_bytes(root + '/podcast-downloads/' + copy['id'] + '.audio')
                copy['actual_sha256'] = hashlib.sha256(data).hexdigest()
        (self.output / f'{name}.json').write_text(json.dumps(out, indent=1))
        return out

    def saved(self, snapshot, scope=None):
        rows = [row for row in snapshot['podcast_episode_state'] if
            row['scope'] == (scope or self.scope) and row['episode_key'] == self.episode['key']]
        if len(rows) != 1:
            raise AssertionError('Missing unique episode history in captured scope')
        return rows[0]

    def publish_transfer(self, name, data):
        (self.directory / f'transfer-{name}.json').write_text(json.dumps(data, indent=1))

    def playback(self, name, action='(?:Play|Resume)', seconds=4, expected_position=None):
        if not self.audio.available():
            raise AssertionError('Actual native output WAV required for playback proof')
        mark = self.audio.mark()
        started = time.monotonic()
        self.row(action)
        deadline = time.monotonic() + 10
        state = session_state()
        while state['state'] != 'PLAYING' and time.monotonic() < deadline:
            time.sleep(.2)
            state = session_state()
        if state['state'] != 'PLAYING' or state['title'] != self.episode['title']:
            raise AssertionError('Target episode did not become actual native playback')
        onset = time.monotonic() - started
        first_playing = dict(state)
        if expected_position is not None:
            if not isinstance(first_playing.get('position_ms'), (int, float)):
                raise AssertionError('First native PLAYING sample has no actual position')
            require_resume(expected_position, first_playing['position_ms'] / 1000, onset,
                           self.state['audio']['resume']['duration_s'])
        time.sleep(seconds)
        # Prefer the media key during playback — uiautomator often cannot reach
        # an idle hierarchy while the player surface is animating.
        checked_adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            try:
                if adrv.find('^Play$') or session_state().get('state') != 'PLAYING':
                    break
            except RuntimeError:
                pass
            time.sleep(0.5)
        wav = self.audio.save(mark, self.output / f'{name}.wav')
        analysis = audio_trace.analyze(wav)
        require_audio(analysis, self.state['audio']['resume']['tone_hz'])
        self.record(name, startup_s=onset, first_playing=first_playing, audio=analysis)
        return time.monotonic() - started

    def standalone(self):
        mappings = checked_adb('reverse', '--list')
        if f'tcp:{self.state["port"]}' in mappings:
            raise AssertionError('Parent must remove only the Player reverse mapping for standalone stage')
        self.no_server_audio()
        self.podcasts()
        for feed in self.state['feeds']:
            if adrv.find(feed_label_pattern(feed['title'])):
                continue  # A failed driver attempt must not edit an existing subscription.
            self.touch('^Subscribe to podcast$')
            self.field('Feed URL', feed['url'])
            self.touch('^Subscribe$')
            if not adrv.wait(feed_label_pattern(feed['title'])):
                raise AssertionError('Direct publisher subscription was not visible')
        self.episodes()
        if not any('Acceptance Alpha' in label for label in app_ui.labels()):
            raise AssertionError('Combined native view omitted podcast label')
        # All filter: a prior accidental complete hides the target under Uncompleted.
        self.episodes(combined=False, filter_name='All')
        self.row('Mark uncompleted', required=False)
        self.row('Delete downloaded file', required=False)
        self.row('Download')
        # Flutter merges status into the episode semantics label.
        if not adrv.wait(r'Available on this device', timeout=90):
            raise AssertionError('Direct device download did not finish')
        self.playback('standalone audible partial', seconds=6)
        app_ui.back()
        snapshot = self.snapshot('standalone')
        saved = self.saved(snapshot, 'local')
        if not 5 < saved['position_seconds'] < 55 or saved['completed']:
            raise AssertionError('Standalone playback did not retain an uncompleted partial position')
        copies = [copy for copy in snapshot['podcast_downloads'] if copy['episode_key'] == self.episode['key']]
        if len(copies) != 1 or copies[0]['actual_sha256'] != self.state['audio']['resume']['sha256']:
            raise AssertionError('Device bytes do not match publisher fixture')
        self.no_server_audio()
        self.publish_transfer('A', {'position_seconds': saved['position_seconds'], 'source': 'native-A', 'scope': 'local'})
        app_ui.launch()
        self.episodes()
        if not any('Saved position:' in label for label in app_ui.labels()):
            raise AssertionError('Standalone history was not visible after process restart')
        self.record('standalone restart retains visible history and publisher copy')

    def login(self, kind='admin'):
        self.root()
        if adrv.find('^Open navigation menu$'):
            return
        if not adrv.find('^Welcome back$'):
            self.touch('^Connect to server$')
            self.field('Server base URL', self.state['origin'])
            self.touch('^Save URL$')
        if not adrv.wait('^Welcome back$'):
            raise AssertionError('Existing persistent install did not reach sign-in')
        self.field('Username', self.auth[kind]['username'])
        self.field('Password', self.auth[kind]['password'])
        self.touch('^Sign In$')
        if not adrv.wait(r"^(Library|Sync this device's podcasts with .*)$", timeout=30):
            raise AssertionError('Native sign-in did not reach the account library or consent')

    def associate(self):
        self.login()
        # Fresh sign-in offers consent once; retries after a prior PASS still need
        # durable association + clear sync footer proof without re-prompting.
        if adrv.wait(r"^Sync this device's podcasts with ", timeout=12):
            self.shot('explicit-consent')
            self.touch('^Sync$')
        if adrv.find(r'^(Sync now|Retry)$'):
            self.touch(r'^(Sync now|Retry)$')
        # Footer semantics may merge; require durable full success below.
        wait_footer_clear(timeout=60)
        self.no_server_audio()
        snapshot = self.snapshot('associated')
        local, account = self.saved(snapshot, 'local'), self.saved(snapshot)
        if local['position_seconds'] != account['position_seconds']:
            raise AssertionError('Consent did not copy standalone listening history')
        scopes = {row['scope']: row for row in snapshot['podcast_scopes']}
        meta = scopes.get(self.scope) or {}
        if not meta.get('associated_from_local'):
            raise AssertionError('Account scope was not marked associated_from_local')
        if not meta.get('last_full_success_ms'):
            raise AssertionError('Account scope has no durable last_full_success_ms')
        self.publish_transfer('A', {'position_seconds': account['position_seconds'], 'source': 'native-A', 'scope': self.scope})
        self.record('explicit consent synchronizes history without server audio')

    def resume(self):
        transfer = json.loads((self.directory / 'transfer-A.json').read_text())
        self.login()
        self.touch('^Sync now$')
        self.podcasts()
        self.touch('^Refresh feeds$')
        self.episodes()
        # Prior resume attempts may already have left B's independent copy.
        if not any(self.episode['title'] in adrv.label(node) and 'Available on this device' in adrv.label(node)
                   for node in adrv.nodes()):
            self.row('Download')
            if not adrv.wait(r'Available on this device', timeout=90):
                raise AssertionError('Client B did not obtain its own publisher copy')
        elapsed = self.playback('cross-client audible resume',
                                expected_position=transfer['position_seconds'])
        app_ui.back()
        snapshot = self.snapshot('resumed')
        saved = self.saved(snapshot)
        require_resume(transfer['position_seconds'], saved['position_seconds'], elapsed, 60)
        self.no_server_audio()
        self.publish_transfer('B', {'position_seconds': saved['position_seconds'], 'source': 'native-B', 'scope': self.scope})
        self.record('independent native B resumes shared state without server media', position_seconds=saved['position_seconds'])

    def episode_visible(self):
        pattern = r'(?:\d+,\s*)?' + re.escape(self.episode['title']) + r'(?:$| /)'
        return any(re.match(pattern, adrv.label(node)) for node in adrv.nodes())

    def observe(self, completed):
        self.login()
        if adrv.find(r'^(Sync now|Retry)$'):
            self.touch(r'^(Sync now|Retry)$')
            wait_footer_clear(timeout=60)
        self.episodes(combined=False, filter_name='Completed' if completed else 'Uncompleted')
        if not self.episode_visible():
            raise AssertionError('Expected synchronized episode missing from matching filter')
        self.shot('matching-filter')
        self.touch('^Uncompleted$' if completed else '^Completed$')
        if self.episode_visible():
            raise AssertionError('Synchronized episode remained in opposite completion filter')
        self.record('completion filters reconcile', completed=completed)
        snapshot = self.snapshot('observed-independent-copy')
        source = next(copy for copy in snapshot['podcast_downloads'] if copy['episode_key'] == self.episode['key'])
        if source.get('actual_sha256') != self.state['audio']['resume']['sha256']:
            raise AssertionError('Other client/server cleanup changed this independent device copy')
        self.saved(snapshot)
        self.record('independent device copy and history survive other-client operations')

    def uncomplete(self):
        self.episodes(filter_name='Completed')
        before = self.saved(self.snapshot('before-manual-uncompletion'))
        if not before['completed']:
            raise AssertionError('Manual uncompletion needs an established completed episode')
        self.episodes(filter_name='Completed')
        self.row('Mark uncompleted')
        self.touch('^Uncompleted$')
        if not self.episode_visible():
            raise AssertionError('Manual uncompletion did not return default visibility')
        after = self.saved(self.snapshot('after-manual-uncompletion'))
        if ((after['position_seconds'], after['position_ms']) !=
                (before['position_seconds'], before['position_ms'])):
            raise AssertionError('Manual uncompletion changed the saved position register')
        if after['completed'] or after['completion_source'] != 'manual':
            raise AssertionError('Manual uncompletion did not retain episode history with manual status')
        self.record('manual uncompletion returns default visibility and retains position history',
                    before=before, after=after)

    def finish(self):
        # Account library like resume: cold start must not settle in empty local mode.
        # Feed-scoped All: combined Uncompleted can be empty after other clients.
        self.login()
        self.podcasts()
        self.touch('^Refresh feeds$')
        if not adrv.wait(feed_label_pattern('Acceptance Alpha'), timeout=30):
            raise AssertionError('Acceptance Alpha missing after Refresh in account podcasts')
        self.episodes(combined=False, filter_name='All')
        if not self.audio.available():
            raise AssertionError('Native output capture required')
        # Resume near EOF seeks to a terminal position; podcast natural-end
        # rejects that path. Restart from 0 so the decoder can complete.
        mark = self.audio.mark()
        self.row('Play from beginning')
        deadline = time.monotonic() + 90
        playing = False
        terminal_stable = 0
        last_terminal = None
        while time.monotonic() < deadline:
            state = session_state()
            playing = playing or state['state'] == 'PLAYING'
            pos = state['position_ms'] or 0
            if not playing or pos < 59000:
                terminal_stable = 0
                last_terminal = None
                time.sleep(.5)
                continue
            # Podcasts record automatic completion without stopping transport;
            # music would advance/stop via the queue. Accept a stable terminal
            # PLAYING sample or an explicit stop/pause at the real duration.
            if state['state'] in ('STOPPED', 'PAUSED'):
                break
            if state['state'] == 'PLAYING':
                terminal_stable = terminal_stable + 1 if pos == last_terminal else 1
                last_terminal = pos
                if terminal_stable >= 4:
                    break
            time.sleep(.5)
        else:
            raise AssertionError('Native episode did not finish naturally within its real duration')
        wav = self.audio.save(mark, self.output / 'natural-end.wav')
        require_audio(audio_trace.analyze(wav), self.state['audio']['resume']['tone_hz'])
        app_ui.back()
        snapshot = self.snapshot('finished')
        saved = self.saved(snapshot)
        if not saved['completed'] or saved['completion_source'] != 'auto' or saved['position_ms'] != saved['completion_ms']:
            raise AssertionError('Natural end did not atomically persist automatic completion')
        self.record('native natural end persists paired automatic clocks', history=saved)

    def cleanup(self):
        self.episodes(filter_name='All')
        before = self.snapshot('before-delete')
        self.episodes(filter_name='All')
        self.row('Delete downloaded file')
        self.row('Download')
        if not adrv.wait(r'Available on this device', timeout=90):
            raise AssertionError('Redownload did not finish')
        after = self.snapshot('after-redownload')
        if self.saved(before) != self.saved(after):
            raise AssertionError('Device cleanup/redownload changed listening history')
        source = next(copy for copy in after['podcast_downloads'] if copy['episode_key'] == self.episode['key'])
        if source['actual_sha256'] != self.state['audio']['resume']['sha256']:
            raise AssertionError('Redownload bytes differ from publisher')
        self.record('device cleanup and redownload preserve listening history and exact bytes')

    def preplay(self):
        self.episodes(filter_name='All')
        with self.proxy(delay_scope=True) as proxy:
            self.playback('bounded native preplay with held real response', action='Play from beginning')
            if not proxy.held.is_set() or self.results[-1]['data']['startup_s'] > 6.5:
                raise AssertionError('Native playback did not use bounded fallback during actual held sync')
            paused = session_state()
            proxy.release.set()
            time.sleep(1)
            after = session_state()
            if after['state'] != 'PAUSED' or after['position_ms'] != paused['position_ms']:
                raise AssertionError('Late scoped response changed paused native playback')
        self.record('late response does not seek or resume actual native player')

    def browse(self):
        for combined in (False, True):
            self.episodes(combined=combined)
            # Reach the actual bottom pager; never infer pagination from SQLite.
            for _ in range(80):
                if adrv.find('^Next page$'):
                    break
                app_ui.swipe()
            else:
                raise AssertionError('Native page-one pager could not be reached')
            self.touch('^Next page$')
            labels = [adrv.label(node) for node in adrv.nodes()]
            episode_pattern = r'^(?:\d+,\s*)?(?:Alpha|Beta) Episode \d{3}(?:$| /)'
            if not any(re.match(episode_pattern, label) for label in labels):
                raise AssertionError('Next native page did not show authoritative newer-first episode identities')
            if any(re.match(r'^(?:\d+,\s*)?Alpha Episode 001(?:$| /)', label)
                   for label in labels):
                raise AssertionError('Next page incorrectly retained the first episode')
            self.shot('next-page-combined' if combined else 'next-page-folder')
            self.record('native actual folder/combined next page', combined=combined)

    def globalprogress(self):
        self.root()
        if adrv.find('^Welcome back$'):
            self.login()
        source = self.state['ordinary']
        client = Client(self.state['origin']).login(**self.auth['admin'])
        detail = client.request('GET', f'/api/v1/media/{source["id"]}')
        media = detail.get('media', detail)
        relative = Path(source['abs_path']).relative_to(
            Path(self.state['server_directory']) / 'media' / source['set_name'])
        folder = str(relative.parent) if str(relative.parent) != '.' else ''
        base = json.loads((Path(self.state['server_directory']) / 'state.json').read_text())
        manifest = json.loads(Path(base['manifest_file']).read_text())
        spec = next(track for track in manifest['tracks'] if track['path'] ==
                    source['set_name'] + '/' + str(relative))
        if not self.audio.available():
            raise AssertionError('Native ordinary-progress check requires actual output capture')
        with self.proxy(fail_progress=True) as proxy:
            if not open_track(source['set_name'], folder, app_ui.media_title(media)):
                raise AssertionError('Ordinary file was not reached through actual native browse controls')
            mark = self.audio.mark()
            self.touch('^Play Audio$')
            deadline = time.monotonic() + 15
            while session_state()['state'] != 'PLAYING' and time.monotonic() < deadline:
                time.sleep(.2)
            if session_state()['state'] != 'PLAYING':
                raise AssertionError('Ordinary native source did not play')
            time.sleep(4)
            # The fixture lasts twelve seconds. A fresh UI dump can outlive
            # the remaining audio, so use Android's real transport control.
            checked_adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
            if session_state()['state'] != 'PAUSED':
                raise AssertionError('Ordinary native source did not pause before its natural end')
            wav = self.audio.save(mark, self.output / 'ordinary-progress.wav')
            require_audio(audio_trace.analyze(wav), spec['tone_hz'][0])
            if not adrv.wait('pending', timeout=20):
                raise AssertionError('Ordinary progress was not retained as pending during real transport failure')
            self.shot('ordinary-pending')
            with self.stale_clock():
                self.touch('^Retry$')
                if not adrv.wait('Last synced 2 days ago', timeout=25):
                    raise AssertionError('Partial native sync cleared controlled stale age')
                if not any(item.get('status') == 200 and item['path'].endswith('/podcasts/sync')
                           for item in proxy.trace):
                    raise AssertionError('Partial check needs a real successful podcast domain')
                self.shot('partial-stale-native')
                self.record('native partial success retains stale age while ordinary progress remains pending')
            checked_adb('shell', 'am', 'force-stop', PKG)
            app_ui.launch()
            self.login()
            if not adrv.wait('pending', timeout=20):
                raise AssertionError('Queued ordinary progress was not visible after process restart')
        self.touch('^Retry$')
        if not wait_footer_clear(timeout=30):
            raise AssertionError('Global Retry did not include queued ordinary progress')
        with sqlite3.connect(Path(self.state['database']).as_uri() + '?mode=ro', uri=True) as db:
            progress = db.execute('SELECT position_seconds FROM playback_progress WHERE user_id=? AND media_id=?',
                (self.state['accounts']['admin'], source['id'])).fetchone()
        if not progress or progress[0] <= 0:
            raise AssertionError('Ordinary position did not reach actual shared server history')
        self.record('global retry delivers actual ordinary audio progress after process death', position_seconds=progress[0])

    def switch_account(self, kind):
        self.root()
        if not adrv.find('^Welcome back$'):
            self.touch('^Settings$')
            for _ in range(30):
                if adrv.find('^Log Out$'):
                    break
                app_ui.swipe()
            self.touch('^Log Out$')
        self.login(kind)
        if adrv.find(r"^Sync this device's podcasts with "):
            self.touch('^Keep separate$')

    def isolation(self):
        if 'other' not in self.state['accounts']:
            raise AssertionError('Parent must run explicit fixture setup for its isolation account')
        with self.proxy(fail_podcasts=True):
            self.switch_account('other')
            status = adrv.wait(r'^(Not synced yet|Sync unavailable)$', timeout=20)
            if not status:
                raise AssertionError('Other account did not expose a quiet, truthful sync state')
            self.shot('quiet-other-account')
            self.podcasts()
            if adrv.find(feed_label_pattern('Acceptance Alpha')):
                raise AssertionError('Primary account subscriptions leaked into the other account')
            self.record('native other-account sync feedback is quiet and subscriptions are isolated',
                        status=adrv.label(status))
        self.touch(r'^(Sync now|Retry)$')
        if not wait_footer_clear(timeout=30):
            raise AssertionError('Other account did not reach full success after reconnection')
        self.switch_account('admin')
        self.podcasts()
        if not adrv.wait(feed_label_pattern('Acceptance Alpha')):
            raise AssertionError('Primary account subscriptions did not return after switching back')
        with sqlite3.connect(Path(self.state['database']).as_uri() + '?mode=ro', uri=True) as db:
            other = self.state['accounts']['other']
            count = db.execute('SELECT count(*) FROM podcast_subscriptions WHERE user_id=? AND feed_key IN (?,?)',
                (other, self.state['feeds'][0]['key'], self.state['feeds'][1]['key'])).fetchone()[0]
        if count:
            raise AssertionError('Keep separate copied standalone subscriptions to another account')
        self.record('explicit Keep separate never associates local history with the other account')

    def held_account(self):
        """A real scoped reply must not populate the account selected after logout."""
        if 'other' not in self.state['accounts']:
            raise AssertionError('Held-account check needs the fixture isolation account')
        self.episodes(filter_name='All')
        # Returning from playback to Settings can take over ninety seconds on
        # the owned emulator while Android's accessibility bridge is busy.
        with self.proxy(delay_scope=True, hold_seconds=240) as proxy:
            self.row('Play from beginning')
            if not proxy.held.wait(10):
                raise AssertionError('Podcast playback did not issue a real scoped sync request')
            scoped = [event for event in proxy.trace if event.get('scoped')]
            if not scoped or scoped[-1].get('status') != 200 or 'delivered_ms' in scoped[-1]:
                raise AssertionError('Scoped reply was not held after upstream success')
            self.root()
            self.touch('^Settings$')
            for _ in range(30):
                if adrv.find('^Log Out$'):
                    break
                app_ui.swipe()
            self.touch('^Log Out$')
            if not adrv.wait('^Welcome back$'):
                raise AssertionError('Logout did not revoke the selected account while reply was held')
            proxy.release.set()
            self.login('other')
            if adrv.find(r"^Sync this device's podcasts with "):
                self.touch('^Keep separate$')
            deadline = time.monotonic() + 5
            while 'delivered_ms' not in scoped[-1] and time.monotonic() < deadline:
                time.sleep(.1)
            if scoped[-1].get('hold_timed_out') or 'delivered_ms' not in scoped[-1]:
                raise AssertionError('Held scoped reply did not leave the proxy after logout')
            self.podcasts()
            if adrv.find(feed_label_pattern('Acceptance Alpha')):
                raise AssertionError('Late admin reply exposed its subscription in the other account')
            other = self.snapshot('other-account-after-held')
            other_scope = f'account:{self.state["origin"]}#{self.state["accounts"]["other"]}'
            if any(row['scope'] == other_scope and row['episode_key'] == self.episode['key']
                   for row in other['podcast_episode_state']):
                raise AssertionError('Late admin reply wrote episode history into the other account')
            if any(row['scope'] == other_scope and row['feed_key'] == self.feed['key']
                   and row['subscribed'] for row in other['podcast_subscriptions']):
                raise AssertionError('Late admin reply associated a feed with the other account')
        self.switch_account('admin')
        self.podcasts()
        if not adrv.wait(feed_label_pattern('Acceptance Alpha')):
            raise AssertionError('Original account subscription was not retained after held reply')
        self.record('held scoped response cannot cross native account logout')

    def end_outage_next(self):
        """A failed completion sync must not prevent another downloaded source."""
        if not self.audio.available():
            raise AssertionError('End-outage check requires real Android audio output')
        first = self.state['feeds'][0]['episodes'][1]
        second = self.state['feeds'][1]['episodes'][1]
        self.episodes(filter_name='All')
        for episode in (first, second):
            self.row('Download', title=episode['title'], required=False)
            self.wait_downloaded_row(episode['title'])
        downloaded = self.snapshot('end-outage-downloads')
        available = {row['episode_key'] for row in downloaded['podcast_downloads']
                     if row['state'] == 'available'}
        if first['key'] not in available or second['key'] not in available:
            raise AssertionError('Both publisher copies must finish before the outage journey')
        self.episodes(filter_name='All')
        with self.proxy(fail_podcasts=True) as proxy:
            first_mark = self.audio.mark()
            self.row('Play from beginning', title=first['title'])
            deadline = time.monotonic() + 30
            playing = False
            terminal_ms = int(self.state['audio']['end']['duration_s'] * 1000)
            last_terminal = None
            terminal_stable = 0
            end_failure_start = None
            while time.monotonic() < deadline:
                state = session_state()
                if state['title'] == first['title'] and state['state'] == 'PLAYING':
                    playing = True
                if playing and end_failure_start is None and (state['position_ms'] or 0) >= terminal_ms - 2000:
                    end_failure_start = len(proxy.trace)
                if playing and (state['position_ms'] or 0) >= terminal_ms - 500:
                    if state['state'] in ('PAUSED', 'STOPPED'):
                        break
                    if state['state'] == 'PLAYING':
                        terminal_stable = terminal_stable + 1 if state['position_ms'] == last_terminal else 1
                        last_terminal = state['position_ms']
                        if terminal_stable >= 4:
                            break
                time.sleep(.25)
            else:
                raise AssertionError('First downloaded episode did not reach its natural end')
            if end_failure_start is None:
                raise AssertionError('First source had no observed near-end playback window')
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline and not any(
                    event.get('transport_failed') and event['path'].endswith('/podcasts/sync')
                    for event in proxy.trace[end_failure_start:]):
                time.sleep(.1)
            if not any(event.get('transport_failed') and event['path'].endswith('/podcasts/sync')
                       for event in proxy.trace[end_failure_start:]):
                raise AssertionError('Natural completion did not attempt sync during the outage')
            require_audio(audio_trace.analyze(self.audio.save(first_mark, self.output / 'failed-end.wav')),
                          self.state['audio']['end']['tone_hz'])
            app_ui.back()
            completed = self.snapshot('completed-during-outage')
            states = [row for row in completed['podcast_episode_state']
                      if row['scope'] == self.scope and row['episode_key'] == first['key']]
            if len(states) != 1 or not states[0]['completed'] or states[0]['completion_source'] != 'auto':
                raise AssertionError('Failed end sync lost the local automatic completion')
            self.episodes(filter_name='All')
            second_mark = self.audio.mark()
            self.row('Play from beginning', title=second['title'])
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                state = session_state()
                if state['state'] == 'PLAYING' and state['title'] == second['title']:
                    break
                time.sleep(.2)
            else:
                raise AssertionError('Next downloaded episode did not play during sync outage')
            time.sleep(3)
            checked_adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
            require_audio(audio_trace.analyze(self.audio.save(second_mark, self.output / 'next-source.wav')),
                          self.state['audio']['end']['tone_hz'])
            self.record('failed native end sync leaves completion durable and next source audible')

    def download_negatives(self):
        """Cancel a held publisher transfer; recover a real HTTP download error."""
        cancelled = self.state['feeds'][1]['episodes'][2]
        failed = self.state['feeds'][0]['episodes'][2]
        server_copies = podcast_media(self.state['database'])
        # An absent copy has no Delete control. Inspect durable rows first so
        # optional cleanup never spends 80 UI dumps looking for that control.
        before = self.snapshot('before-download-negatives')
        available = {row['episode_key'] for row in before['podcast_downloads']
                     if row['state'] == 'available'}
        for feed, episode in ((self.state['feeds'][1], cancelled),
                              (self.state['feeds'][0], failed)):
            if episode['key'] in available:
                self.episodes(combined=False, filter_name='All', feed_title=feed['title'])
                self.row('Delete downloaded file', title=episode['title'])

        self.episodes(combined=False, filter_name='All', feed_title=self.state['feeds'][1]['title'])
        with self.publisher_proxy(hold_tag='beta-003',
                                  evidence_name='publisher-cancel-trace') as proxy:
            self.row(r'(?:Download|Retry download)', title=cancelled['title'])
            if not proxy.held.wait(10):
                raise AssertionError('Native transfer did not reach the actual publisher')
            held = require_publisher_response(proxy.trace, 'beta-003', 200)
            if 'delivered_ms' in held:
                raise AssertionError('Publisher audio was delivered before native cancellation')
            self.row('Cancel download', title=cancelled['title'])
            if held.get('hold_timed_out'):
                raise AssertionError('Publisher hold expired before native Cancel')
            proxy.release.set()
            self.wait_row_action(cancelled['title'], 'Download')
            missing = self.snapshot('cancelled-download')
            copy = require_download_state(missing, cancelled, 'missing')
            if copy['size_bytes'] or copy.get('error'):
                raise AssertionError('Cancelled download retained bytes or an error state')
            for extension in ('.audio', '.part'):
                path = f'/data/user/0/{PKG}/databases/podcast-downloads/{copy["id"]}{extension}'
                if self._device_bytes(path, optional=True):
                    raise AssertionError('Cancelled download left device audio bytes')
        self.record('native cancel discards held publisher transfer and partial copy')

        self.episodes(combined=False, filter_name='All', feed_title=self.state['feeds'][0]['title'])
        with self.publisher_proxy(fail_tag='alpha-003',
                                  evidence_name='publisher-error-trace') as proxy:
            self.row(r'(?:Download|Retry download)', title=failed['title'])
            self.wait_row_action(failed['title'], 'Retry download')
            require_publisher_response(proxy.trace, 'alpha-003', 503)
            failure = self.snapshot('failed-download')
            copy = require_download_state(failure, failed, 'failed')
            if not copy.get('error'):
                raise AssertionError('Failed publisher response did not retain retry guidance')
            for extension in ('.audio', '.part'):
                path = f'/data/user/0/{PKG}/databases/podcast-downloads/{copy["id"]}{extension}'
                if self._device_bytes(path, optional=True):
                    raise AssertionError('Failed publisher response left device audio bytes')
            proxy.fail_tag = None
            self.episodes(combined=False, filter_name='All',
                          feed_title=self.state['feeds'][0]['title'])
            self.row('Retry download', title=failed['title'])
            self.wait_downloaded_row(failed['title'])
            require_publisher_response(proxy.trace, 'alpha-003', 200)
            success = self.snapshot('retried-download')
            copy = require_download_state(success, failed, 'available')
            if copy['actual_sha256'] != self.state['audio']['end']['sha256']:
                raise AssertionError('Retried native copy differs from publisher audio')
        if podcast_media(self.state['database']) != server_copies:
            raise AssertionError('Native negative downloads changed server podcast audio')
        self.record('native publisher error exposes retry and successful direct redownload')

    def offline_seek(self):
        """Seek a downloaded podcast through its touch slider with both hosts offline."""
        if not self.audio.available():
            raise AssertionError('Offline seek needs real Android audio output')
        episode = self.state['feeds'][1]['episodes'][1]
        snapshot = self.snapshot('before-offline-seek')
        require_download_state(snapshot, episode, 'available')
        self.episodes(combined=False, filter_name='All',
                      feed_title=self.state['feeds'][1]['title'])
        mappings = checked_adb('reverse', '--list').splitlines()
        selected = {}
        for port in (self.state['port'], self.state['publisher_port']):
            mapping = f'tcp:{port}'
            original = next((line.split()[-1] for line in mappings
                             if line.split()[-2] == mapping), None)
            if original is None:
                raise AssertionError('Offline seek needs both parent-owned reverse mappings')
            selected[mapping] = original
        try:
            for mapping in selected:
                checked_adb('reverse', '--remove', mapping)
            self.row('Play from beginning', title=episode['title'])
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                state = session_state()
                if state['state'] == 'PLAYING' and state['title'] == episode['title']:
                    break
                time.sleep(.2)
            else:
                raise AssertionError('Downloaded episode did not play with both hosts offline')
            checked_adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
            initial = wait_paused_session(episode['title'])
            nodes, bar = wait_seek_bar()
            start, y, near_start = seek_track_swipe(nodes, bar, fraction=.02)
            checked_adb('shell', 'input', 'swipe', str(start), str(y),
                        str(near_start), str(y), '300')
            reset = wait_paused_session(episode['title'], max_position=1500)
            nodes, bar = wait_seek_bar(max_percent=15)
            start, y, target = seek_track_swipe(nodes, bar)
            quiet_mark = self.audio.mark()
            checked_adb('shell', 'input', 'swipe', str(start), str(y),
                        str(target), str(y), '300')
            after_seek, seek_samples = wait_seek_jump(reset, episode['title'])
            wait_seek_bar(min_percent=50, max_percent=75)
            wait_paused_session(episode['title'])
            quiet_audio = audio_trace.analyze(self.audio.save(
                quiet_mark, self.output / 'offline-seek-paused.wav'))
            require_seek_silence(quiet_audio)
            self.shot('offline-seek-paused')
            mark = self.audio.mark()
            checked_adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PLAY')
            time.sleep(3)
            checked_adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
            require_audio(audio_trace.analyze(self.audio.save(mark, self.output / 'offline-seek.wav')),
                          self.state['audio']['end']['tone_hz'])
            deadline = time.monotonic() + 4
            while time.monotonic() < deadline:
                now = session_state()
                if (now['title'] == episode['title']
                        and (now['position_ms'] or 0) > after_seek['position_ms']):
                    break
                time.sleep(.2)
            else:
                raise AssertionError('Offline seek did not continue downloaded playback')
            app_ui.back()
            persisted = self.snapshot('after-offline-seek')
            require_download_state(persisted, episode, 'available')
            states = [row for row in persisted['podcast_episode_state']
                      if row['scope'] == self.scope and row['episode_key'] == episode['key']]
            if len(states) != 1 or states[0]['position_seconds'] < 5:
                raise AssertionError('Offline seek did not persist listening history')
            self.record('native downloaded podcast seeks and stays audible with both hosts offline',
                        initial_position_ms=initial.get('position_ms'),
                        reset_position_ms=reset['position_ms'],
                        seek_position_ms=after_seek['position_ms'],
                        seek_samples=seek_samples,
                        paused_seek_audio=quiet_audio)
        finally:
            for mapping, original in selected.items():
                checked_adb('reverse', mapping, original)

    def unsubscribe(self):
        before = self.snapshot('before-unsubscribe')
        server_copies = podcast_media(self.state['database'])
        with self.proxy(fail_podcasts=True) as proxy:
            self.podcasts()
            nodes = adrv.nodes()
            names = [node for node in nodes if adrv.label(node).split(' / ')[0] in ('Acceptance Alpha', 'Acceptance Beta')]
            alpha = next(node for node in names if adrv.label(node).split(' / ')[0] == 'Acceptance Alpha')
            end = min((node['b'][1] for node in names if node['b'][1] > alpha['b'][1]), default=10**6)
            menus = [node for node in nodes if adrv.label(node) == 'Manage podcast' and alpha['b'][1] <= node['b'][1] < end]
            if len(menus) != 1:
                raise AssertionError('Could not identify Alpha subscription menu by its actual row')
            adrv.tap_node(menus[0])
            self.touch(r'^Unsubscribe \(keep history\)$')
            pending = self.snapshot('offline-tombstone')
            tombstone = next(row for row in pending['podcast_subscriptions'] if row['scope'] == self.scope and row['feed_key'] == self.feed['key'])
            if tombstone['subscribed'] or not tombstone['pending_edit_id']:
                raise AssertionError('Offline unsubscribe did not persist a durable pending tombstone')
            self.podcasts()  # Cold restart and actual sign-in; sync remains unavailable.
            if adrv.find(feed_label_pattern('Acceptance Alpha')):
                raise AssertionError('Process restart resurrected removed subscription')
            if self.saved(before)['position_seconds'] != self.saved(pending)['position_seconds']:
                raise AssertionError('Unsubscribe changed saved listening position')
            self.record('offline native unsubscribe tombstone survives process restart without history loss')
            # Restore the response while the selected device still talks to
            # this controlled proxy. Swapping the reverse mapping first can
            # trigger an automatic sync and replace Retry before the tap.
            proxy.fail_podcasts = False
            self.touch(r'^(Retry|Sync now)$')
            if not wait_footer_clear(timeout=30):
                raise AssertionError('Reconnected global retry did not deliver native tombstone')
            if not any(item.get('status') == 200 and item['path'].endswith('/podcasts/sync')
                       for item in proxy.trace):
                raise AssertionError('Reconnected retry did not receive a successful podcast sync response')
        with sqlite3.connect(Path(self.state['database']).as_uri() + '?mode=ro', uri=True) as db:
            remote = db.execute('SELECT subscribed FROM podcast_subscriptions WHERE user_id=? AND feed_key=?',
                (self.state['accounts']['admin'], self.feed['key'])).fetchone()
        if remote != (0,):
            raise AssertionError('Reconnected native tombstone did not reach the selected account')
        if podcast_media(self.state['database']) != server_copies:
            raise AssertionError('Subscription removal changed shared server downloads')

    def observe_unsubscribed(self):
        self.podcasts()
        self.touch('^Sync now$')
        if not wait_footer_clear(timeout=30):
            raise AssertionError('Subscription tombstone pull did not fully sync')
        if adrv.find(feed_label_pattern('Acceptance Alpha')):
            raise AssertionError('Stale native client resurrected unsubscribed feed')
        snapshot = self.snapshot('unsubscribed-client')
        if not any(copy['episode_key'] == self.episode['key'] and copy['state'] == 'available'
                   for copy in snapshot['podcast_downloads']):
            raise AssertionError('Subscription synchronization removed another device copy')
        self.saved(snapshot)
        self.record('native tombstone reconciliation retains independent copy and history')

    def resubscribe(self):
        self.podcasts()
        self.touch('^Subscribe to podcast$')
        self.field('Feed URL', self.feed['url'])
        self.touch('^Subscribe$')
        if not adrv.wait(feed_label_pattern('Acceptance Alpha')):
            raise AssertionError('Explicit native resubscribe did not restore subscription')
        self.record('explicit native resubscribe restores feed without redownloading audio')

    @contextmanager
    def stale_clock(self):
        previous_time = checked_adb('shell', 'settings', 'get', 'global', 'auto_time')
        if checked_adb('shell', 'id', '-u') != '0':
            raise AssertionError('Controlled native clock needs the parent-owned rooted emulator')
        try:
            checked_adb('shell', 'settings', 'put', 'global', 'auto_time', '0')
            future = datetime.fromtimestamp(time.time() + 48 * 3600 + 120, timezone.utc)
            checked_adb('shell', 'date', '-u', future.strftime('%m%d%H%M%Y.%S'))
            yield
        finally:
            now = datetime.now(timezone.utc)
            try:
                checked_adb('shell', 'date', '-u', now.strftime('%m%d%H%M%Y.%S'))
            finally:
                checked_adb('shell', 'settings', 'put', 'global', 'auto_time', previous_time)

    def feedback(self):
        self.episodes(filter_name='All')
        self.touch('^Sync now$')
        if not wait_footer_clear():
            raise AssertionError('Clock check requires actual prior full success')
        mapping = f'tcp:{self.state["port"]}'
        original = next((line.split()[-1] for line in checked_adb('reverse', '--list').splitlines()
                         if line.split()[-2] == mapping), None)
        if original is None:
            raise AssertionError('Existing selected-device server mapping required')
        try:
            checked_adb('reverse', '--remove', mapping)
            with self.stale_clock():
                checked_adb('shell', 'input', 'keyevent', 'KEYCODE_HOME')
                app_ui.launch()
                if not adrv.wait('Last synced 2 days ago', timeout=25):
                    raise AssertionError('Actual Android footer did not show controlled stale age')
                if adrv.find('(?i)alert|server is down'):
                    raise AssertionError('Sync failure used alarming/inaccurate wording')
                self.shot('controlled-48-hours-quiet')
                self.record('actual native controlled-clock stale hint during real unavailable sync')
        finally:
            checked_adb('reverse', mapping, original)
        self.touch('^Retry$')
        if not wait_footer_clear():
            raise AssertionError('Actual full success did not clear stale/unavailable footer')
        self.record('native full retry clears stale feedback after restoring real clock')

    def auth_boundary(self):
        self.episodes(filter_name='All')
        if not self.audio.available():
            raise AssertionError('Quiet auth boundary requires actual continuing audio output')
        with self.proxy(strip_auth=True) as proxy:
            self.playback('native playback continues through genuine server 401', action='Play from beginning')
            if not adrv.wait('Sign in again to sync'):
                raise AssertionError('Actual native authentication hint was missing')
            if not any(item.get('status') == 401 for item in proxy.trace):
                raise AssertionError('Authentication boundary needs a real upstream 401')
            if adrv.find('^Welcome back$') or adrv.find('server is down'):
                raise AssertionError('Quiet sync auth failure navigated or claimed an outage')
            self.shot('quiet-auth-native')
        self.switch_account('admin')
        self.record('genuine sync authentication failure stays quiet until explicit sign-in')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture', type=Path)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--client', choices=['A', 'B'], required=True)
    parser.add_argument('--stage', choices=['standalone', 'associate', 'resume', 'finish',
        'observe-completed', 'uncomplete', 'observe-uncompleted', 'cleanup',
        'preplay', 'browse', 'feedback', 'globalprogress', 'isolation',
        'held-account', 'end-outage-next', 'unsubscribe', 'observe-unsubscribed',
        'resubscribe', 'auth', 'download-negatives', 'offline-seek'], required=True)
    parser.add_argument('--emulator-wav', type=Path)
    args = parser.parse_args(argv)
    if args.stage in ('standalone', 'associate') and args.client != 'A':
        parser.error('Standalone and association stages belong to persistent client A')
    if args.stage in ('resume', 'finish') and args.client != 'B':
        parser.error('Resume and finish stages belong to independent client B')
    run = Run(args)
    try:
        if args.stage == 'auth':
            run.auth_boundary()
        elif args.stage == 'held-account':
            run.held_account()
        elif args.stage == 'end-outage-next':
            run.end_outage_next()
        elif args.stage == 'download-negatives':
            run.download_negatives()
        elif args.stage == 'offline-seek':
            run.offline_seek()
        elif args.stage == 'observe-unsubscribed':
            run.observe_unsubscribed()
        elif args.stage.startswith('observe-'):
            run.observe(args.stage == 'observe-completed')
        else:
            getattr(run, args.stage)()
    except Exception as error:
        run.results.append({'name': args.stage, 'result': 'fail', 'detail': str(error)})
        raise
    finally:
        (run.output / 'results.json').write_text(json.dumps(run.results, indent=1))


if __name__ == '__main__':
    main()
