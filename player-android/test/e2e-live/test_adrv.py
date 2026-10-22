"""Shared native UI freshness/selector regressions; no device required."""
import io
import subprocess
import unittest
from unittest.mock import patch

import adrv


XML = (b'<hierarchy rotation="0"><node text="Pause &amp; resume" '
       b'content-desc="Pause" class="android.widget.Button" '
       b'bounds="[10,20][110,80]" clickable="true" checked="false" '
       b'selected="true" focused="false"/></hierarchy>')


class Device:
    """Keep the historical fixed-path dump even when a new dump fails."""
    def __init__(self, *, dump_exit=0, dump_message=None, xml=XML,
                 dump_stderr=b'dump diagnostic', read_exit=0, cleanup_exit=0, timeout=None):
        self.files = {'/sdcard/ui.xml': XML}
        self.calls = []
        self.dump_exit = dump_exit
        self.dump_message = dump_message
        self.dump_stderr = dump_stderr
        self.xml = xml
        self.read_exit = read_exit
        self.cleanup_exit = cleanup_exit
        self.timeout = timeout

    def run(self, args, **kwargs):
        self.calls.append(args)
        assert kwargs['capture_output'] and kwargs['timeout'] == adrv.ADB_TIMEOUT
        operation = 'dump' if 'dump' in args else 'cat' if 'cat' in args else 'rm'
        if operation == self.timeout:
            raise subprocess.TimeoutExpired(args, adrv.ADB_TIMEOUT,
                                            output=b'partial output', stderr=b'timeout detail')
        path = args[-1]
        if operation == 'dump':
            message = self.dump_message
            if message is None:
                message = f'UI hierchary dumped to: {path}\n'.encode()
                if self.xml is not None:
                    self.files[path] = self.xml
            return subprocess.CompletedProcess(args, self.dump_exit, message, self.dump_stderr)
        if operation == 'cat':
            return subprocess.CompletedProcess(args, self.read_exit or (path not in self.files),
                                                self.files.get(path, b''), b'read diagnostic')
        if not self.cleanup_exit:
            self.files.pop(path, None)
        return subprocess.CompletedProcess(args, self.cleanup_exit, b'', b'cleanup diagnostic')


