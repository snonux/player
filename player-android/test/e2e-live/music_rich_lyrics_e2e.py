#!/usr/bin/env python3
"""A03b rich lyric touch acceptance on one owned emulator and local fixture."""

import argparse
import json
from pathlib import Path
import time
import traceback

import adrv
import app_ui
import music_e2e as baseline
import music_offline_browse_e2e as browse


ALBUM = 'Aurora Lane - Night Transit (2021)'


def labels():
    return app_ui.labels()


def selected_rows():
    return [adrv.label(node) for node in adrv.nodes()
            if node['sel'] == 'true' and 'Tap to seek' in adrv.label(node)]


def tap_line(label):
    """Scroll the lyric list itself; its rows are built lazily by Flutter."""
    for _ in range(6):
        language = adrv.find('^Language: ')
        if language and language['b'][1] <= 1400:
            break
        app_ui.swipe(up=True)
    else:
        raise AssertionError(('lyric panel is below the player viewport', labels()))
    for direction in (None, 'up', 'up', 'down', 'down', 'down'):
        if direction:
            start, end = (1800, 1600) if direction == 'up' else (1650, 1850)
            adrv.adb('shell', 'input', 'swipe', '500', str(start), '500', str(end), '350')
        node = adrv.find(label)
        if node and 1400 <= node['b'][1] < node['b'][3] <= 2000:
            adrv.tap_node(node)
            return
    raise AssertionError(('lyric line is not touchable', label, labels()))


def position(expected):
    deadline = time.monotonic() + 15
    while True:
        state = baseline.session_state()
        if state['state'] == 'PAUSED' or time.monotonic() >= deadline:
            break
        time.sleep(.2)
    assert state['title'] == 'First Light' and state['state'] == 'PAUSED', state
    assert abs(state['position_ms'] - expected) < 650, state
    return state


class Run(baseline.Run):
    def __init__(self, args):
        super().__init__(args, evidence_dir=str(Path(args.evidence_dir) / ('native-' + str(time.time_ns()))))
        self.failures = 0

    def record(self, scenario, ok=True, detail='', data=None, artifacts=()):
        self.evidence.record('A03b', scenario, 'pass' if ok else 'fail', detail, data, artifacts)
        print(('PASS ' if ok else 'FAIL ') + scenario + (f' [{detail}]' if detail else ''), flush=True)
        if not ok:
            self.failures += 1


def open_song(run, title, media_id):
    assert baseline.open_track('Music', ALBUM, title), labels()
    baseline.reset_progress(run.viewer, media_id)
    adrv.tap('^Play Audio$')
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        state = baseline.session_state()
        if state['title'] == title and state['state'] in ('PLAYING', 'PAUSED'):
            break
        time.sleep(.2)
    else:
        raise AssertionError(('native source did not start', state))
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
    deadline = time.monotonic() + 12
    while time.monotonic() < deadline and baseline.session_state()['state'] != 'PAUSED':
        time.sleep(.2)
    assert baseline.session_state()['state'] == 'PAUSED', baseline.session_state()
    time.sleep(.6)
    for _ in range(5):
        try:
            if adrv.find('^' + title + '$'):
                break
        except RuntimeError:  # Animated player can briefly deny idle UI dumps.
            pass
        time.sleep(1)
    else:
        raise AssertionError(('native player title did not settle', labels()))
    button = baseline.find_visible('^Lyrics$')
    assert button, labels()
    adrv.tap_node(button)
    assert adrv.wait('^Hide lyrics$', timeout=20), labels()
    app_ui.swipe(up=True)


