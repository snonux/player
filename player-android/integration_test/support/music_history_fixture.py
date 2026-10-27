"""Add two long, tagged songs to a disposable fixture for M37 playback checks."""

import argparse
import json
from pathlib import Path
import time
from urllib.parse import urlparse

from fixture_server import Client, write_private_json
from music_library import ffmpeg

ALBUM = 'M37 History Proof'
TRACKS = ((1, 'Replay Proof', 440), (2, 'Distinct Proof', 660))


def augment(directory, evidence):
    fixture = Path(directory).resolve(strict=True)
    if not (fixture / '.player-music-fixture').is_file():
        raise ValueError('M37 requires a marked disposable fixture')
    state = json.loads((fixture / 'state.json').read_text())
    if urlparse(state['origin']).hostname not in ('127.0.0.1', 'localhost'):
        raise ValueError('M37 requires a loopback server')
    root = Path(state['media_root']).resolve(strict=True)
    if root == fixture or not root.is_relative_to(fixture):
        raise ValueError('Media root must be confined to the fixture')
    targets = {number: root / 'Music' / ALBUM / f'{number:02d}.mp3'
               for number, _, _ in TRACKS}
    if any(target.exists() or not target.resolve().is_relative_to(root)
           for target in targets.values()):
        raise ValueError('M37 destination exists or escapes fixture')
    for number, title, tone in TRACKS:
        target = targets[number]
        target.parent.mkdir(parents=True, exist_ok=True)
        ffmpeg('-f', 'lavfi', '-i',
               f'sine=frequency={tone}:sample_rate=44100:duration=80',
               '-ac', '2', '-c:a', 'libmp3lame', '-b:a', '192k',
               '-threads', '1', '-metadata', f'title={title}',
               '-metadata', 'artist=History Artist',
               '-metadata', f'album={ALBUM}', str(target))
    auth = json.loads((fixture / 'auth.json').read_text())
    admin = Client(state['origin']).login(**auth['admin'])
    try:
        admin.request('POST', '/api/admin/rescan', {})
        deadline = time.monotonic() + 60
        while admin.request('GET', '/api/admin/scan-progress')['running']:
            if time.monotonic() > deadline:
                raise TimeoutError('M37 scan timed out')
            time.sleep(.25)
        rows = admin.request('GET', '/api/v1/media?limit=1000')
        ids = {}
        for number, title, _ in TRACKS:
            matches = [row for row in rows if row['rel_path'] == f'{ALBUM}/{number:02d}.mp3'
                       and row['music']['title'] == title]
            if len(matches) != 1:
                raise ValueError(f'M37 track {number} was not indexed once')
            ids[str(number)] = matches[0]['id']
    finally:
        admin.request('POST', '/api/v1/auth/logout', {})
    evidence_dir = Path(evidence)
    evidence_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    write_private_json(evidence_dir / 'seed.json', {'ids': ids, 'album': ALBUM})
    print(json.dumps({'ids': ids, 'album': ALBUM, 'evidence': str(evidence_dir)}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('evidence_dir')
    arguments = parser.parse_args()
    augment(arguments.fixture_dir, arguments.evidence_dir)
