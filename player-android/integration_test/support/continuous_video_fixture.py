#!/usr/bin/env python3
"""Add three short videos and artwork to a disposable music_fixture.py instance."""
import argparse
import http.cookiejar
import json
from pathlib import Path
import subprocess
import time
import urllib.request


def prepare(directory):
    directory = Path(directory).resolve(strict=True)
    if not (directory / '.player-music-fixture').is_file():
        raise ValueError('Requires a disposable music_fixture.py directory')
    state = json.loads((directory / 'state.json').read_text())
    media = Path(state['media_root']).resolve(strict=True)
    if not media.is_relative_to(directory):
        raise ValueError('Fixture media root must stay inside its directory')
    videos = media / 'Videos'
    videos.mkdir(exist_ok=True)
    for index, color in enumerate(['red', 'green', 'blue'], 1):
        subprocess.run([
            'ffmpeg', '-hide_banner', '-loglevel', 'error', '-f', 'lavfi',
            '-i', f'color=c={color}:s=320x180:r=15', '-f', 'lavfi', '-i',
            f'sine=frequency={400 + index * 100}:sample_rate=44100', '-t', '12',
            '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p',
            '-c:a', 'aac', '-movflags', '+faststart', '-y',
            str(videos / f'0{index}.mp4')], check=True)
    subprocess.run([
        'ffmpeg', '-hide_banner', '-loglevel', 'error', '-f', 'lavfi', '-i',
        'color=c=white:s=100x100', '-frames:v', '1', '-y',
        str(videos / '00-cover.jpg')], check=True)
    opener = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    def post(path, data):
        with opener.open(urllib.request.Request(
                state['origin'] + path, data=json.dumps(data).encode(),
                headers={'Content-Type': 'application/json'}), timeout=120):
            pass
    post('/api/login', json.loads((directory / 'auth.json').read_text())['admin'])
    post('/api/admin/rescan', {})
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        with opener.open(state['origin'] + '/api/sets', timeout=30) as response:
            sets = json.load(response)
        indexed = next((item for item in sets if item['name'] == 'Videos'), None)
        if indexed is not None:
            with opener.open(state['origin'] + f'/api/media?set_id={indexed["id"]}&type=video&limit=100', timeout=30) as response:
                rows = json.load(response)
            rows = rows if isinstance(rows, list) else rows.get('media', [])
            if {'01.mp4', '02.mp4', '03.mp4'} <= {row['file_name'] for row in rows}:
                break
        time.sleep(.25)
    else:
        raise TimeoutError('Rescan did not index all three continuous-playback videos')
    print(f'Continuous video fixture ready at {state["origin"]}')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture_dir')
    prepare(parser.parse_args().fixture_dir)
