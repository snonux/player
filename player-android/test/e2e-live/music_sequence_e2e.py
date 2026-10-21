#!/usr/bin/env python3
"""M12 actual native music sequencing against the disposable music fixture.

Uses touch, media-session state, actual notification/lockscreen buttons,
server progress by media ID and QEMU audio output. No mocked playback.
"""
import argparse
import json
from pathlib import Path
import re
import signal
import subprocess
import sys
import time

import adrv
import app_ui
import music_e2e as baseline
from music_e2e import audio_trace

ALBUM = baseline.ALBUM


class Run(baseline.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M12', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def state():
    return baseline.session_state()


def wait_track(title, timeout=20):
    deadline = time.monotonic() + timeout
    history = []
    while time.monotonic() < deadline:
        current = state()
        history.append(current)
        if current['title'] == title and current['state'] == 'PLAYING':
            return current, history
        time.sleep(0.25)
    raise AssertionError(f'No playing {title}: {history[-12:]}')


def key(name):
    adrv.adb('shell', 'input', 'keyevent', f'KEYCODE_MEDIA_{name}')


def poll_finished(run, media_id):
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        detail = run.admin.request('GET', f'/api/v1/media/{media_id}')
        if detail.get('progress', {}).get('finished'):
            return detail['progress']
        time.sleep(0.5)
    raise AssertionError(f'No saved completion for media {media_id}: {detail}')


def ensure_music_volume(run):
    # Volume keys target STREAM_MUSIC only while actual Player audio is active.
    for _ in range(3):
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_VOLUME_UP')
    dump = adrv.adb('shell', 'dumpsys', 'audio')
    music = dump.split('- STREAM_MUSIC:', 1)[1].split('\n\n', 1)[0]
    path = run.evidence.path('m12-actual-music-volume.txt')
    path.write_text(music)
    maximum = int(re.search(r'Max: (\d+)', music).group(1))
    speaker = int(re.search(r'2 \(speaker\): (\d+)', music).group(1))
    assert speaker == maximum == 15, music
    assert 'Muted: false' in music and 'Muted Internally: false' in music, music
    run.record('actual STREAM_MUSIC volume verified before audio proof', True,
               f'speaker={speaker}, maximum={maximum}', artifacts=[path])


def verify_manual_output(run, song, mark, name, minimum=2):
    # A media title alone cannot prove that transport replaced the source.
    # Every manual action must produce its own expected tone and per-ID save.
    # V01 gives playback seven seconds after PLAYING for QEMU output startup.
    time.sleep(7)
    key('PAUSE')
    wav = run.audio.save(mark, run.evidence.path(name + '.wav'))
    analysis = audio_trace.analyze(wav)
    segment = baseline.main_segment(analysis)
    assert segment and segment['duration_s'] > 2 and baseline.tone_matches(segment, song), analysis
    deadline = time.monotonic() + 20
    detail = None
    while time.monotonic() < deadline:
        detail = run.admin.request('GET', f'/api/v1/media/{song["id"]}')
        progress = detail.get('progress') or {}
        if progress.get('position_seconds', 0) >= minimum or progress.get('finished'):
            break
        time.sleep(.5)
    assert detail['media']['id'] == song['id'], detail
    assert progress.get('position_seconds', 0) >= minimum or progress.get('finished'), detail
    return {'media_id': song['id'], 'progress':progress, 'audio':analysis}, wav


def touch_transport(run, songs):
    for song in songs:
        baseline.reset_progress(run.admin, song['id'])
    assert baseline.open_track('Music', ALBUM, songs[0]['title'])
    assert adrv.tap('^Play Audio$')
    wait_track(songs[0]['title'])
    ensure_music_volume(run)
    key('PAUSE')
    # Pause keeps the controls still while UIAutomator finds their semantics.
    for action, target in [('Next track', songs[1]), ('Previous track', songs[0])]:
        baseline.reset_progress(run.admin, target['id'])
        if action == 'Next track':
            run.admin.request('POST', '/api/v1/progress', {'media_id':target['id'], 'position_seconds':3})
        mark = run.audio.mark()
        assert adrv.tap(f'^{action}$'), f'Missing in-app {action}'
        current, history = wait_track(target['title'])
        if action == 'Next track':
            assert current['position_ms'] >= 2900, f'Sets must resume saved target position: {current}'
        proof, wav = verify_manual_output(run, target, mark, 'm12-touch-' + action.replace(' ', '-'),
                                          minimum=5 if action == 'Next track' else 2)
        run.record(f'in-app {action} changes actual track', True,
                   json.dumps(current), dict(proof, sessions=history),
                   [wav, run.shot('m12-' + action.replace(' ', '-'))])
    labels = app_ui.labels()
    assert 'Skip back 15 seconds' in labels and 'Skip forward 15 seconds' in labels
    density = int(adrv.adb('shell', 'wm', 'density').split(':')[-1].strip()) / 160
    for name in ['Next track', 'Previous track', 'Skip back 15 seconds', 'Skip forward 15 seconds']:
        node = adrv.find('^' + re.escape(name) + '$')
        assert node and min(node['b'][2]-node['b'][0], node['b'][3]-node['b'][1]) / density >= 48, node
    run.record('track controls and separate 15-second seeking have distinct labels', True,
               json.dumps(labels))


def natural_completions(run, songs):
    key('PAUSE')
    for song in songs:
        baseline.reset_progress(run.admin, song['id'])
    assert baseline.open_track('Music', ALBUM, songs[0]['title'])
    mark = run.audio.mark()
    assert adrv.tap('^Play Audio$')
    history = []
    artifacts = []
    for song in songs:
        current, steps = wait_track(song['title'], 22)
        history.extend(steps)
        if song['id'] == songs[0]['id']:
            ensure_music_volume(run)
        track_mark = run.audio.mark()
        deadline = time.monotonic() + 22
        while time.monotonic() < deadline:
            current = state()
            history.append(current)
            if current['title'] != song['title']:
                break
            time.sleep(0.25)
        assert current['title'] != song['title'], f'{song["title"]} did not naturally advance'
        wav = run.audio.save(track_mark, run.evidence.path(f'm12-natural-{song["id"]}.wav'))
        analysis = audio_trace.analyze(wav)
        segment = baseline.main_segment(analysis)
        assert segment and segment['duration_s'] > 4 and baseline.tone_matches(segment, song), analysis
        artifacts.append(wav)
        progress = poll_finished(run, song['id'])
        run.record(f'natural completion saves media {song["id"]} then advances', True,
                   song['title'], {'media_id': song['id'], 'progress': progress, 'next': current, 'audio':analysis}, [wav])
    key('PAUSE')
    wav = run.audio.save(mark, run.evidence.path('m12-three-natural-songs.wav'))
    analysis = audio_trace.analyze(wav)
    run.record('three distinct natural songs audibly output in order', True,
               'actual session transitions, per-ID completions and distinct tones',
               {'sessions': history, 'audio': analysis}, artifacts + [wav])


def native_controls(run, songs):
    # Reopen song one; Back leaves the handler owning the sequence.
    for song in songs:
        baseline.reset_progress(run.admin, song['id'])
    assert baseline.open_track('Music', ALBUM, songs[0]['title'])
    assert adrv.tap('^Play Audio$')
    wait_track(songs[0]['title'])
    ensure_music_volume(run)
    key('PAUSE')
    app_ui.back()
    assert adrv.find('^Play Audio$'), f'Back did not return to track details: {app_ui.labels()}'
    assert songs[0]['title'] in ' '.join(app_ui.labels())
    assert state()['title'] == songs[0]['title']
    run.record('Back returns to details while background sequence remains', True,
               json.dumps(state()), artifacts=[run.shot('m12-after-back')])
    adrv.adb('shell', 'cmd', 'statusbar', 'expand-notifications')
    time.sleep(1)
    for direction, song in [('next', songs[1]), ('previous', songs[0])]:
        labels = app_ui.labels()
        node = adrv.find(r'(?i)^(?:skip to |skip )?' + direction + r'(?: track)?$')
        assert node, f'Missing notification {direction}: {labels}'
        shot = run.shot(f'm12-notification-{direction}')
        baseline.reset_progress(run.admin, song['id'])
        mark = run.audio.mark()
        adrv.tap_node(node)
        current, history = wait_track(song['title'])
        proof, wav = verify_manual_output(run, song, mark, shot.stem)
        run.record(f'notification {direction} changes background track', True,
                   json.dumps(current), dict(proof, sessions=history), [shot, wav])
    adrv.adb('shell', 'cmd', 'statusbar', 'collapse')
    # Enable the disposable emulator's swipe keyguard; no credentials are set.
    adrv.adb('shell', 'locksettings', 'set-disabled', 'false')
    adrv.adb('shell', 'settings', 'put', 'secure', 'lock_screen_show_notifications', '1')
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_SLEEP')
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_WAKEUP')
    time.sleep(1)
    policy = adrv.adb('shell', 'dumpsys', 'window', 'policy')
    windows = adrv.adb('shell', 'dumpsys', 'window')
    keyguard = (re.search(r'mKeyguardShowing=true|mShowingLockscreen=true|isKeyguardShowing=true', policy + windows)
                or re.search(r'KeyguardServiceDelegate[\s\S]*?showing=true', policy))
    run.evidence.path('m12-keyguard-state.txt').write_text(policy + '\n' + windows)
    assert keyguard, 'lockscreen not actually showing; notification controls cannot count as lockscreen proof'
    for direction, song in [('next', songs[1]), ('previous', songs[0])]:
        labels = app_ui.labels()
        node = adrv.find(r'(?i)^(?:skip to |skip )?' + direction + r'(?: track)?$')
        assert node, f'Missing lockscreen {direction}: {labels}'
        shot = run.shot(f'm12-lockscreen-{direction}')
        baseline.reset_progress(run.admin, song['id'])
        mark = run.audio.mark()
        adrv.tap_node(node)
        current, history = wait_track(song['title'])
        proof, wav = verify_manual_output(run, song, mark, shot.stem)
        run.record(f'lockscreen {direction} changes background track', True,
                   json.dumps(current), dict(proof, sessions=history), [shot, wav])
    adrv.adb('shell', 'wm', 'dismiss-keyguard')
    adrv.adb('shell', 'locksettings', 'set-disabled', 'true')


def themes(run, songs):
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        # Open a fresh player route under each system appearance setting.
        adrv.adb('shell', 'cmd', 'uimode', 'night', system)
        time.sleep(3)
        assert f'Night mode: {system}' in adrv.adb('shell', 'cmd', 'uimode', 'night')
        assert baseline.open_track('Music', ALBUM, songs[0]['title'])
        assert adrv.tap('^Play Audio$')
        wait_track(songs[0]['title'])
        ensure_music_volume(run)
        key('PAUSE')
        labels = app_ui.labels()
        assert 'Next track' in labels and 'Previous track' in labels, labels
        assert state()['title'] == songs[0]['title']
        time.sleep(3)
        run.record(f'{theme} native track/seek controls fit portrait', True,
                   artifacts=[run.shot(f'm12-player-{theme}')])


def error_retry(run, songs):
    for song in songs:
        baseline.reset_progress(run.admin, song['id'])
    assert baseline.open_track('Music', ALBUM, songs[0]['title'])
    assert adrv.tap('^Play Audio$')
    wait_track(songs[0]['title'])
    ensure_music_volume(run)
    key('PAUSE')
    source = Path(run.state['media_root']) / songs[1]['path']
    hidden = source.with_suffix(source.suffix + '.m12-hidden')
    source.rename(hidden)
    try:
        assert adrv.tap('^Next track$')
        assert adrv.wait('(?i)could not|cannot|unable|connection|play.*failed', timeout=25), app_ui.labels()
        shot = run.shot('m12-target-unavailable')
        assert adrv.find('^Retry$'), 'failed track must offer Retry'
        run.record('unavailable next source exposes error and Retry without silently skipping', True,
                   json.dumps(app_ui.labels()), artifacts=[shot])
    finally:
        hidden.rename(source)
    assert adrv.tap('^Retry$')
    current, history = wait_track(songs[1]['title'])
    key('PAUSE')
    run.record('Retry recovers the same failed track', True,
               json.dumps(current), {'media_id': songs[1]['id'], 'sessions': history})


def spoken_seek(run, songs):
    # Add a deterministic spoken-source regression fixture only to this local
    # library. Its embedded title deliberately says music; source path wins.
    folder = Path(run.state['media_root']) / 'Music' / 'Audio Books'
    folder.mkdir(exist_ok=True)
    source = folder / 'chapter.mp3'
    subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-y',
                    '-f', 'lavfi', '-i', 'sine=frequency=330:duration=45',
                    '-metadata', 'title=A Music Named Chapter', '-c:a', 'libmp3lame', str(source)], check=True)
    run.admin.request('POST', '/api/v1/admin/rescan', {})
    deadline = time.monotonic() + 30
    media = None
    while time.monotonic() < deadline:
        rows = run.admin.request('GET', '/api/v1/media?limit=1000')
        media = next((row for row in rows if row['rel_path'].endswith('Audio Books/chapter.mp3')), None)
        if media:
            break
        time.sleep(.5)
    assert media, 'spoken regression fixture missing after scan'
    baseline.reset_progress(run.admin, media['id'])
    assert baseline.open_track('Music', 'Audio Books', 'A Music Named Chapter')
    assert adrv.tap('^Play Audio$')
    wait_track('A Music Named Chapter')
    ensure_music_volume(run)
    key('PAUSE')
    labels = app_ui.labels()
    assert 'Next track' not in labels and 'Previous track' not in labels, labels
    assert 'Skip forward 15 seconds' in labels and 'Skip back 15 seconds' in labels
    before = state()
    key('NEXT')
    time.sleep(.5)
    after = state()
    assert after['title'] == before['title']
    assert after['position_ms'] - before['position_ms'] >= 14000, (before, after)
    key('PREVIOUS')
    time.sleep(.5)
    backward = state()
    assert backward['title'] == before['title']
    assert after['position_ms'] - backward['position_ms'] >= 14000, (after, backward)
    run.record('audiobook source keeps native next/previous as 15-second seek', True,
               json.dumps({'before':before,'next':after,'previous':backward}),
               {'media_id':media['id']}, [run.shot('m12-spoken-seek')])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--scenario', choices=['all', 'touch', 'natural', 'native', 'themes', 'errors', 'spoken'], default='all')
    args = parser.parse_args(argv)
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    run = None
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('M12 terminated'))
    try:
        baseline.prepare_device(args, port)
        run = Run(args)
        run.failures = 0
        assert run.audio.available(), 'QEMU output file required for audible proof'
        if not app_ui.connect_and_login(
                run.base, run.auth['admin']['username'], run.auth['admin']['password'],
                lambda name, ok, detail='': run.record('sign-in: ' + name, ok, detail),
                run.shot, check_wrong_password=True):
            raise AssertionError('sign-in failed')
        run.record('sign-in: admin reaches the Library', True)
        songs = [run.track(f'Music/{ALBUM}/{name}') for name in
                 ['01 - First Light.mp3', '02 - Long Road Home.mp3', '03 - Harbour.flac']]
        steps = {'touch':touch_transport, 'natural':natural_completions,
                 'native':native_controls, 'themes':themes, 'errors':error_retry, 'spoken':spoken_seek}
        for name, step in steps.items():
            if args.scenario not in ('all', name):
                continue
            try:
                step(run, songs)
            except Exception as error:
                run.record(name, False, repr(error), artifacts=[run.shot(f'm12-failed-{name}')])
    except Exception as error:
        if run is None:
            raise
        window = run.evidence.path('m12-setup-failure-window.txt')
        window.write_text(adrv.adb('shell', 'dumpsys', 'window'))
        ui = run.evidence.path('m12-setup-failure-ui.xml')
        adrv.adb('shell', 'uiautomator', 'dump', '/sdcard/ui.xml')
        ui.write_text(adrv.adb('exec-out', 'cat', '/sdcard/ui.xml'))
        run.record('native setup', False, repr(error), artifacts=[
            run.shot('m12-setup-failure'), window, ui])
    finally:
        adrv.adb('shell', 'wm', 'dismiss-keyguard')
        baseline.teardown(run, None if args.host_alias else port)
    print(f'Evidence: {run.evidence.directory}', flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
