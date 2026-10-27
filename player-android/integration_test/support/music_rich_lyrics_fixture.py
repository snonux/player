#!/usr/bin/env python3
"""Add one word-timed multilingual fixture to an owned local music server."""

import argparse
import json
from pathlib import Path
import subprocess
import time

from fixture_server import Client


RICH_YAML = """version: '1.0'
metadata: {language: en}
lines:
 - start_ms: 1000
   end_ms: 5000
   voice: Lead
   translations: {fr: Bonjour monde, ja: こんにちは世界}
   words:
    - {text: 'Hello ', start_ms: 1000, end_ms: 2000}
    - {text: world, start_ms: 2000, end_ms: 5000}
 - {text: Harmony sings, start_ms: 1000, end_ms: 3000, voice: Harmony}
 - start_ms: 6000
   end_ms: 9000
   voice: Lead
   translations: {fr: Chemin maison}
   words:
    - {text: 'Road ', start_ms: 6000, end_ms: 7000}
    - {text: home, start_ms: 7000, end_ms: 9000}
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    args = parser.parse_args()
    fixture = Path(args.fixture_dir).resolve(strict=True)
    if not (fixture / '.player-music-fixture').is_file():
        raise ValueError('not an owned music fixture')
    state = json.loads((fixture / 'state.json').read_text())
    if not state['origin'].startswith('http://127.0.0.1:'):
        raise ValueError('only disposable localhost fixtures are supported')
    auth = json.loads((fixture / 'auth.json').read_text())
    folder = fixture / 'media' / 'Music' / 'A03b Rich'
    folder.mkdir(parents=True, exist_ok=True)
    source = fixture / 'media' / 'Music' / 'Aurora Lane - Night Transit (2021)' / '02 - Long Road Home.mp3'
    target = folder / 'Rich Voices.mp3'
    if not target.exists():
        subprocess.run(['ffmpeg', '-v', 'error', '-i', str(source), '-c', 'copy',
                        '-metadata', 'title=Rich Voices', '-metadata', 'album=A03b Rich',
                        str(target)], check=True)
    target.with_suffix('.yaml').write_text(RICH_YAML, encoding='utf-8')
    # The base album is already covered by the native folder journey; keep an
    # identical rich document there for a short, stable touch path.
    native_path = 'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3'
    (fixture / 'media' / native_path).with_suffix('.yaml').write_text(RICH_YAML, encoding='utf-8')
    client = Client(state['origin']).login(**auth['admin'])
    client.request('POST', '/api/v1/admin/rescan')
    for _ in range(60):
        result = client.request('GET', '/api/v1/media?set_id=1&folder=A03b%20Rich&limit=100')
        rows = result.get('media', result.get('items', [])) if isinstance(result, dict) else result
        item = next((row for row in rows if row['rel_path'].endswith('A03b Rich/Rich Voices.mp3')), None)
        if item:
            output = {'id': item['id'], 'title': item.get('music', {}).get('title', item['file_name']), 'rel_path': item['rel_path'],
                      'native_id': state['media_ids'][native_path], 'native_path': native_path,
                      'media_root': state['media_root'], 'origin': state['origin']}
            (fixture / 'rich-lyrics.json').write_text(json.dumps(output, indent=2))
            print(json.dumps(output))
            return
        time.sleep(1)
    raise TimeoutError('rich lyric track was not indexed')


if __name__ == '__main__':
    main()
