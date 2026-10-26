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
    def test_preserve_device_skips_install_login_and_resets_on_success_and_failure(self):
        for fail in [False, True]:
            with self.subTest(fail=fail), TemporaryDirectory() as directory:
                (Path(directory) / 'state.json').write_text(json.dumps({'port': 18816}))
                run = Mock(failures=0)
                def record(*args, **kwargs):
                    run.failures += 1
                run.record.side_effect = record
                with patch.object(facets.baseline, 'prepare_device') as prepare, \
                     patch.object(facets.baseline, 'teardown') as teardown, \
                     patch.object(facets, 'Run', return_value=run), \
                     patch.object(facets.app_ui, 'connect_and_login') as login, \
                     patch.object(facets.app_ui, 'launch') as launch, \
                     patch.object(facets.app_ui, 'go_home', return_value=True), \
                     patch.object(facets, 'extended', side_effect=AssertionError('journey failed') if fail else None) as step:
                    self.assertEqual(facets.main([directory, '--emulator-wav', 'unused.wav',
                                                 '--scenario', 'extended', '--preserve-device']), int(fail))
                    launch.assert_called_once_with()
                    step.assert_called_once_with(run)
                    prepare.assert_not_called()
                    login.assert_not_called()
                    teardown.assert_not_called()

    def test_state_and_default_dispatch_call_scenarios_without_device_io(self):
        for scenario, expected in [(None, ['core', 'state', 'errors', 'detail', 'extended', 'navigation']),
                                   ('state', ['state']), ('detail', ['detail']), ('label', ['label']),
                                   ('extended', ['extended']), ('navigation', ['navigation'])]:
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
                     patch.object(facets, 'errors', side_effect=lambda _: calls.append('errors')), \
                     patch.object(facets, 'detail', side_effect=lambda _: calls.append('detail')), \
                     patch.object(facets, 'label', side_effect=lambda _: calls.append('label')), \
                     patch.object(facets, 'extended', side_effect=lambda _: calls.append('extended')), \
                     patch.object(facets, 'navigation', side_effect=lambda _: calls.append('navigation')):
                    args = [directory, '--emulator-wav', 'unused.wav']
                    if scenario:
                        args += ['--scenario', scenario]
                    self.assertEqual(facets.main(args), 0)
                    self.assertEqual(calls, expected)
                    run.record.assert_not_called()
                    teardown.assert_called_once_with(run, 18816)


class ChipRevealTest(unittest.TestCase):
    def test_horizontal_gesture_uses_current_chip_bounds(self):
        nodes = [node('Filters (3)', [0, 100, 200, 150], True),
                 node('Genre: Ambient', [8, 160, 220, 220]),
                 node('Role: Composer', [230, 160, 430, 220])]
        self.assertEqual(facets.chip_row_region(nodes), (372, 16, 190))
        with patch.object(facets.adrv, 'nodes', side_effect=[nodes, [node('Clear filters', [10, 160, 150, 220], True)]]), \
             patch.object(facets.adrv, 'adb') as adb, \
             patch.object(facets.time, 'sleep'):
            self.assertEqual(facets.visible('^Clear filters$', clickable=True)['text'], 'Clear filters')
            adb.assert_called_once_with('shell', 'input', 'swipe', '372', '190', '16', '190', '350')

    def test_missing_toolbar_sheet_and_invalid_chip_bounds_fail_closed(self):
        toolbar = node('Filters (1)', [0, 100, 200, 150], True)
        chip = node('Genre: Ambient', [8, 160, 220, 220])
        for nodes in [[chip], [toolbar], [toolbar, chip, node('Music filters', [0, 400, 200, 450])],
                      [toolbar, node('Genre: Ambient', [8, 160, 30, 220])]]:
            with self.subTest(nodes=nodes), self.assertRaises(AssertionError):
                facets.chip_row_region(nodes)


class SheetFieldRevealTest(unittest.TestCase):
    def test_field_touching_heading_boundary_keeps_safe_native_tap_center(self):
        first = node('Genre / Any', [20, 350, 480, 500], True)
        nodes = [node('Music filters', [0, 300, 500, 350]),
                 node('Apply', [300, 1100, 500, 1150], True), first]
        with patch.object(facets.adrv, 'nodes', return_value=nodes), \
             patch.object(facets.adrv, 'adb') as adb:
            self.assertIs(facets.visible('Genre', clickable=True, within_sheet=True), first)
            adb.assert_not_called()

    def test_semantic_field_below_footer_is_scrolled_before_tap(self):
        bounds = [node('Music filters', [0, 300, 500, 350]),
                  node('Apply', [300, 1100, 500, 1150], True)]
        clipped = node('Label / Any', [20, 1080, 480, 1200], True)
        revealed = node('Label / Any', [20, 850, 480, 980], True)
        with patch.object(facets.adrv, 'nodes', side_effect=[bounds + [clipped], bounds + [revealed]]), \
             patch.object(facets.adrv, 'adb') as adb, \
             patch.object(facets.time, 'sleep'):
            self.assertIs(facets.visible('Label', clickable=True, within_sheet=True), revealed)
            adb.assert_called_once_with('shell', 'input', 'swipe', '250', '946', '250', '504', '350')


if __name__ == '__main__':
    unittest.main()
