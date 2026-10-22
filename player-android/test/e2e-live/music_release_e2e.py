#!/usr/bin/env python3
"""M05 real native release details, rescan and saved offline snapshot journeys."""
import argparse
import json
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import traceback

import adrv
import app_ui
import music_e2e as baseline
from fixture_server import Client
from music_release import TRACK, EXPECTED, retag


class Run(baseline.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M05', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} {detail}', flush=True)
        if result == 'fail':
            self.failures += 1


def visible(pattern):
    for move in [None] + [True] * 10 + [False] * 10:
        if move is not None:
            app_ui.swipe(up=move)
        node = adrv.find(pattern)
        if node and node['b'][3] > node['b'][1]:
            return node
    raise AssertionError((pattern, app_ui.labels()))


def field_pattern(label, value):
    return '^' + re.escape(f'{label}: {value.strip()}'.replace('\n', ' / ')) + '$'


def fields(expected):
    for label, value in expected.items():
        visible(field_pattern(label, value))


def detail_shots(run, prefix):
    artifacts = []
    for section, label in [('release', 'Original date'), ('comment', 'Comment'), ('credits', 'tape op')]:
        visible(field_pattern(label, EXPECTED[label]))
        artifacts.append(run.shot(prefix + '-' + section))
    return artifacts


def detail(run):
    item = run.track(TRACK)
    assert baseline.open_track('Music', 'Release Metadata', item['title'])
    adrv.tap_node(visible(r'^Music details(?:,|$)'))
    assert adrv.find(r'^Music details, Expanded$'), app_ui.labels()


def scan(run, state):
    retag(run.fixture_dir, state)
    run.admin.request('POST', '/api/admin/rescan', {})
    deadline = time.monotonic() + 40
    while time.monotonic() < deadline:
        if not run.admin.request('GET', '/api/admin/scan-progress')['running']:
            return
        time.sleep(.25)
    raise AssertionError('Rescan did not settle')


def capture_source(run, item):
    """Visible Play, real source capture, then stabilize the animated native UI."""
    baseline.reset_progress(run.admin, item['id'])
    mark = run.audio.mark()
    assert adrv.tap('^Play Audio$'), app_ui.labels()
    deadline = time.monotonic() + 25
    sessions = [baseline.session_state()]
    while sessions[-1]['state'] != 'PLAYING' and time.monotonic() < deadline:
        time.sleep(.5)
        sessions.append(baseline.session_state())
    assert sessions[-1]['state'] == 'PLAYING' and sessions[-1]['title'] == item['title'], sessions
    time.sleep(7)
    sessions.append(baseline.session_state())
    wav = run.audio.save(mark, run.evidence.path('m05-original-source.wav'))
    # A real media key pauses without an idle UI dump during seekbar animation.
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
    deadline = time.monotonic() + 10
    while baseline.session_state()['state'] == 'PLAYING' and time.monotonic() < deadline:
        time.sleep(.25)
    assert baseline.session_state()['state'] != 'PLAYING'
    return sessions, baseline.audio_trace.analyze(wav), wav


