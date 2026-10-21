#!/usr/bin/env python3
"""Tests for the music E2E support modules.

Run from this directory: python3 -m unittest test_music_support

Unit tests need only Python; the library tests need ffmpeg/ffprobe. The
lifecycle test (FixtureLifecycleTest) starts real servers and runs only when
PLAYER_SERVER_BINARY names a built player-server binary; it proves that
restart keeps state and that down/up starts from a clean, isolated fixture.
"""

import json
import math
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
import threading
import unittest
import urllib.error
import urllib.request
import wave

sys.path.insert(0, str(Path(__file__).resolve().parent))
import audio_trace  # noqa: E402
import evidence  # noqa: E402
import fixture_server  # noqa: E402
from fixture_server import Client, port_is_free  # noqa: E402
import music_library  # noqa: E402
import service_doubles  # noqa: E402

HAVE_FFMPEG = shutil.which('ffmpeg') and shutil.which('ffprobe')
HERE = Path(__file__).resolve().parent


def write_wav(path, samples, rate=8000):
    with wave.open(str(path), 'wb') as file:
        file.setnchannels(1)
        file.setsampwidth(2)
        file.setframerate(rate)
        file.writeframes(b''.join(struct.pack('<h', int(s * 32767)) for s in samples))


def tone(frequency, seconds, level_db, rate=8000):
    amplitude = 10 ** (level_db / 20)
    return [amplitude * math.sin(2 * math.pi * frequency * i / rate) for i in range(int(seconds * rate))]


