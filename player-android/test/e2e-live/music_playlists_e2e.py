#!/usr/bin/env python3
"""M26 real native persistent playlist CRUD, totals, decoded art and source audio."""
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
import music_albums_e2e as albums
from fixture_server import Client, PlayerServer, answers, stop_pid
from music_fixture import save_state, started_process


class Run(albums.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M26', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def catalog():
    assert app_ui.go_home(), app_ui.labels()
    assert adrv.tap('^Open navigation menu$')
    assert adrv.tap('^Music · Playlists$'), app_ui.labels()
    assert adrv.wait('^Music · Playlists$', timeout=20), app_ui.labels()


def field(index, value):
    fields = [node for node in adrv.nodes() if node['cls'] == 'EditText']
    assert len(fields) > index, fields
    adrv.tap_node(fields[index])
    app_ui.clear_field(100)
    adrv.type_text(value)


def tap_visible(pattern):
    physical = re.search(r'Physical size: (\d+)x(\d+)', adrv.adb('shell', 'wm', 'size'))
    assert physical
    height = int(physical.group(2))
    for move in (None, True, True, False, False, False):
        if move is not None:
            app_ui.swipe(up=move)
        node = adrv.find(pattern)
        if node and 0 <= node['b'][1] < node['b'][3] <= height and node['b'][2] > node['b'][0]:
            adrv.tap_node(node)
            return
    raise AssertionError((pattern, app_ui.labels()))


def wait_detail(name):
    assert adrv.wait('^' + re.escape(name) + '$', timeout=20), app_ui.labels()
    assert adrv.wait('^Add music$', timeout=20), app_ui.labels()


def restart(run):
    adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
    app_ui.launch()
    assert adrv.wait('^Choose a server$', timeout=40), app_ui.labels()
    saved = adrv.find(re.escape(run.base) + '$')
    assert saved, app_ui.labels()
    adrv.tap_node(saved)
    assert adrv.wait('^Welcome back$', timeout=25), app_ui.labels()
    fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
    adrv.tap_node(fields[-1])
    assert app_ui.sign_in(run.auth['user']['password']), app_ui.labels()


def journey(run, resume_id=None):
    name = 'M26 Road Trip ' + str(int(time.time()))
    renamed = name + ' renamed'
    album = next(a for a in run.viewer.request('GET', '/api/v1/music/albums') if a['title'] == 'Night Transit')
    if resume_id is None:
        catalog()
        assert adrv.tap('^Create playlist$', nth=1), app_ui.labels()
        assert adrv.wait('.*', cls='EditText'), app_ui.labels()
        fields = [node for node in adrv.nodes() if node['cls'] == 'EditText']
        assert len(fields) == 2, fields
        assert any('1–200 characters' in label for label in app_ui.labels()), app_ui.labels()
        assert any('Up to 2000 characters' in label for label in app_ui.labels()), app_ui.labels()
        field(0, name)
        field(1, 'r/h/l/? plain comment')
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_BACK')
        assert adrv.tap('^Create playlist$', cls='Button'), app_ui.labels()
        wait_detail(name)
        assert adrv.wait(r'^This playlist is empty\.$'), app_ui.labels()
        p = next(p for p in run.viewer.request('GET', '/api/v1/playlists') if p['name'] == name)
        pid = p['playlist_id']
        assert p['song_count'] == 0 and p['comment'] == 'r/h/l/? plain comment', p
        assert adrv.tap('^Add music$')
        tap_visible('^Night Transit')
        wait_detail(name)
        p = run.viewer.request('GET', '/api/v1/playlists/' + pid)
        assert p['song_count'] == album['song_count'] and abs(p['duration'] - album['duration']) < .001, p
        assert p['cover_media_id'] == album['cover_media_id'], p
        # Authenticated Image.memory only exposes the artwork semantics after bytes
        # have loaded and decoded. The raw screenshot is retained for visual review.
        assert adrv.wait('^Artwork for ' + re.escape(name) + '$', timeout=20), app_ui.labels()
        seconds = round(p['duration'])
        total_label = f"{p['song_count']} songs · {seconds // 60}:{seconds % 60:02}"
        assert any(total_label in label for label in app_ui.labels()), app_ui.labels()
        run.record('native create empty Add music saves ordered songs totals and rendered artwork', True,
                   data={'playlist': p, 'album': album}, artifacts=[run.shot('m26-populated')])
    else:
        p = run.viewer.request('GET', '/api/v1/playlists/' + resume_id)
        pid = p['playlist_id']
        name = p['name']
        renamed = name + ' renamed'
        assert p['song_count'] == album['song_count'], p
        catalog()
        tap_visible(re.escape(name))
        wait_detail(name)
    tap_visible('^Edit playlist$')
    field(0, renamed)
    field(1, 'Saved after cold client restart')
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_BACK')
    assert adrv.tap('^Save changes$'), app_ui.labels()
    wait_detail(renamed)
    restart(run)
    catalog()
    tap_visible(re.escape(renamed))
    wait_detail(renamed)
    assert adrv.wait('^Saved after cold client restart$'), app_ui.labels()
    assert adrv.wait('^Artwork for ' + re.escape(renamed) + '$'), app_ui.labels()
    persisted = run.viewer.request('GET', '/api/v1/playlists/' + pid)
    assert persisted['comment'] == 'Saved after cold client restart' and persisted['entries'] == p['entries'], persisted
    for method in ('GET', 'DELETE'):
        assert run.admin.call(method, '/api/v1/playlists/' + pid)[0] == 404
    assert not any(x['playlist_id'] == pid for x in run.admin.request('GET', '/api/v1/playlists'))
    assert app_ui.connect_and_login(run.base, run.auth['admin']['username'], run.auth['admin']['password'],
                                    lambda name, ok, detail='': run.record('other-account sign-in: ' + name, ok, detail), run.shot)
    catalog()
    assert adrv.wait('No playlists yet', timeout=20), app_ui.labels()
    assert not any(renamed in label for label in app_ui.labels()), app_ui.labels()
    other_shot = run.shot('m26-other-account')
    assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
                                    lambda name, ok, detail='': run.record('owner return sign-in: ' + name, ok, detail), run.shot)
    catalog()
    tap_visible(re.escape(renamed))
    wait_detail(renamed)
    assert adrv.wait('^Saved after cold client restart$'), app_ui.labels()
    artifacts = [other_shot]
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', system)
        time.sleep(2)
        artifacts.append(run.shot('m26-detail-' + theme))
    run.record('native force-stop/relaunch name comment membership totals artwork and account scope persist', True,
               data={'playlist': persisted}, artifacts=artifacts)
    mark = run.audio.mark()
    tap_visible('^Play playlist$')
    deadline = time.monotonic() + 20
    current = None
    while time.monotonic() < deadline:
        current = baseline.session_state()
        if current['title'] == 'First Light' and current['state'] == 'PLAYING':
            break
        time.sleep(.25)
    assert current and current['title'] == 'First Light' and current['state'] == 'PLAYING', current
    pause_for_inspection('First Light')
    verify_music_volume(run)
    mark = run.audio.mark()
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PLAY')
    wait_playing('First Light')
    time.sleep(4)
    source = next(run.track(t['path']) for t in run.manifest['tracks'] if t['title'] == 'First Light')
    wav, analysis, matches = albums.proof_audio(run, mark, 'm26-source', [source])
    run.record('playlist starts actual music queue with original source output', True,
               data={'session': current, 'analysis': analysis, 'matches': matches}, artifacts=[wav])
    app_ui.back()
    wait_detail(renamed)
    duplicate_occurrence(run, pid, renamed)
    finish(run, pid, renamed)


