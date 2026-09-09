#!/usr/bin/env python3
"""Regression tests for Pi input-screen readiness."""

from __future__ import annotations

import unittest
from pathlib import Path

from pi_input_ready import pi_input_screen_ready

HERE = Path(__file__).resolve().parent
ATTEMPT2 = HERE / "failure-attempt2-01b-ready-visible.txt"

READY_FIXTURE = """
pi v0.0.0
escape interrupt · ctrl+c/ctrl+d clear/exit · / commands · ! bash · ctrl+o more
Press ctrl+o to show full startup help and loaded resources.

Pi can explain its own features and look up its docs. Ask it how to use or extend Pi.

────────────────────────────────────────────────────────────────
────────────────────────────────────────────────────────────────
/tmp/hseh-pi-cwd
?/0 (auto)                                                          no-model
"""

NPM_ARBITRARY = """
added 70 packages, and audited 71 packages in 4s

30 packages are looking for funding
  run `npm fund` for details

found 0 vulnerabilities
Cloning into 'something'...
more lines of package output that is longer than three lines
still not an editor
"""

SHELL_MORE_THAN_THREE = """
sh-5.3$ echo one
one
sh-5.3$ echo two
two
sh-5.3$ echo three
three
sh-5.3$
"""


class PiInputScreenReadyTests(unittest.TestCase):
    def test_attempt2_visible_is_not_ready(self):
        text = ATTEMPT2.read_text()
        self.assertFalse(pi_input_screen_ready(text))

    def test_spinner_is_not_ready(self):
        self.assertFalse(pi_input_screen_ready("pi\n⠹\n"))

    def test_empty_is_not_ready(self):
        self.assertFalse(pi_input_screen_ready(""))
        self.assertFalse(pi_input_screen_ready("   \n  "))

    def test_shell_more_than_three_lines_is_not_ready(self):
        self.assertFalse(pi_input_screen_ready(SHELL_MORE_THAN_THREE))

    def test_stable_arbitrary_npm_output_is_not_ready(self):
        self.assertFalse(pi_input_screen_ready(NPM_ARBITRARY))

    def test_source_derived_ready_screen_is_ready(self):
        self.assertTrue(pi_input_screen_ready(READY_FIXTURE))

    def test_captured_ready_screen_if_present(self):
        captured = HERE / "startup-only" / "1" / "ready-visible.txt"
        if not captured.is_file():
            self.skipTest("startup-only capture not written yet")
        self.assertTrue(pi_input_screen_ready(captured.read_text()))


if __name__ == "__main__":
    unittest.main()
