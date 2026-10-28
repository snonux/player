#!/usr/bin/env python3
"""M14 real native single/batch song insertion, FIFO, duplicates and source ownership.

Uses the installed app, actual media sessions, per-viewer saved IDs and captured
QEMU audio against a fresh music_fixture.py local server. No playback mocks.
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
import music_queue_e2e as queue
from fixture_server import Client


class Run(queue.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M14', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def action(title, name):
    assert adrv.tap('^Actions for ' + re.escape(title) + '$'), app_ui.labels()
    assert adrv.tap('^' + re.escape(name) + '$'), app_ui.labels()


def order():
    # Each overflow is one actual occurrence. Do not deduplicate equal titles.
    found = [(n['b'][1], adrv.label(n).removeprefix('Queue actions for '))
             for n in adrv.nodes() if adrv.label(n).startswith('Queue actions for ')]
    return [title for _, title in sorted(found)]


def inspect_order(expected):
    queue.queue()
    actual = order()
    assert actual == expected, (actual, expected, app_ui.labels())
    app_ui.back()


def base_album(run, songs):
    queue.start_paused(run, songs)
    app_ui.back()  # Background handler keeps playback while album is browsed.
    assert adrv.wait('^Play album$'), app_ui.labels()


def select_songs():
    # Album detail keeps Select songs in the collection More sheet (fs3).
    assert adrv.tap('^More options$'), app_ui.labels()
    assert adrv.tap('^Select songs$'), app_ui.labels()


def editing(run, songs):
    a, b, c = songs
    base_album(run, songs)
    before = sequence.state()
    select_songs()
    # Reverse selection click order still yields displayed album order B,C.
    for song in [c, b]:
        assert adrv.tap('^Select ' + re.escape(song['title']) + '$'), app_ui.labels()
    assert adrv.wait('^2 selected$'), app_ui.labels()
    assert adrv.tap('^Selected song actions$'), app_ui.labels()
    assert adrv.tap('^Play next$')
    assert adrv.wait('^Added 2 songs$'), app_ui.labels()
    assert adrv.find('^2 selected$')
    app_ui.back()  # Back exits selection, does not leave album.
    assert adrv.wait('^Play album$') and not adrv.find('^Cancel selection$'), app_ui.labels()
    action(c['title'], 'Play next')
    expected = [a['title'], b['title'], c['title'], c['title'], b['title'], c['title']]
    inspect_order(expected)
    action(a['title'], 'Add to queue')
    inspect_order(expected + [a['title']])
    after = sequence.state()
    assert after['title'] == before['title'] and after['state'] == 'PAUSED', (before, after)
    assert abs(after['position_ms'] - before['position_ms']) < 1000, (before, after)
    # Wait for the newest snackbar before its Undo; queue Undo is equivalent
    # and remains discoverable after the short-lived snackbar has gone.
    queue.queue()
    assert adrv.tap('^More options$'), app_ui.labels(); assert adrv.tap('^Undo$')
    assert order() == expected, order()
    app_ui.back()
    proof, wav = queue.output(run, c, 'm14-native-play-now', lambda: action(c['title'], 'Play now'))
    queue.queue()
    assert order()[1] == c['title'], order()
    assert adrv.tap('^More options$'), app_ui.labels(); assert adrv.tap('^Undo$')
    assert order() == expected, order()
    assert sequence.state()['state'] != 'PLAYING', sequence.state()
    run.record('single/batch FIFO displayed order, duplicate occurrences, no seek, Play now audible and Undo paused', True,
               data={'before': before, 'after': after, 'proof': proof, 'order': expected},
               artifacts=[wav, run.shot('m14-native-insertion-undo')])
    app_ui.back()
    themes(run, songs, fresh=False)


def natural(run, songs):
    a, b, c = songs
    base_album(run, songs)
    action(c['title'], 'Play next')
    action(b['title'], 'Play next')
    inspect_order([a['title'], c['title'], b['title'], b['title'], c['title']])
    for song in songs:
        baseline.reset_progress(run.viewer, song['id'])
    mark = run.audio.mark()
    sequence.key('PLAY')
    sequence.wait_track(a['title'])
    sequence.wait_track(c['title'])
    sequence.wait_track(b['title'])
    time.sleep(4)
    sequence.key('PAUSE')
    wav, analysis, matches = albums.proof_audio(run, mark, 'm14-native-natural-a-c-b', [a, c, b])
    progress = []
    for song in [a, c]:
        detail = run.viewer.request('GET', f'/api/v1/media/{song["id"]}')
        saved = detail.get('progress') or {}
        assert saved.get('user_id') == run.viewer_id and saved.get('media_id') == song['id'] and saved.get('finished'), detail
        progress.append(saved)
    duplicate, duplicate_wav = queue.output(run, b, 'm14-native-duplicate-b', lambda: sequence.key('NEXT'))
    queue.queue()
    now = [adrv.label(n) for n in adrv.nodes() if n['sel'] == 'true' and b['title'] in adrv.label(n) and 'Now' in adrv.label(n)]
    assert any(label.startswith('4 / ') for label in now), (now, app_ui.labels())
    run.record('natural FIFO A440 C659 B523, fourth duplicateB audible and authoritative viewer finished IDs', True,
               data={'audio': analysis, 'matches': matches, 'progress': progress, 'duplicate': duplicate, 'selected_row': now}, artifacts=[wav, duplicate_wav, run.shot('m14-native-natural-fifo')])
    app_ui.back()


def themes(run, songs, fresh=True):
    if fresh:
        albums.album()
    shots = []
    density = int(re.findall(r'(\d+)', adrv.adb('shell', 'wm', 'density'))[-1]) / 160
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', system)
        time.sleep(2)
        assert adrv.tap('^Actions for ' + re.escape(songs[1]['title']) + '$'), app_ui.labels()
        for label in ['Play now', 'Play next', 'Add to queue', 'Song details']:
            node = adrv.find('^' + label + '$')
            assert node and (node['b'][3] - node['b'][1]) / density >= 48, (label, node)
        shots.append(run.shot('m14-native-menu-' + theme))
        app_ui.back()
        queue.queue()
        shots.append(run.shot('m14-native-queue-' + theme))
        app_ui.back()
    albums.album('Long Album ')
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', system)
        time.sleep(2)
        node = adrv.find('^Actions for ')
        assert node, app_ui.labels()
        adrv.tap_node(node)
        assert adrv.find('^Play next$') and adrv.find('^Add to queue$'), app_ui.labels()
        shots.append(run.shot('m14-native-long-menu-' + theme))
        app_ui.back()
    run.record('light/dark menus and queue; long album titles; 48dp touch actions and Back', True, artifacts=shots)


def boundaries(run, songs):
    base_album(run, songs)
    queue.queue()
    assert adrv.tap('^More options$'), app_ui.labels(); assert adrv.tap('^Clear queue$')
    app_ui.back()
    before = sequence.state()
    action(songs[2]['title'], 'Add to queue')
    action(songs[0]['title'], 'Play next')
    inspect_order([songs[2]['title'], songs[0]['title']])
    assert sequence.state()['state'] != 'PLAYING', sequence.state()
    # Removing the reverse mapping makes the actual app offline; cached album
    # song selections can still edit in-memory queue without a network request.
    port = run.state['port']
    adrv.adb('reverse', '--remove', f'tcp:{port}')
    try:
        action(songs[1]['title'], 'Add to queue')
        inspect_order([songs[2]['title'], songs[0]['title'], songs[1]['title']])
        run.record('empty Next/Append remain paused and actual offline insertion edits queue', True,
                   data={'before': before}, artifacts=[run.shot('m14-native-offline-insertion')])
    finally:
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
    grant = {'set_id': run.state['set_ids']['Music'], 'user_id': run.viewer_id, 'role': 'viewer'}
    try:
        run.admin.request('DELETE', '/api/v1/admin/permissions', grant)
        action(songs[1]['title'], 'Play now')
        queue.queue()
        assert adrv.wait('Could not play track'), app_ui.labels()
        denied = run.shot('m14-native-real-denied')
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', grant)
    proof, wav = queue.output(run, songs[1], 'm14-native-denied-retry', lambda: queue.jump(songs[1]['title']))
    run.record('real denied inserted source shows retry and same occurrence recovers audible expected ID', True,
               data=proof, artifacts=[denied, wav])
    app_ui.back()
    sets_actions(run, songs)


def sets_actions(run, songs):
    # Existing server-file detail overflow, reached through Library (Sets lens).
    assert app_ui.go_home()
    assert app_ui.open_detail('Music', songs[0]['title']), app_ui.labels()
    assert adrv.wait('^Play Audio$'), app_ui.labels()
    before = sequence.state()
    for label in ['Play next', 'Add to queue']:
        assert adrv.tap('^(More options|Show menu)$'), app_ui.labels()
        assert adrv.tap('^' + label + '$'), app_ui.labels()
    after = sequence.state()
    assert after['title'] == before['title'] and after['state'] == before['state'], (before, after)
    assert adrv.tap('^(More options|Show menu)$'), app_ui.labels()
    proof, wav = queue.output(run, songs[0], 'm14-native-sets-play-now', lambda: adrv.tap('^Play now$'))
    run.record('server Sets audio detail exposes all explicit actions; next/append preserve source and Play now switches', True,
               data={'before': before, 'after': after, 'proof': proof}, artifacts=[wav, run.shot('m14-native-sets-detail')])


def batch(run, songs):
    a, b, c = songs
    base_album(run, songs)
    queue.queue()
    queue.action(c['title'], 'Remove')
    assert order() == [a['title'], b['title']], order()
    app_ui.back()
    select_songs()
    for song in [c, a]:
        assert adrv.tap('^Select ' + re.escape(song['title']) + '$'), app_ui.labels()
    assert adrv.tap('^Selected song actions$')
    add = next(n for n in adrv.nodes() if adrv.label(n) == 'Add to queue')
    mark = run.audio.mark()
    sequence.key('PLAY')
    before, _ = sequence.wait_track(a['title'])
    adrv.tap_node(add)
    after = sequence.state()
    assert after['state'] == before['state'] == 'PLAYING' and after['title'] == before['title'] == a['title'], (before, after)
    assert before['position_ms'] - 100 <= after['position_ms'] < before['position_ms'] + 2000, (before, after)
    time.sleep(1.5)
    sequence.key('PAUSE')
    paused = sequence.state()
    assert paused['title'] == a['title'] and paused['state'] == 'PAUSED' and paused['position_ms'] > before['position_ms'] + 500, (before, after, paused)
    append_wav = run.audio.save(mark, run.evidence.path('m14-native-batch-append-current-a.wav'))
    append_audio = baseline.audio_trace.analyze(append_wav)
    segment = baseline.main_segment(append_audio)
    assert segment and segment['duration_s'] > .75 and baseline.tone_matches(segment, a), append_audio
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        detail = run.viewer.request('GET', f'/api/v1/media/{a["id"]}')
        append_progress = detail.get('progress') or {}
        if append_progress.get('position_seconds', 0) > before['position_ms'] / 1000 + .5:
            break
        time.sleep(.5)
    assert append_progress.get('user_id') == run.viewer_id and append_progress.get('media_id') == a['id'], detail
    assert append_progress.get('position_seconds', 0) > before['position_ms'] / 1000 + .5, detail
    inspect_order([a['title'], b['title'], a['title'], c['title']])
    assert adrv.tap('^Cancel selection$')
    select_songs()
    for song in [c, b]:
        assert adrv.tap('^Select ' + re.escape(song['title']) + '$'), app_ui.labels()
    def play_selected():
        assert adrv.tap('^Selected song actions$') and adrv.tap('^Play now$'), app_ui.labels()
    first, first_wav = queue.output(run, b, 'm14-native-batch-first-b', play_selected)
    queue.queue()
    expected = [a['title'], b['title'], c['title'], b['title'], a['title'], c['title']]
    assert order() == expected, (order(), expected)
    first_now = [adrv.label(n) for n in adrv.nodes() if n['sel'] == 'true' and b['title'] in adrv.label(n) and 'Now' in adrv.label(n)]
    assert any(label.startswith('2 / ') for label in first_now), (first_now, app_ui.labels())
    first_shot = run.shot('m14-native-batch-first')
    app_ui.back()
    second, second_wav = queue.output(run, c, 'm14-native-batch-second-c', lambda: sequence.key('NEXT'))
    queue.queue()
    assert order() == expected, (order(), expected)
    second_now = [adrv.label(n) for n in adrv.nodes() if n['sel'] == 'true' and c['title'] in adrv.label(n) and 'Now' in adrv.label(n)]
    assert any(label.startswith('3 / ') for label in second_now), (second_now, app_ui.labels())
    second_shot = run.shot('m14-native-batch-second')
    run.record('batch Append preserves playing source/order; batch Play now inserts contiguous B,C and retains remainder with actual B523/C659 and viewer IDs', True,
               data={'before': before, 'after_append': after, 'paused_after_append': paused, 'append_audio': append_audio, 'append_progress': append_progress, 'order': expected, 'first': first, 'second': second, 'first_now': first_now, 'second_now': second_now},
               artifacts=[append_wav, first_wav, second_wav, first_shot, second_shot])
    app_ui.back()


def feedback(run, songs):
    # Fresh login above starts with an empty in-memory queue. No audio/volume
    # preconditions are needed for this UI-only confirmation correction.
    albums.album()
    select_songs()
    for song in [songs[1], songs[0]]:
        assert adrv.tap('^Select ' + re.escape(song['title']) + '$'), app_ui.labels()
    assert adrv.tap('^Selected song actions$')
    assert adrv.tap('^Play next$')
    batch_nodes = adrv.nodes()
    batch_labels = [adrv.label(node) for node in batch_nodes]
    assert 'Added 2 songs' in batch_labels, batch_labels
    app_ui.back()
    assert adrv.wait('^Play album$') and not adrv.find('^Cancel selection$'), app_ui.labels()
    action(songs[2]['title'], 'Add to queue')
    # Inspect the latest message and obtain its Undo from one actual semantic
    # snapshot, avoiding multiple slow dumps consuming a transient snackbar.
    latest_nodes = adrv.nodes()
    latest_labels = [adrv.label(node) for node in latest_nodes]
    assert 'Added 1 song' in latest_labels and 'Added 2 songs' not in latest_labels, latest_labels
    undo = next((node for node in latest_nodes if adrv.label(node) == 'Undo' and node['click']), None)
    assert undo, latest_labels
    latest = run.shot('m14-native-latest-confirmation')
    adrv.tap_node(undo)
    queue.queue()
    actual = order()
    assert actual == [songs[0]['title'], songs[1]['title']], (actual, app_ui.labels())
    restored = run.shot('m14-native-latest-undo')
    run.record('latest single confirmation replaces batch2 and its snackbar Undo removes only latest1', True,
               data={'batch_labels': batch_labels, 'latest_labels': latest_labels, 'undo_bounds': undo['b'], 'remaining': actual},
               artifacts=[latest, restored])
    app_ui.back()


def scope(run, songs):
    base_album(run, songs)
    action(songs[2]['title'], 'Play next')
    inspect_order([s['title'] for s in [songs[0], songs[2], songs[1], songs[2]]])
    adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
    app_ui.launch()
    assert adrv.wait('^Choose a server$', timeout=40), app_ui.labels()
    saved = adrv.find(re.escape(run.base) + '$')
    assert saved, app_ui.labels()
    adrv.tap_node(saved)
    assert adrv.wait('^Welcome back$', timeout=25), app_ui.labels()
    time.sleep(2)
    fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
    assert any(run.auth['user']['username'] in (n['text'], n['desc']) for n in fields), fields
    adrv.tap_node(fields[-1])
    assert app_ui.sign_in(run.auth['user']['password'])
    assert adrv.wait('^Library$', timeout=30)
    queue.queue()
    assert adrv.wait('Queue is empty') and not adrv.find('^Playing from:'), app_ui.labels()
    run.record('actual cold restart keeps saved login locked and inserted queue is empty after unlock', True,
               artifacts=[run.shot('m14-native-cold-restart-empty')])
    app_ui.back()
    base_album(run, songs)
    action(songs[2]['title'], 'Play next')
    assert app_ui.go_home()
    assert adrv.tap('^Settings$') and adrv.wait('^Log Out$'), app_ui.labels()
    assert adrv.tap('^Log Out$') and adrv.wait('^Welcome back$', timeout=25)
    fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
    assert len(fields) == 2, app_ui.labels()
    adrv.tap_node(fields[0]); app_ui.clear_field(); adrv.type_text(run.auth['admin']['username'])
    adrv.tap_node(fields[1]); app_ui.clear_field()
    assert app_ui.sign_in(run.auth['admin']['password']) and adrv.wait('^Restricted$'), app_ui.labels()
    queue.queue()
    assert adrv.wait('Queue is empty') and not adrv.find('^Playing from:'), app_ui.labels()
    undo = adrv.find('^Undo$')
    assert undo and not undo['click'], undo
    assert not adrv.find('Up next') and not any(adrv.find('^' + re.escape(song['title']) + '$') for song in songs), app_ui.labels()
    run.record('actual logout/admin login clears inserted entries, pending FIFO, label and Undo', True,
               artifacts=[run.shot('m14-native-account-empty')])
    app_ui.back()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--scenario', choices=['all', 'editing', 'natural', 'boundaries', 'themes', 'scope', 'feedback', 'batch'], default='all')
    args = parser.parse_args(argv)
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    run = None
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('M14 terminated'))
    try:
        assert not args.host_alias, 'offline acceptance requires adb reverse'
        baseline.prepare_device(args, port)
        run = Run(args)
        run.args, run.failures = args, 0
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        run.viewer_id = next(u['id'] for u in run.admin.request('GET', '/api/v1/admin/users') if u['username'] == run.auth['user']['username'])
        assert run.audio.available(), 'QEMU WAV output required'
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
            lambda name, ok, detail='': run.record('sign-in: ' + name, ok, detail), run.shot)
        songs = [run.track(f'Music/{baseline.ALBUM}/{name}') for name in
                 ['01 - First Light.mp3', '02 - Long Road Home.mp3', '03 - Harbour.flac']]
        for name, step in [('editing', editing), ('natural', natural), ('boundaries', boundaries), ('themes', themes), ('scope', scope), ('feedback', feedback), ('batch', batch)]:
            if args.scenario not in ('all', name) or (args.scenario == 'all' and name in ('themes', 'feedback', 'batch')):
                continue
            try:
                step(run, songs)
            except Exception:
                trace = run.evidence.path('m14-failed-' + name + '-traceback.txt')
                trace.write_text(traceback.format_exc())
                ui = run.evidence.path('m14-failed-' + name + '-ui.xml')
                adrv.adb('shell', 'uiautomator', 'dump', '/sdcard/ui.xml')
                ui.write_text(adrv.adb('exec-out', 'cat', '/sdcard/ui.xml'))
                run.record(name, False, traceback.format_exc(), artifacts=[run.shot('m14-failed-' + name), trace, ui])
    finally:
        if run:
            baseline.teardown(run, port)
    print(f'Evidence: {run.evidence.directory}', flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
