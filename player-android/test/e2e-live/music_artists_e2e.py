#!/usr/bin/env python3
"""M03 actual native artist roles, discography/back, credits and source audio."""
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
        self.evidence.record('M03', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} [{detail}]', flush=True)
        if result == 'fail':
            self.failures += 1


def catalog():
    assert app_ui.go_home(), app_ui.labels()
    assert adrv.tap('^Open navigation menu$')
    assert adrv.tap('^Music · Artists$'), app_ui.labels()
    assert adrv.wait('^Music · Artists$', timeout=20), app_ui.labels()


def role(label):
    # Native DropdownButtonFormField exposes the selected value and Role label.
    node = adrv.find(r'(^| / )Role($| / )') or adrv.find('Artists \\(track and album\\)')
    assert node, app_ui.labels()
    adrv.tap_node(node)
    assert adrv.tap('^' + re.escape(label) + '$'), app_ui.labels()
    time.sleep(1)


def open_visible(name):
    node = baseline.find_visible(re.escape(name))
    assert node, app_ui.labels()
    adrv.tap_node(node)


def journey(run):
    people = run.viewer.request('GET', '/api/v1/music/artists')
    assert not any(p['name'] in ('Hidden Composer', 'Hidden Performer', 'Secret Artist', 'Composer; 東京 / A') for p in people), people
    assert any(p['name'] == 'A & B; C/D' for p in people), people
    composer = next(p for p in run.viewer.request('GET', '/api/v1/music/artists?role=composer') if p['name'] == 'Composer; 東京 / A')
    assert composer['album_count'] == 2 and composer['song_count'] == 3, composer
    hidden = next(p for p in run.admin.request('GET', '/api/v1/music/artists?role=composer') if p['name'] == 'Hidden Composer')
    assert run.viewer.call('GET', '/api/v1/music/artists/' + hidden['artist_key'])[0] == 404
    catalog()
    assert any('Artists (track and album)' in label for label in app_ui.labels()), app_ui.labels()
    for label, code, visible in [('Track artist', 'track_artist', 'Neon Coast'), ('Album artist', 'album_artist', 'Various Artists'), ('Composer', 'composer', 'Composer; 東京 / A'), ('Performer', 'performer', 'Session Ensemble')]:
        role(label)
        assert app_ui.find_scrolling(re.escape(visible)), app_ui.labels()
        listed = run.viewer.request('GET', '/api/v1/music/artists?role=' + code)
        assert listed and all(code in p['roles'] for p in listed), listed
        assert not any(p['name'].startswith('Hidden ') for p in listed), listed
        run.record('native role ' + label + ' and authoritative permitted catalog', True, data={'people': listed})
    open_visible('Session Ensemble')
    assert adrv.wait('^Session Ensemble$', timeout=20), app_ui.labels()
    assert app_ui.find_scrolling(re.escape('東京 Nights (夜)')), app_ui.labels()
    run.record('performer-only visible person opens actual performer discography', True, artifacts=[run.shot('m03-performer')])
    app_ui.back()
    role('Composer')
    open_visible('Composer; 東京 / A')
    assert adrv.wait('^Composer; 東京 / A$', timeout=20), app_ui.labels()
    assert any('2 albums · 3 songs' in label for label in app_ui.labels()), app_ui.labels()
    open_visible('Synth Compilation')
    assert adrv.wait('^Play album$', timeout=20), app_ui.labels()
    assert any('Various Artists' in label for label in app_ui.labels()), app_ui.labels()
    app_ui.back()
    assert adrv.wait('^Composer; 東京 / A$'), app_ui.labels()
    app_ui.back()
    assert adrv.wait('^Music · Artists$'), app_ui.labels()
    assert any('Composer' in label for label in app_ui.labels()), app_ui.labels()
    artifacts = [run.shot('m03-role-back')]
    role('Track artist')
    open_visible('Mika Sørensen')
    open_visible('東京 Nights (夜)')
    assert adrv.wait('^Play album$', timeout=20), app_ui.labels()
    open_visible('Mika Sørensen')
    assert adrv.wait('^Mika Sørensen$'), app_ui.labels()
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', system)
        time.sleep(2)
        artifacts.append(run.shot('m03-artist-' + theme))
    run.record('artist discography links, structured people, role/Back retention and themes', True,
               data={'composer': composer}, artifacts=artifacts)
    # The existing song details show embedded credits without changing identity.
    song = run.track('Music/Tag Verification/source_001.flac')
    albums.album('Metadata Album')
    open_visible('Actions for A Different Music Title')
    assert adrv.tap('^Song details$'), app_ui.labels()
    node = baseline.find_visible('^Music details')
    assert node, app_ui.labels()
    adrv.tap_node(node)
    assert baseline.find_visible('^Composer: Metadata Composer$'), app_ui.labels()
    assert baseline.find_visible('^Performer: Metadata Performer$'), app_ui.labels()
    run.record('native song details distinguish composer and performer', True, artifacts=[run.shot('m03-credits')])
    app_ui.back()
    assert adrv.wait('^Play album$'), app_ui.labels()
    mark = run.audio.mark()
    assert adrv.tap('^Play album$'), app_ui.labels()
    current = None
    # Explicit, bounded media-session polling: no silently skipped audio proof.
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        current = baseline.session_state()
        if current['title'] == 'A Different Music Title' and current['state'] == 'PLAYING':
            break
        time.sleep(.25)
    assert current and current['title'] == 'A Different Music Title' and current['state'] == 'PLAYING', current
    time.sleep(4)
    wav, analysis, matches = albums.proof_audio(run, mark, 'm03-source', [song])
    run.record('original source audible after artist and credit navigation', True,
               data={'session': current, 'analysis': analysis, 'matches': matches}, artifacts=[wav])


