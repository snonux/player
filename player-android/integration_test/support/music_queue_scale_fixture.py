#!/usr/bin/env python3
"""Add a 1001-song album to an owned disposable music fixture for M13 checks.

Uses one tagged FLAC and hard links: distinct media paths/IDs and readable
filenames, identical known 440Hz audio. Never touches a deployed library.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time

from fixture_server import Client


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir', type=Path)
    args = parser.parse_args()
    directory = args.fixture_dir.resolve(strict=True)
    assert (directory / '.player-music-fixture').is_file(), 'owned disposable fixture marker required'
    state = json.loads((directory / 'state.json').read_text())
    auth = json.loads((directory / 'auth.json').read_text())
    assert state['origin'].startswith('http://127.0.0.1:'), state['origin']
    root = Path(state['media_root']).resolve(strict=True)
    assert root.is_relative_to(directory), 'media must live inside disposable fixture'
    folder = root / 'Music' / 'Queue Scale'
    folder.mkdir(exist_ok=True)
    first = folder / 'Queue 0001.flac'
    if not first.exists():
        subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-y', '-f', 'lavfi',
                        '-i', 'sine=frequency=440:duration=12', '-metadata', 'album=Queue Scale',
                        '-metadata', 'album_artist=Queue Scale', '-metadata', 'artist=Queue Scale',
                        '-c:a', 'flac', str(first)], check=True)
    for i in range(2, 1002):
        path = folder / f'Queue {i:04}.flac'
        if not path.exists():
            os.link(first, path)
    admin = Client(state['origin']).login(**auth['admin'])
    admin.request('POST', '/api/v1/admin/rescan', {})
    deadline = time.monotonic() + 300
    while time.monotonic() < deadline:
        albums = admin.request('GET', '/api/v1/music/albums')
        album = next((a for a in albums if a['title'] == 'Queue Scale'), None)
        if album and album['song_count'] == 1001:
            detail = admin.request('GET', '/api/v1/music/albums/' + album['album_key'])
            result = {'album': album, 'first': detail['songs'][0], 'last': detail['songs'][-1], 'count': len(detail['songs'])}
            (directory / 'queue-scale.json').write_text(json.dumps(result, indent=2))
            print(json.dumps({'album_key': album['album_key'], 'song_count': album['song_count']}))
            return
        time.sleep(.5)
    raise AssertionError('1001-song scale album did not finish scanning')


if __name__ == '__main__':
    main()
