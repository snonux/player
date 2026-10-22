"""M04 synthetic discs; guarded augmentation preserves an existing fixture."""
import argparse
import json
import os
from pathlib import Path
import time

from fixture_server import Client, write_private_json
from music_library import _make_track, ffmpeg, set_flac_artists

ALBUM = 'M04 Two Disc Album'
COMPILATION = 'M04 Compilation'
SUBTITLE = 'Encore 東京 — ' + 'Long disc subtitle ' * 5
SPECS = []
for compilation in (False, True):
    title = COMPILATION if compilation else ALBUM
    folder = 'Compilation' if compilation else 'Album'
    for index, (disc, track) in enumerate([(1, 1), (1, 2), (2, 1)]):
        tags = {'album': title, 'title': f'M04 {folder} {disc}-{track}',
                'artists': ['M04 A / B', 'M04 東京'], 'artist': 'M04 A / B',
                'track': str(track), 'disc': f'{disc}/2',
                'discsubtitle': 'Original' if disc == 1 else SUBTITLE}
        if compilation:
            tags['compilation'] = '1'
            if (disc, track) == (1, 2):
                tags.update(artist='M04 Guest', artists=['M04 Guest'])
        else:
            tags['album_artist'] = 'M04 Ensemble'
        SPECS.append(dict(path=f'Music/M04/{folder}/Disc {disc}/{track}.flac',
                          codec='flac-16-44', seconds=18, tone_hz=[730 + 40 * index + (140 if compilation else 0)],
                          level_dbfs=-12, tags=tags))
# Hidden first/only art and subtitle must not supply the viewer's disc fields.
SPECS.append(dict(path='Restricted/M04/hidden.flac', codec='flac-16-44', seconds=12,
                  tone_hz=[240], level_dbfs=-12, tags={'album': ALBUM,
                  'album_artist': 'M04 Ensemble', 'title': 'M04 Hidden',
                  'disc': '3', 'track': '1', 'discsubtitle': 'M04 Hidden subtitle'}))
IMAGES = [f'Music/M04/{folder}/Disc {disc}/cover.jpg'
          for folder in ('Album', 'Compilation') for disc in (1, 2)]
IMAGES += ['Restricted/M04/cover.jpg', 'Music/M04/Album/cover.jpg', 'Music/M04/Compilation/cover.jpg']
SPECS.append(dict(path='Music/M04/Missing/no-art.flac',codec='flac-16-44',seconds=12,tone_hz=[990],level_dbfs=-12,tags={'title':'M04 No artwork','album':'M04 Missing Art','artist':'M04 Solo'}))


def make_tracks(root):
    root = Path(root)
    for namespace in ('Music/M04', 'Restricted/M04'):
        path = root / namespace
        if path.exists() or path.is_symlink() or not path.resolve().is_relative_to(root.resolve()):
            raise ValueError('M04 namespace already exists or escapes root: ' + namespace)
    tracks = [_make_track(root, spec) for spec in SPECS]
    for relative in IMAGES:
        target = root / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        ffmpeg('-f', 'lavfi', '-i', f'color=c={"red" if "/Disc 2/" in relative else "blue"}:s=128x128',
               '-frames:v', '1', '-threads', '1', str(target))
    # Existing policy must choose the closer disc image over ancestor and embedded art.
    for folder in ('Album', 'Compilation'):
        source = root / f'Music/M04/{folder}/Disc 2/1.flac'
        temporary = source.with_suffix('.embedded.flac')
        ffmpeg('-i', str(source), '-i', str(root / f'Music/M04/{folder}/cover.jpg'),
               '-map', '0:a', '-map', '1:v', '-c', 'copy', '-disposition:v', 'attached_pic',
               '-metadata:s:v', 'title=Album cover', '-metadata:s:v', 'comment=Cover (front)', str(temporary))
        os.replace(temporary, source)
        set_flac_artists(source, ['M04 A / B', 'M04 東京'])
    return tracks


