"""Device-free negative checks for F03 native result acceptance, not E2E proof."""
import unittest
from unittest.mock import patch
import music_filters_e2e as runner
from music_filters_e2e import completed_result, temporary_music_owner
from types import SimpleNamespace


class FilterResultOracleTest(unittest.TestCase):
    def test_only_fresh_completed_filtered_empty_passes(self):
        labels = ['Nothing matches these filters', 'Clear filters']
        self.assertTrue(completed_result([], [], labels, fresh_empty_refresh=True))
        for state in [
            dict(labels=labels),  # Stale empty while the next query has not completed.
            dict(labels=labels, fresh_empty_refresh=False),
            dict(labels=labels, loading=True, fresh_empty_refresh=True),
            dict(labels=labels + ['Updating media'], fresh_empty_refresh=True),
            dict(labels=['Could not reach the server.', 'Retry'], fresh_empty_refresh=True),
            dict(labels=['Unexpected error. Please try again.'], fresh_empty_refresh=True),
            dict(labels=['Loading'], fresh_empty_refresh=True),
            dict(labels=['No media found'], fresh_empty_refresh=True),
            dict(labels=[], fresh_empty_refresh=True),
        ]:
            with self.subTest(state=state):
                self.assertFalse(completed_result([], [], **state))
        self.assertFalse(completed_result(['f03-a'], [], labels, fresh_empty_refresh=True))

    def test_populated_matches_still_require_no_loading_or_error(self):
        self.assertTrue(completed_result(['f03-a'], ['f03-a'], ['f03-a']))
        self.assertFalse(completed_result(['f03-a'], ['f03-a'], ['Updating media']))
        self.assertFalse(completed_result(['f03-a'], ['f03-a'], ['Server error (500)']))
        self.assertFalse(completed_result([], ['f03-a'], ['Nothing matches these filters']))


class KeyboardDismissalTest(unittest.TestCase):
    def test_hidden_keyboard_never_pops_the_route(self):
        with patch.object(runner.adrv, 'adb', return_value='mWindowVisible=false mIsInputViewShown=true'), \
                patch.object(runner.app_ui, 'back') as back:
            runner.dismiss_keyboard()
            back.assert_not_called()

    def test_uses_actual_input_view_and_waits_for_closed_keyboard(self):
        states = ['mInputShown=false mIsInputViewShown=true mWindowVisible=true',
                  'mWindowVisible=false mIsInputViewShown=true', 'mWindowVisible=false']
        with patch.object(runner.adrv, 'adb', side_effect=states), \
                patch.object(runner.app_ui, 'back') as back:
            runner.dismiss_keyboard()
            back.assert_called_once()

    def test_unknown_or_stuck_keyboard_fails_explicitly(self):
        with patch.object(runner.adrv, 'adb', return_value='unknown'), \
                patch.object(runner.app_ui, 'back') as back:
            with self.assertRaisesRegex(AssertionError, 'visibility'):
                runner.dismiss_keyboard()
            back.assert_not_called()
        with patch.object(runner.adrv, 'adb', return_value='mWindowVisible=true'), \
                patch.object(runner.app_ui, 'back'), \
                patch.object(runner.time, 'monotonic', side_effect=[0, 10]):
            with self.assertRaisesRegex(AssertionError, 'did not close'):
                runner.dismiss_keyboard()


class GridSwipeTest(unittest.TestCase):
    def test_both_swipe_directions_stay_below_the_actual_toolbar(self):
        nodes = [{'text': '', 'desc': '', 'b': [0, 0, 1080, 2220]},
                 {'text': '', 'desc': 'Filters and sort', 'b': [500, 810, 700, 900]}]
        with patch.object(runner.adrv, 'nodes', return_value=nodes), \
                patch.object(runner.adrv, 'adb') as adb, patch.object(runner.time, 'sleep'):
            for up in (True, False):
                runner.swipe_grid(up)
                command = adb.call_args.args
                start, end = int(command[4]), int(command[6])
                self.assertTrue(916 < start < 2060 and 916 < end < 2060)
                self.assertEqual(start > end, up)

    def test_unknown_or_occluded_grid_never_uses_fixed_screen_coordinates(self):
        for nodes in ([{'text': '', 'desc': '', 'b': [0, 0, 1080, 2220]}],
                      [{'text': '', 'desc': 'Filters and sort', 'b': [0, 800, 1080, 900]}]):
            with patch.object(runner.adrv, 'nodes', return_value=nodes), \
                    patch.object(runner.adrv, 'adb') as adb:
                with self.assertRaises(AssertionError):
                    runner.swipe_grid(True)
                adb.assert_not_called()


class ScopedOwnerRestorationTest(unittest.TestCase):
    def test_restores_exact_role_after_success_and_failed_edit(self):
        for fail in (False, True):
            class Admin:
                def __init__(self):
                    self.role = 'viewer'
                    self.writes = []
                def request(self, method, path, data=None):
                    if method == 'POST':
                        self.writes.append(dict(data))
                        self.role = data['role']
                        return {}
                    return {'users': [{'id': 7, 'username': 'viewer'}], 'permissions': [
                        {'set_id': 10, 'user_id': 7, 'role': self.role},
                        {'set_id': 20, 'user_id': 7, 'role': 'viewer'}]}
            admin = Admin()
            run = SimpleNamespace(admin=admin, state={'set_ids': {'Music': 10}},
                                  auth={'user': {'username': 'viewer'}})
            try:
                with temporary_music_owner(run):
                    self.assertEqual(admin.role, 'owner')
                    if fail:
                        raise RuntimeError('UI edit failed')
            except RuntimeError:
                self.assertTrue(fail)
            self.assertEqual(admin.role, 'viewer')
            self.assertEqual(admin.writes, [
                {'set_id': 10, 'user_id': 7, 'role': 'owner'},
                {'set_id': 10, 'user_id': 7, 'role': 'viewer'}])


if __name__ == '__main__':
    unittest.main()
