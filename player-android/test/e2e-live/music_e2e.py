#!/usr/bin/env python3
"""V01 music baseline of the Android app on an emulator against the music fixture.

Drives the installed app through adb/uiautomator (adrv.py, app_ui.py) against
a disposable server from
player-android/integration_test/support/music_fixture.py and writes
structured evidence (evidence.py schema) with screenshots and audio captures
to <evidence>/android/. It proves the harness on today's app; it does not
verify any future music feature.

Journey: connect (adb reverse or the 10.0.2.2 host alias) and sign in with a
wrong and then the right password; the admin library lists both sets; a
touch journey opens Music > Browse folders > album > track > Play Audio and
checks the media session and the captured output (seconds of the track's
own tone show that playback advances); tap
target sizes; Back returns through the hierarchy without leaving the app;
progress is saved on the server; light and dark screenshots; two
ReplayGain fixture tracks are captured at their real 18 dB level difference;
the capture is calibrated against tracks with known 5/20/50 ms gaps and the
EQ probe's bands; gapless across tracks is recorded as a gap (no automatic
track change on Android yet); a server restart keeps sessions and the
running app keeps working; a cold relaunch remembers the server but locks
the saved login until explicit password sign-in; the regular
account sees only its granted set. At the end the app data is cleared and
the adb reverse mapping removed.

Audio output capture needs the emulator started with QEMU's wav audio
backend (QEMU_AUDIO_DRV=wav QEMU_WAV_PATH=<file>), see
../../docs/music-e2e-verification.md; pass the
file with --emulator-wav. Without it the audio checks are recorded as gaps.

Usage: music_e2e.py <fixture-dir> [--emulator-wav FILE] [--apk FILE] [--host-alias]
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import wave

import adrv
import app_ui
from app_ui import PKG, back, labels

SUPPORT = Path(__file__).resolve().parents[2] / 'integration_test' / 'support'
sys.path.insert(0, str(SUPPORT))
import audio_trace  # noqa: E402
from evidence import Evidence  # noqa: E402
from fixture_server import Client  # noqa: E402

FEATURE = 'V01 baseline'
ALBUM = 'Aurora Lane - Night Transit (2021)'
# Material/Android guidance: touch targets of at least 48 x 48 dp.
MIN_TARGET_DP = 48
TONE_TOLERANCE = 0.02
LEVEL_TOLERANCE_DB = 1.5
# The emulator's output passes through Android's mixer and QEMU's 44.1 kHz
# resampling; a known gap must still be measured within this many ms.
ANDROID_GAP_TOLERANCE_MS = 3
# Silences this close to the start or end of the audible stream are the
# emulator stream starting or stopping, not part of the media.
EDGE_S = 0.05
# Audible seconds a capture must contain to show playback advancing.
MIN_AUDIBLE_S = 2.0


class EmulatorAudio:
    """The emulator's audio output as written by QEMU's wav backend.

    QEMU appends 44.1 kHz 16-bit stereo PCM while the guest's output stream
    is open and writes the WAV header only on exit, so captures are byte
    ranges of the growing file. The file stops growing while Android has no
    output stream open; time in a capture is therefore stream time, not wall
    time: silence *inside* an open stream is captured, a closed stream is not.
    """

    HEADER, RATE, CHANNELS, WIDTH = 44, 44100, 2, 2

    def __init__(self, path):
        self.path = Path(path) if path else None

    def available(self):
        return self.path is not None and self.path.exists()

    def mark(self):
        return self.path.stat().st_size

    def save(self, mark, out):
        """Write everything appended since mark as a WAV file; return its path."""
        with self.path.open('rb') as file:
            file.seek(max(mark, self.HEADER))
            data = file.read()
        frame = self.CHANNELS * self.WIDTH
        with wave.open(str(out), 'wb') as target:
            target.setnchannels(self.CHANNELS)
            target.setsampwidth(self.WIDTH)
            target.setframerate(self.RATE)
            target.writeframes(data[:len(data) // frame * frame])
        return out


class Run:
    """Fixture, accounts, evidence and helpers shared by the journey steps."""

    def __init__(self, args, *, evidence_dir=None):
        self.fixture_dir = Path(args.fixture_dir)
        self.state = json.loads((self.fixture_dir / 'state.json').read_text())
        self.auth = json.loads((self.fixture_dir / 'auth.json').read_text())
        self.manifest = json.loads(Path(self.state['manifest_file']).read_text())
        android = self.state['android']
        self.base = android['emulator_host_origin' if args.host_alias else 'adb_reverse_origin']
        self.audio = EmulatorAudio(args.emulator_wav)
        versions = device_versions(args, self.base)
        versions.update(server_version=self.state['server']['version'], run_id=self.state['run_id'],
                        server_build_revision=self.state['server'].get('build_revision'))
        self.evidence = Evidence(evidence_dir or self.state['evidence_dir'], 'android', versions)
        self.admin = Client(self.state['origin']).login(**self.auth['admin'])

    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M01' if scenario.startswith('M01:') else FEATURE, scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario}' + (f'  [{detail}]' if detail else ''), flush=True)

    def shot(self, name):
        path = self.evidence.path(f'{name}.png')
        adrv.shot(str(path))
        return path

    def track(self, path):
        entry = next(t for t in self.manifest['tracks'] if t['path'] == path)
        return dict(entry, id=self.state['media_ids'][path])


def device_versions(args, base):
    getprop = lambda key: adrv.adb('shell', 'getprop', key).strip()
    package = adrv.adb('shell', 'dumpsys', 'package', PKG)
    version = re.search(r'versionName=(\S+)', package)
    code = re.search(r'versionCode=(\d+)', package)
    return {
        'avd': adrv.adb('emu', 'avd', 'name').splitlines()[0].strip() if adrv.adb('emu', 'avd', 'name') else '',
        'api': getprop('ro.build.version.sdk'), 'abi': getprop('ro.product.cpu.abi'),
        'build_type': getprop('ro.build.type'), 'fingerprint': getprop('ro.build.fingerprint'),
        'density': adrv.adb('shell', 'wm', 'density').split(':')[-1].strip(),
        'app_version': version.group(1) if version else '', 'app_version_code': code.group(1) if code else '',
        'apk_sha256': hashlib.sha256(Path(args.apk).read_bytes()).hexdigest() if args.apk else None,
        'server_url_in_app': base, 'emulator_wav': args.emulator_wav,
    }


def session_state():
    """State, position and title of the app's media session (dumpsys)."""
    block = adrv.adb('shell', 'dumpsys', 'media_session').split(f'package={PKG}', 1)[-1]
    state = re.search(r'state=PlaybackState \{state=(\w+)\(\d+\), position=(\d+)', block)
    title = re.search(r'description=([^,\n]*)', block)
    return {'state': state.group(1) if state else None,
            'position_ms': int(state.group(2)) if state else None,
            'title': title.group(1).strip() if title else None}


