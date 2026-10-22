"""M05 optional synthetic release tracks and guarded retagging, no real media."""
import argparse
import json
import os
from pathlib import Path

from music_library import _make_track, ffmpeg, set_flac_artists

TRACK = 'Music/Release Metadata/release.flac'
COMMENT = '<b>Preserved text</b>\n第二行 · ' + 'Long release comment 東京 ' * 8
TAGS = {
    'title': 'Release Metadata 東京', 'artist': 'Release Ensemble', 'album': 'Release Metadata',
    'date': '2024', 'release_date': '2025-04', 'originaldate': '1979',
    'recordingdate': '1978-11-03', 'albumversion': 'Expanded / Live',
    'releasetype': 'album', 'label': 'Label A; B', 'catalognumber': 'CAT/01',
    'media': 'CD', 'mood': 'Reflective', 'grouping': 'Suite / One',
    'comment': COMMENT, 'producer': 'Producer A / B', 'credit:Tape Op': 'Op A; B',
}
REPEATED = {'LABEL': ['Label A; B', 'Label 東京'], 'RELEASETYPE': ['album', 'live'],
            'CATALOGNUMBER': ['CAT/01', 'CAT,02'], 'MEDIA': ['CD', 'Digital'],
            'MOOD': ['Reflective', 'Bright'], 'PRODUCER': ['Producer A / B', 'Producer C'],
            'CREDIT:Tape Op': ['Op A; B', 'Op 東京 / C']}
EXPECTED = {'Release date': '2025-04', 'Original date': '1979', 'Recording date': '1978-11-03',
            'Version': 'Expanded / Live', 'Release type': 'album\nlive',
            'Label': 'Label A; B\nLabel 東京', 'Catalog number': 'CAT/01\nCAT,02',
            'Media': 'CD\nDigital', 'Mood': 'Reflective\nBright', 'Grouping': 'Suite / One',
            'Comment': COMMENT, 'Producer': 'Producer A / B\nProducer C',
            'tape op': 'Op A; B\nOp 東京 / C'}


def repeat(path):
    for key, values in REPEATED.items():
        set_flac_artists(path, values, key=key)


def make_tracks(root):
    entries = []
    for path, codec in [(TRACK, 'flac-16-44'), ('Music/Release Metadata/aliases.mp3', 'mp3-128'),
                        ('Music/Release Metadata/aliases.opus', 'opus-96'),
                        ('Music/Release Metadata/aliases.m4a', 'aac-128')]:
        tags = dict(TAGS)
        if codec != 'flac-16-44':
            tags.update(title='Release Aliases ' + Path(path).suffix[1:])
        spec = dict(path=path, codec=codec, seconds=45, tone_hz=[613.0], level_dbfs=-12, tags=tags)
        entries.append(_make_track(Path(root), spec))
        if codec == 'aac-128':
            source = Path(root) / path
            temporary = source.with_name('metadata.m4a')
            args = ['-i', str(source), '-map_metadata', '-1', '-c:a', 'copy', '-movflags', '+use_metadata_tags']
            for key, value in tags.items():
                args += ['-metadata', f'{key}={value}']
            ffmpeg(*args, str(temporary))
            os.replace(temporary, source)
        if codec == 'flac-16-44':
            repeat(Path(root) / path)
    spec = dict(path='Restricted/Release Metadata/hidden.flac', codec='flac-16-44', seconds=12,
                tone_hz=[241.0], level_dbfs=-12, tags=dict(TAGS, title='Hidden Release Metadata'))
    entries.append(_make_track(Path(root), spec))
    return entries


def retag(directory, state):
    fixture = Path(directory).resolve()
    if not (fixture / '.player-music-fixture').is_file():
        raise ValueError('Not a disposable music fixture')
    config = json.loads((fixture / 'state.json').read_text())
    root = Path(config['media_root']).resolve()
    if not root.is_relative_to(fixture):
        raise ValueError('Fixture media must be inside fixture directory')
    source = root / TRACK
    if state == 'broken':
        backup = source.with_suffix('.m05-backup')
        if backup.exists():
            raise ValueError('Backup already exists')
        source.rename(backup)
        source.write_bytes(b'fLaC' + bytes(100))
        return
    backup = source.with_suffix('.m05-backup')
    if backup.exists():
        os.replace(backup, source)
    tags = TAGS if state == 'initial' else {'title': TAGS['title']}
    if state == 'changed':
        tags = dict(tags, release_date='2026-06', label='Changed Label', **{'credit:Tape Op': 'Changed / Op'})
    temporary = source.with_suffix('.retag.flac')
    args = ['-i', str(source), '-map_metadata', '-1', '-c:a', 'copy']
    for key, value in tags.items():
        args += ['-metadata', f'{key}={value}']
    try:
        ffmpeg(*args, str(temporary))
        os.replace(temporary, source)
        if state == 'initial':
            repeat(source)
    finally:
        temporary.unlink(missing_ok=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    parser.add_argument('state', choices=['initial', 'changed', 'cleared', 'broken'])
    args = parser.parse_args()
    retag(args.fixture_dir, args.state)
