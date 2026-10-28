#!/usr/bin/env python3
"""M15 actual native collection detail/selection actions and queue ordering.

Runs only against the disposable Music fixture, installed app and API34 emulator.
Rooted-emulator queue preferences are read for authoritative IDs/occurrences;
all mutations use visible native controls. Original source output uses QEMU WAV.
Leaves the app/server/emulator and fixture state intact for root review.
"""
import argparse
import json
from pathlib import Path
import re
import sys
import time
import traceback
import xml.etree.ElementTree as ET

import adrv
import app_ui
import music_e2e as baseline
import music_albums_e2e as albums
import music_artists_e2e as artists
import music_playlists_e2e as playlists
import music_queue_e2e as queue_ui
import music_queue_recovery_e2e as recovery
import music_sequence_e2e as sequence
from fixture_server import Client, PlayerServer, answers, stop_pid
from music_fixture import save_state, started_process


ACTIVE_RUN = None


class Run(albums.Run):
    def shot(self, name):
        return super().shot(name + '-' + str(time.time_ns()))

    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M15', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def catalog(kind):
    {'album': albums.catalog, 'artist': artists.catalog, 'playlist': playlists.catalog}[kind]()


def open_item(name):
    tap(r'(^| / )' + re.escape(name) + r'($| / )')


def swipe_collection(up):
    # The artist Role field and selection toolbar sit above the scroll view.
    # Both gesture endpoints must remain inside the actual scrollable surface.
    adrv.nodes()
    xml = adrv.adb('exec-out', 'cat', '/sdcard/ui.xml')
    bounds = [list(map(int, re.findall(r'\d+', node.get('bounds', ''))))
              for node in ET.fromstring(xml).iter('node')
              if node.get('scrollable') == 'true']
    bounds = [rect for rect in bounds if len(rect) == 4 and rect[3] - rect[1] > 200]
    assert bounds, 'No actual native scrollable viewport'
    left, top, right, bottom = max(bounds, key=lambda rect: (rect[2]-rect[0])*(rect[3]-rect[1]))
    x = (left + right) // 2
    start, end = top + (bottom-top)*3//4, top + (bottom-top)//4
    if not up:
        start, end = end, start
    if ACTIVE_RUN is not None:
        with ACTIVE_RUN.evidence.path('m15-native-scroll-targets.jsonl').open('a') as file:
            file.write(json.dumps({'time_ns': time.time_ns(), 'viewport': [left, top, right, bottom],
                                   'up': up, 'start': [x, start], 'end': [x, end]}) + '\n')
    adrv.adb('shell', 'input', 'swipe', str(x), str(start), str(x), str(end), '400')
    time.sleep(1.2)


def tap(pattern):
    # Search the full native scroll surface rather than stopping after three
    # upward swipes. Use the actual clickable semantic descendant, never the
    # center of a merged ancestor or an unrelated nonclickable text node.
    physical = re.search(r'Physical size: (\d+)x(\d+)', adrv.adb('shell', 'wm', 'size'))
    assert physical
    height = int(physical.group(2))
    for move in (None, *([True] * 7), *([False] * 14)):
        if move is not None:
            swipe_collection(up=move)
        candidates = [node for node in adrv.nodes() if re.search(pattern, adrv.label(node))
                      and node['click'] and 0 <= node['b'][1] < node['b'][3] <= height
                      and node['b'][2] > node['b'][0]]
        if not candidates:
            continue
        node = min(candidates, key=lambda item: (item['b'][2]-item['b'][0])*(item['b'][3]-item['b'][1]))
        time.sleep(.35)
        stable = next((item for item in adrv.nodes() if item['click']
                       and adrv.label(item) == adrv.label(node) and item['b'] == node['b']), None)
        if stable is None:
            continue
        if ACTIVE_RUN is not None:
            path = ACTIVE_RUN.evidence.path('m15-native-touch-targets.jsonl')
            with path.open('a') as file:
                file.write(json.dumps({'time_ns': time.time_ns(), 'pattern': pattern, 'target': stable}) + '\n')
        adrv.tap_node(stable)
        return
    raise AssertionError(('No stable clickable native target', pattern, app_ui.labels()))


