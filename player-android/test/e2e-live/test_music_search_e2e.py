"""Device-free oracle checks; actual native search remains required."""
import unittest
from music_search_e2e import assert_unicode_field, assert_source_audio, result_node, matching_nodes


class SearchOracleTests(unittest.TestCase):
    def test_result_selection_rejects_field_heading_menu_and_miniplayer(self):
        def node(label, cls='View', click=True, bounds=(0, 200, 500, 280)):
            return {'text': '', 'desc': label, 'cls': cls, 'click': click, 'b': list(bounds)}
        title = 'A Different Music Title'
        impostors = [node(title, cls='EditText'), node(title, click=False),
                     node('Actions for ' + title), node('Open player: ' + title),
                     node(title, bounds=(0, 200, 500, 200))]
        self.assertIsNone(result_node(impostors, title))
        actual = node(title + ' / A & B; C/D · Metadata Album')
        self.assertIs(result_node(impostors + [actual], title), actual)
        with self.assertRaisesRegex(AssertionError, 'Ambiguous'):
            result_node([actual, dict(actual)], title)
        self.assertEqual(matching_nodes([impostors[0]], title), [])
        self.assertEqual(matching_nodes([impostors[1]], title, clickable=True), [])

    def test_unicode_requires_exact_editable_query_not_label_substring(self):
        self.assertEqual(assert_unicode_field({'text': '東京 Nights (夜)', 'desc': ''}), '東京 Nights (夜)')
        for value in ['Album: 東京 Nights (夜)', 'Nights', '', '東京', 'Search music 東京 Nights (夜)']:
            with self.subTest(value=value), self.assertRaises(AssertionError):
                assert_unicode_field({'text': value, 'desc': ''})

    def test_audio_requires_actual_matching_audible_segment(self):
        good = {'segments': [{'tone_hz': 415.4, 'duration_s': 1.2}]}
        self.assertEqual(len(assert_source_audio(good, 415.3)), 1)
        for segments in [[], [{'tone_hz': 220, 'duration_s': 2}], [{'tone_hz': 415.3, 'duration_s': .1}], [{'tone_hz': None, 'duration_s': 2}]]:
            with self.subTest(segments=segments), self.assertRaises(AssertionError):
                assert_source_audio({'segments': segments}, 415.3)


if __name__ == '__main__':
    unittest.main()
