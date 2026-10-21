#!/usr/bin/env python3
"""Disposable local Player server with the synthetic music library.

The shared fixture for music end-to-end runs (task oj3, Music V01) in Google
Chrome, Mozilla Firefox and the Android app. Unlike all_formats_fixture.py it
runs detached, so one fixture can serve the three platform runners in turn
and be restarted or torn down from separate commands:

  music_fixture.py up <player-binary> [--port 18766]   create, start, print state
  music_fixture.py restart <fixture-dir>               same database, new process
  music_fixture.py status <fixture-dir>
  music_fixture.py down <fixture-dir>                  stop and delete everything

`up` creates a private (0700) temporary directory holding the media root,
the SQLite database, the server log, state.json and the credentials
(auth.json and e2e.env, both 0600). Accounts: an admin and a regular user
who may only view the `Music` set; the `Restricted` set is admin-only.
Passwords are random per fixture, so nothing from an earlier run can sign in.
Local service doubles (service_doubles.py) start next to the server unless
--no-doubles is given.

Evidence (screenshots, audio captures, results) goes to a separate
directory - $PLAYER_E2E_EVIDENCE or <tmp>/player-music-evidence/<run-id> -
which `down` keeps. Nothing is ever written inside the repository.

The Android emulator reaches the server through `adb reverse tcp:<port>
tcp:<port>` (app URL http://127.0.0.1:<port>) or the emulator host alias
http://10.0.2.2:<port>; both are recorded in state.json.
"""

import argparse
import datetime
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import tempfile
import time

from fixture_server import (FixtureError, PlayerServer, answers, bootstrap_and_scan,
                            ensure_port_free, owns_process, port_is_free, stop_pid,
                            write_private_json)
import music_library

MARKER = '.player-music-fixture'
ADMIN = 'music-admin'
USER = 'music-user'
USER_SET = 'Music'
DOUBLES_SCRIPT = Path(__file__).resolve().with_name('service_doubles.py')


def cmd_up(args):
    ensure_port_free(args.port)
    if not args.no_doubles:
        ensure_port_free(args.doubles_port)
    directory = make_directory(args.dir)
    server = PlayerServer(args.server_binary, directory, args.port)
    manifest = music_library.generate(server.media)
    auth = make_credentials(directory)
    run_id = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    state = base_state(directory, server, auth, run_id, args)
    try:
        state['server']['pid'] = server.start(detached=True)
        save_state(directory, state)
        admin = bootstrap_and_scan(server.origin, auth['admin'], manifest['media_count'])
        state['set_ids'], state['media_ids'] = setup_accounts(admin, auth['user'])
        if not args.no_doubles:
            state['doubles'] = start_doubles(directory, args.doubles_port)
        save_state(directory, state)
    except Exception:
        teardown(directory, state)
        raise
    print(json.dumps(state, indent=1, ensure_ascii=False))


def cmd_restart(args):
    """Stop and start the server on the same database and media.

    Proves that state written before the restart is persisted (and lets a
    runner check how clients behave while the server is briefly away).
    """
    state = load_state(args.fixture_dir)
    server = PlayerServer(state['server']['binary'], args.fixture_dir, state['port'])
    stop_pid(state['server']['pid'], state['server']['binary'])
    state['server']['pid'] = server.start(detached=True)
    state['server']['restarts'] = state['server'].get('restarts', 0) + 1
    save_state(args.fixture_dir, state)
    print(json.dumps({'origin': state['origin'], 'pid': state['server']['pid'],
                      'restarts': state['server']['restarts']}))


def cmd_status(args):
    state = load_state(args.fixture_dir)
    print(json.dumps({
        'origin': state['origin'], 'healthz': answers(state['origin'], '/healthz'),
        'server_running': owns_process(state['server']['pid'], state['server']['binary']),
        'doubles_running': bool(state.get('doubles')) and owns_process(
            state['doubles']['pid'], sys.executable, str(DOUBLES_SCRIPT)),
        'evidence_dir': state['evidence_dir']}))


