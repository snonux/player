#!/usr/bin/env python3
"""Build/read the genuine Android MediaController metadata on a task emulator."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import adrv
import app_ui

REMOTE_JAR = '/data/local/tmp/qk3-media-session.jar'


def build(directory):
    directory = Path(directory).resolve()
    directory.mkdir(parents=True, exist_ok=True)
    source = Path(__file__).with_name('MediaSessionProbe.java').resolve()
    sdk = Path(os.environ.get('ANDROID_HOME', '/home/paul/Android/Sdk'))
    platform = sdk / 'platforms/android-34/android.jar'
    d8 = sdk / 'build-tools/34.0.0/d8'
    classes, dex = directory / 'classes', directory / 'dex'
    classes.mkdir(exist_ok=True); dex.mkdir(exist_ok=True)
    commands = [
        ['javac', '-source', '8', '-target', '8', '-cp', str(platform), '-d', str(classes), str(source)],
        [str(d8), '--lib', str(platform), '--output', str(dex), str(classes / 'MediaSessionProbe.class')],
        ['jar', 'cf', str(directory / 'media-session-probe.jar'), '-C', str(dex), 'classes.dex'],
    ]
    (directory / 'compile-commands.json').write_text(json.dumps(commands, indent=2))
    for command in commands:
        subprocess.run(command, check=True)
    return directory / 'media-session-probe.jar'


def install(jar):
    assert 'uid=0' in adrv.adb('shell', 'id'), 'Disposable rooted emulator required'
    adrv.adb('push', str(Path(jar).resolve(strict=True)), REMOTE_JAR)


def snapshot(art_path):
    remote = '/data/local/tmp/qk3-session-art.png'
    adrv.adb('shell', 'rm', '-f', remote)
    command = ['adb', 'shell', f'CLASSPATH={REMOTE_JAR}', 'app_process', '/system/bin',
               'MediaSessionProbe', app_ui.PKG, remote]
    result = subprocess.run(command, capture_output=True, text=True)
    Path(str(art_path) + '.probe.json').write_text(json.dumps({
        'command': command, 'returncode': result.returncode,
        'stdout': result.stdout, 'stderr': result.stderr}, indent=2))
    lines = [line for line in result.stdout.splitlines() if line.startswith('{')]
    if result.returncode or not lines:
        raise RuntimeError(('Actual OS probe failed', result.returncode, result.stdout, result.stderr))
    state = json.loads(lines[0])
    if state.get('artwork'):
        adrv.adb('pull', remote, str(art_path))
        state['art_path'] = str(art_path)
    return state


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('output')
    args = parser.parse_args()
    print(build(args.output))