class AudioTraceTest(unittest.TestCase):
    def setUp(self):
        self.dir = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.dir)

    def test_segments_gap_level_and_tone(self):
        path = self.dir / 'gap.wav'
        write_wav(path, tone(440, 1, -6) + [0.0] * 800 + tone(440, 1, -26))
        result = audio_trace.analyze(path)
        self.assertEqual(len(result['segments']), 2)
        self.assertAlmostEqual(result['gaps_ms'][0], 100, delta=20)
        first, second = result['segments']
        self.assertAlmostEqual(first['tone_hz'], 440, delta=1)
        # A sine's RMS is 3 dB below its peak.
        self.assertAlmostEqual(first['rms_dbfs'], -9, delta=0.2)
        self.assertAlmostEqual(first['rms_dbfs'] - second['rms_dbfs'], 20, delta=0.2)

    def test_band_levels_of_a_mixture(self):
        path = self.dir / 'bands.wav'
        mix = [a + b for a, b in zip(tone(200, 1, -12), tone(1000, 1, -24))]
        write_wav(path, mix)
        bands = audio_trace.analyze(path, [200, 1000, 3000])
        levels = bands['segments'][0]['bands_dbfs']
        self.assertAlmostEqual(levels['200'], -12, delta=0.5)
        self.assertAlmostEqual(levels['1000'], -24, delta=0.5)
        self.assertLess(levels['3000'], -60)

    def test_short_gaps_are_measured_to_the_sample(self):
        rate = 48000
        for gap_ms in (1.5, 5, 12, 20, 50):
            samples = tone(880, 0.5, -12, rate) + [0.0] * int(gap_ms * rate / 1000) + tone(880, 0.5, -12, rate)
            result = audio_trace.analyze_samples(samples, rate, tone=880)
            self.assertEqual(len(result['gaps']), 1, gap_ms)
            self.assertAlmostEqual(result['gaps'][0]['duration_ms'], gap_ms, delta=0.1)
            self.assertAlmostEqual(result['gaps'][0]['start_s'], 0.5, delta=0.001)

    def test_sub_millisecond_silence_and_tone_zero_crossings_are_not_gaps(self):
        rate = 48000
        samples = tone(880, 0.5, -12, rate) + [0.0] * 20 + tone(880, 0.5, -12, rate)
        self.assertEqual(audio_trace.analyze_samples(samples, rate)['gaps'], [])
        quiet_low = tone(100, 1, -30, rate)
        self.assertEqual(audio_trace.analyze_samples(quiet_low, rate)['gaps'], [])

    def test_click_without_silence_is_a_discontinuity(self):
        rate = 48000
        first = tone(880, 0.5, -12, rate)
        amplitude = 10 ** (-12 / 20)
        jumped = [amplitude * math.sin(2 * math.pi * 880 * i / rate + 1.5) for i in range(len(first), 2 * len(first))]
        result = audio_trace.analyze_samples(first + jumped, rate, tone=880)
        self.assertEqual(result['gaps'], [])
        self.assertEqual(len(result['discontinuities']), 1)
        self.assertAlmostEqual(result['discontinuities'][0]['time_s'], 0.5, delta=0.001)

    def test_continuous_tone_has_no_discontinuities(self):
        result = audio_trace.analyze_samples(tone(880, 1, -12, 48000), 48000, tone=880)
        self.assertEqual((result['gaps'], result['discontinuities']), ([], []))

    def test_transition_requires_expected_tone_on_both_sides(self):
        rate = 48000
        before = tone(880, 1.5, -12, rate)
        after = tone(880, 3.5, -12, rate)
        for gap_ms in (0, 20, 100):
            samples = before + [0.0] * int(gap_ms * rate / 1000) + after
            result = audio_trace.analyze_transition(samples, rate, 1.5, 880)
            self.assertEqual(set(result), {'before', 'after'})
            for segment in result.values():
                self.assertAlmostEqual(segment['tone_hz'], 880, delta=1)

    def test_transition_rejects_silent_second_track_despite_no_reported_gap(self):
        rate = 48000
        samples = tone(880, 1.5, -12, rate) + [0.0] * int(3.5 * rate)
        self.assertEqual(audio_trace.analyze_samples(samples, rate)['gaps'], [])
        with self.assertRaisesRegex(ValueError, 'no sustained audio after'):
            audio_trace.analyze_transition(samples, rate, 1.5, 880)

    def test_transition_rejects_missing_wrong_or_brief_audio(self):
        rate = 48000
        good = tone(880, 1, -12, rate)
        for before, after, message in (
                ([0.0] * rate, good, 'before'),
                (tone(440, 1, -12, rate), good, 'before'),
                (good, tone(440, 1, -12, rate), 'after'),
                (good, tone(880, 0.1, -12, rate) + [0.0] * rate, 'after')):
            with self.subTest(message=message), self.assertRaisesRegex(ValueError, message):
                audio_trace.analyze_transition(before + after, rate, 1, 880)

    def test_transition_rejects_event_outside_capture(self):
        samples = tone(880, 1, -12, 48000)
        for ended in (0, -1, 1, 2, float('nan')):
            with self.subTest(ended=ended), self.assertRaisesRegex(ValueError, 'inside the capture'):
                audio_trace.analyze_transition(samples, 48000, ended, 880)

    def test_silence_has_no_segments(self):
        path = self.dir / 'silence.wav'
        write_wav(path, [0.0] * 8000)
        result = audio_trace.analyze(path)
        self.assertEqual(result['segments'], [])
        self.assertEqual(result['audible_s'], 0)

    def test_rejects_non_16_bit_wav(self):
        path = self.dir / 'eight.wav'
        with wave.open(str(path), 'wb') as file:
            file.setnchannels(1)
            file.setsampwidth(1)
            file.setframerate(8000)
            file.writeframes(b'\x80' * 100)
        with self.assertRaises(ValueError):
            audio_trace.analyze(path)


class EvidenceTest(unittest.TestCase):
    def setUp(self):
        self.dir = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.dir)

    def test_latest_result_per_scenario_wins(self):
        log = evidence.Evidence(self.dir, 'android', {'api': '34'})
        log.record('V01', 'play', 'fail')
        log.record('V01', 'play', 'pass', artifacts=[self.dir / 'android' / 'a.png'])
        evidence.Evidence(self.dir, 'chrome').record('V01', 'play', 'gap', proof='none')
        evidence.Evidence(self.dir, 'firefox').record('V01', 'play', 'pass', proof='service-double')
        records = evidence.load(self.dir)
        self.assertEqual(records[1]['artifacts'], ['android/a.png'])
        self.assertEqual(evidence.summarize(records),
                         {('V01', 'play'): {'android': 'pass', 'chrome': 'gap', 'firefox': 'pass*'}})
        self.assertEqual(json.loads((self.dir / 'android' / 'versions.json').read_text()), {'api': '34'})

    def test_rejects_unknown_result_and_proof(self):
        log = evidence.Evidence(self.dir, 'chrome')
        with self.assertRaises(ValueError):
            log.record('V01', 'x', 'ok')
        with self.assertRaises(ValueError):
            log.record('V01', 'x', 'pass', proof='mock')
        self.assertFalse((self.dir / 'chrome' / 'evidence.jsonl').exists())


class ServiceDoublesTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.dir = Path(tempfile.mkdtemp())
        radio = cls.dir / 'radio.mp3'
        radio.write_bytes(b'ID3' + bytes(range(256)) * 64)
        cls.server = service_doubles.serve(0, radio)
        cls.origin = f'http://127.0.0.1:{cls.server.server_address[1]}'
        threading.Thread(target=cls.server.serve_forever, daemon=True).start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        shutil.rmtree(cls.dir)

    def setUp(self):
        Client(self.origin).request('POST', '/_double/reset')

    def call(self, method, path, body=None, headers=None):
        data = body if isinstance(body, bytes) or body is None else json.dumps(body).encode()
        req = urllib.request.Request(self.origin + path, data=data, method=method, headers=headers or {})
        try:
            with urllib.request.urlopen(req, timeout=5) as response:
                return response.status, response.headers, response.read()
        except urllib.error.HTTPError as err:
            with err:
                return err.code, err.headers, err.read()

    def test_listenbrainz_token_and_submission(self):
        good = {'Authorization': f'Token {service_doubles.VALID_TOKEN}', 'Content-Type': 'application/json'}
        status, headers, body = self.call('GET', '/listenbrainz/1/validate-token', headers=good)
        self.assertEqual((status, json.loads(body)['valid']), (200, True))
        self.assertEqual(headers['X-Player-Service-Double'], 'listenbrainz')
        listen = {'listen_type': 'single', 'payload': [{'listened_at': 1, 'track_metadata': {
            'artist_name': 'Aurora Lane', 'track_name': 'First Light'}}]}
        self.assertEqual(self.call('POST', '/listenbrainz/1/submit-listens', listen, good)[0], 200)
        log = Client(self.origin).request('GET', '/_double/requests')
        self.assertEqual(log[-1]['body'], listen)

    def test_listenbrainz_refusals(self):
        status, _, body = self.call('GET', '/listenbrainz/1/validate-token', headers={'Authorization': 'Token nope'})
        self.assertEqual((status, json.loads(body)['valid']), (200, False))
        bad = {'listen_type': 'playing_now', 'payload': [{'listened_at': 1, 'track_metadata': {
            'artist_name': 'A', 'track_name': 'B'}}]}
        auth = {'Authorization': f'Token {service_doubles.VALID_TOKEN}'}
        self.assertEqual(self.call('POST', '/listenbrainz/1/submit-listens', bad, auth)[0], 400)
        self.assertEqual(self.call('POST', '/listenbrainz/1/submit-listens', bad, {})[0], 401)
        for body in ({'listen_type': 'single', 'payload': []},
                     {'listen_type': 'single', 'payload': [{'listened_at': 1, 'track_metadata': {}}]},
                     {'listen_type': 'other', 'payload': [{}]}):
            self.assertIsNotNone(service_doubles.listen_payload_problem(body))

    def test_lastfm_artist_info(self):
        status, _, body = self.call('GET', '/lastfm/2.0/?method=artist.getinfo&artist=Aurora%20Lane&format=json')
        self.assertEqual(status, 200)
        self.assertIn('fictional', json.loads(body)['artist']['bio']['summary'])
        status, _, body = self.call('GET', '/lastfm/2.0/?method=artist.getinfo&artist=Nobody')
        self.assertEqual((status, json.loads(body)['error']), (404, 6))
        self.assertEqual(self.call('GET', '/lastfm/2.0/?method=track.love')[0], 400)

    def test_radio_stream_and_outage(self):
        status, headers, body = self.call('GET', '/radio/fixture.mp3?seconds=0.3')
        self.assertEqual((status, headers['icy-name'], headers['Content-Type']), (200, 'Fixture Radio', 'audio/mpeg'))
        self.assertGreater(len(body), 0)
        self.assertEqual(self.call('GET', '/radio/down.mp3')[0], 503)

    def test_upnp_renderer_actions(self):
        self.assertIn(b'MediaRenderer', self.call('GET', '/upnp/description.xml')[2])
        soap = lambda action, body=b'': self.call('POST', '/upnp/control/AVTransport', body, {
            'SOAPAction': f'"urn:schemas-upnp-org:service:AVTransport:1#{action}"', 'Content-Type': 'text/xml'})
        self.assertEqual(soap('SetAVTransportURI', b'<CurrentURI>http://x/a.mp3</CurrentURI>')[0], 200)
        self.assertEqual(soap('Play')[0], 200)
        self.assertIn(b'PLAYING', soap('GetTransportInfo')[2])
        self.assertEqual(soap('SelfDestruct')[0], 500)
        self.assertEqual(self.call('GET', '/nowhere')[0], 404)

    def test_info_marks_double(self):
        info = Client(self.origin).request('GET', '/_double/info')
        self.assertEqual((info['double'], info['proof']), (True, 'service-double'))


