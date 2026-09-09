"""The writer emit path (SPEC 4.5.4 and 4.1).

Nothing here is reachable by any corpus tier. Every tier re-renders a document
that already exists, so a writer is only ever asked what a value should look
like when it is emitted from a model with no text behind it. That is why
SPEC 4.5.3's tie rule and the rounding rules are per-implementation tests.
"""

from __future__ import annotations

import math
import unittest

import vnx
from vnx.model import Document, Position, Waypoint
from vnx.writer import encode_bytes, round_position, rounded_document


def waypoint_at(*channels: float | None) -> Document:
    return Document(vnx="0.1.0", waypoints=[Waypoint(at=Position(tuple(channels)))])


class TwoEmitPaths(unittest.TestCase):
    """The formatter and the writer must NOT agree, and the corpus says so."""

    def test_the_full_precision_case_reformats_to_itself(self) -> None:
        path = vnx.__file__.rsplit("/vnx/", 1)[0]
        source = open(
            f"{path}/../corpus/accept/full-precision-coordinate.vnx", "rb"
        ).read()
        document = vnx.read_bytes(source).document
        self.assertEqual(vnx.reformat_bytes(document), source)

    def test_the_same_case_encodes_to_the_rounded_value(self) -> None:
        """Its sidecar: "Encoding this document through a writer gives
        -3.671235, and that is correct." """
        path = vnx.__file__.rsplit("/vnx/", 1)[0]
        source = open(
            f"{path}/../corpus/accept/full-precision-coordinate.vnx", "rb"
        ).read()
        document = vnx.read_bytes(source).document
        encoded = encode_bytes(document)
        self.assertIn(b"[-3.671235, 57.118123]", encoded)
        self.assertNotEqual(encoded, source)

    def test_the_writer_does_not_touch_the_caller_s_model(self) -> None:
        """SPEC 4.5.4: rounding MUST NOT happen in the model."""
        document = waypoint_at(-3.6712345678901235, 57.118)
        encode_bytes(document)
        assert document.waypoints is not None
        at = document.waypoints[0].at
        assert at is not None
        self.assertEqual(at.lon, -3.6712345678901235)


class Rounding(unittest.TestCase):
    def test_places(self) -> None:
        p = round_position(Position((-3.6712345678901235, 57.11812345678901, 340.26)))
        self.assertEqual(p.channels, (-3.671235, 57.118123, 340.3))

    def test_time_is_never_rounded(self) -> None:
        p = round_position(Position((1.0, 2.0, 3.0, 1778914991000.0)))
        self.assertEqual(p.time_ms, 1778914991000.0)

    def test_a_channel_beyond_the_fourth_is_carried_through(self) -> None:
        """The fifth channel keeps full precision; this format defines none.

        `3.05` rounds DOWN to `3.0`, and that is correct: the literal is really
        3.04999999999999982..., so it is below the midpoint and not a tie at
        all. A rounding test written from decimal intuition asserts 3.1 here
        and calls a correct writer broken.
        """
        p = round_position(Position((1.0, 2.0, 3.05, 4.0, 5.6789)))
        self.assertEqual(p.channels, (1.0, 2.0, 3.0, 4.0, 5.6789))


class ShortestSpelling(unittest.TestCase):
    """SPEC 4.1: a writer emits the shortest spelling of a position."""

    def test_a_trailing_null_goes(self) -> None:
        self.assertEqual(round_position(Position((1.0, 2.0, None))).channels, (1.0, 2.0))

    def test_several_trailing_nulls_go(self) -> None:
        self.assertEqual(
            round_position(Position((1.0, 2.0, None, None))).channels, (1.0, 2.0)
        )

    def test_an_interior_null_stays(self) -> None:
        """Dropping it would move the time into the elevation channel."""
        self.assertEqual(
            round_position(Position((1.0, 2.0, None, 5.0))).channels,
            (1.0, 2.0, None, 5.0),
        )

    def test_the_formatter_keeps_the_spelling_the_writer_would_shorten(self) -> None:
        document = waypoint_at(1.0, 2.0, None)
        self.assertIn(b"[1, 2, null]", vnx.reformat_bytes(document))
        self.assertIn(b"[1, 2]", encode_bytes(document))


