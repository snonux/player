#!/usr/bin/env python3
"""Measure M24 through the installed native app and owned QEMU WAV output.

Requires an already signed-in disposable fixture and rooted API34 emulator.
Never clears app data, installs an APK, restarts a runtime or writes preferences.
Native touch controls mutate loudness; scoped preferences are a saved-state
oracle. Pause dispatch only stabilizes actual PLAYING before fresh UI reads.
"""
import argparse
import json
import math
from pathlib import Path
import re
import time
from types import SimpleNamespace
import urllib.request
import uuid
import xml.etree.ElementTree as ET

import adrv
import app_ui
import music_e2e as baseline
from fixture_server import Client


def clipping_waveform(path, segment):
    """Reject flat-topped overdrive as well as a misleading level-only pass."""
    rate, samples = baseline.audio_trace.read_wav(path)
    middle = round((segment['start_s'] + segment['end_s']) * rate / 2)
    body = samples[max(0, middle - rate):middle + rate]
    assert body
    rms = math.sqrt(sum(value * value for value in body) / len(body))
    peak = max(abs(value) for value in body)
    crest = 20 * math.log10(peak / rms)
    flat_fraction = sum(abs(value) >= .999 for value in body) / len(body)
    # A full-scale sine has ~3 dB crest and <3% samples near full scale.
    # Native resampler spikes may increase crest, so do not cap its upper end.
    assert crest >= 2.5, {'crest_db': crest}
    assert peak >= 10 ** (-1.5 / 20), {'peak': peak}
    assert flat_fraction < .05, {'near_full_scale_fraction': flat_fraction}
    return {'crest_db': crest, 'near_full_scale_fraction': flat_fraction}


