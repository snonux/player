#!/usr/bin/env python3
"""A06d native automatic-scan acceptance on one leased API34 emulator.

Requires a disposable music fixture started with SCAN_WATCH_SECONDS=10 and
SCAN_INTERVAL_MINUTES=1, an installed release APK, and adb reverse. This runner
clears only Player app data, never stops the fixture server or emulator, and
restores the prior reverse route on exit.
"""

import argparse
import json
from pathlib import Path
import subprocess
import time
import traceback

import adrv
import app_ui
import music_e2e as baseline
from fixture_server import Client


def labels():
    return app_ui.labels()


def touch(pattern):
    node = baseline.find_visible(pattern)
    assert node, (pattern, labels())
    adrv.tap_node(node)
    time.sleep(.35)


def wait(pattern, timeout=25):
    assert adrv.wait(pattern, timeout=timeout), (pattern, labels())


class Run(baseline.Run):
    def __init__(self, args):
        super().__init__(args, evidence_dir=str(Path(args.evidence_dir) / ('native-' + str(time.time_ns()))))
        self.failures = 0
        self.admin = Client(self.state['origin']).login(**self.auth['admin'])

    def record(self, scenario, ok=True, detail='', data=None, artifacts=()):
        self.evidence.record('A06d', scenario, 'pass' if ok else 'fail', detail, data, artifacts)
        print(('PASS ' if ok else 'FAIL ') + scenario + (f' [{detail}]' if detail else ''), flush=True)
        if not ok:
            self.failures += 1

    def progress(self):
        return self.admin.request('GET', '/api/v1/admin/scan-progress')

    def rows(self):
        return self.admin.request('GET', f'/api/v1/media?set_id={self.state["set_ids"]["Music"]}&limit=1000')

    def open_rescan(self):
        assert app_ui.go_home(), labels()
        touch('^Settings$')
        wait('^Settings$')
        for _ in range(12):
            node = adrv.find(r'^Rescan Library(?: /|$)')
            if node and node['b'][3] > node['b'][1]:
                adrv.tap_node(node)
                wait(r'^Rescan Library$')
                return
            app_ui.swipe(up=True)
        raise AssertionError(('Rescan Library missing', labels()))


def retag(source, stage, target, title):
    subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-y', '-i', str(source),
                    '-map', '0:a', '-c:a', 'copy', '-metadata', f'title={title}', str(stage)],
                   check=True, timeout=30)
    stage.replace(target)


