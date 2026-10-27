#!/usr/bin/env python3
"""M10 real Android enrichment against the disposable Player and JSON agent.

The caller owns the running emulator, installed APK, fixture and provider. This
runner changes only the provider's disposable mode file and Music-set grant;
both are restored even if a check fails. It never starts another emulator.
"""
import argparse
import json
from pathlib import Path
import re
import sys
import time
import traceback
from urllib.parse import quote

import adrv
import app_ui
import music_e2e as baseline
import music_albums_e2e as albums
import music_artists_e2e as artists
from fixture_server import Client


class Run(baseline.Run):
    def __init__(self, args):
        super().__init__(args, evidence_dir=args.evidence)
        self.viewer = Client(self.state['origin']).login(**self.auth['user'])
        self.failures = 0

    def record(self, scenario, ok, detail='', data=None, artifacts=()):
        self.evidence.record('M10', scenario, 'pass' if ok else 'fail',
                             detail, data, artifacts)
        print(f"{'PASS' if ok else 'FAIL'} M10 {scenario} {detail}", flush=True)
        if not ok:
            self.failures += 1


def labels():
    return ' | '.join(app_ui.labels())


def visible(pattern, swipes=10):
    """Find a touch target above the persistent player, scrolling both ways."""
    for move in (None, *([True] * swipes), *([False] * (2 * swipes))):
        if move is not None:
            start, end = ('1350', '500') if move else ('500', '1350')
            adrv.adb('shell', 'input', 'swipe', '540', start, '540', end, '350')
            time.sleep(.5)
        node = adrv.find(pattern)
        if node and node['b'][3] <= 1550:
            return node
    raise AssertionError((pattern, labels()))


def top_visible(pattern):
    """Find a detail-header control without traversing the related list."""
    for move in range(9):
        node = adrv.find(pattern)
        if node and 0 < node['b'][3] <= 1550:
            return node
        if move < 8:
            if node and node['b'][3] > 1550:
                adrv.adb('shell', 'input', 'swipe', '540', '1350', '540', '750', '220')
            else:
                adrv.adb('shell', 'input', 'swipe', '540', '500', '540', '1450', '220')
    raise AssertionError((pattern, labels()))


def tap(pattern):
    adrv.tap_node(visible(pattern))
    time.sleep(.4)


def open_about():
    about = top_visible(r'^About(?:, (?:Expanded|Collapsed))?$')
    if 'Expanded' not in adrv.label(about):
        adrv.tap_node(about)
    visible(r'^About, Expanded$')


def state(client, kind, key):
    return client.request('GET', f'/api/v1/music/enrichment/{kind}/{quote(key)}')


def wait_state(client, kind, key, wanted, previous=None):
    deadline = time.monotonic() + 15
    last = None
    while time.monotonic() < deadline:
        last = state(client, kind, key)
        if last['state'] == wanted and (previous is None or last.get('fetched_at') != previous):
            return last
        time.sleep(.3)
    raise AssertionError((kind, key, wanted, previous, last))


def sign_in(run, account):
    credentials = run.auth[account]
    assert app_ui.connect_and_login(
        run.base, credentials['username'], credentials['password'],
        lambda name, ok, detail='': run.record(account + ' sign-in: ' + name, ok, detail),
        run.shot), labels()


def album():
    albums.album('Night Transit')
    top_visible(r'^Artwork for Night Transit$')
    top_visible(r'^Play album$')


def artist():
    artists.catalog()
    artists.open_visible('Aurora Lane')
    assert adrv.wait(r'^Aurora Lane$', timeout=20), labels()
    visible(r'^Play artist$')


def source(run):
    track = run.track('Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3')
    mark = run.audio.mark()
    adrv.tap_node(top_visible(r'^Play album$'))
    deadline = time.monotonic() + 20
    current = None
    while time.monotonic() < deadline:
        current = baseline.session_state()
        if current['state'] == 'PLAYING' and current['title'] == track['title']:
            break
        time.sleep(.3)
    assert current and current['state'] == 'PLAYING' and current['title'] == track['title'], current
    time.sleep(4)
    wav = run.audio.save(mark, run.evidence.path('m10-album-source.wav'))
    analysis = baseline.audio_trace.analyze(wav)
    segment = baseline.main_segment(analysis)
    assert segment and segment['duration_s'] >= 2 and baseline.tone_matches(segment, track), analysis
    run.record('About leaves original album audio playable', True,
               data={'session': current, 'audio': analysis}, artifacts=[wav])
    adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
    # A short fixture track can end before capture analysis completes, in
    # which case the player has already returned to the album. Only pop it
    # when its controls are still on screen.
    if adrv.find(r'^Info$'):
        app_ui.back()
    visible(r'Fixture album description for Night Transit')


