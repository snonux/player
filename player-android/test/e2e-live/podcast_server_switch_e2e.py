#!/usr/bin/env python3
"""Installed-app podcast scope check across two saved server URLs.

Use the isolated podcast fixture with Android A signed in as admin and Alpha
subscribed. The secondary URL is a second *device* port reversed to the same
isolated server. Keeping the account identical isolates the URL part of the
scope; the primary port is routed through a failing podcast-sync proxy while
the secondary port reaches the server normally. No second server is needed.

From player-android/ after the parent has booted A and mapped fixture ports:

    python3 -u test/e2e-live/podcast_server_switch_e2e.py FIXTURE \
        --serial emulator-5554 --client A --secondary-device-port 18767

The stage uses normal Settings, Sign In and Switch server controls. It saves
sanitized snapshots under the fixture evidence directory. It always restores
its temporary reverse mapping; on success it also returns to the primary
selection and flushes its test edit.
"""

import argparse
from contextlib import contextmanager
import json
from pathlib import Path
import re
import time
from types import SimpleNamespace
from urllib.parse import urlsplit, urlunsplit

import adrv
import app_ui
from podcast_sync_e2e import Run, checked_adb


def secondary_origin(primary, port):
    parsed = urlsplit(primary)
    if parsed.scheme not in ('http', 'https') or not parsed.hostname:
        raise ValueError('Fixture origin must be an HTTP server URL')
    if not 1 <= port <= 65535 or port == parsed.port:
        raise ValueError('Secondary device port must differ from primary port')
    if parsed.username or parsed.password or parsed.path not in ('', '/') or parsed.query or parsed.fragment:
        raise ValueError('Fixture origin must not contain credentials or a path')
    host = f'[{parsed.hostname}]' if ':' in parsed.hostname else parsed.hostname
    return urlunsplit((parsed.scheme, f'{host}:{port}', '', '', ''))


def account_scope(origin, user_id):
    return f'account:{origin}#{user_id}'


def completion_row(snapshot, scope, episode_key):
    rows = [row for row in snapshot['podcast_episode_state']
            if row['scope'] == scope and row['episode_key'] == episode_key]
    if len(rows) > 1:
        raise AssertionError('Duplicate episode state in one server scope')
    return rows[0] if rows else None


def assert_pending_isolated(snapshot, primary_scope, secondary_scope,
                            episode_key, edit_id):
    primary = completion_row(snapshot, primary_scope, episode_key)
    secondary = completion_row(snapshot, secondary_scope, episode_key)
    if not primary or primary['completion_pending_edit_id'] != edit_id:
        raise AssertionError('Primary server pending edit was lost or rewritten')
    if secondary and secondary['completion_pending_edit_id']:
        raise AssertionError('Primary pending edit leaked into the other server scope')


def library_action_ready(labels, action):
    """A cold relaunch can stop on Sign In instead of the Library app bar."""
    return 'Open navigation menu' in labels and action in labels


def saved_server_target(nodes, origin):
    """Require an enabled option; the currently selected URL is disabled."""
    matches = [node for node in nodes
               if adrv.label(node).startswith(origin) and node['click']]
    if not matches:
        raise AssertionError('Saved server target is missing or already selected')
    return matches[0]


def reverse_target(mapping):
    for line in checked_adb('reverse', '--list').splitlines():
        fields = line.split()
        if len(fields) >= 3 and fields[-2] == mapping:
            return fields[-1]
    return None


@contextmanager
def secondary_reverse(device_port, host_port):
    mapping = f'tcp:{device_port}'
    original = reverse_target(mapping)
    try:
        checked_adb('reverse', mapping, f'tcp:{host_port}')
        yield
    finally:
        if original is None:
            checked_adb('reverse', '--remove', mapping)
        else:
            checked_adb('reverse', mapping, original)


