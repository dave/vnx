"""ECMAScript `Number::toString` (SPEC 4.5.3).

`corpus/format/messy-spellings.in.vnx` reaches the four values of the table in
SPEC 4.5.3 plus `1E21` and `-0.0`.  These cover the boundaries around them,
where an off-by-one in the exponent rule hides.
"""

from __future__ import annotations

import math
import unittest

from vnx.numbers import NonFiniteNumberError, render_number


class RenderNumber(unittest.TestCase):
    def check(self, cases: list[tuple[float, str]]) -> None:
        for value, want in cases:
            with self.subTest(value=repr(value)):
                self.assertEqual(render_number(value), want)

    def test_the_table_in_section_4_5_3(self) -> None:
        self.check([
            (340.0, "340"),
            (0.000001, "0.000001"),
            (0.0000001, "1e-7"),
            (1778914991000.0, "1778914991000"),
        ])

    def test_negative_zero_collapses(self) -> None:
        self.check([(-0.0, "0"), (0.0, "0")])
        self.assertEqual(math.copysign(1.0, -0.0), -1.0)  # it really was -0.0

    def test_the_small_exponent_boundary(self) -> None:
        """1e-6 renders in full, 1e-7 goes exponential."""
        self.check([
            (1e-5, "0.00001"),
            (1e-6, "0.000001"),
            (1e-7, "1e-7"),
            (1.5e-7, "1.5e-7"),
            # n is -6 for this one too, so it goes exponential with 9.9e-7 and
            # not 0.00000099: the rule turns on n, never on the digit count.
            (9.9e-7, "9.9e-7"),
            (9.9e-6, "0.0000099"),
        ])

    def test_the_large_exponent_boundary(self) -> None:
        """Up to 21 digits renders in full, 1e21 goes exponential."""
        self.check([
            (1e20, "100000000000000000000"),
            (1e21, "1e+21"),
            (1.2345678901234568e21, "1.2345678901234568e+21"),
            (123456789012345678901.0, "123456789012345680000"),
        ])

    def test_shortest_round_trip_digits(self) -> None:
        self.check([
            (-3.6712345678901235, "-3.6712345678901235"),
            (0.1, "0.1"),
            (0.3, "0.3"),
            (1 / 3, "0.3333333333333333"),
            (57.11812345678901, "57.11812345678901"),
        ])

    def test_the_extremes(self) -> None:
        self.check([
            (5e-324, "5e-324"),
            (1.7976931348623157e308, "1.7976931348623157e+308"),
        ])

    def test_an_integer_valued_float_loses_its_point(self) -> None:
        self.check([(1.0, "1"), (-2.0, "-2"), (1e6, "1000000")])

    def test_non_finite_is_refused(self) -> None:
        for value in (float("nan"), float("inf"), float("-inf")):
            with self.subTest(value=value), self.assertRaises(NonFiniteNumberError):
                render_number(value)

    def test_every_rendering_reads_back_as_the_same_double(self) -> None:
        """Whatever the spelling, the value is preserved: SPEC 4.2 defines
        round-trip at value level."""
        import random

        rng = random.Random(20260909)
        values = [
            rng.uniform(-180.0, 180.0) for _ in range(2000)
        ] + [
            rng.uniform(-1e-8, 1e-8) for _ in range(500)
        ] + [
            rng.uniform(-1e22, 1e22) for _ in range(500)
        ]
        for value in values:
            self.assertEqual(float(render_number(value)), value)


if __name__ == "__main__":
    unittest.main()


class AgainstV8(unittest.TestCase):
    """Cross-check the renderer against a real ECMAScript engine.

    SPEC 4.5.3 defines the rendering by reference to `Number::toString`, so the
    strongest available check is to ask an implementation of it.  Skipped where
    `node` is not installed: the corpus is the requirement and it needs no
    toolchain, this is a stronger check where one happens to be present.
    """

    def test_six_thousand_doubles_agree_with_node(self) -> None:
        import json
        import random
        import shutil
        import subprocess

        if shutil.which("node") is None:
            self.skipTest("node is not installed")

        rng = random.Random(20260909)
        values = [0.0, -0.0, 1e21, 1e20, 1e-6, 1e-7, 5e-324, -5e-324,
                  1.7976931348623157e308]
        for _ in range(6000):
            kind = rng.randrange(6)
            if kind == 0:
                values.append(rng.uniform(-180.0, 180.0))
            elif kind == 1:
                values.append(rng.uniform(-1e-5, 1e-5))
            elif kind == 2:
                values.append(rng.uniform(-1e22, 1e22))
            elif kind == 3:
                values.append(float(rng.randrange(-10**15, 10**15)))
            elif kind == 4:
                values.append(rng.uniform(-1.0, 1.0) * 10.0 ** rng.randrange(-320, 308))
            else:
                values.append(round(rng.uniform(-1e3, 1e3), rng.randrange(0, 10)))

        # `repr` round-trips exactly and Number() reads it, so the engine is
        # handed the same doubles.
        script = (
            "const r=%s;process.stdout.write("
            "JSON.stringify(r.map(s=>String(Number(s)))));"
            % json.dumps([repr(value) for value in values])
        )
        completed = subprocess.run(
            ["node", "-e", script], capture_output=True, text=True, check=True
        )
        engine = json.loads(completed.stdout)
        mismatches = [
            (value, theirs, render_number(value))
            for value, theirs in zip(values, engine)
            if theirs != render_number(value)
        ]
        self.assertEqual(mismatches, [], f"{len(mismatches)} of {len(values)} differ")


