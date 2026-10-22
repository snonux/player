"""Generate the synthetic tagged music library used by the music E2E fixture.

Everything is made by ffmpeg from sine tones, so the library is public,
small (about 5 MB) and reproducible. Each track's tone frequency and level
are known, which lets the audio analysis in audio_trace.py prove *which*
track is audible and at what level, instead of only "something played".

The library is described once, in TRACKS. generate() writes the files and
returns a manifest (also saved as manifest.json) so runners assert against
the same expected titles, artists, durations, tones and gains that produced
the media. Tests should read the manifest instead of repeating literals.

Coverage, by the music tasks that rely on it (docs/music-candidate-map.md):
multiple albums/artists, two discs and a compilation (M02-M04), release
details such as label and original date (M05), sample rate/bit depth/bitrate
variety (M06, M50), Unicode and missing tags (M01), timed LRC, plain-text and
embedded lyrics, ReplayGain tags at different real levels (M24), a
continuous tone split sample-exactly into tracks (gapless, M21), single
files with known 5/20/50 ms gaps to calibrate gap detection, a three-tone
probe for equalizer band checks (M25), an M3U playlist (M31), cover art, and
a second set the regular test account cannot see (permissions).
"""

import json
from pathlib import Path
import struct
import subprocess

SAMPLE_RATE = 44100
TRACK_SECONDS = 12
GAPLESS_SECONDS = 6
GAPLESS_TRACKS = 3

NIGHT_TRANSIT = {
    'album': 'Night Transit', 'album_artist': 'Aurora Lane', 'date': '2021',
    'originaldate': '2020-11-01', 'label': 'Fixture Records',
    'catalognumber': 'FXR-001', 'genre': 'Synthwave',
}
COMPILATION = {
    'album': 'Synth Compilation', 'album_artist': 'Various Artists',
    'compilation': '1', 'date': '2019', 'genre': 'Synthwave',
}
UNICODE_ALBUM = {
    'album': '東京 Nights (夜)', 'album_artist': 'Ünïcødé Artïst', 'date': '2022',
    'genre': 'Électronique',
}
GAPLESS_ALBUM = {
    'album': 'Gapless Suite', 'album_artist': 'Continuum', 'date': '2023',
    'genre': 'Test Tones',
}
GAIN_ALBUM = {'album': 'Gain Check', 'album_artist': 'Level Lab', 'genre': 'Test Tones'}

# Timed lyrics for "Long Road Home": lines at 0, 4 and 8 seconds so lyric
# seeking (F01) and highlighting can be checked against known positions.
TIMED_LYRICS = ('[ar:Aurora Lane feat. Kei]\n[ti:Long Road Home]\n'
                '[00:00.00]Headlights on the long road home\n'
                '[00:04.00]Every mile a quiet song\n'
                '[00:08.00]Harbour lights to guide me on\n')
PLAIN_LYRICS = 'Café au lait, ñoño día\nUnsynchronised lyric line two\n'
EMBEDDED_LYRICS = 'Afterglow over the water\nEmbedded lyric line two'