def reset_progress(client, media_id):
    """Start from 0:00: a short fixture track resumed near its end would finish at once."""
    client.request('POST', '/api/v1/progress/status', {'media_id': media_id, 'status': 'not_started'})


def find_visible(pattern):
    """Scroll to a labeled control/detail; text rows need no card-size threshold."""
    # Music details now include mix/recommendation sections before Play Audio.
    # The primary action can be several screens below the original metadata.
    for move in [None] + [True] * 10 + [False] * 10:
        if move is not None:
            app_ui.swipe(up=move)
        node = adrv.find(pattern)
        if node and node['b'][3] > node['b'][1]:
            return node
    return None


def open_track(set_name, folder, file_name):
    """Library > set > Browse folders > folder > track detail, by touch."""
    if not app_ui.go_home():
        return False
    adrv.tap(f'^{re.escape(set_name)}$'); time.sleep(3)
    if adrv.find('^Browse folders$'):
        adrv.tap('^Browse folders$'); time.sleep(2)
    for part in folder.split('/') if folder else []:
        node = app_ui.find_scrolling('^' + re.escape(part) + '$')
        if not node:
            # Browse intentionally lists single-file folders inline. Only
            # skip the missing path segment when this source is visible.
            if find_visible('^' + re.escape(file_name) + r'(?: /|$)'):
                break
            return False
        adrv.tap_node(node); time.sleep(2)
    node = app_ui.find_scrolling('^' + re.escape(file_name))
    if not node:
        return False
    adrv.tap_node(node); time.sleep(2)
    return find_visible('^Play Audio$') is not None


