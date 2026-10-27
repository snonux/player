#!/usr/bin/env python3
"""M18 real native shuffle, optional collection inputs and scoped queue recovery.

Root owns fixture/emulator startup and stop-only cleanup. Reads authoritative
scoped queue preferences, never writes app state. UI dumps use unique checked
paths so a failed dump cannot silently reuse an earlier screen.
"""
import argparse
import json
from pathlib import Path
import re
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
import music_playlists_e2e as playlists
import music_playlist_songs_e2e as edits
import music_queue_e2e as queue_ui
import music_queue_recovery_e2e as recovery
import music_repeat_e2e as repeat
import music_accessibility_probe as accessibility
from fixture_server import Client

ACTIVE = None


class Run(baseline.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M18', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def fresh_nodes():
    remote = f'/sdcard/zk3-{time.time_ns()}.xml'
    last = None
    try:
        for _ in range(3):
            result = subprocess.run(['adb', 'shell', 'uiautomator', 'dump', remote],
                                    capture_output=True, text=True, timeout=20)
            last = {'exit': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr}
            if result.returncode == 0 and 'UI hierchary dumped to:' in result.stdout:
                break
            time.sleep(.4)
        else:
            raise AssertionError(('fresh UI dump failed', last))
        output = subprocess.run(['adb', 'exec-out', 'cat', remote], capture_output=True, timeout=10)
        assert output.returncode == 0 and b'<hierarchy' in output.stdout, last
        root = ET.fromstring(output.stdout)
        nodes = []
        for node in root.iter('node'):
            get = node.get
            bounds = [int(v) for v in re.findall(r'\d+', get('bounds', ''))]
            if len(bounds) != 4:
                continue
            nodes.append(dict(text=get('text', ''), desc=get('content-desc', ''),
                cls=get('class', '').split('.')[-1], b=bounds,
                click=get('clickable') == 'true', checked=get('checked', ''),
                sel=get('selected', ''), focused=get('focused', '')))
        assert nodes, 'Fresh UI dump contains no nodes'
        return nodes
    finally:
        subprocess.run(['adb', 'shell', 'rm', '-f', remote], capture_output=True, timeout=10)


def touch(pattern, name=None):
    candidates = [n for n in adrv.nodes() if n['click'] and re.search(pattern, adrv.label(n))]
    if not candidates and adrv.reveal(pattern):
        candidates = [n for n in adrv.nodes() if n['click'] and re.search(pattern, adrv.label(n))]
    assert candidates, (pattern, app_ui.labels())
    node = min(candidates, key=lambda n: (n['b'][2]-n['b'][0])*(n['b'][3]-n['b'][1]))
    stable = next((n for n in adrv.nodes() if n['click'] and n['b'] == node['b']
                   and adrv.label(n) == adrv.label(node)), None)
    assert stable, ('touch target changed', node, app_ui.labels())
    if ACTIVE:
        shot = ACTIVE.shot('m18-touch-' + str(time.time_ns()))
        with ACTIVE.evidence.path('m18-touch-targets.jsonl').open('a') as file:
            file.write(json.dumps({'pattern': pattern, 'node': stable, 'screenshot': str(shot)}) + '\n')
    adrv.tap_node(stable)


def snapshot(run):
    return repeat.snapshot(run)


def saved(run, predicate, timeout=15):
    return repeat.wait_saved(run, predicate, timeout)


def stable_queue(left, right):
    for field in ['entries', 'index', 'canonical', 'shuffle']:
        assert left[field] == right[field], (field, left, right)


def open_queue():
    if not adrv.find('^Now Playing$'):
        queue_ui.queue()
    assert adrv.wait('^Now Playing$'), app_ui.labels()


def toggle(run, enabled):
    open_queue()
    current = snapshot(run)['shuffle']
    if current != enabled:
        touch('^Shuffle: ' + ('On' if current else 'Off') + '$')
    result = saved(run, lambda s: s['shuffle'] == enabled)
    assert adrv.find('^Shuffle: ' + ('On' if enabled else 'Off') + '$'), app_ui.labels()
    return result


def detail(run):
    playlists.catalog()
    playlists.tap_visible(r'(^| / )' + re.escape(run.playlist_name) + r'($| / )')
    assert adrv.wait('^Play playlist$'), app_ui.labels()


def pause():
    if sequence.state()['state'] != 'PAUSED':
        sequence.key('PAUSE')
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        if sequence.state()['state'] == 'PAUSED':
            # Let the handler's asynchronous pause snapshot reach preferences.
            time.sleep(.25)
            return
        time.sleep(.1)
    raise AssertionError(('media Pause did not settle', sequence.state()))


def setup_playlist(run):
    album = next(a for a in run.viewer.request('GET', '/api/v1/music/albums') if a['title'] == 'Night Transit')
    songs = run.viewer.request('GET', '/api/v1/music/albums/' + album['album_key'])['songs']
    durations = []
    for song in songs:
        manifest = next(t for t in run.manifest['tracks'] if run.state['media_ids'][t['path']] == song['id'])
        output = subprocess.run(['ffprobe','-v','error','-show_entries','format=duration',
            '-of','default=noprint_wrappers=1:nokey=1', str(Path(run.state['media_root'])/manifest['path'])],
            capture_output=True,text=True,check=True,timeout=15)
        seconds=float(output.stdout.strip())
        assert 44.9 <= seconds <= 45.2 and 44.9 <= song['duration'] <= 45.2 and manifest['duration_s']==45, (song['id'],seconds,song['duration'],manifest['duration_s'])
        durations.append({'media_id':song['id'],'ffprobe_seconds':seconds,'server_seconds':song['duration'],'manifest_seconds':manifest['duration_s']})
    run.record('actual generated source durations match server and manifest before shuffle playback',True,data=durations)
    run.songs = songs + songs
    run.playlist_name = 'M18 native ' + str(time.time_ns())
    run.pid = run.viewer.request('POST', '/api/v1/playlists', {'name': run.playlist_name})['playlist_id']
    run.viewer.request('POST', '/api/v1/playlists/' + run.pid + '/songs',
                       {'action': 'add', 'media_ids': [s['id'] for s in run.songs]})
    detail(run)
    adrv.tap_node(edits.row(2, run.songs[1]['music']['title']))
    sequence.wait_track(run.songs[1]['music']['title'])
    # Hardware keys target the music stream while playing. Then keep the source
    # paused during slow UI dump/screenshot work and overlay dismissal.
    for _ in range(15):
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_VOLUME_UP')
    sequence.ensure_music_volume(run)
    pause()
    assert sequence.state()['state'] == 'PAUSED', sequence.state()
    time.sleep(4)
    open_queue()
    saved(run, lambda s: len(s['entries']) == len(run.songs))


def source(run, entry, name):
    expected = next(run.track(t['path']) for t in run.manifest['tracks']
                    if run.state['media_ids'][t['path']] == entry['mediaId'])
    mark = run.audio.mark()
    sequence.wait_track(expected['title'])
    time.sleep(7)
    session = sequence.state()
    pause()
    assert session['state'] == 'PLAYING' and session['title'] == expected['title'], session
    wav, analysis, matches = albums.proof_audio(run, mark, name, [expected])
    return {'entry': entry, 'session': session, 'audio': analysis, 'matches': matches, 'wav': str(wav)}


def core(run):
    if not run.from_cycle:
        before = snapshot(run)
        mixed = toggle(run, True)
        assert mixed['entries'][:before['index']+1] == before['entries'][:before['index']+1]
        assert sorted(e['id'] for e in mixed['entries']) == sorted(e['id'] for e in before['entries'])
        assert abs(mixed['position'] - before['position']) < .1, (before, mixed)
        touch('^Reshuffle remaining$')
        touch('^Undo$')
        stable_queue(mixed, snapshot(run))
        off = toggle(run, False)
        assert off['entries'] == before['entries'], (before, off)
        toggle(run, True)
        retained = snapshot(run)
        app_ui.back()
        albums.catalog()
        stable_queue(retained, snapshot(run))
        assert adrv.find('^Music · Albums$'), app_ui.labels()
        run.record('queue remainder toggle canonical Off reshuffle Undo preserves occurrence/position while browsing independently', True,
                   data={'before': before, 'mixed': mixed, 'retained': retained}, artifacts=[run.shot('m18-native-independent-browse')])
    detail(run)
    touch('^Shuffle$')
    saved(run, lambda s: s['shuffle'] and s['index'] == 0)
    time.sleep(.3)
    pause()
    collection = snapshot(run)
    assert sorted(e['mediaId'] for e in collection['entries']) == sorted(s['id'] for s in run.songs)
    assert collection['canonical'] == sorted(e['id'] for e in collection['entries']), collection
    proof, visited = [], []
    for at, entry in enumerate(collection['entries']):
        current = snapshot(run)
        assert current['index'] == at and current['entries'][at]['id'] == entry['id'], current
        touch('^Play$')
        proof.append(source(run, entry, f'm18-native-cycle-{at}'))
        visited.append(snapshot(run)['entries'][snapshot(run)['index']]['id'])
        if at + 1 < len(collection['entries']):
            touch('^Next track$')
            target = collection['entries'][at+1]
            expected = next(s for s in run.songs if s['id'] == target['mediaId'])
            sequence.wait_track(expected['music']['title'])
            pause()
            saved(run, lambda s: s['index'] == at+1)
    assert visited == [e['id'] for e in collection['entries']] and len(set(visited)) == len(visited), visited
    open_queue()
    last = toggle(run, False)
    assert last['index'] == len(collection['entries'])-1 and last['entries'] == collection['entries']
    run.record('direct collection Shuffle preserves duplicate occurrences and every actual cycle source is audible exactly once', True,
               data={'collection': collection, 'visited': visited, 'proof': proof}, artifacts=[p['wav'] for p in proof])


def open_options():
    if adrv.find('^Now Playing$'):
        app_ui.back()
    touch(r'^Queue actions(?: ▾)?$')
    touch('^Shuffle options…$')
    assert adrv.wait('^Shuffle options$'), app_ui.labels()


def field(label, value):
    stamp=str(time.time_ns())
    nodes=accessibility.snapshot(ACTIVE.evidence.path('m18-field-native-hints-'+stamp+'.json'))
    node=accessibility.editable_field(nodes,label)
    shot=ACTIVE.shot('m18-field-native-hint-'+stamp)
    with ACTIVE.evidence.path('m18-field-hint-targets.jsonl').open('a') as file:
        file.write(json.dumps({'label':label,'native_node':node,'screenshot':str(shot)})+'\n')
    adrv.tap_node({'b':node['bounds']}); app_ui.clear_field(80)
    if value:
        adrv.type_text(value)
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_BACK')


def options(run):
    detail(run)
    before = snapshot(run)
    open_options()
    field('Maximum songs (optional)', '0')
    touch('^Shuffle$')
    assert adrv.wait('Maximum songs must be a whole number'), app_ui.labels()
    stable_queue(before, snapshot(run))
    field('Maximum songs (optional)', '2')
    field('Genre (optional)', 'No such genre')
    touch('^Shuffle$')
    assert adrv.wait('No songs match these options.'), app_ui.labels()
    stable_queue(before, snapshot(run))
    metadata = next(s['music'] for s in run.songs if s.get('music', {}).get('genres') and s['music'].get('year'))
    field('Genre (optional)', metadata['genres'][0])
    field('From year (optional)', str(metadata['year']))
    field('To year (optional)', str(metadata['year']))
    shots = []
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', system)
        time.sleep(.7)
        assert adrv.find('^Shuffle options$'), app_ui.labels()
        shots.append(run.shot('m18-native-options-' + theme))
    assert not run.host_alias, 'Actual outage requires the owned adb reverse path'
    port = run.state['port']
    adrv.adb('reverse', '--remove', f'tcp:{port}')
    try:
        touch('^Shuffle$')
        assert adrv.wait('Could not load songs.', timeout=60), app_ui.labels()
        stable_queue(before, snapshot(run))
        shots.append(run.shot('m18-native-options-offline'))
    finally:
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
    touch('^Shuffle$')
    result = saved(run, lambda s: s['shuffle'] and len(s['entries']) == 2 and s['entries'] != before['entries'])
    # Successful collection replacement starts playback asynchronously. Observe
    # that real source directly; an early pause can see the previous PAUSED
    # session before the new player has started.
    assert not adrv.find('^Shuffle options$'), app_ui.labels()
    eligible = [s for s in run.songs if s['music'].get('year') == metadata['year']
                and any(g.strip().lower() == metadata['genres'][0].strip().lower() for g in s['music'].get('genres', []))]
    assert len(result['entries']) == min(2, len(eligible))
    assert all(any(s['id'] == entry['mediaId'] for s in eligible) for entry in result['entries'])
    proof = source(run, result['entries'][0], 'm18-native-filtered-source')
    # Cancel and Back are actual touch/native-key paths. The delayed-result
    # boundary is additionally covered by the production widget regression.
    open_options(); touch('^Cancel$'); stable_queue(result, snapshot(run))
    open_options(); app_ui.back(); stable_queue(result, snapshot(run))
    run.record('optional genre/year/max invalid no-match offline retry filtered audible subset and Cancel/Back preserve queue', True,
               data={'before': before, 'eligible': [s['id'] for s in eligible], 'selected': result, 'proof': proof},
               artifacts=shots+[proof['wav']])


def persistence(run):
    detail(run); touch('^Shuffle$')
    selected = saved(run, lambda s: s['shuffle'] and len(s['entries']) == len(run.songs))
    expected = next(song for song in run.songs if song['id'] == selected['entries'][selected['index']]['mediaId'])
    sequence.wait_track(expected['music']['title'])
    time.sleep(3)
    pause()
    before = saved(run, lambda s: s['position'] >= 2)
    recovery.restart(run); open_queue()
    assert adrv.wait('^Resume queue$'), app_ui.labels()
    after = snapshot(run); stable_queue(before, after)
    assert abs(after['position']-before['position']) < .1 and sequence.state()['state'] != 'PLAYING'
    shots = []
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', system); time.sleep(.7)
        shots.append(run.shot('m18-native-queue-' + theme))
    app_ui.back(); recovery.login_other(run, run.auth['admin']); open_queue()
    actor = type('Actor', (), {'base':run.base,'viewer_id':run.admin_id})()
    # A never-used account has no queue document until its first mutation.
    # Both absent storage and an explicitly empty default session are valid.
    assert adrv.wait('Queue is empty'), app_ui.labels()
    current = repeat.snapshot(actor)
    assert current is None or (not current['entries'] and not current['shuffle']), current
    assert adrv.find('^Shuffle: Off$') and not adrv.find('^Resume queue$'), app_ui.labels()
    shots.append(run.shot('m18-native-other-account-empty'))
    app_ui.back(); recovery.login_other(run, run.auth['user']); open_queue()
    stable_queue(before, snapshot(run))
    mark = run.audio.mark(); touch('^Resume queue$'); app_ui.back()
    expected = next(run.track(t['path']) for t in run.manifest['tracks'] if run.state['media_ids'][t['path']] == before['entries'][before['index']]['mediaId'])
    sequence.wait_track(expected['title']); time.sleep(7); pause()
    wav, analysis, matches = albums.proof_audio(run, mark, 'm18-native-restored-source', [expected])
    run.record('mode canonical order occurrences position restore paused after force-stop and account roundtrip then actual Resume audio', True,
               data={'before': before, 'after': after, 'audio': analysis, 'matches': matches}, artifacts=shots+[wav])



def permission(run):
    # Keep a visible album route while only this fixture user's actual grant is
    # revoked. Each collection action resolves afresh through the real API.
    album = next(a for a in run.viewer.request('GET', '/api/v1/music/albums') if a['title'] == 'Night Transit')
    albums.album()
    before, runtime = snapshot(run), sequence.state()
    grants = run.admin.request('GET', '/api/v1/admin/permissions')['permissions']
    prior = next(g for g in grants if g['set_id'] == run.state['set_ids']['Music'] and g['user_id'] == run.viewer_id)
    grant = {key: prior[key] for key in ['set_id', 'user_id', 'role']}
    artifacts = []
    try:
        run.admin.request('DELETE', '/api/v1/admin/permissions', grant)
        status, body = run.viewer.call('GET', '/api/v1/music/albums/' + album['album_key'])
        assert status >= 400, (status, body)
        touch('^Shuffle$')
        assert adrv.wait('Could not load songs.', timeout=30), app_ui.labels()
        stable_queue(before, snapshot(run))
        artifacts.append(run.shot('m18-native-permission-direct-retained'))
        open_options(); touch('^Shuffle$')
        assert adrv.wait('Could not load songs.', timeout=30), app_ui.labels()
        retained, after_runtime = snapshot(run), sequence.state()
        stable_queue(before, retained)
        assert abs(before['position']-retained['position']) < .1
        assert after_runtime['state'] == 'PAUSED' and after_runtime['title'] == runtime['title']
        assert abs(after_runtime['position_ms']-runtime['position_ms']) < 100, (runtime, after_runtime)
        artifacts.append(run.shot('m18-native-permission-options-retained'))
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', grant)
        restored = run.admin.request('GET', '/api/v1/admin/permissions')['permissions']
        assert any(all(g.get(k)==v for k,v in grant.items()) for g in restored), restored
    touch('^Shuffle$')
    selected = saved(run, lambda s: s['shuffle'] and len(s['entries']) == len(run.songs)//2)
    assert not adrv.find('^Shuffle options$'), app_ui.labels()
    proof = source(run, selected['entries'][0], 'm18-native-permission-restored-source')
    run.record('real Music grant revocation preserves queue current source position for direct Shuffle and options; exact grant restored then retry audible', True,
        data={'grant':grant,'denied_http_status':status,'before':before,'retained':retained,
              'runtime_before':runtime,'runtime_denied':after_runtime,'selected':selected,'proof':proof},
        artifacts=artifacts+[proof['wav']])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir'); parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True); parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--ownership-path'); parser.add_argument('--resume-device', action='store_true')
    parser.add_argument('--from-cycle', action='store_true')
    parser.add_argument('--scenario', choices=['all', 'core', 'options', 'recovery', 'remaining', 'permission'], default='all')
    args = parser.parse_args(argv)
    run = None
    global ACTIVE
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('M18 interrupted; root owns stop-only cleanup'))
    adrv.nodes = fresh_nodes
    try:
        assert 'uid=0' in adrv.adb('shell', 'id'), 'Rooted disposable emulator required'
        state = json.loads((Path(args.fixture_dir)/'state.json').read_text())
        baseline.prepare_device(args, state['port'])
        run = Run(args); ACTIVE = run; run.failures = 0; run.host_alias = args.host_alias; run.from_cycle = args.from_cycle
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        users = run.admin.request('GET', '/api/v1/admin/users')
        run.viewer_id = next(u['id'] for u in users if u['username'] == run.auth['user']['username'])
        run.admin_id = next(u['id'] for u in users if u['username'] == run.auth['admin']['username'])
        assert run.audio.available(), 'Actual QEMU output WAV required'
        if args.resume_device:
            recovery.restart(run)
        else:
            assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
                lambda name, ok, detail='': run.record('sign-in: '+name, ok, detail), run.shot)
        accessibility.build_install(run.evidence.path('accessibility-probe'))
        setup_playlist(run)
        for name, step in [('core', core), ('options', options), ('recovery', persistence), ('permission', permission)]:
            if args.scenario in ('all', name) or (args.scenario=='remaining' and name in ('options','recovery')):
                step(run)
    except Exception:
        if not run:
            raise
        trace = run.evidence.path('m18-native-failure-'+str(time.time_ns())+'.txt')
        trace.write_text(traceback.format_exc())
        run.record('native shuffle journey', False, traceback.format_exc(), artifacts=[trace,run.shot('m18-native-failed')])
    finally:
        print('Runtime retained for root evidence audit and stop-only cleanup.', flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
