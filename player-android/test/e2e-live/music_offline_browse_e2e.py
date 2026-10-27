#!/usr/bin/env python3
"""M49 native offline music browser acceptance on one leased emulator.

Use a fresh disposable music fixture and an installed app with QEMU WAV output.
This runner clears only Player app data at startup to select its fixture. It
never installs an APK, restarts the emulator, or stops the fixture server.
After pinning through the app it removes only this fixture's adb reverse route,
cold-opens local files, and restores the prior reverse route on exit.

python3 -u player-android/test/e2e-live/music_offline_browse_e2e.py \
  /home/paul/tmp/codex/player-m49-18846 \
  --emulator-wav /path/to/emulator-output.wav \
  --evidence-dir /home/paul/tmp/codex/player-m49-native-evidence
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
import music_albums_e2e as albums
import music_downloads_e2e as downloads
import music_e2e as baseline
import music_playlists_e2e as playlists
from fixture_server import Client


ALBUM = 'Night Transit'
FIRST = 'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3'


def reverse_target(port):
    for line in adrv.adb('reverse', '--list').splitlines():
        parts = line.split()
        if len(parts) >= 3 and parts[1] == f'tcp:{port}':
            return parts[2]
    return None


def set_reverse(port, target):
    if target is None:
        adrv.adb('reverse', '--remove', f'tcp:{port}')
    else:
        adrv.adb('reverse', f'tcp:{port}', target)
    assert reverse_target(port) == target, ('adb reverse mismatch', port, target)


def touch(pattern):
    assert adrv.tap(pattern), (pattern, app_ui.labels())
    time.sleep(.3)


def wait(pattern, timeout=25):
    assert adrv.wait(pattern, timeout=timeout), (pattern, app_ui.labels())


def browse_kind(kind):
    # Form-field semantics include its visible label and selected value.
    fields = [node for node in adrv.nodes()
              if re.search(r'(^| / )Browse($| / )', adrv.label(node))]
    field = next((node for node in fields if node['click']), None)
    field = field or (fields[0] if fields else None)
    assert field, app_ui.labels()
    adrv.tap_node(field)
    touch('^' + re.escape(kind) + '$')
    wait('^Offline music$')


def open_group(title, count):
    pattern = '^' + re.escape(title) + r'(?: /|$)'
    node = adrv.find(pattern)
    assert node, (pattern, app_ui.labels())
    adrv.tap_node(node)
    wait(rf'{count} of {count} songs available')
    wait('^Play now$')


def close_group():
    app_ui.back()
    wait('^Offline music$')


def search(text):
    fields = [node for node in adrv.nodes() if node['cls'] == 'EditText']
    field = next((node for node in fields
                  if 'Search downloaded music' in adrv.label(node)), None)
    field = field or (fields[0] if len(fields) == 1 else None)
    assert field, app_ui.labels()
    adrv.tap_node(field)
    app_ui.clear_field(80)
    if text:
        adrv.type_text(text)
    # The first Back dismisses the software keyboard and keeps the browser.
    app_ui.back()
    wait('^Offline music$')


class Run(baseline.Run):
    def __init__(self, args):
        super().__init__(args, evidence_dir=args.evidence_dir)
        self.failures = 0
        self.viewer = Client(self.state['origin']).login(**self.auth['user'])
        self.user_id = next(user['id'] for user in
                            self.admin.request('GET', '/api/v1/admin/users')
                            if user['username'] == self.auth['user']['username'])
        self.playlist_id = None
        self.playlist_name = 'M49 native offline mix ' + str(time.time_ns())
        assert self.audio.available(), 'QEMU WAV capture is required'

    def record(self, scenario, ok=True, detail='', data=None, artifacts=()):
        result = 'pass' if ok else 'fail'
        self.evidence.record('M49', scenario, result, detail, data, artifacts)
        print(result.upper(), scenario, detail, flush=True)
        if not ok:
            self.failures += 1

    def pin(self, title, count):
        playlists.tap_visible('^Pin offline…$')
        wait('^' + re.escape(title) + '$')
        touch('^Pin offline$')
        wait(rf'^Pinned · {count} of {count} songs downloaded$', timeout=100)
        shot = self.shot('pinned-' + re.sub(r'\W+', '-', title.lower()))
        touch('^Close$')
        return shot

    def prepare(self):
        checks = []
        ok = app_ui.connect_and_login(self.base, self.auth['user']['username'],
                                      self.auth['user']['password'],
                                      lambda name, passed, detail='': checks.append((name, passed, detail)),
                                      self.shot)
        assert ok and all(passed for _, passed, _ in checks), checks
        self.record('fresh app connects to disposable server and signs in',
                    data={'origin': self.base, 'user_id': self.user_id})

        album = next(row for row in self.viewer.request('GET', '/api/v1/music/albums')
                     if row['title'] == ALBUM)
        songs = self.viewer.request('GET', '/api/v1/music/albums/' + album['album_key'])['songs']
        assert len(songs) == 3, songs
        albums.album(ALBUM)
        album_shot = self.pin(ALBUM, 3)
        rows = downloads.original_rows(downloads.Run.rows(self), self.base,
                                       self.user_id, [song['id'] for song in songs])
        self.record('pin Night Transit through actual album UI and retain three original files',
                    data={'album_key': album['album_key'], 'media_ids': [song['id'] for song in songs],
                          'sizes': [row['size_bytes'] for row in rows]},
                    artifacts=[album_shot])

        created = self.viewer.request('POST', '/api/v1/playlists',
                                     {'name': self.playlist_name})
        self.playlist_id = created['playlist_id']
        self.viewer.request('POST', f'/api/v1/playlists/{self.playlist_id}/songs',
                            {'action': 'add', 'media_ids': [songs[0]['id'], songs[2]['id']]})
        playlists.catalog()
        node = app_ui.find_scrolling(r'(^| / )' + re.escape(self.playlist_name))
        assert node, app_ui.labels()
        adrv.tap_node(node)
        playlists.wait_detail(self.playlist_name)
        playlist_shot = self.pin(self.playlist_name, 2)
        self.record('overlapping playlist is pinned through actual UI',
                    data={'playlist_id': self.playlist_id, 'song_ids':
                          [songs[0]['id'], songs[2]['id']]}, artifacts=[playlist_shot])

    def cold_local_entry(self):
        port = self.state['port']
        set_reverse(port, None)
        adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
        app_ui.launch()
        landing = adrv.wait(r'^Choose a server$|^Welcome back$|^On this device$',
                            timeout=45)
        assert landing, app_ui.labels()
        if adrv.label(landing) != 'On this device':
            touch('^Use files on this device$')
        wait('^On this device$', timeout=30)
        touch('^Downloads$')
        wait('^Downloads$')
        touch('^Browse offline music$')
        wait('^Offline music$')
        assert not adrv.find('^Welcome back$'), app_ui.labels()
        assert reverse_target(port) is None
        self.record('cold relaunch opens device-local browser with fixture reverse removed',
                    data={'server_port': port, 'account': self.user_id},
                    artifacts=[self.shot('cold-offline-browser')])

    def browse(self):
        wait('^Saved server and account')
        assert sum(node['cls'] == 'EditText' for node in adrv.nodes()) == 1
        wait('^First Light(?: /|$)')
        open_group('First Light', 1)
        wait('Downloaded')
        close_group()
        search('zzzm49nomatch')
        wait(r'^No downloaded music matches\.$')
        search('First Light')
        wait('^First Light(?: /|$)')
        search('')
        self.record('Songs browse and local search work with no server route',
                    artifacts=[self.shot('offline-songs-search')])

        browse_kind('Albums')
        open_group(ALBUM, 3)
        adrv.adb('shell', 'cmd', 'uimode', 'night', 'no')
        time.sleep(1)
        light = self.shot('offline-album-light')
        close_group()
        browse_kind('Artists')
        open_group('Aurora Lane', 2)
        adrv.adb('shell', 'cmd', 'uimode', 'night', 'yes')
        time.sleep(1)
        theme = self.shot('offline-artist-dark')
        wait('^Play now$')
        close_group()
        browse_kind('Playlists')
        open_group(self.playlist_name, 2)
        playlist = self.shot('offline-playlist')
        close_group()
        self.record('Albums, credited Artists, Playlists, Back and theme remain usable offline',
                    artifacts=[light, theme, playlist])

    def play(self):
        browse_kind('Albums')
        open_group(ALBUM, 3)
        first = self.track(FIRST)
        mark = self.audio.mark()
        touch('^Play now$')
        deadline = time.monotonic() + 25
        current = baseline.session_state()
        while (current['state'] != 'PLAYING' or current['title'] != first['title']) and time.monotonic() < deadline:
            time.sleep(.3)
            current = baseline.session_state()
        assert current['state'] == 'PLAYING' and current['title'] == first['title'], current
        time.sleep(3)
        after = baseline.session_state()
        assert after['state'] == 'PLAYING' and after['title'] == first['title'], (current, after)
        adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
        wav = self.audio.save(mark, self.evidence.path('offline-first-light.wav'))
        analysis = baseline.audio_trace.analyze(wav)
        matches = downloads.assert_audio(analysis, first['tone_hz'][0])
        touch('^Now Playing queue$')
        wait('^Now Playing$')
        assert not adrv.find('^Continue session$'), app_ui.labels()
        assert not adrv.find('^Save as playlist$'), app_ui.labels()
        queue_shot = self.shot('offline-queue')
        app_ui.back()
        wait('^Offline music$')
        self.record('offline album queue plays the saved first song with real QEMU audio',
                    data={'media_id': first['id'], 'before': current, 'after': after,
                          'tone_hz': first['tone_hz'][0], 'matches': matches},
                    artifacts=[wav, queue_shot])

    def reconnect(self):
        port = self.state['port']
        set_reverse(port, f'tcp:{port}')
        wait('^Offline music$')
        # Network restoration alone must not silently replace the local view.
        self.record('reconnected transport retains the local browser until explicit navigation',
                    artifacts=[self.shot('reconnected-still-offline')])
        app_ui.back()
        wait('^Downloads$')
        app_ui.back()
        wait('^On this device$')
        touch('^Connect to server$')
        landing = adrv.wait(r'^Choose a server$|^Welcome back$', timeout=30)
        assert landing, app_ui.labels()
        if adrv.label(landing) == 'Choose a server':
            touch('^' + re.escape(self.base) + '$')
        wait('^Welcome back$', timeout=30)
        fields = [node for node in adrv.nodes() if node['cls'] == 'EditText']
        assert len(fields) >= 2, app_ui.labels()
        adrv.tap_node(fields[0]); app_ui.clear_field(80)
        adrv.type_text(self.auth['user']['username'])
        adrv.tap_node(fields[1]); app_ui.clear_field(80)
        assert app_ui.sign_in(self.auth['user']['password']), app_ui.labels()
        wait('^Library$', timeout=30)
        self.record('explicit reconnect and sign-in return to the online library',
                    artifacts=[self.shot('reconnected-library')])

    def partial_playlist(self):
        assert app_ui.go_home(), app_ui.labels()
        touch('^Open navigation menu$')
        touch('^Downloads$')
        wait('^Downloads$')
        row = app_ui.find_scrolling(r'^Harbour /')
        assert row, app_ui.labels()
        remove = next((node for node in adrv.nodes()
                       if adrv.label(node) == 'Remove download'
                       and row['b'][1] <= node['b'][1] <= row['b'][3]), None)
        assert remove, app_ui.labels()
        adrv.tap_node(remove)
        wait(r'^Remove download\?$')
        touch('^Remove download$')
        deadline = time.monotonic() + 15
        while any(r['title'] == 'Harbour' and r['user_id'] == self.user_id
                  for r in downloads.Run.rows(self)) and time.monotonic() < deadline:
            time.sleep(.3)
        assert not any(r['title'] == 'Harbour' and r['user_id'] == self.user_id
                       for r in downloads.Run.rows(self))
        self.cold_local_entry()
        browse_kind('Playlists')
        row = adrv.find('^' + re.escape(self.playlist_name) + r' / 1 of 2 songs available$')
        assert row, app_ui.labels()
        adrv.tap_node(row)
        wait('1 of 2 songs available')
        wait('Some downloads are missing. Reconnect and download them again.')
        detail = self.shot('offline-partial-playlist-guidance')
        touch('^Play now$')
        deadline = time.monotonic() + 25
        current = baseline.session_state()
        while (current['state'] != 'PLAYING' or current['title'] != 'First Light') and time.monotonic() < deadline:
            time.sleep(.3)
            current = baseline.session_state()
        assert current['state'] == 'PLAYING' and current['title'] == 'First Light', current
        touch('^Now Playing queue$')
        labels = app_ui.labels()
        assert sum(' / First Light / ' in label for label in labels) == 1, labels
        assert not any('Harbour' in label for label in labels), labels
        self.record('partial playlist guides reconnection and queues only its saved song offline',
                    data={'available': 1, 'total': 2, 'playback': current,
                          'reverse': reverse_target(self.state['port'])},
                    artifacts=[detail, self.shot('offline-partial-playlist-queue')])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--evidence-dir', required=True)
    parser.add_argument('--apk', help='Optional read-only APK identity; never installed')
    args = parser.parse_args(argv)
    fixture = Path(args.fixture_dir)
    state = json.loads((fixture / 'state.json').read_text())
    assert (fixture / '.player-music-fixture').is_file(), 'Disposable fixture marker required'
    assert state['origin'].startswith('http://127.0.0.1:'), 'Loopback fixture required'
    args.host_alias = False
    args.evidence_dir = str(Path(args.evidence_dir) / ('m49-' + str(time.time_ns())))
    run = Run(args)
    port = state['port']
    previous_reverse = reverse_target(port)
    previous_theme = adrv.adb('shell', 'cmd', 'uimode', 'night').strip().rsplit(' ', 1)[-1]
    try:
        # The release app's private download index is readable only through
        # root adb on this disposable emulator; pinning itself uses the UI.
        adrv.adb('root')
        adrv.adb('wait-for-device')
        set_reverse(port, f'tcp:{port}')
        run.prepare()
        run.cold_local_entry()
        run.browse()
        run.play()
        run.reconnect()
        run.partial_playlist()
    except Exception:
        failure = run.evidence.path('m49-failure.txt')
        failure.write_text(traceback.format_exc())
        artifacts = [failure]
        try:
            artifacts.append(run.shot('m49-failure'))
        except Exception:
            pass
        run.record('native offline browser journey', False, traceback.format_exc(),
                   artifacts=artifacts)
    finally:
        set_reverse(port, previous_reverse)
        if previous_theme in ('yes', 'no', 'auto'):
            adrv.adb('shell', 'cmd', 'uimode', 'night', previous_theme)
        if run.playlist_id:
            run.viewer.request('DELETE', f'/api/v1/playlists/{run.playlist_id}')
        print('Evidence:', run.evidence.directory, flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    sys.exit(main())
