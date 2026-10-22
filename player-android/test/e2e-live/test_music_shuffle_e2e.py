"""Checks for the M18 native driver's fresh-screen oracle; no device required."""
import subprocess
import tempfile
from pathlib import Path
import unittest
from unittest.mock import patch
import music_shuffle_e2e as shuffle
import music_accessibility_probe as accessibility


class FreshDumpTest(unittest.TestCase):
    def test_failed_idle_dump_never_reads_an_old_screen(self):
        calls = []

        def command(args, **_):
            calls.append(args)
            if args[1:3] == ['shell', 'uiautomator']:
                return subprocess.CompletedProcess(args, 0, stdout='ERROR: could not get idle state', stderr='')
            return subprocess.CompletedProcess(args, 0, stdout='', stderr='')

        with patch.object(shuffle.subprocess, 'run', command), patch.object(shuffle.time, 'sleep'):
            with self.assertRaisesRegex(AssertionError, 'fresh UI dump failed'):
                shuffle.fresh_nodes()
        self.assertFalse(any('cat' in args for args in calls))
        self.assertEqual(sum('dump' in args for args in calls), 3)
        self.assertIn('rm', calls[-1])

    def test_success_reads_only_its_unique_verified_dump(self):
        calls = []
        xml = b'<hierarchy><node text="Shuffle: On" content-desc="" class="android.widget.Button" bounds="[10,20][110,80]" clickable="true" selected="true"/></hierarchy>'

        def command(args, **_):
            calls.append(args)
            if 'dump' in args:
                return subprocess.CompletedProcess(args, 0, stdout='UI hierchary dumped to: '+args[-1], stderr='')
            return subprocess.CompletedProcess(args, 0, stdout=xml if 'cat' in args else b'', stderr=b'')

        with patch.object(shuffle.subprocess, 'run', command):
            result = shuffle.fresh_nodes()
        self.assertEqual(result[0]['text'], 'Shuffle: On')
        self.assertEqual(result[0]['b'], [10, 20, 110, 80])
        self.assertTrue(result[0]['click'])
        self.assertEqual(result[0]['sel'], 'true')
        self.assertTrue(calls[0][-1].startswith('/sdcard/zk3-'))
        self.assertEqual(calls[0][-1], calls[1][-1])
        self.assertEqual(calls[0][-1], calls[2][-1])


class NativeHintTest(unittest.TestCase):
    def test_exhausted_empty_native_tree_is_never_accepted(self):
        response=subprocess.CompletedProcess([],0,stdout='{"nodes":[],"freshRootPolls":70}',stderr='')
        with tempfile.TemporaryDirectory() as directory, patch.object(accessibility.subprocess,'run',return_value=response):
            with self.assertRaisesRegex(RuntimeError,'bounded root polling'):
                accessibility.snapshot(Path(directory)/'probe.json')

    def test_requires_real_hint_editable_visible_enabled_and_unique(self):
        node={'hint':'Genre (optional)','text':'null','description':'null',
              'editable':True,'visible':True,'enabled':True,'bounds':[10,20,110,80]}
        self.assertEqual(accessibility.editable_field([node],'Genre (optional)'),node)
        for values in [[],[node,node],[{**node,'hint':'Other'}],[{**node,'editable':False}],
                       [{**node,'enabled':False}],[{**node,'visible':False}],[{**node,'bounds':[0,0,0,0]}]]:
            with self.assertRaises(AssertionError):
                accessibility.editable_field(values,'Genre (optional)')


if __name__ == '__main__':
    unittest.main()
