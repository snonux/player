#!/usr/bin/env python3
"""M48 native automatic music acceptance on one owned, rooted API34 emulator.

Requires a fresh disposable music fixture, installed release APK, adb reverse,
and QEMU WAV output. Only Player app data and this fixture's reverse route are
changed. The reverse route is restored even when a check fails.
"""
import argparse
import hashlib
import io
import json
from pathlib import Path
import re
import sqlite3
import tarfile
import tempfile
import time
import traceback
from datetime import datetime, timedelta, timezone

import adrv
import app_ui
import music_downloads_e2e as downloads
import music_offline_browse_e2e as browse
import music_playlists_e2e as playlists


STAR = 'Music/Gain Check/No Gain Tags.mp3'


def touch(pattern):
    playlists.tap_visible(pattern)
    time.sleep(.4)


def wait(pattern, timeout=35):
    node = adrv.wait(pattern, timeout=timeout)
    assert node, (pattern, app_ui.labels())
    return node


def retry_sync():
    touch(r'^(Sync now|Retry)$')


def table_rows(table):
    names = adrv.adb('shell', 'ls', downloads.PRIVATE).splitlines()
    files = [name for name in ('index.db', 'index.db-wal', 'index.db-shm') if name in names]
    assert 'index.db' in files, names
    raw = adrv.adb('exec-out', 'tar', '-C', downloads.PRIVATE, '-cf', '-', *files, binary=True)
    with tempfile.TemporaryDirectory(prefix='player-m48-index-') as directory:
        with tarfile.open(fileobj=io.BytesIO(raw)) as archive:
            for name in files:
                item = archive.extractfile(name)
                assert item is not None, name
                Path(directory, name).write_bytes(item.read())
        with sqlite3.connect(f'file:{directory}/index.db?mode=ro', uri=True) as database:
            database.row_factory = sqlite3.Row
            return [dict(row) for row in database.execute(f'SELECT * FROM {table}')]