def ids(saved):
    return [entry['mediaId'] for entry in saved['entries']]


def saved_order(run, expected):
    saved = recovery.wait_saved(run, lambda value: ids(value) == expected)
    assert len({entry['id'] for entry in saved['entries']}) == len(expected), saved
    queue_ui.queue()
    assert adrv.wait('^Now Playing$'), app_ui.labels()
    assert any(label.startswith('Playing from:') for label in app_ui.labels()), app_ui.labels()
    shot = run.shot('m15-queue-' + str(len(expected)))
    app_ui.back()
    return saved, shot


def enqueue(name):
    tap(r'^More options$')
    assert adrv.tap('^' + re.escape(name) + '$'), app_ui.labels()
    assert adrv.wait(r'^Added \d+ songs?$', timeout=30), app_ui.labels()


def audible(run, song_id, trigger, name):
    song = next(run.track(t['path']) for t in run.manifest['tracks']
                if run.state['media_ids'][t['path']] == song_id)
    trigger()
    sequence.wait_track(song['title'])
    # Set the actual media route, then capture a fresh resume so slow UI/volume
    # dialogs cannot consume a short track before the audio measurement.
    for _ in range(15):
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_VOLUME_UP')
    sequence.key('PAUSE')
    time.sleep(5)
    sequence.ensure_music_volume(run)
    mark = run.audio.mark()
    sequence.key('PLAY')
    sequence.wait_track(song['title'])
    time.sleep(4)
    sequence.key('PAUSE')
    wav = run.audio.save(mark, run.evidence.path(name + '-' + str(time.time_ns()) + '.wav'))
    analysis = baseline.audio_trace.analyze(wav)
    segment = baseline.main_segment(analysis)
    assert segment and segment['duration_s'] > 2 and baseline.tone_matches(segment, song), analysis
    return {'media_id': song_id, 'session': sequence.state(), 'segment': segment, 'wav': str(wav)}


def artist_ids(run, key):
    artist = run.viewer.request('GET', '/api/v1/music/artists/' + key)
    result = []
    for album in artist['albums']:
        detail = run.viewer.request('GET', '/api/v1/music/albums/' + album['album_key'])
        for song in detail['songs']:
            if song['id'] not in result and any(p['artist_key'] == key for p in detail['song_credits'].get(str(song['id']), [])):
                result.append(song['id'])
    return result


def setup(run):
    releases = run.viewer.request('GET', '/api/v1/music/albums')
    selected_albums = [a for a in releases if a['title'] in ('Night Transit', 'Synth Compilation')]
    assert len(selected_albums) == 2, selected_albums
    details = {a['album_key']: run.viewer.request('GET', '/api/v1/music/albums/' + a['album_key']) for a in selected_albums}
    night = next(a for a in selected_albums if a['title'] == 'Night Transit')
    songs = details[night['album_key']]['songs']
    created = []
    for name, order in [('M15 Alpha native', [songs[2]['id'], songs[0]['id'], songs[2]['id']]), ('M15 Beta native', [songs[1]['id']])]:
        p = next((p for p in run.viewer.request('GET', '/api/v1/playlists') if p['name'] == name), None)
        if p is None:
            p = run.viewer.request('POST', '/api/v1/playlists', {'name': name, 'comment': 'M15 isolated native acceptance'})
            p = run.viewer.request('POST', '/api/v1/playlists/' + p['playlist_id'] + '/songs', {'action': 'add', 'media_ids': order})
        p = run.viewer.request('GET', '/api/v1/playlists/' + p['playlist_id'])
        assert [song['id'] for song in p['songs']] == order, p
        created.append(p)
    people = run.viewer.request('GET', '/api/v1/music/artists')
    selected_people = [p for p in people if p['name'] in ('Aurora Lane', 'Neon Coast')]
    assert len(selected_people) == 2, selected_people
    return night, selected_albums, details, selected_people, created