def play_and_capture(run, client, track, name, seconds=4.0, bands=None, tone=None):
    """Tap Play Audio from 0:00; return (sessions, analysis or None, wav path).

    The media session is read (dumpsys, fast) until it reports PLAYING and
    again after seconds of playback following PLAYING. The elapsed-time label
    is not used: on the animated player screen a uiautomator dump waits for
    an idle UI and can take longer than the 12 s fixture track.
    """
    reset_progress(client, track['id'])
    mark = run.audio.mark() if run.audio.available() else None
    adrv.tap('^Play Audio$')
    # Buffering shows as CONNECTING/BUFFERING first; record the first PLAYING.
    deadline = time.monotonic() + 20
    sessions = [session_state()]
    while sessions[-1]['state'] != 'PLAYING' and time.monotonic() < deadline:
        time.sleep(0.5)
        sessions.append(session_state())
    # Startup on a cold emulator can consume the whole capture window.
    # Keep the mark before Play (calibration must include the track's gap),
    # but give actual playback its full requested duration after PLAYING.
    if sessions[-1]['state'] == 'PLAYING':
        time.sleep(seconds)
    sessions.append(session_state())
    if mark is None:
        return sessions, None, None
    wav = run.audio.save(mark, run.evidence.path(f'{name}.wav'))
    adrv.tap('^Pause$', timeout=3)  # stop before the next capture starts
    return sessions, audio_trace.analyze(wav, bands, tone=tone), wav


def main_segment(analysis):
    segments = sorted(analysis['segments'], key=lambda s: s['duration_s'], reverse=True)
    return segments[0] if segments else None


def tone_matches(segment, track):
    expected = track['tone_hz'][0]
    return bool(segment and segment['tone_hz'] and abs(segment['tone_hz'] - expected) <= expected * TONE_TOLERANCE)


def record_audio(run, scenario, track, analysis, wav):
    """Record a tone check, or a gap when no emulator output is captured."""
    if analysis is None:
        return run.record(scenario, False, 'emulator started without the QEMU wav backend (--emulator-wav)',
                          result='gap')
    segment = main_segment(analysis)
    ok = tone_matches(segment, track) and analysis['audible_s'] >= MIN_AUDIBLE_S
    run.record(scenario, ok,
               f"expected {track['tone_hz'][0]} Hz, heard {segment and segment['tone_hz']} Hz "
               f"for {analysis['audible_s']} s", {'segments': analysis['segments']}, [wav])


def step_sign_in(run):
    """Wrong password first (visible error), then the admin signs in."""
    checks = []
    ok = app_ui.connect_and_login(run.base, run.auth['admin']['username'], run.auth['admin']['password'],
                                  lambda name, passed, detail='': checks.append((name, passed, detail)),
                                  run.shot, check_wrong_password=True)
    for name, passed, detail in checks:
        run.record(f'sign-in: {name}', passed, detail,
                   artifacts=[run.evidence.path('login-wrong-password.png')] if 'wrong' in name else ())
    run.record('sign-in: admin reaches the Library', ok)
    found = labels()
    run.record('library: admin sees both fixture sets', all(s in found for s in ('Music', 'Restricted')),
               ', '.join(l for l in found if l in ('Music', 'Restricted')), artifacts=[run.shot('library-admin')])
    return ok


def step_touch_journey(run):
    """Open the album by touch, play track 2, prove playback by media session and captured tone."""
    track = run.track(f'Music/{ALBUM}/02 - Long Road Home.mp3')
    opened = open_track('Music', ALBUM, track['title'])
    run.record('touch journey: Music > Browse folders > album > track detail', opened,
               artifacts=[run.shot('track-detail')])
    if not opened:
        return False
    # Seven seconds after PLAYING leaves room for the emulator output stream
    # to start and still capture MIN_AUDIBLE_S of this 12 s track.
    sessions, analysis, wav = play_and_capture(run, run.admin, track, 'touch-long-road-home', seconds=7)
    # The last reading (end of the capture) must still be this track, playing.
    playing = sessions[-1]['state'] == 'PLAYING' and sessions[-1]['title'] == track['title']
    run.record('touch journey: Play Audio starts the media session with this track', playing,
               f'sessions={sessions}', {'sessions': sessions}, [run.shot('player-light')])
    # Advancing playback is proven by the captured output: seconds of the
    # track's own tone (the session position alone is only a snapshot).
    record_audio(run, 'audio output: emulator output carries the track tone', track, analysis, wav)
    return playing


