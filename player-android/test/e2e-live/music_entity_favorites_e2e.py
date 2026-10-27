#!/usr/bin/env python3
"""M34 native entity-heart acceptance on a root-owned emulator and music fixture.

Run after the root installs the current APK on a running emulator. This runner
uses the real app, touch controls and server; it does not create an emulator or
install an APK. It clears only Player app data when changing fixture accounts.
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
from fixture_server import Client


class Run(baseline.Run):
    def __init__(self, args):
        super().__init__(args, evidence_dir=args.evidence)
        self.viewer = Client(self.state['origin']).login(**self.auth['user'])
        self.failures = 0

    def record(self, scenario, ok, detail='', data=None, artifacts=()):
        self.evidence.record('M34', scenario, 'pass' if ok else 'fail', detail,
                             data, artifacts)
        print(f"{'PASS' if ok else 'FAIL'} M34 {scenario} {detail}", flush=True)
        if not ok:
            self.failures += 1


def tap(pattern):
    assert adrv.tap(pattern), (pattern, app_ui.labels())
    time.sleep(.4)


def touch_item(pattern):
    node = app_ui.find_scrolling(pattern)
    assert node, (pattern, app_ui.labels())
    adrv.tap_node(node)
    time.sleep(.6)


def catalog(name):
    assert app_ui.go_home(), app_ui.labels()
    tap('^Open navigation menu$')
    tap('^Music · ' + name + '$')
    assert adrv.wait('^Music · ' + name + '$', timeout=25), app_ui.labels()


def favorite_keys(client, kind):
    path = '/api/v1/playlists' if kind == 'playlist' else '/api/v1/music/' + kind + 's'
    field = 'playlist_id' if kind == 'playlist' else kind + '_key'
    return {item[field] for item in client.request('GET', path + '?favorite=true')}


def wait_favorite(client, kind, key, wanted):
    deadline = time.monotonic() + 12
    while time.monotonic() < deadline:
        actual = key in favorite_keys(client, kind)
        if actual == wanted:
            return
        time.sleep(.3)
    raise AssertionError((kind, key, wanted, favorite_keys(client, kind)))


def toggle_detail(run, kind, key, title):
    tap('^Favorite ' + kind + '$')
    assert adrv.wait('^Unfavorite ' + kind + '$'), app_ui.labels()
    wait_favorite(run.viewer, kind, key, True)
    run.record(kind + ' detail touch heart saves scoped entity', True,
               data={'key': key, 'title': title},
               artifacts=[run.shot('m34-' + kind + '-detail')])


def catalog_favorite(run, kind, name, title):
    catalog(name)
    tap('^Favorites only$')
    assert app_ui.find_scrolling(r'(^| / )' + re.escape(title)), app_ui.labels()
    run.record(kind + ' Favorites only shows the saved item', True,
               data={'title': title}, artifacts=[run.shot('m34-' + kind + '-filter')])
    tap('^Show all ' + name.lower() + '$')


def sign_in(run, account):
    credentials = run.auth[account]
    assert app_ui.connect_and_login(run.base, credentials['username'],
                                    credentials['password'],
                                    lambda name, ok, detail='': run.record(
                                        account + ' sign-in ' + name, ok, detail),
                                    run.shot), app_ui.labels()


def journey(run):
    albums = run.viewer.request('GET', '/api/v1/music/albums')
    album = next(item for item in albums if item['title'] == 'Night Transit')
    artist = next(item for item in run.viewer.request('GET', '/api/v1/music/artists')
                  if item['name'] == 'Aurora Lane')
    album_key, artist_key = album['album_key'], artist['artist_key']
    media_id = run.state['media_ids'][
        'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3']
    # Fixture reuse must never turn a heart tap into an accidental unheart.
    for kind, key in [('album', album_key), ('artist', artist_key)]:
        run.viewer.request('DELETE', '/api/v1/music/favorites/' + kind + '/' + quote(key))
    for key in favorite_keys(run.viewer, 'playlist'):
        run.viewer.request('DELETE', '/api/v1/music/favorites/playlist/' + quote(key))
    file_favorites = lambda: {item['id'] for item in run.viewer.request(
        'GET', '/api/v1/media?favorites=true&limit=1000')}
    if media_id not in file_favorites():
        run.viewer.request('POST', f'/api/v1/media/{media_id}/favorite')
    assert media_id in file_favorites()

    name = 'M34 Native ' + str(int(time.time()))
    playlist = run.viewer.request('POST', '/api/v1/playlists',
                                  {'name': name, 'comment': 'Entity favorite',
                                   'visibility': 'shared'})
    playlist_id = playlist['playlist_id']
    sign_in(run, 'user')

    catalog('Albums')
    touch_item(r'(^| / )Night Transit')
    assert adrv.wait('^Favorite album$', timeout=20), app_ui.labels()
    toggle_detail(run, 'album', album_key, 'Night Transit')
    app_ui.back()
    catalog_favorite(run, 'album', 'Albums', 'Night Transit')

    catalog('Artists')
    touch_item(r'(^| / )Aurora Lane')
    assert adrv.wait('^Favorite artist$', timeout=20), app_ui.labels()
    toggle_detail(run, 'artist', artist_key, 'Aurora Lane')
    app_ui.back()
    catalog_favorite(run, 'artist', 'Artists', 'Aurora Lane')

    catalog('Playlists')
    touch_item(r'(^| / )' + re.escape(name))
    assert adrv.wait('^Favorite playlist$', timeout=20), app_ui.labels()
    toggle_detail(run, 'playlist', playlist_id, name)
    app_ui.back()
    catalog_favorite(run, 'playlist', 'Playlists', name)

    # Offline is a real network cutoff for the app's loopback endpoint. The
    # previous UI and saved value must remain; restoring reverse permits retry.
    catalog('Albums')
    touch_item(r'(^| / )Night Transit')
    assert adrv.wait('^Unfavorite album$'), app_ui.labels()
    port = run.state['port']
    adrv.adb('reverse', '--remove', f'tcp:{port}')
    try:
        tap('^Unfavorite album$')
        assert adrv.wait('Could not save favorite', timeout=30), app_ui.labels()
        assert adrv.find('^Unfavorite album$'), app_ui.labels()
        wait_favorite(run.viewer, 'album', album_key, True)
        run.record('offline touch failure keeps album heart and offers retry', True,
                   artifacts=[run.shot('m34-offline-retained')])
    finally:
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
    tap('^Unfavorite album$')
    assert adrv.wait('^Favorite album$'), app_ui.labels()
    wait_favorite(run.viewer, 'album', album_key, False)
    app_ui.back()
    assert adrv.wait('^Music · Albums$'), app_ui.labels()
    tap('^Favorites only$')
    assert adrv.wait('No favorite albums\\. Show all albums to favorite one\\.'), app_ui.labels()
    run.record('empty Favorites only collection gives Show all guidance', True,
               artifacts=[run.shot('m34-empty-albums')])
    tap('^Show all albums$')
    touch_item(r'(^| / )Night Transit')
    assert adrv.wait('^Favorite album$'), app_ui.labels()
    tap('^Favorite album$')
    wait_favorite(run.viewer, 'album', album_key, True)
    run.record('reconnected touch retry saves album heart', True,
               artifacts=[run.shot('m34-reconnected')])

    shots = []
    for theme, mode in [('light', 'no'), ('dark', 'yes')]:
        adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
        time.sleep(2)
        assert adrv.find('^Unfavorite album$'), app_ui.labels()
        shots.append(run.shot('m34-album-' + theme))
    app_ui.back()
    assert adrv.wait('^Music · Albums$'), app_ui.labels()
    run.record('native light/dark detail and predictable Back', True,
               artifacts=shots)

    # A shared playlist remains readable to another account, but its heart is
    # personal. Check actual native controls before the second account saves it.
    sign_in(run, 'admin')
    catalog('Playlists')
    touch_item(r'(^| / )' + re.escape(name))
    assert adrv.wait('^Favorite playlist$'), app_ui.labels()
    tap('^Favorite playlist$')
    wait_favorite(run.admin, 'playlist', playlist_id, True)
    assert album_key not in favorite_keys(run.admin, 'album')
    assert artist_key not in favorite_keys(run.admin, 'artist')
    run.record('second native account has independent shared-playlist heart', True,
               data={'playlist_id': playlist_id},
               artifacts=[run.shot('m34-admin-playlist')])

    sign_in(run, 'user')
    assert {album_key} <= favorite_keys(run.viewer, 'album')
    assert {artist_key} <= favorite_keys(run.viewer, 'artist')
    assert {playlist_id} <= favorite_keys(run.viewer, 'playlist')
    assert media_id in file_favorites()
    catalog('Playlists')
    touch_item(r'(^| / )' + re.escape(name))
    assert adrv.wait('^Unfavorite playlist$'), app_ui.labels()
    run.record('return to user retains all three hearts and original file heart',
               True, data={'album_key': album_key, 'artist_key': artist_key,
                           'playlist_id': playlist_id, 'media_id': media_id},
               artifacts=[run.shot('m34-return-user')])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--evidence', help='external evidence directory')
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--emulator-wav', help='optional QEMU audio path for version evidence')
    args = parser.parse_args(argv)
    args.apk = None  # Root installs one shared APK before serial native journeys.
    port = json.loads((Path(args.fixture_dir) / 'state.json').read_text())['port']
    run = None
    try:
        baseline.prepare_device(args, port)
        run = Run(args)
        journey(run)
    except Exception:
        if run:
            trace = run.evidence.path('m34-failed.txt')
            trace.write_text(traceback.format_exc())
            run.record('native journey', False, traceback.format_exc(),
                       artifacts=[run.shot('m34-failed'), trace])
        else:
            traceback.print_exc()
    finally:
        if run:
            baseline.teardown(run, port)
    return 1 if not run or run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
