#!/usr/bin/env python3
"""Check or bump the shared server/Android release version."""

import argparse
from pathlib import Path
import re


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", nargs="?", help="new X.Y.Z version")
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    if args.check == bool(args.version):
        parser.error("provide either --check or a new version")

    root = Path(__file__).resolve().parents[2]
    android = root / "player-android/pubspec.yaml"
    server = root / "player-server/internal/version.go"
    android_text, server_text = android.read_text(), server.read_text()
    semver = r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
    android_matches = list(re.finditer(
        rf"^version: ({semver})\+([1-9][0-9]*)$", android_text, re.MULTILINE
    ))
    server_matches = list(re.finditer(
        rf'^const Version = "({semver})"$', server_text, re.MULTILINE
    ))
    if len(android_matches) != 1 or len(server_matches) != 1:
        parser.error("expected exactly one version declaration per component")
    app, go = android_matches[0], server_matches[0]
    if app.group(1) != go.group(1):
        parser.error("server and Android versions must match")
    if args.check:
        print(app.group(1))
        return
    if not re.fullmatch(semver, args.version):
        parser.error("version must be X.Y.Z, without v or build number")
    if tuple(map(int, args.version.split('.'))) <= tuple(
        map(int, app.group(1).split('.'))
    ):
        parser.error("new version must be greater than the current version")
    build = int(app.group(2)) + 1
    android.write_text(android_text[:app.start()] +
                       f"version: {args.version}+{build}" +
                       android_text[app.end():])
    server.write_text(server_text[:go.start()] +
                      f'const Version = "{args.version}"' +
                      server_text[go.end():])
    print(f"Server {args.version}; Android {args.version}+{build}")


if __name__ == "__main__":
    main()