def geometry_sanity(run, data):
    catalog('playlist')
    open_item(data[4][0]['name'])
    assert adrv.wait('^Play playlist$'), app_ui.labels()
    nodes = adrv.nodes()
    matches = [n for n in nodes if adrv.label(n) == 'More options' and n['click']]
    adjacent = [n for n in nodes if adrv.label(n) == 'Add songs' and n['click']]
    assert matches and adjacent, nodes
    node = min(matches, key=lambda n: (n['b'][2]-n['b'][0])*(n['b'][3]-n['b'][1]))
    menu, other = node['b'], adjacent[0]['b']
    density = int(re.search(r'Physical density: (\d+)', adrv.adb('shell', 'wm', 'density')).group(1)) / 160
    assert 48*density-1 <= menu[3]-menu[1] <= 80*density, node
    overlap = menu[0] < other[2] and other[0] < menu[2] and menu[1] < other[3] and other[1] < menu[3]
    assert not overlap, ('More options overlaps Add songs', node, adjacent)
    before = run.shot('m15-final-menu-compact')
    tap(r'^More options$')
    assert adrv.wait('^Play next$') and adrv.find('^Add to queue$'), app_ui.labels()
    opened = run.shot('m15-final-menu-open')
    app_ui.back()
    run.record('final APK native semantic menu is compact48dp, excludes Add songs and opens correct actions', True,
               data={'target': node, 'adjacent': adjacent, 'pixel_ratio': density}, artifacts=[before, opened])


def detail_journey(run, data):
    night, _, details, people, lists = data
    person = next(p for p in people if p['name'] == 'Aurora Lane')
    targets = [('album', night['title'], [s['id'] for s in details[night['album_key']]['songs']]),
               ('artist', person['name'], artist_ids(run, person['artist_key'])),
               ('playlist', lists[0]['name'], [s['id'] for s in lists[0]['songs']])]
    for kind, name, order in targets:
        if run.kind and kind != run.kind:
            continue
        catalog(kind)
        open_item(name)
        assert adrv.wait('^Play ' + kind + '$'), app_ui.labels()
        proof = audible(run, order[0], lambda: tap('^Play ' + kind + '$'), 'm15-native-detail-' + kind)
        base, shot = saved_order(run, order)
        before = sequence.state()
        enqueue('Play next')
        next_order = [order[0], *order, *order[1:]]
        inserted, _ = saved_order(run, next_order)
        after = sequence.state()
        assert after['title'] == before['title'] and after['state'] == 'PAUSED', (before, after)
        assert abs(after['position_ms'] - before['position_ms']) < 1000, (before, after)
        enqueue('Add to queue')
        appended, _ = saved_order(run, next_order + order)
        queue_ui.queue()
        assert adrv.tap('^Undo$'), app_ui.labels()
        app_ui.back()
        restored, _ = saved_order(run, next_order)
        run.record('native ' + kind + ' detail Play/next/append whole-batch Undo and audible original source', True,
                   data={'proof': proof, 'base': base, 'inserted': inserted, 'appended': appended, 'undo': restored}, artifacts=[shot, proof['wav']])


