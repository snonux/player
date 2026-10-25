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


def visible(pattern, clickable=False):
    deadline = time.monotonic() + 25
    swipes = 0
    while time.monotonic() < deadline:
        nodes = adrv.nodes()
        candidates = matching_nodes(nodes, pattern, clickable)
        if candidates:
            if clickable:
                assert len(candidates) == 1, ('Ambiguous control', pattern, candidates)
            return candidates[0]
        if matching_nodes(nodes, '^Music filters$') and swipes < 4:
            x, top, bottom = sheet_region(nodes)
            inset = (bottom - top) // 5
            adrv.adb('shell', 'input', 'swipe', str(x), str(bottom-inset), str(x), str(top+inset), '350')
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
    node = visible(r'(?:^| / )' + re.escape(label) + r'(?: /|$)', clickable=True)
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


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk')
    parser.add_argument('--scenario', choices=['all', 'core', 'state', 'errors'], default='all')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--host-alias', action='store_true')
    args = parser.parse_args(argv)
    fixture_state = json.loads((Path(args.fixture_dir) / 'state.json').read_text())
    run = None
    try:
        baseline.prepare_device(args, fixture_state['port'])
        run = Run(args)
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'],
                                        run.auth['user']['password'],
                                        lambda name, ok, detail='': run.record(name, ok, detail), run.shot)
        for name, step in [('core', core), ('state', state), ('errors', errors)]:
            if args.scenario in ('all', name):
                step(run)
    except Exception:
        if run:
            run.record('facet journey', False, traceback.format_exc(),
                       artifacts=[run.shot('m08-failed')])
        else:
            raise
    finally:
        if run:
            baseline.teardown(run, fixture_state['port'])
    return 1 if not run or run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
