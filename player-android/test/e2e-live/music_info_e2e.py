#!/usr/bin/env python3
"""M06 real native Info, uninterrupted source, errors and saved snapshots."""
import argparse
import json
from pathlib import Path
import signal
import sys
import time
import traceback

import adrv
import app_ui
import music_e2e as baseline
from fixture_server import Client, PlayerServer, answers, stop_pid
from music_fixture import save_state, started_process
from music_info import TRACK, LOSSY, HIDDEN, FOLDER, EXPECTED
from music_release_e2e import field_pattern


class Run(baseline.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M06', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} {detail}', flush=True)
        if result == 'fail':
            self.failures += 1


def playing(item):
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline:
        state = baseline.session_state()
        if state['state'] == 'PLAYING' and state['title'] == item['title']:
            return state
        time.sleep(.25)
    raise AssertionError('Expected native playing source: ' + repr(state))


def continues(item, before):
    time.sleep(1)
    after = playing(item)
    # Android's MediaSession reports position only at sparse update points.
    # The QEMU output capture below verifies audible progress across Info.
    assert after['position_ms'] >= before['position_ms'], (before, after)
    return after


def visible(pattern):
    """Find a row by scrolling inside the Info dialog's content viewport."""
    nodes = adrv.nodes()
    scroll = next((n for n in nodes if n['cls'] == 'ScrollView'), None)
    if scroll is None:
        node = adrv.find(pattern)
        assert node, (pattern, app_ui.labels())
        return node
    x = (scroll['b'][0] + scroll['b'][2]) // 2
    top, bottom = scroll['b'][1], scroll['b'][3]
    for direction in (None, *([True] * 6), *([False] * 6)):
        node = adrv.find(pattern)
        if node and node['b'][3] > node['b'][1]:
            return node
        if direction is not None:
            start = bottom - (bottom - top) // 4 if direction else top + (bottom - top) // 4
            end = top + (bottom - top) // 4 if direction else bottom - (bottom - top) // 4
            adrv.adb('shell', 'input', 'swipe', str(x), str(start), str(x), str(end), '350')
    raise AssertionError((pattern, app_ui.labels()))


def fields(expected):
    for label, value in expected.items():
        visible(field_pattern(label, value))


def open_info(run):
    # The player animates while audio runs, so a fresh UI dump can time out
    # waiting for Android's idle state. Reuse the button found while paused.
    adrv.tap_node(run.info_button)
    assert adrv.wait('^Music and file information$'), app_ui.labels()


def advanced():
    node = visible(r'^Advanced audio, Collapsed$')
    adrv.tap_node(node)
    visible(r'^Advanced audio, Expanded$')


def source(run, path=TRACK):
    item = run.track(path)
    baseline.reset_progress(run.viewer, item['id'])
    assert baseline.open_track('Music', FOLDER, item['title'])
    # Persistent queue/sync bars can cover the bottom of the detail. A fresh
    # semantic node still exists there, but tapping its center hits the bar.
    for _ in range(10):
        play = adrv.find('^Play Audio$')
        if play and play['b'][3] <= 1500:
            adrv.tap_node(play)
            break
        adrv.adb('shell', 'input', 'swipe', '250', '1350', '250', '750', '350')
        time.sleep(.25)
    else:
        raise AssertionError(('Play Audio remains obstructed', app_ui.labels()))
    playing(item)
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
    run.info_button = adrv.find('^Info$')
    assert run.info_button, app_ui.labels()
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PLAY')
    return item, playing(item)


def journey(run):
    artifacts = []
    for theme, mode in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
        time.sleep(2)
        item, before = source(run)
        mark = run.audio.mark()
        open_info(run)
        visible(field_pattern('Tempo', EXPECTED['Tempo']))
        assert adrv.find(r'^Advanced audio, Collapsed$'), app_ui.labels()
        advanced()
        fields(EXPECTED)
        artifacts.append(run.shot('m06-' + theme + '-audio-details'))
        visible(r'^Engineer: Engineer 東京 / Long technical credit participant')
        artifacts.append(run.shot('m06-' + theme + '-long-credit'))
        during = continues(item, before)
        app_ui.back()
        # UIAutomator cannot always obtain an idle tree from the animated
        # player, so verify Back by the retained playing MediaSession below.
        after = continues(item, during)
        wav = run.audio.save(mark, run.evidence.path('m06-' + theme + '-info-source.wav'))
        analysis = baseline.audio_trace.analyze(wav)
        baseline.record_audio(run, 'Info retains original source ' + theme, item, analysis, wav)
        run.record('touch disclosure and Back keep live player ' + theme, True,
                   data={'before': before, 'during': during, 'after': after}, artifacts=artifacts[-2:])
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
        app_ui.back()
    item, _ = source(run, LOSSY)
    open_info(run)
    advanced()
    visible(field_pattern('Sample rate', EXPECTED['Sample rate']))
    # Bit depth would immediately follow sample rate in this same visible group.
    assert not adrv.find(r'^Bit depth:'), app_ui.labels()
    fields({'Track ReplayGain': '0 dB'})
    # Traverse the rest of the available source/tag rows as well.

    for label in ('Track peak', 'Album peak'):
        visible(field_pattern(label, EXPECTED[label]))
    assert not adrv.find(r'^Bit depth:'), app_ui.labels()
    app_ui.back()
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
    app_ui.back()
    run.record('lossy source has known rate/channels and no invented bit depth', True)