def selection_journey(run, data):
    _, releases, details, people, lists = data
    targets = [('album', releases, 'title', lambda a: [s['id'] for s in details[a['album_key']]['songs']]),
               ('artist', people, 'name', lambda p: artist_ids(run, p['artist_key'])),
               ('playlist', lists, 'name', lambda p: [s['id'] for s in p['songs']])]
    for kind, collections, name_key, expand in targets:
        if run.kind and kind != run.kind:
            continue
        catalog(kind)
        tap('^Select ' + kind + 's$')
        for item in reversed(collections):
            tap('^Select ' + re.escape(item[name_key]) + '$')
        assert adrv.wait('^2 selected$'), app_ui.labels()
        order = [song for item in collections for song in expand(item)]
        proof = audible(run, order[0], lambda: tap('^Play selected$'), 'm15-native-selected-' + kind)
        base, shot = saved_order(run, order)
        enqueue('Play next')
        next_order = [order[0], *order, *order[1:]]
        saved_order(run, next_order)
        enqueue('Add to queue')
        appended, _ = saved_order(run, next_order + order)
        artifacts = [shot, proof['wav']]
        if kind == 'album':
            for theme, value in [('light', 'no'), ('dark', 'yes')]:
                adrv.adb('shell', 'cmd', 'uimode', 'night', value)
                time.sleep(2)
                artifacts.append(run.shot('m15-native-selection-' + theme))
        app_ui.back()
        assert adrv.wait('^Select ' + kind + 's$'), app_ui.labels()
        assert not adrv.find('^Play selected$'), app_ui.labels()
        run.record('native ' + kind + ' reverse collection selection catalog order all actions and Back cancellation', True,
                   data={'proof': proof, 'base': base, 'appended': appended}, artifacts=artifacts)


def boundaries(run, data):
    night, _, details, _, _ = data
    catalog('album')
    open_item(night['title'])
    assert adrv.wait('^Play album$'), app_ui.labels()
    before = recovery.snapshot(run)
    if not before or not before['entries']:
        tap('^Play album$')
        sequence.wait_track(details[night['album_key']]['songs'][0]['music']['title'])
        sequence.key('PAUSE')
        before = recovery.wait_saved(run, lambda value: bool(value['entries']))
    port = run.state['port']
    previous = dict(run.state['server'])
    assert stop_pid(previous['pid'], previous['binary'], start=previous['start']), 'Owned server identity mismatch'
    assert answers(run.state['origin'], '/healthz') is None, 'Owned server still answers'
    try:
        adrv.adb('reverse', '--remove', f'tcp:{port}')
        tap(r'^More options$')
        assert adrv.tap('^Add to queue$')
        assert adrv.wait('Could not load songs', timeout=45), app_ui.labels()
        assert recovery.snapshot(run)['entries'] == before['entries']
        offline = run.shot('m15-native-offline-retained')
    finally:
        server = PlayerServer(previous['binary'], run.fixture_dir, port)
        run.state['server'].update(started_process(server.start(detached=True)))
        run.state['server']['restarts'] = run.state['server'].get('restarts', 0) + 1
        save_state(run.fixture_dir, run.state)
        if run.ownership_path:
            ownership = Path(run.ownership_path)
            owned = json.loads(ownership.read_text())
            owned['server'] = run.state['server']
            ownership.write_text(json.dumps(owned, indent=2))
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
        run.record('same final binary/database/media restarted after verified owned offline cutoff', True,
                   data={'previous': previous, 'current': run.state['server']})
    grant = {'set_id': run.state['set_ids']['Music'], 'user_id': run.viewer_id, 'role': 'viewer'}
    try:
        run.admin.request('DELETE', '/api/v1/admin/permissions', grant)
        tap(r'^More options$')
        assert adrv.tap('^Add to queue$')
        assert adrv.wait('Could not load songs', timeout=30), app_ui.labels()
        assert recovery.snapshot(run)['entries'] == before['entries']
        denied = run.shot('m15-native-permission-retained')
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', grant)
    enqueue('Add to queue')
    expected = ids(before) + [s['id'] for s in details[night['album_key']]['songs']]
    recovered, _ = saved_order(run, expected)
    run.record('native real offline and revoked permission preserve existing queue then Retry appends once', True,
               data={'before': before, 'after': recovered}, artifacts=[offline, denied])


