"""The writer emit path (SPEC 4.1).

Nothing here is reachable by the accept, reject or diagnose tiers. Each of them
re-encodes a document that already satisfies the emit-boundary rules, so a
writer is only ever asked what a position should look like when it is emitted
from a model with no text behind it.

⚠ PRECISION IS NOT AN EMIT-BOUNDARY RULE. SPEC 4.1 does not constrain how
precisely a writer states a coordinate, so the write path passes a value
through unchanged. The rules that remain are the shortest spelling of a
position and SPEC 3.8's `hidden`.
"""

from __future__ import annotations

import unittest

import vnx
from vnx.encode import _position
from vnx.model import (
    AppKey,
    Arc,
    Area,
    Highlight,
    Document,
    Network,
    Pass,
    Position,
    Plan,
    Waypoint,
)
from vnx.writer import encode_bytes, normalised_document


def waypoint_at(
    lon: float,
    lat: float,
    ele: float | None = None,
    time_ms: float | None = None,
) -> Document:
    return Document(
        vnx="0.10.0", waypoints=[Waypoint(at=Position(lon, lat, ele, time_ms))]
    )


class TheEmitBoundary(unittest.TestCase):
    def source(self) -> bytes:
        path = vnx.__file__.rsplit("/vnx/", 1)[0]
        with open(f"{path}/../corpus/accept/full-precision-coordinate.vnx", "rb") as f:
            return f.read()

    def test_the_full_precision_case_writes_back_unchanged(self) -> None:
        """SPEC 4.1: the format does not constrain a writer's precision, so
        every digit the document stated comes back out."""
        document = vnx.read_bytes(self.source()).document
        self.assertIn(
            b"[-3.6712345678901235,57.11812345678901]", encode_bytes(document)
        )

    def test_the_reader_keeps_the_full_precision_value(self) -> None:
        """SPEC 4.1: a reader MUST NOT round."""
        document = vnx.read_bytes(self.source()).document
        assert document.waypoints is not None
        at = document.waypoints[0].at
        assert at is not None
        self.assertEqual(at.lon, -3.6712345678901235)

    def test_the_writer_does_not_touch_the_caller_s_model(self) -> None:
        """The `hidden` normalisation builds a new model rather than editing
        the caller's."""
        document = Document(vnx="0.10.0", areas=[Area(hidden=False)])
        encode_bytes(document)
        assert document.areas is not None
        self.assertIs(document.areas[0].hidden, False)

    def test_the_normalised_copy_is_a_different_document(self) -> None:
        document = waypoint_at(-3.6712345678901235, 57.118)
        self.assertIsNot(normalised_document(document), document)


class PrecisionPassesThrough(unittest.TestCase):
    """SPEC 4.1's precision paragraph is advice and carries no requirement.

    These are the values that used to prove the rounding: a tie at `340.25`,
    the scaled-multiplication trap at `0.15`, and a small negative that
    rounding would have turned into a signed zero. None of them moves now.
    """

    def emitted(self, lon: float, lat: float, ele: float | None = None) -> bytes:
        return encode_bytes(waypoint_at(lon, lat, ele))

    def test_seventeen_digits_survive(self) -> None:
        self.assertIn(
            b'"at":[-3.6712345678901235,57.11812345678901]',
            self.emitted(-3.6712345678901235, 57.11812345678901),
        )

    def test_an_elevation_tie_is_not_moved(self) -> None:
        self.assertIn(b'"at":[1.0,2.0,340.25]', self.emitted(1.0, 2.0, 340.25))

    def test_the_scaling_trap_values_are_not_moved(self) -> None:
        for value in (0.15, 0.05, 0.45):
            with self.subTest(value=value):
                self.assertIn(
                    f'"at":[1.0,2.0,{value}]'.encode(), self.emitted(1.0, 2.0, value)
                )

    def test_a_small_negative_keeps_its_sign_and_its_digits(self) -> None:
        """Rounding made a negative zero out of these; nothing makes one now."""
        self.assertIn(b'"at":[-4e-07,-4e-07,-0.04]', self.emitted(-4e-7, -4e-7, -0.04))

    def test_a_negative_zero_in_the_model_is_written_as_the_model_holds_it(
        self,
    ) -> None:
        """A caller that puts one in gets one out. No document carries a
        negative zero, so nothing but a caller's own arithmetic makes one."""
        self.assertIn(b'"at":[-0.0,2.0]', self.emitted(-0.0, 2.0))