# One dict per audio file. Keys:
#   path       relative to MEDIA_ROOT; the first segment is the Player set
#   codec      ffmpeg encoder arguments key in CODECS
#   tone_hz    sine frequency(ies); level_dbfs is the peak level of the
#              sum: one tone peaks there, the three EQ probe tones each sit
#              20*log10(3) = 9.5 dB lower so that their sum peaks there
#   tags       written tags (None = strip every tag); 'artists' (a list)
#              becomes repeated ARTIST Vorbis comments in a FLAC file
#   lyrics     None | 'timed-sidecar' | 'plain-sidecar' | 'embedded'
#   replaygain dict of REPLAYGAIN_* values written as tags
TRACKS = [
    dict(path='Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3',
         codec='mp3-192', tone_hz=[440.0], level_dbfs=-12,
         tags=dict(NIGHT_TRANSIT, title='First Light', artist='Aurora Lane',
                   track='1/3', disc='1/1'),
         replaygain={'REPLAYGAIN_TRACK_GAIN': '-3.00 dB', 'REPLAYGAIN_ALBUM_GAIN': '-4.50 dB'}),
    dict(path='Music/Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3',
         codec='mp3-320', tone_hz=[523.25], level_dbfs=-12, lyrics='timed-sidecar',
         tags=dict(NIGHT_TRANSIT, title='Long Road Home', artist='Aurora Lane feat. Kei',
                   track='2/3', disc='1/1'),
         replaygain={'REPLAYGAIN_TRACK_GAIN': '-3.20 dB', 'REPLAYGAIN_ALBUM_GAIN': '-4.50 dB'}),
    dict(path='Music/Aurora Lane - Night Transit (2021)/03 - Harbour.flac',
         codec='flac-24-96', tone_hz=[659.25], level_dbfs=-12,
         tags=dict(NIGHT_TRANSIT, title='Harbour', artist='Aurora Lane',
                   track='3/3', disc='1/1', genre='Ambient')),
    dict(path='Music/Various Artists - Synth Compilation/Disc 1/01 - Neon Rain.m4a',
         codec='aac-128', tone_hz=[392.0], level_dbfs=-12,
         tags=dict(COMPILATION, title='Neon Rain', artist='Neon Coast', composer='Composer; 東京 / A', performer='Session Ensemble',
                   track='1/2', disc='1/2')),
    dict(path='Music/Various Artists - Synth Compilation/Disc 1/02 - Glass City.opus',
         codec='opus-96', tone_hz=[587.33], level_dbfs=-12,
         tags=dict(COMPILATION, title='Glass City', artist='Glass Avenue', composer='Composer; 東京 / A', performer='Glass Avenue',
                   track='2/2', disc='1/2')),
    dict(path='Music/Various Artists - Synth Compilation/Disc 2/01 - Afterglow.ogg',
         codec='vorbis-q4', tone_hz=[698.46], level_dbfs=-12, lyrics='embedded',
         tags=dict(COMPILATION, title='Afterglow', artist='Dusk Choir',
                   track='1/1', disc='2/2', discsubtitle='Encore')),
    dict(path='Music/Ünïcødé Artïst - 東京 Nights/01 - Café Ñoño.mp3',
         codec='mp3-128', tone_hz=[349.23], level_dbfs=-12, lyrics='plain-sidecar',
         tags=dict(UNICODE_ALBUM, title='Café Ñoño', artist='Ünïcødé Artïst',
                   track='1/2', disc='1/1')),
    dict(path='Music/Ünïcødé Artïst - 東京 Nights/02 - Партия для двоих.flac',
         codec='flac-16-44', tone_hz=[311.13], level_dbfs=-12,
         tags=dict(UNICODE_ALBUM, title='Партия для двоих',
                   artists=['Ünïcødé Artïst', 'Mika Sørensen'], genres=['Électronique', 'Ambient'], composers=['Composer; 東京 / A', 'Mika Sørensen'], performers=['Session Ensemble', 'Mika Sørensen'], track='2/2', disc='1/1')),
    dict(path='Music/Partial Tags/Lonely Single.mp3',
         codec='mp3-128', tone_hz=[466.16], level_dbfs=-12,
         tags={'title': 'Lonely Single', 'artist': 'Solo Act'}),
    dict(path='Music/Untagged/track_0042.mp3',
         codec='mp3-128', tone_hz=[277.18], level_dbfs=-12, tags=None),
    dict(path='Music/Tag Verification/source_001.flac', codec='flac-16-44',
         tone_hz=[415.30], level_dbfs=-12,
         tags={'title': 'A Different Music Title', 'artist': 'A & B; C/D',
               'album_artist': 'Album Ensemble', 'album': 'Metadata Album', 'composer': 'Metadata Composer', 'performer': 'Metadata Performer',
               'track': '4/9', 'disc': '2/3', 'date': '2024-02-29', 'genre': 'Jazz'}),
    dict(path='Music/Tag Verification/blank_title.mp3', codec='mp3-128',
         tone_hz=[293.66], level_dbfs=-12,
         tags={'title': '   ', 'artist': '  ', 'track': '-7', 'disc': 'invalid',
               'album': 'Partial Metadata', 'date': 'unknown'}),
    dict(path='Music/Tag Verification/long_name.flac', codec='flac-16-44',
         tone_hz=[329.63], level_dbfs=-12,
         tags={'title': '東京 Café Ñoño — ' + 'Long music title ' * 10,
               'artist': 'Long Artist ' * 10, 'album': 'Long Album ' * 10}),
    dict(path='Music/Gain Check/Loud.flac',
         codec='flac-16-44', tone_hz=[1000.0], level_dbfs=-3,
         tags=dict(GAIN_ALBUM, title='Loud', artist='Level Lab', track='1/3'),
         replaygain={'REPLAYGAIN_TRACK_GAIN': '-9.00 dB', 'REPLAYGAIN_TRACK_PEAK': '0.707946',
                     'REPLAYGAIN_ALBUM_GAIN': '-0.50 dB', 'REPLAYGAIN_ALBUM_PEAK': '0.707946'}),
    dict(path='Music/Gain Check/Quiet.flac',
         codec='flac-16-44', tone_hz=[1000.0], level_dbfs=-21,
         tags=dict(GAIN_ALBUM, title='Quiet', artist='Level Lab', track='2/3'),
         replaygain={'REPLAYGAIN_TRACK_GAIN': '+9.00 dB', 'REPLAYGAIN_TRACK_PEAK': '0.089125',
                     'REPLAYGAIN_ALBUM_GAIN': '-0.50 dB', 'REPLAYGAIN_ALBUM_PEAK': '0.707946'}),
    dict(path='Music/Gain Check/No Gain Tags.mp3',
         codec='mp3-320', tone_hz=[1000.0], level_dbfs=-12,
         tags=dict(GAIN_ALBUM, title='No Gain Tags', artist='Level Lab', track='3/3')),
    dict(path='Music/EQ Probe/Three Tones.flac',
         codec='flac-16-44', tone_hz=[100.0, 1000.0, 8000.0], level_dbfs=-18,
         tags={'title': 'Three Tones', 'artist': 'Level Lab', 'album': 'EQ Probe',
               'genre': 'Test Tones'}),
    # Shares the public album identity, but must never affect viewer totals,
    # ordered songs or artwork. M02 verifies aggregation after ACL filtering.
    dict(path='Restricted/Hidden contribution/00 - Hidden First Light.mp3',
         codec='mp3-128', tone_hz=[233.08], level_dbfs=-12,
         tags=dict(NIGHT_TRANSIT, title='Hidden Album Contribution',
                   artist='Aurora Lane', track='4/4', disc='1/1')),
    dict(path='Restricted/Admin Only - Hidden Album/01 - Hidden Track.mp3',
         codec='mp3-128', tone_hz=[220.0], level_dbfs=-12,
         tags={'title': 'Hidden Track', 'artist': 'Secret Artist', 'album': 'Hidden Album',
               'album_artist': 'Secret Artist', 'composer': 'Hidden Composer', 'performer': 'Hidden Performer', 'track': '1/1'}),
]

