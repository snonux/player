#!/usr/bin/env python3
"""M57 Locale-compatible fire-host acceptance on one leased emulator.

Requires: disposable music fixture, installed Player x86_64 APK, installed
zone.foo.localefire host APK, QEMU wav optional. Signs in as fixture admin,
queues one Music track, configures Start through the Locale EDIT_SETTING
Activity via the local host, fires FIRE_SETTING, and checks real session
playback plus Settings last-result text.
"""

import argparse
import json
from pathlib import Path
import time
import traceback

import adrv
import app_ui
import music_e2e as baseline


HOST = 'zone.foo.localefire'
HOST_ACTIVITY = 'zone.foo.localefire/.HostActivity'


def labels():
    return app_ui.labels()


def touch(pattern):
    node = baseline.find_visible(pattern)
    assert node, (pattern, labels())
    adrv.tap_node(node)
    time.sleep(.4)


class Run(baseline.Run):
    def __init__(self, args):
        super().__init__(args, evidence_dir=str(Path(args.evidence_dir) / ('native-' + str(time.time_ns()))))
        self.failures = 0

    def record(self, scenario, ok=True, detail='', data=None, artifacts=()):
        self.evidence.record('M57', scenario, 'pass' if ok else 'fail', detail, data, artifacts)
        print(('PASS ' if ok else 'FAIL ') + scenario + (f' [{detail}]' if detail else ''), flush=True)
        if not ok:
            self.failures += 1


def ensure_paused():
    for _ in range(3):
        if baseline.session_state().get('state') == 'PAUSED':
            return
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            if baseline.session_state().get('state') == 'PAUSED':
                return
            time.sleep(.3)
    raise AssertionError(('expected PAUSED', baseline.session_state()))


def save_locale_action(run, action_pattern, label):
    adrv.adb('shell', 'am', 'start', '-W', '-n', HOST_ACTIVITY)
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        try:
            if adrv.find('^Edit Player setting$'):
                break
        except RuntimeError:
            time.sleep(1)
            continue
        time.sleep(.5)
    else:
        raise AssertionError(('Locale host UI missing', labels()))
    touch('^Edit Player setting$')
    deadline = time.monotonic() + 40
    while time.monotonic() < deadline:
        try:
            if adrv.find('^Save action$') or adrv.find(action_pattern):
                break
        except RuntimeError:
            time.sleep(1)
            continue
        time.sleep(.5)
    else:
        raise AssertionError(('EDIT_SETTING UI missing', labels()))
    if adrv.find(action_pattern):
        touch(action_pattern)
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        node = adrv.find('^Save action$')
        if node and node.get('enabled', True) is not False:
            adrv.tap_node(node)
            time.sleep(1)
            try:
                if adrv.find(r'^Saved:') or adrv.find('^Fire saved setting$'):
                    break
            except RuntimeError:
                time.sleep(1)
                continue
        time.sleep(.8)
    else:
        raise AssertionError(('Save action did not return a capability', labels()))
    run.record(f'Locale host saved {label} capability',
               artifacts=[run.shot('m57-configured-' + label.replace(' ', '-').lower())])


def fire_saved_setting(run, expect_playing):
    if expect_playing:
        ensure_paused()
    adrv.adb('shell', 'am', 'start', '-W', '-n', 'zone.foo.player_android/.MainActivity')
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        try:
            labs = labels()
            if 'First Light' in ' '.join(labs) or adrv.find('^Settings$'):
                break
        except RuntimeError:
            time.sleep(1)
            continue
        time.sleep(.5)
    time.sleep(2)
    before = baseline.session_state()
    adrv.adb('shell', 'am', 'broadcast', '-a', 'zone.foo.localefire.FIRE_NOW',
             '-n', 'zone.foo.localefire/.FireNowReceiver')
    if expect_playing:
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            state = baseline.session_state()
            if state.get('state') == 'PLAYING':
                break
            time.sleep(.4)
        after = baseline.session_state()
        # Pause before Settings so short fixture tracks don't advance mid-check.
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            if baseline.session_state().get('state') == 'PAUSED':
                break
            time.sleep(.3)
    else:
        time.sleep(2)
        after = baseline.session_state()
    open_automation_settings(run)
    result_labels = labels()
    last = next((text for text in result_labels
                 if text.startswith('Last automation result:')), '')
    if not last:
        last = next((text for text in result_labels
                     if text in (
                         'Queued music is playing.',
                         'Shuffled queued music is playing.',
                         'Music automation now uses offline copies only.',
                         'Music automation now allows online music with your normal network settings.',
                     ) or text.startswith('Music did not start') or
                     text.startswith('Music queue is busy') or
                     text.startswith('Download every queued') or
                     text.startswith('Another source') or
                     text.startswith('Open Player and queue')), '')
    if not last:
        last = ' | '.join(result_labels[-8:])
    if expect_playing:
        success = after.get('state') == 'PLAYING' and 'playing' in last.lower()
        if not success:
            raise AssertionError(('fire did not start playback', before, after, last, result_labels))
    run.record('Settings Music automation last result after fire',
               ok=True, detail=last, data={'session': after},
               artifacts=[run.shot('m57-settings')])
    app_ui.back()
    assert app_ui.go_home(), labels()
    time.sleep(.8)
    return after, last


