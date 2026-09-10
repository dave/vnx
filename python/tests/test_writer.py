"""The writer emit path (SPEC 4.1).

Nothing here is reachable by the accept, reject or diagnose tiers. Each of them
re-encodes a document that already satisfies the emit-boundary rules, so a
writer is only ever asked what a value should look like when it is emitted from
a model with no text behind it. That is why the rounding rules are
per-implementation tests as well as a corpus tier.
"""

from __future__ import annotations

import math
import unittest

import vnx
from vnx.encode import _position
from vnx.model import Document, Position, Waypoint
from vnx.writer import encode_bytes, round_position, round_to, rounded_document


def waypoint_at(
    lon: float,
    lat: float,
    ele: float | None = None,
    time_ms: float | None = None,
) -> Document:
    return Document(
        vnx="0.1.0", waypoints=[Waypoint(at=Position(lon, lat, ele, time_ms))]
    )


class TheEmitBoundary(unittest.TestCase):
    def source(self) -> bytes:
        path = vnx.__file__.rsplit("/vnx/", 1)[0]
        with open(f"{path}/../corpus/accept/full-precision-coordinate.vnx", "rb") as f:
            return f.read()

    def test_the_full_precision_case_encodes_to_the_rounded_value(self) -> None:
        """Its sidecar: "Encoding this document through a writer gives
        -3.671235, and that is correct." """
        document = vnx.read_bytes(self.source()).document
        self.assertIn(b"[-3.671235,57.118123]", encode_bytes(document))

    def test_the_reader_keeps_the_full_precision_value(self) -> None:
        """SPEC 4.1: a reader MUST NOT round. Rounding is the writer's."""
        document = vnx.read_bytes(self.source()).document
        assert document.waypoints is not None
        at = document.waypoints[0].at
        assert at is not None
        self.assertEqual(at.lon, -3.6712345678901235)

    def test_the_writer_does_not_touch_the_caller_s_model(self) -> None:
        """SPEC 4.1: rounding MUST NOT happen in the model."""
        document = waypoint_at(-3.6712345678901235, 57.118)
        encode_bytes(document)
        assert document.waypoints is not None
        at = document.waypoints[0].at
        assert at is not None
        self.assertEqual(at.lon, -3.6712345678901235)

    def test_the_rounded_copy_is_a_different_document(self) -> None:
        document = waypoint_at(-3.6712345678901235, 57.118)
        self.assertIsNot(rounded_document(document), document)


class Rounding(unittest.TestCase):
    def test_places(self) -> None:
        p = round_position(Position(-3.6712345678901235, 57.11812345678901, 340.26))
        self.assertEqual((p.lon, p.lat, p.ele), (-3.671235, 57.118123, 340.3))

    def test_time_is_never_rounded(self) -> None:
        p = round_position(Position(1.0, 2.0, 3.0, 1778914991000.0))
        self.assertEqual(p.time_ms, 1778914991000.0)

    def test_an_absent_elevation_stays_absent(self) -> None:
        """Rounding a missing elevation into 0.0 would put every unknown point
        at sea level."""
        self.assertIsNone(round_position(Position(1.0, 2.0)).ele)

    def test_rounding_is_by_formatting_and_not_by_scaling(self) -> None:
        """SPEC 4.1 names the scaled route as non-conforming, and these are the
        three values it argues from.

        The scaled route gives 0.2, 0 and 0.4, because the scaled product is
        itself a binary64 and is not always the exact scaled value.
        """
        for value, want in ((0.15, 0.1), (0.05, 0.1), (0.45, 0.5)):
            with self.subTest(value=value):
                self.assertEqual(round_to(value, 1), want)


class ShortestSpelling(unittest.TestCase):
    """SPEC 4.1: a writer emits the shortest spelling of a position."""

    def test_an_absent_elevation_and_time_leave_two_channels(self) -> None:
        self.assertEqual(_position(Position(1.0, 2.0)), [1.0, 2.0])

    def test_an_absent_time_leaves_three(self) -> None:
        self.assertEqual(_position(Position(1.0, 2.0, 3.0)), [1.0, 2.0, 3.0])

    def test_an_interior_null_stays(self) -> None:
        """Dropping it would move the time into the elevation channel."""
        self.assertEqual(
            _position(Position(1.0, 2.0, None, 5.0)), [1.0, 2.0, None, 5.0]
        )

    def test_the_document_carries_the_short_spelling(self) -> None:
        """`1.0` rather than `1` is a spelling, and a spelling is the writer's
        own business: SPEC 4.2 defines the round trip at value level."""
        self.assertIn(b'"at":[1.0,2.0]', encode_bytes(waypoint_at(1.0, 2.0)))


class RoundingTies(unittest.TestCase):
    """SPEC 4.1: a rounding tie goes to even.

    The JavaScript seat's case: `340.25` to one place is `340.2` in Go and
    Python and `340.3` from JS `toFixed`, and `340.25` is an exact double
    reachable from any quarter-metre elevation.
    """

    def elevation(self, value: float) -> float:
        rounded = round_position(Position(0.0, 0.0, value)).ele
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
                got = round_position(Position(value, 2.0)).lon
                self.assertEqual(got, want)
                self.assertEqual(int(f"{got:.6f}"[-1]) % 2, 0, "chose the odd digit")


class RoundingCreatesNegativeZero(unittest.TestCase):
    """Rounding can CREATE a negative zero, which the writer must collapse.

    No document carries one, so nothing but the writer's own arithmetic can
    produce it, and only once a writer exists.
    """

    def test_the_formatting_primitive_really_does_produce_a_signed_zero(self) -> None:
        self.assertEqual(format(-0.04, ".1f"), "-0.0")
        self.assertTrue(math.copysign(1.0, float(format(-0.04, ".1f"))) < 0)

    def test_the_rounding_collapses_it(self) -> None:
        rounded = round_to(-0.04, 1)
        self.assertEqual(rounded, 0.0)
        self.assertTrue(math.copysign(1.0, rounded) > 0, "still a negative zero")

    def test_a_created_negative_zero_never_reaches_the_bytes(self) -> None:
        # -0.04 is a legitimate longitude at six places and must KEEP its sign;
        # only the elevation, at one place, becomes a negative zero.  Asserting
        # "no minus anywhere" would pass for the wrong reason and fail for a
        # correct writer, so the whole array is pinned.
        self.assertIn(
            b'"at":[-0.04,-0.04,0.0]', encode_bytes(waypoint_at(-0.04, -0.04, -0.04))
        )
        # Below half a microdegree every channel collapses, signs and all.
        self.assertIn(
            b'"at":[0.0,0.0,0.0]', encode_bytes(waypoint_at(-4e-7, -4e-7, -0.04))
        )

    def test_a_negative_longitude_that_rounds_to_zero(self) -> None:
        self.assertIn(b"[0.0,0.0]", encode_bytes(waypoint_at(-0.0000004, -0.0000004)))


class WriterOutputIsStable(unittest.TestCase):
    def test_encoding_twice_is_stable(self) -> None:
        document = waypoint_at(-3.6712345678901235, 57.11812345678901, 340.26)
        once = encode_bytes(document)
        twice = encode_bytes(vnx.read_bytes(once).document)
        self.assertEqual(once, twice)

    def test_the_output_carries_no_whitespace(self) -> None:
        """Whitespace is the writer's own business, and the smallest spelling
        is the useful one."""
        encoded = encode_bytes(waypoint_at(1.0, 2.0))
        self.assertNotIn(b" ", encoded)
        self.assertNotIn(b"\n", encoded)


if __name__ == "__main__":
    unittest.main()
