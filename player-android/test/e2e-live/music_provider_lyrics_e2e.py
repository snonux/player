#!/usr/bin/env python3
"""A03c provider lookup and cold offline rich lyrics on one leased emulator."""

import argparse
import json
from pathlib import Path
import re
import time
import traceback

import adrv
import app_ui
import music_albums_e2e as albums
import music_e2e as baseline
import music_offline_browse_e2e as browse
from fixture_server import Client


def labels():
    return app_ui.labels()


def touch(pattern):
    node = baseline.find_visible(pattern)
    assert node, (pattern, labels())
    adrv.tap_node(node)


class Run(baseline.Run):
    def __init__(self, args):
        super().__init__(args, evidence_dir=str(Path(args.evidence_dir) / ('native-' + str(time.time_ns()))))
        self.failures = 0
        self.viewer = Client(self.state['origin']).login(**self.auth['user'])
        self.media = json.loads((self.fixture_dir / 'provider-lyrics.json').read_text())

    def record(self, scenario, ok=True, detail='', data=None, artifacts=()):
        self.evidence.record('A03c', scenario, 'pass' if ok else 'fail', detail, data, artifacts)
        print(('PASS ' if ok else 'FAIL ') + scenario + (f' [{detail}]' if detail else ''), flush=True)
        if not ok:
            self.failures += 1


def journey(run, mode_file, resume_device=False):
    mode_file.write_text('ready')
    if not resume_device:
        assert app_ui.connect_and_login(run.base, run.auth['user']['username'],
                                        run.auth['user']['password'],
                                        lambda *_: None, run.shot), labels()
    run.viewer.request('DELETE', f'/api/v1/media/{run.media["id"]}/lyrics')
    run.record('signed in to disposable provider fixture')
    # Single-file folders are presented as inline songs in the Music set.
    assert baseline.open_track('Music', None, 'Provider Sky'), labels()
    baseline.reset_progress(run.viewer, run.media['id'])
    adrv.tap('^Play Audio$')
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        state = baseline.session_state()
        if state['title'] == 'Provider Sky' and state['state'] in ('PLAYING', 'PAUSED'):
            break
        time.sleep(.2)
    else:
        raise AssertionError(('wrong native source', state))
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
    assert baseline.session_state()['title'] == 'Provider Sky', baseline.session_state()
    touch('^Lyrics$')
    app_ui.swipe(up=True)
    assert baseline.find_visible('^Find lyrics online'), labels()
    assert not adrv.find('Provider sky'), labels()
    touch('^Find lyrics online')
    assert baseline.find_visible('Provider sky'), labels()
    assert adrv.find('^Remove provider lyrics$'), labels()
    run.record('touch lookup shows provider words and removal control',
               artifacts=[run.shot('provider-found')])
    touch('^Language: Original')
    assert adrv.wait('^Language: fr$', timeout=10), labels()
    touch('^Language: fr$')
    assert adrv.wait('Ciel du fournisseur', timeout=10), labels()
    run.record('native translated lyric is selectable', artifacts=[run.shot('provider-french')])
    mode_file.write_text('error')
    touch('^Find lyrics online')
    assert adrv.wait('Lyric lookup failed', timeout=20), labels()
    assert adrv.find('Provider sky'), labels()
    run.record('provider failure keeps cached lyric and audio available')
    mode_file.write_text('ready')
    touch('^Remove provider lyrics$')
    assert adrv.wait('^Find lyrics online', timeout=20), labels()
    assert not adrv.find('Ciel du fournisseur'), labels()
    touch('^Find lyrics online')
    assert adrv.wait('Provider sky', timeout=20), labels()
    run.record('removal and explicit retry restore provider lyric')

    albums.album('A03c Provider')
    playlists.tap_visible('^More options$')
    touch('^Pin offline…$')
    assert adrv.wait('^A03c Provider$', timeout=15), labels()
    touch('^Pin offline$')
    assert adrv.wait(r'^Pinned · 1 of 1 songs downloaded$', timeout=100), labels()
    run.record('album pin saves complete original audio and lyric snapshot',
               artifacts=[run.shot('provider-pinned')])
    touch('^Close$')
    offline_check(run)


def offline_check(run):
    port = run.state['port']
    browse.set_reverse(port, None)
    adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
    app_ui.launch()
    landing = adrv.wait(r'^Choose a server$|^Welcome back$|^On this device$', timeout=45)
    assert landing, labels()
    if adrv.label(landing) != 'On this device':
        touch('^Use files on this device$')
    assert adrv.wait('^On this device$', timeout=30), labels()
    touch('^Downloads$')
    touch('^Browse offline music$')
    assert adrv.wait('^Offline music$', timeout=20), labels()
    node = app_ui.find_scrolling(r'^Provider Sky(?: /|$)')
    assert node, labels()
    adrv.tap_node(node)
    assert adrv.wait('^Play now$', timeout=20), labels()
    mark = run.audio.mark()
    touch('^Play now$')
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline and baseline.session_state()['title'] != 'Provider Sky':
        time.sleep(.3)
    assert baseline.session_state()['title'] == 'Provider Sky', baseline.session_state()
    time.sleep(1.5)
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
    wav = run.audio.save(mark, run.evidence.path('provider-offline-audio.wav'))
    analysis = baseline.audio_trace.analyze(wav, [440, 523.25])
    main = baseline.main_segment(analysis)
    assert main and main['bands_dbfs']['440'] > main['bands_dbfs']['523.25'] + 15, analysis
    touch('^Open player: Provider Sky')
    touch('^Lyrics$')
    app_ui.swipe(up=True)
    assert adrv.wait('^Saved lyrics$', timeout=20), labels()
    assert adrv.find('Provider sky'), labels()
    touch('^Language: Original')
    touch('^Language: fr$')
    assert adrv.find('Ciel du fournisseur'), labels()
    run.record('cold offline native playback keeps rich lyric, translation and real 440 Hz audio',
               data={'media_id': run.media['id'], 'bands_dbfs': main['bands_dbfs']},
               artifacts=[wav, run.shot('provider-offline-french')])
    app_ui.back()
    run.record('Back leaves offline player through native navigation')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--evidence-dir', required=True)
    parser.add_argument('--apk', required=True)
    parser.add_argument('--mode-file', type=Path, required=True)
    parser.add_argument('--resume-device', action='store_true',
                        help='reuse an already signed-in disposable app after a driver retry')
    parser.add_argument('--offline-only', action='store_true',
                        help='finish offline proof with an existing saved copy on this emulator')
    args = parser.parse_args()
    args.host_alias = False
    fixture = Path(args.fixture_dir)
    state = json.loads((fixture / 'state.json').read_text())
    assert (fixture / '.player-music-fixture').is_file()
    assert state['origin'].startswith('http://127.0.0.1:')
    previous = browse.reverse_target(state['port'])
    if not args.offline_only:
        baseline.prepare_device(args, state['port'])
    run = Run(args)
    try:
        if args.offline_only:
            offline_check(run)
        else:
            journey(run, args.mode_file, args.resume_device)
    except Exception:
        run.record('native provider and offline lyric journey', False, traceback.format_exc(),
                   artifacts=[run.shot('failure')])
    finally:
        browse.set_reverse(state['port'], previous)
        args.mode_file.write_text('ready')
    print(f'A03c native failures: {run.failures}', flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    raise SystemExit(main())
