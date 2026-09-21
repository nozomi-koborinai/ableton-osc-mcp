"""Unit tests for the dB helpers in the AbletonOSC patch. Runs without Live:

    python3 -m unittest discover -s remote-script/tests -v
"""
import importlib.util
import pathlib
import sys
import types
import unittest


def load_browser_module():
    """Import remote-script/abletonosc/browser.py with stand-ins for Live-only modules."""
    sys.modules.setdefault("Live", types.ModuleType("Live"))
    package = types.ModuleType("abletonosc")
    package.__path__ = []
    sys.modules["abletonosc"] = package
    handler = types.ModuleType("abletonosc.handler")
    handler.AbletonOSCHandler = object
    sys.modules["abletonosc.handler"] = handler

    path = pathlib.Path(__file__).resolve().parents[1] / "abletonosc" / "browser.py"
    spec = importlib.util.spec_from_file_location("abletonosc.browser", str(path))
    module = importlib.util.module_from_spec(spec)
    sys.modules["abletonosc.browser"] = module
    spec.loader.exec_module(module)
    return module


browser = load_browser_module()


def fake_fader_display(value):
    """A fader law shaped like Live's: 0.85 is 0 dB, 1.0 is +6 dB, linear down
    to -18 dB at 0.4, steeper below, and silence at the bottom."""
    if value <= 0.0:
        return "-inf dB"
    if value >= 0.4:
        db = 40.0 * value - 34.0
    else:
        db = -18.0 - (0.4 - value) * 130.0
    return "%.1f dB" % db


def fine_fader_display(value):
    """What Live 11 actually returns from str_for_value: up to three decimals
    ("-6.031 dB"), not the single decimal the mixer UI shows."""
    if value <= 0.0:
        return "-inf dB"
    if value >= 0.4:
        db = 40.0 * value - 34.0
    else:
        db = -18.0 - (0.4 - value) * 130.0
    return "%.3f dB" % db


class ParseDbDisplayTest(unittest.TestCase):
    def test_reads_negative_zero_and_positive_levels(self):
        self.assertEqual(browser.parse_db_display("-6.0 dB"), -6.0)
        self.assertEqual(browser.parse_db_display("0.0 dB"), 0.0)
        self.assertEqual(browser.parse_db_display("6.0 dB"), 6.0)
        self.assertEqual(browser.parse_db_display("+3.5 dB"), 3.5)

    def test_silence_is_none(self):
        self.assertIsNone(browser.parse_db_display("-inf dB"))

    def test_rejects_strings_that_are_not_levels(self):
        for text in ("", "50 %", "180 Hz", "loud"):
            with self.assertRaises(ValueError):
                browser.parse_db_display(text)


class FindValueForDbTest(unittest.TestCase):
    def find(self, target):
        return browser.find_value_for_db(fake_fader_display, 0.0, 1.0, target)

    def test_lands_on_the_requested_level_in_the_linear_region(self):
        raw, reached = self.find(-6.0)
        self.assertTrue(reached)
        self.assertEqual(fake_fader_display(raw), "-6.0 dB")

    def test_lands_on_the_requested_level_where_the_law_is_steep(self):
        # Layers often sit around -24 dB, below where a linear formula holds.
        raw, reached = self.find(-24.0)
        self.assertTrue(reached)
        self.assertEqual(fake_fader_display(raw), "-24.0 dB")

    def test_unity_is_the_documented_fader_position(self):
        raw, reached = self.find(0.0)
        self.assertTrue(reached)
        self.assertAlmostEqual(raw, 0.85, places=2)

    def test_minus_seventy_and_below_mean_silence(self):
        for target in (-70.0, -120.0):
            raw, reached = self.find(target)
            self.assertTrue(reached)
            self.assertEqual(raw, 0.0)

    def test_uses_all_the_precision_the_display_offers(self):
        # Live answers with three decimals, so "-6 dB" should not settle for -6.03.
        for target in (-6.0, -24.0, 0.0):
            raw, reached = browser.find_value_for_db(fine_fader_display, 0.0, 1.0, target)
            self.assertTrue(reached)
            shown = browser.parse_db_display(fine_fader_display(raw))
            self.assertLessEqual(abs(shown - target), 0.002, "asked %s, got %s" % (target, shown))

    def test_a_coarse_display_still_resolves_targets_between_its_steps(self):
        # With one decimal on show, -6.04 can only ever read -6.0: that is a hit,
        # not a level the fader cannot reach.
        raw, reached = self.find(-6.04)
        self.assertTrue(reached)
        self.assertEqual(fake_fader_display(raw), "-6.0 dB")

    def test_reports_a_level_the_fader_cannot_reach(self):
        raw, reached = self.find(10.0)  # the fader tops out at +6 dB
        self.assertFalse(reached)
        self.assertEqual(fake_fader_display(raw), "6.0 dB")


if __name__ == "__main__":
    unittest.main()
