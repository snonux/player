#!/usr/bin/env python3
"""Serve synthetic media for all_formats_playback_test.dart on loopback.

Build player-server first, then pass its binary path. The printed auth file
is used with Flutter's --dart-define-from-file. Ctrl-C stops the server.
"""

import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('server_binary', type=Path)
    parser.add_argument('--port', type=int, default=18765)
    args = parser.parse_args()
    binary = args.server_binary.resolve(strict=True)
    with socket.socket() as probe:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        probe.bind(('127.0.0.1', args.port))
    root = Path(__file__).resolve().parents[3]
    directory = Path(tempfile.mkdtemp(prefix='player-formats-'))
    directory.chmod(0o700)
    media = directory / 'media'
    subprocess.run(['bash', str(root / 'player-server/testdata/gen-all-formats.sh'),
                    str(media)], check=True, stdout=subprocess.DEVNULL)
    videos = media / 'test-videos'
    shutil.copyfile(videos / 'sample-wmv.wmv', videos / 'sample-asf.asf')
    subprocess.run(['ffmpeg', '-nostdin', '-v', 'error', '-y', '-i',
                    str(videos / 'sample-mp4.mp4'), '-c', 'copy', '-f', 'mpegts',
                    str(videos / 'sample-transport.mp4')], check=True)
    origin = f'http://127.0.0.1:{args.port}'
    credentials = {'username': 'formats-e2e', 'password': secrets.token_urlsafe(24)}
    auth = directory / 'auth.json'
    descriptor = os.open(auth, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w') as file:
        json.dump(dict(credentials, VIDEO_E2E_ORIGIN=origin), file)
    environment = dict(os.environ, PORT=str(args.port), MEDIA_ROOT=str(media),
                       DB_PATH=str(directory / 'player.db'), SECURE_COOKIES='false',
                       LOG_LEVEL='warn')
    opener = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def request(path, data=None):
        body = None if data is None else json.dumps(data).encode()
        req = urllib.request.Request(origin + path, data=body,
                                     headers={'Content-Type': 'application/json'})
        with opener.open(req, timeout=5) as response:
            content = response.read()
            if not content:
                return None
            if 'application/json' in response.headers.get('Content-Type', ''):
                return json.loads(content)
            return content.decode()

    process = None
    try:
        with (directory / 'server.log').open('w') as log:
            process = subprocess.Popen([str(binary)], cwd=root / 'player-server',
                                       env=environment, stdout=log, stderr=log)
        deadline = time.monotonic() + 60
        while True:
            if process.poll() is not None:
                raise RuntimeError(f'Server exited; see {directory / "server.log"}')
            try:
                request('/healthz')
                break
            except (urllib.error.URLError, TimeoutError):
                if time.monotonic() > deadline:
                    raise
                time.sleep(0.2)
        request('/api/bootstrap', credentials)
        request('/api/admin/rescan', {})
        while len(request('/api/v1/media?limit=100')) != 28:
            if time.monotonic() > deadline:
                raise RuntimeError('Fixture scan did not finish')
            time.sleep(0.2)
        request('/api/v1/auth/logout', {})
        print(json.dumps({'fixture_directory': str(directory),
                          'origin': origin, 'auth_file': str(auth)}), flush=True)
        process.wait()
    except KeyboardInterrupt:
        pass
    finally:
        if process is not None and process.poll() is None:
            process.send_signal(signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


if __name__ == '__main__':
    main()