def scroll_anchor(pattern, limit=14):
    for swipes in range(limit + 1):
        node = adrv.find(pattern)
        if node and node['b'][3] - node['b'][1] > 150:
            return node, swipes
        assert swipes < limit, app_ui.labels()
        app_ui.swipe()
    raise AssertionError('No scroll anchor ' + pattern)


def position(run):
    catalog()
    person = next(p for p in run.viewer.request('GET', '/api/v1/music/artists')
                  if p['name'].startswith('Zzz Catalog Artist'))
    pattern = '^' + re.escape(person['name'])
    assert not adrv.find(pattern), 'Artist anchor unexpectedly visible without scrolling'
    node, list_swipes = scroll_anchor(pattern)
    assert list_swipes > 0
    list_bounds = node['b']
    list_shot = run.shot('m03-scrolled-artists-before')
    adrv.tap_node(node)
    assert adrv.wait('^' + re.escape(person['name']) + '$', timeout=20), app_ui.labels()
    release_pattern = '^Catalog Release 14'
    assert not adrv.find(release_pattern), 'Release anchor unexpectedly visible without scrolling'
    release, detail_swipes = scroll_anchor(release_pattern)
    assert detail_swipes > 0
    detail_bounds = release['b']
    detail_shot = run.shot('m03-scrolled-discography-before')
    adrv.tap_node(release)
    assert adrv.wait('^Play album$', timeout=20), app_ui.labels()
    assert any('Catalog Release 14' in label for label in app_ui.labels()), app_ui.labels()
    app_ui.back()
    restored = adrv.find(release_pattern)
    assert restored and max(abs(a-b) for a, b in zip(restored['b'], detail_bounds)) <= 8, (detail_bounds, restored, app_ui.labels())
    restored_detail_shot = run.shot('m03-scrolled-discography-restored')
    app_ui.back()
    restored_list = adrv.find(pattern)
    assert restored_list and max(abs(a-b) for a, b in zip(restored_list['b'], list_bounds)) <= 8, (list_bounds, restored_list, app_ui.labels())
    artifacts = [list_shot, detail_shot, restored_detail_shot, run.shot('m03-scrolled-artists-restored')]
    adrv.tap_node(restored_list)
    for theme, system in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', system)
        time.sleep(2)
        artifacts.append(run.shot('m03-long-artist-' + theme))
    run.record('genuinely scrolled artist and discography restore visible anchor bounds on native Back', True,
               data={'list_swipes': list_swipes, 'detail_swipes': detail_swipes,
                     'list_before': list_bounds, 'list_after': restored_list['b'],
                     'detail_before': detail_bounds, 'detail_after': restored['b']}, artifacts=artifacts)


