#!/usr/bin/env python3
"""M36 native personal listening on the owned 85 s / 18 s music fixture.

The caller owns an already running, user-signed-in app, adb reverse and QEMU
WAV. This runner never installs, clears app data, starts an emulator or stops
the server. Offline verification temporarily removes only the fixture reverse
mapping and restores it in finally. Run online first, then offline.
"""
import argparse
import io
import json
from pathlib import Path
import re
import sqlite3
import tarfile
import tempfile
import time
import traceback

import adrv
import app_ui
import music_albums_e2e as albums
import music_downloads_e2e as downloads
import music_e2e as baseline
import music_queue_recovery_e2e as recovery
import music_sequence_e2e as sequence
from fixture_server import Client

ALBUM = 'M36 Personal Ledger'
ARTIST = 'Ledger Artist'
LONG = 'Qualified Listening'
SHORT = 'Skipped Listening'
PRIVATE = downloads.PRIVATE


class Run(baseline.Run):
    def __init__(self, args):
        super().__init__(args, evidence_dir=args.evidence_dir)
        assert self.audio.available(), 'QEMU output WAV required'
        assert self.state['port'] == 18936 and self.state['origin'] == 'http://127.0.0.1:18936', self.state['origin']
        self.user = Client(self.state['origin']).login(**self.auth['user'])
        self.other = Client(self.state['origin']).login(**self.auth['admin'])
        self.user_id = next(row['id'] for row in self.admin.request('GET', '/api/v1/admin/users')
                            if row['username'] == self.auth['user']['username'])
        listing = self.other.request('GET', '/api/v1/music/albums')
        self.album_key = next(row['album_key'] for row in listing if row['title'] == ALBUM)
        artists = self.other.request('GET', '/api/v1/music/artists')
        self.artist_key = next(row['artist_key'] for row in artists if row['name'] == ARTIST)
        detail = self.album(self.user)
        self.ids = {name: next(row['id'] for row in detail['songs']
                               if row['music']['title'] == name)
                    for name in (LONG, SHORT)}
        assert all(Path(self.state['media_root'], 'Music', ALBUM, name).exists()
                   for name in ('01 - Qualified Listening.mp3', '02 - Skipped Listening.mp3'))
        self.failures = 0

    def record(self, scenario, ok=True, detail='', data=None, artifacts=(), result=None):
        result = result or ('pass' if ok else 'fail')
        self.evidence.record('M36', scenario, result, detail, data, artifacts,
                             proof='none' if result == 'gap' else 'real')
        print(result.upper(), scenario, detail, flush=True)
        self.failures += result == 'fail'

    def album(self, client):
        return client.request('GET', f'/api/v1/music/albums/{self.album_key}')

    def artist(self, client):
        return client.request('GET', f'/api/v1/music/artists/{self.artist_key}')

    def song(self, detail, title):
        return next(row for row in detail['songs'] if row['id'] == self.ids[title])


def pause():
    if baseline.session_state()['state'] == 'PLAYING':
        sequence.key('PAUSE')
    deadline = time.monotonic() + 10
    while baseline.session_state()['state'] == 'PLAYING' and time.monotonic() < deadline:
        time.sleep(.2)
    assert baseline.session_state()['state'] != 'PLAYING'


def audible(run, mark, name, hz):
    wav = run.audio.save(mark, run.evidence.path(name + '.wav'))
    analysis = baseline.audio_trace.analyze(wav)
    matching = downloads.assert_audio(analysis, hz)
    return wav, matching[0]


def visible_counts(run, detail, name):
    pause()
    albums.album(ALBUM)
    labels = ' | '.join(app_ui.labels())
    expected = [f'Your plays: {detail["personal_play_count"]}',
                f'Your plays: {run.song(detail, LONG)["personal_play_count"]}',
                f'Your plays: {run.song(detail, SHORT)["personal_play_count"]}',
                'Last played']
    assert all(label in labels for label in expected), (expected, labels)
    shot = run.shot(name)
    return shot, labels


