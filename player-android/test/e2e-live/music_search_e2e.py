#!/usr/bin/env python3
"""M07 actual native metadata search, touch/Back, Unicode clipboard and source audio."""
import argparse
import json
import re
from pathlib import Path
import signal
import sys
import time
import traceback
from urllib.parse import urlencode

import adrv
import app_ui
import music_e2e as baseline
from fixture_server import Client, PlayerServer, answers, stop_pid
from music_fixture import save_state, started_process


class Run(baseline.Run):
    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M07', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} {detail}', flush=True)
        if result == 'fail':
            self.failures += 1


def open_search():
    assert app_ui.go_home(), app_ui.labels()
    assert adrv.tap('^Open navigation menu$')
    assert adrv.tap('^Music · Albums$'), app_ui.labels()
    assert adrv.wait('^Music · Albums$', timeout=20), app_ui.labels()
    assert adrv.tap('^Search music$'), app_ui.labels()
    assert adrv.wait(r'^Search songs, albums and artists\.$', timeout=15), app_ui.labels()


def field():
    fields = [node for node in adrv.nodes() if node['cls'] == 'EditText']
    assert len(fields) == 1, (fields, app_ui.labels())
    return fields[0]


def search(text, expect_results=True):
    adrv.tap_node(field())
    app_ui.clear_field()
    adrv.type_text(text)
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_ENTER')
    if expect_results:
        assert adrv.wait(r'^Songs \(\d+\)$', timeout=30), app_ui.labels()
    return field()


def matching_nodes(nodes, pattern, clickable=False):
    return [node for node in nodes if node['cls'] != 'EditText'
            and re.search(pattern, adrv.label(node))
            and node['b'][2] > node['b'][0] and node['b'][3] > node['b'][1]
            and (not clickable or node['click'])]


def result_node(nodes, title):
    # A result starts with its exact title. The query field, headings, action
    # menus and retained mini-player must never stand in for the result row.
    candidates = [node for node in matching_nodes(nodes, '^' + re.escape(title), True)
                  if adrv.label(node) == title or adrv.label(node).startswith(title + ' / ')]
    assert len(candidates) <= 1, ('Ambiguous native search results', title, candidates)
    return candidates[0] if candidates else None


def visible(pattern, clickable=False):
    for move in (None, True, True, False, False, False):
        if move is not None:
            app_ui.swipe(up=move)
        candidates = matching_nodes(adrv.nodes(), pattern, clickable)
        if candidates:
            if clickable:
                assert len(candidates) == 1, ('Ambiguous native control', pattern, candidates)
            return candidates[0]
    raise AssertionError((pattern, app_ui.labels()))


def tap(pattern):
    adrv.tap_node(visible(pattern, clickable=True))


def tap_result(title):
    for move in (None, True, True, False, False, False):
        if move is not None:
            app_ui.swipe(up=move)
        node = result_node(adrv.nodes(), title)
        if node is None:
            continue
        # A fresh dump confirms that a moving/rebuilt row didn't change targets.
        stable = result_node(adrv.nodes(), title)
        assert stable and stable['b'] == node['b'] and adrv.label(stable) == adrv.label(node), ('Search result moved', title, node, stable)
        adrv.tap_node(stable)
        return
    raise AssertionError(('No clickable search result', title, app_ui.labels()))


def assert_unicode_field(node):
    value = node['text'] or node['desc']
    assert value == '東京 Nights (夜)', node
    return value


def assert_source_audio(trace, tone):
    matches = [part for part in trace['segments'] if part.get('tone_hz') is not None
               and part['duration_s'] > .7 and abs(part['tone_hz'] - tone) < 5]
    assert matches, trace
    return matches


