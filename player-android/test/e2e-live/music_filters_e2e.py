#!/usr/bin/env python3
"""F03 native touch acceptance against the F03-augmented disposable fixture.

Uses visible native controls and authoritative IDs/measurements, never mocks or
coordinates detached from actual semantics. Root owns server/emulator lifecycle.
"""
import argparse
from contextlib import contextmanager
import json
import os
import signal
from pathlib import Path
import re
import sys
import time
import traceback
from urllib.parse import urlencode

import adrv
import app_ui
import music_e2e as baseline
from fixture_server import Client, PlayerServer, owns_process, stop_pid
from music_fixture import save_state, started_process


class Run(baseline.Run):
    failures = 0

    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('F03', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} {detail}', flush=True)
        if result == 'fail':
            self.failures += 1


def visible(pattern, scroll=False):
    for direction in ([None, True, True, True, False, False, False, False] if scroll else [None]):
        if direction is not None:
            app_ui.swipe(direction)
        node = baseline.find_visible(pattern)
        if node:
            return node
    raise AssertionError((pattern, app_ui.labels()))


def tap(pattern, scroll=False):
    adrv.tap_node(visible(pattern, scroll))
    time.sleep(.3)


def dismiss_keyboard():
    def shown():
        # Input-view layout can remain "shown" after its IME window is hidden.
        state = re.search(r'\bmWindowVisible=(true|false)\b',
                          adrv.adb('shell', 'dumpsys', 'input_method'))
        assert state, 'Cannot determine native keyboard visibility'
        return state[1] == 'true'
    if shown():
        app_ui.back()
        deadline = time.monotonic() + 8
        while shown() and time.monotonic() < deadline:
            time.sleep(.2)
        assert not shown(), 'Native keyboard did not close'


def query(text):
    # An empty Flutter TextField exposes neither hint nor value in UIAutomator.
    # Select the unique editable control on the grid, including its empty state.
    fields = [n for n in adrv.nodes() if n['cls'] == 'EditText']
    assert len(fields) == 1, fields
    adrv.tap_node(fields[0])
    app_ui.clear_field(100)
    adrv.type_text(text)
    dismiss_keyboard()
    time.sleep(.7)
    return fields[0]


def bound(label, value):
    labels = ['Minimum duration (minutes)', 'Maximum duration (minutes)',
              'Minimum file size (MiB)', 'Maximum file size (MiB)']
    index = labels.index(label)
    # Flutter exposes empty input labels as accessibility hints, which this
    # Android UIAutomator XML format omits. Scroll the real sheet until all four
    # editable bounds are visible, then use their fixed top-to-bottom order.
    for move in (False, True, True, True):
        if move:
            app_ui.swipe(up=True)
        nodes = adrv.nodes()
        assert any(adrv.label(n) == 'Close filters' for n in nodes), 'Filter sheet is not open'
        fields = sorted((n for n in nodes if n['cls'] == 'EditText'
                         and n['b'][2] > n['b'][0] and n['b'][3] > n['b'][1]),
                        key=lambda n: n['b'][1])
        if len(fields) == 4:
            break
    else:
        raise AssertionError(('Expected four visible filter bounds', fields, app_ui.labels()))
    node = fields[index]
    adrv.tap_node(node)
    app_ui.clear_field(100)
    adrv.type_text(str(value))
    dismiss_keyboard()


def apply_case(bounds=(), tags=(), other=False, all_sets=False):
    tap('^Filters$')
    assert adrv.wait('^Apply$'), app_ui.labels()
    tap('^Clear filters$')  # draft only
    if other:
        tap('^F03 Other$', True)
    if all_sets:
        tap('^Music$', True)
    for tag in tags:
        tap('^' + re.escape(tag) + '$', True)
    labels = ['Minimum duration (minutes)', 'Maximum duration (minutes)',
              'Minimum file size (MiB)', 'Maximum file size (MiB)']
    for index, value in bounds:
        bound(labels[index], value)
    tap('^Apply$')
    assert adrv.wait('^Filters$'), app_ui.labels()
    query('f03-')


