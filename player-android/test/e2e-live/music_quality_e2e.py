#!/usr/bin/env python3
"""M50 touch acceptance on a signed-in owned fixture; never reset app data.

Root owns the emulator, installed APK and fixture. The local transparent proxy
records request paths/status/ranges only, never credentials. Preferences are read
only. Playback and output must come from the actual released app and QEMU WAV.
"""
import argparse
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import re
import threading
import time
from urllib.parse import parse_qs, urlencode, urlsplit

import adrv
import app_ui
import music_e2e as baseline
from music_loudness_e2e import Run as BaseRun


class Run(BaseRun):
    def open_sheet(self):
        if not self.sheet:
            node = baseline.find_visible('^Quality$')
            assert node, app_ui.labels()
            adrv.tap_node(node)
            time.sleep(.3)
            self.sheet = True

    def close_sheet(self):
        if self.sheet:
            app_ui.back()
            self.sheet = False

    def choose(self, label):
        self.open_sheet()
        self.touch('^Quality /')
        self.touch('^' + re.escape(label) + '$')
        self.touch('^Network settings to edit / Wi-Fi$')
        self.touch('^Mobile / unknown$')
        self.touch('^Quality /')
        self.touch('^' + re.escape(label) + '$')


def boundaries(run):
    port = str(run.state['port'])
    run.open_sheet()
    original_night = adrv.adb('shell', 'cmd', 'uimode', 'night').strip().rsplit(' ', 1)[-1]
    try:
        for theme, mode in [('light', 'no'), ('dark', 'yes')]:
            adrv.adb('shell', 'cmd', 'uimode', 'night', mode)
            time.sleep(.5)
            adrv.shot(str(run.args.evidence / ('quality-' + theme + '.png')))
        adrv.adb('reverse', '--remove', 'tcp:' + port)
        run.touch('^Reload profiles$')
        assert adrv.wait('Server profiles unavailable', timeout=30), app_ui.labels()
        run.touch('^Quality /')
        run.touch('^Auto \\(compatibility when needed\\)$')
        run.close_sheet()
        run.touch('^Skip back 15 seconds$')
        play = run.visible('^Play$')
        mark = run.audio.mark()
        adrv.tap_node(play)
        run.wait_playing()
        time.sleep(3)
        run.pause()
        assert baseline.session_state()['title'] == 'Three Tones'
        wav = run.audio.save(mark, run.args.evidence / 'buffered-offline.wav')
        analysis = baseline.audio_trace.analyze(wav, [100, 1000, 8000])
        assert baseline.main_segment(analysis)['duration_s'] >= 2, analysis
        run.record('Actual disconnected endpoint: profile list fails; Auto remains usable; existing buffered source still audible', audio=analysis)
    finally:
        run.pause()
        adrv.adb('reverse', 'tcp:' + port, 'tcp:' + port)
        adrv.adb('shell', 'cmd', 'uimode', 'night', original_night)
    run.open_sheet()
    run.touch('^Reload profiles$')
    assert adrv.wait('^Server profiles ready.$'), app_ui.labels()
    run.close_sheet()
    run.record('Quality light/dark native sheets, Back, and profile reload recover after restoring endpoint')


