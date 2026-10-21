#!/usr/bin/env python3
"""M16 actual native queue persistence, force-stop and explicit recovery.

Use a disposable rooted API34 emulator, current release APK and QEMU output
WAV. Reads only the scoped queue JSON from app preferences; never writes app
state. Root orchestrator owns fixture/emulator/adb cleanup after evidence audit.
"""
import argparse
import json
from pathlib import Path
import re
import signal
import sys
import time
import traceback
import xml.etree.ElementTree as ET

import adrv
import app_ui
import music_e2e as baseline
import music_sequence_e2e as sequence
import music_playlists_e2e as playlists
import music_playlist_songs_e2e as edits
import music_queue_e2e as queue_ui
from fixture_server import Client, PlayerServer, answers, stop_pid
from music_fixture import save_state, started_process


class Run(baseline.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M16', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def snapshot(run, uid=None):
    # Rooted disposable emulator only. Full preferences are kept in memory and
    # never printed or saved; artifacts contain this one queue document only.
    root = ET.fromstring(adrv.adb('exec-out', 'cat',
        f'/data/user/0/{app_ui.PKG}/shared_prefs/FlutterSharedPreferences.xml'))
    key = f'flutter.player.queue.{run.base}.{uid or run.viewer_id}'
    for node in root:
        if node.attrib.get('name') == key:
            return json.loads(node.text)
    return None


def wait_saved(run, predicate, timeout=15):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        last = snapshot(run)
        if last is not None and predicate(last):
            return last
        time.sleep(.2)
    raise AssertionError(('no persisted queue state', last))


def same_queue(before, after):
    assert after['entries'] == before['entries'], (before, after)
    assert after['index'] == before['index'], (before, after)
    assert abs(after['position'] - before['position']) < .05, (before, after)


def queue_action(number, title, action):
    target = edits.row(number, title)
    menus = [node for node in adrv.nodes() if adrv.label(node) == 'Queue actions for ' + title
             and target['b'][1] <= (node['b'][1] + node['b'][3]) // 2 <= target['b'][3]]
    assert menus, (target, app_ui.labels())
    adrv.tap_node(menus[-1])
    assert adrv.tap('^' + re.escape(action) + '$'), app_ui.labels()
    assert adrv.wait('^Now Playing$'), app_ui.labels()


def restart(run):
    adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
    app_ui.launch()
    assert adrv.wait('^Choose a server$', timeout=40), app_ui.labels()
    saved = adrv.find(re.escape(run.base) + '$')
    assert saved, app_ui.labels()
    adrv.tap_node(saved)
    assert adrv.wait('^Welcome back$', timeout=25), app_ui.labels()
    assert adrv.find('^Unlock saved login with fingerprint$'), app_ui.labels()
    fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
    assert any(run.auth['user']['username'] in (n['text'], n['desc']) for n in fields), fields
    adrv.tap_node(fields[-1])
    assert app_ui.sign_in(run.auth['user']['password']), app_ui.labels()
    assert adrv.wait('^Library$', timeout=30), app_ui.labels()


def login_other(run, account):
    assert app_ui.go_home(), app_ui.labels()
    assert adrv.tap('^Settings$'), app_ui.labels()
    assert adrv.wait('^Log Out$'), app_ui.labels()
    assert adrv.tap('^Log Out$'), app_ui.labels()
    assert adrv.wait('^Welcome back$', timeout=25), app_ui.labels()
    fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
    assert len(fields) == 2, app_ui.labels()
    adrv.tap_node(fields[0]); app_ui.clear_field(); adrv.type_text(account['username'])
    adrv.tap_node(fields[1]); app_ui.clear_field()
    assert app_ui.sign_in(account['password']), app_ui.labels()
    assert adrv.wait('^Library$', timeout=30), app_ui.labels()


def make_queue(run, a, b):
    name = 'M16 repeated queue ' + str(int(time.time()))
    pid = run.viewer.request('POST', '/api/v1/playlists', {'name': name})['playlist_id']
    run.viewer.request('POST', '/api/v1/playlists/' + pid + '/songs',
                       {'action': 'add', 'media_ids': [a['id'], b['id'], a['id']]})
    playlists.catalog(); playlists.tap_visible(re.escape(name)); playlists.wait_detail(name)
    adrv.tap_node(edits.row(3, a['title']))
    sequence.wait_track(a['title']); sequence.key('PAUSE')
    assert sequence.state()['state'] == 'PAUSED', sequence.state()
    queue_ui.queue()
    queue_action(3, a['title'], 'Move up')
    wait_saved(run, lambda s: s['index'] == 1 and [v['mediaId'] for v in s['entries']] == [a['id'], a['id'], b['id']])
    app_ui.back()
    sequence.key('PLAY'); sequence.wait_track(a['title'])
    # Set hardware volume during deliberate playback, then pause before slow UI.
    for _ in range(15):
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_VOLUME_UP')
    time.sleep(1)
    sequence.key('PAUSE')
    assert sequence.state()['state'] == 'PAUSED', sequence.state()
    saved = wait_saved(run, lambda s: 2 <= s['position'] < 9)
    time.sleep(5)  # volume overlay dismissal while paused
    return pid, saved


def journey(run):
    a = run.track(f'Music/{baseline.ALBUM}/01 - First Light.mp3')
    b = run.track(f'Music/{baseline.ALBUM}/02 - Long Road Home.mp3')
    pid, saved = make_queue(run, a, b)
    assert saved['entries'][0]['id'] != saved['entries'][1]['id'], saved
    run.evidence.path('m16-native-before-force-stop.json').write_text(json.dumps(saved, indent=2))
    restart(run)
    queue_ui.queue()
    assert adrv.wait('^Resume queue$', timeout=25), app_ui.labels()
    after = wait_saved(run, lambda s: len(s['entries']) == 3)
    same_queue(saved, after)
    assert sequence.state()['state'] != 'PLAYING', sequence.state()
    time.sleep(2)
    same_queue(after, snapshot(run))
    assert sequence.state()['state'] != 'PLAYING', sequence.state()
    assert adrv.find('^Open current item$'), app_ui.labels()
    shots = []
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', system)
        time.sleep(1)
        shots.append(run.shot('m16-native-recovered-' + theme))
    run.record('force-stop remembered locked login, explicit unlock restores exact repeated order/index/offset paused with no autoplay', True,
               data={'before': saved, 'after': after, 'session': sequence.state()}, artifacts=shots)
    # The detail route is deliberately navigation only. Back returns to Library.
    assert adrv.tap('^Open current item$'), app_ui.labels()
    assert adrv.wait('^Play Audio$', timeout=20), app_ui.labels()
    assert sequence.state()['state'] != 'PLAYING', sequence.state()
    app_ui.back(); queue_ui.queue()
    mark = run.audio.mark()
    resumed_at = time.monotonic()
    assert adrv.tap('^Resume queue$'), app_ui.labels()
    resumed_runtime, _ = sequence.wait_track(a['title'])
    runtime_position = resumed_runtime['position_ms'] / 1000
    assert saved['position'] - .15 <= runtime_position <= saved['position'] + time.monotonic() - resumed_at + 1, (saved, resumed_runtime)
    time.sleep(min(4.5, max(2, 11 - saved['position'])))
    sequence.key('PAUSE')
    wav = run.audio.save(mark, run.evidence.path('m16-native-resumed-output.wav'))
    analysis = baseline.audio_trace.analyze(wav)
    segment = baseline.main_segment(analysis)
    assert segment and segment['duration_s'] > 1.5 and baseline.tone_matches(segment, a), analysis
    progressed = wait_saved(run, lambda s: s['position'] > saved['position'] + 1)
    assert progressed['index'] == saved['index'], progressed
    run.record('touch Resume reclaims repeated current source at saved offset and produces original tone', True,
               data={'saved': saved, 'progressed': progressed, 'resumed_runtime': resumed_runtime, 'audio': analysis}, artifacts=[wav])
    app_ui.back()
    login_other(run, run.auth['admin'])
    queue_ui.queue()
    assert adrv.wait('Queue is empty'), app_ui.labels()
    assert not adrv.find('^Playing from:'), app_ui.labels()
    assert not adrv.find('^Resume queue$'), app_ui.labels()
    shot = run.shot('m16-native-other-account-empty')
    app_ui.back(); login_other(run, run.auth['user'])
    queue_ui.queue(); assert adrv.wait('^Resume queue$', timeout=25), app_ui.labels()
    restored = wait_saved(run, lambda s: len(s['entries']) == 3)
    same_queue(progressed, restored)
    run.record('actual logout/admin sign-in hides all prior queue metadata; original user recovers own exact session', True,
               data={'viewer_id': run.viewer_id, 'restored': restored}, artifacts=[shot])
    assert adrv.tap('^Clear queue$'), app_ui.labels()
    wait_saved(run, lambda s: not s['entries'])
    app_ui.back(); restart(run); queue_ui.queue()
    assert adrv.wait('Queue is empty'), app_ui.labels()
    assert not snapshot(run)['entries'], snapshot(run)
    run.record('Clear survives force-stop and locked-login restart without resurrecting old tracks', True,
               artifacts=[run.shot('m16-native-cleared-restart')])
    app_ui.back()
    # Create a real new queue and deny its media before restart. Revalidation
    # drops occurrences while retaining a quiet readable unavailable notice.
    _, denied_before = make_queue(run, a, b)
    run.admin.request('DELETE', '/api/v1/admin/permissions', {'set_id': run.state['set_ids']['Music'], 'user_id': run.viewer_id})
    try:
        restart(run); queue_ui.queue()
        assert adrv.wait('3 songs are no longer available', timeout=25), app_ui.labels()
        assert not snapshot(run)['entries'], snapshot(run)
        run.record('real revoked permission drops unavailable repeated occurrences after restart', True,
                   data={'before': denied_before}, artifacts=[run.shot('m16-native-denied-recovery')])
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', {'set_id': run.state['set_ids']['Music'], 'user_id': run.viewer_id, 'role': 'viewer'})
    run.evidence.path('m16-native-checkpoint.json').write_text(json.dumps({'playlist_id': pid, 'saved': saved, 'progressed': progressed}, indent=2))


def offline_unlock_recovery(run, saved):
    previous = dict(run.state['server'])
    assert stop_pid(previous['pid'], previous['binary'], start=previous['start']), 'Owned server identity mismatch'
    assert answers(run.state['origin'], '/healthz') is None, 'Owned server still answers'
    adrv.adb('reverse', '--remove', f'tcp:{run.state["port"]}')
    try:
        adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
        app_ui.launch()
        assert adrv.wait('^Choose a server$', timeout=40), app_ui.labels()
        node = adrv.find(re.escape(run.base) + '$'); assert node, app_ui.labels()
        adrv.tap_node(node)
        assert adrv.wait('^Welcome back$', timeout=25), app_ui.labels()
        fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
        adrv.tap_node(fields[-1]); adrv.type_text(run.auth['user']['password'])
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_BACK')
        assert adrv.tap('^Sign In$', cls='Button'), app_ui.labels()
        error = adrv.wait('Could not reach the server', timeout=20)
        assert error and adrv.find('^Welcome back$') and not adrv.find('^Library$'), app_ui.labels()
        same_queue(saved, snapshot(run))
        assert sequence.state()['state'] != 'PLAYING', sequence.state()
        run.record('real owned server outage during cold unlock shows connection failure, preserves exact queue and never autoplays', True,
                   data={'saved': saved, 'offline_session': sequence.state(), 'server_stopped': previous['pid']},
                   artifacts=[run.shot('m16-native-offline-unlock')])
    finally:
        server = PlayerServer(previous['binary'], run.fixture_dir, run.state['port'])
        run.state['server'].update(started_process(server.start(detached=True)))
        run.state['server']['restarts'] = run.state['server'].get('restarts', 0) + 1
        save_state(run.fixture_dir, run.state)
        if run.ownership_path is not None:
            ownership = Path(run.ownership_path)
            owned = json.loads(ownership.read_text()); owned['server'] = run.state['server']
            ownership.write_text(json.dumps(owned, indent=2))
        adrv.adb('reverse', f'tcp:{run.state["port"]}', f'tcp:{run.state["port"]}')
        run.record('same final binary database and media restarted after verified offline cutoff', True,
                   data={'previous': previous, 'current': run.state['server']})
    assert adrv.tap('^Sign In$', cls='Button'), app_ui.labels()
    assert adrv.wait('^Library$', timeout=30), app_ui.labels()
    queue_ui.queue()
    assert adrv.wait('^Resume queue$', timeout=25), app_ui.labels()
    same_queue(saved, wait_saved(run, lambda state: len(state['entries']) == 3))
    assert sequence.state()['state'] != 'PLAYING', sequence.state()
    run.record('online explicit password retry recovers same paused queue after actual outage', True,
               data={'saved': saved, 'recovered': snapshot(run)}, artifacts=[run.shot('m16-native-offline-recovered')])


def normal_next(run):
    a = run.track(f'Music/{baseline.ALBUM}/01 - First Light.mp3')
    b = run.track(f'Music/{baseline.ALBUM}/02 - Long Road Home.mp3')
    _, saved = make_queue(run, a, b)
    offline_unlock_recovery(run, saved)
    same_queue(saved, wait_saved(run, lambda state: len(state['entries']) == 3))
    assert sequence.state()['state'] != 'PLAYING', sequence.state()
    assert adrv.tap('^Resume queue$'), app_ui.labels()
    sequence.wait_track(a['title'])
    sequence.key('PAUSE')
    assert sequence.state()['state'] == 'PAUSED', sequence.state()
    assert snapshot(run)['index'] == 1, snapshot(run)
    # Open the real Player notification while paused so a short fixture cannot
    # naturally advance before the explicit Next control is tapped.
    app_ui.back()
    adrv.adb('shell', 'cmd', 'statusbar', 'expand-notifications')
    time.sleep(1)
    node = adrv.find(r'(?i)^(?:skip to |skip )?next(?: track)?$')
    assert node, app_ui.labels()
    shot = run.shot('m16-native-recovered-next-notification')
    baseline.reset_progress(run.viewer, b['id'])
    mark = run.audio.mark()
    started = time.monotonic()
    adrv.tap_node(node)
    runtime, _ = sequence.wait_track(b['title'])
    assert 0 <= runtime['position_ms'] / 1000 <= time.monotonic() - started + 1, runtime
    time.sleep(6)
    sequence.key('PAUSE')
    wav = run.audio.save(mark, run.evidence.path('m16-native-recovered-normal-next.wav'))
    analysis = baseline.audio_trace.analyze(wav)
    segment = baseline.main_segment(analysis)
    assert segment and segment['duration_s'] > 2 and baseline.tone_matches(segment, b), analysis
    after = wait_saved(run, lambda state: state['index'] == 2 and state['position'] >= 2)
    assert after['entries'][2]['id'] == saved['entries'][2]['id'], (saved, after)
    assert after['entries'][2]['mediaId'] == b['id'], after
    detail = run.viewer.request('GET', f'/api/v1/media/{b["id"]}')
    progress = detail.get('progress') or {}
    assert progress.get('user_id') == run.viewer_id and progress.get('media_id') == b['id'], progress
    assert progress.get('position_seconds', 0) >= 2, progress
    adrv.adb('shell', 'cmd', 'statusbar', 'collapse')
    run.record('supplemental recovered later occurrence Resume then real notification Next chooses following Long Road Home', True,
               data={'saved': saved, 'after': after, 'runtime': runtime, 'progress': progress, 'audio': analysis}, artifacts=[shot, wav])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--ownership-path', help='Optional orchestration ownership JSON to refresh after owned server restart')
    parser.add_argument('--scenario', choices=['main', 'normal-next'], default='main')
    parser.add_argument('--resume-device', action='store_true')
    parser.add_argument('--hold-device', action='store_true', help='Compatibility flag: root owns cleanup in all cases')
    args = parser.parse_args(argv)
    run = None
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('M16 interrupted; root owns stop-only cleanup'))
    try:
        assert 'uid=0' in adrv.adb('shell', 'id'), 'Disposable rooted emulator required; run adb root before reverse/setup'
        port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
        if args.resume_device:
            assert baseline.has_online_network(adrv.adb('shell', 'dumpsys', 'connectivity'))
            adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
        else:
            baseline.prepare_device(args, port)
        run = Run(args); run.failures = 0; run.ownership_path = args.ownership_path
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        run.viewer_id = next(u['id'] for u in run.admin.request('GET', '/api/v1/admin/users') if u['username'] == run.auth['user']['username'])
        assert run.audio.available(), 'Actual QEMU WAV output required'
        if not args.resume_device:
            assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
                lambda name, ok, detail='': run.record('sign-in: ' + name, ok, detail), run.shot)
        if args.scenario == 'normal-next':
            normal_next(run)
        else:
            journey(run)
    except Exception:
        if run:
            trace = run.evidence.path('m16-native-failure.txt'); trace.write_text(traceback.format_exc())
            run.record('native recovery journey', False, traceback.format_exc(), artifacts=[trace, run.shot('m16-native-failure')])
        else:
            raise
    finally:
        print('Runtime retained for root evidence audit and stop-only cleanup.', flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