def step_metadata(run):
    """M01: actual folder/detail semantics, complete tags, playback and live rescans."""
    from music_metadata import retag, TRACK
    entries = [
        (TRACK, {'Filename': 'source_001.flac', 'Artist': 'A & B; C/D',
                 'Album artist': 'Album Ensemble', 'Album': 'Metadata Album',
                 'Track': '4 / 9', 'Disc': '2 / 3', 'Release date': '2024-02-29', 'Genres': 'Jazz'}),
        ('Music/Ünïcødé Artïst - 東京 Nights/02 - Партия для двоих.flac',
         {'Artist': 'Ünïcødé Artïst, Mika Sørensen', 'Genres': 'Électronique, Ambient'}),
        ('Music/Various Artists - Synth Compilation/Disc 2/01 - Afterglow.ogg',
         {'Artist': 'Dusk Choir', 'Disc': '2 / 2'}),
        ('Music/Various Artists - Synth Compilation/Disc 1/01 - Neon Rain.m4a',
         {'Artist': 'Neon Coast', 'Disc': '1 / 2'}),
        ('Music/Various Artists - Synth Compilation/Disc 1/02 - Glass City.opus',
         {'Artist': 'Glass Avenue', 'Disc': '1 / 2'}),
        ('Music/Untagged/track_0042.mp3', {'Filename': 'track_0042.mp3'}),
        ('Music/Tag Verification/blank_title.mp3', {'Filename': 'blank_title.mp3', 'Album': 'Partial Metadata'}),
    ]
    for path, expected in entries:
        track = run.track(path)
        folder = str(Path(path).parent.relative_to('Music'))
        ok = open_track('Music', folder, track['title'])
        title_visible = any(track['title'] in label for label in labels())
        expansion = find_visible(r'^Music details(?:,|$)') if ok else None
        if expansion:
            adrv.tap_node(expansion); time.sleep(1)
        found = {'expanded': adrv.find(r'^Music details, Expanded$') is not None} if expansion else {'expanded': False}
        for key, value in expected.items():
            label = f'{key}: {value}'
            found[key] = find_visible('^' + re.escape(label) + '$') is not None
        if path.endswith('blank_title.mp3') or '/Untagged/' in path:
            found['missing fields omitted'] = find_visible(r'^(Artist|Album artist|Track|Disc|Year):') is None
        run.record(f'M01: title and embedded details {track["file_name"]}',
                   ok and title_visible and bool(expansion) and all(found.values()),
                   f'title={track["title"]}; fields={found}', artifacts=[run.shot('m01-' + str(track['id']))])
        back()
    item = run.track(TRACK)
    # Persist a known value before the rescan; field patch must retain it.
    run.admin.request('POST', '/api/progress', {'media_id': item['id'], 'position_seconds': 4})
    before = run.admin.request('GET', f'/api/media/{item["id"]}')
    if not before.get('progress') or before['progress']['position_seconds'] != 4:
        raise RuntimeError('M01 pre-rescan progress was not saved as 4 seconds')
    initial_rows = run.admin.request('GET', f'/api/media?set_id={run.state["set_ids"]["Music"]}')
    try:
        for state, title in [('changed', 'Changed 東京 Title'), ('cleared', 'source_001')]:
            retag(run.fixture_dir, state)
            run.admin.request('POST', '/api/admin/rescan', {})
            deadline = time.monotonic() + 30
            after = None
            while time.monotonic() < deadline:
                after = run.admin.request('GET', f'/api/media/{item["id"]}')
                actual = after['media'].get('music', {}).get('title', '')
                if actual == (title if state == 'changed' else ''):
                    break
                time.sleep(0.25)
            visible = open_track('Music', 'Tag Verification', title)
            visible = visible and any(title in label for label in labels())
            saved = after['media']['id'] == before['media']['id'] and after['progress'] == before['progress']
            saved = saved and all(after['media'][key] == before['media'][key] for key in ('file_name', 'rel_path', 'play_count'))
            rows = run.admin.request('GET', f'/api/media?set_id={run.state["set_ids"]["Music"]}')
            copies = [row for row in rows if row['rel_path'] == before['media']['rel_path']]
            saved = saved and len(rows) == len(initial_rows) and len(copies) == 1 and copies[0]['id'] == item['id']
            run.record(f'M01: {state} tags visible after rescan with saved progress', visible and saved,
                       data={'before': before, 'after': after}, artifacts=[run.shot('m01-rescan-' + state)])
            back()
    finally:
        retag(run.fixture_dir, 'initial')
        run.admin.request('POST', '/api/admin/rescan', {})
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            restored = run.admin.request('GET', f'/api/media/{item["id"]}')
            if restored['media'].get('music', {}).get('title') == item['title']:
                break
            time.sleep(0.25)
        else:
            raise RuntimeError('Restored music title did not become authoritative after rescan')
    for mode in ('no', 'yes'):
        adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
        track = run.track('Music/Tag Verification/long_name.flac')
        ok = open_track('Music', 'Tag Verification', track['title'])
        run.record(f'M01: long Unicode detail {"dark" if mode == "yes" else "light"}', ok,
                   artifacts=[run.shot('m01-long-' + mode)])
        back()
    # Play the differently named source after the tag removal/restore boundary.
    step_metadata_audio(run, item)


