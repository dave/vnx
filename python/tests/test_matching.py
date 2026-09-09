"""Proof that the diagnostic matcher still discriminates.

SPEC 6.3 makes emission order insignificant, so diagnostics are compared as a
set -- and a set matcher is exactly the shape that decays into "anything
passes" with nothing going red.  Every case in `corpus/diagnose/` expects at
most one diagnostic, so a matcher that checked nothing would keep the whole tier
green.  One test per way a reader can be wrong.
"""

from __future__ import annotations

import unittest

from vnx.errors import Diagnostic

from .matching import match_diagnostics


class MatchDiagnostics(unittest.TestCase):
    A = Diagnostic("/waypoints/0/nmae", "unknown key `nmae`", "unknown-key")
    B = Diagnostic(
        "/areas/0/polygon", "nested rings: one closed ring only", "polygon-nested-rings"
    )

    def test_an_exact_pair_matches(self) -> None:
        self.assertIsNone(
            match_diagnostics(
                [{"path": "/waypoints/0/nmae", "code": "unknown-key"}], [self.A]
            )
        )

    def test_order_does_not_matter(self) -> None:
        expected = [
            {"path": "/areas/0/polygon", "code": "polygon-nested-rings"},
            {"path": "/waypoints/0/nmae", "code": "unknown-key"},
        ]
        self.assertIsNone(match_diagnostics(expected, [self.A, self.B]))

    def test_a_missing_report_fails(self) -> None:
        self.assertIsNotNone(
            match_diagnostics(
                [{"path": "/waypoints/0/nmae", "code": "unknown-key"}], []
            )
        )

    def test_an_extra_report_fails(self) -> None:
        """An unexpected diagnostic is a defect, not a bonus."""
        self.assertIsNotNone(
            match_diagnostics(
                [{"path": "/waypoints/0/nmae", "code": "unknown-key"}],
                [self.A, self.B],
            )
        )

    def test_a_wrong_path_fails(self) -> None:
        self.assertIsNotNone(
            match_diagnostics(
                [{"path": "/waypoints/1/nmae", "code": "unknown-key"}], [self.A]
            )
        )

    def test_a_wrong_code_fails(self) -> None:
        self.assertIsNotNone(
            match_diagnostics(
                [{"path": "/waypoints/0/nmae", "code": "polygon-nested-rings"}],
                [self.A],
            )
        )

    def test_the_english_is_not_compared_at_all(self) -> None:
        """SPEC 6.3, ruled 2026-09-09.

        A reader whose wording shares not one word with the sidecar's must still
        pass, or the corpus is pinning translations of a rule rather than the
        rule. This is the assertion that says the old substring matching is
        really gone rather than merely unused.
        """
        foreign = Diagnostic("/waypoints/0/nmae", "clé inconnue", "unknown-key")
        self.assertIsNone(
            match_diagnostics(
                [{"path": "/waypoints/0/nmae", "code": "unknown-key",
                  "message_advisory": "unknown key"}],
                [foreign],
            )
        )

    def test_a_missing_code_on_the_report_fails(self) -> None:
        """A pinned case needs a code from the reader too, not just a path."""
        codeless = Diagnostic("/waypoints/0/nmae", "unknown key `nmae`")
        self.assertIsNotNone(
            match_diagnostics(
                [{"path": "/waypoints/0/nmae", "code": "unknown-key"}], [codeless]
            )
        )

    def test_one_report_cannot_satisfy_two_expectations(self) -> None:
        """The property that makes a set comparison a comparison.

        A runner that let one report answer both expectations would pass a
        reader that found half the defects.
        """
        expected = [
            {"path": "/waypoints/0/nmae", "code": "unknown-key"},
            {"path": "/waypoints/0/nmae", "code": "unknown-key"},
        ]
        self.assertIsNotNone(match_diagnostics(expected, [self.A]))

    def test_two_reports_answer_two_expectations(self) -> None:
        expected = [
            {"path": "/waypoints/0/nmae", "code": "unknown-key"},
            {"path": "/waypoints/0/nmae", "code": "unknown-key"},
        ]
        twice = [self.A, Diagnostic("/waypoints/0/nmae", "again", "unknown-key")]
        self.assertIsNone(match_diagnostics(expected, twice))

    def test_a_greedy_pairing_does_not_decide_it(self) -> None:
        """The first expectation must not be allowed to take the only report
        the second one could have used."""
        wide = {"path": "/x"}
        narrow = {"path": "/x", "code": "specific"}
        reports = [Diagnostic("/x", "", "specific"), Diagnostic("/x", "", "other")]
        self.assertIsNone(match_diagnostics([wide, narrow], reports))

    def test_an_empty_expectation_needs_an_empty_report(self) -> None:
        """`diagnose/duplicate-object-key.vnx` expects nothing at all."""
        self.assertIsNone(match_diagnostics([], []))
        self.assertIsNotNone(match_diagnostics([], [self.A]))


if __name__ == "__main__":
    unittest.main()