def open_automation_settings(run):
    assert app_ui.go_home(), labels()
    touch('^Settings$')
    # Music automation sits below Headphones near the end of Settings.
    for _ in range(28):
        try:
            node = adrv.find(r'Music automation')
        except RuntimeError:
            time.sleep(1)
            continue
        if node and node['b'][3] > node['b'][1] and node['b'][1] < 1900:
            adrv.tap_node(node)
            time.sleep(.5)
            break
        app_ui.swipe(up=True)
        time.sleep(.25)
    else:
        raise AssertionError(('Music automation missing', labels()))
    for _ in range(10):
        try:
            joined = ' | '.join(labels())
        except RuntimeError:
            time.sleep(1)
            continue
        if ('Refresh result' in joined or 'Revoke saved actions' in joined or
                'Last automation result' in joined or 'Queued music is playing' in joined or
                'Shuffled queued music' in joined):
            try:
                if adrv.find('^Refresh result$'):
                    touch('^Refresh result$')
                    time.sleep(.6)
            except RuntimeError:
                pass
            return
        app_ui.swipe(up=True)
        time.sleep(.25)


def journey(run):
    assert app_ui.connect_and_login(run.base, run.auth['admin']['username'],
                                    run.auth['admin']['password'],
                                    lambda *_: None, run.shot), labels()
    run.record('signed in as fixture admin')
    # Folder "Play Audio" uses wrap traversal and does not own the interactive
    # Now Playing queue that Locale Start resumes. Seed via Play album instead.
    assert baseline.open_track('Music', 'Aurora Lane - Night Transit (2021)', 'First Light'), labels()
    touch('^Open album$')
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        try:
            if adrv.find('^Play album$'):
                break
        except RuntimeError:
            time.sleep(1)
            continue
        time.sleep(.4)
    else:
        raise AssertionError(('album screen missing Play album', labels()))
    touch('^Play album$')
    # Avoid uiautomator dumps while Now Playing animates — they wait for idle
    # and hang past the short fixture track. Use media session + media key.
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline:
        state = baseline.session_state()
        if state.get('title') == 'First Light' and state.get('state') == 'PLAYING':
            break
        time.sleep(.3)
    else:
        raise AssertionError(('queue seed failed', baseline.session_state()))
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        state = baseline.session_state()
        if state.get('title') == 'First Light' and state.get('state') == 'PAUSED':
            break
        time.sleep(.3)
    else:
        raise AssertionError(('queue seed pause failed', baseline.session_state()))
    run.record('seeded Now Playing queue with First Light via Play album',
               artifacts=[run.shot('m57-queued')])

    save_locale_action(run, '^Start queued music$', 'Start queued music')
    playing, last = fire_saved_setting(run, expect_playing=True)
    run.record('FIRE_SETTING started audible queued music',
               data={'session': playing, 'last_result': last},
               artifacts=[run.shot('m57-fired')])
    ensure_paused()

    save_locale_action(run, '^Shuffle and start queued music$', 'Shuffle and start')
    playing, last = fire_saved_setting(run, expect_playing=True)
    if 'shuffled' not in last.lower():
        raise AssertionError(('shuffle last-result missing', last, playing))
    run.record('FIRE_SETTING shuffle started audible queued music',
               detail=last, data={'session': playing},
               artifacts=[run.shot('m57-shuffle')])
    ensure_paused()

    # Policy actions do not start audio; verify last-result text only.
    save_locale_action(run, '^Use offline music only$', 'Use offline music only')
    _, last = fire_saved_setting(run, expect_playing=False)
    if 'offline copies only' not in last.lower():
        raise AssertionError(('offline policy last-result missing', last))
    run.record('FIRE_SETTING offline policy saved without requiring downloads yet',
               detail=last, artifacts=[run.shot('m57-offline')])
    save_locale_action(run, '^Allow online music$', 'Allow online music')
    _, last = fire_saved_setting(run, expect_playing=False)
    if 'allows online music' not in last.lower():
        raise AssertionError(('online policy last-result missing', last))
    run.record('FIRE_SETTING allow-online policy restored',
               detail=last, artifacts=[run.shot('m57-online')])


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
    packages = adrv.adb('shell', 'pm', 'list', 'packages', HOST)
    assert HOST in packages, ('install locale-fire-host.apk first', packages)
    baseline.prepare_device(args, state['port'])
    run = Run(args)
    try:
        journey(run)
    except Exception:
        run.record('native Locale automation journey', False, traceback.format_exc(),
                   artifacts=[run.shot('failure')])
    print(f'M57 native failures: {run.failures}', flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    raise SystemExit(main())
