#!/usr/bin/env python3
"""Native startup/default-polling acceptance on an owned music fixture.

Restarts only the marked localhost fixture and clears only Player app data.
Requires one leased online emulator; optionally installs a normal debug APK.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time
import traceback

import adrv
import app_ui
import music_e2e as baseline
import music_scan_automation_e2e as automation


def wait_for(check, timeout=70):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = check()
        if value:
            return value
        time.sleep(1)
    raise AssertionError('Quickscan did not finish before timeout')


def browse_song(title):
    """Find the arriving song in native browsing without starting playback."""
    if not app_ui.go_home() or not adrv.tap('^Music$'):
        return False
    time.sleep(3)
    # Search keeps this bounded even when the fixture has many retained rows.
    # Visibility needs positive bounds; the shared tappable-card helper's
    # minimum-height rule does not apply to compact text labels.
    field = adrv.find('', cls='EditText')
    if not field:
        return False
    adrv.tap_node(field)
    app_ui.clear_field(100)
    adrv.type_text(title)
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_BACK')
    for _ in range(8):
        for node in adrv.nodes():
            if (node['cls'] != 'EditText' and title in adrv.label(node)
                    and node['b'][2] > node['b'][0] and node['b'][3] > node['b'][1]):
                return True
        time.sleep(.5)
    return False


def journey(run):
    folder = run.fixture_dir / 'media' / 'Music' / 'Tag Verification'
    source = folder / 'source_001.flac'
    stamp = time.time_ns()
    startup = folder / f'pz3-native-{stamp}-startup.flac'
    watched = folder / f'pz3-native-{stamp}-watched.flac'
    stage = folder / '.pz3-native-stage.flac'
    startup_title, watched_title = f'PZ3 Startup {stamp}', f'PZ3 Watched {stamp}'

    def restart(polling):
        env = dict(os.environ)
        env.pop('SCAN_WATCH_SECONDS', None)
        env.pop('SCAN_INTERVAL_MINUTES', None)
        if not polling:
            env.update(SCAN_WATCH_SECONDS='0', SCAN_INTERVAL_MINUTES='0')
        subprocess.run(['python3', str(baseline.SUPPORT / 'music_fixture.py'),
                        'restart', str(run.fixture_dir)], check=True,
                       capture_output=True, env=env, timeout=45)
        wait_for(lambda: not run.progress()['running'])

    try:
        restart(False)
        automation.retag(source, stage, startup, startup_title)
        restart(False)
        added = wait_for(lambda: next((r for r in run.rows() if r['file_name'] == startup.name), None))
        assert run.progress()['trigger'] == 'startup'
        assert run.progress()['watch_seconds'] == 0
        assert app_ui.connect_and_login(run.base, run.auth['admin']['username'],
                                        run.auth['admin']['password'],
                                        record=lambda *_: None, shot=run.shot), app_ui.labels()
        assert added['music']['title'] == startup_title
        assert browse_song(startup_title), app_ui.labels()
        run.record('startup inventory with polling disabled appears in native browse',
                   data={'media_id': added['id']}, artifacts=[run.shot('pz3-startup')])
        restart(True)
        # Let the initial startup-boundary follow-up finish before adding a
        # source, so only a subsequent ordinary poll can discover this file.
        wait_for(lambda: (lambda p: p['trigger'] == 'watch' and p['state'] == 'complete'
                          and not p['running'] and not p['pending'])(run.progress()))
        assert run.progress()['watch_seconds'] == 30 and run.progress()['interval_minutes'] == 0
        automation.retag(source, stage, watched, watched_title)
        added = wait_for(lambda: next((r for r in run.rows() if r['file_name'] == watched.name), None))
        wait_for(lambda: not run.progress()['running'])
        assert run.progress()['trigger'] == 'watch'
        assert added['music']['title'] == watched_title
        assert browse_song(watched_title), app_ui.labels()
        run.record('default 30-second polling appears in native browse without Rescan',
                   data={'media_id': added['id']}, artifacts=[run.shot('pz3-watched')])
        run.open_rescan()
        automation.touch(r'^Scan settings')
        assert baseline.find_visible('polling every 30 seconds'), app_ui.labels()
        assert baseline.find_visible('A quick scan runs at startup'), app_ui.labels()
        run.record('native settings explain startup and default polling', artifacts=[run.shot('pz3-settings')])
    finally:
        for path in (startup, watched, stage):
            path.unlink(missing_ok=True)


class Run(automation.Run):
    def open_rescan(self):
        assert app_ui.go_home(), app_ui.labels()
        assert adrv.tap('^Settings$'), app_ui.labels()
        # Administration is a focused category in the current Settings UI.
        for _ in range(6):
            category = adrv.find(r'^Administration(?: /|$)')
            if category and category['b'][3] > category['b'][1]:
                adrv.tap_node(category)
                break
            app_ui.swipe(up=True)
        else:
            raise AssertionError(('Administration missing', app_ui.labels()))
        assert adrv.tap(r'^Rescan Library(?: /|$)'), app_ui.labels()
        assert adrv.wait('^Rescan Library$'), app_ui.labels()

    def record(self, scenario, ok=True, detail='', data=None, artifacts=()):
        self.evidence.record('pz3', scenario, 'pass' if ok else 'fail', detail, data, artifacts)
        print(('PASS ' if ok else 'FAIL ') + scenario + (' ' + detail if detail else ''), flush=True)
        if not ok:
            self.failures += 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--evidence-dir', required=True)
    parser.add_argument('--apk')
    args = parser.parse_args()
    args.host_alias, args.emulator_wav = False, None
    state = json.loads((Path(args.fixture_dir) / 'state.json').read_text())
    assert (Path(args.fixture_dir) / '.player-music-fixture').is_file()
    assert state['origin'].startswith('http://127.0.0.1:')
    previous = next((line.split()[2] for line in adrv.adb('reverse', '--list').splitlines()
                     if len(line.split()) >= 3 and line.split()[1] == f'tcp:{state["port"]}'), None)
    baseline.prepare_device(args, state['port'])
    run = Run(args)
    try:
        journey(run)
    except Exception:
        run.record('native quickscan journey', False, traceback.format_exc(), artifacts=[run.shot('pz3-failure')])
    finally:
        if previous:
            adrv.adb('reverse', f'tcp:{state["port"]}', previous)
        else:
            adrv.adb('reverse', '--remove', f'tcp:{state["port"]}')
    return 1 if run.failures else 0


if __name__ == '__main__':
    raise SystemExit(main())