def journey(run):
    fixture = json.loads((run.fixture_dir / 'rich-lyrics.json').read_text())
    assert fixture['native_id'] == run.state['media_ids'][fixture['native_path']]
    run.viewer = browse.Client(run.state['origin']).login(**run.auth['user'])
    assert app_ui.connect_and_login(run.base, run.auth['user']['username'],
                                    run.auth['user']['password'],
                                    lambda *_: None, run.shot), labels()
    run.record('fresh native app signs in to disposable lyric fixture')
    open_song(run, 'First Light', fixture['native_id'])
    assert adrv.wait(r'^Language: Original \(en\)$', timeout=20), labels()
    assert 'Voice: Lead / Hello world / Tap to seek' in labels(), labels()
    assert 'Voice: Harmony / Harmony sings / Tap to seek' in labels(), labels()
    tap_line('Voice: Lead / Hello world / Tap to seek')
    position(1000)
    active = selected_rows()
    assert len(active) == 2 and any('Lead' in row for row in active)
    assert any('Harmony' in row for row in active), active
    run.record('native touch seeks to simultaneous Lead and Harmony cues',
               data={'position_ms': 1000, 'active': active},
               artifacts=[run.shot('rich-overlapping-voices')])

    mark = run.audio.mark()
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PLAY')
    time.sleep(1.8)
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
    state = baseline.session_state()
    assert state['title'] == 'First Light' and 1900 <= state['position_ms'] < 4500, state
    # The native word span's underline is visual, so retain the real screen
    # at an authoritative 2–4 second position for direct visual review.
    adrv.adb('shell', 'input', 'swipe', '500', '1700', '500', '1850', '350')
    wav = run.audio.save(mark, run.evidence.path('rich-word-audio.wav'))
    analysis = baseline.audio_trace.analyze(wav, [440, 523.25, 659.25])
    main = baseline.main_segment(analysis)
    assert main and analysis['audible_s'] >= 1.4, analysis
    bands = main['bands_dbfs']
    assert bands['440'] > -55 and bands['440'] > max(bands['523.25'], bands['659.25']) + 15, analysis
    run.record('word timing follows audible 440 Hz playback', data={'position_ms': state['position_ms'],
               'bands_dbfs': bands, 'audible_s': analysis['audible_s']},
               artifacts=[wav, run.shot('rich-world-word')])

    adrv.tap('^Language: Original')
    assert adrv.wait('^Language: fr$', timeout=10), labels()
    adrv.tap('^Language: fr$')
    assert 'Voice: Lead / Bonjour monde / Tap to seek' in labels(), labels()
    assert 'Voice: Harmony / Harmony sings / Original: en / Tap to seek' in labels(), labels()
    assert baseline.session_state()['position_ms'] == state['position_ms']
    tap_line('Voice: Lead / Chemin maison / Tap to seek')
    position(6000)
    assert 'Current: Voice: Lead · Chemin maison' in labels(), labels()
    run.record('native language choice keeps position, partial Original fallback and translated seek',
               artifacts=[run.shot('rich-french')])

    previous = browse.reverse_target(run.state['port'])
    assert previous == f'tcp:{run.state["port"]}', previous
    browse.set_reverse(run.state['port'], None)
    try:
        app_ui.swipe(up=True)
        assert adrv.wait('^Language: fr$', timeout=10), labels()
        assert 'Current: Voice: Lead · Chemin maison' in labels(), labels()
        adrv.tap('^Language: fr$')
        adrv.tap('^Language: Original')
        assert 'Current: Voice: Lead · Road home' in labels(), labels()
        adrv.tap('^Language: Original')
        adrv.tap('^Language: fr$')
        assert 'Current: Voice: Lead · Chemin maison' in labels(), labels()
        assert baseline.session_state()['title'] == 'First Light'
        run.record('loaded rich words and language choice remain visible during a real server disconnection',
                   artifacts=[run.shot('rich-offline-loaded')])
    finally:
        browse.set_reverse(run.state['port'], previous)

    for name, mode in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
        time.sleep(1)
        assert 'Language: fr' in labels() and 'Current: Voice: Lead · Chemin maison' in labels(), labels()
        run.record('native ' + name + ' rich lyric controls remain readable',
                   artifacts=[run.shot('rich-' + name)])

    adrv.tap('^Language: fr$')
    adrv.tap('^Language: Original')
    tap_line('Voice: Lead / Hello world / Tap to seek')
    position(1000)
    run.record('backward lyric seek restores original-language active cues')
    app_ui.back()
    open_song(run, 'Long Road Home', run.state['media_ids'][
        'Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3'])
    assert adrv.wait('Every mile a quiet song', timeout=20), labels()
    assert not any(label.startswith('Language:') for label in labels()), labels()
    run.record('line-only lyrics remain readable without a language control',
               artifacts=[run.shot('rich-line-only-fallback')])
    app_ui.back()
    run.record('Back leaves the lyric player through native navigation')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--evidence-dir', required=True)
    parser.add_argument('--apk', required=True)
    args = parser.parse_args()
    args.host_alias = False
    state = json.loads((Path(args.fixture_dir) / 'state.json').read_text())
    assert (Path(args.fixture_dir) / '.player-music-fixture').is_file()
    assert state['origin'].startswith('http://127.0.0.1:')
    original = browse.reverse_target(state['port'])
    theme = adrv.adb('shell', 'cmd', 'uimode', 'night').rsplit(':', 1)[-1].strip()
    baseline.prepare_device(args, state['port'])
    run = Run(args)
    try:
        journey(run)
    except Exception as error:
        run.record('native rich lyrics journey', False, str(error), artifacts=[run.shot('failure')])
        traceback.print_exc()
    finally:
        browse.set_reverse(state['port'], original)
        if theme in ('yes', 'no', 'auto'):
            adrv.adb('shell', 'cmd', 'uimode', 'night', theme)
    print(f'A03b native failures: {run.failures}', flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    raise SystemExit(main())