# M03 needs actual scrolled Artists and discography views for Back-position proof.
# Existing artists have only one or two releases, so use a dedicated synthetic
# artist without changing the identities/order of any baseline album.
POSITION_ARTIST = 'Zzz Catalog Artist — ' + 'Long artist name ' * 6
TRACKS += [dict(path=f'Music/Catalog Position/Release {index:02}/01 - Position.mp3',
               codec='mp3-128', tone_hz=[610.0 + index], level_dbfs=-12,
               tags={'title': f'Position {index:02}', 'artist': POSITION_ARTIST,
                     'album_artist': POSITION_ARTIST,
                     'album': f'Catalog Release {index:02}', 'track': '1/1'})
           for index in range(1, 15)]

# Gapless: one continuous 880 Hz tone cut into GAPLESS_TRACKS pieces at exact
# sample boundaries, in a lossless (FLAC) and a lossy (MP3, LAME gapless
# header) copy. Any silence between the pieces during playback is a playback
# gap, not part of the media.
GAPLESS_TONE_HZ = 880.0
GAPLESS_TITLES = ['Movement I', 'Movement II', 'Movement III']
GAPLESS_FOLDERS = {'flac-16-44': 'Music/Gapless Suite (FLAC)',
                   'mp3-320': 'Music/Gapless Suite (MP3)'}

# Gap calibration: single files holding the 880 Hz tone with a stretch of
# exact digital silence of known length at GAP_AT_S. They calibrate the gap
# detection of every capture path (browser tap, emulator output) without
# depending on automatic track changes, which Android cannot do yet.
GAP_CALIBRATION_MS = [5, 20, 50]
GAP_AT_S = 3.0
GAP_TRACK_SECONDS = 6
GAP_FOLDER = 'Music/Gap Calibration'

