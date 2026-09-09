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