def step_metadata_audio(run, item):
    """Mandatory native title/session and audible-source oracle; never silently skip."""
    session_name = 'M01: playback media session uses music title'
    tone_name = 'M01: original media source produces the expected tone'
    if not open_track('Music', 'Tag Verification', item['title']):
        run.record(session_name, False, 'Restored titled source could not be opened')
        run.record(tone_name, False, 'Playback did not start because source navigation failed')
        return False
    sessions, analysis, wav = play_and_capture(run, run.admin, item, 'm01-title-source', seconds=7)
    playing = sessions[-1]['state'] == 'PLAYING' and sessions[-1]['title'] == item['title']
    run.record(session_name, playing, data={'sessions': sessions})
    record_audio(run, tone_name, item, analysis, wav)
    return playing


def step_tap_targets(run):
    """Every clickable control on the player screen is at least 48 x 48 dp."""
    density = int(adrv.adb('shell', 'wm', 'density').split(':')[-1].strip() or 160)
    sizes = {}
    for node in adrv.nodes():
        if node['click'] and adrv.label(node):
            width, height = node['b'][2] - node['b'][0], node['b'][3] - node['b'][1]
            sizes[adrv.label(node)[:40]] = [round(width * 160 / density), round(height * 160 / density)]
    small = {name: size for name, size in sizes.items() if min(size) < MIN_TARGET_DP}
    run.record(f'tap targets: player controls are at least {MIN_TARGET_DP} dp', bool(sizes) and not small,
               f'below {MIN_TARGET_DP} dp: {small}' if small else f'{len(sizes)} controls checked',
               {'sizes_dp': sizes, 'density': density}, [run.shot('player-tap-targets')])


def step_back_and_progress(run):
    """System Back walks up player > folder > set > Library and stays in the app."""
    track = run.track(f'Music/{ALBUM}/02 - Long Road Home.mp3')
    trail = []
    # What identifies each level, measured on 0.8.2: the track detail (Play
    # Audio), the album's track list, the set's folder list (another album
    # folder), the set's flat grid (Browse folders toggle), the Library.
    for expected in (r'^Play Audio$', '^' + re.escape(track['title']) + r' / ', r'^Gain Check$',
                     r'^Browse folders$', r'^Library$'):
        back(); time.sleep(1)
        seen = any(re.search(expected, label) for label in labels())
        trail.append({'expected': expected, 'seen': seen, 'foreground': app_ui.foreground()})
    run.record('back: player > detail > album > folders > set grid > Library, app stays in the foreground',
               all(t['seen'] and t['foreground'] for t in trail), json.dumps(trail), {'trail': trail},
               [run.shot('back-to-library')])
    deadline = time.monotonic() + 15
    while True:
        progress = (run.admin.request('GET', f"/api/v1/media/{track['id']}") or {}).get('progress') or {}
        position = progress.get('position_seconds') or 0
        if position > 1 or time.monotonic() >= deadline:
            break
        time.sleep(1)
    run.record('saved state: playback position stored on the server after leaving the player', position > 1,
               f'position_seconds={position}', {'progress': progress})


