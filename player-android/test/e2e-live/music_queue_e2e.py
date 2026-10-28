#!/usr/bin/env python3
"""M13 actual native queue edits using touch/Back, source tones and per-ID progress.

Requires a fresh disposable music_fixture.py server and the freshly built native
APK on a running emulator. All evidence/media remain outside the repository.
"""
import argparse
import json
from pathlib import Path
import re
import signal
import sys
import time
import traceback

import adrv
import app_ui
import music_e2e as baseline
import music_sequence_e2e as sequence
import music_albums_e2e as albums
from fixture_server import Client


class Run(albums.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M13', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def queue():
    assert adrv.tap('^Now Playing queue$'), app_ui.labels()
    assert adrv.wait('^Now Playing$'), app_ui.labels()


def action(title, name):
    assert adrv.tap('^Queue actions for ' + re.escape(title) + '$'), app_ui.labels()
    assert adrv.tap('^' + re.escape(name) + '$'), app_ui.labels()
    assert adrv.wait('^Now Playing$'), app_ui.labels()


def jump(title):
    action(title, 'Play now')
    sequence.wait_track(title)


def row_order(titles):
    result = []
    for node in adrv.nodes():
        label = adrv.label(node)
        if label.startswith('Queue actions for'):
            continue
        for title in titles:
            if title in label and node['click']:
                result.append((node['b'][1], title, label))
    result.sort()
    # Merged native semantics may expose both the row and its parent node;
    # de-duplicate only adjacent copies of the same row label.
    found = []
    for _, title, _ in result:
        if not found or found[-1] != title:
            found.append(title)
    return found


def start_paused(run, songs):
    for song in songs:
        baseline.reset_progress(run.viewer, song['id'])
    albums.album()
    assert adrv.tap('^Play album$')
    sequence.wait_track(songs[0]['title'])
    # Shell stream-set is ineffective on this emulator; actual active-route
    # volume keys reach maximum, verified strictly before any audio proof.
    for _ in range(15):
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_VOLUME_UP')
    sequence.ensure_music_volume(run)
    sequence.key('PAUSE')
    assert sequence.state()['state'] == 'PAUSED', sequence.state()
    # Android's volume dialog can cover the row overflow at the same x/y.
    # Pause first so this environment-settling wait cannot consume a song.
    time.sleep(5)
    music = adrv.adb('shell', 'dumpsys', 'audio').split('- STREAM_MUSIC:', 1)[1].split('\n\n', 1)[0]
    assert '2 (speaker): 15' in music and 'Muted: false' in music, music


def output(run, song, name, trigger):
    baseline.reset_progress(run.viewer, song['id'])
    mark = run.audio.mark()
    trigger()
    sequence.wait_track(song['title'])
    time.sleep(7)
    sequence.key('PAUSE')
    wav = run.audio.save(mark, run.evidence.path(name + '.wav'))
    analysis = baseline.audio_trace.analyze(wav)
    segment = baseline.main_segment(analysis)
    assert segment and segment['duration_s'] > 2 and baseline.tone_matches(segment, song), analysis
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        detail = run.viewer.request('GET', f'/api/v1/media/{song["id"]}')
        progress = detail.get('progress') or {}
        if progress.get('position_seconds', 0) >= 2 or progress.get('finished'):
            break
        time.sleep(.5)
    assert detail['media']['id'] == song['id'], detail
    assert progress.get('user_id') == run.viewer_id and progress.get('media_id') == song['id'], progress
    assert progress.get('position_seconds', 0) >= 2 or progress.get('finished'), detail
    return {'media_id': song['id'], 'viewer_id': run.viewer_id, 'progress': progress, 'audio': analysis}, wav


def editing(run, songs):
    a, b, c = songs
    start_paused(run, songs)
    queue()
    original = row_order([s['title'] for s in songs])
    assert original == [s['title'] for s in songs], original
    action(b['title'], 'Move down')
    assert row_order([s['title'] for s in songs]) == [a['title'], c['title'], b['title']]
    action(b['title'], 'Move up')
    assert row_order([s['title'] for s in songs]) == [a['title'], b['title'], c['title']]
    action(b['title'], 'Move down')
    # Closing with actual Back leaves the native player route and edited queue.
    app_ui.back()
    assert adrv.find('^Next track$'), app_ui.labels()
    proof, wav = output(run, c, 'm13-native-edited-next', lambda: adrv.tap('^Next track$'))
    run.record('touch Next loads edited-order source with real tone and fresh target progress', True,
               data=proof, artifacts=[wav, run.shot('m13-edited-next')])
    adrv.adb('shell', 'cmd', 'statusbar', 'expand-notifications')
    time.sleep(1)
    try:
        for direction, song in [('next', b), ('previous', c)]:
            node = adrv.find(r'(?i)^(?:skip to |skip )?' + direction + r'(?: track)?$')
            assert node, app_ui.labels()
            shot = run.shot('m13-notification-' + direction)
            proof, wav = output(run, song, 'm13-native-edited-notification-' + direction, lambda: adrv.tap_node(node))
            run.record('actual notification ' + direction + ' obeys edited order with fresh ID/tone', True,
                       data=proof, artifacts=[shot, wav])
    finally:
        adrv.adb('shell', 'cmd', 'statusbar', 'collapse')
    queue()
    proof, wav = output(run, b, 'm13-native-jump', lambda: jump(b['title']))
    run.record('queue Play now jumps to actual source without replacing order', True,
               data=proof, artifacts=[wav])
    action(a['title'], 'Remove')
    assert row_order([s['title'] for s in songs]) == [c['title'], b['title']]
    assert adrv.tap('^More options$'), app_ui.labels(); assert adrv.tap('^Undo$')
    assert row_order([s['title'] for s in songs]) == [a['title'], c['title'], b['title']]
    # The source is already paused at its measured position; removal must flush
    # B's identity, stop it, and Undo must not start it again.
    action(b['title'], 'Remove')
    assert sequence.state()['state'] != 'PLAYING'
    saved = run.viewer.request('GET', f'/api/v1/media/{b["id"]}')['progress']
    assert saved['position_seconds'] >= 2 and saved['user_id'] == run.viewer_id and saved['media_id'] == b['id'], saved
    assert adrv.tap('^More options$'), app_ui.labels(); assert adrv.tap('^Undo$')
    assert sequence.state()['state'] != 'PLAYING'
    assert adrv.tap('^More options$'), app_ui.labels(); assert adrv.tap('^Clear queue$')
    assert adrv.wait('Queue is empty'), app_ui.labels()
    assert sequence.state()['state'] != 'PLAYING'
    assert adrv.tap('^More options$'), app_ui.labels(); assert adrv.tap('^Undo$')
    assert row_order([s['title'] for s in songs]) == [a['title'], c['title'], b['title']]
    assert sequence.state()['state'] != 'PLAYING'
    proof, wav = output(run, c, 'm13-native-clear-undo-resume', lambda: jump(c['title']))
    run.record('current removal and Clear Undo stay stopped, explicit jump reclaims fresh source', True,
               data={'proof': proof, 'removed_progress': saved}, artifacts=[wav, run.shot('m13-clear-undo-resumed')])
    app_ui.back()  # sheet
    assert adrv.wait('^Harbour$'), app_ui.labels()
    assert not adrv.find('^Retry$'), app_ui.labels()
    app_ui.back(2)  # player and album -> catalog, audio handler retains queue
    queue()
    assert row_order([s['title'] for s in songs]) == [a['title'], c['title'], b['title']]
    run.record('Back/library queue uses handler-owned retained edited entries', True, artifacts=[run.shot('m13-library-queue')])
    app_ui.back()


def natural(run, songs):
    a, b, c = songs
    start_paused(run, songs)
    queue()
    action(b['title'], 'Move down')
    for song in songs:
        baseline.reset_progress(run.viewer, song['id'])
    mark = run.audio.mark()
    jump(a['title'])
    app_ui.back(3)  # queue, player, album -> catalog
    assert adrv.wait('^Music · Albums$'), app_ui.labels()
    sessions = []
    for song in [c, b]:
        current, history = sequence.wait_track(song['title'], timeout=25)
        sessions.extend(history)
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline:
        current = sequence.state()
        sessions.append(current)
        if current['title'] == b['title'] and current['state'] != 'PLAYING' and current['position_ms'] >= 11000:
            break
        time.sleep(.3)
    else:
        raise AssertionError(sessions[-20:])
    wav, analysis, segments = albums.proof_audio(run, mark, 'm13-native-edited-natural-order', [a, c, b])
    assert [s['start_s'] for s in segments] == sorted(s['start_s'] for s in segments), segments
    progress = [run.viewer.request('GET', f'/api/v1/media/{s["id"]}')['progress'] for s in [a, c, b]]
    assert all(p['finished'] and p['user_id'] == run.viewer_id and p['media_id'] == song['id'] for p, song in zip(progress, [a, c, b])), progress
    run.record('edited natural A C B tones/IDs/finished and stopend while browsing catalog', True,
               data={'audio': analysis, 'sessions': sessions, 'progress': progress}, artifacts=[wav, run.shot('m13-native-natural-ended')])


def retained(run, songs):
    start_paused(run, songs)
    app_ui.back(2)
    source = run.track('Music/Various Artists - Synth Compilation/Disc 1/01 - Neon Rain.m4a')
    following = run.track('Music/Various Artists - Synth Compilation/Disc 1/02 - Glass City.opus')
    assert baseline.open_track('Music', 'Various Artists - Synth Compilation/Disc 1', source['title'])
    assert adrv.tap('^Play Audio$')
    sequence.wait_track(source['title'])
    sequence.key('PAUSE')
    queue()
    before = sequence.state()
    action(songs[0]['title'], 'Remove')
    assert adrv.tap('^More options$'), app_ui.labels(); assert adrv.tap('^Undo$')
    assert adrv.tap('^More options$'), app_ui.labels(); assert adrv.tap('^Clear queue$')
    assert adrv.tap('^More options$'), app_ui.labels(); assert adrv.tap('^Undo$')
    after = sequence.state()
    assert after['title'] == before['title'] and abs(after['position_ms'] - before['position_ms']) < 1000, (before, after)
    app_ui.back()
    proof, wav = output(run, following, 'm13-native-retained-sets-next', lambda: adrv.tap('^Next track$'))
    run.record('inactive queue edits cannot stop/seek unrelated Sets; Sets Next remains browse order', True,
               data={'before': before, 'after': after, 'proof': proof}, artifacts=[wav])
    queue()
    proof, wav = output(run, songs[0], 'm13-native-retained-explicit-reclaim', lambda: jump(songs[0]['title']))
    run.record('retained album explicit jump reclaims actual playback from Sets', True, data=proof, artifacts=[wav])
    app_ui.back(3)


def errors(run, songs):
    start_paused(run, songs)
    queue()
    uid = next(u['id'] for u in run.admin.request('GET', '/api/v1/admin/users') if u['username'] == run.auth['user']['username'])
    grant = {'set_id': run.state['set_ids']['Music'], 'user_id': uid, 'role': 'viewer'}
    try:
        run.admin.request('DELETE', '/api/v1/admin/permissions', grant)
        jump_title = songs[1]['title']
        action(jump_title, 'Play now')
        assert adrv.wait('Could not play track'), app_ui.labels()
        failed = run.shot('m13-native-real-permission-error')
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', grant)
    proof, wav = output(run, songs[1], 'm13-native-permission-restored', lambda: jump(songs[1]['title']))
    run.record('real denied source remains in queue; visible tap-to-retry recovers same audible ID', True,
               data=proof, artifacts=[failed, wav])
    port = run.state['port']
    adrv.adb('reverse', '--remove', f'tcp:{port}')
    try:
        action(songs[2]['title'], 'Remove')
        assert adrv.tap('^More options$'), app_ui.labels(); assert adrv.tap('^Undo$')
        assert row_order([s['title'] for s in songs]) == [s['title'] for s in songs]
        run.record('offline queue edits and Undo require no network', True, artifacts=[run.shot('m13-native-offline-edit')])
    finally:
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
    app_ui.back(3)


def themes(run, songs):
    start_paused(run, songs)
    app_ui.back(2)
    adrv.adb('shell', 'cmd', 'uimode', 'night', 'no')
    time.sleep(2)
    queue()
    run.record('light queue current/paused rows and 48dp touch controls', True, artifacts=[run.shot('m13-native-light-queue')])
    app_ui.back()
    adrv.adb('shell', 'cmd', 'uimode', 'night', 'yes')
    time.sleep(2)
    queue()
    run.record('dark queue current/paused rows and touch controls', True, artifacts=[run.shot('m13-native-dark-queue')])
    app_ui.back()
    albums.album('Long Album ')
    long = next(a for a in run.viewer.request('GET', '/api/v1/music/albums') if a['title'].startswith('Long Album '))
    detail = run.viewer.request('GET', '/api/v1/music/albums/' + long['album_key'])
    title = detail['songs'][0]['music']['title']
    assert adrv.tap('^Play album$')
    sequence.wait_track(title)
    sequence.key('PAUSE')
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', system)
        time.sleep(2)
        queue()
        assert adrv.find('^Queue actions for ' + re.escape(title) + '$'), app_ui.labels()
        run.record('long title/album/source text native queue ' + theme + ' remains actionable', True,
                   artifacts=[run.shot('m13-native-long-queue-' + theme)])
        app_ui.back()
    app_ui.back(2)


def large(run, _songs):
    catalog = run.viewer.request('GET', '/api/v1/music/albums')
    scale = next(a for a in catalog if a['title'] == 'Queue Scale')
    assert scale['song_count'] == 1001, scale
    detail = run.viewer.request('GET', '/api/v1/music/albums/' + scale['album_key'])
    albums.album('Queue Scale')
    assert adrv.tap('^Play album$')
    sequence.wait_track('Queue 0001')
    sequence.key('PAUSE')
    queue()
    dimensions = [int(n) for n in re.findall(r'\d+', adrv.adb('shell', 'wm', 'size').split(':')[-1])]
    width, height = dimensions[-2:]
    target = None
    for _ in range(120):
        target = adrv.find(r'(^| / )Queue 1001($| / )')
        if target:
            break
        for _ in range(5):
            adrv.adb('shell', 'input', 'swipe', str(width // 2), str(int(height * .8)), str(width // 2), str(int(height * .24)), '80')
        time.sleep(.2)
    assert target, app_ui.labels()
    shot = run.shot('m13-native-1001-last-row')
    song = {'id': detail['songs'][-1]['id'], 'title': 'Queue 1001', 'tone_hz': [440]}
    proof, wav = output(run, song, 'm13-native-1001-last-source', lambda: adrv.tap_node(target))
    run.record('actual 1001-entry lazy queue scroll reaches last row and plays actual ID/tone', True,
               data={'proof': proof, 'count': len(detail['songs'])}, artifacts=[shot, wav])
    app_ui.back(3)


def close_contrast(run, songs):
    from PIL import Image
    start_paused(run, songs)
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', system)
        time.sleep(2)
        queue()  # explicitly opened from the player's white-ink AppBar
        node = adrv.find('^Close queue$')
        assert node, app_ui.labels()
        shot = run.shot('m13-native-player-close-' + theme)
        image = Image.open(shot).convert('RGB')
        left, top, right, bottom = node['b']
        background = image.getpixel((left + 2, top + 2))
        def luminance(rgb):
            channels = [v / 255 for v in rgb]
            channels = [v / 12.92 if v <= .04045 else ((v + .055) / 1.055) ** 2.4 for v in channels]
            return .2126 * channels[0] + .7152 * channels[1] + .0722 * channels[2]
        base = luminance(background)
        contrasted = 0
        for rgb in image.crop((left, top, right, bottom)).getdata():
            value = luminance(rgb)
            if (max(base, value) + .05) / (min(base, value) + .05) >= 3:
                contrasted += 1
        assert contrasted > 20, {'bounds': node['b'], 'background': background, 'contrasted_pixels': contrasted}
        adrv.tap_node(node)
        assert not adrv.find('^Now Playing$'), app_ui.labels()
        assert adrv.find('^Now Playing queue$'), app_ui.labels()
        run.record('player-opened ' + theme + ' queue Close glyph visible and touch closes sheet', True,
                   data={'bounds': node['b'], 'background_rgb': background, 'contrast3_pixels': contrasted}, artifacts=[shot])
    app_ui.back(2)


def scope(run, songs):
    # Begin with a visibly nonempty queue. Force-stop kills this process but
    # leaves remembered server/login data intact; M16 persistence is outside this task.
    start_paused(run, songs)
    queue()
    assert row_order([s['title'] for s in songs]) == [s['title'] for s in songs]
    app_ui.back()
    adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
    app_ui.launch()
    assert adrv.wait('^Choose a server$', timeout=40), app_ui.labels()
    saved = adrv.find(re.escape(run.base) + '$')
    assert saved and not adrv.find('^Library$'), app_ui.labels()
    adrv.tap_node(saved)
    assert adrv.wait('^Welcome back$', timeout=25), app_ui.labels()
    time.sleep(2)
    fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
    assert adrv.find('^Unlock saved login with fingerprint$'), app_ui.labels()
    assert any(run.auth['user']['username'] in (n['text'], n['desc']) for n in fields), fields
    assert not adrv.find('^Library$'), app_ui.labels()
    adrv.tap_node(fields[-1])
    assert app_ui.sign_in(run.auth['user']['password']), app_ui.labels()
    assert adrv.wait('^Library$', timeout=30), app_ui.labels()
    queue()
    assert adrv.wait('Queue is empty'), app_ui.labels()
    run.record('actual force-stop/relaunch remembers locked login; explicit sign-in has empty queue', True,
               artifacts=[run.shot('m13-native-cold-relaunch-empty')])
    app_ui.back()
    account(run, songs)


def account(run, songs):
    start_paused(run, songs)
    app_ui.back(2)
    assert app_ui.go_home()
    queue()
    assert row_order([s['title'] for s in songs]) == [s['title'] for s in songs]
    app_ui.back()
    assert adrv.tap('^Settings$'), app_ui.labels()
    logout = adrv.wait('^Log Out$')
    assert logout, app_ui.labels()
    adrv.tap_node(logout)
    assert adrv.wait('^Welcome back$', timeout=25), app_ui.labels()
    # Deliberately no connect_and_login()/pm clear here: this is an actual
    # account boundary in the running app, exercising its auth-clear hooks.
    fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
    assert len(fields) == 2, app_ui.labels()
    adrv.tap_node(fields[0]); app_ui.clear_field(); adrv.type_text(run.auth['admin']['username'])
    adrv.tap_node(fields[1]); app_ui.clear_field()
    assert app_ui.sign_in(run.auth['admin']['password']), app_ui.labels()
    assert adrv.wait('^Restricted$'), app_ui.labels()
    queue()
    assert adrv.wait('Queue is empty'), app_ui.labels()
    undo = adrv.find('^Undo$')
    assert undo and not undo['click'], undo
    assert not adrv.find('^Playing from:'), app_ui.labels()
    assert not adrv.find(r'^Queue paused\. Choose a track to play\.$'), app_ui.labels()
    assert not any(adrv.find('^' + re.escape(song['title']) + '$') for song in songs), app_ui.labels()
    run.record('actual logout from nonempty viewer queue and admin login clears entries/Undo', True,
               data={'previous_viewer_id': run.viewer_id, 'new_account': run.auth['admin']['username']},
               artifacts=[run.shot('m13-native-account-boundary-empty')])
    app_ui.back()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--scenario', choices=['all', 'editing', 'natural', 'retained', 'errors', 'themes', 'large', 'scope', 'account', 'close', 'remaining'], default='all')
    args = parser.parse_args(argv)
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    run = None
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('M13 terminated'))
    try:
        assert not args.host_alias, 'offline acceptance requires adb reverse'
        baseline.prepare_device(args, port)
        run = Run(args)
        run.args = args
        run.failures = 0
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        run.viewer_id = next(user['id'] for user in run.admin.request('GET', '/api/v1/admin/users') if user['username'] == run.auth['user']['username'])
        assert run.audio.available(), 'QEMU WAV output required'
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
                                        lambda name, ok, detail='': run.record('sign-in: '+name, ok, detail), run.shot)
        songs = [run.track(f'Music/{baseline.ALBUM}/{name}') for name in
                 ['01 - First Light.mp3', '02 - Long Road Home.mp3', '03 - Harbour.flac']]
        steps = [('editing', editing), ('natural', natural), ('retained', retained), ('errors', errors), ('themes', themes), ('large', large), ('close', close_contrast), ('scope', scope)]
        if args.scenario == 'account':
            steps = [('account', account)]
        for name, step in steps:
            if args.scenario == 'remaining':
                if name not in ('editing', 'natural', 'retained', 'close', 'scope'):
                    continue
            elif args.scenario not in ('all', name):
                continue
            try:
                step(run, songs)
            except Exception:
                trace = run.evidence.path('m13-failed-' + name + '-traceback.txt')
                trace.write_text(traceback.format_exc())
                ui = run.evidence.path('m13-failed-' + name + '-ui.xml')
                adrv.adb('shell', 'uiautomator', 'dump', '/sdcard/ui.xml')
                ui.write_text(adrv.adb('exec-out', 'cat', '/sdcard/ui.xml'))
                run.record(name, False, traceback.format_exc(), artifacts=[run.shot('m13-failed-'+name), trace, ui])
    finally:
        if run:
            baseline.teardown(run, None if args.host_alias else port)
    print(f'Evidence: {run.evidence.directory}', flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
