#!/usr/bin/env python3
"""Run a disposable real Player server for the Android offline test.

Requires ffmpeg and a built player-server binary. Credentials and media stay in
the printed temporary directory. Ctrl-C stops the server and verifies originals.
"""

import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import signal
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("server_binary", type=Path)
    parser.add_argument("--port", type=int, default=18765)
    parser.add_argument("--personal-media", action="store_true", help="Also add two 60-second files for personal_media_test.dart")
    args = parser.parse_args()
    binary = args.server_binary.resolve(strict=True)
    # Refuse an occupied port instead of accidentally bootstrapping some other
    # Player instance. Keep all network operations bound to loopback.
    import socket
    with socket.socket() as probe:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        probe.bind(("127.0.0.1", args.port))

    directory = Path(tempfile.mkdtemp(prefix="player-offline-"))
    directory.chmod(0o700)
    media = directory / "media" / "OfflineE2E"
    media.mkdir(parents=True)

    def ffmpeg(*arguments):
        subprocess.run(["ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error",
                        "-y", *arguments], check=True)

    ffmpeg("-f", "lavfi", "-i", "sine=frequency=440:duration=4",
           "-c:a", "libmp3lame", str(media / "offline-audio.mp3"))
    ffmpeg("-f", "lavfi", "-i", "color=blue:size=160x120:duration=4",
           "-f", "lavfi", "-i", "sine=frequency=660:duration=4",
           "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
           "-c:a", "aac", "-shortest", "-movflags", "+faststart",
           str(media / "offline-video.mp4"))
    ffmpeg("-i", str(media / "offline-video.mp4"), "-c", "copy", "-f", "mpegts",
           str(media / "offline-transport.mp4"))
    ffmpeg("-f", "lavfi", "-i", "color=red:size=96x96", "-frames:v", "1",
           "-threads", "1", str(media / "offline-image.png"))
    (media / "offline-vector.svg").write_text(
        '<svg xmlns="http://www.w3.org/2000/svg" width="96" height="96">'
        '<rect width="96" height="96" fill="green"/></svg>')
    (media / "offline-audio.lrc").write_text("[00:00.00]Offline audio\n[00:02.00]Second line\n")
    if args.personal_media:
        ffmpeg("-f", "lavfi", "-i", "sine=frequency=440:duration=60",
               "-c:a", "libmp3lame", str(media / "personal-audio.mp3"))
        ffmpeg("-f", "lavfi", "-i", "color=blue:size=160x120:duration=60",
               "-f", "lavfi", "-i", "sine=frequency=660:duration=60",
               "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
               "-c:a", "aac", "-shortest", "-movflags", "+faststart",
               str(media / "personal-video.mp4"))
    hashes = {path.name: digest(path) for path in sorted(media.iterdir())}
    (directory / "remote-hashes.json").write_text(json.dumps(hashes, indent=2))
    credentials = {"username": "offline-e2e", "password": secrets.token_urlsafe(24)}
    auth_path = directory / "auth.json"
    descriptor = os.open(auth_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w") as file:
        json.dump(credentials, file)

    environment = dict(os.environ, PORT=str(args.port), MEDIA_ROOT=str(directory / "media"),
                       DB_PATH=str(directory / "player.db"), SECURE_COOKIES="false",
                       LOG_LEVEL="warn")
    origin = f"http://127.0.0.1:{args.port}"
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def request(path, data=None):
        body = None if data is None else json.dumps(data).encode()
        req = urllib.request.Request(origin + path, data=body,
                                     headers={"Content-Type": "application/json"})
        with opener.open(req, timeout=5) as response:
            content = response.read()
            if not content:
                return None
            try:
                return json.loads(content)
            except json.JSONDecodeError:
                return content.decode()

    process = None
    try:
        with (directory / "server.log").open("w") as log:
            process = subprocess.Popen([str(binary)], cwd=Path(__file__).resolve().parents[3] / "player-server",
                                       env=environment, stdout=log, stderr=subprocess.STDOUT)
        deadline = time.monotonic() + 30
        while True:
            if process.poll() is not None:
                raise RuntimeError(f"Server exited. See {directory / 'server.log'}")
            try:
                request("/healthz")
                break
            except (urllib.error.URLError, TimeoutError):
                if time.monotonic() > deadline:
                    raise
                time.sleep(0.2)
        request("/api/bootstrap", credentials)
        request("/api/admin/rescan", {})
        while True:
            result = request("/api/v1/media")
            items = result if isinstance(result, list) else result.get("items", result.get("media", []))
            if len(items) == (7 if args.personal_media else 5):
                break
            if time.monotonic() > deadline + 30:
                raise RuntimeError("Fixture media scan did not finish")
            time.sleep(0.2)
        (directory / "media.json").write_text(json.dumps(items, indent=2))
        request("/api/v1/auth/logout", {})
        print(json.dumps({"fixture_directory": str(directory), "origin": origin,
                          "auth_file": str(auth_path), "pid": process.pid}), flush=True)
        print("Fixture ready. Run online phase, then Ctrl-C here before offline phase.", flush=True)
        process.wait()
        if process.returncode:
            raise RuntimeError(f"Server exited with status {process.returncode}")
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
        for name, expected in hashes.items():
            path = media / name
            if not path.is_file() or digest(path) != expected:
                raise RuntimeError(f"Remote original changed: {name}")
        print(f"Remote originals unchanged. Fixture retained at {directory}", flush=True)


if __name__ == "__main__":
    main()
