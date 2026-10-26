import unittest

from podcast_server_switch_e2e import (
    account_scope,
    assert_pending_isolated,
    secondary_origin,
)


class ServerSwitchOracleTest(unittest.TestCase):
    def test_alias_changes_only_selected_origin(self):
        self.assertEqual(
            secondary_origin('http://127.0.0.1:18766', 18767),
            'http://127.0.0.1:18767',
        )
        with self.assertRaises(ValueError):
            secondary_origin('http://127.0.0.1:18766', 18766)
        with self.assertRaises(ValueError):
            secondary_origin('http://user:pass@127.0.0.1:18766', 18767)

    def test_pending_edit_cannot_follow_same_account_to_alias(self):
        first = account_scope('http://127.0.0.1:18766', 1)
        second = account_scope('http://127.0.0.1:18767', 1)
        episode = 'guid:alpha-005'
        snapshot = {'podcast_episode_state': [
            {'scope': first, 'episode_key': episode,
             'completion_pending_edit_id': 'primary-edit'},
            {'scope': second, 'episode_key': episode,
             'completion_pending_edit_id': None},
        ]}
        assert_pending_isolated(snapshot, first, second, episode, 'primary-edit')

        snapshot['podcast_episode_state'][1]['completion_pending_edit_id'] = 'primary-edit'
        with self.assertRaisesRegex(AssertionError, 'leaked'):
            assert_pending_isolated(snapshot, first, second, episode, 'primary-edit')

        snapshot['podcast_episode_state'][1]['completion_pending_edit_id'] = None
        snapshot['podcast_episode_state'][0]['completion_pending_edit_id'] = None
        with self.assertRaisesRegex(AssertionError, 'lost'):
            assert_pending_isolated(snapshot, first, second, episode, 'primary-edit')


if __name__ == '__main__':
    unittest.main()
