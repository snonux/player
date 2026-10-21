#!/usr/bin/env python3
"""Serve synthetic media for all_formats_playback_test.dart on loopback.

Build player-server first, then pass its binary path. The printed auth file
is used with Flutter's --dart-define-from-file. Ctrl-C or SIGTERM stops the
server.
"""

import argparse
import json
from pathlib import Path
import secrets
import shutil
import signal
import subprocess
import sys
import tempfile

from fixture_server import PlayerServer, bootstrap_and_scan, ensure_port_free, write_private_json

# 26 generated samples (one per extension in internal/mediatype) plus the two
# derived copies made in generate_media(): the scan must list exactly these.
EXPECTED_MEDIA = 28


def generate_media(media):
    """All-formats library plus the ASF and MPEG-TS-in-.mp4 edge cases."""
    root = Path(__file__).resolve().parents[3]
    subprocess.run(['bash', str(root / 'player-server/testdata/gen-all-formats.sh'),
                    str(media)], check=True, stdout=subprocess.DEVNULL)
    videos = media / 'test-videos'
    shutil.copyfile(videos / 'sample-wmv.wmv', videos / 'sample-asf.asf')
    subprocess.run(['ffmpeg', '-nostdin', '-v', 'error', '-y', '-i',
                    str(videos / 'sample-mp4.mp4'), '-c', 'copy', '-f', 'mpegts',
                    str(videos / 'sample-transport.mp4')], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('server_binary', type=Path)
    parser.add_argument('--port', type=int, default=18765)
    args = parser.parse_args()
    ensure_port_free(args.port)
    directory = Path(tempfile.mkdtemp(prefix='player-formats-'))
    directory.chmod(0o700)
    server = PlayerServer(args.server_binary, directory, args.port)
    generate_media(server.media)
    credentials = {'username': 'formats-e2e', 'password': secrets.token_urlsafe(24)}
    auth = directory / 'auth.json'
    write_private_json(auth, dict(credentials, VIDEO_E2E_ORIGIN=server.origin))
    # Stop the server on SIGTERM as on Ctrl-C: a fixture started in the
    # background ignores SIGINT, and an orphaned server would keep the port.
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
    try:
        server.start()
        client = bootstrap_and_scan(server.origin, credentials, EXPECTED_MEDIA)
        client.request('POST', '/api/v1/auth/logout', {})
        print(json.dumps({'fixture_directory': str(directory),
                          'origin': server.origin, 'auth_file': str(auth)}), flush=True)
        server.process.wait()
    except KeyboardInterrupt:
        pass
    finally:
        server.stop()


if __name__ == '__main__':
    main()