def clipboard_unicode_query(run):
    """Copy real selectable Unicode album metadata, then edit/paste natively.

    adb input text cannot inject Unicode. This journey uses the app's own
    selectable metadata and actual native Copy/Paste controls, without adding
    a debug-only route or replacing the keyboard/driver.
    """
    search('Nights')
    tap('^Albums$')
    tap_result('東京 Nights (夜)')
    assert adrv.wait('^Play album$', timeout=20), app_ui.labels()
    tap('Actions for Café Ñoño')
    assert adrv.tap('^Song details$'), app_ui.labels()
    tap(r'^Music details(?:,|$)')
    node = visible(r'^Album: 東京 Nights \(夜\)$')
    x = (node['b'][0] + node['b'][2]) // 2
    y = (node['b'][1] + node['b'][3]) // 2
    adrv.adb('shell', 'input', 'swipe', str(x), str(y), str(x), str(y), '1100')
    assert adrv.wait('^Select all$', timeout=10), app_ui.labels()
    assert adrv.tap('^Select all$'), app_ui.labels()
    assert adrv.tap('^Copy$'), app_ui.labels()
    app_ui.back()
    assert adrv.wait('^Play album$'), app_ui.labels()
    app_ui.back()
    assert adrv.wait('^Albums$'), app_ui.labels()
    tap('^All$')
    assert adrv.tap('^Clear search$'), app_ui.labels()
    node = field()
    adrv.tap_node(node)
    x, y = (node['b'][0] + node['b'][2]) // 2, (node['b'][1] + node['b'][3]) // 2
    adrv.adb('shell', 'input', 'swipe', str(x), str(y), str(x), str(y), '1100')
    assert adrv.wait('^Paste$', timeout=10), app_ui.labels()
    assert adrv.tap('^Paste$'), app_ui.labels()
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MOVE_HOME', *['KEYCODE_FORWARD_DEL'] * len('Album: '))
    value = field()
    assert_unicode_field(value)
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_ENTER')
    assert adrv.wait(r'^Songs \(2\)$', timeout=20), app_ui.labels()
    tap('^Songs$')
    visible('Café Ñoño')
    visible('Партия для двоих')
    run.record('native Unicode text copied/pasted and edited in actual search field', True,
               data={'field': value}, artifacts=[run.shot('m07-unicode-query')])
    tap('^All$')


def journey(run):
    open_search()
    search('A Different Music Title')
    visible('A Different Music Title')
    visible(r'^Songs \(1\)$')
    item = run.track('Music/Tag Verification/source_001.flac')
    mark = run.audio.mark()
    tap_result('A Different Music Title')
    deadline = time.monotonic() + 20
    session = baseline.session_state()
    while session['state'] != 'PLAYING' and time.monotonic() < deadline:
        time.sleep(.3)
        session = baseline.session_state()
    assert session['state'] == 'PLAYING' and session['title'] == item['title'], session
    time.sleep(2)
    wav = run.audio.save(mark, run.evidence.path('m07-source.wav'))
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
    trace = baseline.audio_trace.analyze(wav)
    assert_source_audio(trace, item['tone_hz'][0])
    app_ui.back()
    assert adrv.wait(r'^Songs \(1\)$'), app_ui.labels()
    run.record('title differs from filename, touch starts result-song queue and real source output', True,
               data={'session': session, 'audio': trace}, artifacts=[wav, run.shot('m07-title-back')])
    search('A & B; C/D')
    tap('^Artists$')
    tap_result('A & B; C/D')
    assert adrv.wait('^Play artist$', timeout=20), app_ui.labels()
    app_ui.back()
    assert adrv.wait(r'^Artists \(1\)$'), app_ui.labels()
    assert any('A & B; C/D' in (node['text'], node['desc']) for node in adrv.nodes() if node['cls'] == 'EditText')
    tap('^All$')
    search('Metadata Album')
    tap('^Albums$')
    tap_result('Metadata Album')
    assert adrv.wait('^Play album$', timeout=20), app_ui.labels()
    app_ui.back()
    assert adrv.wait(r'^Albums \(1\)$'), app_ui.labels()
    tap('^All$')
    search('Kei')
    tap('^Artists$')
    visible('Aurora Lane feat. Kei')
    assert result_node(adrv.nodes(), 'Kei') is None, app_ui.labels()
    tap('^All$')
    clipboard_unicode_query(run)
    retained = baseline.session_state()
    assert retained['title'] == item['title'] and retained['state'] != 'PLAYING', retained
    run.record('searching and opening entities retains paused original Music source', True, data=retained)
    search('no-result-100%_')
    visible('Nothing matches these filters')
    assert adrv.tap('^Clear filters$'), app_ui.labels()
    assert adrv.wait(r'^Search songs, albums and artists\.$'), app_ui.labels()
    shots = []
    for theme, mode in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
        search('Long music title')
        visible('Long music title')
        shots.append(run.shot('m07-long-' + theme))
    app_ui.back()
    assert adrv.wait('^Music · Albums$', timeout=20), app_ui.labels()
    run.record('touch entity navigation, Back retains query/chips, real names, empty and long themes', True, artifacts=shots)


