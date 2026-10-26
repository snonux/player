#!/usr/bin/env python3
"""M02 real native album catalog, ordered collection, source audio and ACLs.

Run only against music_fixture.py's disposable local server and the native
app on its running emulator. Uses actual touch/Back, media sessions, server
progress, real permission edits and QEMU audio; no playback mocks.
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
from fixture_server import Client


class Run(sequence.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M02', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def catalog():
    assert app_ui.go_home(), app_ui.labels()
    assert adrv.tap('^Open navigation menu$')
    assert adrv.tap('^Music · Albums$'), app_ui.labels()
    assert adrv.wait('^Music · Albums$', timeout=20)


def album(title='Night Transit', fresh=True):
    if fresh:
        catalog()
    node = app_ui.find_scrolling(r'(^| / )' + re.escape(title))
    if not node:
        # Dense catalogs (many Catalog Release rows) can outrun scroll probing;
        # Music search with the Albums scope still opens the same album detail.
        assert adrv.tap('^Search music$'), app_ui.labels()
        fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
        assert fields, app_ui.labels()
        adrv.tap_node(fields[0]); app_ui.clear_field(); adrv.type_text(title); time.sleep(1.5)
        assert adrv.tap('^Albums$'), app_ui.labels(); time.sleep(1)
        # Prefer the result row, never the search field that still holds the query.
        node = next((n for n in adrv.nodes()
                     if n['cls'] != 'EditText' and re.search(r'^' + re.escape(title) + r' / ', adrv.label(n) or '')), None)
    assert node, app_ui.labels()
    if node['b'][1] > 1500:
        # The persistent mini player covers catalog cards near the bottom.
        # Move this card into the unobscured center before tapping it.
        adrv.adb('shell', 'input', 'swipe', '540', '1600', '540', '1250', '300')
        time.sleep(.5)
        node = adrv.find(r'(^| / )' + re.escape(title))
        assert node and node['b'][1] <= 1500, app_ui.labels()
    adrv.tap_node(node)
    assert adrv.wait('^Play album$', timeout=20), app_ui.labels()


def labels_text():
    return ' | '.join(app_ui.labels())


def proof_audio(run, mark, name, songs):
    wav = run.audio.save(mark, run.evidence.path(name + '.wav'))
    analysis = baseline.audio_trace.analyze(wav)
    rate, samples = baseline.audio_trace.read_wav(wav)
    # Inspect three-second windows as well as whole audible runs: adjacent
    # distinct songs need not have a silence gap to prove both source tones.
    windows = []
    for offset in range(0, len(samples) - 3 * rate + 1, rate):
        result = baseline.audio_trace.analyze_samples(samples[offset:offset + 3 * rate], rate)
        for segment in result['segments']:
            segment['start_s'] += offset / rate
            segment['end_s'] += offset / rate
            windows.append(segment)
    analysis['source_windows'] = windows
    matches = []
    for song in songs:
        found = [s for s in windows
                 if baseline.tone_matches(s, song) and s['duration_s'] > 2]
        assert found, {'song': song, 'analysis': analysis}
        matches.append(found[0])
    return wav, analysis, matches


def catalog_design(run):
    visible = run.viewer.request('GET', '/api/v1/music/albums')
    own = next(a for a in visible if a['title'] == 'Night Transit')
    admin = run.admin.request('GET', '/api/v1/music/albums')
    hidden = next(a for a in admin if a['title'] == 'Hidden Album')
    assert own['song_count'] == 3 and 36 <= own['duration'] < 37, own
    assert next(a for a in admin if a['title'] == 'Night Transit')['song_count'] == 4
    assert not any(a['title'] == 'Hidden Album' for a in visible)
    status, denied = run.viewer.call('GET', '/api/v1/music/albums/' + hidden['album_key'])
    assert status == 404, (status, denied)
    assert 'unavailable' in denied['error'].lower(), denied
    detail = run.viewer.request('GET', '/api/v1/music/albums/' + own['album_key'])
    assert [s['music']['title'] for s in detail['songs']] == ['First Light', 'Long Road Home', 'Harbour']
    assert all(s['set_id'] == run.state['set_ids']['Music'] for s in detail['songs'])
    assert own['cover_media_id'] in [s['id'] for s in detail['songs']], own
    catalog()
    catalog_shots = []
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', system)
        time.sleep(2)
        catalog_shots.append(run.shot('m02-catalog-' + theme))
    album(fresh=False)
    assert adrv.wait('^Artwork for Night Transit$', timeout=20), app_ui.labels()
    text = labels_text()
    assert '3 songs' in text and 'Aurora Lane' in text and 'First Light' in text, text
    artifacts = list(catalog_shots)
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', system)
        time.sleep(2)
        artifacts.append(run.shot('m02-detail-' + theme))
    assert adrv.tap('^Aurora Lane$'), app_ui.labels()
    assert adrv.wait('^Aurora Lane$'), app_ui.labels()
    artifacts.append(run.shot('m02-linked-artist-albums-dark'))
    adrv.adb('shell', 'cmd', 'uimode', 'night', 'no')
    time.sleep(2)
    artifacts.append(run.shot('m02-linked-artist-albums-light'))
    app_ui.back()
    assert adrv.wait('^Play album$'), app_ui.labels()
    album('Synth Compilation')
    text = labels_text()
    for value in ['1-1', '1-2', '2-1', 'Neon Rain', 'Glass City', 'Afterglow']:
        assert value in text, (value, text)
    artifacts.append(run.shot('m02-disc-ordered-songs'))
    adrv.adb('shell', 'cmd', 'uimode', 'night', 'yes')
    time.sleep(2)
    album('Long Album ')
    artifacts.append(run.shot('m02-long-album-dark'))
    adrv.adb('shell', 'cmd', 'uimode', 'night', 'no')
    time.sleep(2)
    artifacts.append(run.shot('m02-long-album-light'))
    # Navigate from a real existing song's detail, using the visible action.
    assert baseline.open_track('Music', baseline.ALBUM, 'First Light')
    assert adrv.tap('^Open album$'), app_ui.labels()
    assert adrv.wait('^Play album$'), app_ui.labels()
    app_ui.back()
    assert adrv.wait('^Play Audio$'), app_ui.labels()
    artifacts.append(run.shot('m02-song-back-restores-detail'))
    run.record('library/song entry, actual ACL aggregate and artwork identity, totals/discs/linked artist/themes/Back', True,
               data={'viewer': own, 'album': detail, 'hidden_key_denied': hidden['album_key']}, artifacts=artifacts)
    notification_next(run)


def notification_next(run):
    songs = [run.track(f'Music/{baseline.ALBUM}/{name}') for name in
             ['01 - First Light.mp3', '02 - Long Road Home.mp3']]
    for song in songs:
        baseline.reset_progress(run.viewer, song['id'])
    album()
    assert adrv.tap('^Play album$')
    sequence.wait_track(songs[0]['title'])
    sequence.ensure_music_volume(run)
    sequence.key('PAUSE')
    app_ui.back()
    assert adrv.wait('^Play album$'), app_ui.labels()
    assert sequence.state()['title'] == songs[0]['title']
    adrv.adb('shell', 'cmd', 'statusbar', 'expand-notifications')
    time.sleep(1)
    try:
        node = adrv.find(r'(?i)^(?:skip to |skip )?next(?: track)?$')
        assert node, app_ui.labels()
        before = run.shot('m02-notification-next-control')
        mark = run.audio.mark()
        adrv.tap_node(node)
        current, sessions = sequence.wait_track(songs[1]['title'])
        time.sleep(3)
        sequence.key('PAUSE')
        wav, analysis, _ = proof_audio(run, mark, 'm02-album-notification-next', [songs[1]])
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            detail = run.viewer.request('GET', f'/api/v1/media/{songs[1]["id"]}')
            progress = detail.get('progress') or {}
            if not progress.get('finished') and 2 <= progress.get('position_seconds', 0) < 10:
                break
            time.sleep(.5)
        else:
            raise AssertionError(detail)
        assert detail['media']['id'] == songs[1]['id']
        run.record('actual notification Next keeps album mode, audible target and viewer progress', True,
                   data={'session': current, 'sessions': sessions, 'media_id': songs[1]['id'], 'progress': progress, 'audio': analysis},
                   artifacts=[before, wav, run.shot('m02-notification-next-target')])
    finally:
        adrv.adb('shell', 'cmd', 'statusbar', 'collapse')


def natural(run):
    songs = [run.track(f'Music/{baseline.ALBUM}/{name}') for name in
             ['01 - First Light.mp3', '02 - Long Road Home.mp3', '03 - Harbour.flac']]
    for song in songs:
        baseline.reset_progress(run.viewer, song['id'])
    run.viewer.request('POST', '/api/v1/progress', {'media_id': songs[0]['id'], 'position_seconds': 8})
    album()
    mark = run.audio.mark()
    assert adrv.tap('^Play album$')
    current, sessions = sequence.wait_track(songs[0]['title'])
    assert current['position_ms'] < 4000, current
    # No per-file Sets resume or queue wrap may leak into this collection.
    for _ in range(15):
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_VOLUME_UP')
    sequence.ensure_music_volume(run)
    # Leave the player route and album while playback continues in the handler.
    app_ui.back(2)
    assert adrv.wait('^Music · Albums$'), app_ui.labels()
    for song in songs[1:]:
        current, history = sequence.wait_track(song['title'], timeout=25)
        sessions.extend(history)
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline:
        current = sequence.state()
        sessions.append(current)
        if current['title'] == songs[-1]['title'] and current['state'] != 'PLAYING' and current['position_ms'] >= 11000:
            break
        time.sleep(.3)
    else:
        raise AssertionError(sessions[-20:])
    time.sleep(2)
    assert sequence.state()['title'] == songs[-1]['title'] and sequence.state()['state'] != 'PLAYING'
    wav, analysis, segments = proof_audio(run, mark, 'm02-natural-ordered-collection', songs)
    assert [s['start_s'] for s in segments] == sorted(s['start_s'] for s in segments), segments
    progress = [run.viewer.request('GET', f'/api/v1/media/{song["id"]}')['progress'] for song in songs]
    assert all(p['finished'] for p in progress), progress
    run.record('ordered natural original-source audio, short start0, Back/catalog independence and stopend', True,
               data={'sessions': sessions, 'audio': analysis, 'progress': progress}, artifacts=[wav, run.shot('m02-background-stopend')])
    # Replaying the ended collection source must create fresh progress, including
    # when the screen that originally started the album has been disposed.
    sequence.key('PLAY')
    sequence.wait_track(songs[-1]['title'])
    time.sleep(4)
    sequence.key('PAUSE')
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        saved = run.viewer.request('GET', f'/api/v1/media/{songs[-1]["id"]}').get('progress') or {}
        if not saved.get('finished') and saved.get('position_seconds', 0) >= 2:
            break
        time.sleep(.5)
    else:
        raise AssertionError(saved)
    run.record('background replay after natural album end saves fresh same-track progress', True, data=saved)


def errors(run):
    # A cached visible card is deliberately opened after real revocation. The
    # server's production ACL, not a synthetic response, supplies the error.
    album()
    app_ui.back()
    user_id = next(u['id'] for u in run.admin.request('GET', '/api/v1/admin/users') if u['username'] == run.auth['user']['username'])
    grant = {'set_id': run.state['set_ids']['Music'], 'user_id': user_id, 'role': 'viewer'}
    try:
        run.admin.request('DELETE', '/api/v1/admin/permissions', grant)
        assert run.viewer.request('GET', '/api/v1/music/albums') == []
        node = app_ui.find_scrolling('^Night Transit')
        assert node
        adrv.tap_node(node)
        assert adrv.wait('unavailable|permission', timeout=25), app_ui.labels()
        assert adrv.find('^Retry$')
        denied = run.shot('m02-real-acl-unavailable')
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', grant)
    assert adrv.tap('^Retry$')
    assert adrv.wait('^Play album$', timeout=25), app_ui.labels()
    run.record('real revoked album permission is visible and Retry recovers after restoration', True, artifacts=[denied, run.shot('m02-acl-retry-restored')])
    # Original stream failures retain the failed next track and native Retry.
    assert adrv.tap('^Play album$')
    sequence.wait_track('First Light')
    sequence.key('PAUSE')
    try:
        run.admin.request('DELETE', '/api/v1/admin/permissions', grant)
        assert adrv.tap('^Next track$'), app_ui.labels()
        assert adrv.wait('^Retry$', timeout=30), app_ui.labels()
        failed = run.shot('m02-next-track-permission-error')
        assert 'Long Road Home' in labels_text(), app_ui.labels()
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', grant)
    mark = run.audio.mark()
    assert adrv.tap('^Retry$')
    sequence.wait_track('Long Road Home')
    time.sleep(7)
    sequence.key('PAUSE')
    song = run.track(f'Music/{baseline.ALBUM}/02 - Long Road Home.mp3')
    wav, analysis, _ = proof_audio(run, mark, 'm02-native-real-permission-retry', [song])
    run.record('actual original-stream permission failure keeps target; Retry restores audible target', True,
               data={'audio': analysis, 'session': sequence.state()}, artifacts=[failed, wav])
    app_ui.back(2)
    if run.args.host_alias:
        raise AssertionError('Offline route test requires adb reverse; omit --host-alias')
    port = run.state['port']
    adrv.adb('reverse', '--remove', f'tcp:{port}')
    try:
        node = app_ui.find_scrolling('^Night Transit')
        assert node
        adrv.tap_node(node)
        assert adrv.wait('connection|reach the server', timeout=60), app_ui.labels()
        offline = run.shot('m02-offline-album-error')
    finally:
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
    assert adrv.tap('^Retry$')
    assert adrv.wait('^Play album$', timeout=25)
    run.record('offline album request has readable Retry and recovers after route restoration', True, artifacts=[offline, run.shot('m02-online-retry-restored')])
    adrv.adb('shell', 'cmd', 'uimode', 'night', 'yes')
    time.sleep(2)
    album('Long Album ')
    mode = adrv.adb('shell', 'cmd', 'uimode', 'night')
    assert 'Night mode: yes' in mode, mode
    dark_state = run.evidence.path('m02-long-album-dark-system.txt')
    dark_state.write_text(mode + '\n' + labels_text())
    run.record('long album dark appearance explicitly settled and readable', True,
               artifacts=[dark_state, run.shot('m02-long-album-dark-settled')])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--scenario', choices=['all', 'catalog', 'natural', 'errors'], default='all')
    args = parser.parse_args(argv)
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    run = None
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('M02 terminated'))
    try:
        baseline.prepare_device(args, port)
        run = Run(args)
        run.args = args
        run.failures = 0
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        assert run.audio.available(), 'QEMU WAV output required'
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
                                        lambda name, ok, detail='': run.record('sign-in: '+name, ok, detail), run.shot)
        for name, step in [('catalog', catalog_design), ('natural', natural), ('errors', errors)]:
            if args.scenario not in ('all', name):
                continue
            try:
                step(run)
            except Exception as error:
                trace = run.evidence.path('m02-failed-' + name + '-traceback.txt')
                trace.write_text(traceback.format_exc())
                ui = run.evidence.path('m02-failed-' + name + '-ui.xml')
                adrv.adb('shell', 'uiautomator', 'dump', '/sdcard/ui.xml')
                ui.write_text(adrv.adb('exec-out', 'cat', '/sdcard/ui.xml'))
                run.record(name, False, traceback.format_exc(), artifacts=[run.shot('m02-failed-'+name), trace, ui])
    except Exception as error:
        if not run:
            raise
        run.record('native setup', False, repr(error), artifacts=[run.shot('m02-setup-failure')])
    finally:
        if run:
            baseline.teardown(run, None if args.host_alias else port)
    print(f'Evidence: {run.evidence.directory}', flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