def recovery_journey(run, data):
    night, _, details, _, _ = data
    saved = recovery.snapshot(run)
    if not saved or not saved['entries']:
        catalog('album'); open_item(night['title'])
        tap('^Play album$')
        first = details[night['album_key']]['songs'][0]
        sequence.wait_track(first['music']['title']); sequence.key('PAUSE')
        saved = recovery.wait_saved(run, lambda value: bool(value['entries']))
    sequence.key('PAUSE')
    time.sleep(1)
    saved = recovery.snapshot(run)
    recovery.restart(run)
    queue_ui.queue()
    assert adrv.wait('^Resume queue$', timeout=25), app_ui.labels()
    recovery.same_queue(saved, recovery.wait_saved(run, lambda value: value['entries'] == saved['entries']))
    assert sequence.state()['state'] != 'PLAYING', sequence.state()
    restart_shot = run.shot('m15-native-recovered-batch')
    app_ui.back()
    recovery.login_other(run, run.auth['admin'])
    queue_ui.queue()
    assert adrv.wait('Queue is empty', timeout=25), app_ui.labels()
    empty = run.shot('m15-native-other-account-empty')
    app_ui.back()
    recovery.login_other(run, run.auth['user'])
    queue_ui.queue()
    assert adrv.wait('^Resume queue$', timeout=25), app_ui.labels()
    recovery.same_queue(saved, recovery.wait_saved(run, lambda value: value['entries'] == saved['entries']))
    assert sequence.state()['state'] != 'PLAYING', sequence.state()
    assert adrv.tap('^Resume queue$'), app_ui.labels()
    current = saved['entries'][saved['index']]['mediaId']
    song = next(t for t in run.manifest['tracks'] if run.state['media_ids'][t['path']] == current)
    sequence.wait_track(song['title']); sequence.key('PAUSE')
    run.record('native collection batch survives actual force-stop/unlock and account roundtrip; paused until explicit Resume', True,
               data={'saved': saved, 'resumed': recovery.snapshot(run), 'session': sequence.state()}, artifacts=[restart_shot, empty])
    app_ui.back()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--scenario', choices=['all', 'core', 'detail', 'selection', 'boundaries', 'recovery'], default='all')
    parser.add_argument('--kind', choices=['album', 'artist', 'playlist'])
    parser.add_argument('--ownership-path')
    parser.add_argument('--resume-device', action='store_true')
    args = parser.parse_args(argv)
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    assert 'uid=0' in adrv.adb('shell', 'id'), 'Rooted disposable emulator required'
    baseline.prepare_device(args, port)
    run = Run(args)
    run.failures = 0
    run.ownership_path = args.ownership_path
    run.kind = args.kind
    global ACTIVE_RUN
    ACTIVE_RUN = run
    run.viewer = Client(run.state['origin']).login(**run.auth['user'])
    run.viewer_id = next(u['id'] for u in run.admin.request('GET', '/api/v1/admin/users') if u['username'] == run.auth['user']['username'])
    assert run.audio.available(), 'QEMU WAV capture required'
    if args.resume_device:
        recovery.restart(run)
    else:
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
            lambda name, ok, detail='': run.record('sign-in: ' + name, ok, detail), run.shot)
    data = setup(run)
    if args.scenario in ('all', 'core'):
        geometry_sanity(run, data)
    for name, step in [('detail', detail_journey), ('selection', selection_journey), ('boundaries', boundaries), ('recovery', recovery_journey)]:
        if args.scenario == 'core':
            if name == 'recovery':
                continue
        elif args.scenario not in ('all', name):
            continue
        try:
            step(run, data)
        except Exception:
            trace = run.evidence.path('m15-native-failed-' + name + '-' + str(time.time_ns()) + '.txt')
            trace.write_text(traceback.format_exc())
            run.record(name, False, traceback.format_exc(), artifacts=[trace, run.shot('m15-native-failed-' + name)])
    print('Evidence: ' + str(run.evidence.directory), flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