def cmd_down(args):
    state = load_state(args.fixture_dir)
    teardown(Path(args.fixture_dir), state)
    leftovers = {
        'fixture_dir_exists': Path(args.fixture_dir).exists(),
        'port_free': port_is_free(state['port']),
        'doubles_port_free': not state.get('doubles') or port_is_free(state['doubles']['port']),
    }
    print(json.dumps(dict(leftovers, evidence_dir=state['evidence_dir'])))
    if leftovers['fixture_dir_exists'] or not leftovers['port_free'] or not leftovers['doubles_port_free']:
        raise FixtureError(f'Teardown incomplete: {leftovers}')


def make_directory(explicit):
    """Private fixture directory with a marker that `down` checks."""
    if explicit:
        directory = Path(explicit)
        directory.mkdir(mode=0o700, parents=True, exist_ok=False)
    else:
        directory = Path(tempfile.mkdtemp(prefix='player-music-'))
    directory.chmod(0o700)
    (directory / MARKER).write_text('Disposable Player music fixture; safe to delete.\n')
    return directory


def make_credentials(directory):
    """Random passwords in auth.json, plus e2e.env for android_e2e.py-style runners."""
    auth = {'admin': {'username': ADMIN, 'password': secrets.token_urlsafe(18)},
            'user': {'username': USER, 'password': secrets.token_urlsafe(18)}}
    write_private_json(directory / 'auth.json', auth)
    env = (f"E2E_ADMIN_USER={ADMIN}\nE2E_ADMIN_PASS={auth['admin']['password']}\n"
           f"E2E_USER={USER}\nE2E_USER_PASS={auth['user']['password']}\n")
    descriptor = os.open(directory / 'e2e.env', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w') as file:
        file.write(env)
    return auth


def base_state(directory, server, auth, run_id, args):
    evidence = Path(args.evidence or os.environ.get('PLAYER_E2E_EVIDENCE') or
                    Path(tempfile.gettempdir()) / 'player-music-evidence' / run_id)
    evidence.mkdir(parents=True, exist_ok=True)
    return {
        'schema': 1, 'run_id': run_id, 'fixture_dir': str(directory),
        'origin': server.origin, 'port': server.port,
        'android': {'adb_reverse_origin': server.origin,
                    'emulator_host_origin': f'http://10.0.2.2:{server.port}'},
        'server': {'binary': str(server.binary), 'pid': None,
                   'version': binary_version(server.binary),
                   'build_revision': build_revision(server.binary), 'checkout': git_revision()},
        'accounts': {'admin': ADMIN, 'user': USER, 'user_sets': [USER_SET]},
        'auth_file': str(directory / 'auth.json'), 'env_file': str(directory / 'e2e.env'),
        'manifest_file': str(directory / 'manifest.json'), 'media_root': str(server.media),
        'evidence_dir': str(evidence), 'set_ids': {}, 'media_ids': {}, 'doubles': None,
    }


def setup_accounts(admin, user):
    """Create the regular user, grant it the Music set only.

    Returns ({set name: id}, {"<set>/<path in set>": media id}).
    """
    created = admin.request('POST', '/api/v1/admin/users', dict(user, is_admin=False))
    sets = {s['name']: s['id'] for s in admin.request('GET', '/api/v1/sets')}
    admin.request('POST', '/api/v1/admin/permissions',
                  {'set_id': sets[USER_SET], 'user_id': created['id'], 'role': 'viewer'})
    names = {v: k for k, v in sets.items()}
    media = admin.request('GET', '/api/v1/media?limit=1000')
    admin.request('POST', '/api/v1/auth/logout', {})
    return sets, {f"{names[m['set_id']]}/{m['rel_path']}": m['id'] for m in media}


def start_doubles(directory, port):
    """Start service_doubles.py detached; its radio loops a generated tone."""
    radio = directory / 'radio-source.mp3'
    music_library.ffmpeg('-f', 'lavfi', '-i', music_library.tone_filter([440.0], -12, 10),
                         '-c:a', 'libmp3lame', '-b:a', '128k', str(radio))
    with (directory / 'doubles.log').open('a') as log:
        process = subprocess.Popen(
            [sys.executable, str(DOUBLES_SCRIPT), '--port', str(port), '--radio-source', str(radio)],
            stdout=log, stderr=log, stdin=subprocess.DEVNULL, start_new_session=True)
    origin = f'http://127.0.0.1:{port}'
    deadline = time.monotonic() + 15
    while answers(origin, '/_double/info') != 200:
        if process.poll() is not None or time.monotonic() > deadline:
            raise FixtureError(f'Service doubles did not start; see {directory / "doubles.log"}')
        time.sleep(0.2)
    return {'origin': origin, 'port': port, 'pid': process.pid, 'proof': 'service-double'}


def teardown(directory, state):
    """Stop our processes, then delete the fixture directory (marker checked)."""
    if state.get('server', {}).get('pid'):
        stop_pid(state['server']['pid'], state['server']['binary'])
    if state.get('doubles'):
        stop_pid(state['doubles']['pid'], sys.executable, str(DOUBLES_SCRIPT))
    directory = Path(directory)
    if (directory / MARKER).is_file():
        shutil.rmtree(directory)
    elif directory.exists():
        raise FixtureError(f'{directory} has no {MARKER}; refusing to delete it')


def save_state(directory, state):
    write_private_json(Path(directory) / 'state.json', state)


def load_state(directory):
    path = Path(directory) / 'state.json'
    if not (Path(directory) / MARKER).is_file() or not path.is_file():
        raise FixtureError(f'{directory} is not a music fixture directory')
    return json.loads(path.read_text())


def binary_version(binary):
    result = subprocess.run([str(binary), '-version'], capture_output=True, text=True, timeout=10)
    return (result.stdout or result.stderr).strip()


def build_revision(binary):
    """Commit the binary was built from (Go's embedded VCS stamp), '-dirty'
    when built from a modified tree; '' when it carries no stamp. This, not
    the checkout's HEAD, is what the runs actually tested.
    """
    try:
        info = subprocess.run(['go', 'version', '-m', str(binary)], capture_output=True,
                              text=True, timeout=30).stdout
    except (OSError, subprocess.TimeoutExpired):
        return ''
    stamp = dict(line.split('=', 1) for line in (l.split('\t')[-1] for l in info.splitlines()) if '=' in line)
    revision = stamp.get('vcs.revision', '')
    return revision + ('-dirty' if revision and stamp.get('vcs.modified') == 'true' else '')


def git_revision():
    """HEAD of this checkout, with '-dirty' when tracked files are modified."""
    root = Path(__file__).resolve().parents[3]
    head = subprocess.run(['git', '-C', str(root), 'rev-parse', 'HEAD'],
                          capture_output=True, text=True).stdout.strip()
    dirty = subprocess.run(['git', '-C', str(root), 'diff', '--quiet', 'HEAD'],
                           capture_output=True).returncode != 0
    return head + ('-dirty' if dirty else '')


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__.split('\n\n')[0])
    commands = parser.add_subparsers(dest='command', required=True)
    up = commands.add_parser('up', help='create and start a fixture')
    up.add_argument('server_binary', type=Path)
    up.add_argument('--port', type=int, default=18766)
    up.add_argument('--doubles-port', type=int, default=18767)
    up.add_argument('--no-doubles', action='store_true')
    up.add_argument('--dir', help='fixture directory to create (default: new temp dir)')
    up.add_argument('--evidence', help='evidence directory (default: see module doc)')
    for name in ('restart', 'status', 'down'):
        commands.add_parser(name).add_argument('fixture_dir', type=Path)
    return parser.parse_args(argv)


def main(argv=None):
    args = parse_args(argv)
    handlers = {'up': cmd_up, 'restart': cmd_restart, 'status': cmd_status, 'down': cmd_down}
    try:
        handlers[args.command](args)
    except FixtureError as err:
        sys.exit(f'music_fixture: {err}')


if __name__ == '__main__':
    main()
