#!/usr/bin/env python3
"""M25 released-app touch controls and QEMU frequency output; no data reset."""
import argparse
import json
import re
import time
from pathlib import Path
from types import SimpleNamespace

import adrv
import app_ui
import music_e2e as baseline
import music_loudness_e2e as loudness


class Run(loudness.Run):
    def __init__(self, args):
        super().__init__(args)
        previous = args.evidence / 'results.json'
        if previous.exists():
            self.results = json.loads(previous.read_text())

    def settings(self):
        raw = self.preference('flutter.player-equalizer-v1')
        return json.loads(raw) if raw else {'enabled': False, 'bands': [0] * 5}

    def open_sheet(self):
        if not self.sheet:
            self.touch('^Equalizer$')
            self.sheet = True

    def preset(self, name):
        self.open_sheet()
        self.touch('Equalizer preset')
        self.touch('^' + re.escape(name) + '$')

    def output(self, mark, name):
        wav = self.audio.save(mark, self.args.evidence / (name + '.wav'))
        analysis = baseline.audio_trace.analyze(wav, [100, 1000, 8000])
        (self.args.evidence / (name + '.json')).write_text(json.dumps(analysis, indent=2))
        segment = baseline.main_segment(analysis)
        assert segment and segment['duration_s'] >= 2, analysis
        assert baseline.session_state()['title'] == 'Three Tones', baseline.session_state()
        self.last_bands = segment['bands_dbfs']
        self.record(name, bands=self.last_bands, settings=self.settings(), session=baseline.session_state())
        return self.last_bands

    def slider(self, frequency, value):
        self.open_sheet()
        thumb = self.visible(r'^' + str(frequency) + r' hertz ', 'SeekBar')
        field = self.visible('Equalizer preset')
        inset = (thumb['b'][2] - thumb['b'][0]) / 2
        left, right = field['b'][0] + inset, field['b'][2] - inset
        x = round(left + (right - left) * (value + 12) / 24)
        y = (thumb['b'][1] + thumb['b'][3]) // 2
        (self.args.evidence / 'slider-target.json').write_text(json.dumps(
            {'thumb': thumb, 'field': field, 'requested': value, 'touch': [x, y]}))
        adrv.adb('shell', 'input', 'tap', str(x), str(y))
        time.sleep(.4)
        assert self.settings()['bands'][[60, 230, 910, 3600, 14000].index(frequency)] == value, self.settings()

    def live_preset(self, name):
        self.close_sheet()
        self.touch('^Skip back 15 seconds$')
        self.open_sheet()
        self.touch('Equalizer preset')
        target = self.visible('^' + re.escape(name) + '$')
        adrv.shot(str(self.args.evidence / 'live-preset-before.png'))
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PLAY')
        self.wait_playing()
        before = baseline.session_state()
        assert before['title'] == 'Three Tones', before
        adrv.tap_node(target)
        time.sleep(.4)
        assert baseline.session_state()['state'] == 'PLAYING'
        mark = self.audio.mark()
        time.sleep(3)
        after = baseline.session_state()
        assert after['state'] == 'PLAYING' and after['title'] == 'Three Tones', after
        self.pause()
        expected = {'Voice': [-4, -2, 4, 3, -2], 'Bass boost': [6, 3, 0, -2, -4]}
        assert self.settings() == {'enabled': True, 'bands': expected[name]}, self.settings()
        (self.args.evidence / 'live-preset-touch.json').write_text(json.dumps(
            {'target': target, 'before': before, 'after': after, 'settings': self.settings()}, indent=2))
        return self.output(mark, 'live-' + name.lower())


def assert_same(actual, expected):
    assert all(abs(actual[f] - expected[f]) < 1.5 for f in expected), (actual, expected)