def names_from_nodes(run, nodes):
    found = set()
    for node in nodes:
        label = adrv.label(node)
        for track in run.manifest['tracks']:
            if '/F03/' in track['path']:
                continue
            title = track.get('title') or Path(track['path']).stem
            if label == title or label.startswith(title + ' / '):
                found.add('UNEXPECTED:' + title)
        for name in re.findall(r'(?<![\w-])(f03-(?:a|b|c|zero|other|secret)(?:\.wav|\.jpg)?)(?![\w-])', label):
            found.add(re.sub(r'\.(wav|jpg)$', '', name))
    return found


def swipe_grid(up):
    nodes = adrv.nodes()
    toolbar = [n for n in nodes if adrv.label(n) in ('Filters and sort', 'Clear')]
    assert toolbar, 'Grid search bar is not visible'
    top = max(n['b'][3] for n in toolbar) + 16
    bottom = max(n['b'][3] for n in nodes) - 160
    width = max(n['b'][2] for n in nodes)
    assert bottom - top > 200, ('Grid viewport is too small for a swipe', top, bottom)
    inset = (bottom - top) // 6
    start, end = (bottom - inset, top + inset) if up else (top + inset, bottom - inset)
    adrv.adb('shell', 'input', 'swipe', str(width // 2), str(start),
             str(width // 2), str(end), '400')
    time.sleep(.5)


def names_on_grid(run):
    found = set()
    for direction in (None, False, True, True):
        if direction is not None:
            swipe_grid(direction)
        found.update(names_from_nodes(run, adrv.nodes()))
    return found


def completed_result(actual, expected, labels, loading=False, fresh_empty_refresh=False):
    """A zero-row snapshot alone is never proof of a completed empty query."""
    if loading or any(re.search(r'Updating media|Loading|Could not|Unexpected error|Server error|^Retry$', label)
                      for label in labels):
        return False
    if set(actual) != set(expected):
        return False
    if not expected:
        return fresh_empty_refresh and 'Nothing matches these filters' in labels
    return True


def completed_empty_refresh(run):
    """Prove a new real native request completes for the currently applied filter.

    A brief identity-checked pause exposes the real existing loading indicator;
    finally always resumes the same fixture process. This is latency injection,
    not a result mock, API-only comparison or stale-empty/sleep oracle.
    """
    fixture = run.fixture_dir.resolve(strict=True)
    assert (fixture / '.player-music-fixture').is_file(), 'Not a marked fixture'
    state = json.loads((fixture / 'state.json').read_text())
    server = state['server']
    assert adrv.wait('^Nothing matches these filters$', timeout=20), app_ui.labels()
    identity = (server['pid'], server['binary'], server.get('start'))
    assert identity[2] is not None, 'Missing process start fingerprint'
    assert owns_process(identity[0], identity[1], start=identity[2]), 'Fixture identity changed'
    os.kill(identity[0], signal.SIGSTOP)
    try:
        # The actual result ListView owns pull-to-refresh, below the toolbar.
        node = visible('^Nothing matches these filters$')
        left, top, right, bottom = node['b']
        adrv.adb('shell', 'input', 'swipe', str((left + right) // 2), str(top),
                 str((left + right) // 2), str(top + max(300, bottom - top)), '500')
        assert adrv.wait('^Updating media$', timeout=8), app_ui.labels()
    finally:
        if not owns_process(identity[0], identity[1], start=identity[2]):
            raise AssertionError('Fixture identity changed before SIGCONT')
        os.kill(identity[0], signal.SIGCONT)
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        nodes = adrv.nodes()
        labels = [adrv.label(node) for node in nodes]
        if completed_result(names_from_nodes(run, nodes), [], labels,
                            loading=any(node['cls'] == 'ProgressBar' for node in nodes),
                            fresh_empty_refresh=True):
            return True
        time.sleep(.3)
    raise AssertionError(('Native refresh did not complete with filtered empty state', labels))


def expect_names(run, expected, scenario, params=None):
    # Returning from a sheet can restore the grid search focus and reopen IME.
    # Grid scrolling must never turn into gesture typing on that keyboard.
    dismiss_keyboard()
    fresh_empty = completed_empty_refresh(run) if not expected else False
    deadline = time.monotonic() + 20
    ready = False
    while time.monotonic() < deadline:
        nodes = adrv.nodes()
        actual = names_on_grid(run) if expected else names_from_nodes(run, nodes)
        # Read completion/error semantics after any scrolling.
        nodes = adrv.nodes()
        labels = [adrv.label(node) for node in nodes]
        ready = completed_result(actual, expected, labels,
            loading=any(node['cls'] == 'ProgressBar' for node in nodes),
            fresh_empty_refresh=fresh_empty)
        if ready:
            break
        time.sleep(.5)
    assert ready, (scenario, actual, expected, labels)
    if params is not None:
        params = dict(search='f03-', set_id=run.state['set_ids']['Music'], **params)
        rows = run.viewer.request('GET', '/api/v1/media?' + urlencode(params))
        server_names = {Path(row['file_name']).stem for row in rows}
        assert server_names == set(expected), (server_names, expected)
    run.record(scenario, True, data={'visible': sorted(actual), 'expected': sorted(expected),
               'fresh_completed_empty_refresh': fresh_empty},
               artifacts=[run.shot('f03-' + re.sub(r'[^a-z0-9]+', '-', scenario.lower()))])


@contextmanager
def temporary_music_owner(run):
    """Grant only Music for the UI edit; restore the captured role on every exit."""
    matrix = run.admin.request('GET', '/api/admin/permissions')
    user = next(user for user in matrix['users'] if user['username'] == run.auth['user']['username'])
    target = {'set_id': run.state['set_ids']['Music'], 'user_id': user['id']}
    original = next((row for row in matrix['permissions']
                     if row['set_id'] == target['set_id'] and row['user_id'] == target['user_id']), None)
    assert original and original['role'] == 'viewer', original
    try:
        run.admin.request('POST', '/api/admin/permissions', dict(target, role='owner'))
        yield
    finally:
        run.admin.request('POST', '/api/admin/permissions', dict(target, role=original['role']))
        restored = run.admin.request('GET', '/api/admin/permissions')['permissions']
        row = next(row for row in restored if row['set_id'] == target['set_id'] and row['user_id'] == target['user_id'])
        assert row['role'] == original['role'], row


def add_native_tag(tag, expected_error=None):
    dismiss_keyboard()
    visible('^Add tag$', True)
    fields = [n for n in adrv.nodes() if n['cls'] == 'EditText'
              and n['b'][2] > n['b'][0] and n['b'][3] > n['b'][1]]
    assert len(fields) == 1, ('Expected unique visible detail tag input', fields)
    adrv.tap_node(fields[0])
    app_ui.clear_field(100)
    adrv.type_text(tag)
    tap('^Add tag$')
    error_node = None
    if expected_error:
        # Read the real short-lived Snackbar before Back/IME animation can
        # consume its display interval. API state alone is not a visible error.
        error_node = adrv.wait(expected_error, timeout=8)
        assert error_node, app_ui.labels()
    dismiss_keyboard()
    return error_node


def remove_native_tag(tag):
    label = visible('^' + re.escape(tag) + r'(?: /|$)', True)
    left, top, right, bottom = label['b']
    # Chip delete controls have the platform's generic Delete tooltip. Couple
    # the actual labeled Delete node to this visible chip's row and position.
    candidates = [node for node in adrv.nodes()
                  if adrv.label(node) == 'Delete' and
                  abs((node['b'][1] + node['b'][3]) / 2 - (top + bottom) / 2) <= max(40, bottom - top) and
                  left <= node['b'][0] <= right + 200]
    assert candidates, (tag, app_ui.labels())
    adrv.tap_node(min(candidates, key=lambda node: abs(node['b'][0] - right)))


def open_grid():
    assert app_ui.go_home(), app_ui.labels()
    tap('^Music$')
    assert adrv.wait('Current set: Music'), app_ui.labels()


def journey(run):
    open_grid()
    query('f03-')
    all_names = ['f03-a', 'f03-b', 'f03-c', 'f03-zero']
    expect_names(run, all_names, 'query alone', {})
    cases = [
        ('keep tag', (), ('F03 keep',), ['f03-a', 'f03-b', 'f03-zero'], {'tags': 'F03 keep'}),
        ('calm tag', (), ('F03 calm',), ['f03-a', 'f03-c', 'f03-zero'], {'tags': 'F03 calm'}),
        ('AND tags', (), ('F03 keep', 'F03 calm'), ['f03-a', 'f03-zero'], {'tags': 'F03 keep,F03 calm'}),
        ('inclusive minimum duration', ((0, 1),), (), ['f03-a', 'f03-b', 'f03-c'], {'min_duration': 60}),
        ('inclusive maximum duration', ((1, 1),), (), ['f03-a', 'f03-c', 'f03-zero'], {'max_duration': 60}),
        ('equal duration endpoints', ((0, 1), (1, 1)), (), ['f03-a', 'f03-c'], {'min_duration': 60, 'max_duration': 60}),
        ('explicit zero duration', ((0, 0), (1, 0)), (), ['f03-zero'], {'min_duration': 0, 'max_duration': 0}),
        ('inclusive minimum size', ((2, 1),), (), all_names, {'filesize_min': 1048576}),
        ('inclusive maximum size', ((3, 1),), (), ['f03-a', 'f03-zero'], {'filesize_max': 1048576}),
        ('equal size endpoints', ((2, 2), (3, 2)), (), ['f03-b', 'f03-c'], {'filesize_min': 2097152, 'filesize_max': 2097152}),
        ('explicit zero size no match', ((2, 0), (3, 0)), (), [], {'filesize_min': 0, 'filesize_max': 0}),
        ('all advanced dimensions', ((0, 1), (1, 1), (2, 1), (3, 1)), ('F03 keep', 'F03 calm'), ['f03-a'],
         {'min_duration': 60, 'max_duration': 60, 'filesize_min': 1048576, 'filesize_max': 1048576, 'tags': 'F03 keep,F03 calm'}),
    ]
    for title, bounds, tags, expected, params in cases:
        apply_case(bounds, tags)
        expect_names(run, expected, title, params)
    tap('^Audio$')
    tap('^Show favourites only$')
    expect_names(run, ['f03-a'], 'query type favorites AND advanced combination')
    # Cancel/back leaves applied state unchanged; invalid draft cannot commit.
    tap('^Filters$')
    bound('Minimum duration (minutes)', -1)
    tap('^Apply$')
    visible('Enter a finite number')
    app_ui.back()
    expect_names(run, ['f03-a'], 'invalid draft and Back preserve applied filter')
    controls(run, prepared=True)


def controls(run, prepared=False):
    if not prepared:
        open_grid()
        apply_case(bounds=((0, 1), (1, 1), (2, 1), (3, 1)), tags=('F03 keep', 'F03 calm'))
        tap('^Audio$')
        tap('^Show favourites only$')
        expect_names(run, ['f03-a'], 'continued controls: restore accepted combined filter')
    # Shared tags are owner-only. Verify viewer denial before a brief scoped grant.
    tap('^f03-a(?: /|$)', True)
    assert adrv.wait('^Play Audio$', timeout=15), app_ui.labels()
    identity = run.state['media_ids']['Music/F03/f03-a.wav']
    status, _ = run.viewer.call('POST', f'/api/v1/media/{identity}/tags', {'tag': 'F03 forbidden viewer edit'})
    assert status == 403, status
    denial = add_native_tag('F03 forbidden viewer edit', r'Server error \(403\)')
    detail = run.viewer.request('GET', f'/api/v1/media/{identity}')
    assert 'F03 forbidden viewer edit' not in [tag['name'] for tag in detail['tags']], detail
    assert not any('F03 forbidden viewer edit' in label for label in app_ui.labels()), app_ui.labels()
    run.record('viewer shared-tag edit rejected, authoritative API boundary 403', True,
               data={'visible_error': adrv.label(denial)})
    with temporary_music_owner(run):
        add_native_tag('F03 edited')
        detail = run.viewer.request('GET', f'/api/v1/media/{identity}')
        assert 'F03 edited' in [tag['name'] for tag in detail['tags']], detail
        visible('^F03 edited(?: /|$)', True)
        remove_native_tag('F03 edited')
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            detail = run.viewer.request('GET', f'/api/v1/media/{identity}')
            if 'F03 edited' not in [tag['name'] for tag in detail['tags']]:
                break
            time.sleep(.2)
        assert 'F03 edited' not in [tag['name'] for tag in detail['tags']], detail
    app_ui.back()
    expect_names(run, ['f03-a'], 'owner-only shared tag add-remove and Back refresh; exact viewer role restored')
    scopes(run, prepared=True)


def scopes(run, prepared=False):
    all_names = ['f03-a', 'f03-b', 'f03-c', 'f03-zero']
    if not prepared:
        open_grid()
    apply_case(other=True)
    expect_names(run, all_names + ['f03-other'], 'selected sets union replaces route intersection')
    visible('Selected sets:')
    apply_case(all_sets=True)
    expect_names(run, all_names + ['f03-other'], 'all visible sets excludes restricted set')
    visible('All visible sets')
    for kind, expected in [('Image', ['f03-zero']), ('Video', []), ('Audio', ['f03-a', 'f03-b', 'f03-c', 'f03-other']), ('All', all_names + ['f03-other'])]:
        tap('^' + kind + '$')
        expect_names(run, expected, 'type ' + kind)
    current_sort = 'Default'
    for label in ['Name', 'Date', 'Duration', 'Play count', 'Random', 'Default']:
        # This popup's merged native semantics expose its current visible label.
        tap('^' + re.escape(current_sort) + '$')
        tap('^' + label + '$')
        expect_names(run, all_names + ['f03-other'], 'sort ' + label)
        current_sort = label
    tap('^Clear filters$')
    visible('Current set: Music')
    query('f03-')
    expect_names(run, all_names, 'Clear restores current set and removes all constraints')
    for theme, mode in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
        time.sleep(2)
        tap('^Filters$')
        visible('F03 long label', True)
        run.record(theme + ' narrow sheet and long option labels', True,
                   artifacts=[run.shot('f03-sheet-' + theme)])
        tap('^Cancel$')


def errors(run):
    open_grid()
    apply_case(bounds=((1, 1),), tags=('F03 keep',))
    expect_names(run, ['f03-a', 'f03-zero'], 'applied filter before offline')
    # Stop only the fixture's identity-checked process; restore it in finally.
    state = json.loads((run.fixture_dir / 'state.json').read_text())
    server = state['server']
    stop_pid(server['pid'], server['binary'], start=server.get('start'))
    try:
        tap('^Filters$')
        assert adrv.wait('Could not load sets and tags', timeout=45), app_ui.labels()
        run.record('real offline filter options retain draft with Retry', True,
                   artifacts=[run.shot('f03-offline-options')])
    finally:
        instance = PlayerServer(server['binary'], run.fixture_dir, state['port'])
        state['server'].update(started_process(instance.start(detached=True)))
        save_state(run.fixture_dir, state)
    tap('^Retry filter options$')
    assert adrv.wait('^F03 Other$', timeout=25), app_ui.labels()
    tap('^Apply$')
    query('f03-')
    expect_names(run, ['f03-a', 'f03-zero'], 'offline Retry preserves scope tag and duration')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--emulator-wav')
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--scenario', choices=['all', 'journey', 'controls', 'scopes', 'errors'], default='all')
    args = parser.parse_args(argv)
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    run = None
    try:
        baseline.prepare_device(args, port)
        run = Run(args)
        assert 'f03' in run.state, 'Run music_filters.py augmentation first'
        run.viewer = Client(run.state['origin']).login(**run.auth['user'])
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'], run.auth['user']['password'],
            lambda name, ok, detail='': run.record('sign-in: ' + name, ok, detail), run.shot)
        continuations = {'controls': controls, 'scopes': scopes}
        steps = [(args.scenario, continuations[args.scenario])] if args.scenario in continuations else [('journey', journey), ('errors', errors)]
        for name, step in steps:
            if args.scenario not in ('all', name):
                continue
            try:
                if name == 'errors' and args.scenario == 'errors':
                    assert app_ui.go_home()
                    tap('^Music$')
                step(run)
            except Exception:
                trace = run.evidence.path('f03-failed-' + name + '.txt')
                trace.write_text(traceback.format_exc())
                run.record(name, False, traceback.format_exc(), artifacts=[run.shot('f03-failed-' + name), trace])
    finally:
        if run:
            baseline.teardown(run, port)
    return 1 if not run or run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