def step_themes(run):
    """Light and dark screenshots of the Library (the app follows the system theme)."""
    shots = []
    for mode in ('yes', 'no'):
        adrv.adb('shell', 'cmd', 'uimode', 'night', mode); time.sleep(3)
        shots.append(run.shot('library-dark' if mode == 'yes' else 'library-light'))
    run.record('themes: Library captured in dark and light system theme', True, artifacts=shots)


def step_gain_levels(run):
    """The two ReplayGain fixture tracks are captured 18 dB apart (no gain applied today)."""
    if not run.audio.available():
        return run.record('audio output: Loud/Quiet level difference', False,
                          'emulator started without the QEMU wav backend', result='gap')
    levels, wavs = {}, []
    for name in ('Loud.flac', 'Quiet.flac'):
        track = run.track(f'Music/Gain Check/{name}')
        if not open_track('Music', 'Gain Check', track['title']):
            return run.record('audio output: Loud/Quiet level difference', False, f'could not open {name}')
        _, analysis, wav = play_and_capture(run, run.admin, track, f'gain-{name}', seconds=3.5)
        segment = main_segment(analysis)
        levels[name] = segment and segment['rms_dbfs']
        wavs.append(wav)
    loud, quiet = run.track('Music/Gain Check/Loud.flac'), run.track('Music/Gain Check/Quiet.flac')
    expected = loud['level_dbfs'] - quiet['level_dbfs']
    measured = (levels['Loud.flac'] or 0) - (levels['Quiet.flac'] or 0)
    run.record('audio output: Loud/Quiet captured at their real level difference',
               all(levels.values()) and abs(measured - expected) < LEVEL_TOLERANCE_DB,
               f'measured {measured:.2f} dB, expected {expected} dB', {'rms_dbfs': levels}, wavs)


def step_server_restart(run):
    """Restart the fixture server on the same database while the app runs.

    Server persistence is proved by the API (a session opened before the
    restart still works); the running app (not force-stopped) must keep
    working against the restarted server without a new sign-in.
    """
    app_ui.go_home()
    script = SUPPORT / 'music_fixture.py'
    result = subprocess.run([sys.executable, str(script), 'restart', str(run.fixture_dir)],
                            capture_output=True, text=True)
    run.record('server restart: fixture restarts on the same database', result.returncode == 0,
               (result.stdout or result.stderr).strip()[:200])
    status, _ = run.admin.call('GET', '/api/v1/sets')
    run.record('server restart: a session opened before the restart is still valid', status == 200,
               f'HTTP {status}')
    adrv.tap('^Music$'); time.sleep(3)
    grid = adrv.wait('^Browse folders$', timeout=20) is not None
    run.record('server restart: the running app keeps working without a new sign-in',
               grid and not adrv.find('^Welcome back$'), artifacts=[run.shot('after-server-restart')])


def step_cold_relaunch(run):
    """A new app process remembers the server and keeps its credential locked.

    README requires server selection and fingerprint unlock or password
    sign-in on every app start, including after a cold device boot. This
    runner exercises the password path, without native network interception.
    """
    adrv.adb('shell', 'am', 'force-stop', PKG)
    app_ui.launch()
    chooser = adrv.wait('^Choose a server$', timeout=40) is not None
    saved = adrv.find(re.escape(run.base) + '$') if chooser else None
    remembered = chooser and saved is not None and not adrv.find('^Library$')
    run.record('cold relaunch: saved server is remembered and selection is required', remembered,
               data={'server_origin': run.base}, artifacts=[run.shot('cold-relaunch-server-chooser')])
    if not remembered:
        return
    adrv.tap_node(saved)
    login = adrv.wait('^Welcome back$', timeout=20) is not None
    time.sleep(2)  # no approval: the saved credential must remain locked
    fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
    username = run.auth['admin']['username']
    locked = (login and adrv.find('^Unlock saved login with fingerprint$') is not None
              and any(username in (n['text'], n['desc']) for n in fields)
              and not adrv.find('^Library$'))
    run.record('cold relaunch: saved login remains locked with remembered username', locked,
               data={'remembered_username': username, 'unlock_path': 'password'},
               artifacts=[run.shot('cold-relaunch-locked-login')])
    if not locked:
        return
    adrv.tap_node(fields[-1]); time.sleep(1)
    signed_in = app_ui.sign_in(run.auth['admin']['password'])
    sets = [label for label in labels() if label in ('Music', 'Restricted')]
    run.record('cold relaunch: explicit password sign-in opens the server library',
               signed_in and set(sets) == {'Music', 'Restricted'}, ', '.join(sets),
               artifacts=[run.shot('cold-relaunch-signed-in')])