COVER = 'Music/Aurora Lane - Night Transit (2021)/cover.jpg'
PLAYLIST = 'Music/Road Trip.m3u'
# The second playlist entry deliberately does not exist (unresolved path).
PLAYLIST_ENTRIES = ['Aurora Lane - Night Transit (2021)/02 - Long Road Home.mp3',
                    'Missing Album/99 - Not Here.mp3',
                    'Various Artists - Synth Compilation/Disc 2/01 - Afterglow.ogg']

CODECS = {
    'mp3-128': ['-c:a', 'libmp3lame', '-b:a', '128k'],
    'mp3-192': ['-c:a', 'libmp3lame', '-b:a', '192k'],
    'mp3-320': ['-c:a', 'libmp3lame', '-b:a', '320k'],
    'flac-16-44': ['-c:a', 'flac', '-sample_fmt', 's16'],
    'flac-24-96': ['-c:a', 'flac', '-sample_fmt', 's32', '-bits_per_raw_sample', '24',
                   '-ar', '96000'],
    # The default mov muxer writes iTunes atoms (©nam, ©ART, aART, trkn,
    # disk, cpil). '+use_metadata_tags' would write QuickTime mdta keys
    # instead, which tag readers expecting iTunes atoms ignore.
    'aac-128': ['-c:a', 'aac', '-b:a', '128k'],
    'opus-96': ['-c:a', 'libopus', '-b:a', '96k'],
    'vorbis-q4': ['-c:a', 'libvorbis', '-q:a', '4'],
}

# ffmpeg writes ID3v2.3 for MP3 so that both clients' tag readers agree.
ID3_ARGS = ['-id3v2_version', '3', '-write_id3v1', '0']
# No container tags at all: ffmpeg would otherwise add an encoder tag.
STRIP_ARGS = ['-map_metadata', '-1', '-fflags', '+bitexact', '-flags:a', '+bitexact']


def generate(media_root, *, session_artwork=False, shuffle_playback=False, release_metadata=False, multi_disc=False):
    """Write the library below media_root and return the manifest dict."""
    media_root = Path(media_root)
    session_tracks = {
        'Music/Aurora Lane - Night Transit (2021)/01 - First Light.mp3',
        'Music/Tag Verification/source_001.flac',
        'Music/Untagged/track_0042.mp3',
    }
    specs = [dict(spec, seconds=45)
             if ((session_artwork and spec['path'] in session_tracks) or
                 (shuffle_playback and spec['path'].startswith('Music/Aurora Lane - Night Transit (2021)/')))
             else spec for spec in TRACKS]
    tracks = [_make_track(media_root, spec) for spec in specs]
    if release_metadata:
        from music_release import make_tracks
        tracks += make_tracks(media_root)
    tracks += _make_gapless(media_root)
    tracks += _make_gap_calibration(media_root)
    _make_cover(media_root / COVER)
    images = [COVER]
    if multi_disc:
        from music_discs import make_tracks, IMAGES
        tracks += make_tracks(media_root)
        images += IMAGES
    if session_artwork:
        second = 'Music/Tag Verification/cover.jpg'
        ffmpeg('-f', 'lavfi', '-i', 'color=c=0xd25a20:s=600x600', '-frames:v', '1',
               '-threads', '1', str(media_root / second))
        images.append(second)
    _write_playlist(media_root / PLAYLIST)
    manifest = {
        'sets': sorted({t['path'].split('/')[0] for t in tracks}),
        'tracks': tracks,
        'images': images,
        'playlists': [{'path': PLAYLIST, 'entries': PLAYLIST_ENTRIES,
                       'unresolved': [PLAYLIST_ENTRIES[1]]}],
        # Every file the server lists as media (audio plus the cover image);
        # the M3U and lyric sidecars are not media items.
        'media_count': len(tracks) + len(images),
    }
    (media_root.parent / 'manifest.json').write_text(
        json.dumps(manifest, indent=1, ensure_ascii=False))
    return manifest


def expected_title(track):
    """Title a music-aware client should show: the tag, else the file stem."""
    tags = track.get('tags') or {}
    return (tags.get('title') or '').strip() or Path(track['path']).stem


def ffmpeg(*arguments):
    subprocess.run(['ffmpeg', '-nostdin', '-hide_banner', '-loglevel', 'error', '-y',
                    *arguments], check=True)