class Run:
    def __init__(self, args):
        self.args = args
        self.state = json.loads((args.fixture / 'state.json').read_text())
        auth = json.loads((args.fixture / 'auth.json').read_text())
        self.account = auth['admin']
        self.client = Client(self.state['origin']).login(**self.account)
        self.audio = baseline.EmulatorAudio(args.emulator_wav)
        assert self.audio.available(), 'Owned QEMU WAV is required'
        args.evidence.mkdir(parents=True, exist_ok=True)
        self.results = []
        self.sequence = 0
        self.sheet = any('Loudness mode' in adrv.label(n) for n in adrv.nodes())
        self.dimensions = [int(n) for n in re.search(r'(\d+)x(\d+)',
            adrv.adb('shell', 'wm', 'size')).groups()]

    def settings(self):
        return self.preference('flutter.player-loudness-v1') or {'mode': 'off', 'preamp': 0, 'untagged': 0}

    def preference(self, key):
        root = ET.fromstring(adrv.adb('exec-out', 'cat',
            f'/data/user/0/{app_ui.PKG}/shared_prefs/FlutterSharedPreferences.xml'))
        for item in root:
            if item.attrib.get('name') == key:
                return json.loads(item.text) if (key.endswith('player-loudness-v1') or
                    key.startswith('flutter.player.queue.')) else item.text
        return None

    def record(self, name, **data):
        self.results.append({'scenario': name, 'result': 'pass', **data})
        (self.args.evidence / 'results.json').write_text(json.dumps(self.results, indent=2))
        print('PASS', name, flush=True)

    def pause(self):
        if baseline.session_state()['state'] == 'PLAYING':
            adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
        end = time.monotonic() + 10
        while baseline.session_state()['state'] == 'PLAYING' and time.monotonic() < end:
            time.sleep(.2)
        assert baseline.session_state()['state'] != 'PLAYING'

    def visible(self, pattern, cls=None):
        assert baseline.session_state()['state'] != 'PLAYING', 'Pause before idle-dependent UI observation'
        width, height = self.dimensions
        for direction in (None, 'up', 'up', 'down', 'down', 'down'):
            if direction:
                start, stop = (.8, .35) if direction == 'up' else (.35, .8)
                adrv.adb('shell', 'input', 'swipe', str(width // 2), str(int(height * start)),
                         str(width // 2), str(int(height * stop)), '350')
                time.sleep(.4)
            nodes = adrv.nodes()
            found = [n for n in nodes if re.search(pattern, adrv.label(n)) and
                     (cls is None or n['cls'] == cls) and
                     0 <= n['b'][1] < n['b'][3] <= height and n['b'][2] > n['b'][0]]
            if found:
                return found[0]
        node = adrv.reveal(pattern, cls)
        if node:
            return node
        raise AssertionError((pattern, [adrv.label(n) for n in nodes if adrv.label(n)]))

    def touch(self, pattern, cls=None):
        node = self.visible(pattern, cls)
        self.sequence += 1
        stem = f'touch-{self.sequence:03}'
        adrv.shot(str(self.args.evidence / (stem + '.png')))
        (self.args.evidence / (stem + '.json')).write_text(json.dumps(
            {'pattern': pattern, 'node': node, 'dimensions': self.dimensions}, indent=2))
        adrv.tap_node(node)
        time.sleep(.3)

    def open_sheet(self):
        if not self.sheet:
            self.touch('^Loudness$')
            self.sheet = True

    def close_sheet(self):
        if self.sheet:
            self.touch('^Done$')
            self.sheet = False

    def mode(self, value):
        self.open_sheet()
        self.touch('Loudness mode')
        self.touch('^' + value.capitalize() + '$')
        end = time.monotonic() + 5
        while self.settings()['mode'] != value and time.monotonic() < end:
            time.sleep(.2)
        assert self.settings()['mode'] == value, self.settings()

    def slider(self, key, value):
        self.open_sheet()
        nodes = adrv.nodes()
        collapsed = next((n for n in nodes if adrv.label(n) == 'Advanced loudness, Collapsed'), None)
        if collapsed:
            adrv.tap_node(collapsed)
            time.sleep(.3)
        label = 'Preamp' if key == 'preamp' else 'Untagged gain'
        thumb = self.visible(r', ' + label + r' [-\d]+ decibels$', 'SeekBar')
        field = self.visible('Loudness mode')
        # Current Flutter semantics give only the thumb touch bounds. The rail
        # spans the full content width inset by half that touch target on each
        # side; record the original before touching the observed rail.
        inset = (thumb['b'][2] - thumb['b'][0]) / 2
        left, right = field['b'][0] + inset, field['b'][2] - inset
        x, y = round(left + (right - left) * (value + 12) / 24), (thumb['b'][1] + thumb['b'][3]) // 2
        self.sequence += 1
        stem = f'slider-{self.sequence:03}'
        adrv.shot(str(self.args.evidence / (stem + '.png')))
        (self.args.evidence / (stem + '.json')).write_text(json.dumps(
            {'key': key, 'requested': value, 'thumb': thumb, 'field': field, 'x': x, 'y': y}, indent=2))
        adrv.adb('shell', 'input', 'tap', str(x), str(y))
        end = time.monotonic() + 5
        while self.settings()[key] != value and time.monotonic() < end:
            time.sleep(.2)
        assert self.settings()[key] == value, self.settings()

    def open_track(self, name):
        self.close_sheet()
        self.pause()
        assert baseline.open_track('Music', 'Gain Check', name)
        media_id = self.state['media_ids'][f'Music/Gain Check/{name}.flac' if name != 'No Gain Tags'
            else 'Music/Gain Check/No Gain Tags.mp3']
        baseline.reset_progress(self.client, media_id)
        assert adrv.tap('^Play Audio$')
        self.wait_playing()
        self.pause()

    def wait_playing(self):
        end = time.monotonic() + 20
        while baseline.session_state()['state'] != 'PLAYING' and time.monotonic() < end:
            time.sleep(.2)
        assert baseline.session_state()['state'] == 'PLAYING'

    def capture(self, name, background=False):
        self.close_sheet()
        # Flutter exposes only the seek thumb's bounds. The labeled native
        # skip control reliably resets these twelve-second fixtures to zero.
        self.touch('^Skip back 15 seconds$')
        play = self.visible('^Play$')
        mark = self.audio.mark()
        adrv.tap_node(play)
        self.wait_playing()
        if background:
            adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_HOME')
            assert not app_ui.foreground()
            adrv.shot(str(self.args.evidence / (name + '-background.png')))
        time.sleep(3)
        self.pause()
        if background:
            app_ui.launch()
            time.sleep(.5)
        return self.output(mark, name)

    def output(self, mark, name):
        wav = self.audio.save(mark, self.args.evidence / (name + '.wav'))
        analysis = baseline.audio_trace.analyze(wav)
        (self.args.evidence / (name + '.json')).write_text(json.dumps(analysis, indent=2))
        segment = baseline.main_segment(analysis)
        assert segment and segment['duration_s'] >= 2, analysis
        assert abs(segment['tone_hz'] - 1000) < 20, segment
        print(name, segment, flush=True)
        self.last_segment = segment
        self.last_wav = wav
        return segment['rms_dbfs']


def boundaries(run):
    import music_search_e2e as search
    if not run.args.resume_album:
        run.open_track('Loud')
        run.slider('preamp', 0)
        run.slider('untagged', 0)
        run.close_sheet()
        # Filter the real catalog rather than relying on a remembered scroll cap.
        search.open_search()
        search.search('Gain Check')
        search.tap('^Albums$')
        search.tap_result('Gain Check')
        assert adrv.wait('^Play album$', timeout=20)
        assert adrv.tap('^Play album$')
        run.wait_playing()
        run.pause()
    else:
        import music_saved_session_e2e as portable
        uid = run.client.request('GET', '/api/v1/config')['user_id']
        queue = portable.snapshot(run.state['origin'], uid)
        expected = [run.state['media_ids']['Music/Gain Check/' + name] for name in
                    ('Loud.flac', 'Quiet.flac', 'No Gain Tags.mp3')]
        assert [entry['mediaId'] for entry in queue['entries']] == expected, queue
        assert queue['index'] == 0 and baseline.session_state()['state'] != 'PLAYING', queue
        assert run.settings()['preamp'] == run.settings()['untagged'] == 0, run.settings()
    assert baseline.session_state()['title'] == 'Loud'
    mini = adrv.find('^Open player: Loud,')
    if mini:
        adrv.tap_node(mini)
    run.mode('automatic')
    assert any('Album tags' in label for label in app_ui.labels()), app_ui.labels()
    loud = run.capture('ordered-album-automatic-loud')
    run.touch('^Next track$')
    run.wait_playing()
    run.pause()
    assert baseline.session_state()['title'] == 'Quiet'
    quiet = run.capture('ordered-album-automatic-quiet')
    assert abs(loud - quiet - 18) < 1.5, (loud, quiet)
    run.record('Native Automatic uses album tags across ordered album source transitions',
               loud_dbfs=loud, quiet_dbfs=quiet, difference_db=loud-quiet)
    run.mode('track')
    foreground = run.capture('quiet-track-foreground')
    background = run.capture('quiet-track-background', background=True)
    assert abs(background - foreground) < 1.5, (foreground, background)
    run.record('Native positive gain survives Home and background playback',
               foreground_dbfs=foreground, background_dbfs=background)
    assert run.preference('flutter.theme_mode') in (None, 'system'), 'Theme must follow system for this scenario'
    original_night = adrv.adb('shell', 'cmd', 'uimode', 'night').strip().rsplit(' ', 1)[-1]
    try:
        for theme, mode in [('light', 'no'), ('dark', 'yes')]:
            adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
            time.sleep(.5)
            run.open_sheet()
            collapsed = adrv.find('^Advanced loudness, Collapsed$')
            if collapsed:
                adrv.tap_node(collapsed)
                time.sleep(.3)
            adrv.shot(str(run.args.evidence / ('loudness-advanced-' + theme + '.png')))
            assert any('Preamp: 0 dB' in label for label in app_ui.labels())
            app_ui.back()
            run.sheet = False
            assert not any('Loudness mode' in label for label in app_ui.labels())
        run.record('Native light and dark advanced sheet labels and predictable Back')
    finally:
        adrv.adb('shell', 'cmd', 'uimode', 'night', original_night)
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
    assert baseline.session_state()['state'] != 'PLAYING'
    run.touch('^Now Playing queue$')
    node = run.visible('^Resume queue$')
    mark = run.audio.mark()
    adrv.tap_node(node)
    run.wait_playing()
    time.sleep(3)
    run.pause()
    recovered = run.output(mark, 'quiet-track-after-cold-restart')
    assert abs(recovered - foreground) < 1.5, (recovered, foreground)
    run.record('Native saved gain reapplies to recreated session after explicit cold unlock and Resume',
               before=saved, after=run.settings(), rms_dbfs=recovered)
    app_ui.back()
    run.open_track('Quiet')
    original = json.loads(run.args.restore_settings.read_text())
    run.slider('preamp', original['preamp'])
    run.slider('untagged', original['untagged'])
    run.mode(original['mode'])
    run.close_sheet()
    assert run.settings() == original
    assert app_ui.go_home()
    assert baseline.session_state()['state'] != 'PLAYING'
    run.record('Original loudness settings restored through native controls; admin Library paused', settings=original)
    if run.args.restore_queue:
        restore_queue(run, json.loads(run.args.restore_queue.read_text()))


def restore_queue(run, original):
    """Restore the fixture session through its public API and explicit UI."""
    import music_saved_session_e2e as portable
    saved = run.client.request('GET', '/api/v1/queue')
    entries = [{'id': entry['id'], 'media_id': entry['mediaId']} for entry in original['entries']]
    body = {'entries': entries, 'current_id': entries[original['index']]['id'],
            'position_seconds': original['position'], 'label': original['label'],
            'repeat': original['repeat'], 'shuffle': original['shuffle'],
            'canonical': original['canonical'], 'up_next': original['upNext'],
            'mutation_id': str(uuid.uuid4())}
    request = urllib.request.Request(run.state['origin'] + '/api/v1/queue', method='PUT',
        data=json.dumps(body).encode(), headers={'Content-Type': 'application/json',
                                               'If-Match': f'"{saved["revision"]}"'})
    with run.client.opener.open(request) as response:
        accepted = json.load(response)
    run.touch('^Now Playing queue$')
    run.touch('^Continue session$')
    assert adrv.wait('^Replace$', timeout=20)
    run.touch('^Replace$')
    uid = run.client.request('GET', '/api/v1/config')['user_id']
    end = time.monotonic() + 10
    while time.monotonic() < end:
        restored = portable.snapshot(run.state['origin'], uid)
        if restored and restored['entries'] == original['entries']:
            break
        time.sleep(.2)
    assert restored['entries'] == original['entries']
    assert restored['index'] == original['index']
    assert abs(restored['position'] - original['position']) < .15
    assert restored['canonical'] == original['canonical']
    assert restored['repeat'] == original['repeat'] and restored['shuffle'] == original['shuffle']
    assert baseline.session_state()['state'] != 'PLAYING'
    app_ui.back()
    assert app_ui.go_home()
    run.record('Original queue/current/offset restored with CAS and explicit native Continue paused',
               original=original, restored=restored, accepted_revision=accepted['revision'])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture', type=Path)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--apk', type=Path)
    parser.add_argument('--restore-settings', type=Path)
    parser.add_argument('--restore-queue', type=Path)
    parser.add_argument('--resume-album', action='store_true', help='Continue an already verified paused Gain Check album queue')
    parser.add_argument('--scenario', choices=('attenuation', 'modes', 'boundaries'), default='attenuation')
    args = parser.parse_args()
    run = Run(args)
    (args.evidence / 'versions.json').write_text(json.dumps(baseline.device_versions(
        SimpleNamespace(apk=args.apk, emulator_wav=args.emulator_wav), run.state['origin']), indent=2))
    (args.evidence / 'initial-loudness.json').write_text(json.dumps(run.settings(), indent=2))
    if args.scenario == 'boundaries':
        assert args.restore_settings, '--restore-settings requires the ORIGINAL pre-task snapshot'
        boundaries(run)
        return
    if args.scenario == 'attenuation':
        uid = run.client.request('GET', '/api/v1/config')['user_id']
        original = run.preference(f'flutter.player.queue.{run.state["origin"]}.{uid}')
        (args.evidence / 'initial-queue.json').write_text(json.dumps(original, indent=2))
        run.open_track('Loud')
        if run.settings()['preamp'] != 0:
            run.slider('preamp', 0)
        if run.settings()['untagged'] != 0:
            run.slider('untagged', 0)
        run.mode('off')
        off = run.capture('loud-off')
        run.mode('track')
        track = run.capture('loud-track')
        assert abs(track - off + 9) < 1.5, {'off': off, 'track': track}
        run.record('Native Loud track attenuation measures -9 dB against Off', off_dbfs=off,
                   track_dbfs=track, delta_db=track-off, settings=run.settings())
    else:
        run.slider('preamp', 0)
        run.slider('untagged', 0)
        levels = {}
        for title in ('Quiet', 'Loud'):
            run.open_track(title)
            levels[title] = {}
            for mode in ('off', 'track', 'album', 'automatic'):
                run.mode(mode)
                levels[title][mode] = run.capture(title.lower() + '-' + mode)
            delta = levels[title]['track'] - levels[title]['off']
            expected = 9 if title == 'Quiet' else -9
            assert abs(delta - expected) < 1.5, levels
            assert abs(levels[title]['album'] - levels[title]['off'] + .5) < 1.5, levels
            assert abs(levels[title]['automatic'] - levels[title]['track']) < 1.5, levels
            run.record(title + ' Off Track Album Automatic measured output', levels_dbfs=levels[title])
        assert abs(levels['Loud']['track'] - levels['Quiet']['track']) < 1.5, levels
        assert abs(levels['Loud']['album'] - levels['Quiet']['album'] - 18) < 1.5, levels
        run.mode('track')
        for preamp in (6, -6):
            run.slider('preamp', preamp)
            measured = run.capture('loud-preamp-' + str(preamp))
            assert abs(measured - levels['Loud']['track'] - preamp) < 1.5, (measured, levels)
            run.record('Native preamp ' + str(preamp) + ' dB measured output', rms_dbfs=measured)
        run.slider('preamp', 12)
        run.mode('album')
        status = [adrv.label(n) for n in adrv.nodes() if 'Gain limited' in adrv.label(n)]
        assert status, app_ui.labels()
        clipped = run.capture('loud-album-preamp12-clipping-limited')
        assert abs(clipped - levels['Loud']['album'] - 3.5) < 1.5, (clipped, levels)
        waveform = clipping_waveform(run.last_wav, run.last_segment)
        run.record('Native Album preamp12 caps requested gain at known peak', rms_dbfs=clipped,
                   segment=run.last_segment, waveform=waveform, status=status)
        run.open_track('No Gain Tags')
        run.mode('off')
        off = run.capture('untagged-off')
        run.mode('track')
        run.slider('untagged', 12)
        status = [adrv.label(n) for n in adrv.nodes() if 'positive boost disabled' in adrv.label(n)]
        assert status, app_ui.labels()
        safe = run.capture('untagged-boost12-safe')
        assert abs(safe - off) < 1.5, (safe, off)
        run.slider('untagged', -12)
        attenuated = run.capture('untagged-minus12')
        assert abs(attenuated - off + 12) < 1.5, (attenuated, off)
        run.record('Native missing tags prevent positive boost and permit attenuation', off_dbfs=off,
                   positive12_dbfs=safe, negative12_dbfs=attenuated, status=status)
        run.slider('preamp', 0)
        run.slider('untagged', 0)
        run.mode('off')
        run.close_sheet()


if __name__ == '__main__':
    main()