class RoundingTies(unittest.TestCase):
    """SPEC 4.5.3: a tie rounds to even, in the ROUNDING path as well.

    The JavaScript seat's case: `340.25` to one place is `340.2` in Go and
    Python and `340.3` from JS `toFixed`, and `340.25` is an exact double
    reachable from any quarter-metre elevation.
    """

    def elevation(self, value: float) -> float:
        rounded = round_position(Position((0.0, 0.0, value))).ele
        assert rounded is not None
        return rounded

    def test_the_javascript_case(self) -> None:
        from decimal import Decimal

        self.assertEqual(str(Decimal(340.25)), "340.25", "not an exact tie")
        self.assertEqual(self.elevation(340.25), 340.2)

    def test_ties_go_to_even_in_both_directions(self) -> None:
        for value, want in (
            (340.25, 340.2), (340.35, 340.4), (340.45, 340.4), (340.55, 340.6),
            (-340.25, -340.2), (-340.45, -340.4),
        ):
            with self.subTest(value=value):
                self.assertEqual(self.elevation(value), want)

    def test_a_six_place_tie_goes_to_even(self) -> None:
        """Six-place ties are rare but real, and worth knowing how to build.

        A tie at n places needs the midpoint (2k+1)/(2*10**n) to be exactly
        representable, i.e. DYADIC, which needs 5**n to divide (2k+1).  At six
        places the smallest such value is 5**6 / (2 * 10**6) = 2**-7.  Nothing
        like `0.0000005` works: it is not a binary64 value at all, so a naive
        attempt at this test silently exercises a non-tie.
        """
        from decimal import Decimal

        for value, want in (
            (0.0078125, 0.007812),      # 2**-7,  even wins over 0.007813
            (0.0234375, 0.023438),      # 3*2**-7, even wins over 0.023437
            (0.0390625, 0.039062),
            (0.0546875, 0.054688),
        ):
            with self.subTest(value=value):
                self.assertEqual(
                    str(Decimal(value))[-1], "5", "not an exact six-place tie"
                )
                got = round_position(Position((value, 2.0))).lon
                self.assertEqual(got, want)
                self.assertEqual(int(f"{got:.6f}"[-1]) % 2, 0, "chose the odd digit")


class RoundingCreatesNegativeZero(unittest.TestCase):
    """Rounding can CREATE a negative zero, which the renderer must collapse.

    `format/messy-spellings.in.vnx` carries `-0.0` as an input LITERAL, so the
    corpus tests a renderer collapsing a negative zero it was GIVEN. Nothing
    anywhere tests one a writer produced, and that is a different code path
    which only exists once a writer does.
    """

    def test_rounding_really_does_produce_a_signed_zero(self) -> None:
        rounded = self.rounded(-0.04)
        self.assertEqual(rounded, 0.0)
        self.assertTrue(math.copysign(1.0, rounded) < 0, "not a negative zero")

    @staticmethod
    def rounded(value: float) -> float:
        ele = round_position(Position((0.0, 0.0, value))).ele
        assert ele is not None
        return ele

    def test_a_created_negative_zero_never_reaches_the_bytes(self) -> None:
        # -0.04 is a legitimate longitude at six places and must KEEP its sign;
        # only the elevation, at one place, becomes a negative zero.  Asserting
        # "no minus anywhere" would pass for the wrong reason and fail for a
        # correct writer, so the whole array is pinned.
        self.assertIn(b'"at": [-0.04, -0.04, 0]', encode_bytes(waypoint_at(-0.04, -0.04, -0.04)))
        # Below half a microdegree every channel collapses, signs and all.
        self.assertIn(b'"at": [0, 0, 0]', encode_bytes(waypoint_at(-4e-7, -4e-7, -0.04)))

    def test_a_negative_longitude_that_rounds_to_zero(self) -> None:
        self.assertIn(b"[0, 0]", encode_bytes(waypoint_at(-0.0000004, -0.0000004)))

    def test_a_formatting_primitive_would_get_both_wrong(self) -> None:
        """Why the renderer, not `format()`, decides the bytes."""
        self.assertEqual(format(-0.04, ".1f"), "-0.0")
        self.assertEqual(format(340.0, ".1f"), "340.0")
        self.assertEqual(vnx.render_number(round(-0.04, 1)), "0")
        self.assertEqual(vnx.render_number(round(340.0, 1)), "340")


class WriterOutputIsCanonical(unittest.TestCase):
    def test_encoding_is_a_fixed_point_under_the_formatter(self) -> None:
        document = waypoint_at(-3.6712345678901235, 57.11812345678901, 340.26)
        encoded = encode_bytes(document)
        self.assertEqual(vnx.canonicalise(encoded), encoded)

    def test_encoding_twice_is_stable(self) -> None:
        document = waypoint_at(-3.6712345678901235, 57.11812345678901, 340.26)
        once = encode_bytes(document)
        twice = encode_bytes(vnx.read_bytes(once).document)
        self.assertEqual(once, twice)


if __name__ == "__main__":
    unittest.main()