class ServerSwitchRun(Run):
    def snapshot_for(self, name, scope):
        previous = self.scope
        try:
            self.scope = scope
            return self.snapshot(name)
        finally:
            self.scope = previous

    def open_library_action(self, action):
        # snapshot() force-stops the app. A fresh launch may show the saved
        # server chooser and then Sign In, even though the previous screen was
        # the episode list. Run.root() accepts Sign In as a root, so complete
        # authentication before looking for Library app-bar actions.
        self.login('admin')
        if not library_action_ready(app_ui.labels(), action):
            if not app_ui.go_home():
                raise AssertionError('Native app did not return to Library')
        if not library_action_ready(app_ui.labels(), action):
            raise AssertionError(f'Library action {action!r} is not visible')
        self.touch('^' + re.escape(action) + '$')

    def server_url_from_settings(self, origin):
        self.open_library_action('Settings')
        for _ in range(35):
            if adrv.find('^Save URL$'):
                break
            app_ui.swipe()
        else:
            raise AssertionError('Could not reach the native server address control')
        self.field('Server base URL', origin)
        self.touch('^Save URL$')
        if not adrv.wait('^Welcome back$', timeout=30):
            raise AssertionError('Native server selection did not lock the previous session')
        if not adrv.wait('^Sign in to ' + re.escape(origin) + '$', timeout=10):
            raise AssertionError('Native sign-in did not select the secondary URL')

    def choose_saved_server(self, origin):
        self.open_library_action('Switch server')
        target = saved_server_target(adrv.nodes(), origin)
        self.shot('saved-server-target')
        adrv.tap_node(target)
        if adrv.wait('^Welcome back$', timeout=10):
            if not adrv.wait('^Sign in to ' + re.escape(origin) + '$', timeout=10):
                raise AssertionError('Saved server switch selected the wrong URL')
            return
        # A saved login can reopen after biometric unlock instead of leaving
        # the sign-in page visible. Run.login() accepts that signed-in root.
        if not adrv.wait('^Open navigation menu$', timeout=20):
            raise AssertionError('Saved server switch reached neither sign-in nor library')

    def wait_primary_pending(self, episode_key, primary_scope, timeout=20):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            rows = adrv.nodes()
            if any('pending' in adrv.label(row).lower() for row in rows):
                break
            time.sleep(.5)
        else:
            raise AssertionError('Native primary footer did not disclose its pending edit')
        snapshot = self.snapshot_for('primary-pending', primary_scope)
        row = completion_row(snapshot, primary_scope, episode_key)
        edit_id = row and row['completion_pending_edit_id']
        if not edit_id:
            raise AssertionError('Native primary completion edit was not durable')
        return edit_id

    def run_server_switch(self, device_port):
        primary_origin = self.state['origin']
        secondary = secondary_origin(primary_origin, device_port)
        user_id = self.state['accounts']['admin']
        primary_scope = account_scope(primary_origin, user_id)
        secondary_scope = account_scope(secondary, user_id)
        episode = self.state['feeds'][0]['episodes'][4]

        # Alpha 005 is short, on the first few combined pages, and not needed
        # for the main A/B resume and natural-end journey.
        before = self.snapshot_for('before-server-switch', primary_scope)
        original = completion_row(before, primary_scope, episode['key'])
        action = 'Mark uncompleted' if original and original['completed'] else 'Mark completed'

        with secondary_reverse(device_port, self.state['port']):
            with self.proxy(fail_podcasts=True):
                self.episodes(filter_name='All')
                self.row(action, title=episode['title'])
                edit_id = self.wait_primary_pending(episode['key'], primary_scope)
                self.server_url_from_settings(secondary)
                self.login('admin')
                if adrv.find(r"^Sync this device's podcasts with "):
                    self.touch('^Keep separate$')
                self.podcasts()
                self.touch(r'^(Sync now|Retry)$')
                if not adrv.wait('^Synced$', timeout=30):
                    raise AssertionError('Secondary server did not report its own full sync')

                # Exercise the saved-server menu before taking another DB
                # snapshot. snapshot() force-stops the app; its cold chooser
                # defaults to the fixture's primary URL on relaunch.
                self.choose_saved_server(primary_origin)
                self.login('admin')
                if adrv.find(r"^Sync this device's podcasts with "):
                    self.touch('^Keep separate$')
                self.podcasts()
                if not any('pending' in adrv.label(node).lower()
                           for node in adrv.nodes()):
                    raise AssertionError('Primary footer lost its pending status on return')
                second = self.snapshot_for('secondary-synced', secondary_scope)
                assert_pending_isolated(second, primary_scope, secondary_scope,
                                        episode['key'], edit_id)
                scopes = {row['scope']: row for row in second['podcast_scopes']}
                if not (scopes.get(secondary_scope) or {}).get('last_full_success_ms'):
                    raise AssertionError('Secondary server has no independent full-sync timestamp')
                assert_pending_isolated(
                    self.snapshot_for('primary-restored', primary_scope),
                    primary_scope, secondary_scope, episode['key'], edit_id)

        # After restoring the original network mapping, deliver the pending
        # test edit so this stage does not leave fixture sync work behind.
        self.login('admin')
        self.podcasts()
        self.touch(r'^(Sync now|Retry)$')
        if not adrv.wait('^Synced$', timeout=30):
            raise AssertionError('Primary pending edit did not sync after return')
        finished = self.snapshot_for('primary-flushed', primary_scope)
        row = completion_row(finished, primary_scope, episode['key'])
        if not row or row['completion_pending_edit_id']:
            raise AssertionError('Primary test edit remained pending after reconnect')
        self.record('saved server selection isolates podcast pending edits and sync status',
                    primary_origin=primary_origin, secondary_origin=secondary)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fixture', type=Path)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--client', choices=['A', 'B'], default='A')
    parser.add_argument('--secondary-device-port', type=int, default=18767)
    args = parser.parse_args()
    run = ServerSwitchRun(SimpleNamespace(
        fixture=args.fixture, serial=args.serial, client=args.client,
        stage='server-switch', emulator_wav=None))
    try:
        run.run_server_switch(args.secondary_device_port)
    except Exception as error:
        run.results.append({'name': 'server-switch', 'result': 'fail', 'detail': str(error)})
        raise
    finally:
        (run.output / 'results.json').write_text(json.dumps(run.results, indent=1))


if __name__ == '__main__':
    main()