def online(run):
    before = run.album(run.user)
    other_before = run.album(run.other)
    artist_before = run.artist(run.user)
    for title in (SHORT, LONG):
        baseline.reset_progress(run.user, run.ids[title])
    shot_before, _ = visible_counts(run, before, 'm36-user-before')

    short_before = run.song(before, SHORT)
    long_before = run.song(before, LONG)
    # The album row opens the native player. Pause after a few real seconds,
    # well below the 60 s qualification boundary.
    albums.album(ALBUM)
    mark = run.audio.mark()
    assert adrv.tap('^' + SHORT + '$'), app_ui.labels()
    sequence.wait_track(SHORT)
    time.sleep(4)
    pause()
    short_wav, short_audio = audible(run, mark, 'm36-below-threshold', 660)
    assert run.song(run.album(run.user), SHORT)['personal_play_count'] == short_before['personal_play_count']
    short_shot = run.shot('m36-skipped-player')

    albums.album(ALBUM)
    mark = run.audio.mark()
    assert adrv.tap('^' + LONG + '$'), app_ui.labels()
    sequence.wait_track(LONG)
    # Playback is left real-time; seeking cannot prove accumulated listening.
    deadline = time.monotonic() + 78
    after = run.album(run.user)
    while run.song(after, LONG)['personal_play_count'] == long_before['personal_play_count'] and time.monotonic() < deadline:
        assert baseline.session_state()['state'] == 'PLAYING', baseline.session_state()
        time.sleep(1)
        after = run.album(run.user)
    pause()
    long_wav, long_audio = audible(run, mark, 'm36-qualified', 440)
    assert run.song(after, LONG)['personal_play_count'] == long_before['personal_play_count'] + 1, after
    assert run.song(after, SHORT)['personal_play_count'] == short_before['personal_play_count'], after
    assert after['personal_play_count'] == before['personal_play_count'] + 1, (before, after)
    assert run.artist(run.user)['personal_play_count'] == artist_before['personal_play_count'] + 1
    assert run.song(after, LONG)['last_played_at'] and after['last_played_at'] == run.song(after, LONG)['last_played_at']
    assert after['last_played_at'] != before['last_played_at'], (before, after)
    assert run.album(run.other)['personal_play_count'] == other_before['personal_play_count']
    assert run.song(run.album(run.other), LONG)['personal_play_count'] == run.song(other_before, LONG)['personal_play_count']
    shot_after, labels = visible_counts(run, after, 'm36-user-qualified-album')
    run.record('real short skip and 60-second native qualification update personal song/album/artist/date only for user',
               data={'before': before['personal_play_count'], 'after': after['personal_play_count'],
                     'other': other_before['personal_play_count'], 'last_played_at': after['last_played_at'],
                     'short_audio': short_audio, 'long_audio': long_audio,
                     'visible_labels': labels[:900]},
               artifacts=[shot_before, short_shot, shot_after, short_wav, long_wav])

    recovery.login_other(run, run.auth['admin'])
    other_shot, other_labels = visible_counts(run, other_before, 'm36-other-account-album')
    assert f'Your plays: {other_before["personal_play_count"]}' in other_labels
    recovery.login_other(run, run.auth['user'])
    user_shot, _ = visible_counts(run, after, 'm36-user-after-account-switch')
    run.record('native account switch shows independent personal rows and restores user rows',
               data={'user': after['personal_play_count'], 'other': other_before['personal_play_count']},
               artifacts=[other_shot, user_shot])


def database_rows():
    """Read a consistent copy of device SQLite main/WAL without writing it."""
    names = adrv.adb('shell', 'ls', PRIVATE).splitlines()
    files = [name for name in ('index.db', 'index.db-wal', 'index.db-shm') if name in names]
    assert 'index.db' in files, names
    archive = adrv.adb('exec-out', 'tar', '-C', PRIVATE, '-cf', '-', *files, binary=True)
    with tempfile.TemporaryDirectory(prefix='player-m36-db-') as directory:
        with tarfile.open(fileobj=io.BytesIO(archive)) as bundle:
            for name in files:
                stream = bundle.extractfile(name)
                assert stream is not None
                Path(directory, name).write_bytes(stream.read())
        with sqlite3.connect(f'file:{directory}/index.db?mode=ro', uri=True) as db:
            db.row_factory = sqlite3.Row
            copies = [dict(row) for row in db.execute('SELECT * FROM downloads')]
            events = [dict(row) for row in db.execute('''SELECT p.id,p.owner_id,p.accumulated_seconds,p.observed_ms,
                i.origin,i.user_id,i.media_id FROM sync_music_plays p
                JOIN sync_items i ON i.key=p.owner_id''')]
    return copies, events


def saved_copy(run):
    copies, _ = database_rows()
    rows = [row for row in copies if row['origin'] == run.base and row['user_id'] == run.user_id
            and row['media_id'] == run.ids[LONG]]
    assert len(rows) <= 1, rows
    if rows:
        return rows[0]
    albums.album(ALBUM)
    # Album collection action opens the normal two-song Keep offline flow.
    node = baseline.find_visible('^Keep offline$')
    assert node, app_ui.labels()
    adrv.tap_node(node)
    assert adrv.wait('^Keep music offline$'), app_ui.labels()
    assert adrv.tap('^Keep offline$'), app_ui.labels()
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        copies, _ = database_rows()
        rows = [row for row in copies if row['origin'] == run.base and row['user_id'] == run.user_id
                and row['media_id'] == run.ids[LONG]]
        if rows:
            return rows[0]
        time.sleep(1)
    raise AssertionError('Qualified Listening was not saved offline')


def open_download():
    pause()
    assert app_ui.go_home(), app_ui.labels()
    assert adrv.tap('^Open navigation menu$'), app_ui.labels()
    assert adrv.tap('^Downloads$'), app_ui.labels()
    assert adrv.wait('^Downloads$'), app_ui.labels()
    node = app_ui.find_scrolling('^' + LONG + r'(?: /|$)')
    assert node, app_ui.labels()
    adrv.tap_node(node)
    assert adrv.wait('^Play$', timeout=20) or adrv.find('^Pause$'), app_ui.labels()