def journey(run):
    media_root = run.fixture_dir / 'media'
    folder = media_root / 'Music' / 'Tag Verification'
    source = folder / 'source_001.flac'
    target = folder / f'a06d-native-{time.time_ns()}.flac'
    stage = folder / '.a06d-native-stage.flac'
    rel = f'Tag Verification/{target.name}'
    root_offline = False
    try:
        assert app_ui.connect_and_login(run.base, run.auth['admin']['username'],
                                        run.auth['admin']['password'],
                                        lambda *_: None, run.shot), labels()
        run.record('signed in as fixture admin')
        run.open_rescan()
        touch(r'^Scan settings')
        assert baseline.find_visible('polling every 10 seconds'), labels()
        assert baseline.find_visible('every 1 minutes'), labels()
        assert baseline.find_visible('Configure on the server'), labels()
        run.record('Scan settings show effective watch and schedule',
                   artifacts=[run.shot('a06d-settings')])
        initial = run.progress()
        assert initial['watch_seconds'] == 10 and initial['interval_minutes'] == 1, initial
        # Fixture has already been running; accept a completed schedule or the
        # next one within a minute of the opt-in interval.
        progress = run.progress()
        if not (progress.get('trigger') == 'schedule' and progress.get('state') == 'complete'):
            deadline = time.monotonic() + 85
            while time.monotonic() < deadline:
                progress = run.progress()
                if progress.get('trigger') == 'schedule' and progress.get('state') == 'complete':
                    break
                time.sleep(1)
            else:
                raise AssertionError(('scheduled scan did not complete', run.progress()))
        assert baseline.find_visible('Scan complete') or baseline.find_visible('Idle') or baseline.find_visible('Files scanned'), labels()
        run.record('scheduled inventory completed with visible status',
                   artifacts=[run.shot('a06d-scheduled')])

        retag(source, stage, target, 'A06d Native Arrival')
        added = None
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            added = next((row for row in run.rows() if row.get('rel_path') == rel), None)
            if added and (added.get('music') or {}).get('title') == 'A06d Native Arrival':
                break
            time.sleep(1)
        else:
            raise AssertionError(('watch did not index arrival', rel, run.progress()))
        assert not added.get('deleted_at'), added
        assert run.progress().get('trigger') == 'watch', run.progress()
        assert baseline.open_track('Music', 'Tag Verification', 'A06d Native Arrival'), labels()
        run.record('watch discovery surfaces the new track in native browse',
                   data={'media_id': added['id']}, artifacts=[run.shot('a06d-arrival')])

        retag(target, stage, target, 'A06d Native Retagged')
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            row = next((item for item in run.rows() if item['id'] == added['id']), None)
            if row and (row.get('music') or {}).get('title') == 'A06d Native Retagged':
                break
            time.sleep(1)
        else:
            raise AssertionError(('watch did not retag', added['id'], run.progress()))
        assert len([row for row in run.rows() if row.get('rel_path') == rel]) == 1
        run.record('watch retag refreshes the existing catalog id')

        before_total = run.progress()['files_total']
        target.unlink()
        deadline = time.monotonic() + 25
        while time.monotonic() < deadline:
            progress = run.progress()
            if (progress.get('trigger') == 'watch' and progress.get('state') == 'complete'
                    and progress.get('files_total', before_total) < before_total):
                break
            time.sleep(1)
        else:
            raise AssertionError(('watch delete did not finish', run.progress()))
        retained = next(row for row in run.rows() if row['id'] == added['id'])
        assert not retained.get('deleted_at'), retained
        run.record('automatic inventory retains the absent row')

        run.open_rescan()
        touch('^Trigger Rescan$')
        deadline = time.monotonic() + 40
        while time.monotonic() < deadline:
            progress = run.progress()
            if progress.get('trigger') == 'manual' and progress.get('state') == 'complete':
                break
            if progress.get('running'):
                # Prove cancel is reachable while a long scan could run.
                cancel = adrv.find('^Cancel scan$')
                if cancel:
                    run.record('Cancel scan control visible while running')
            time.sleep(.5)
        else:
            raise AssertionError(('manual rescan did not complete', run.progress()))
        trash = run.admin.request('GET', '/api/v1/admin/trash')
        assert any(row.get('id') == added['id'] for row in trash), trash
        run.record('manual rescan soft-deletes the absent row into trash',
                   artifacts=[run.shot('a06d-manual')])

        media_offline = Path(str(media_root) + '-offline')
        media_root.rename(media_offline)
        root_offline = True
        deadline = time.monotonic() + 25
        while time.monotonic() < deadline:
            err = run.progress().get('watch_error') or ''
            if 'Could not inspect' in err:
                break
            time.sleep(1)
        else:
            raise AssertionError(('watch error missing', run.progress()))
        assert baseline.find_visible('Could not inspect') or baseline.find_visible('Check root'), labels()
        media_offline.rename(media_root)
        root_offline = False
        deadline = time.monotonic() + 25
        while time.monotonic() < deadline:
            if not (run.progress().get('watch_error') or ''):
                break
            time.sleep(1)
        else:
            raise AssertionError(('watch error stuck', run.progress()))
        run.record('disconnected root surfaces an actionable watch error and recovers',
                   artifacts=[run.shot('a06d-recovered')])

        app_ui.back()
        wait('^Settings$')
        run.record('Back leaves Rescan through native navigation')
        app_ui.back()
        assert app_ui.go_home(), labels()
    finally:
        if root_offline and Path(str(media_root) + '-offline').exists():
            Path(str(media_root) + '-offline').rename(media_root)
        if stage.exists():
            stage.unlink()
        if target.exists():
            target.unlink()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--evidence-dir', required=True)
    parser.add_argument('--apk', required=True)
    args = parser.parse_args()
    args.host_alias = False
    fixture = Path(args.fixture_dir)
    state = json.loads((fixture / 'state.json').read_text())
    assert (fixture / '.player-music-fixture').is_file()
    assert state['origin'].startswith('http://127.0.0.1:')
    previous = None
    for line in adrv.adb('reverse', '--list').splitlines():
        parts = line.split()
        if len(parts) >= 3 and parts[1] == f'tcp:{state["port"]}':
            previous = parts[2]
    baseline.prepare_device(args, state['port'])
    run = Run(args)
    try:
        journey(run)
    except Exception:
        run.record('native automatic scan journey', False, traceback.format_exc(),
                   artifacts=[run.shot('failure')])
    finally:
        if previous is None:
            try:
                adrv.adb('reverse', '--remove', f'tcp:{state["port"]}')
            except Exception:
                pass
        else:
            adrv.adb('reverse', f'tcp:{state["port"]}', previous)
    print(f'A06d native failures: {run.failures}', flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    raise SystemExit(main())