class FreshNodesTest(unittest.TestCase):
    def test_failed_idle_dump_never_finds_or_taps_existing_stale_pause(self):
        for action in (lambda: adrv.find('^Pause$'), lambda: adrv.wait('^Pause$'),
                       lambda: adrv.tap('^Pause$')):
            with self.subTest(action=action):
                device = Device(dump_message=b'ERROR: could not get idle state.\n')
                with patch.object(adrv.subprocess, 'run', device.run):
                    with self.assertRaisesRegex(RuntimeError, 'could not get idle state'):
                        action()
                self.assertEqual([call[1:3] for call in device.calls],
                                 [['shell', 'uiautomator'], ['shell', 'rm']])
                self.assertEqual(device.files, {'/sdcard/ui.xml': XML})

    def test_success_keeps_node_fields_and_reads_only_each_unique_path(self):
        device = Device()
        with patch.object(adrv.subprocess, 'run', device.run):
            node = adrv.find('^Pause$', cls='Button')
            self.assertIsNone(adrv.find('^Missing$'))
        self.assertEqual(node, {'text': 'Pause & resume', 'desc': 'Pause', 'cls': 'Button',
                               'b': [10, 20, 110, 80], 'click': True, 'checked': 'false',
                               'sel': 'true', 'focused': 'false'})
        paths = [call[-1] for call in device.calls]
        self.assertEqual(paths[0:3], [paths[0]] * 3)
        self.assertEqual(paths[3:6], [paths[3]] * 3)
        self.assertNotEqual(paths[0], paths[3])
        self.assertTrue(all(path.startswith('/sdcard/adrv-') for path in paths))
        self.assertEqual(device.files, {'/sdcard/ui.xml': XML})

    def test_failed_or_unconfirmed_dump_cannot_read_a_screen(self):
        cases = [Device(dump_exit=1), Device(dump_message=b''),
                 Device(dump_stderr=b'ERROR: write failed'),
                 Device(dump_message=b'UI hierchary dumped to: /sdcard/ui.xml\n'),
                 Device(dump_message=b'ERROR: failed\nUI hierchary dumped to: /sdcard/ui.xml\n')]
        for device in cases:
            with self.subTest(device=device), patch.object(adrv.subprocess, 'run', device.run):
                with self.assertRaisesRegex(RuntimeError, 'exit=.*stdout=.*stderr='):
                    adrv.nodes()
                self.assertFalse(any('cat' in call for call in device.calls))
                self.assertEqual(device.calls[-1][1:4], ['shell', 'rm', '-f'])

    def test_missing_or_failed_read_preserves_read_diagnostics(self):
        for device in (Device(xml=None), Device(read_exit=7)):
            with self.subTest(device=device), patch.object(adrv.subprocess, 'run', device.run):
                with self.assertRaisesRegex(RuntimeError, "UI command failed: .*exec-out.*exit=.*read diagnostic"):
                    adrv.nodes()
                self.assertIn('rm', device.calls[-1])

    def test_invalid_xml_or_node_is_rejected_instead_of_partially_parsed(self):
        for xml in (b'', XML[:-12], b'<wrong/>', XML.replace(b'[10,20][110,80]', b'[10,20][110]'),
                    XML.replace(b'focused="false"', b'')):
            with self.subTest(xml=xml):
                device = Device(xml=xml)
                with patch.object(adrv.subprocess, 'run', device.run):
                    with self.assertRaisesRegex(RuntimeError, 'Invalid fresh UI dump: .*stdout=.*stderr='):
                        adrv.nodes()
                self.assertIn('rm', device.calls[-1])

    def test_complete_empty_hierarchy_and_signed_zero_area_bounds_are_valid(self):
        for xml, expected in ((b'<hierarchy/>', []),
                              (XML.replace(b'[10,20][110,80]', b'[-10,+20][-10,20]'), [-10, 20, -10, 20])):
            device = Device(xml=xml)
            with self.subTest(xml=xml), patch.object(adrv.subprocess, 'run', device.run):
                result = adrv.nodes()
                self.assertEqual(result[0]['b'] if result else [], expected)

    def test_timeout_preserves_partial_output_and_still_cleans_owned_path(self):
        for operation in ('dump', 'cat'):
            device = Device(timeout=operation)
            with self.subTest(operation=operation), patch.object(adrv.subprocess, 'run', device.run):
                with self.assertRaisesRegex(RuntimeError, 'timed out: .*partial output.*timeout detail'):
                    adrv.nodes()
            self.assertIn('rm', device.calls[-1])

    def test_cleanup_failure_is_reported_and_does_not_mask_original_failure(self):
        device = Device(dump_message=b'ERROR: could not get idle state.', cleanup_exit=9)
        stderr = io.StringIO()
        with patch.object(adrv.subprocess, 'run', device.run), patch.object(adrv.sys, 'stderr', stderr):
            with self.assertRaisesRegex(RuntimeError, 'Fresh UI dump failed: .*could not get idle state'):
                adrv.nodes()
        self.assertIn('cleanup also failed', stderr.getvalue())
        self.assertIn('exit=9', stderr.getvalue())
        self.assertIn('cleanup diagnostic', stderr.getvalue())

    def test_cleanup_failure_after_success_is_a_failure(self):
        device = Device(cleanup_exit=9)
        with patch.object(adrv.subprocess, 'run', device.run):
            with self.assertRaisesRegex(RuntimeError, 'UI command failed: .*exit=9'):
                adrv.nodes()

    def test_unavailable_adb_preserves_launch_and_cleanup_diagnostics(self):
        stderr = io.StringIO()
        with patch.object(adrv.subprocess, 'run', side_effect=FileNotFoundError('adb missing')), \
                patch.object(adrv.sys, 'stderr', stderr):
            with self.assertRaisesRegex(RuntimeError, 'UI command could not run: .*uiautomator.*adb missing'):
                adrv.nodes()
        self.assertIn('cleanup also failed', stderr.getvalue())
        self.assertIn('adb missing', stderr.getvalue())


if __name__ == '__main__':
    unittest.main()