def errors(run):
    catalog()
    role('Composer')
    previous = dict(run.state['server'])
    stopped = stop_pid(previous['pid'], previous['binary'], start=previous.get('start'))
    assert stopped and answers(run.state['origin'], '/healthz') is None, 'Owned server cutoff failed'
    adrv.adb('reverse', '--remove', f"tcp:{run.state['port']}")
    try:
        open_visible('Composer; 東京 / A')
        assert adrv.wait('^Retry$', timeout=30), app_ui.labels()
        assert any('Could not load artists.' in label for label in app_ui.labels()), app_ui.labels()
        run.record('actual offline artist failure retains native Back and Retry', True, artifacts=[run.shot('m03-offline')])
    finally:
        server = PlayerServer(previous['binary'], run.fixture_dir, run.state['port'])
        run.state['server'].update(started_process(server.start(detached=True)))
        run.state['server']['restarts'] = run.state['server'].get('restarts', 0) + 1
        save_state(run.fixture_dir, run.state)
        adrv.adb('reverse', f"tcp:{run.state['port']}", f"tcp:{run.state['port']}")
        run.record('owned fixture server restarts on same database after closing old transport', True,
                   detail=f"PID {previous['pid']} -> {run.state['server']['pid']}",
                   data={'previous': previous, 'current': run.state['server']})
    assert adrv.tap('^Retry$'), app_ui.labels()
    assert adrv.wait('^Composer; 東京 / A$', timeout=25), app_ui.labels()
    app_ui.back()
    uid = next(u['id'] for u in run.admin.request('GET', '/api/v1/admin/users') if u['username'] == run.auth['user']['username'])
    permission = {'set_id': run.state['set_ids']['Music'], 'user_id': uid}
    run.admin.request('DELETE', '/api/v1/admin/permissions', permission)
    try:
        # An already visible credit is denied on the actual pushed detail request.
        open_visible('Composer; 東京 / A')
        assert adrv.wait('^Retry$', timeout=20), app_ui.labels()
        assert any('unavailable' in label for label in app_ui.labels()), app_ui.labels()
        run.record('permission revocation denies existing artist without leaking hidden credits', True, artifacts=[run.shot('m03-permission')])
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', dict(permission, role='viewer'))
    assert adrv.tap('^Retry$'), app_ui.labels()
    assert adrv.wait('^Composer; 東京 / A$', timeout=20), app_ui.labels()
    run.record('online and permission restoration retry same artist', True)


def restart(run):
    adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
    app_ui.launch()
    assert adrv.wait('^Choose a server$', timeout=40), app_ui.labels()
    saved = adrv.find(re.escape(run.base) + '$')
    assert saved, app_ui.labels()
    adrv.tap_node(saved)
    assert adrv.wait('^Welcome back$', timeout=25), app_ui.labels()
    fields = [node for node in adrv.nodes() if node['cls'] == 'EditText']
    assert fields and any(run.auth['user']['username'] in (node['text'], node['desc']) for node in fields), fields
    assert not adrv.find('^Library$'), 'Saved credential unexpectedly unlocked'
    adrv.tap_node(fields[-1])
    assert app_ui.sign_in(run.auth['user']['password']), app_ui.labels()
    catalog()
    role('Composer')
    open_visible('Composer; 東京 / A')
    assert adrv.wait('^Composer; 東京 / A$', timeout=25), app_ui.labels()
    assert any('2 albums · 3 songs' in label for label in app_ui.labels()), app_ui.labels()
    run.record('cold app restart remembers locked account and explicit sign-in restores artist role/discography', True,
               artifacts=[run.shot('m03-cold-restart-artist')])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--scenario', choices=['all', 'journey', 'position', 'errors', 'restart'], default='all')
    args = parser.parse_args(argv)
    assert not args.host_alias, 'Offline acceptance requires isolated adb reverse transport'
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    run = None
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('M03 terminated'))
    try:
        baseline.prepare_device(args, port)
        run = Run(args)
        run.failures = 0
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        assert run.audio.available(), 'QEMU WAV output required'
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
                                        lambda name, ok, detail='': run.record('sign-in: ' + name, ok, detail), run.shot)
        for name, step in [('journey', journey), ('position', position), ('errors', errors), ('restart', restart)]:
            if args.scenario not in ('all', name):
                continue
            try:
                step(run)
            except Exception:
                trace = run.evidence.path('m03-failed-' + name + '-traceback.txt')
                trace.write_text(traceback.format_exc())
                run.record(name, False, traceback.format_exc(), artifacts=[run.shot('m03-failed-' + name), trace])
    finally:
        if run:
            baseline.teardown(run, port)
    return 1 if not run or run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