def boundaries(run):
    flat = json.loads((run.args.evidence / 'flat.json').read_text())['segments'][0]['bands_dbfs']
    # audio_trace CLI uses decimal keys; Python native analysis uses integer keys.
    flat = {str(int(float(f))): value for f, value in flat.items()}
    run.open_sheet()
    run.touch('^Reset flat$')
    run.slider(910, -12)
    custom = run.capture('custom-mid-cut')
    assert custom['1000'] - flat['1000'] < -6, (custom, flat)
    assert custom['1000'] - flat['1000'] < custom['100'] - flat['100'] - 4, (custom, flat)
    run.open_sheet()
    run.touch('^Enable equalizer /')
    assert run.settings() == {'enabled': False, 'bands': [0, 0, -12, 0, 0]}, run.settings()
    bypass = run.capture('bypass')
    assert_same(bypass, flat)
    voice = run.live_preset('Voice')
    assert (voice['1000'] - flat['1000']) - (voice['100'] - flat['100']) > 3, (voice, flat)
    background = run.capture('voice-background', background=True)
    assert_same(background, voice)
    run.open_sheet()
    run.touch('^Retry equalizer$')
    assert any('Equalizer active.' in label for label in app_ui.labels())
    original_night = adrv.adb('shell', 'cmd', 'uimode', 'night').strip().rsplit(' ', 1)[-1]
    try:
        for theme, mode in [('light', 'no'), ('dark', 'yes')]:
            adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
            time.sleep(.5)
            adrv.shot(str(run.args.evidence / ('equalizer-' + theme + '.png')))
            assert any('Equalizer preset / Voice' in label for label in app_ui.labels())
        app_ui.back()
        run.sheet = False
        assert not any('Equalizer preset' in label for label in app_ui.labels())
    finally:
        adrv.adb('shell', 'cmd', 'uimode', 'night', original_night)
    run.open_sheet()
    run.touch('^Reset flat$')
    assert run.settings() == {'enabled': False, 'bands': [0] * 5}
    assert_same(run.capture('reset'), flat)
    run.record('Custom band, bypass preserving curve, live preset, Home/background, Retry, themes, Back and Reset')


def panel_and_recovery(run):
    query = ('shell', 'cmd', 'package', 'query-activities', '--brief', '-a',
             'android.media.action.DISPLAY_AUDIO_EFFECT_CONTROL_PANEL')
    package = 'com.android.musicfx'
    original = adrv.adb('shell', 'dumpsys', 'package', package)
    match = re.search(r'User 0:.*?enabled=(\d+)', original)
    assert match, 'Record the exact panel package state before disabling it'
    original_state = int(match.group(1))
    restore_commands = {0: 'default-state', 1: 'enable', 2: 'disable',
                        3: 'disable-user', 4: 'disable-until-used'}
    assert original_state in restore_commands
    (run.args.evidence / 'panel-original-state.txt').write_text(original)
    try:
        adrv.adb('shell', 'pm', 'disable-user', '--user', '0', package)
        resolver = adrv.adb(*query)
        (run.args.evidence / 'panel-unavailable-resolver.txt').write_text(resolver)
        assert 'No activities found' in resolver, resolver
        voice = run.live_preset('Voice')
        assert voice['1000'] - voice['100'] > 3, voice
        run.record('Absent external Android effect panel leaves embedded equalizer and measured output available',
                   resolver=resolver, bands=voice)
    finally:
        adrv.adb('shell', 'pm', restore_commands[original_state], '--user', '0', package)
        restored = adrv.adb('shell', 'dumpsys', 'package', package)
        assert int(re.search(r'User 0:.*?enabled=(\d+)', restored).group(1)) == original_state
        (run.args.evidence / 'panel-restored-resolver.txt').write_text(adrv.adb(*query))
    # The 12-second source is already buffered. Removing only this owned
    # reverse mapping makes its server unavailable to this emulator while
    # exercising the device-local DSP; always restore the shared mapping.
    port = str(run.state['port'])
    try:
        adrv.adb('reverse', '--remove', 'tcp:' + port)
        mappings = adrv.adb('reverse', '--list')
        assert 'tcp:' + port not in mappings, mappings
        offline = run.capture('voice-buffered-offline')
        assert_same(offline, voice)
        run.record('Buffered native audio retains local Voice EQ without server reverse mapping', mappings=mappings)
    finally:
        adrv.adb('reverse', 'tcp:' + port, 'tcp:' + port)
    recovery(run, voice)


