#!/usr/bin/env python3
"""M04 actual native disc groups, compilation credits and audible boundaries."""
import argparse
import json
from pathlib import Path
import re
import sys
import subprocess
import time
import traceback

import adrv
import app_ui
import music_e2e as baseline
import music_albums_e2e as albums
import music_sequence_e2e as sequence
from music_release_e2e import visible, fields
from music_discs import ALBUM, COMPILATION, SUBTITLE, set_art, retag, RETAG_PATH
from music_queue_recovery_e2e import restart
from fixture_server import Client, PlayerServer, owns_process, stop_pid, write_private_json


class Run(albums.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M04', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} {detail}', flush=True)
        if result == 'fail':
            self.failures += 1


def artwork_placeholder_pattern():
    """Recognize the exact placeholder token in standalone or merged semantics."""
    return r'(^| / )No album artwork( /|$)'


def broken_disc_pattern():
    """Couple missing-image semantics to the actual second disc and full subtitle."""
    return '^' + re.escape('No album artwork / Disc 2 — ' + SUBTITLE.strip()) + '$'


def song_row_pattern(number, title, duration='0:18'):
    """Match the complete native ListTile semantics, including its track identity."""
    return '^' + re.escape(f'{number} / {title} / {duration}') + '$'


def song_row(folder, number):
    return visible(song_row_pattern(number, f'M04 {folder} {number}'))