def admin_journey(run, mode_file, album_key, artist_key, *, skip_artist=False):
    sign_in(run, 'admin')
    if not skip_artist:
        artist()
        open_about()
        before = state(run.admin, 'artist', artist_key).get('fetched_at')
        tap(r'^Refresh metadata$')
        refreshed = wait_state(run.admin, 'artist', artist_key, 'ready', before)
        visible(r'Fixture artist description for Aurora Lane')
        visible(r'^Source: Local deterministic fixture$')
        visible(r'^Provider image from Local deterministic fixture$')
        assert not adrv.find(r'Provider image is unavailable'), labels()
        link = visible(r'^MusicBrainz$')
        run.record('artist touch refresh shows provider biography, image, attribution and link', True,
                   data={'artist_key': artist_key, 'fetched_at': refreshed.get('fetched_at')},
                   artifacts=[run.shot('m10-artist-ready')])
        adrv.tap_node(link)
        time.sleep(1)
        assert not app_ui.foreground(), adrv.adb('shell', 'dumpsys', 'activity', 'activities')[-600:]
        app_ui.back()
        assert app_ui.foreground() and adrv.wait(r'^Aurora Lane$', timeout=20), labels()
        run.record('MusicBrainz touch opens Android browser and Back returns to artist', True)

    album()
    open_about()
    before = state(run.admin, 'album', album_key).get('fetched_at')
    tap(r'^Refresh metadata$')
    ready = wait_state(run.admin, 'album', album_key, 'ready', before)
    visible(r'Fixture album description for Night Transit')
    top_visible(r'^Artwork for Night Transit$')
    for theme, night in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', night)
        time.sleep(1)
        about = top_visible(r'^About(?:, (?:Expanded|Collapsed))?$')
        if 'Collapsed' in adrv.label(about):
            adrv.tap_node(about)
            visible(r'^About, Expanded$')
        run.record('album About and local art ' + theme, True,
                   artifacts=[run.shot('m10-album-' + theme)])

    mode_file.write_text('failure')
    open_about()
    tap(r'^Refresh metadata$')
    open_about()
    visible(r'Metadata refresh failed')
    visible(r'Fixture album description for Night Transit')
    top_visible(r'^Artwork for Night Transit$')
    assert state(run.admin, 'album', album_key)['fetched_at'] == ready['fetched_at']
    run.record('provider failure retains last biography and local album art', True,
               artifacts=[run.shot('m10-provider-failure')])

    mode_file.write_text('empty')
    open_about()
    tap(r'^Refresh metadata$')
    empty = wait_state(run.admin, 'album', album_key, 'empty', ready['fetched_at'])
    open_about()
    visible(r'^No metadata found for this artist or album\.$')
    assert not adrv.find(r'Fixture album description for Night Transit'), labels()
    top_visible(r'^Artwork for Night Transit$')
    run.record('provider no-match clears optional biography but retains local art', True,
               data={'state': empty['state']}, artifacts=[run.shot('m10-no-match')])

    mode_file.write_text('ready')
    open_about()
    tap(r'^Refresh metadata$')
    wait_state(run.admin, 'album', album_key, 'ready', empty['fetched_at'])
    open_about()
    visible(r'Fixture album description for Night Transit')
    source(run)

    # A dropped adb reverse cuts the actual native HTTP path, not a mocked API.
    port = run.state['port']
    adrv.adb('reverse', '--remove', f'tcp:{port}')
    try:
        open_about()
        tap(r'^Refresh metadata$')
        open_about()
        visible(r'Could not load metadata')
        visible(r'Fixture album description for Night Transit')
        run.record('network failure keeps cached About and shows Retry', True,
                   artifacts=[run.shot('m10-offline-retry')])
    finally:
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
    tap(r'^Retry$')
    visible(r'Fixture album description for Night Transit')
    run.record('Retry restores the same album metadata after reconnect', True)