def finish(run, pid, renamed):
    tap_visible('^Delete playlist$')
    assert adrv.tap('^Cancel$'), app_ui.labels()
    assert run.viewer.call('GET', '/api/v1/playlists/' + pid)[0] == 200
    tap_visible('^Edit playlist$')
    port = run.state['port']
    before_offline = run.viewer.request('GET', '/api/v1/playlists/' + pid)
    previous = dict(run.state['server'])
    stopped = stop_pid(previous['pid'], previous['binary'], start=previous.get('start'))
    assert stopped and answers(run.state['origin'], '/healthz') is None, 'Owned server cutoff failed'
    adrv.adb('reverse', '--remove', f'tcp:{port}')
    try:
        field(1, 'Retained unsaved offline comment')
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_BACK')
        assert adrv.tap('^Save changes$')
        assert adrv.wait('Could not save playlist', timeout=30), app_ui.labels()
        assert adrv.find('Retained unsaved offline comment', cls='EditText'), app_ui.labels()
        run.record('actual offline save keeps unsaved comment form', True, artifacts=[run.shot('m26-offline-form')])
    finally:
        server = PlayerServer(previous['binary'], run.fixture_dir, port)
        run.state['server'].update(started_process(server.start(detached=True)))
        run.state['server']['restarts'] = run.state['server'].get('restarts', 0) + 1
        save_state(run.fixture_dir, run.state)
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
        run.record('owned fixture server restarted on same database after closing transport', True,
                   data={'previous': previous, 'current': run.state['server']})
    after_offline = run.viewer.request('GET', '/api/v1/playlists/' + pid)
    assert after_offline == before_offline, (before_offline, after_offline)
    assert after_offline['comment'] == 'Saved after cold client restart'
    run.record('offline failed save leaves authoritative name comment and ordered membership unchanged after server restart', True, data={'playlist': after_offline})
    assert adrv.tap('^Cancel$')
    tap_visible('^Delete playlist$')
    assert adrv.tap('^Delete playlist$', cls='Button'), app_ui.labels()
    assert adrv.wait('^Music · Playlists$', timeout=20), app_ui.labels()
    assert run.viewer.call('GET', '/api/v1/playlists/' + pid)[0] == 404
    app_ui.back()
    assert adrv.wait('^Library$'), app_ui.labels()
    run.record('delete cancel then confirmed deletion and predictable native Back', True, artifacts=[run.shot('m26-deleted')])