def offline(run):
    copy = saved_copy(run)
    assert copy['download_profile'] == 'original' and copy['size_bytes'] > 0, copy
    before = run.album(run.user)
    other_before = run.album(run.other)
    artist_before = run.artist(run.user)
    long_before = run.song(before, LONG)
    port = run.state['port']
    disconnected = False
    try:
        # Only the fixture's reverse mapping is touched; the server stays up.
        adrv.adb('reverse', '--remove', f'tcp:{port}')
        disconnected = True
        open_download()
        mark = run.audio.mark()
        if adrv.find('^Play$'):
            assert adrv.tap('^Play$'), app_ui.labels()
        sequence.wait_track(LONG)
        # Start from a saved copy near zero. A resumed copy too close to the
        # end cannot qualify; the run fails instead of manufacturing an event.
        position = baseline.session_state()['position_ms']
        assert position is not None and position < 15000, ('offline copy must start near zero', position)
        time.sleep(67)
        assert baseline.session_state()['state'] == 'PLAYING', baseline.session_state()
        pause()
        wav, audio = audible(run, mark, 'm36-offline-qualified', 440)
        copies, events = database_rows()
        pending = [row for row in events if row['origin'] == run.base and row['user_id'] == run.user_id
                   and row['media_id'] == run.ids[LONG] and row['accumulated_seconds'] >= 60]
        assert len(pending) == 1, pending
        assert re.fullmatch('[0-9a-f]{32}', pending[0]['id']), pending
        assert run.song(run.album(run.user), LONG)['personal_play_count'] == long_before['personal_play_count']
        shot = run.shot('m36-offline-pending')
        run.record('saved offline song creates one durable qualified listening UUID while disconnected',
                   data={'event': pending[0], 'audio': audio, 'server_count': long_before['personal_play_count']},
                   artifacts=[shot, wav])

        # Stop and inspect while disconnected, then relaunch after reconnect.
        adrv.adb('shell', 'am', 'force-stop', app_ui.PKG)
        _, persisted = database_rows()
        assert any(row['id'] == pending[0]['id'] for row in persisted), persisted
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
        disconnected = False
        app_ui.launch()
        if adrv.wait('^Welcome back$', timeout=8):
            fields = [node for node in adrv.nodes() if node['cls'] == 'EditText']
            assert fields, app_ui.labels()
            adrv.tap_node(fields[-1])
            assert app_ui.sign_in(run.auth['user']['password']), app_ui.labels()
        assert adrv.wait('^Library$', timeout=30), app_ui.labels()
        if adrv.find('^Sync now$'):
            adrv.tap('^Sync now$')
        deadline = time.monotonic() + 45
        after = run.album(run.user)
        while run.song(after, LONG)['personal_play_count'] == long_before['personal_play_count'] and time.monotonic() < deadline:
            time.sleep(1)
            after = run.album(run.user)
        assert run.song(after, LONG)['personal_play_count'] == long_before['personal_play_count'] + 1, after
        assert after['personal_play_count'] == before['personal_play_count'] + 1, after
        assert after['last_played_at'] != before['last_played_at'], (before, after)
        assert run.artist(run.user)['personal_play_count'] == artist_before['personal_play_count'] + 1
        assert run.song(run.album(run.other), LONG)['personal_play_count'] == run.song(other_before, LONG)['personal_play_count']
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            _, remaining = database_rows()
            if not any(row['id'] == pending[0]['id'] for row in remaining):
                break
            time.sleep(.5)
        assert not any(row['id'] == pending[0]['id'] for row in remaining), remaining
        final_shot, _ = visible_counts(run, after, 'm36-offline-synced-album')
        run.record('restarted native client syncs exactly one offline qualified event to account and acknowledges UUID',
                   data={'event_id': pending[0]['id'], 'before': before['personal_play_count'],
                         'after': after['personal_play_count'], 'other': other_before['personal_play_count'],
                         'last_played_at': after['last_played_at']},
                   artifacts=[final_shot])
    finally:
        if disconnected:
            adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--fixture-dir', required=True)
    parser.add_argument('--evidence-dir', required=True)
    parser.add_argument('--emulator-wav', required=True)
    parser.add_argument('--apk', help='Installed APK path for evidence hash only')
    parser.add_argument('--host-alias', action='store_true')
    parser.add_argument('--scenario', choices=('online', 'offline', 'all'), default='online')
    args = parser.parse_args(argv)
    if args.host_alias and args.scenario in ('offline', 'all'):
        parser.error('offline requires adb reverse; omit --host-alias')
    run = Run(args)
    try:
        if args.scenario in ('online', 'all'):
            online(run)
        if args.scenario in ('offline', 'all'):
            offline(run)
    except Exception as error:
        trace = traceback.format_exc()
        path = run.evidence.path('m36-failure.txt')
        path.write_text(trace)
        run.record('native acceptance interrupted', False, str(error), artifacts=[path])
    return 1 if run.failures else 0


if __name__ == '__main__':
    raise SystemExit(main())
