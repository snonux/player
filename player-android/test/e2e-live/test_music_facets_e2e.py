"""Negative checks for M08's bounded, semantics-derived sheet gesture region."""
import unittest
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


if __name__ == '__main__':
    unittest.main()