def tone_filter(frequencies, level_dbfs, seconds, rate=SAMPLE_RATE):
    """lavfi expression: equal-level sines whose summed peak is level_dbfs."""
    amplitude = 10 ** (level_dbfs / 20) / len(frequencies)
    terms = '+'.join(f'{amplitude:.6f}*sin(2*PI*{f}*t)' for f in frequencies)
    return f'aevalsrc={terms}:s={rate}:d={seconds}'


def _make_track(media_root, spec, source=None):
    target = media_root / spec['path']
    target.parent.mkdir(parents=True, exist_ok=True)
    inputs = ['-i', str(source)] if source else [
        '-f', 'lavfi', '-i', tone_filter(spec['tone_hz'], spec['level_dbfs'], spec.get('seconds', TRACK_SECONDS))]
    tags = spec.get('tags')
    ffmpeg(*inputs, *CODECS[spec['codec']], *_tag_args(spec, target), str(target))
    if tags and 'artists' in tags:
        set_flac_artists(target, tags['artists'])
    if tags and 'genres' in tags:
        set_flac_artists(target, tags['genres'], key='GENRE')
    for field, key in [('composers', 'COMPOSER'), ('performers', 'PERFORMER')]:
        if tags and field in tags:
            set_flac_artists(target, tags[field], key=key)
    _write_lyrics(target, spec.get('lyrics'))
    return _manifest_entry(spec, source)


def _tag_args(spec, target):
    tags = spec.get('tags')
    if tags is None:
        return STRIP_ARGS + (['-id3v2_version', '0', '-write_id3v1', '0']
                             if target.suffix == '.mp3' else [])
    values = {k: v for k, v in tags.items() if k not in ('artists', 'genres', 'composers', 'performers')}
    values.update(spec.get('replaygain') or {})
    if spec.get('lyrics') == 'embedded':
        values['LYRICS'] = EMBEDDED_LYRICS
    arguments = ['-map_metadata', '-1']
    for key, value in values.items():
        arguments += ['-metadata', f'{key}={value}']
    return arguments + (ID3_ARGS if target.suffix == '.mp3' else [])


def _write_lyrics(target, kind):
    if kind == 'timed-sidecar':
        target.with_suffix('.lrc').write_text(TIMED_LYRICS, encoding='utf-8')
    elif kind == 'plain-sidecar':
        target.with_suffix('.txt').write_text(PLAIN_LYRICS, encoding='utf-8')


def _manifest_entry(spec, source):
    seconds = spec.get('seconds', TRACK_SECONDS)
    entry = {key: spec.get(key) for key in ('path', 'codec', 'tone_hz', 'level_dbfs',
                                            'lyrics', 'replaygain', 'tags')}
    entry.update(set=spec['path'].split('/')[0], file_name=Path(spec['path']).name,
                 title=expected_title(spec), duration_s=seconds,
                 sample_rate=96000 if spec['codec'] == 'flac-24-96' else (
                     48000 if spec['codec'] == 'opus-96' else SAMPLE_RATE),
                 gapless=spec.get('gapless'), gap=spec.get('gap'))
    return entry


def _make_gap_calibration(media_root):
    """FLAC files with one known gap of exact silence each (lossless keeps it exact)."""
    entries = []
    amplitude = 10 ** (-12 / 20)
    for gap_ms in GAP_CALIBRATION_MS:
        end = GAP_AT_S + gap_ms / 1000
        source = (f'aevalsrc=if(between(t\\,{GAP_AT_S}\\,{end:.4f})\\,0\\,'
                  f'{amplitude:.6f}*sin(2*PI*{GAPLESS_TONE_HZ}*t)):s={SAMPLE_RATE}:d={GAP_TRACK_SECONDS}')
        spec = dict(path=f'{GAP_FOLDER}/Gap {gap_ms:02d} ms.flac', codec='flac-16-44',
                    tone_hz=[GAPLESS_TONE_HZ], level_dbfs=-12, seconds=GAP_TRACK_SECONDS,
                    gap={'at_s': GAP_AT_S, 'ms': gap_ms},
                    tags={'title': f'Gap {gap_ms} ms', 'artist': 'Level Lab', 'album': 'Gap Calibration',
                          'track': f'{GAP_CALIBRATION_MS.index(gap_ms) + 1}/{len(GAP_CALIBRATION_MS)}'})
        target = media_root / spec['path']
        target.parent.mkdir(parents=True, exist_ok=True)
        ffmpeg('-f', 'lavfi', '-i', source, *CODECS[spec['codec']], *_tag_args(spec, target), str(target))
        entries.append(_manifest_entry(spec, None))
    return entries