def step_capture_calibration(run):
    """Calibrate the emulator capture: known gaps and the EQ probe's bands."""
    if not run.audio.available():
        return run.record('audio output: capture calibration', False,
                          'emulator started without the QEMU wav backend', result='gap')
    clicks = 0
    for track in [t for t in run.manifest['tracks'] if t.get('gap')]:
        clicks += calibrate_gap(run, run.track(track['path']))
    # The calibration tracks are one continuous tone apart from their gap;
    # any further discontinuity comes from the emulator's audio path.
    run.record('audio output: click detection on the emulator capture', clicks == 0,
               f'{clicks} tone discontinuities besides the known gaps: the emulator output is not click-free, '
               'so clicks cannot be judged on Android' if clicks else 'emulator output is click-free',
               {'discontinuities': clicks}, result=None if clicks == 0 else 'gap')
    probe = run.track('Music/EQ Probe/Three Tones.flac')
    # Single-file folders are listed inline in the set's folder view.
    if not open_track('Music', None, probe['title']):
        return run.record('audio output: EQ probe bands measured', False, 'could not open the EQ probe')
    _, analysis, wav = play_and_capture(run, run.admin, probe, 'eq-probe', seconds=6, bands=probe['tone_hz'])
    bands = (main_segment(analysis) or {}).get('bands_dbfs', {})
    spread = max(bands.values()) - min(bands.values()) if len(bands) == 3 else None
    run.record('audio output: EQ probe bands 100 Hz / 1 kHz / 8 kHz measured (flat today)',
               spread is not None and spread < LEVEL_TOLERANCE_DB, f'spread {spread} dB',
               {'bands_dbfs': bands}, [wav])


def calibrate_gap(run, track):
    """One known gap must appear once, with its length.

    The emulator's output stream starts and stops with a few ms of silence
    (seen: 3.6-5.8 ms), so silences within EDGE_S of the audible span's ends
    are reported but not counted. Its output also carries clicks inside a
    continuous tone, so discontinuities are recorded as data only; click
    detection is not possible on this capture path (see step_capture_calibration).
    """
    gap = track['gap']
    scenario = f"audio output: known {gap['ms']} ms gap measured"
    if not open_track('Music', 'Gap Calibration', track['title']):
        run.record(scenario, False, 'could not open the track')
        return 0
    _, analysis, wav = play_and_capture(run, run.admin, track, f"gap-calibration-{gap['ms']}ms",
                                        seconds=track['duration_s'] + 3, tone=track['tone_hz'][0])
    segments = analysis['segments']
    span = (segments[0]['start_s'] + EDGE_S, segments[-1]['end_s'] - EDGE_S) if segments else (0, 0)
    silences = [g for g in analysis['gaps'] if g['kind'] == 'silence']
    interior = [g for g in silences if span[0] < g['start_s'] < span[1]]
    measured = interior[0]['duration_ms'] if len(interior) == 1 else None
    ok = measured is not None and abs(measured - gap['ms']) <= ANDROID_GAP_TOLERANCE_MS
    # Discontinuities at a silence's edges are the gap itself, not clicks.
    clicks = [d for d in analysis.get('discontinuities') or [] if not any(
        g['start_s'] - 0.005 <= d['time_s'] <= g['start_s'] + g['duration_ms'] / 1000 + 0.005 for g in silences)]
    run.record(scenario, ok, f"expected {gap['ms']} ms, measured {[g['duration_ms'] for g in interior]} ms "
               f"(stream-edge silences ignored: {[g['duration_ms'] for g in silences if g not in interior]})",
               {'gaps': analysis['gaps'], 'counted': interior, 'discontinuities': clicks}, [wav])
    return len(clicks)


