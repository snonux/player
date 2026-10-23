"""M06 guarded additive source-information fixture. Run once before acceptance."""
import argparse
import json
from pathlib import Path
import time
from urllib.parse import urlparse

from fixture_server import Client, write_private_json
from music_library import ffmpeg

FOLDER = 'M06 Information'
TRACK = f'Music/{FOLDER}/technical.flac'
LOSSY = f'Music/{FOLDER}/lossy.mp3'
HIDDEN = f'Restricted/{FOLDER}/hidden.flac'
CREDIT = 'Engineer 東京 / ' + 'Long technical credit participant ' * 8
EXPECTED = {
    'Tempo': '123.5 BPM (beats/min)', 'Sample rate': '48 kHz (48000 Hz)',
    'Bit depth': '24 bits', 'Channels': '2 (stereo)',
    'Track ReplayGain': '0 dB', 'Album ReplayGain': '-6.25 dB',
    'Track peak': '0.89 (1 = full scale)', 'Album peak': '1 (1 = full scale)',
}
SPECS = [(TRACK, 'M06 Technical source', 733, 'flac'),
         (LOSSY, 'M06 Lossy source', 839, 'libmp3lame'),
         (HIDDEN, 'M06 Restricted source', 947, 'flac')]


def guarded_fixture(directory):
    fixture = Path(directory).resolve(strict=True)
    if not (fixture / '.player-music-fixture').is_file():
        raise ValueError('Not a marked disposable fixture')
    state = json.loads((fixture / 'state.json').read_text())
    if urlparse(state['origin']).hostname not in ('127.0.0.1', 'localhost'):
        raise ValueError('M06 requires a loopback disposable server')
    root = Path(state['media_root']).resolve(strict=True)
    if root == fixture or not root.is_relative_to(fixture):
        raise ValueError('Media root must be confined to the fixture')
    return fixture, state, root


def augment(directory):
    fixture, state, root = guarded_fixture(directory)
    manifest = json.loads((fixture / 'manifest.json').read_text())
    old_ids = dict(state['media_ids'])
    for relative, _, _, _ in SPECS:
        target = root / relative
        if target.exists() or not target.resolve().is_relative_to(root):
            raise ValueError('M06 destination exists or escapes fixture')
    for relative, title, tone, codec in SPECS:
        target = root / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        args = ['-f', 'lavfi', '-i', f'sine=frequency={tone}:sample_rate=48000:duration=180',
                '-ac', '2', '-c:a', codec]
        args += ['-sample_fmt', 's32'] if codec == 'flac' else ['-b:a', '192k']
        tags = {'title': title, 'artist': 'M06 Audio Lab', 'album': 'M06 Information',
                'genre': 'Technical verification', 'bpm': '123.5',
                'engineer': CREDIT, 'replaygain_track_gain': '0 dB',
                'replaygain_album_gain': '-6.25 dB', 'replaygain_track_peak': '0.89',
                'replaygain_album_peak': '1'}
        for key, value in tags.items():
            args += ['-metadata', f'{key}={value}']
        ffmpeg(*args, str(target))
    auth = json.loads((fixture / 'auth.json').read_text())
    admin = Client(state['origin']).login(**auth['admin'])
    try:
        admin.request('POST', '/api/admin/rescan', {})
        deadline = time.monotonic() + 60
        while admin.request('GET', '/api/admin/scan-progress')['running']:
            if time.monotonic() > deadline:
                raise ValueError('M06 scan timed out')
            time.sleep(.25)
        sets = admin.request('GET', '/api/v1/sets')
        names = {entry['id']: entry['name'] for entry in sets}
        rows = admin.request('GET', '/api/v1/media?limit=1000')
        indexed = {f"{names[row['set_id']]}/{row['rel_path']}": row for row in rows}
        if any(indexed.get(path, {}).get('id') != identity for path, identity in old_ids.items()):
            raise ValueError('Existing media identity changed')
        for relative, title, tone, codec in SPECS:
            row = indexed[relative]
            music = row['music']
            if music.get('sample_rate') != 48000 or music.get('channels') != 2 or music.get('bpm') != 123.5:
                raise ValueError('Incorrect technical fields: ' + relative)
            if (music.get('bit_depth', 0) != (24 if codec == 'flac' else 0)
                    or music.get('replaygain_track_gain_db') != 0):
                raise ValueError('Incorrect bit depth or zero gain: ' + relative)
            state['media_ids'][relative] = row['id']
            manifest['tracks'].append({'path':relative, 'title':title,
                'set':relative.split('/')[0], 'file_name':Path(relative).name,
                'codec':codec, 'tone_hz':[tone], 'duration_s':180, 'seconds':180,
                'level_dbfs':-18.06, 'gapless':None, 'gap':None})
        state['m06'] = {'paths':[entry[0] for entry in SPECS]}
        manifest['media_count'] += len(SPECS)
        write_private_json(fixture / 'manifest.json', manifest)
        write_private_json(fixture / 'state.json', state)
    finally:
        admin.request('POST', '/api/v1/auth/logout', {})
    print(json.dumps({'added':len(SPECS), 'preserved_media_ids':len(old_ids)}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    augment(parser.parse_args().fixture_dir)