def _make_gapless(media_root):
    """Split one continuous tone into tracks at exact sample offsets."""
    total = GAPLESS_SECONDS * GAPLESS_TRACKS
    master = media_root.parent / 'gapless-master.wav'
    ffmpeg('-f', 'lavfi', '-i', tone_filter([GAPLESS_TONE_HZ], -12, total),
           '-c:a', 'pcm_s16le', str(master))
    per_track = GAPLESS_SECONDS * SAMPLE_RATE
    entries = []
    for codec, folder in GAPLESS_FOLDERS.items():
        for index, title in enumerate(GAPLESS_TITLES):
            piece = media_root.parent / f'gapless-{index}.wav'
            ffmpeg('-i', str(master), '-af',
                   f'atrim=start_sample={index * per_track}:end_sample={(index + 1) * per_track}',
                   '-c:a', 'pcm_s16le', str(piece))
            spec = dict(path=f'{folder}/{index + 1:02d} - {title}.{codec[:codec.index("-")]}',
                        codec=codec, tone_hz=[GAPLESS_TONE_HZ], level_dbfs=-12,
                        seconds=GAPLESS_SECONDS,
                        gapless={'group': folder, 'index': index, 'samples': per_track},
                        tags=dict(GAPLESS_ALBUM, title=title, artist='Continuum',
                                  track=f'{index + 1}/{GAPLESS_TRACKS}'))
            entries.append(_make_track(media_root, spec, source=piece))
            piece.unlink()
    master.unlink()
    return entries


def _make_cover(path):
    ffmpeg('-f', 'lavfi', '-i', 'color=c=0x2b4c7e:s=600x600', '-frames:v', '1',
           '-threads', '1', str(path))


def _write_playlist(path):
    lines = ['#EXTM3U', '#PLAYLIST:Road Trip']
    for entry in PLAYLIST_ENTRIES:
        lines += [f'#EXTINF:{TRACK_SECONDS},{Path(entry).stem}', entry]
    path.write_text('\n'.join(lines) + '\n', encoding='utf-8')


def set_flac_artists(path, artists, key='ARTIST'):
    """Replace a FLAC file's ARTIST comments with one comment per artist.

    ffmpeg can only write a single value per key, but real libraries carry
    repeated ARTIST fields (the case the M03 artist-splitting policy covers),
    so the VORBIS_COMMENT block is rewritten directly. Other comments are kept.
    """
    data = Path(path).read_bytes()
    if data[:4] != b'fLaC':
        raise ValueError(f'{path} is not a FLAC file')
    blocks, offset, last = [], 4, False
    while not last:
        if offset + 4 > len(data):
            raise ValueError(f'{path}: truncated FLAC metadata')
        header = data[offset]
        last, kind = bool(header & 0x80), header & 0x7F
        length = int.from_bytes(data[offset + 1:offset + 4], 'big')
        blocks.append([kind, data[offset + 4:offset + 4 + length]])
        offset += 4 + length
    audio = data[offset:]
    comments = [b for b in blocks if b[0] == 4]
    if not comments:
        raise ValueError(f'{path}: no VORBIS_COMMENT block to rewrite')
    comments[0][1] = _rewrite_comments(comments[0][1], artists, key)
    out = bytearray(b'fLaC')
    for index, (kind, body) in enumerate(blocks):
        out.append(kind | (0x80 if index == len(blocks) - 1 else 0))
        out += len(body).to_bytes(3, 'big') + body
    Path(path).write_bytes(bytes(out) + audio)


def _rewrite_comments(body, artists, key='ARTIST'):
    (vendor_length,) = struct.unpack_from('<I', body, 0)
    vendor = body[4:4 + vendor_length]
    offset = 4 + vendor_length
    (count,) = struct.unpack_from('<I', body, offset)
    offset += 4
    kept = []
    for _ in range(count):
        (length,) = struct.unpack_from('<I', body, offset)
        comment = body[offset + 4:offset + 4 + length]
        offset += 4 + length
        if not comment.upper().startswith((key + '=').encode()):
            kept.append(comment)
    kept += [f'{key}={artist}'.encode() for artist in artists]
    out = struct.pack('<I', len(vendor)) + vendor + struct.pack('<I', len(kept))
    return out + b''.join(struct.pack('<I', len(c)) + c for c in kept)