class TiesRoundToEven(unittest.TestCase):
    """SPEC 4.5.3: a rendering tie rounds to even.

    A tie is a double whose exact value sits EXACTLY midway between the two
    shortest decimal spellings that both round-trip to it.  Kotlin shipped
    away-from-zero and emitted `1497898211722073.3` where V8 and Go emit
    `...073.2` -- the same double, different bytes, and nothing red anywhere,
    because every corpus tier re-renders a document that already spells its
    numbers one way.

    A neighbouring spelling that merely round-trips is NOT a tie: above 2^49 the
    double spacing exceeds 0.1, so several one-decimal spellings map to one
    double and only one of them is nearest.  These tests use exact `Decimal`
    arithmetic to tell the two apart, because the naive check reports ties that
    are not ties and would pass for a renderer that is simply wrong.
    """

    @staticmethod
    def _tie(value: float) -> "tuple[str, str] | None":
        import re
        from decimal import Decimal

        spelling = repr(value)
        if "e" in spelling:
            return None
        match = re.fullmatch(r"(-?)(\d+)(?:\.(\d+))?", spelling)
        if match is None:
            return None
        sign, whole, frac = match.group(1), match.group(2), match.group(3) or ""
        digits = whole + frac
        exact = Decimal(value)
        here = abs(Decimal(spelling) - exact)
        for delta in (-1, 1):
            shifted = str(int(digits) + delta).rjust(len(digits), "0")
            if len(shifted) != len(digits):
                continue
            head, tail = shifted[: len(whole)], shifted[len(whole) :]
            other = sign + head + ("." + tail if tail else "")
            if float(other) == value and abs(Decimal(other) - exact) == here:
                return spelling, other
        return None

    def test_the_case_kotlin_got_wrong(self) -> None:
        from decimal import Decimal

        value = 1497898211722073.2
        self.assertEqual(str(Decimal(value)), "1497898211722073.25", "not a tie")
        self.assertEqual(self._tie(value), ("1497898211722073.2", "1497898211722073.3"))
        self.assertEqual(render_number(value), "1497898211722073.2")

    def test_every_tie_found_takes_the_even_digit(self) -> None:
        import random
        import re

        # Around 1e15 the double spacing is 0.25, so exact values ending .25 or
        # .75 are genuine one-decimal ties.  That is the Kotlin case's magnitude
        # and the cheapest place to find them.
        rng = random.Random(7)
        ties: list[float] = []
        while len(ties) < 200:
            value = rng.uniform(1e15, 4.5e15)
            if rng.random() < 0.5:
                value = -value
            if self._tie(value):
                ties.append(value)

        odd = [
            value
            for value in ties
            if int(re.sub(r"[^0-9]", "", render_number(value))[-1]) % 2
        ]
        self.assertEqual(odd, [], f"{len(odd)} of {len(ties)} ties chose an odd digit")

    def test_a_tie_still_round_trips_whichever_way_it_went(self) -> None:
        """Guards against reading the test above as being about round-tripping.

        Both spellings of a tie round-trip; that is what makes it a tie, and it
        is why round-tripping cannot detect the defect.
        """
        value = 1497898211722073.2
        pair = self._tie(value)
        assert pair is not None
        for spelling in pair:
            self.assertEqual(float(spelling), value)

    def test_the_corpus_coordinate_is_a_phantom_and_not_a_tie(self) -> None:
        """Freezes the value the round-trip shortcut misreads at full precision.

        `-3.6712345678901235` appears in two accept files. Near 3.67 the spacing
        between adjacent doubles is about 4.4e-16 while the seventeenth
        significant digit steps by 1e-16, so three spellings map to that one
        double and only the middle one is nearest. The shortcut calls it a tie;
        exact arithmetic does not.

        Without this, the tie tests above cannot attribute their failures: if
        that coordinate were a tie, a mutated renderer would legitimately redden
        the accept tier too, and a mutation reddening four cases would get
        written up as a claim about ties. Found at this magnitude by the
        JavaScript seat, and at a different one by the search in
        test_every_tie_found_takes_the_even_digit.
        """
        value = -3.6712345678901235
        self.assertIsNone(self._tie(value), "this is a phantom, not a tie")
        for spelling in ("-3.6712345678901234", "-3.6712345678901235",
                         "-3.6712345678901236"):
            self.assertEqual(
                float(spelling), value, "all three must share the one double"
            )
        self.assertEqual(render_number(value), "-3.6712345678901235")

    def test_a_rounding_tie_is_not_a_rendering_tie(self) -> None:
        """`340.25` rounds as a tie at one place and renders uniquely.

        The two kinds of tie are different properties of a value, which is why
        Sections 1.3 and 1.5 of the guide are separate defects in separate code.
        """
        self.assertIsNone(self._tie(340.25))
        self.assertEqual(render_number(340.25), "340.25")
