#!/usr/bin/env python3
"""M08 actual native facet sheet, applied chips, full collection and restart.

Uses the existing disposable --release-metadata fixture and stabilized driver.
No server/emulator startup or shutdown is owned by this runner.
"""
import argparse
import json
from pathlib import Path
import re
import sys
import time
import traceback

import adrv
import app_ui
import music_e2e as baseline
import music_search_e2e as search_driver
from music_search_e2e import matching_nodes
from music_queue_recovery_e2e import login_other, restart
from fixture_server import Client, PlayerServer, answers, stop_pid
from music_fixture import save_state, started_process


class Run(baseline.Run):
    failures = 0

    def record(self, scenario, ok, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M08', scenario, result, detail, data, artifacts)
        print(f'{result.upper():5} {scenario} {detail}', flush=True)
        if result == 'fail':
            self.failures += 1


def sheet_region(nodes):
    headings = matching_nodes(nodes, '^Music filters$')
    actions = matching_nodes(nodes, '^Apply$', True)
    assert len(headings) == 1 and len(actions) == 1, ('No unique sheet bounds', headings, actions)
    top, bottom = headings[0]['b'][3] + 8, actions[0]['b'][1] - 8
    width = max(node['b'][2] for node in nodes)
    assert bottom - top > 120 and width > 0, ('Invalid sheet viewport', top, bottom, width)
    return width // 2, top, bottom


def chip_row_region(nodes):
    """Bound horizontal reveal to the current applied-chip row."""
    assert matching_nodes(nodes, r'^Filters \(\d+\)$'), 'No applied filter toolbar'
    assert not matching_nodes(nodes, '^Music filters$'), 'Sheet is open'
    chips = matching_nodes(nodes, r'^(Genre|Year / decade|Role|Release type|Compilation|Mood|Grouping|Label): ')
    assert chips, 'No visible applied chips'
    left = min(node['b'][0] for node in chips) + 8
    right = max(node['b'][2] for node in chips) - 8
    top = max(node['b'][1] for node in chips)
    bottom = min(node['b'][3] for node in chips)
    assert right - left > 80 and bottom > top, ('Invalid chip row', chips)
    # A clipped final chip can end at the physical display edge, where Android
    # interprets a leftward drag as Back instead of scrolling this row.
    right -= max(24, (right - left) // 8)
    return right, left, (top + bottom) // 2


def visible(pattern, clickable=False, within_sheet=False):
    deadline = time.monotonic() + 25
    swipes = 0
    while time.monotonic() < deadline:
        nodes = adrv.nodes()
        candidates = matching_nodes(nodes, pattern, clickable)
        if within_sheet:
            _, top, bottom = sheet_region(nodes)
            candidates = [node for node in candidates
                          if top + 24 <= (node['b'][1] + node['b'][3]) // 2 <= bottom - 24]
        if candidates:
            if clickable:
                assert len(candidates) == 1, ('Ambiguous control', pattern, candidates)
            return candidates[0]
        if matching_nodes(nodes, '^Music filters$') and swipes < 4:
            x, top, bottom = sheet_region(nodes)
            inset = (bottom - top) // 5
            adrv.adb('shell', 'input', 'swipe', str(x), str(bottom-inset), str(x), str(top+inset), '350')
            swipes += 1
        elif pattern == '^Clear filters$' and swipes < 4:
            right, left, y = chip_row_region(nodes)
            adrv.adb('shell', 'input', 'swipe', str(right), str(y), str(left), str(y), '350')
            swipes += 1
        time.sleep(.3)
    raise AssertionError((pattern, app_ui.labels()))


def tap(pattern):
    node = visible(pattern, clickable=True)
    fresh = visible(pattern, clickable=True)
    assert node['b'] == fresh['b'] and adrv.label(node) == adrv.label(fresh), ('Control moved', node, fresh)
    adrv.tap_node(fresh)


def albums():
    assert app_ui.go_home(), app_ui.labels()
    tap('^Open navigation menu$')
    tap('^Music · Albums$')
    assert adrv.wait(r'^Filters \(\d+\)$', timeout=25), app_ui.labels()


def sheet():
    tap(r'^Filters \(\d+\)$')
    assert adrv.wait('^Music filters$', timeout=20), app_ui.labels()


def choose(label, value):
    # The labelled DropdownButton is selected from real current semantics;
    # no empty hint or remembered coordinate substitutes for its field.
    node = visible(r'(?:^| / )' + re.escape(label) + r'(?: /|$)', clickable=True,
                   within_sheet=True)
    adrv.tap_node(node)
    tap('^' + re.escape(value) + '$')


def apply():
    tap('^Apply$')
    assert not adrv.wait('^Music filters$', timeout=1), app_ui.labels()


def core(run):
    albums()
    sheet()
    choose('Genre', 'Ambient')
    choose('Year / decade', '2021')
    apply()
    visible(r'^1 matching albums · 1 matching songs$')
    visible('Night Transit')
    sheet()
    choose('Role', 'Composer')
    apply()
    visible('Nothing matches these filters')
    # Clear is immediate on the applied toolbar; sheet Clear remains a draft.
    tap('^Clear filters$')
    sheet()
    choose('Genre', 'Ambient')
    choose('Year / decade', '2021')
    apply()
    run.record('same-song combination, no cross-song composer, Clear', True,
               artifacts=[run.shot('m08-applied')])
    sheet()
    choose('Year / decade', '2019')
    app_ui.back()
    visible(r'^1 matching albums · 1 matching songs$')
    run.record('Back dismisses draft without applying', True)
    tap('^Clear filters$')


def state(run):
    albums()
    if adrv.find('^Clear filters$'):
        tap('^Clear filters$')
    sheet()
    choose('Genre', 'Ambient')
    choose('Year / decade', '2021')
    apply()
    visible(r'^1 matching albums · 1 matching songs$')
    restart(run)
    albums()
    visible(r'^1 matching albums · 1 matching songs$')
    login_other(run, run.auth['admin'])
    albums()
    visible(r'^Filters \(0\)$')
    login_other(run, run.auth['user'])
    albums()
    visible(r'^1 matching albums · 1 matching songs$')
    run.record('cold restart and actual account switch preserve only own filters', True,
               artifacts=[run.shot('m08-restored')])
    label(run)


def label(run):
    albums()
    if adrv.find('^Clear filters$'):
        tap('^Clear filters$')
    sheet()
    choose('Label', 'Label A; B')
    apply()
    visible('Release Metadata')
    run.record('literal punctuation release facet', True,
               artifacts=[run.shot('m08-release-label')])
    tap('^Clear filters$')


def errors(run):
    albums()
    if adrv.find('^Clear filters$'):
        tap('^Clear filters$')
    sheet()
    choose('Genre', 'Ambient')
    apply()
    viewer = Client(run.state['origin']).login(**run.auth['user'])
    admin_options = run.admin.request('GET', '/api/v1/music/facets')
    options = viewer.request('GET', '/api/v1/music/facets')
    assert 'Restricted only' in admin_options['genre'], admin_options
    assert 'Restricted only' not in options['genre'], options
    sheet()
    choose('Year / decade', '2021')
    previous = dict(run.state['server'])
    assert stop_pid(previous['pid'], previous['binary'], start=previous.get('start'))
    assert answers(run.state['origin'], '/healthz') is None
    adrv.adb('reverse', '--remove', f"tcp:{run.state['port']}")
    try:
        apply()
        assert adrv.wait('^Retry$', timeout=40), app_ui.labels()
        visible('Genre: Ambient')
        run.record('actual offline request keeps applied chips and offers Retry', True,
                   artifacts=[run.shot('m08-offline')])
    finally:
        server = PlayerServer(previous['binary'], run.fixture_dir, run.state['port'])
        run.state['server'].update(started_process(server.start(detached=True)))
        run.state['server']['restarts'] = run.state['server'].get('restarts', 0) + 1
        save_state(run.fixture_dir, run.state)
        adrv.adb('reverse', f"tcp:{run.state['port']}", f"tcp:{run.state['port']}")
    tap('^Retry$')
    visible(r'^1 matching albums · 1 matching songs$')
    uid = next(user['id'] for user in run.admin.request('GET', '/api/v1/admin/users')
               if user['username'] == run.auth['user']['username'])
    permission = {'set_id': run.state['set_ids']['Music'], 'user_id': uid}
    run.admin.request('DELETE', '/api/v1/admin/permissions', permission)
    try:
        albums()
        visible('Nothing matches these filters')
        assert not viewer.request('GET', '/api/v1/music/facets')['genre']
        assert not viewer.request('GET', '/api/v1/music/albums?genre=Ambient')
        run.record('permission revocation removes facet values and result counts', True,
                   artifacts=[run.shot('m08-acl')])
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', dict(permission, role='viewer'))
    albums()
    visible(r'^1 matching albums · 1 matching songs$')
    tap('^Clear filters$')


def detail(run):
    albums()
    if adrv.find('^Clear filters$'):
        tap('^Clear filters$')
    sheet()
    choose('Genre', 'Ambient')
    choose('Year / decade', '2021')
    apply()
    visible(r'^1 matching albums · 1 matching songs$')
    artifacts = []
    for theme, mode in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
        time.sleep(2)
        visible(r'^1 matching albums · 1 matching songs$')
        artifacts.append(run.shot('m08-filtered-catalog-' + theme))
        sheet()
        visible('Genre', clickable=True, within_sheet=True)
        artifacts.append(run.shot('m08-filter-sheet-' + theme))
        app_ui.back()
    tap(r'(^| / )Night Transit($| / )')
    visible('^Play album$')
    visible(r'(?:^| / )3 songs ·')
    visible('First Light')
    artifacts.append(run.shot('m08-full-authorized-album-dark'))
    app_ui.back()
    visible(r'^1 matching albums · 1 matching songs$')
    visible('Genre: Ambient')
    visible('Year / decade: 2021')
    run.record('filtered album opens all three authorized songs and Back restores facets; light/dark narrow catalog and sheet',
               True, artifacts=artifacts)
    tap('^Clear filters$')


def clear_applied_facets():
    """Remove persisted chips. Clear filters can sit off-screen; Delete is reliable."""
    if adrv.find('^Clear filters$'):
        tap('^Clear filters$')
        time.sleep(.5)
    for _ in range(8):
        nodes = matching_nodes(adrv.nodes(), '^Delete$', clickable=True)
        if not nodes:
            break
        adrv.tap_node(nodes[0])
        time.sleep(.6)
    assert not matching_nodes(adrv.nodes(), r'^Filters \([1-9]'), app_ui.labels()


def apply_fields(fields):
    """Choose fields in screen order so bounded sheet scrolling stays forward."""
    sheet()
    for name, value in fields:
        choose(name, value)
    apply()


def extended(run):
    albums()
    if adrv.find('^Clear filters$'):
        tap('^Clear filters$')
    apply_fields([('Genre', 'Ambient'), ('Year / decade', '2021'),
                  ('Role', 'Track artist'), ('Label', 'Fixture Records')])
    visible(r'^1 matching albums · 1 matching songs$')
    visible(r'^Filters \(4\)$')
    apply_fields([('Release type', 'album')])
    visible(r'^0 matching albums · 0 matching songs$')
    visible('Nothing matches these filters')
    apply_fields([('Release type', 'Any')])
    visible(r'^1 matching albums · 1 matching songs$')
    tap(r'(^| / )Night Transit($| / )')
    visible('^Play album$')
    visible(r'(?:^| / )3 songs ·')
    app_ui.back()
    visible(r'^Filters \(4\)$')
    visible(r'^1 matching albums · 1 matching songs$')
    run.record('simultaneous genre/year/role/release label, release-type exclusion and full album Back',
               True, artifacts=[run.shot('m08-four-facets')])
    tap('^Clear filters$')
    apply_fields([('Genre', 'Synthwave'), ('Year / decade', '2010-2019'),
                  ('Compilation', 'Marked')])
    visible('Synth Compilation')
    apply_fields([('Compilation', 'Not marked')])
    visible(r'^0 matching albums · 0 matching songs$')
    tap('^Clear filters$')
    apply_fields([('Year / decade', '2024'), ('Role', 'Track artist'),
                  ('Release type', 'live'), ('Compilation', 'Not marked'),
                  ('Mood', 'Reflective'), ('Grouping', 'Suite / One'),
                  ('Label', 'Label A; B')])
    # Only the FLAC carries repeated RELEASETYPE=live; the aliases are album.
    visible(r'^1 matching albums · 1 matching songs$')
    visible('Release Metadata')
    visible(r'^Filters \(7\)$')
    run.record('decade and marked/unmarked compilation; positive rich release type/mood/grouping/label combination',
               True, artifacts=[run.shot('m08-rich-facets')])
    tap('^Clear filters$')


def navigation(run):
    assert app_ui.go_home(), app_ui.labels()
    # Prior failed attempts can leave Music search open with applied chips.
    if adrv.find('^Clear search$'):
        tap('^Clear search$')
        app_ui.back()
    albums()
    clear_applied_facets()
    search_driver.open_search()
    search_driver.search('Harbour genre:Ambient year:2022')
    search_driver.visible(r'^Songs \(0\)$')
    search_driver.visible('Nothing matches these filters')
    search_driver.search('Harbour genre:Ambient')
    search_driver.visible(r'^Songs \(1\)$')
    search_driver.visible('^Harbour(?: /|$)')
    search_driver.search('Harbour')
    search_driver.visible(r'^Songs \(1\)$')
    search_driver.visible('^Harbour(?: /|$)')
    apply_fields([('Year / decade', '2022')])
    search_driver.visible(r'^Songs \(0\)$')
    apply_fields([('Year / decade', '2021')])
    search_driver.visible(r'^Songs \(1\)$')
    tap('^Clear filters$')
    search_driver.visible(r'^Songs \(1\)$')
    run.record('Search typed year then genre deletion reloads results without stale facets', True,
               artifacts=[run.shot('m08-search-modifier-deletion')])
    assert app_ui.go_home(), app_ui.labels()
    tap('^Open navigation menu$')
    tap('^Music · Artists$')
    visible(r'^Filters \(0\)$')
    apply_fields([('Genre', 'Synthwave'), ('Year / decade', '2019'),
                  ('Role', 'Composer')])
    visible('2 matching songs')
    tap(r'^Composer; 東京 / A(?: /|$)')
    # Artist detail puts ranked albums below Play/Share chrome; scroll if needed.
    node = app_ui.find_scrolling(r'(^| / )Synth Compilation')
    assert node, app_ui.labels()
    adrv.tap_node(node)
    visible('^Play album$')
    app_ui.back()
    visible(r'^Composer; 東京 / A(?: /|$)')
    app_ui.back()
    visible(r'^Filters \(3\)$')
    visible('2 matching songs')
    visible(r'^Composer; 東京 / A(?: /|$)')
    run.record('Artists composer/genre/year facets, matching song counts and detail Back restoration',
               True, artifacts=[run.shot('m08-artists-restored')])
    clear_applied_facets()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--scenario', choices=['all', 'core', 'state', 'errors', 'detail', 'label', 'extended', 'navigation'], default='all')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--preserve-device', action='store_true',
                        help='Use an already unlocked fixture-user session and existing connection; skip installation, initial login and all teardown resets')
    args = parser.parse_args(argv)
    if args.preserve_device and args.apk:
        parser.error('--preserve-device requires an already installed APK; omit --apk')
    fixture_state = json.loads((Path(args.fixture_dir) / 'state.json').read_text())
    run = None
    try:
        if not args.preserve_device:
            baseline.prepare_device(args, fixture_state['port'])
        run = Run(args)
        if args.preserve_device:
            app_ui.launch()
            assert app_ui.go_home(), 'Preserved device must already be unlocked and signed in to this fixture user'
        else:
            assert app_ui.connect_and_login(run.base, run.auth['user']['username'],
                                            run.auth['user']['password'],
                                            lambda name, ok, detail='': run.record(name, ok, detail), run.shot)
        if args.scenario == 'label':
            label(run)
        else:
            for name, step in [('core', core), ('state', state), ('errors', errors), ('detail', detail),
                               ('extended', extended), ('navigation', navigation)]:
                if args.scenario in ('all', name):
                    step(run)
    except Exception:
        if run:
            run.record('facet journey', False, traceback.format_exc(),
                       artifacts=[run.shot('m08-failed')])
        else:
            raise
    finally:
        if run and not args.preserve_device:
            baseline.teardown(run, fixture_state['port'])
    return 1 if not run or run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
