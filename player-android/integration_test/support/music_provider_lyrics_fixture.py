#!/usr/bin/env python3
"""Add a provider-only music track to an owned disposable music fixture."""

import argparse
import json
from pathlib import Path
import subprocess
import time

from fixture_server import Client


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir', type=Path)
    args = parser.parse_args()
    fixture = args.fixture_dir.resolve(strict=True)
    if not (fixture / '.player-music-fixture').is_file():
        raise ValueError('not an owned music fixture')
    state = json.loads((fixture / 'state.json').read_text())
    if not state['origin'].startswith('http://127.0.0.1:'):
        raise ValueError('only localhost fixtures are supported')
    auth = json.loads((fixture / 'auth.json').read_text())
    source = fixture / 'media' / 'Music' / 'Aurora Lane - Night Transit (2021)' / '01 - First Light.mp3'
    folder = fixture / 'media' / 'Music' / 'A03c Provider'
    folder.mkdir(parents=True, exist_ok=True)
    target = folder / 'Provider Sky.mp3'
    if not target.exists():
        subprocess.run(['ffmpeg', '-v', 'error', '-i', str(source), '-c', 'copy',
                        '-metadata', 'title=Provider Sky', '-metadata', 'artist=Fixture Artist',
                        '-metadata', 'album=A03c Provider', str(target)], check=True)
    client = Client(state['origin']).login(**auth['admin'])
    client.request('POST', '/api/v1/admin/rescan')
    for _ in range(60):
        result = client.request('GET', '/api/v1/media?set_id=' + str(state['set_ids']['Music']) +
                                '&folder=A03c%20Provider&limit=100')
        rows = result.get('media', result.get('items', [])) if isinstance(result, dict) else result
        item = next((row for row in rows if row['rel_path'].endswith('A03c Provider/Provider Sky.mp3')), None)
        if item:
            output = {'id': item['id'], 'title': 'Provider Sky',
                      'rel_path': item['rel_path'], 'origin': state['origin']}
            (fixture / 'provider-lyrics.json').write_text(json.dumps(output, indent=2))
            print(json.dumps(output))
            return
        time.sleep(1)
    raise TimeoutError('provider track was not indexed')


if __name__ == '__main__':
    main()
