import http.client
from contextlib import closing
import json
from pathlib import Path
import sqlite3
import tempfile
import threading
import time
import unittest
import wave
import xml.etree.ElementTree as ET
from urllib.parse import urlsplit
from http.server import ThreadingHTTPServer

import podcast_fixture as fixture


class PodcastFixtureTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        attached = self.root / 'music'
        attached.mkdir()
        (attached / '.player-music-fixture').write_text('Disposable fixture')
        (attached / 'state.json').write_text(json.dumps({'port': 18766,
            'server': {'binary': '/owned/player', 'pid': 123}}))
        (attached / 'auth.json').write_text(json.dumps({'admin': {
            'username': 'admin', 'password': 'private-fixture-password'}}))
        with sqlite3.connect(attached / 'player.db') as db:
            db.executescript('CREATE TABLE users(id INTEGER,username TEXT);'
                "INSERT INTO users VALUES(1,'admin');"
                'CREATE TABLE sets(id INTEGER,name TEXT,is_podcast INTEGER);'
                "INSERT INTO sets VALUES(1,'Music',0);"
                'CREATE TABLE media(id INTEGER,set_id INTEGER,file_name TEXT,duration REAL,abs_path TEXT,type TEXT,deleted_at TEXT);'
                "INSERT INTO media VALUES(1,1,'ordinary.wav',45,'/fixture/ordinary.wav','audio',NULL);")
        self.directory = self.root / 'podcasts'
        self.state = fixture.prepare(self.directory, attached)
        self.server = ThreadingHTTPServer(('127.0.0.1', 0),
            fixture.Publisher(self.directory, self.state).handler())
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
        self.temp.cleanup()

    def request(self, path, method='GET', headers=None):
        with closing(http.client.HTTPConnection('127.0.0.1', self.server.server_port, timeout=3)) as client:
            client.request(method, path, headers=headers or {})
            response = client.getresponse()
            return response.status, dict(response.getheaders()), response.read()

    def test_real_audio_feed_counts_and_attached_library_are_preserved(self):
        before = (self.root / 'music/player.db').read_bytes()
        for feed, count in zip(self.state['feeds'], (103, 3)):
            status, _, data = self.request(f'/feeds/{feed["slug"]}.xml')
            self.assertEqual(status, 200)
            items = ET.fromstring(data).findall('./channel/item')
            self.assertEqual(len(items), count)
            self.assertEqual(len({i.findtext('guid') for i in items}), count)
            # Canonical identities retain each query: even an ingest path that
            # deduplicates enclosures must see all 103/3 episodes independently.
            urls = [urlsplit(i.find('enclosure').attrib['url']) for i in items]
            self.assertEqual(len({(url.hostname, url.port, url.path, url.query) for url in urls}), count)
            self.assertEqual(int(items[0].find('enclosure').attrib['length']),
                             self.state['audio']['resume']['size'])
        source = self.directory / 'publisher/resume.wav'
        with wave.open(str(source)) as audio:
            self.assertEqual(audio.getnframes(), 60 * fixture.RATE)
            self.assertEqual(audio.getnframes() / audio.getframerate(), 60)
        self.assertEqual((self.root / 'music/player.db').read_bytes(), before)
        self.assertFalse((self.directory / 'media').exists())
        self.assertEqual((self.directory / 'auth.json').stat().st_mode & 0o777, 0o600)

    def test_head_range_suffix_unsatisfiable_and_if_range(self):
        whole = (self.directory / 'publisher/resume.wav').read_bytes()
        status, headers, data = self.request('/audio/resume.wav', 'HEAD')
        self.assertEqual((status, int(headers['Content-Length']), data), (200, len(whole), b''))
        for header, expected in [('bytes=0-43', whole[:44]), ('bytes=-16', whole[-16:]),
                                 ('bytes=100-', whole[100:])]:
            status, headers, body = self.request('/audio/resume.wav', headers={'Range': header})
            self.assertEqual(status, 206)
            self.assertEqual(body, expected)
            self.assertIn('Content-Range', headers)
        for header in ('bytes=999999999-', 'bytes=-0', 'bytes=5-2', 'bytes=0-2,4-6'):
            status, headers, body = self.request('/audio/resume.wav', headers={'Range': header})
            self.assertEqual((status, body), (416, b''))
            self.assertEqual(headers['Content-Range'], f'bytes */{len(whole)}')
        self.assertEqual(self.request('/audio/resume.wav', headers={
            'Range': 'bytes=0-3', 'If-Range': 'different-validator'})[0], 200)

    def test_conditional_refresh_and_sanitized_request_accounting(self):
        _, headers, _ = self.request('/feeds/alpha.xml')
        self.assertEqual(self.request('/feeds/alpha.xml', headers={
            'If-None-Match': headers['ETag']})[::2], (304, b''))
        self.assertEqual(self.request('/feeds/alpha.xml', headers={
            'If-Modified-Since': headers['Last-Modified']})[0], 304)
        self.request('/audio/end.wav?private=do-not-log', headers={'Authorization': 'Bearer do-not-log'})
        self.request('/audio/end.wav?episode=alpha-003&private=do-not-log')
        # A new request finishes the preceding handler before inspecting its log.
        self.server.shutdown()
        self.thread.join()
        log = (self.directory / 'publisher-requests.jsonl').read_text()
        self.assertNotIn('do-not-log', log)
        rows = [json.loads(line) for line in log.splitlines()]
        self.assertTrue(any(r['player_credentials_present'] for r in rows))
        self.assertTrue(any(r['episode_tag'] == 'alpha-003' for r in rows))

    def test_fault_is_one_shot_and_private_marker_is_required(self):
        fixture.main(['fault', str(self.directory), '--path', '/audio/end.wav', '--mode', 'fail'])
        self.assertEqual(self.request('/audio/end.wav')[0], 503)
        self.assertEqual(self.request('/audio/end.wav')[0], 200)
        with self.assertRaises(fixture.FixtureError):
            fixture.load(self.root / 'music')
        with self.assertRaises(fixture.FixtureError):
            fixture.prepare(self.directory, self.root / 'music')

    def test_delay_and_truncation_are_real_bounded_http_faults(self):
        fixture.main(['fault', str(self.directory), '--path', '/feeds/alpha.xml',
                      '--mode', 'delay', '--seconds', '.03'])
        start = time.monotonic()
        self.assertEqual(self.request('/feeds/alpha.xml')[0], 200)
        self.assertGreaterEqual(time.monotonic() - start, .025)
        fixture.main(['fault', str(self.directory), '--path', '/audio/end.wav',
                      '--mode', 'truncate'])
        with self.assertRaises(http.client.IncompleteRead):
            self.request('/audio/end.wav')
        self.assertEqual(self.request('/audio/end.wav')[0], 200)
        with self.assertRaises(fixture.FixtureError):
            fixture.main(['fault', str(self.directory), '--path', '/feeds/not-owned.xml', '--mode', 'fail'])


if __name__ == '__main__':
    unittest.main()