def release(run):
    item = run.track(TRACK)
    for theme, mode in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
        time.sleep(2)
        detail(run)
        fields(EXPECTED)
        fields({'Tagged date': '2024', 'Year': '2024'})
        run.record('rich release, multiline participants and comment ' + theme, True,
                   artifacts=detail_shots(run, 'm05-' + theme))
        app_ui.back()
    # Other physical formats exercise the same production metadata view.
    for ext in ('mp3', 'opus', 'm4a'):
        track = run.track('Music/Release Metadata/aliases.' + ext)
        assert baseline.open_track('Music', 'Release Metadata', track['title'])
        adrv.tap_node(visible(r'^Music details(?:,|$)'))
        fields({'Release date':'2025-04', 'Original date':'1979', 'Recording date':'1978-11-03', 'Label':'Label A; B', 'tape op':'Op A; B'})
        run.record('real ' + ext + ' release aliases visible', True, artifacts=[run.shot('m05-' + ext)])
        app_ui.back()
    run.admin.request('POST', '/api/progress', {'media_id':item['id'], 'position_seconds':4})
    run.admin.request('POST', f'/api/media/{item["id"]}/favorite')
    before = run.admin.request('GET', f'/api/media/{item["id"]}')
    try:
        scan(run, 'changed')
        detail(run)
        fields({'Release date':'2026-06','Label':'Changed Label','tape op':'Changed / Op'})
        app_ui.back()
        scan(run, 'broken')
        retained = run.admin.request('GET', f'/api/media/{item["id"]}')
        assert retained['media']['music']['release_date'] == '2026-06', retained
        scan(run, 'cleared')
        detail(run)
        assert not adrv.find('^Release details$'), app_ui.labels()
        app_ui.back()
        after = run.admin.request('GET', f'/api/media/{item["id"]}')
        assert not after['media']['music'].get('release_date') and not after['media']['music'].get('credits'), after
        assert after['media']['id'] == before['media']['id'] and after['favorite'] == before['favorite'] and after['progress'] == before['progress'], (before, after)
        run.record('changed/cleared/failed probe retain identity and saved state', True, data={'before':before,'after':after})
    finally:
        scan(run, 'initial')
    subprocess.run([sys.executable, str(baseline.SUPPORT / 'music_fixture.py'), 'restart', str(run.fixture_dir)], check=True)
    detail(run)
    fields({'Original date':'1979','Recording date':'1978-11-03','Label':EXPECTED['Label']})
    run.record('server restart retains release snapshot visibly', True, artifacts=[run.shot('m05-restart')])
    app_ui.back()
    viewer = Client(run.state['origin']).login(**run.auth['user'])
    hidden = run.track('Restricted/Release Metadata/hidden.flac')
    status, denied = viewer.call('GET', f'/api/media/{hidden["id"]}')
    assert status == 403 and '1979' not in json.dumps(denied), (status, denied)
    run.record('restricted release metadata not disclosed to viewer', True)
    detail(run)
    adrv.tap_node(visible('^Keep offline$'))
    visible('^Available offline · Manage downloads$')
    app_ui.back()
    # Playback proof remains the original source/tone, no new audio behavior.
    assert baseline.open_track('Music', 'Release Metadata', item['title'])
    sessions, analysis, wav = capture_source(run, item)
    assert sessions[-1]['title'] == item['title'], sessions
    baseline.record_audio(run, 'original source plays after metadata round trip', item, analysis, wav)
    app_ui.back()
    assert app_ui.go_home()
    assert adrv.tap('^Open navigation menu$')
    assert adrv.tap('^Downloads$')
    assert adrv.wait('^Downloads$', timeout=20)
    adrv.adb('reverse', '--remove', f'tcp:{run.state["port"]}')
    try:
        # Data is read from the stored download, after the server route is removed.
        for theme, mode in [('light', 'no'), ('dark', 'yes')]:
            adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
            time.sleep(2)
            assert adrv.tap('^Music details$'), app_ui.labels()
            assert adrv.wait('^Saved offline snapshot$'), app_ui.labels()
            fields(EXPECTED)
            run.record('actual offline saved snapshot retains all rich metadata ' + theme, True,
                       artifacts=detail_shots(run, 'm05-offline-snapshot-' + theme))
            app_ui.back()
            assert adrv.find('^Downloads$') and not adrv.find('^Saved offline snapshot$'), app_ui.labels()
        run.record('native Back closes saved detail and retains Downloads', True)
    finally:
        adrv.adb('reverse', f'tcp:{run.state["port"]}', f'tcp:{run.state["port"]}')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    args = parser.parse_args(argv)
    if args.host_alias:
        parser.error('M05 offline acceptance requires adb reverse, omit --host-alias')
    run = None
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('M05 terminated'))
    try:
        baseline.prepare_device(args, port)
        run = Run(args)
        run.failures = 0
        assert run.audio.available(), 'QEMU WAV capture required'
        assert app_ui.connect_and_login(run.base, run.auth['admin']['username'], run.auth['admin']['password'],
                                        lambda name, ok, detail='': run.record('sign-in: '+name, ok, detail), run.shot)
        release(run)
    except Exception:
        if run is None:
            raise
        trace = run.evidence.path('m05-failure.txt')
        trace.write_text(traceback.format_exc())
        run.record('native M05 journey', False, traceback.format_exc(), artifacts=[run.shot('m05-failure'), trace])
    finally:
        if run:
            baseline.teardown(run, port)
    return 1 if run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