def viewer_journey(run, album_key):
    sign_in(run, 'user')
    album()
    open_about()
    visible(r'Fixture album description for Night Transit')
    assert not any(re.search(r'^Refresh metadata$', label) for label in app_ui.labels()), labels()
    assert state(run.viewer, 'album', album_key)['can_refresh'] is False
    status, _ = run.viewer.call('POST', f'/api/v1/music/enrichment/album/{quote(album_key)}')
    assert status == 403, status
    run.record('viewer reads cached About without a refresh action', True,
               artifacts=[run.shot('m10-viewer-read-only')])

    user_id = next(user['id'] for user in run.admin.request('GET', '/api/v1/admin/users')
                   if user['username'] == run.auth['user']['username'])
    permission = {'set_id': run.state['set_ids']['Music'], 'user_id': user_id}
    run.admin.request('DELETE', '/api/v1/admin/permissions', permission)
    try:
        status, denied = run.viewer.call('GET', f'/api/v1/music/enrichment/album/{quote(album_key)}')
        assert status == 404 and 'Fixture album description' not in json.dumps(denied), (status, denied)
        app_ui.back()
        assert app_ui.go_home(), labels()
        adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
        app_ui.launch()
        assert adrv.wait('^Choose a server$', timeout=40), labels()
        saved = adrv.find(re.escape(run.base) + '$')
        assert saved, labels()
        adrv.tap_node(saved)
        assert adrv.wait('^Welcome back$', timeout=25), labels()
        password = [node for node in adrv.nodes() if node['cls'] == 'EditText'][-1]
        adrv.tap_node(password)
        assert app_ui.sign_in(run.auth['user']['password']), labels()
        albums.catalog()
        assert not any('Night Transit' in label or 'Fixture album description' in label
                       for label in app_ui.labels()), labels()
        run.record('revoked Music grant hides cached enrichment after native restart', True,
                   artifacts=[run.shot('m10-revoked')])
    finally:
        run.admin.request('POST', '/api/v1/admin/permissions', dict(permission, role='viewer'))
    album()
    open_about()
    visible(r'Fixture album description for Night Transit')
    run.record('restored Music grant reopens persisted metadata', True,
               artifacts=[run.shot('m10-restored')])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--apk', help='optional APK to install before the journey')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--evidence', help='external evidence directory')
    parser.add_argument('--provider-port', type=int, default=18910)
    parser.add_argument('--provider-mode-file', default='/tmp/player-m10-mode')
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--resume-device', action='store_true')
    parser.add_argument('--scenario', choices=['all', 'album', 'viewer'], default='all')
    args = parser.parse_args(argv)
    assert not args.host_alias, 'Provider image and offline acceptance require adb reverse'
    mode_file = Path(args.provider_mode_file)
    assert mode_file.is_file(), f'Provider mode file missing: {mode_file}'
    state_file = Path(args.fixture_dir) / 'state.json'
    port = json.loads(state_file.read_text())['port']
    run = None
    try:
        baseline.prepare_device(args, port)
        adrv.adb('reverse', f'tcp:{args.provider_port}', f'tcp:{args.provider_port}')
        run = Run(args)
        assert run.audio.available(), 'QEMU WAV capture required for actual output proof'
        albums_list = run.admin.request('GET', '/api/v1/music/albums')
        people = run.admin.request('GET', '/api/v1/music/artists')
        night = next(item for item in albums_list if item['title'] == 'Night Transit')
        aurora = next(item for item in people if item['name'] == 'Aurora Lane')
        assert night['cover_media_id'], 'Local cover required to prove preservation'
        assert state(run.admin, 'album', night['album_key'])['enabled'], 'Configure MUSIC_ENRICHMENT_URL on the fixture server'
        mode_file.write_text('ready')
        if args.scenario in ('all', 'album'):
            admin_journey(run, mode_file, night['album_key'], aurora['artist_key'],
                          skip_artist=args.scenario == 'album')
        if args.scenario in ('all', 'album', 'viewer'):
            viewer_journey(run, night['album_key'])
    except Exception:
        if run is None:
            raise
        trace = run.evidence.path('m10-failure.txt')
        trace.write_text(traceback.format_exc())
        run.record('native enrichment journey', False, traceback.format_exc(),
                   artifacts=[run.shot('m10-failure'), trace])
    finally:
        mode_file.write_text('ready')
        if not args.resume_device:
            baseline.teardown(run, port)
            adrv.adb('reverse', '--remove', f'tcp:{args.provider_port}')
    return 1 if not run or run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