def augment(directory):
    fixture = Path(directory).resolve(strict=True)
    if not (fixture / '.player-music-fixture').is_file():
        raise ValueError('Not a disposable music fixture')
    state_path = fixture / 'state.json'
    state = json.loads(state_path.read_text())
    root = Path(state['media_root']).resolve(strict=True)
    if not root.is_relative_to(fixture) or root == fixture:
        raise ValueError('Media root must be confined within fixture')
    manifest_path = fixture / 'manifest.json'
    manifest = json.loads(manifest_path.read_text())
    known = {entry['path'] for entry in manifest['tracks']} | set(manifest['images'])
    if known.intersection({spec['path'] for spec in SPECS} | set(IMAGES)):
        raise ValueError('M04 manifest entries already exist')
    old_ids = dict(state['media_ids'])
    tracks = make_tracks(root)
    manifest['tracks'] += tracks
    manifest['images'] += IMAGES
    manifest['media_count'] += len(tracks) + len(IMAGES)
    write_private_json(manifest_path, manifest)
    auth = json.loads((fixture / 'auth.json').read_text())
    admin = Client(state['origin']).login(**auth['admin'])
    try:
        admin.request('POST', '/api/admin/rescan', {})
        deadline = time.monotonic() + 60
        while admin.request('GET', '/api/admin/scan-progress')['running']:
            if time.monotonic() > deadline:
                raise ValueError('M04 scan did not finish')
            time.sleep(.25)
        names = {value: key for key, value in state['set_ids'].items()}
        rows = admin.request('GET', '/api/v1/media?limit=1000')
        ids = {f"{names[row['set_id']]}/{row['rel_path']}": row['id'] for row in rows}
        if any(ids.get(path) != identity for path, identity in old_ids.items()):
            raise ValueError('Existing media IDs changed; refusing state replacement')
        expected = {spec['path'] for spec in SPECS} | set(IMAGES)
        if not expected.issubset(ids):
            raise ValueError('Missing indexed M04 files')
        state['media_ids'].update({path: ids[path] for path in expected})
        write_private_json(state_path, state)
    finally:
        admin.request('POST', '/api/v1/auth/logout', {})
    print(json.dumps({'added_tracks': len(tracks), 'added_images': len(IMAGES),
                      'preserved_media_ids': len(old_ids)}))


RETAG_PATH = 'Music/M04/Album/Disc 2/1.flac'


def retag(directory, state):
    fixture = Path(directory).resolve(strict=True)
    if not (fixture / '.player-music-fixture').is_file():
        raise ValueError('Not a disposable fixture')
    config = json.loads((fixture / 'state.json').read_text())
    root = Path(config['media_root']).resolve(strict=True)
    source = root / RETAG_PATH
    if root == fixture or not root.is_relative_to(fixture) or not source.resolve().is_relative_to(root):
        raise ValueError('Source escapes fixture')
    backup = fixture / '.m04-probe-backup.flac'
    if state == 'broken':
        if backup.exists():
            raise ValueError('M04 probe backup already exists')
        source.rename(backup)
        source.write_bytes(b'fLaC' + bytes(100))
        return
    if backup.exists():
        os.replace(backup, source)
    spec = next(spec for spec in SPECS if spec['path'] == RETAG_PATH)
    tags = dict(spec['tags'])
    if state == 'changed':
        tags.update(discsubtitle='Changed disc 東京', compilation='1')
    elif state == 'cleared':
        tags.pop('discsubtitle', None)
        tags.pop('compilation', None)
    temporary = source.with_suffix('.retag.flac')
    args = ['-i', str(source), '-map', '0', '-map_metadata', '-1', '-c', 'copy']
    for key, value in tags.items():
        if key != 'artists':
            args += ['-metadata', f'{key}={value}']
    try:
        ffmpeg(*args, str(temporary))
        set_flac_artists(temporary, tags['artists'])
        os.replace(temporary, source)
    finally:
        temporary.unlink(missing_ok=True)



def set_art(directory, state):
    fixture = Path(directory).resolve(strict=True)
    if not (fixture / '.player-music-fixture').is_file():
        raise ValueError('Not a disposable fixture')
    config = json.loads((fixture / 'state.json').read_text())
    root = Path(config['media_root']).resolve(strict=True)
    if not root.is_relative_to(fixture) or root == fixture:
        raise ValueError('Media root escapes fixture')
    target = root / 'Music/M04/Album/Disc 2/cover.jpg'
    if not target.resolve().is_relative_to(root):
        raise ValueError('Artwork escapes media root')
    backup = fixture / '.m04-disc-art-backup.jpg'
    if state == 'broken':
        if backup.exists():
            raise ValueError('M04 artwork backup already exists')
        backup.write_bytes(target.read_bytes())
        target.write_bytes(b'Not an image: M04 controlled broken cover')
    else:
        if not backup.is_file():
            raise ValueError('M04 artwork backup missing')
        target.write_bytes(backup.read_bytes())
        backup.unlink()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['augment', 'art', 'retag'])
    parser.add_argument('fixture_dir')
    parser.add_argument('state', nargs='?', choices=['broken', 'initial', 'changed', 'cleared'])
    args = parser.parse_args()
    if args.command == 'augment':
        augment(args.fixture_dir)
    elif args.command == 'retag' and args.state:
        retag(args.fixture_dir, args.state)
    elif args.state in ('broken', 'initial'):
        set_art(args.fixture_dir, args.state)
    else:
        parser.error('art requires broken or initial')
