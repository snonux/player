"""Lifecycle of a disposable local Player server for end-to-end fixtures.

Shared by all_formats_fixture.py and music_fixture.py so that both start,
probe, bootstrap and stop the real server the same way. Everything stays on
loopback: the server is started with its own MEDIA_ROOT and DB_PATH inside a
private temporary directory and never touches a user's library or database.

Only processes started from here are ever signalled. A process is recognised
by its PID *and* its executable path, so a recycled PID that now belongs to an
unrelated program is never killed.
"""

import http.cookiejar
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import time
import urllib.error
import urllib.request

# Generous enough for a first start that creates the database schema on a
# slow laptop, short enough that a broken binary fails the run quickly.
HEALTH_TIMEOUT = 60


class FixtureError(RuntimeError):
    """A fixture step failed; the message says which step and where to look."""


class Client:
    """Cookie-holding JSON client for one Player account (or anonymous)."""

    def __init__(self, origin):
        self.origin = origin.rstrip('/')
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(self, method, path, data=None, timeout=10):
        """Return (status, decoded body); HTTP errors are returned, not raised.

        Callers that only care about success use request() instead. Returning
        the status lets negative checks (401, 403) assert the exact refusal.
        """
        body = None if data is None else json.dumps(data).encode()
        req = urllib.request.Request(self.origin + path, data=body, method=method,
                                     headers={'Content-Type': 'application/json'})
        try:
            with self.opener.open(req, timeout=timeout) as response:
                return response.status, _decode(response)
        except urllib.error.HTTPError as err:
            with err:
                return err.code, _decode(err)

    def request(self, method, path, data=None, timeout=10):
        """Like call(), but any non-2xx status raises FixtureError."""
        status, body = self.call(method, path, data, timeout)
        if not 200 <= status < 300:
            raise FixtureError(f'{method} {path} returned HTTP {status}: {body!r:.200}')
        return body

    def login(self, username, password):
        self.request('POST', '/api/v1/auth/login',
                     {'username': username, 'password': password})
        return self


class PlayerServer:
    """One server process bound to a fixture directory.

    The directory layout is fixed so that a later process (restart, down) can
    find the database, media root and log from the directory alone.
    """

    def __init__(self, binary, directory, port):
        self.binary = Path(binary).resolve(strict=True)
        self.directory = Path(directory)
        self.port = port
        self.origin = f'http://127.0.0.1:{port}'
        self.media = self.directory / 'media'
        self.database = self.directory / 'player.db'
        self.log = self.directory / 'server.log'
        self.process = None

    def environment(self):
        """Server settings: private paths, plain-HTTP cookies, quiet log."""
        return dict(os.environ, PORT=str(self.port), MEDIA_ROOT=str(self.media),
                    DB_PATH=str(self.database), SECURE_COOKIES='false',
                    LOG_LEVEL='warn')

    def start(self, detached=False):
        """Start the server and wait until /healthz answers.

        A detached server runs in its own session so that it survives the
        command that started it (music_fixture.py up); stop_pid() ends it.
        The working directory is player-server/ because the binary resolves
        its web assets relative to it.
        """
        ensure_port_free(self.port)
        with self.log.open('a') as log:
            self.process = subprocess.Popen(
                [str(self.binary)], cwd=server_directory(), env=self.environment(),
                stdout=log, stderr=log, stdin=subprocess.DEVNULL,
                start_new_session=detached)
        self.wait_healthy()
        return self.process.pid

    def wait_healthy(self, timeout=HEALTH_TIMEOUT):
        deadline = time.monotonic() + timeout
        anonymous = Client(self.origin)
        while True:
            if self.process is not None and self.process.poll() is not None:
                raise FixtureError(f'Server exited; see {self.log}')
            try:
                anonymous.request('GET', '/healthz', timeout=2)
                return
            except (FixtureError, urllib.error.URLError, OSError):
                if time.monotonic() > deadline:
                    raise FixtureError(f'Server not healthy after {timeout}s; see {self.log}')
                time.sleep(0.2)

    def stop(self):
        """Stop the process this object started (foreground fixtures)."""
        if self.process is None or self.process.poll() is not None:
            return
        self.process.send_signal(signal.SIGTERM)
        try:
            self.process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait()


def server_directory():
    """player-server/ in this monorepo checkout."""
    return Path(__file__).resolve().parents[3] / 'player-server'


def ensure_port_free(port):
    """Refuse an occupied port instead of bootstrapping some other instance."""
    with socket.socket() as probe:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        try:
            probe.bind(('127.0.0.1', port))
        except OSError as err:
            raise FixtureError(f'Port {port} is already in use') from err


def port_is_free(port):
    try:
        ensure_port_free(port)
        return True
    except FixtureError:
        return False


def owns_process(pid, binary, argument=None):
    """True when pid is alive, runs exactly this binary and (for interpreters
    such as python3) has argument, e.g. the script path, on its command line.
    """
    try:
        if Path(f'/proc/{pid}/exe').resolve() != Path(binary).resolve():
            return False
        if argument is None:
            return True
        return argument in Path(f'/proc/{pid}/cmdline').read_bytes().decode(errors='replace').split('\0')
    except (OSError, ValueError):
        return False


def answers(origin, path, timeout=2):
    """HTTP status of GET origin+path, or None when nothing is listening."""
    try:
        return Client(origin).call('GET', path, timeout=timeout)[0]
    except (urllib.error.URLError, OSError):
        return None


def stop_pid(pid, binary, argument=None, timeout=10):
    """SIGTERM a detached process started by a fixture, then wait for exit.

    Returns False without signalling when the PID no longer belongs to the
    recorded program (already gone or recycled by another program).
    """
    if not owns_process(pid, binary, argument):
        return False
    os.kill(pid, signal.SIGTERM)
    deadline = time.monotonic() + timeout
    while owns_process(pid, binary, argument):
        if time.monotonic() > deadline:
            os.kill(pid, signal.SIGKILL)
            break
        time.sleep(0.1)
    return True


def bootstrap_and_scan(origin, admin, expected_media, timeout=HEALTH_TIMEOUT):
    """Create the first admin, rescan, and wait for expected_media items.

    Returns the admin client, still signed in, for further setup.
    """
    client = Client(origin)
    client.request('POST', '/api/bootstrap', admin)
    client.login(admin['username'], admin['password'])
    client.request('POST', '/api/admin/rescan', {})
    deadline = time.monotonic() + timeout
    while len(client.request('GET', '/api/v1/media?limit=1000') or []) != expected_media:
        if time.monotonic() > deadline:
            raise FixtureError(f'Fixture scan did not reach {expected_media} media items')
        time.sleep(0.2)
    return client


def write_private_json(path, data):
    """Write JSON readable only by the current user (credentials inside)."""
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, 'w') as file:
        json.dump(data, file, indent=1, ensure_ascii=False)


def _decode(response):
    content = response.read()
    if not content:
        return None
    if 'application/json' in (response.headers.get('Content-Type') or ''):
        return json.loads(content)
    return content.decode(errors='replace')
