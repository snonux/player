#!/usr/bin/env python3
"""Run native podcast acceptance, verify browser intents, and return to Player."""
import argparse
import queue
import subprocess
import threading
import time
from pathlib import Path
from urllib.parse import urlsplit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', default='emulator-5554')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]

    def adb(*command):
        return subprocess.run(['adb', '-s', args.device, *command],
                              check=True, capture_output=True, text=True).stdout

    def browser_log():
        return adb('logcat', '-d', '-v', 'threadtime', '-s', 'ActivityTaskManager:I').splitlines()

    baseline = set(browser_log())
    process = subprocess.Popen([
        'flutter', 'test', '--no-pub', 'integration_test/offline_podcast_resume_test.dart',
        '-d', args.device,
    ], cwd=root, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    lines = queue.Queue()

    def read_output():
        for line in process.stdout:
            print(line, end='', flush=True)
            lines.put(line)

    reader = threading.Thread(target=read_output, daemon=True)
    reader.start()
    verified = set()
    deadline = time.monotonic() + 600
    try:
        while process.poll() is None or not lines.empty():
            if time.monotonic() > deadline:
                raise RuntimeError('Timed out waiting for native podcast acceptance')
            try:
                line = lines.get(timeout=.5)
            except queue.Empty:
                continue
            if 'PODCAST_BROWSER_CHECK ' not in line:
                continue
            url = line.split('PODCAST_BROWSER_CHECK ', 1)[1].strip()
            parsed = urlsplit(url)
            origin = f'{parsed.scheme}://{parsed.netloc}/'
            browser_deadline = time.monotonic() + 20
            match = None
            while time.monotonic() < browser_deadline:
                match = next((entry for entry in browser_log() if entry not in baseline
                              and 'act=android.intent.action.VIEW' in entry
                              and 'dat=' + origin in entry and 'cmp=com.android.chrome/' in entry), None)
                if match:
                    break
                time.sleep(.2)
            if not match:
                raise RuntimeError(f'No Chrome browser intent received for {url}')
            print('Browser intent verified:', match, flush=True)
            verified.add(parsed.scheme)
            time.sleep(1)
            adb('shell', 'input', 'keyevent', '4')
        reader.join(timeout=2)
        if process.returncode or verified != {'http', 'https'}:
            raise RuntimeError(f'Native test exit={process.returncode}; browser schemes={verified}')
    finally:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


if __name__ == '__main__':
    main()