class Run(browse.Run):
    def record(self, scenario, ok=True, detail='', data=None, artifacts=()):
        self.evidence.record('M48', scenario, 'pass' if ok else 'fail', detail, data, artifacts)
        print(('PASS ' if ok else 'FAIL ') + scenario + (f' [{detail}]' if detail else ''), flush=True)
        if not ok:
            self.failures += 1

    def settings(self):
        assert app_ui.go_home(), app_ui.labels()
        touch('^Settings$')
        wait('^Settings$')
        for _ in range(10):
            node = adrv.find('Automatic offline music')
            if node and 0 <= node['b'][1] < node['b'][3] <= 2220:
                touch('Automatic offline music')
                wait('Automatic offline music / .*Expanded')
                return
            app_ui.swipe(up=True)
        raise AssertionError(('Automatic offline music settings absent', app_ui.labels()))

    def status(self, pattern):
        for move in [None] + [True] * 10 + [False] * 18:
            if move is not None:
                app_ui.swipe(up=move)
            node = adrv.find(pattern)
            if node and node['b'][3] > node['b'][1]:
                return adrv.label(node)
        raise AssertionError((pattern, app_ui.labels()))

    def network_mode(self, value):
        header = self.status(r'Music network /')
        if 'Collapsed' in header:
            touch(r'Music network /')
        touch(r'^Network /')
        touch('^' + re.escape(value) + '$')
        self.status('Music network / Using ' + ('Mobile' if value.startswith('Mobile') else 'Wi-Fi'))

    def set_favorite(self, media_id, expected):
        favorites = self.viewer.request('GET', '/api/v1/media?favorites=true&type=audio&limit=200&offset=0')
        if any(row['id'] == media_id for row in favorites) != expected:
            self.viewer.request('POST', f'/api/v1/media/{media_id}/favorite')

    def locked(self):
        """A closed engine cannot run a job; unlock catches up a due source."""
        self.prepare()
        first = self.state['media_ids']['Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3']
        second = self.state['media_ids']['Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3']
        third = self.state['media_ids']['Music/Aurora Lane - Night Transit (2021)/03 - Harbour.flac']
        self.settings()
        touch('^Pinned playlists$')
        touch('^Keep music current$')
        retry_sync()
        self.status(r'2 current songs')
        self.viewer.request('POST', f'/api/v1/playlists/{self.playlist_id}/songs',
                            {'action': 'add', 'media_ids': [second]})
        auto_time = adrv.adb('shell', 'settings', 'get', 'global', 'auto_time').strip()
        try:
            adrv.adb('shell', 'settings', 'put', 'global', 'auto_time', '0')
            future = datetime.now(timezone.utc) + timedelta(hours=7)
            adrv.adb('shell', 'date', '-u', future.strftime('%m%d%H%M%Y.%S'))
            adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
            output = adrv.adb('shell', 'cmd', 'jobscheduler', 'run', '-f', app_ui.PKG, '19784')
            time.sleep(2)
            saved = next(row for row in table_rows('music_offline_pins')
                         if row['collection_key'] == self.playlist_id)
            assert sorted(json.loads(saved['media_ids'])) == sorted([first, third]), (output, saved)
            self.record('closed or locked app cannot publish automatic membership from a headless job',
                        data={'job_output': output.strip(), 'media_ids': [first, third]})

            app_ui.launch()
            landing = wait(r'^Choose a server$|^Welcome back$|^Library$', timeout=45)
            if adrv.label(landing) == 'Choose a server':
                touch('^' + re.escape(self.base) + '$')
                landing = wait('^Welcome back$')
            if adrv.label(landing) == 'Welcome back':
                fields = [node for node in adrv.nodes() if node['cls'] == 'EditText']
                assert fields, app_ui.labels()
                adrv.tap_node(fields[-1])
                assert app_ui.sign_in(self.auth['user']['password']), app_ui.labels()
            wait('^Library$')
            deadline = time.monotonic() + 45
            while time.monotonic() < deadline:
                saved = next(row for row in table_rows('music_offline_pins')
                             if row['collection_key'] == self.playlist_id)
                if sorted(json.loads(saved['media_ids'])) == sorted([first, second, third]):
                    break
                time.sleep(.8)
            else:
                raise AssertionError(('unlocked overdue sync did not catch up', saved))
            self.record('explicit sign-in and unlocked foreground catch up the due automatic playlist',
                        data={'media_ids': [first, second, third]},
                        artifacts=[self.shot('automatic-unlocked-catchup')])
        finally:
            now = datetime.now(timezone.utc)
            adrv.adb('shell', 'date', '-u', now.strftime('%m%d%H%M%Y.%S'))
            adrv.adb('shell', 'settings', 'put', 'global', 'auto_time', auto_time)

    def run(self):
        self.prepare()  # real UI pin of album and overlapping playlist
        first = self.state['media_ids']['Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3']
        second = self.state['media_ids']['Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3']
        third = self.state['media_ids']['Music/Aurora Lane - Night Transit (2021)/03 - Harbour.flac']
        starred = self.state['media_ids'][STAR]
        self.set_favorite(starred, False)
        self.settings()
        touch('^Pinned playlists$')
        touch('^Starred songs$')
        touch('^Keep music current$')
        wait('Automatic offline music / Enabled')
        jobs = adrv.adb('shell', 'dumpsys', 'jobscheduler')
        assert '19784' in jobs and app_ui.PKG in jobs, 'Android periodic job was not scheduled'
        self.record('native visible policy enables an OS periodic automatic job',
                    artifacts=[self.shot('automatic-enabled')])

        self.network_mode('Mobile (manual)')
        retry_sync()
        wait('^Sync waits for Wi-Fi$')
        assert sorted(json.loads(next(row['media_ids'] for row in table_rows('music_offline_pins')
                                      if row['collection_key'] == self.playlist_id))) == sorted([first, third])
        touch('^Download on Wi-Fi only$')
        retry_sync()
        self.status(r'Offline music is current')
        touch('^Download on Wi-Fi only$')
        self.network_mode('Wi-Fi (manual)')
        self.record('native Wi-Fi-only policy blocks Mobile, and manual override permits explicit sync',
                    artifacts=[self.shot('automatic-wifi-policy')])

        # Change the authoritative playlist and starred set on this disposable
        # server, then use the app's labeled Sync now control to reconcile them.
        self.viewer.request('POST', f'/api/v1/playlists/{self.playlist_id}/songs',
                            {'action': 'add', 'media_ids': [second]})
        self.set_favorite(starred, True)
        retry_sync()
        self.status(r'4 current songs')
        pins = table_rows('music_offline_pins')
        playlist = next(row for row in pins if row['origin'] == self.base
                        and row['user_id'] == self.user_id and row['collection_key'] == self.playlist_id)
        assert sorted(json.loads(playlist['media_ids'])) == sorted([first, second, third]), playlist
        automatic = next(row for row in pins if row['origin'] == self.base
                         and row['user_id'] == self.user_id and row['collection_key'] == '@automatic-starred')
        assert json.loads(automatic['media_ids']) == [starred], automatic
        rows = downloads.original_rows(downloads.Run.rows(self), self.base, self.user_id,
                                       [first, second, third, starred])
        owned = {row['download_key'] for row in table_rows('automatic_music_copies')}
        assert len(owned) == 1 and next(row for row in rows if row['media_id'] == starred)['key'] in owned
        saved_star = next(row for row in rows if row['media_id'] == starred)
        actual = adrv.adb('exec-out', 'cat',
                          f'{downloads.PRIVATE}/{saved_star["key"]}.media', binary=True)
        expected = Path(self.state['media_root'], STAR).read_bytes()
        assert len(actual) == saved_star['size_bytes'] == len(expected)
        assert hashlib.sha256(actual).digest() == hashlib.sha256(expected).digest()
        proof = {'media_id': starred, 'sha256': hashlib.sha256(actual).hexdigest(),
                 'size': len(actual)}
        self.record('native global Sync now reconciles changed playlist/starred membership and exact saved bytes',
                    data={'playlist_id': self.playlist_id, 'song_ids': [first, second, third, starred],
                          'owned_copies': len(owned), 'saved_proof': proof},
                    artifacts=[self.shot('automatic-reconciled')])

        # A forced OS wake is only meaningful once the chosen interval is due.
        # Move this disposable rooted device clock forward and restore it in
        # finally, then confirm the real JobService reaches a changed source.
        fourth = self.state['media_ids']['Music/Gain Check/Quiet.flac']
        self.viewer.request('POST', f'/api/v1/playlists/{self.playlist_id}/songs',
                            {'action': 'add', 'media_ids': [fourth]})
        auto_time = adrv.adb('shell', 'settings', 'get', 'global', 'auto_time').strip()
        try:
            adrv.adb('shell', 'settings', 'put', 'global', 'auto_time', '0')
            future = datetime.now(timezone.utc) + timedelta(hours=7)
            adrv.adb('shell', 'date', '-u', future.strftime('%m%d%H%M%Y.%S'))
            output = adrv.adb('shell', 'cmd', 'jobscheduler', 'run', '-f',
                              app_ui.PKG, '19784')
            assert 'Running job' in output, output
            self.status(r'5 current songs')
            scheduled = next(row for row in table_rows('music_offline_pins')
                             if row['collection_key'] == self.playlist_id)
            assert fourth in json.loads(scheduled['media_ids']), scheduled
            assert any(row['media_id'] == fourth for row in downloads.Run.rows(self))
            self.record('due Android JobScheduler wake fetches changed playlist and saves source bytes',
                        data={'job_id': 19784, 'added_media_id': fourth},
                        artifacts=[self.shot('automatic-scheduled')])
        finally:
            now = datetime.now(timezone.utc)
            adrv.adb('shell', 'date', '-u', now.strftime('%m%d%H%M%Y.%S'))
            adrv.adb('shell', 'settings', 'put', 'global', 'auto_time', auto_time)
        detail = self.viewer.request('GET', f'/api/v1/playlists/{self.playlist_id}')
        entry = next(item['entry_id'] for item in detail['entries'] if item['media_id'] == fourth)
        self.viewer.request('POST', f'/api/v1/playlists/{self.playlist_id}/songs',
                            {'action': 'remove', 'entry_id': entry})
        retry_sync()
        self.status(r'4 current songs')

        # Force a real transport outage. A failed retry cannot replace the
        # already committed snapshot or remove the downloaded song.
        port = self.state['port']
        original = browse.reverse_target(port)
        assert original == f'tcp:{port}', original
        browse.set_reverse(port, None)
        try:
            retry_sync()
            self.status(r'Sync unavailable|Could not|failed|unavailable')
            assert json.loads(next(row['media_ids'] for row in table_rows('music_offline_pins')
                                   if row['collection_key'] == self.playlist_id)) == [first, third, second]
            assert any(row['media_id'] == starred for row in downloads.Run.rows(self))
            self.record('unavailable server leaves the prior native snapshot and files intact',
                        artifacts=[self.shot('automatic-unavailable')])
        finally:
            browse.set_reverse(port, original)
        retry_sync()
        self.status(r'Offline music is current')
        self.record('native explicit Retry recovers after server reconnection')

        # The source playlist can contract without deleting borrowed or
        # retired audio. The automatic starred copy remains available locally.
        playlist_detail = self.viewer.request('GET', f'/api/v1/playlists/{self.playlist_id}')
        entry = next(item['entry_id'] for item in playlist_detail['entries'] if item['media_id'] == second)
        self.viewer.request('POST', f'/api/v1/playlists/{self.playlist_id}/songs',
                            {'action': 'remove', 'entry_id': entry})
        self.set_favorite(starred, False)
        retry_sync()
        self.status(r'2 current songs')
        pins = table_rows('music_offline_pins')
        playlist = next(row for row in pins if row['collection_key'] == self.playlist_id)
        assert sorted(json.loads(playlist['media_ids'])) == sorted([first, third]), playlist
        assert any(row['media_id'] == starred for row in downloads.Run.rows(self)), 'retired byte disappeared'
        self.record('removed membership reconciles while retired automatic audio remains safe',
                    artifacts=[self.shot('automatic-retired')])

        # The scheduled policy and snapshot survive process death. Open the
        # saved playlist offline and capture actual output through QEMU WAV.
        browse.set_reverse(port, None)
        try:
            self.cold_local_entry()
            browse.browse_kind('Playlists')
            browse.open_group(self.playlist_name, 2)
            mark = self.audio.mark()
            touch('^Play now$')
            deadline = time.monotonic() + 25
            while time.monotonic() < deadline:
                state = browse.baseline.session_state()
                if state['state'] == 'PLAYING' and state['title'] == 'First Light':
                    break
                time.sleep(.3)
            else:
                raise AssertionError(('offline automatic playlist did not play', browse.baseline.session_state()))
            time.sleep(3)
            adrv.adb('shell', 'input', 'keyevent', 'KEYCODE_MEDIA_PAUSE')
            wav = self.audio.save(mark, self.evidence.path('automatic-offline.wav'))
            matches = downloads.assert_audio(browse.baseline.audio_trace.analyze(wav), 440)
            self.record('process restart and offline playlist playback produce real saved audio',
                        data={'tone_hz': matches[0]['tone_hz']}, artifacts=[wav, self.shot('automatic-offline')])
        finally:
            browse.set_reverse(port, original)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--evidence-dir', required=True)
    parser.add_argument('--apk')
    parser.add_argument('--scenario', choices=('main', 'locked'), default='main')
    args = parser.parse_args(argv)
    args.host_alias = False
    fixture = Path(args.fixture_dir)
    state = json.loads((fixture / 'state.json').read_text())
    assert (fixture / '.player-music-fixture').is_file()
    assert state['origin'].startswith('http://127.0.0.1:')
    assert adrv.adb('shell', 'id', '-u').strip() == '0', 'rooted owned emulator required for read-only DB oracle'
    args.evidence_dir = str(Path(args.evidence_dir) / ('native-' + str(time.time_ns())))
    run = Run(args)
    try:
        if args.scenario == 'locked':
            run.locked()
        else:
            run.run()
    except Exception as error:
        run.record('native automatic offline journey', False, str(error), artifacts=[run.shot('failure')])
        traceback.print_exc()
    finally:
        browse.set_reverse(state['port'], f'tcp:{state["port"]}')
        run.set_favorite(state['media_ids'][STAR], False)
        if run.playlist_id:
            run.viewer.call('DELETE', f'/api/v1/playlists/{run.playlist_id}')
    print(f'M48 native failures: {run.failures}', flush=True)
    return 1 if run.failures else 0


if __name__ == '__main__':
    raise SystemExit(main())
