"""F03 deterministic real files and guarded disposable-fixture augmentation.

Run only at setup, before acceptance: python3 music_filters.py FIXTURE_DIR.
Existing media identities are checked and retained. No database writes are used.
"""
import argparse
import json
import math
from pathlib import Path
import struct
import time
import wave
from urllib.parse import urlparse

from fixture_server import Client, write_private_json
from music_library import ffmpeg

MIB = 1048576
TAGS = ('F03 keep', 'F03 calm')
LONG_TAG = 'F03 long label ' + 'readable filter option ' * 5
SPECS = [
    ('Music/F03/f03-a.wav', 60, 1, TAGS, True),
    ('Music/F03/f03-b.wav', 120, 2, TAGS[:1], False),
    ('Music/F03/f03-c.wav', 60, 2, (*TAGS[1:], LONG_TAG), False),
    ('Music/F03/f03-zero.jpg', 0, 1, TAGS, False),
    ('F03 Other/F03/f03-other.wav', 60, 1, TAGS, True),
    ('Restricted/F03/f03-secret.wav', 60, 1, TAGS, True),
]


def make_wave(path, seconds, mib):
    """PCM with exact duration and valid RIFF JUNK chunk to exact binary size."""
    path.parent.mkdir(parents=True, exist_ok=True)
    rate = 8000
    # A repeated one-second 400 Hz signal makes frame count and size exact.
    signal = b''.join(struct.pack('<h', round(5000 * math.sin(2 * math.pi * 400 * i / rate)))
                      for i in range(rate))
    with wave.open(str(path), 'wb') as stream:
        stream.setparams((1, 2, rate, 0, 'NONE', 'not compressed'))
        for _ in range(seconds):
            stream.writeframesraw(signal)
    remaining = mib * MIB - path.stat().st_size
    if remaining < 8 or remaining % 2:
        raise ValueError('Invalid exact-size WAV target')
    with path.open('r+b') as stream:
        stream.seek(0, 2)
        stream.write(b'JUNK' + struct.pack('<I', remaining - 8))
        stream.write(bytes(remaining - 8))
        stream.seek(4)
        stream.write(struct.pack('<I', mib * MIB - 8))


def make_files(root):
    root = Path(root).resolve(strict=True)
    for relative, _, _, _, _ in SPECS:
        target = root / relative
        if target.exists() or not target.resolve().is_relative_to(root):
            raise ValueError('F03 destination exists or escapes fixture')
    for relative, seconds, mib, _, _ in SPECS:
        target = root / relative
        if seconds:
            make_wave(target, seconds, mib)
        else:
            target.parent.mkdir(parents=True, exist_ok=True)
            ffmpeg('-f', 'lavfi', '-i', 'color=c=blue:s=32x32', '-frames:v', '1', '-threads', '1', str(target))
            with target.open('ab') as stream:
                stream.write(bytes(mib * MIB - target.stat().st_size))


def augment(directory):
    fixture = Path(directory).resolve(strict=True)
    if not (fixture / '.player-music-fixture').is_file():
        raise ValueError('Not a marked disposable fixture')
    state = json.loads((fixture / 'state.json').read_text())
    if urlparse(state['origin']).hostname not in ('127.0.0.1', 'localhost'):
        raise ValueError('F03 only supports a loopback disposable server')
    root = Path(state['media_root']).resolve(strict=True)
    if not root.is_relative_to(fixture) or root == fixture:
        raise ValueError('Media root must be confined to the fixture')
    manifest = json.loads((fixture / 'manifest.json').read_text())
    old_ids = dict(state['media_ids'])
    make_files(root)
    auth = json.loads((fixture / 'auth.json').read_text())
    admin = Client(state['origin']).login(**auth['admin'])
    viewer = None
    try:
        admin.request('POST', '/api/admin/rescan', {})
        deadline = time.monotonic() + 60
        while admin.request('GET', '/api/admin/scan-progress')['running']:
            if time.monotonic() > deadline:
                raise ValueError('F03 scan timed out')
            time.sleep(.25)
        sets = admin.request('GET', '/api/v1/sets')
        names = {entry['id']: entry['name'] for entry in sets}
        rows = admin.request('GET', '/api/v1/media?limit=1000')
        ids = {f"{names[row['set_id']]}/{row['rel_path']}": row['id'] for row in rows}
        if any(ids.get(path) != identity for path, identity in old_ids.items()):
            raise ValueError('Existing media identity changed')
        expected = {spec[0] for spec in SPECS}
        if not expected.issubset(ids):
            raise ValueError('Missing indexed F03 media')
        user = next(u for u in admin.request('GET', '/api/admin/users')
                    if u['username'] == auth['user']['username'])
        other = next(entry['id'] for entry in sets if entry['name'] == 'F03 Other')
        admin.request('POST', '/api/admin/permissions',
                      {'set_id': other, 'user_id': user['id'], 'role': 'viewer'})
        viewer = Client(state['origin']).login(**auth['user'])
        indexed = {row['id']: row for row in rows}
        for relative, seconds, mib, tags, favorite in SPECS:
            identity = ids[relative]
            row = indexed[identity]
            if row.get('duration', 0) != seconds or row['file_size_bytes'] != mib * MIB:
                raise ValueError('Server indexed incorrect F03 bounds: ' + relative)
            for tag in tags:
                admin.request('POST', f'/api/v1/media/{identity}/tags', {'tag': tag})
            if favorite:
                for client in ([admin] if relative.startswith('Restricted/') else [admin, viewer]):
                    client.request('POST', f'/api/v1/media/{identity}/favorite', {})
        state['set_ids'].update({entry['name']: entry['id'] for entry in sets})
        state['media_ids'].update({path: ids[path] for path in expected})
        state['f03'] = {'paths': sorted(expected), 'tags': list(TAGS)}
        for relative, seconds, mib, _, _ in SPECS:
            if seconds:
                manifest['tracks'].append({'path': relative, 'seconds': seconds,
                    'duration_s': seconds, 'title': Path(relative).stem,
                    'set': relative.split('/')[0], 'file_name': Path(relative).name,
                    'codec': 'pcm_s16le', 'level_dbfs': -16.33, 'gapless': None, 'gap': None,
                    'tone_hz': [400], 'file_size_bytes': mib * MIB})
            else:
                manifest['images'].append(relative)
        manifest['media_count'] += len(SPECS)
        write_private_json(fixture / 'manifest.json', manifest)
        write_private_json(fixture / 'state.json', state)
    finally:
        if viewer:
            viewer.request('POST', '/api/v1/auth/logout', {})
        admin.request('POST', '/api/v1/auth/logout', {})
    print(json.dumps({'added': len(SPECS), 'preserved_media_ids': len(old_ids)}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    augment(parser.parse_args().fixture_dir)