def step_gapless_auto_advance(run):
    """Gapless across tracks needs automatic track changes, which Android lacks."""
    run.record('capture timing: gap at automatic track change (gapless)', False,
               'Android plays one item and stops; no automatic track change to measure yet (design B7, M12/M21)',
               result='gap')


def step_regular_user(run):
    """The viewer account sees only Music; the server refuses Restricted media."""
    user = run.auth['user']
    ok = app_ui.connect_and_login(run.base, user['username'], user['password'],
                                  lambda *_: None, run.shot)
    found = [l for l in labels() if l in ('Music', 'Restricted')]
    run.record('permissions: regular user signs in and sees only the granted set',
               ok and found == run.state['accounts']['user_sets'], ', '.join(found),
               artifacts=[run.shot('library-regular-user')])
    hidden = run.state['media_ids']['Restricted/Admin Only - Hidden Album/01 - Hidden Track.mp3']
    status, _ = Client(run.state['origin']).login(**user).call('GET', f'/api/v1/media/{hidden}')
    run.record('permissions: server refuses the restricted media to the regular user', status in (403, 404),
               f'HTTP {status}')


def teardown(run, port):
    """Leave no session, data or port mapping behind on the emulator.

    run is None when setup failed before the evidence log existed.
    """
    adrv.adb('shell', 'am', 'force-stop', PKG)
    adrv.adb('shell', 'pm', 'clear', PKG)
    adrv.adb('shell', 'cmd', 'uimode', 'night', 'no')
    if port:
        adrv.adb('reverse', '--remove', f'tcp:{port}')
    remaining = adrv.adb('reverse', '--list')
    if run is None:
        return
    run.record('teardown: app data cleared and adb reverse mapping removed', f'tcp:{port}' not in remaining,
               remaining.strip())


def prepare_device(args, port):
    """Optionally install the APK; map the server port unless the host alias is used."""
    # adb reverse works even in airplane mode, whereas connectivity_plus
    # correctly keeps progress queued offline. This is an online baseline.
    connectivity = adrv.adb('shell', 'dumpsys', 'connectivity')
    if not has_online_network(connectivity):
        sys.exit('Online emulator required: disable airplane mode, enable Wi-Fi, '
                 'and wait for a connected NetworkAgentInfo before running the baseline')
    if args.apk:
        output = adrv.adb('install', '-r', str(args.apk))
        if 'Success' not in output:
            sys.exit(f'APK install failed: {output.strip()}')
    if not args.host_alias:
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')


def has_online_network(connectivity):
    """An active Android network, not merely a working adb reverse tunnel."""
    return re.search(r'^\s*NetworkAgentInfo\{[^\n]*\bCONNECTED\b[^\n]*\bINTERNET\b',
                     connectivity, re.MULTILINE) is not None


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__.split('\n\n')[0])
    parser.add_argument('fixture_dir', help='directory printed by music_fixture.py up')
    parser.add_argument('--emulator-wav', help='QEMU_WAV_PATH the emulator was started with')
    parser.add_argument('--metadata-only', action='store_true', help='M01 tags/rescan/title/audio feature journeys only')
    parser.add_argument('--apk', help='install this APK first (adb install -r)')
    parser.add_argument('--host-alias', action='store_true',
                        help='reach the server as http://10.0.2.2:<port> instead of adb reverse')
    return parser.parse_args(argv)


def main(argv=None):
    args = parse_args(argv)
    # Run the teardown also when stopped with SIGTERM (e.g. by `timeout`).
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('music_e2e: terminated'))
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    run = None
    try:
        prepare_device(args, port)
        run = Run(args)
        if step_sign_in(run):
            for step in ((step_metadata,) if args.metadata_only else (step_metadata, step_touch_journey, step_tap_targets, step_back_and_progress, step_themes,
                         step_gain_levels, step_capture_calibration, step_gapless_auto_advance,
                         step_server_restart, step_cold_relaunch, step_regular_user)):
                try:
                    step(run)
                except Exception as error:  # keep going: one broken step must not hide the others
                    run.record(f'step crashed: {step.__name__}', False, repr(error)[:300])
    finally:
        teardown(run, None if args.host_alias else port)
    print(f"Evidence: {run.evidence.directory}")


if __name__ == '__main__':
    main()
