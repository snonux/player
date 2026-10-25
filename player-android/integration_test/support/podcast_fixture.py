#!/usr/bin/env python3
"""Disposable publisher and Player fixture for the four-client podcast journey.

Parent-owned commands, from the repository root:
  python3 player-android/integration_test/support/podcast_fixture.py prepare DIR --attach MUSIC_FIXTURE
  python3 player-android/integration_test/support/podcast_fixture.py publisher DIR
  python3 player-android/integration_test/support/podcast_fixture.py setup DIR

`publisher` stays foreground. The existing marked music fixture supplies
Player, its administrator and ordinary media. This helper never starts/stops
Player, changes its configuration or resets media. Setup adds only its own
isolation account. Catalog/listening actions belong to the installed clients.
Fault controls are private files, not an exposed network administration API.
"""
import argparse
from array import array
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from email.utils import format_datetime
import hashlib
import fcntl
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import math
import os
from pathlib import Path
import re
import secrets
import sqlite3
import sys
import threading
import time
from urllib.parse import parse_qs, urlsplit
import wave
from xml.sax.saxutils import escape

from fixture_server import Client, FixtureError, ensure_port_free, write_private_json

MARKER = '.player-podcast-fixture'
RATE = 44100
MAX_DELAY = 8


@contextmanager
def locked_faults(directory):
    """Share one atomic counter update with the optional CLI control process."""
    directory = Path(directory)
    with (directory / '.faults.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        faults = json.loads((directory / 'faults.json').read_text())
        yield faults
        temporary = directory / ('faults-' + secrets.token_hex(8) + '.tmp')
        try:
            write_private_json(temporary, faults)
            os.replace(temporary, directory / 'faults.json')
        finally:
            temporary.unlink(missing_ok=True)


def load(directory):
    directory = Path(directory).resolve(strict=True)
    if not (directory / MARKER).is_file():
        raise FixtureError('Refusing an unmarked podcast fixture directory')
    state = json.loads((directory / 'state.json').read_text())
    if Path(state['directory']).resolve() != directory:
        raise FixtureError('Fixture directory differs from its ownership record')
    for name in ('origin', 'publisher_origin'):
        url = urlsplit(state[name])
        if url.scheme != 'http' or url.hostname != '127.0.0.1' or not url.port:
            raise FixtureError('Only explicit loopback fixture origins are supported')
    return directory, state


def write_tone(path, seconds, frequency):
    """Write real deterministic PCM; duration derives from frames, not metadata."""
    samples = array('h', (round(8000 * math.sin(2 * math.pi * frequency * i / RATE))
                          for i in range(RATE)))
    if sys.byteorder == 'big':
        samples.byteswap()
    with wave.open(str(path), 'wb') as target:
        target.setparams((1, 2, RATE, 0, 'NONE', 'not compressed'))
        for _ in range(seconds):
            target.writeframesraw(samples.tobytes())
    data = path.read_bytes()
    return {'file': path.name, 'sha256': hashlib.sha256(data).hexdigest(),
            'size': len(data), 'duration_s': seconds, 'tone_hz': frequency,
            'frames': RATE * seconds, 'sample_rate': RATE}


def prepare(directory, attached, publisher_port=18776):
    directory = Path(directory).resolve()
    if directory.exists():
        raise FixtureError('Choose a new directory; existing directories are never overwritten')
    attached = Path(attached).resolve(strict=True)
    if not (attached / '.player-music-fixture').is_file():
        raise FixtureError('Attach requires a marked disposable music fixture')
    base = json.loads((attached / 'state.json').read_text())
    auth = json.loads((attached / 'auth.json').read_text())
    port = base['port']
    directory.mkdir(mode=0o700, parents=True)
    (directory / MARKER).write_text('Disposable podcast acceptance fixture; retain evidence.\n')
    publisher = directory / 'publisher'
    publisher.mkdir()
    audio = {
        'resume': write_tone(publisher / 'resume.wav', 60, 441),
        'end': write_tone(publisher / 'end.wav', 12, 882),
    }
    origin = f'http://127.0.0.1:{port}'
    publisher_origin = f'http://127.0.0.1:{publisher_port}'
    feeds = []
    for slug, title, count in [('alpha', 'Acceptance Alpha', 103), ('beta', 'Acceptance Beta', 3)]:
        episodes = []
        for index in range(1, count + 1):
            source = 'resume' if index == 1 else 'end'
            episodes.append({
                'key': f'guid:acceptance-{slug}-{index:03}',
                'title': f'{slug.title()} Episode {index:03}',
                'description': f'Isolated {slug} publisher episode {index:03}.',
                'published': format_datetime(datetime(2026, 1, 1, tzinfo=timezone.utc)
                                             - timedelta(days=index)),
                'audio': source,
                'url': f'{publisher_origin}/audio/{audio[source]["file"]}?episode={slug}-{index:03}',
            })
        feed_url = f'{publisher_origin}/feeds/{slug}.xml'
        feeds.append({'slug': slug, 'title': title, 'url': feed_url,
                      'key': 'feed:' + feed_url.split('://', 1)[1], 'episodes': episodes})
    state = {'schema': 1, 'run_id': secrets.token_hex(8), 'directory': str(directory),
             'origin': origin, 'port': port, 'publisher_origin': publisher_origin,
             'publisher_port': publisher_port, 'server': base['server'],
             'server_directory': str(attached), 'database': str(attached / 'player.db'),
             'evidence_dir': str(directory / 'evidence'), 'feeds': feeds, 'audio': audio}
    with sqlite3.connect((attached / 'player.db').as_uri() + '?mode=ro', uri=True) as db:
        state['accounts'] = {'admin': db.execute('SELECT id FROM users WHERE username=?', (auth['admin']['username'],)).fetchone()[0]}
        db.row_factory = sqlite3.Row
        ordinary = db.execute("SELECT m.id,m.file_name,m.duration,m.abs_path,s.name AS set_name FROM media m JOIN sets s ON s.id=m.set_id WHERE s.is_podcast=0 AND m.type='audio' AND m.deleted_at IS NULL ORDER BY m.duration DESC LIMIT 1").fetchone()
        if ordinary is None:
            raise FixtureError('Attached fixture needs one ordinary audio source')
        state['ordinary'] = dict(ordinary)
    write_private_json(directory / 'state.json', state)
    write_private_json(directory / 'auth.json', {
        'admin': auth['admin'],
        'other': {'username': 'podcast-isolation-' + state['run_id'], 'password': secrets.token_urlsafe(24)}})
    write_private_json(directory / 'faults.json', {})
    return state


def feed_document(feed, audio):
    items = []
    for episode in feed['episodes']:
        source = audio[episode['audio']]
        guid = episode['key'].removeprefix('guid:')
        items.append(f'<item><title>{escape(episode["title"])}</title>'
                     f'<guid isPermaLink="false">{escape(guid)}</guid>'
                     f'<description>{escape(episode["description"])}</description>'
                     f'<pubDate>{episode["published"]}</pubDate>'
                     f'<enclosure url="{escape(episode["url"])}" length="{source["size"]}" type="audio/wav"/>'
                     f'<itunes:duration>{source["duration_s"]}</itunes:duration></item>')
    return ('<?xml version="1.0"?><rss version="2.0" '
            'xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">'
            f'<channel><title>{escape(feed["title"])}</title>'
            f'<link>{escape(feed["url"])}</link><description>Isolated acceptance feed</description>'
            + ''.join(items) + '</channel></rss>').encode()


def byte_range(header, size):
    """One RFC byte range; reject malformed/multiple/unsatisfiable ranges."""
    match = re.fullmatch(r'bytes=(\d*)-(\d*)', header or '')
    if not match or not any(match.groups()):
        raise ValueError('Invalid byte range')
    first, last = match.groups()
    if first:
        start = int(first)
        end = min(int(last), size - 1) if last else size - 1
    else:
        suffix = int(last)
        if suffix <= 0:
            raise ValueError('Invalid suffix')
        start, end = max(0, size - suffix), size - 1
    if start >= size or start > end:
        raise ValueError('Unsatisfiable byte range')
    return start, end


class Publisher:
    def __init__(self, directory, state):
        self.directory, self.state = Path(directory), state
        self.lock = threading.Lock()

    def fault(self, path):
        with self.lock, locked_faults(self.directory) as faults:
            fault = faults.get(path, {})
            if fault.get('remaining', 0) <= 0:
                return {}
            fault = dict(fault)
            faults[path]['remaining'] -= 1
            return fault

    def record(self, data):
        with self.lock, (self.directory / 'publisher-requests.jsonl').open('a') as target:
            target.write(json.dumps({'at_ms': round(time.time() * 1000), **data}) + '\n')

    def handler(self):
        publisher = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def serve(self):
                url = urlsplit(self.path)
                path = url.path
                tag = parse_qs(url.query).get('episode', [''])[0]
                if not re.fullmatch(r'(alpha|beta)-\d{3}', tag):
                    tag = None
                status, length, sent = 500, 0, 0
                try:
                    fault = publisher.fault(path)
                    time.sleep(min(MAX_DELAY, max(0, fault.get('delay_s', 0))))
                    feeds = {f'/feeds/{f["slug"]}.xml': f for f in publisher.state['feeds']}
                    if path in feeds:
                        body = feed_document(feeds[path], publisher.state['audio'])
                        content_type = 'application/rss+xml; charset=utf-8'
                    elif path in ('/audio/resume.wav', '/audio/end.wav'):
                        body = (publisher.directory / 'publisher' / Path(path).name).read_bytes()
                        content_type = 'audio/wav'
                    else:
                        body, content_type = b'Not found\n', 'text/plain'
                        status = 404
                    etag = '"' + hashlib.sha256(body).hexdigest() + '"'
                    modified = 'Thu, 01 Jan 2026 00:00:00 GMT'
                    start, end = 0, len(body) - 1
                    status = fault.get('status', status if status == 404 else 200)
                    if status == 200 and (self.headers.get('If-None-Match') == etag or
                            (not self.headers.get('If-None-Match') and
                             self.headers.get('If-Modified-Since') == modified)):
                        status, body = 304, b''
                    elif status == 200 and self.headers.get('Range') and (
                            self.headers.get('If-Range') in (None, etag, modified)):
                        try:
                            start, end = byte_range(self.headers['Range'], len(body))
                            status = 206
                        except ValueError:
                            status = 416
                    length = end - start + 1 if status in (200, 206) else len(body) if status == 404 else 0
                    self.send_response(status)
                    self.send_header('Content-Type', content_type)
                    self.send_header('Content-Length', str(length))
                    self.send_header('ETag', etag)
                    self.send_header('Last-Modified', modified)
                    self.send_header('Accept-Ranges', 'bytes')
                    if status == 206:
                        self.send_header('Content-Range', f'bytes {start}-{end}/{len(body)}')
                    if status == 416:
                        self.send_header('Content-Range', f'bytes */{len(body)}')
                    self.end_headers()
                    if self.command != 'HEAD' and length:
                        payload = body[start:end + 1] if status in (200, 206) else body
                        if fault.get('truncate'):
                            payload = payload[:max(1, len(payload) // 2)]
                        self.wfile.write(payload)
                        sent = len(payload)
                except (BrokenPipeError, ConnectionResetError):
                    pass
                finally:
                    publisher.record({'method': self.command, 'path': path,
                        'episode_tag': tag,
                        'status': status, 'declared_bytes': length,
                        'sent_bytes': sent,
                        'range': self.headers.get('Range'),
                        'player_credentials_present': bool(self.headers.get('Authorization') or self.headers.get('Cookie'))})

            do_GET = do_HEAD = serve
        return Handler


def podcast_media(database):
    """Read live server state through SQLite's normal read-only WAL snapshot."""
    with sqlite3.connect(Path(database).as_uri() + '?mode=ro', uri=True) as db:
        return {'linked_media': db.execute('SELECT count(*) FROM podcast_episodes WHERE media_id IS NOT NULL').fetchone()[0],
                'podcast_media': db.execute('SELECT count(*) FROM media m JOIN sets s ON s.id=m.set_id WHERE s.is_podcast=1').fetchone()[0]}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    setup = sub.add_parser('prepare')
    setup.add_argument('directory', type=Path)
    setup.add_argument('--attach', type=Path, required=True)
    setup.add_argument('--publisher-port', type=int, default=18776)
    for name in ('publisher', 'setup', 'status', 'fault'):
        action = sub.add_parser(name)
        action.add_argument('directory', type=Path)
        if name == 'fault':
            action.add_argument('--path', required=True)
            action.add_argument('--mode', choices=['clear', 'fail', 'delay', 'truncate'], required=True)
            action.add_argument('--count', type=int, default=1)
            action.add_argument('--seconds', type=float, default=5)
    args = parser.parse_args(argv)
    if args.command == 'prepare':
        state = prepare(args.directory, args.attach, args.publisher_port)
        print(json.dumps({'directory': state['directory'], 'origin': state['origin'],
                          'publisher_origin': state['publisher_origin']}))
        return
    directory, state = load(args.directory)
    if args.command == 'publisher':
        ensure_port_free(state['publisher_port'])
        server = ThreadingHTTPServer(('127.0.0.1', state['publisher_port']), Publisher(directory, state).handler())
        print(json.dumps({'publisher_origin': state['publisher_origin']}), flush=True)
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            pass
        finally:
            server.server_close()
    elif args.command == 'setup':
        auth = json.loads((directory / 'auth.json').read_text())
        admin = Client(state['origin']).login(**auth['admin'])
        users = admin.request('GET', '/api/v1/admin/users')
        other = next((u for u in users if u['username'] == auth['other']['username']), None)
        if other is None:
            other = admin.request('POST', '/api/v1/admin/users', {**auth['other'], 'is_admin': False})
        state['accounts']['other'] = other['id']
        write_private_json(directory / 'state.json', state)
        print(json.dumps({'isolation_account_id': other['id']}))
    elif args.command == 'fault':
        known = {f'/feeds/{feed["slug"]}.xml' for feed in state['feeds']}
        known.update(('/audio/resume.wav', '/audio/end.wav'))
        if args.path not in known:
            raise FixtureError('Faults may target only known publisher paths')
        if not 1 <= args.count <= 20 or not 0 <= args.seconds <= MAX_DELAY:
            raise FixtureError('Fault count/delay exceeds the bounded fixture limits')
        with locked_faults(directory) as faults:
            faults[args.path] = ({'remaining': 0} if args.mode == 'clear' else {
                'remaining': args.count,
                **({'status': 503} if args.mode == 'fail' else {}),
                **({'delay_s': args.seconds} if args.mode == 'delay' else {}),
                **({'truncate': True} if args.mode == 'truncate' else {})})
    else:
        print(json.dumps({'directory': str(directory),
            'podcast_media': podcast_media(state['database'])}))


if __name__ == '__main__':
    main()
