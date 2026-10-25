"""Device-free M08 scenario dispatch and sheet gesture-region checks."""
import json
from pathlib import Path
from tempfile import TemporaryDirectory
import unittest
from unittest.mock import Mock, patch
import music_facets_e2e as facets
from music_facets_e2e import sheet_region


def node(text, bounds, click=False):
    return {'text': text, 'desc': '', 'cls': 'Button' if click else 'TextView',
            'b': bounds, 'click': click}


class SheetRegionTest(unittest.TestCase):
    def test_bounds_follow_real_sheet_controls(self):
        self.assertEqual(sheet_region([node('Music filters', [20, 300, 400, 350]),
                                       node('Apply', [600, 1100, 750, 1150], True)]),
                         (375, 358, 1092))

    def test_missing_or_ambiguous_controls_fail_closed(self):
        title = node('Music filters', [20, 300, 400, 350])
        apply = node('Apply', [600, 1100, 750, 1150], True)
        for nodes in ([title], [title, apply, apply]):
            with self.assertRaises(AssertionError):
                sheet_region(nodes)

    def test_collapsed_viewport_is_rejected(self):
        with self.assertRaises(AssertionError):
            sheet_region([node('Music filters', [20, 300, 400, 350]),
                          node('Apply', [400, 355, 600, 405], True)])


class ScenarioDispatchTest(unittest.TestCase):
    def test_state_and_default_dispatch_call_scenarios_without_device_io(self):
        for scenario, expected in [(None, ['core', 'state', 'errors']),
                                   ('state', ['state'])]:
            with self.subTest(scenario=scenario), TemporaryDirectory() as directory:
                (Path(directory) / 'state.json').write_text(json.dumps({'port': 18816}))
                run = Mock(failures=0, base='unused', auth={'user': {'username': 'u', 'password': 'p'}})
                calls = []
                with patch.object(facets.baseline, 'prepare_device'), \
                     patch.object(facets.baseline, 'teardown') as teardown, \
                     patch.object(facets, 'Run', return_value=run), \
                     patch.object(facets.app_ui, 'connect_and_login', return_value=True), \
                     patch.object(facets, 'core', side_effect=lambda _: calls.append('core')), \
                     patch.object(facets, 'state', side_effect=lambda _: calls.append('state')), \
                     patch.object(facets, 'errors', side_effect=lambda _: calls.append('errors')):
                    args = [directory, '--emulator-wav', 'unused.wav']
                    if scenario:
                        args += ['--scenario', scenario]
                    self.assertEqual(facets.main(args), 0)
                    self.assertEqual(calls, expected)
                    run.record.assert_not_called()
                    teardown.assert_called_once_with(run, 18816)


if __name__ == '__main__':
    unittest.main()
