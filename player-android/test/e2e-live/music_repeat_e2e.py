#!/usr/bin/env python3
"""M20 real native repeat modes, completion, occurrence edits and scoped recovery.

Root owns disposable fixture/emulator setup and stop-only cleanup. Requires rooted
API34, current APK and actual QEMU WAV. Never changes app preference contents.
"""
import argparse
import json
from pathlib import Path
import signal
import subprocess
import sys
import time
import traceback
import xml.etree.ElementTree as ET

import adrv
import app_ui
import music_e2e as baseline
import music_sequence_e2e as sequence
import music_albums_e2e as albums
import music_queue_e2e as queue_ui
import music_queue_recovery_e2e as recovery
from fixture_server import Client


class Run(baseline.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M20', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def snapshot(run):
    # Android may replace the preference file while persisting each second.
    # Keep XML/credentials only in memory; report read status, never contents.
    for attempt in range(10):
        result = subprocess.run(['adb', 'exec-out', 'cat',
            f'/data/user/0/{app_ui.PKG}/shared_prefs/FlutterSharedPreferences.xml'],
            capture_output=True, timeout=15)
        try:
            if result.returncode or not result.stdout:
                raise OSError(f'queue read rc={result.returncode}, bytes={len(result.stdout)}, stderr={result.stderr.decode(errors="replace")[:200]}')
            root = ET.fromstring(result.stdout)
        except (OSError, ET.ParseError) as error:
            if attempt == 9:
                raise
            print(f'Retrying transient scoped queue read {attempt + 1}: {type(error).__name__}', flush=True)
            time.sleep(.1)
            continue
        key = f'flutter.player.queue.{run.base}.{run.viewer_id}'
        for node in root:
            if node.attrib.get('name') == key:
                return json.loads(node.text)
        return None
    raise AssertionError('Unreachable queue read')


def wait_saved(run, predicate, timeout=15):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        last = snapshot(run)
        if last is not None and predicate(last):
            return last
        time.sleep(.2)
    raise AssertionError(('no persisted queue state', last))


def open_queue():
    if not adrv.find('^Now Playing$'):
        queue_ui.queue()


def mode(run, value):
    open_queue()
    for _ in range(3):
        current = snapshot(run).get('repeat', 'off')
        if current == value:
            break
        assert adrv.tap('^Repeat: ' + current.title() + '$'), app_ui.labels()
        wait_saved(run, lambda s: s.get('repeat', 'off') != current)
    assert adrv.find('^Repeat: ' + value.title() + '$'), app_ui.labels()
    assert snapshot(run)['repeat'] == value


def songs(run):
    return [run.track(f'Music/{baseline.ALBUM}/{name}') for name in
            ['01 - First Light.mp3', '02 - Long Road Home.mp3']]


def seed(run):
    a, b = songs(run)
    pid, saved = recovery.make_queue(run, a, b)
    queue_ui.queue()
    return pid, saved, a, b


def jump(number, song):
    recovery.queue_action(number, song['title'], 'Play now')
    app_ui.back()  # sheet closes; actual player/notification owns playback
    return sequence.wait_track(song['title'])[0]


def wait_end(run, index, timeout=25):
    deadline = time.monotonic() + timeout
    history = []
    while time.monotonic() < deadline:
        current = sequence.state()
        history.append(current)
        saved = snapshot(run)
        if current['state'] != 'PLAYING' and saved['index'] == index and saved['position'] >= 10:
            time.sleep(1)
            assert sequence.state()['state'] != 'PLAYING', sequence.state()
            return {'session': current, 'saved': saved, 'history': history}
        time.sleep(.25)
    raise AssertionError(('no natural stopped end', history[-12:], snapshot(run)))


def transport(run):
    pid, _, a, b = seed(run)
    proof = []
    if run.transport_from == 'off':
        mode(run, 'off')
        baseline.reset_progress(run.viewer, b['id'])
        mark = run.audio.mark()
        jump(3, b)
        stopped = wait_end(run, 2)
        wav, analysis, matches = albums.proof_audio(run, mark, 'm20-native-off-last', [b])
        saved = run.viewer.request('GET', f'/api/media/{b["id"]}')['progress']
        assert saved['finished'] and saved['media_id'] == b['id'] and saved['user_id'] == run.viewer_id, saved
        proof.append(stopped)
        run.record('Off natural last stops with actual B tone and authoritative finished ID', True,
                   data={'stopped': stopped, 'progress': saved, 'audio': analysis, 'matches': matches}, artifacts=[wav])
        sequence.key('PLAY')
        sequence.wait_track(b['title'])
        replay = wait_end(run, 2)
        run.record('Off same-source Play restarts and second natural end also stops', True, data=replay)
    if run.transport_from in ('off', 'all'):
        mode(run, 'all')
        recovery.queue_action(1, a['title'], 'Move down')
        edited_first = snapshot(run)['entries'][0]['id']
        mark = run.audio.mark(); jump(3, b)
        wrapped = wait_saved(run, lambda s: s['index'] == 0, timeout=25)
        assert wrapped['entries'][0]['id'] == edited_first, wrapped
        state, history = sequence.wait_track(a['title'])
        assert 0 <= state['position_ms'] < 5000, state
        time.sleep(4); sequence.key('PAUSE')
        wav, analysis, matches = albums.proof_audio(run, mark, 'm20-native-all-b-to-a', [b, a])
        run.record('All natural last wraps edited queue to first distinct actual source', True,
                   data={'wrapped': wrapped, 'state': state, 'history': history, 'audio': analysis, 'matches': matches}, artifacts=[wav])
    if run.transport_from != 'manual':
        mode(run, 'one')
        occurrence = snapshot(run)['entries'][1]['id']
        mark = run.audio.mark(); jump(2, a)
        deadline = time.monotonic() + 25
        high = False
        history = []
        while time.monotonic() < deadline:
            current = sequence.state(); live = snapshot(run)
            current['queue_position'] = live['position']; history.append(current)
            if current['title'] == a['title'] and live['position'] >= 10:
                high = True
            if high and current['title'] == a['title'] and current['state'] == 'PLAYING' and live['position'] < 2:
                break
            time.sleep(.2)
        else:
            raise AssertionError(('no same-occurrence natural time reset', history))
        repeated = snapshot(run)
        assert repeated['index'] == 1 and repeated['entries'][1]['id'] == occurrence, repeated
        time.sleep(4); sequence.key('PAUSE')
        wav, analysis, matches = albums.proof_audio(run, mark, 'm20-native-one-same-occurrence', [a])
        run.record('One natural completion resets time to zero on exact later duplicate occurrence', True,
                   data={'saved': repeated, 'history': history, 'audio': analysis, 'matches': matches}, artifacts=[wav])
    else:
        mode(run, 'one'); jump(2, a); sequence.key('PAUSE')
    # Actual touch control, then actual notification control, each with own
    # distinct tone and fresh target media progress; One must not trap either.
    proof_next, wav_next = queue_ui.output(run, b, 'm20-native-one-touch-next', lambda: adrv.tap('^Next track$'))
    assert snapshot(run)['index'] == 2
    adrv.adb('shell', 'cmd', 'statusbar', 'expand-notifications'); time.sleep(1)
    node = adrv.find(r'(?i)^(?:skip to |skip )?previous(?: track)?$')
    assert node, app_ui.labels()
    shot = run.shot('m20-native-one-notification-previous')
    proof_prev, wav_prev = queue_ui.output(run, a, 'm20-native-one-notification-previous', lambda: adrv.tap_node(node))
    assert snapshot(run)['index'] == 1
    adrv.adb('shell', 'cmd', 'statusbar', 'collapse')
    run.record('One explicit touch Next and notification Previous follow adjacency with actual source output and fresh per-ID saves', True,
               data={'next': proof_next, 'previous': proof_prev}, artifacts=[wav_next, wav_prev, shot])
    run.evidence.path('m20-native-transport-playlist.json').write_text(json.dumps({'playlist_id': pid}))


def persistence(run):
    _, _, a, b = seed(run)
    mode(run, 'one')
    recovery.queue_action(2, a['title'], 'Move down')
    saved = wait_saved(run, lambda s: s['index'] == 2)
    assert saved['repeat'] == 'one'
    app_ui.back(); recovery.restart(run); queue_ui.queue()
    assert adrv.wait('^Resume queue$', timeout=25), app_ui.labels()
    restored = wait_saved(run, lambda s: s['entries'] == saved['entries'])
    recovery.same_queue(saved, restored)
    assert restored['repeat'] == 'one' and sequence.state()['state'] != 'PLAYING'
    shots = []
    for theme, setting in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', setting); time.sleep(1)
        shots.append(run.shot('m20-native-repeat-' + theme))
    assert adrv.tap('^Resume queue$'); sequence.wait_track(a['title']); time.sleep(2); sequence.key('PAUSE')
    run.record('repeat mode and edited duplicate occurrence survive force-stop/unlock paused until explicit Resume; light/dark portrait', True,
               data={'saved': saved, 'restored': restored, 'session': sequence.state()}, artifacts=shots)
    app_ui.back(); recovery.login_other(run, run.auth['admin']); queue_ui.queue()
    assert adrv.find('^Repeat: Off$') and adrv.wait('Queue is empty'), app_ui.labels()
    other_shot = run.shot('m20-native-other-account-off')
    app_ui.back(); recovery.login_other(run, run.auth['user']); queue_ui.queue()
    assert adrv.wait('^Repeat: One$', timeout=25), app_ui.labels()
    recovery.queue_action(3, a['title'], 'Remove')
    assert sequence.state()['state'] != 'PLAYING'
    assert adrv.tap('^Undo$')
    assert snapshot(run)['repeat'] == 'one'
    assert adrv.tap('^Clear queue$'); assert adrv.tap('^Undo$')
    assert snapshot(run)['repeat'] == 'one'
    assert sequence.state()['state'] != 'PLAYING'
    assert adrv.tap('^Clear queue$'); wait_saved(run, lambda s: not s['entries'])
    app_ui.back(); recovery.restart(run); queue_ui.queue()
    assert adrv.wait('Queue is empty') and adrv.find('^Repeat: One$'), app_ui.labels()
    assert not snapshot(run)['entries'] and snapshot(run)['repeat'] == 'one'
    run.record('account switch defaults Off without previous queue, return restores One; Clear Undo and empty restart keep scoped mode without autoplay', True,
               data={'saved': snapshot(run)}, artifacts=[other_shot, run.shot('m20-native-empty-one')])
    app_ui.back()


def offline(run):
    retained = snapshot(run)
    if run.offline_snapshot is not None:
        expected = json.loads(Path(run.offline_snapshot).read_text())
        recovery.same_queue(expected, retained)
        assert retained['repeat'] == expected['repeat'], (expected, retained)
        a, b = songs(run)
        assert [e['mediaId'] for e in retained['entries']] == [a['id'], a['id'], b['id']], retained
        assert retained['index'] == 1 and 0 < retained['position'] < a['duration_s'], retained
    if not retained or not retained['entries']:
        # Offline recovery needs a real paused queue, not a particular offset
        # or the volume-calibration timing of the transport setup helper.
        albums.album()
        assert adrv.tap('^Play album$'), app_ui.labels()
        sequence.wait_track(songs(run)[0]['title']); sequence.key('PAUSE')
    assert sequence.state()['state'] != 'PLAYING', sequence.state()
    mode(run, 'all')
    saved = snapshot(run)
    app_ui.back()
    recovery.offline_unlock_recovery(run, saved)
    assert adrv.find('^Repeat: All$'), app_ui.labels()
    assert snapshot(run)['repeat'] == 'all'
    run.record('real stopped local server prevents unlock, retains scoped All mode; online explicit retry restores paused', True,
               data={'saved': saved, 'restored': snapshot(run)}, artifacts=[run.shot('m20-native-offline-recovered-all')])
    app_ui.back()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--ownership-path')
    parser.add_argument('--scenario', choices=['all', 'transport', 'recovery', 'offline'], default='all')
    parser.add_argument('--offline-snapshot')
    parser.add_argument('--transport-from', choices=['off', 'all', 'one', 'manual'], default='off')
    parser.add_argument('--resume-device', action='store_true')
    args = parser.parse_args(argv)
    run = None
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('M20 interrupted; root owns stop-only cleanup'))
    try:
        assert 'uid=0' in adrv.adb('shell', 'id'), 'Rooted disposable emulator required'
        port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
        baseline.prepare_device(args, port)
        run = Run(args); run.failures = 0; run.ownership_path = args.ownership_path; run.transport_from = args.transport_from; run.offline_snapshot = args.offline_snapshot
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        run.viewer_id = next(u['id'] for u in run.admin.request('GET', '/api/v1/admin/users') if u['username'] == run.auth['user']['username'])
        assert run.audio.available(), 'Actual QEMU output WAV required'
        if args.resume_device:
            recovery.restart(run)
        else:
            assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
                lambda name, ok, detail='': run.record('sign-in: ' + name, ok, detail), run.shot)
        for name, step in [('transport', transport), ('recovery', persistence), ('offline', offline)]:
            if args.scenario not in ('all', name):
                continue
            try:
                step(run)
            except Exception:
                trace = run.evidence.path('m20-native-failed-' + name + '-' + str(time.time_ns()) + '.txt')
                trace.write_text(traceback.format_exc())
                run.record(name, False, traceback.format_exc(), artifacts=[trace, run.shot('m20-native-failed-' + name)])
                break
    except Exception:
        if not run:
            raise
        run.record('setup', False, traceback.format_exc(), artifacts=[run.shot('m20-native-setup-failure')])
    finally:
        print('Runtime retained for root evidence audit and stop-only cleanup.', flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