class ShortestSpelling(unittest.TestCase):
    """SPEC 4.1: a writer emits the shortest spelling of a position.

    This is arity, not precision. It is the one emit-boundary rule left on a
    position, and `corpus/write/emit-boundary` is its fixture.
    """

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


class HiddenIsWrittenOnlyWhenTrue(unittest.TestCase):
    """SPEC 3.8. Absence already means visible, so a false carries nothing."""

    def test_a_false_hidden_is_dropped(self) -> None:
        document = Document(vnx="0.10.0", areas=[Area(hidden=False)])
        self.assertNotIn(b"hidden", encode_bytes(document))

    def test_a_true_hidden_is_kept(self) -> None:
        document = Document(vnx="0.10.0", areas=[Area(hidden=True)])
        self.assertIn(b'"hidden":true', encode_bytes(document))


class ApplicationKeysAreWrittenAsHeld(unittest.TestCase):
    """SPEC 3.9. A null is written as null, and a key the encoder cannot write
    is refused rather than lost."""

    def test_a_null_value_is_written(self) -> None:
        document = Document(vnx="0.10.0", app_keys=[AppKey("com.example.x", None)])
        self.assertEqual(
            encode_bytes(document), b'{"vnx":"0.10.0","com.example.x":null}'
        )

    def test_a_key_with_no_full_stop_is_refused(self) -> None:
        for key in ("rulesets", "folders", ""):
            with self.subTest(key=key):
                with self.assertRaisesRegex(ValueError, "no full stop"):
                    encode_bytes(Document(vnx="0.10.0", app_keys=[AppKey(key, [])]))

    def test_a_key_written_twice_is_refused(self) -> None:
        document = Document(vnx="0.10.0", app_keys=[AppKey("a.b", 1), AppKey("a.b", 2)])
        with self.assertRaisesRegex(ValueError, "twice"):
            encode_bytes(document)


class EncodeFileLeavesTheTargetOnRefusal(unittest.TestCase):
    """A refused encode must not cost the caller the file already there."""

    def test_a_refused_encode_leaves_the_existing_file_unchanged(self) -> None:
        import os
        import tempfile

        import vnx

        with tempfile.TemporaryDirectory() as directory:
            target = os.path.join(directory, "out.vnx")
            with open(target, "wb") as handle:
                handle.write(b"previous bytes")
            bad = Document(vnx="0.10.0", app_keys=[AppKey("nodot", 1)])
            with self.assertRaises(ValueError):
                vnx.encode_file(target, bad)
            with open(target, "rb") as handle:
                self.assertEqual(handle.read(), b"previous bytes")
            self.assertEqual(
                os.listdir(directory), ["out.vnx"], "a temporary file was left behind"
            )

    def test_a_good_encode_replaces_the_file(self) -> None:
        import os
        import tempfile

        import vnx

        with tempfile.TemporaryDirectory() as directory:
            target = os.path.join(directory, "out.vnx")
            with open(target, "wb") as handle:
                handle.write(b"previous bytes")
            document = Document(vnx="0.10.0", app_keys=[AppKey("a.b", None)])
            vnx.encode_file(target, document)
            with open(target, "rb") as handle:
                self.assertEqual(handle.read(), encode_bytes(document))
            self.assertEqual(os.listdir(directory), ["out.vnx"])


class AppKeyValueIsRequired(unittest.TestCase):
    def test_an_app_key_without_a_value_is_a_type_error(self) -> None:
        with self.assertRaises(TypeError):
            AppKey("a.b")  # type: ignore[call-arg]


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


def _network(walk: list[Pass], **overrides: object) -> Document:
    """A network whose arcs are `ab` (a to b) and `cb` (c to b)."""
    nodes = {
        "a": Position(-3.60, 57.10),
        "b": Position(-3.59, 57.10),
        "c": Position(-3.58, 57.10),
    }
    arcs = {
        "ab": Arc(from_="a", to="b", pips=[Position(-3.595, 57.101)]),
        "cb": Arc(from_="c", to="b", pips=[]),
    }
    network = Network(
        nodes=nodes, arcs=arcs, highlights=[Highlight(name="C", walk=walk)]
    )
    for key, value in overrides.items():
        setattr(network, key, value)
    return Document(networks=[network])