@unittest.skipUnless(HAVE_FFMPEG, 'ffmpeg/ffprobe not installed')
class MusicLibraryTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.dir = Path(tempfile.mkdtemp())
        cls.manifest = music_library.generate(cls.dir / 'media')

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.dir)

    def probe(self, path):
        out = subprocess.run(['ffprobe', '-v', 'error', '-show_entries', 'format=duration:format_tags',
                              '-of', 'json', str(self.dir / 'media' / path)], capture_output=True, text=True, check=True)
        return json.loads(out.stdout)['format']

    def test_manifest_matches_files(self):
        self.assertEqual(self.manifest['sets'], ['Music', 'Restricted'])
        self.assertEqual(self.manifest['media_count'], len(self.manifest['tracks']) + 1)
        for track in self.manifest['tracks']:
            self.assertTrue((self.dir / 'media' / track['path']).is_file(), track['path'])

    def test_tags_unicode_untagged_and_multi_artist(self):
        tags = self.probe('Music/Ünïcødé Artïst - 東京 Nights/02 - Партия для двоих.flac')['tags']
        self.assertEqual(tags['title'], 'Партия для двоих')
        self.assertEqual(tags['ARTIST'], 'Ünïcødé Artïst;Mika Sørensen')
        self.assertNotIn('tags', self.probe('Music/Untagged/track_0042.mp3'))
        self.assertEqual(music_library.expected_title({'path': 'Music/Untagged/track_0042.mp3', 'tags': None}),
                         'track_0042')

    def test_gapless_pieces_are_sample_exact(self):
        out = subprocess.run(['ffprobe', '-v', 'error', '-show_entries', 'stream=duration_ts', '-of', 'csv=p=0',
                              str(self.dir / 'media/Music/Gapless Suite (FLAC)/02 - Movement II.flac')],
                             capture_output=True, text=True, check=True)
        self.assertEqual(int(out.stdout.strip()), music_library.GAPLESS_SECONDS * music_library.SAMPLE_RATE)

    def test_m4a_has_itunes_atoms(self):
        data = (self.dir / 'media/Music/Various Artists - Synth Compilation/Disc 1/01 - Neon Rain.m4a').read_bytes()
        for atom in (b'\xa9nam', b'\xa9ART', b'aART', b'trkn', b'disk', b'cpil'):
            self.assertIn(atom, data)
        self.assertNotIn(b'mdta', data)

    def test_gap_calibration_tracks_hold_their_known_gap(self):
        tracks = [t for t in self.manifest['tracks'] if t['gap']]
        self.assertEqual([t['gap']['ms'] for t in tracks], music_library.GAP_CALIBRATION_MS)
        for item in tracks:
            wav = self.dir / f"{item['gap']['ms']}.wav"
            subprocess.run(['ffmpeg', '-v', 'error', '-y', '-i', str(self.dir / 'media' / item['path']),
                            '-c:a', 'pcm_s16le', str(wav)], check=True)
            result = audio_trace.analyze(wav, tone=880)
            self.assertEqual(len(result['gaps']), 1)
            self.assertAlmostEqual(result['gaps'][0]['duration_ms'], item['gap']['ms'], delta=0.1)
            self.assertAlmostEqual(result['gaps'][0]['start_s'], item['gap']['at_s'], delta=0.001)

    def test_flac_artist_rewrite_rejects_non_flac(self):
        path = self.dir / 'not.flac'
        path.write_bytes(b'RIFF0000')
        with self.assertRaises(ValueError):
            music_library.set_flac_artists(path, ['A'])


