"""Retag one known synthetic M01 fixture track, never an arbitrary library."""
import argparse
import json
from pathlib import Path
import os
from music_library import TRACKS, ffmpeg

TRACK = 'Music/Tag Verification/source_001.flac'


def retag(fixture_dir, state):
    fixture = Path(fixture_dir).resolve()
    if not (fixture / '.player-music-fixture').is_file():
        raise ValueError('Not a disposable music fixture')
    config = json.loads((fixture / 'state.json').read_text())
    root = Path(config['media_root']).resolve()
    if not root.is_relative_to(fixture):
        raise ValueError('Fixture media must be inside fixture directory')
    original = next(track['tags'] for track in TRACKS if track['path'] == TRACK)
    tags = original if state == 'initial' else (
        {'title': 'Changed 東京 Title', 'artist': 'New Artist'} if state == 'changed' else {})
    path = root / TRACK
    temporary = path.with_suffix('.retag.flac')
    args = ['-i', str(path), '-map_metadata', '-1', '-c:a', 'copy']
    for key, value in tags.items():
        args += ['-metadata', f'{key}={value}']
    try:
        ffmpeg(*args, str(temporary))
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('state', choices=['initial', 'changed', 'cleared'])
    args = parser.parse_args()
    retag(args.fixture_dir, args.state)