def recovery(run, voice=None):
    if voice is None:
        previous = json.loads((run.args.evidence / 'live-voice.json').read_text())
        voice = baseline.main_segment(previous)['bands_dbfs']
    saved = run.settings()
    run.close_sheet()
    adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
    app_ui.launch()
    assert adrv.wait('^Choose a server$', timeout=30)
    run.touch('^' + re.escape(run.state['origin']) + '$')
    assert adrv.wait('^Welcome back$', timeout=20)
    fields = [node for node in adrv.nodes() if node['cls'] == 'EditText']
    assert len(fields) == 2
    adrv.tap_node(fields[0])
    app_ui.clear_field()
    adrv.type_text(run.account['username'])
    adrv.tap_node(fields[1])
    app_ui.clear_field()
    assert app_ui.sign_in(run.account['password'])
    assert adrv.wait('^Library$', timeout=30)
    assert run.settings() == saved
    # A direct file play deliberately keeps the independently saved music
    # queue. Re-open this probe through real Library touch navigation after
    # unlock rather than assuming Resume queue refers to the transient file.
    assert baseline.open_track('Music', 'EQ Probe', 'Three Tones')
    baseline.reset_progress(run.client, run.state['media_ids']['Music/EQ Probe/Three Tones.flac'])
    play = run.visible('^Play Audio$')
    mark = run.audio.mark()
    adrv.tap_node(play)
    run.wait_playing()
    assert baseline.session_state()['title'] == 'Three Tones'
    time.sleep(3)
    run.pause()
    recovered = run.output(mark, 'voice-cold-restart')
    assert_same(recovered, voice)
    run.record('Saved Voice setting reapplies to recreated session after explicit cold unlock and source reopen', settings=saved)
    # Re-open the real player from the retained mini-player if queue Resume
    # stays on its list screen.
    mini = adrv.find('^Open player: Three Tones,')
    if mini:
        adrv.tap_node(mini)
    run.open_sheet()
    run.touch('^Reset flat$')
    original_path = run.args.evidence / 'original-eq.json'
    original_eq = json.loads(original_path.read_text()) if original_path.exists() else None
    original_eq = original_eq or {'enabled': False, 'bands': [0] * 5}
    for frequency, value in zip([60, 230, 910, 3600, 14000], original_eq['bands']):
        if value:
            run.slider(frequency, value)
    if run.settings()['enabled'] != original_eq['enabled']:
        run.touch('^Enable equalizer /')
    run.close_sheet()
    assert run.settings() == original_eq
    if run.args.restore_queue:
        original_queue = json.loads(run.args.restore_queue.read_text())
        if original_queue:
            assert app_ui.go_home()
            loudness.restore_queue(run, original_queue)
    run.record('Original equalizer setting restored through native controls', settings=run.settings())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture', type=Path)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--resume', action='store_true')
    parser.add_argument('--boundaries', action='store_true')
    parser.add_argument('--panel-recovery', action='store_true')
    parser.add_argument('--recovery', action='store_true')
    parser.add_argument('--restore-queue', type=Path)
    args = parser.parse_args()
    run = Run(args)
    (args.evidence / 'versions.json').write_text(json.dumps(baseline.device_versions(
        SimpleNamespace(apk=None, emulator_wav=args.emulator_wav), run.state['origin']), indent=2))
    run.sheet = any('Equalizer preset' in adrv.label(n) for n in adrv.nodes())
    if not any((args.boundaries, args.panel_recovery, args.recovery, args.resume)):
        uid = run.client.request('GET', '/api/v1/config')['user_id']
        original_queue = args.evidence / 'original-queue.json'
        if not original_queue.exists():
            original_queue.write_text(json.dumps(run.preference(
                f'flutter.player.queue.{run.state["origin"]}.{uid}')))
        if args.restore_queue is None:
            args.restore_queue = original_queue
        original_eq = args.evidence / 'original-eq.json'
        if not original_eq.exists():
            original_eq.write_text(json.dumps(run.settings()))
    if args.panel_recovery:
        panel_and_recovery(run)
        return
    if args.recovery:
        recovery(run)
        return
    if args.boundaries:
        boundaries(run)
        return
    if not args.resume:
        assert baseline.open_track('Music', 'EQ Probe', 'Three Tones')
        baseline.reset_progress(run.client, run.state['media_ids']['Music/EQ Probe/Three Tones.flac'])
        assert adrv.tap('^Play Audio$')
        run.wait_playing()
        run.pause()
        mini = adrv.find('^Open player: Three Tones,')
        if mini:
            adrv.tap_node(mini)
    run.open_sheet()
    run.touch('^Reset flat$')
    flat = run.capture('flat')
    assert max(flat.values()) - min(flat.values()) < 1.5, flat
    run.open_sheet()
    run.preset('Bass boost')
    print('STATUS', app_ui.labels(), flush=True)
    bass = run.capture('bass')
    run.open_sheet()
    run.preset('Voice')
    voice = run.capture('voice')
    assert (bass['100'] - flat['100']) - (bass['1000'] - flat['1000']) > 3, (bass, flat)
    assert (voice['1000'] - flat['1000']) - (voice['100'] - flat['100']) > 3, (voice, flat)
    print('CAPTURES', {'flat': flat, 'bass': bass, 'voice': voice}, flush=True)
    boundaries(run)
    panel_and_recovery(run)


if __name__ == '__main__':
    main()