def errors(run):
    item, before = source(run)
    previous = dict(json.loads((run.fixture_dir / 'state.json').read_text())['server'])
    assert stop_pid(previous['pid'], previous['binary'], start=previous.get('start'))
    assert answers(run.state['origin'], '/healthz') is None
    try:
        open_info(run)
        visible(r'^Could not load information\.')
        continues(item, before)
        run.record('actual offline Info fails without replacing buffered source', True,
                   artifacts=[run.shot('m06-offline-info')])
    finally:
        server = PlayerServer(previous['binary'], run.fixture_dir, run.state['port'])
        run.state = json.loads((run.fixture_dir / 'state.json').read_text())
        run.state['server'].update(started_process(server.start(detached=True)))
        run.state['server']['restarts'] = run.state['server'].get('restarts', 0) + 1
        save_state(run.fixture_dir, run.state)
    assert adrv.tap('^Retry$'), app_ui.labels()
    visible(field_pattern('Tempo', EXPECTED['Tempo']))
    advanced()
    fields({'Bit depth': EXPECTED['Bit depth'], 'Track ReplayGain': '0 dB'})
    app_ui.back()
    before = playing(item)
    uid = next(u['id'] for u in run.admin.request('GET', '/api/v1/admin/users')
               if u['username'] == run.auth['user']['username'])
    permission = {'set_id':run.state['set_ids']['Music'], 'user_id':uid}
    run.admin.request('DELETE', '/api/v1/admin/permissions', permission)
    try:
        open_info(run)
        visible(r'^Could not load information\.')
        assert not adrv.find(r'^Tempo:'), app_ui.labels()
        continues(item, before)
        run.record('actual denied Info clears fields and keeps source', True,
                   artifacts=[run.shot('m06-denied-info')])
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', dict(permission, role='viewer'))
    assert adrv.tap('^Retry$'), app_ui.labels()
    visible(field_pattern('Tempo', EXPECTED['Tempo']))
    app_ui.back()
    hidden = run.track(HIDDEN)
    status, denied = run.viewer.call('GET', f'/api/v1/media/{hidden["id"]}')
    assert status == 403 and 'replaygain' not in json.dumps(denied), (status, denied)
    after = run.viewer.request('GET', f'/api/v1/media/{item["id"]}')
    assert after['media']['id'] == item['id'] and after['media']['music']['bit_depth'] == 24
    run.record('same database restart/permission recovery retain indexed fields', True)
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
    app_ui.back()


def saved(run):
    item = run.track(TRACK)
    assert baseline.open_track('Music', FOLDER, item['title'])
    adrv.tap_node(visible('^Keep offline$'))
    visible('^Available offline · Manage downloads$')
    app_ui.back()
    assert app_ui.go_home()
    assert adrv.tap('^Open navigation menu$')
    assert adrv.tap('^Downloads$')
    assert adrv.wait('^Downloads$')
    adrv.adb('reverse', '--remove', f'tcp:{run.state["port"]}')
    try:
        assert adrv.tap('^Music details$'), app_ui.labels()
        visible('^Saved offline snapshot$')
        advanced()
        fields(EXPECTED)
        run.record('available saved fields survive actual offline download dialog', True,
                   artifacts=[run.shot('m06-offline-saved')])
        app_ui.back()
        assert adrv.find('^Downloads$') and not adrv.find('^Saved offline snapshot$')
    finally:
        adrv.adb('reverse', f'tcp:{run.state["port"]}', f'tcp:{run.state["port"]}')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--resume-device', action='store_true')
    parser.add_argument('--scenario', choices=['all','journey','errors','saved'], default='all')
    args = parser.parse_args(argv)
    assert not args.host_alias, 'Offline acceptance requires isolated adb reverse'
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    run = None
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('M06 terminated'))
    try:
        baseline.prepare_device(args, port)
        run = Run(args)
        run.failures = 0
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        assert run.audio.available(), 'QEMU WAV capture required'
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
            lambda name, ok, detail='':run.record('sign-in: '+name, ok, detail), run.shot)
        for name, step in [('journey',journey), ('errors',errors), ('saved',saved)]:
            if args.scenario in ('all',name):
                step(run)
    except Exception:
        if run is None:
            raise
        trace = run.evidence.path('m06-failure.txt')
        trace.write_text(traceback.format_exc())
        run.record('native Info journey', False, traceback.format_exc(), artifacts=[run.shot('m06-failure'),trace])
    finally:
        if run and not args.resume_device:
            baseline.teardown(run, port)
    return 1 if not run or run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
