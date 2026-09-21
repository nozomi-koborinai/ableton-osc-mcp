"""Unit tests for the arrangement helpers in the AbletonOSC patch. Runs without Live:

    python3 -m unittest discover -s remote-script/tests -v
"""
import unittest

from test_mixer_db import browser


CLIPS = [
    ("Intro", 0.0, 16.0),
    ("Hook", 16.0, 24.0),
    ("Hook", 24.0, 32.0),
    ("Vocal take", 30.0, 50.0),
]


class ClipsTouching(unittest.TestCase):
    def test_a_clip_that_only_meets_the_range_at_its_edge_does_not_touch_it(self):
        got = browser.clips_touching(CLIPS, 16.0, 24.0)
        self.assertEqual([c[0:2] for c in got], [("Hook", 16.0)])

    def test_a_clip_that_reaches_into_the_range_touches_it(self):
        got = browser.clips_touching(CLIPS, 28.0, 31.0)
        self.assertEqual([c[0:2] for c in got], [("Hook", 24.0), ("Vocal take", 30.0)])

    def test_open_bounds_take_everything(self):
        self.assertEqual(len(browser.clips_touching(CLIPS, None, None)), 4)
        self.assertEqual([c[0] for c in browser.clips_touching(CLIPS, 31.0, None)], ["Hook", "Vocal take"])

    def test_rounding_noise_at_an_edge_is_not_an_overlap(self):
        got = browser.clips_touching([("Hook", 16.0, 24.0000001)], 24.0, 32.0)
        self.assertEqual(got, [])


class ClipsInside(unittest.TestCase):
    def test_only_clips_wholly_inside_are_picked(self):
        got = browser.clips_inside(CLIPS, 16.0, 32.0)
        self.assertEqual([c[0:2] for c in got], [("Hook", 16.0), ("Hook", 24.0)])

    def test_a_take_that_sticks_out_of_the_range_is_left_alone(self):
        # Deleting works on whole clips: one that reaches beyond the range is not ours to delete.
        self.assertEqual(browser.clips_inside(CLIPS, 24.0, 40.0), [("Hook", 24.0, 32.0)])

    def test_rounding_noise_does_not_keep_a_clip_out(self):
        got = browser.clips_inside([("Hook", 15.9999999, 24.0000001)], 16.0, 24.0)
        self.assertEqual(len(got), 1)


if __name__ == "__main__":
    unittest.main()