def occurrence_in_queue(number, title):
    # The player adds personal actions after asynchronous source setup. Wait
    # for its real transport controls and two stable toolbar layouts before
    # using coordinates from the Android accessibility dump.
    assert adrv.wait('^Next track$'), app_ui.labels()
    previous = None
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        node = adrv.find('^Now Playing queue$')
        if node and node['b'] == previous:
            adrv.tap_node(node)
            break
        previous = node['b'] if node else None
        time.sleep(.3)
    else:
        raise AssertionError(('queue toolbar did not settle', app_ui.labels()))
    assert adrv.wait('^Now Playing$'), app_ui.labels()
    labels = app_ui.labels()
    assert any(title in label and 'Now' in label and re.search(r'(^| / )' + str(number) + r'($| / )', label)
               for label in labels), (number, title, labels)
    app_ui.back()
    return labels


def wait_playing(title):
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        current = baseline.session_state()
        if current['title'] == title and current['state'] == 'PLAYING':
            return current
        time.sleep(.25)
    raise AssertionError(('not playing', title, baseline.session_state()))


def pause_for_inspection(title):
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        current = baseline.session_state()
        if current['title'] == title and current['state'] == 'PAUSED':
            return
        time.sleep(.25)
    raise AssertionError(('not paused for queue inspection', title, baseline.session_state()))


def verify_music_volume(run):
    for attempt in range(3):
        adrv.adb('shell', 'input', 'keyevent', *['KEYCODE_VOLUME_UP'] * 15)
        time.sleep(.5)
        raw = adrv.adb('shell', 'dumpsys', 'audio')
        section = raw.split('- STREAM_MUSIC:', 1)[-1].split('- STREAM_', 1)[0]
        maximum = re.search(r'Max: (\d+)', section)
        speaker = re.search(r'2 \(speaker\): (\d+)', section)
        if maximum and speaker and maximum.group(1) == speaker.group(1):
            path = run.evidence.path('m26-media-volume.txt')
            path.write_text(section)
            return
        time.sleep(.5)
    raise AssertionError(('speaker music volume did not reach max', section))