def errors(run):
    open_search()
    before = run.viewer.request('GET', '/api/v1/music/search?' + urlencode({'q': 'Metadata Album'}))
    previous = dict(run.state['server'])
    assert stop_pid(previous['pid'], previous['binary'], start=previous.get('start'))
    assert answers(run.state['origin'], '/healthz') is None
    adrv.adb('reverse', '--remove', f"tcp:{run.state['port']}")
    try:
        search('Metadata Album', expect_results=False)
        assert adrv.wait('^Retry$', timeout=40), app_ui.labels()
        visible('Could not search music')
        run.record('actual offline request reports error and keeps search editable', True, artifacts=[run.shot('m07-offline')])
    finally:
        server = PlayerServer(previous['binary'], run.fixture_dir, run.state['port'])
        run.state['server'].update(started_process(server.start(detached=True)))
        run.state['server']['restarts'] = run.state['server'].get('restarts', 0) + 1
        save_state(run.fixture_dir, run.state)
        adrv.adb('reverse', f"tcp:{run.state['port']}", f"tcp:{run.state['port']}")
    assert adrv.tap('^Retry$'), app_ui.labels()
    assert adrv.wait(r'^Songs \(1\)$', timeout=25), app_ui.labels()
    after = run.viewer.request('GET', '/api/v1/music/search?' + urlencode({'q': 'Metadata Album'}))
    assert [s['id'] for s in before['songs']] == [s['id'] for s in after['songs']], (before, after)
    assert [a['album_key'] for a in before['albums']] == [a['album_key'] for a in after['albums']]
    uid = next(u['id'] for u in run.admin.request('GET', '/api/v1/admin/users') if u['username'] == run.auth['user']['username'])
    permission = {'set_id': run.state['set_ids']['Music'], 'user_id': uid}
    run.admin.request('DELETE', '/api/v1/admin/permissions', permission)
    try:
        search('Metadata Album')
        visible('Nothing matches these filters')
        assert run.viewer.request('GET', '/api/v1/music/search?q=Secret') == {'songs': [], 'albums': [], 'artists': []}
        run.record('permission revocation immediately excludes music entities and hidden tags', True, artifacts=[run.shot('m07-permission')])
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', dict(permission, role='viewer'))
    search('Metadata Album')
    visible(r'^Songs \(1\)$')
    run.record('same database restart preserves IDs; online and permission restoration recover query', True,
               data={'before': before, 'after': after})


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--scenario', choices=['all', 'journey', 'errors'], default='all')
    args = parser.parse_args(argv)
    assert not args.host_alias, 'Offline acceptance requires isolated adb reverse transport'
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    run = None
    signal.signal(signal.SIGTERM, lambda *_: sys.exit('M07 terminated'))
    try:
        baseline.prepare_device(args, port)
        run = Run(args)
        run.failures = 0
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        assert run.audio.available(), 'QEMU WAV output required'
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
                                        lambda name, ok, detail='': run.record('sign-in: ' + name, ok, detail), run.shot)
        for name, step in [('journey', journey), ('errors', errors)]:
            if args.scenario not in ('all', name):
                continue
            try:
                step(run)
            except Exception:
                trace = run.evidence.path('m07-failed-' + name + '-traceback.txt')
                trace.write_text(traceback.format_exc())
                run.record(name, False, traceback.format_exc(), artifacts=[run.shot('m07-failed-' + name), trace])
    finally:
        if run:
            baseline.teardown(run, port)
    return 1 if not run or run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