def encoder_recovery(run, requests):
    assert adrv.wait('^Retry$'), app_ui.labels()
    assert any('could not convert' in label for label in app_ui.labels())
    media_id = run.state['media_ids']['Music/Aurora Lane - Night Transit (2021)/03 - Harbour.flac']
    # Repeat the failing request through the owned proxy so the native error
    # has its own actual HTTP500 trace before changing the preference.
    run.touch('^Retry$')
    deadline = time.monotonic() + 30
    failed_path = f'/media/{media_id}/compat?profile=opus96'
    while not any(failed_path in r['path'] and r['status'] == 500 for r in requests):
        assert time.monotonic() < deadline, requests
        time.sleep(.2)
    assert adrv.wait('^Retry$'), app_ui.labels()
    adrv.shot(str(run.args.evidence / 'encoder-error-before.png'))
    run.choose('Original (no conversion)' if run.args.profile == 'original'
               else 'Auto (compatibility when needed)')
    run.close_sheet()
    retry = run.visible('^Retry$')
    mark = run.audio.mark()
    adrv.tap_node(retry)
    run.wait_playing()
    time.sleep(3)
    run.pause()
    session = baseline.session_state()
    assert session['title'] == 'Harbour', session
    assert any(f'/media/{media_id}/stream' in r['path'] and r['status'] in [200, 206]
               for r in requests), requests
    wav = run.audio.save(mark, run.args.evidence / 'recovered.wav')
    analysis = baseline.audio_trace.analyze(wav)
    manifest = json.loads(Path(run.state['manifest_file']).read_text())
    expected_tone = next(t['tone_hz'][0] for t in manifest['tracks'] if
                         t['path'] == 'Music/Aurora Lane - Night Transit (2021)/03 - Harbour.flac')
    segment = baseline.main_segment(analysis)
    assert segment['duration_s'] >= 2 and abs(segment['tone_hz'] - expected_tone) < 20, analysis
    thumb = run.visible(r'^\d+%$', 'SeekBar')
    adrv.adb('shell', 'input', 'tap', str(run.dimensions[0] // 2),
             str((thumb['b'][1] + thumb['b'][3]) // 2))
    time.sleep(.4)
    sought = baseline.session_state()
    assert 5500 <= sought['position_ms'] <= 6500, sought
    play = run.visible('^Play$')
    seek_mark = run.audio.mark()
    adrv.tap_node(play)
    run.wait_playing()
    time.sleep(1.5)
    run.pause()
    seek_wav = run.audio.save(seek_mark, run.args.evidence / 'recovered-seek.wav')
    seek_audio = baseline.audio_trace.analyze(seek_wav)
    seek_segment = baseline.main_segment(seek_audio)
    assert seek_segment['duration_s'] >= 1 and abs(seek_segment['tone_hz'] - expected_tone) < 20, seek_audio
    run.record('Real unavailable encoder error: Quality choice and Retry recover original source, actual PCM and seek',
               profile=run.args.profile, session=session, requests=requests, audio=analysis,
               sought=sought, seek_audio=seek_audio)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture', type=Path)
    parser.add_argument('--evidence', required=True, type=Path)
    parser.add_argument('--emulator-wav', required=True, type=Path)
    parser.add_argument('--proxy-port', type=int, default=18772)
    parser.add_argument('--profile', choices=['auto', 'original', 'aac128', 'aac256', 'mp3192', 'opus96'], required=True)
    parser.add_argument('--boundaries', action='store_true')
    parser.add_argument('--encoder-recovery', action='store_true')
    args = parser.parse_args()
    run = Run(args)
    run.sheet = False
    if args.boundaries:
        boundaries(run)
        return
    requests = []
    port = run.state['port']

    class Proxy(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def forward(self):
            conn = http.client.HTTPConnection('127.0.0.1', port, timeout=60)
            body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
            try:
                conn.request(self.command, self.path, body, dict(self.headers))
                response = conn.getresponse()
                content = response.read()
                parsed = urlsplit(self.path)
                profile = parse_qs(parsed.query).get('profile')
                safe_path = parsed.path + ('?' + urlencode({'profile': profile[0]}) if profile else '')
                requests.append({'method': self.command, 'path': safe_path,
                                 'status': response.status, 'range': self.headers.get('Range'),
                                 'content_type': response.getheader('Content-Type'),
                                 'content_range': response.getheader('Content-Range'),
                                 'bytes': len(content)})
                self.send_response(response.status)
                for key, value in response.getheaders():
                    if key.lower() not in ['transfer-encoding', 'connection']:
                        self.send_header(key, value)
                self.end_headers()
                if self.command != 'HEAD':
                    self.wfile.write(content)
            except (BrokenPipeError, ConnectionResetError):
                pass
            finally:
                conn.close()

        do_GET = forward
        do_HEAD = forward
        do_POST = forward
        do_PUT = forward
        do_DELETE = forward

    proxy = ThreadingHTTPServer(('127.0.0.1', args.proxy_port), Proxy)
    threading.Thread(target=proxy.serve_forever, daemon=True).start()
    adrv.adb('reverse', f'tcp:{port}', f'tcp:{args.proxy_port}')
    try:
        if args.encoder_recovery:
            encoder_recovery(run, requests)
            return
        assert args.profile != 'auto', 'Auto is only used by the encoder-recovery scenario'
        labels = {'original': 'Original (no conversion)', 'aac128': 'Balanced · AAC 128 kbps',
                  'aac256': 'High · AAC 256 kbps', 'mp3192': 'Compatible · MP3 192 kbps',
                  'opus96': 'Data saver · Opus 96 kbps'}
        run.choose(labels[args.profile])
        adrv.shot(str(args.evidence / 'quality-sheet.png'))
        expected_key = f'flutter.stream-quality:{run.state["origin"]}:mobile'
        assert run.preference(expected_key) == args.profile
        run.close_sheet()
        media_id = run.state['media_ids']['Music/EQ Probe/Three Tones.flac']
        # Reopen the prebuilt queue through ordinary Next/Previous controls.
        run.touch('^Next track$')
        run.wait_playing()
        run.pause()
        previous = run.visible('^Previous track$')
        mark = run.audio.mark()
        adrv.tap_node(previous)
        run.wait_playing()
        before = baseline.session_state()
        time.sleep(3)
        after = baseline.session_state()
        assert after['state'] == 'PLAYING' and after['title'] == 'Three Tones', after
        run.pause()
        paused = baseline.session_state()
        assert paused['position_ms'] >= 2500, paused
        wav = run.audio.save(mark, args.evidence / 'output.wav')
        analysis = baseline.audio_trace.analyze(wav, [100, 1000, 8000])
        segment = baseline.main_segment(analysis)
        assert segment and segment['duration_s'] >= 2, analysis
        assert all(segment['bands_dbfs'].get(str(f), segment['bands_dbfs'].get(f, -100)) > -40
                   for f in [100, 1000, 8000]), segment
        expected = f'/media/{media_id}/' + ('stream' if args.profile == 'original' else f'compat?profile={args.profile}')
        assert any(expected in r['path'] and r['status'] in [200, 206] for r in requests), requests
        thumb = run.visible(r'^\d+%$', 'SeekBar')
        # The screen and slider padding are symmetric. The screen midpoint is
        # the actual rail midpoint, independent of the thumb's narrow bounds.
        x, y = run.dimensions[0] // 2, (thumb['b'][1] + thumb['b'][3]) // 2
        adrv.shot(str(args.evidence / 'seek-before.png'))
        (args.evidence / 'seek-touch.json').write_text(json.dumps({'thumb': thumb, 'touch': [x, y]}))
        adrv.adb('shell', 'input', 'tap', str(x), str(y))
        time.sleep(.4)
        sought = baseline.session_state()
        assert 5500 <= sought['position_ms'] <= 6500, sought
        play = run.visible('^Play$')
        seek_mark = run.audio.mark()
        adrv.tap_node(play)
        run.wait_playing()
        time.sleep(1.5)
        run.pause()
        seek_wav = run.audio.save(seek_mark, args.evidence / 'seek-output.wav')
        seek_audio = baseline.audio_trace.analyze(seek_wav, [100, 1000, 8000])
        assert baseline.main_segment(seek_audio)['duration_s'] >= 1, seek_audio
        run.record('Selected profile actual native source, progression and PCM', profile=args.profile,
                   before=before, after=after, paused=paused, requests=requests, analysis=analysis,
                   sought=sought, seek_audio=seek_audio)
    finally:
        run.pause()
        (args.evidence / 'requests.json').write_text(json.dumps(requests, indent=2))
        adrv.adb('reverse', f'tcp:{port}', f'tcp:{port}')
        proxy.shutdown()
        proxy.server_close()


if __name__ == '__main__':
    main()