class ProcessOwnershipTest(unittest.TestCase):
    def test_identifies_own_process_also_after_its_binary_is_deleted(self):
        directory = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, directory)
        binary = directory / 'sleeper'
        shutil.copy(shutil.which('sleep'), binary)
        process = subprocess.Popen([str(binary), '30'])
        self.addCleanup(process.wait)
        self.addCleanup(process.kill)
        start = fixture_server.process_start(process.pid)
        self.assertTrue(fixture_server.owns_process(process.pid, binary, start=start))
        self.assertFalse(fixture_server.owns_process(process.pid, binary, start=start + 1))
        self.assertFalse(fixture_server.owns_process(process.pid, shutil.which('true')))
        binary.unlink()
        self.assertTrue(fixture_server.stop_pid(process.pid, binary, start=start))
        self.assertIsNotNone(process.poll())
        self.assertFalse(fixture_server.stop_pid(process.pid, binary, start=start))


@unittest.skipUnless(os.environ.get('PLAYER_SERVER_BINARY') and HAVE_FFMPEG, 'set PLAYER_SERVER_BINARY')
class FixtureLifecycleTest(unittest.TestCase):
    """up -> state written -> restart keeps it -> down removes all -> up is clean."""

    PORT, DOUBLES = 18796, 18797

    def fixture(self, *args):
        return subprocess.run([sys.executable, str(HERE / 'music_fixture.py'), *args],
                              capture_output=True, text=True, check=True).stdout

    def up(self, root):
        state = json.loads(self.fixture('up', os.environ['PLAYER_SERVER_BINARY'], '--port', str(self.PORT),
                                        '--doubles-port', str(self.DOUBLES), '--evidence', str(root / 'evidence')))
        directory = state['fixture_dir']
        self.addCleanup(lambda: Path(directory).exists() and self.fixture('down', directory))
        return state, json.loads(Path(state['auth_file']).read_text())

    def test_restart_persists_and_down_isolates(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root)
        state, auth = self.up(root)
        media = state['media_ids']['Music/Gain Check/Loud.flac']
        admin = Client(state['origin']).login(**auth['admin'])
        admin.request('POST', f'/api/v1/media/{media}/favorite')
        user = Client(state['origin']).login(**auth['user'])
        self.assertEqual([s['name'] for s in user.request('GET', '/api/v1/sets')], ['Music'])
        self.assertEqual(Client(state['origin']).call('POST', '/api/v1/auth/login', dict(
            auth['user'], password='wrong'))[0], 401)
        self.fixture('restart', state['fixture_dir'])
        favorites = Client(state['origin']).login(**auth['admin']).request('GET', '/api/v1/media?favorites=true')
        self.assertEqual([m['id'] for m in favorites], [media])
        self.fixture('down', state['fixture_dir'])
        self.assertFalse(Path(state['fixture_dir']).exists())
        self.assertTrue(port_is_free(self.PORT) and port_is_free(self.DOUBLES))
        self.assertTrue((root / 'evidence').is_dir(), 'evidence survives teardown')
        fresh, fresh_auth = self.up(root)
        self.assertNotEqual(fresh['fixture_dir'], state['fixture_dir'])
        self.assertEqual(Client(fresh['origin']).call('POST', '/api/v1/auth/login', auth['admin'])[0], 401)
        again = Client(fresh['origin']).login(**fresh_auth['admin'])
        self.assertEqual(again.request('GET', '/api/v1/media?favorites=true'), [])

    def test_down_refuses_a_directory_without_marker(self):
        directory = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, directory)
        (directory / 'state.json').write_text('{}')
        result = subprocess.run([sys.executable, str(HERE / 'music_fixture.py'), 'down', str(directory)],
                                capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue((directory / 'state.json').exists())


if __name__ == '__main__':
    unittest.main()