def duplicate_occurrence(run, pid, name, already_populated=False):
    if not already_populated:
        tap_visible('^Add music$')
        tap_visible('^Night Transit')
        wait_detail(name)
    saved = run.viewer.request('GET', '/api/v1/playlists/' + pid)
    assert saved['song_count'] == 6 and len(saved['entries']) == 6, saved
    assert [e['media_id'] for e in saved['entries'][:3]] == [e['media_id'] for e in saved['entries'][3:]], saved
    assert len({e['entry_id'] for e in saved['entries']}) == 6
    # Scroll to the actual final song row in the playlist, then tap that
    # occurrence. Do not use the queue's jump action to choose it.
    for _ in range(3):
        app_ui.swipe()
    hits = [n for n in adrv.nodes() if n['click'] and re.search(r'(^| / )Harbour($| / )', adrv.label(n))]
    assert hits, app_ui.labels()
    mark = run.audio.mark()
    adrv.tap_node(hits[-1])
    wait_playing('Harbour')
    pause_for_inspection('Harbour')
    selected = occurrence_in_queue(6, 'Harbour')
    assert adrv.tap('^Previous track$'), app_ui.labels()
    wait_playing('Long Road Home')
    pause_for_inspection('Long Road Home')
    occurrence_in_queue(5, 'Long Road Home')
    assert adrv.tap('^Next track$'), app_ui.labels()
    wait_playing('Harbour')
    pause_for_inspection('Harbour')
    occurrence_in_queue(6, 'Harbour')
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PLAY')
    wait_playing('Harbour')
    time.sleep(4)
    source = next(run.track(t['path']) for t in run.manifest['tracks'] if t['title'] == 'Harbour')
    wav, analysis, matches = albums.proof_audio(run, mark, 'm26-later-duplicate-source', [source])
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline and baseline.session_state()['state'] == 'PLAYING':
        time.sleep(.5)
    terminal = baseline.session_state()
    assert terminal['state'] != 'PLAYING' and terminal['title'] == 'Harbour', terminal
    # Completion at the last occurrence must not wrap back to the first block.
    assert adrv.tap('^Now Playing queue$')
    assert adrv.wait('^Now Playing$')
    ended = app_ui.labels()
    assert any('Harbour' in label and ('Now' in label or 'Selected' in label) and re.search(r'(^| / )6($| / )', label)
               for label in ended), ended
    run.record('later duplicate playlist occurrence starts queue index6 previous5 next6 and ends without wrap with original source', True,
               data={'playlist': saved, 'selected': selected, 'ended': ended, 'analysis': analysis, 'matches': matches},
               artifacts=[wav, run.shot('m26-later-duplicate')])
    app_ui.back()
    app_ui.back()
    wait_detail(name)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--resume-stage', choices=['edit','remaining'], default='edit')
    parser.add_argument('--resume-playlist', help='Resume the failed journey from its already populated playlist')
    args = parser.parse_args(argv)
    assert not args.host_alias, 'Offline acceptance requires isolated adb reverse transport'
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    run = None
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('M26 terminated'))
    try:
        baseline.prepare_device(args, port)
        run = Run(args)
        run.failures = 0
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        assert run.audio.available(), 'QEMU WAV output required'
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
                                        lambda name, ok, detail='': run.record('sign-in: ' + name, ok, detail), run.shot)
        try:
            if args.resume_stage == 'remaining':
                assert args.resume_playlist
                p = run.viewer.request('GET','/api/v1/playlists/' + args.resume_playlist)
                catalog()
                tap_visible(re.escape(p['name']))
                wait_detail(p['name'])
                duplicate_occurrence(run, p['playlist_id'], p['name'], already_populated=True)
                finish(run, p['playlist_id'], p['name'])
            else:
                journey(run, args.resume_playlist)
        except Exception:
            trace = run.evidence.path('m26-failed-traceback.txt')
            trace.write_text(traceback.format_exc())
            run.record('native playlist journey', False, traceback.format_exc(), artifacts=[run.shot('m26-failed'), trace])
    finally:
        if run:
            baseline.teardown(run, port)
    return 1 if not run or run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