def tap_song(folder, number):
    row = song_row(folder, number)
    left, top, right, bottom = row['b']
    # Touch the leading number column of this actual ListTile, keeping clear
    # of its nested artist links and trailing menu buttons.
    leading = dict(row, b=[left, top, left + max(1, (right - left) // 12), bottom])
    adrv.tap_node(leading)


def album(title):
    """Open an actual M04 card across the augmented catalog's full scroll range."""
    albums.catalog()
    card = visible(r'(^| / )' + re.escape(title) + r'( /|$)')
    adrv.tap_node(card)
    assert adrv.wait('^Play album$', timeout=25), app_ui.labels()


def core(run):
    targets = [(ALBUM, 'Album'), (COMPILATION, 'Compilation')]
    choice = getattr(run, 'core_album', 'all')
    if choice != 'all':
        targets = [target for target in targets if target[1].lower() == ('album' if choice == 'ordinary' else choice)]
    for title, folder in targets:
        catalog = run.viewer.request('GET', '/api/v1/music/albums')
        matches = [a for a in catalog if a['title'] == title]
        assert len(matches) == 1, matches
        if folder == 'Compilation':
            assert matches[0]['artist'] == 'Various Artists' and matches[0]['song_count'] == 3, matches
        detail = run.viewer.request('GET', '/api/v1/music/albums/' + matches[0]['album_key'])
        assert len(detail['discs']) == 2 and len(detail['songs']) == 3, detail
        assert [s['music']['title'] for s in detail['songs']] == [f'M04 {folder} 1-1', f'M04 {folder} 1-2', f'M04 {folder} 2-1']
        if folder == 'Compilation':
            assert [song['music']['artists'] for song in detail['songs']] == [['M04 A / B', 'M04 東京'], ['M04 Guest'], ['M04 A / B', 'M04 東京']], detail
        assert 'M04 Hidden' not in json.dumps(detail)
        album(title)
        for label in ['Disc 1 — Original', 'Disc 2 — ' + SUBTITLE.strip()]:
            visible('^' + re.escape(label) + '$')
        for number in ('1-1', '1-2', '2-1'):
            song_row(folder, number)
        for disc in detail['discs']:
            visible('^Artwork for Disc ' + str(disc['number']))
        for name in (['M04 Guest'] if folder == 'Compilation' else []) + ['M04 A / B', 'M04 東京']:
            adrv.tap_node(visible('^' + re.escape(name) + '$'))
            assert adrv.wait('^' + re.escape(name) + '$'), app_ui.labels()
            assert app_ui.find_scrolling('^' + re.escape(title)), app_ui.labels()
            app_ui.back()
            assert adrv.wait('^Play album$'), app_ui.labels()
        for theme, mode in [('light', 'no'), ('dark', 'yes')]:
            adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
            time.sleep(2)
            visible('^Disc 2 — ')
            run.record(title + ' disc subtitle/art/credits ' + theme, True,
                       artifacts=[run.shot('m04-' + folder + '-' + theme)])
        # Heading insertion must not shift the original global playback index.
        tap_song(folder, '2-1')
        sequence.wait_track(f'M04 {folder} 2-1')
        mark = run.audio.mark()
        time.sleep(5)
        sequence.key('PAUSE')
        song = run.track(f'Music/M04/{folder}/Disc 2/1.flac')
        wav, analysis, _ = albums.proof_audio(run, mark, 'm04-' + folder + '-global-index', [song])
        run.record(title + ' disc2 global index source audio', True, data=analysis, artifacts=[wav])
        app_ui.back()
        tap_song(folder, '1-2')
        sequence.wait_track(f'M04 {folder} 1-2')
        mark = run.audio.mark()
        sequence.wait_track(f'M04 {folder} 2-1', timeout=35)
        time.sleep(5)
        sequence.key('PAUSE')
        last = run.track(f'Music/M04/{folder}/Disc 1/2.flac')
        wav, analysis, matches = albums.proof_audio(run, mark, 'm04-' + folder + '-disc-boundary', [last, song])
        assert matches[0]['start_s'] < matches[1]['start_s'], matches
        run.record(title + ' natural ordered audible disc boundary', True, data=analysis, artifacts=[wav])
        # Resume and prove collection end does not wrap.
        sequence.key('PLAY')
        deadline = time.monotonic() + 30
        while sequence.state()['state'] == 'PLAYING' and time.monotonic() < deadline:
            time.sleep(.5)
        assert sequence.state()['state'] != 'PLAYING' and sequence.state()['title'] == song['title'], sequence.state()
        run.record(title + ' collection end stays stopped without wrapping', True, data=sequence.state())
        app_ui.back()
    album('M04 Missing Art')
    visible(artwork_placeholder_pattern())
    run.record('missing artwork uses native placeholder', True, artifacts=[run.shot('m04-missing-art')])
    # Cold-start unlock is explicit and does not silently reuse authentication.
    restart(run)
    album(COMPILATION)
    visible('^Disc 2 — ')
    run.record('cold app restart preserves compilation and subtitles', True, artifacts=[run.shot('m04-restart')])


def errors(run):
    set_art(run.fixture_dir, 'broken')
    try:
        cover_id = run.state['media_ids']['Music/M04/Album/Disc 2/1.flac']
        # Verify the actual authenticated endpoint now serves invalid image
        # bytes; opening a new album screen creates a new uncached Dio read.
        with run.viewer.opener.open(run.state['origin'] + f'/api/v1/media/{cover_id}/thumbnail', timeout=10) as response:
            assert response.status == 200
            assert response.read() == b'Not an image: M04 controlled broken cover'
        album(ALBUM)
        visible(broken_disc_pattern())
        run.record('actual broken disc thumbnail uses native placeholder', True, artifacts=[run.shot('m04-broken-art')])
    finally:
        set_art(run.fixture_dir, 'initial')
    album(ALBUM)
    app_ui.back()
    user = next(u['id'] for u in run.admin.request('GET', '/api/admin/users') if u['username'] == run.auth['user']['username'])
    grant = {'set_id': run.state['set_ids']['Music'], 'user_id': user, 'role': 'viewer'}
    try:
        run.admin.request('DELETE', '/api/admin/permissions', grant)
        adrv.tap_node(visible('^' + ALBUM))
        assert adrv.wait('unavailable|permission', timeout=30), app_ui.labels()
        run.record('real permission denial hides disc data', True, artifacts=[run.shot('m04-permission')])
    finally:
        run.admin.request('POST', '/api/admin/permissions', grant)
    assert adrv.tap('^Retry$') and adrv.wait('^Play album$', timeout=25)
    visible('^Disc 2 — ')
    app_ui.back()
    # Close existing native keepalive as well as removing adb reverse.
    state_path = run.fixture_dir / 'state.json'
    state = json.loads(state_path.read_text())
    server = state['server']
    assert owns_process(server['pid'], server['binary'], start=server.get('start'))
    stop_pid(server['pid'], server['binary'], start=server.get('start'))
    adrv.adb('reverse', '--remove', f'tcp:{state["port"]}')
    try:
        adrv.tap_node(visible('^' + ALBUM))
        assert adrv.wait('connection|reach the server', timeout=60), app_ui.labels()
        run.record('actual native offline album error', True, artifacts=[run.shot('m04-offline')])
    finally:
        new = PlayerServer(server['binary'], run.fixture_dir, state['port'])
        pid = new.start(detached=True)
        from music_fixture import started_process
        state['server'].update(started_process(pid))
        write_private_json(state_path, state)
        adrv.adb('reverse', f'tcp:{state["port"]}', f'tcp:{state["port"]}')
    assert adrv.tap('^Retry$') and adrv.wait('^Play album$', timeout=25)
    visible('^Disc 2 — ')
    run.record('same-server offline/permission Retry restores discs', True, artifacts=[run.shot('m04-retry')])


def state_roundtrips(run):
    item = run.track(RETAG_PATH)
    def scan(state):
        retag(run.fixture_dir, state)
        run.admin.request('POST', '/api/admin/rescan', {})
        deadline = time.monotonic() + 40
        while run.admin.request('GET', '/api/admin/scan-progress')['running']:
            assert time.monotonic() < deadline, 'Scan timeout'
            time.sleep(.25)
    def details():
        assert baseline.open_track('Music', 'M04/Album/Disc 2', item['title'])
        adrv.tap_node(visible('^Music details(?:,|$)'))
    run.viewer.request('POST', '/api/progress', {'media_id': item['id'], 'position_seconds': 4})
    run.viewer.request('POST', f'/api/media/{item["id"]}/favorite')
    before = run.viewer.request('GET', f'/api/media/{item["id"]}')
    try:
        scan('changed')
        details()
        fields({'Disc subtitle': 'Changed disc 東京', 'Compilation': 'Yes'})
        app_ui.back()
        scan('broken')
        retained = run.viewer.request('GET', f'/api/media/{item["id"]}')
        assert retained['media']['music']['disc_subtitle'] == 'Changed disc 東京' and retained['media']['music']['compilation']
        scan('cleared')
        after = run.viewer.request('GET', f'/api/media/{item["id"]}')
        assert not after['media']['music'].get('disc_subtitle') and not after['media']['music'].get('compilation')
        assert after['media']['id'] == before['media']['id'] and after['favorite'] == before['favorite'] and after['progress'] == before['progress'], (before, after)
        album(ALBUM)
        visible('^Disc 2$')
        scan('initial')
        subprocess.run([sys.executable, str(baseline.SUPPORT / 'music_fixture.py'), 'restart', str(run.fixture_dir)], check=True)
        album(ALBUM)
        visible('^Disc 2 — ')
        run.record('M04 changed/cleared/failed probe and same-DB server restart preserve identity/state', True,
                   data={'before': before, 'after': after}, artifacts=[run.shot('m04-state-restored')])
    finally:
        scan('initial')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--resume-device', action='store_true', help='Retain app data and device for combined batch')
    parser.add_argument('--core-album', choices=['all', 'ordinary', 'compilation', 'none'], default='all', help='Select a core album for bounded continuation; default covers both')
    parser.add_argument('--scenario', choices=['all', 'core', 'errors', 'state'], default='all')
    args = parser.parse_args(argv)
    if args.host_alias:
        parser.error('Offline checks require adb reverse')
    run = None
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    try:
        baseline.prepare_device(args, port)
        run = Run(args)
        run.failures = 0
        run.core_album = args.core_album
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        assert run.audio.available(), 'Actual emulator output WAV required'
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
                                        lambda name, ok, detail='': run.record('sign-in: ' + name, ok, detail), run.shot)
        for name, step in [('core', core), ('errors', errors), ('state', state_roundtrips)]:
            if args.scenario in ('all', name):
                step(run)
    except Exception:
        if run is None:
            raise
        trace = run.evidence.path('m04-failure.txt')
        trace.write_text(traceback.format_exc())
        run.record('native M04 journey', False, traceback.format_exc(), artifacts=[run.shot('m04-failure'), trace])
    finally:
        if run and not args.resume_device:
            baseline.teardown(run, port)
    return 1 if run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