class TheWriterRefusesWhatItsReaderDrops(unittest.TestCase):
    """SPEC 3.3.2, 3.3.3, 3.4 and 3.7: a writer never emits an element its own
    reader would drop or repair.  Each refusal names the element and the rule.

    The walk, arc and ring checks are the reader's own functions
    (`vnx/walk.py`), so these tests pin that the writer calls them.
    """

    def refused(self, document: Document) -> str:
        with self.assertRaises(ValueError) as caught:
            encode_bytes(document)
        return str(caught.exception)

    def test_a_valid_walk_is_written(self) -> None:
        # A position start, a pinned pass leaving `cb` at c, and an out-and-back
        # turn on `cb` that leaves by c, the node it entered.
        document = _network(
            [
                Pass("ab", from_=Position(-3.597, 57.1005), to="b"),
                Pass("cb", pin=Position(-3.586, 57.1)),
                Pass("cb", turn=Position(-3.585, 57.10), to="c"),
            ]
        )
        encoded = encode_bytes(document)
        self.assertIn(b'"pin":[-3.586,57.1]', encoded)
        self.assertEqual(vnx.read_bytes(encoded).diagnostics, [])

    def test_a_pass_without_a_pin_writes_no_pin(self) -> None:
        encoded = encode_bytes(_network([Pass("ab", from_="a", to="b")]))
        self.assertNotIn(b"pin", encoded)

    def test_a_walk_that_breaks_a_rule_is_refused_naming_pass_and_code(self) -> None:
        for walk, code in (
            ([Pass("ghost", from_="a", to="b")], "pass-arc-absent"),
            ([Pass("ab", to="b")], "walk-spelling"),
            ([Pass("ab", from_="b"), Pass("cb", to="c")], "walk-not-connected"),
            (
                [
                    Pass(
                        "ab",
                        from_="a",
                        turn=Position(-3.595, 57.1),
                        pin=Position(-3.596, 57.1),
                        to="a",
                    )
                ],
                "pinned-turn",
            ),
            (
                [Pass("ab", from_=Position(-3.597, 57.1005, 400.0), to="b")],
                "extra-channel",
            ),
        ):
            with self.subTest(code=code):
                message = self.refused(_network(walk))
                self.assertIn(
                    "pass 0" if code != "walk-not-connected" else "pass 1", message
                )
                self.assertIn(code, message)

    def test_a_plan_carrying_a_position_is_refused(self) -> None:
        plan = Plan(
            name="R",
            nodes={"a": Position(-3.60, 57.10), "b": Position(-3.59, 57.10)},
            arcs={"ab": Arc(from_="a", to="b", pips=[])},
            walk=[Pass("ab", from_="a", to=Position(-3.595, 57.10))],
        )
        self.assertIn("plan-pin-or-position", self.refused(Document(plans=[plan])))
        plan.walk = [Pass("ab", from_="a", to="b")]
        self.assertEqual(
            vnx.read_bytes(encode_bytes(Document(plans=[plan]))).diagnostics, []
        )

    def test_a_self_loop_is_refused(self) -> None:
        arcs = {"loop": Arc(from_="a", to="a", pips=[])}
        self.assertIn("self-loop", self.refused(_network([], arcs=arcs)))

    def test_an_arc_naming_an_absent_node_is_refused(self) -> None:
        arcs = {"ax": Arc(from_="a", to="x", pips=[])}
        self.assertIn("'x'", self.refused(_network([], arcs=arcs)))

    def test_a_time_on_a_node_or_an_arc_pip_is_refused(self) -> None:
        nodes = {
            "a": Position(-3.60, 57.10, None, 1000.0),
            "b": Position(-3.59, 57.10),
            "c": Position(-3.58, 57.10),
        }
        self.assertIn("node 'a'", self.refused(_network([], nodes=nodes)))
        arcs = {
            "ab": Arc(from_="a", to="b", pips=[Position(-3.595, 57.101, None, 5.0)])
        }
        self.assertIn("arc 'ab'", self.refused(_network([], arcs=arcs)))

    def test_a_closed_ring_is_refused_not_opened(self) -> None:
        ring = [Position(0, 0), Position(1, 0), Position(1, 1), Position(0, 0)]
        self.assertIn("open", self.refused(Document(areas=[Area(polygon=ring)])))

    def test_a_ring_of_two_distinct_positions_is_refused(self) -> None:
        ring = [Position(0, 0), Position(1, 0), Position(1, 0, 5.0)]
        self.assertIn("3 distinct", self.refused(Document(areas=[Area(polygon=ring)])))


if __name__ == "__main__":
    unittest.main()
