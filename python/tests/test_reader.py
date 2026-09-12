"""Unit tests for what the corpus does not reach.

Each test says what the corpus cannot see, because a test that duplicates a
corpus case adds nothing.
"""

from __future__ import annotations

import io
import unittest

import vnx

from . import tree

# A round trip through the model applies SPEC 4.1's emit-boundary rules, so a
# comparison against the source applies them to both sides.
_ROUND_TRIP = tree.Options(emit_boundary=True)
from vnx.decode import read_bytes
from vnx.errors import VNXRefusal


def doc(body: str) -> bytes:
    return body.encode("utf-8")


def read(body: str) -> vnx.ReadResult:
    return read_bytes(doc(body))


# A minimal VALID area: `polygon` is REQUIRED (SPEC 3.10), so a test about
# something else still has to supply one.
RING = '{"vnx": "0.1.0", "areas": [{"polygon": [[0,0],[1,0],[1,1],[0,0]]}]}'


def messages(result: vnx.ReadResult) -> list[tuple[str, str]]:
    return sorted((d.path, d.message) for d in result.diagnostics)


class Versions(unittest.TestCase):
    """SPEC 2.2.  The corpus covers a newer patch, a newer minor and a
    two-component version; these are the rest of the rule."""

    def test_a_matching_version_is_accepted(self) -> None:
        self.assertEqual(read('{"vnx": "0.1.0"}').document.vnx, "0.1.0")

    def test_a_lower_minor_is_refused(self) -> None:
        """SPEC 2.2: through 0.x each MINOR is ITS OWN SERIES.

        0.0 is not an older spelling of 0.1, and this reader has no rules for
        it -- "a lower minor is refused for the same reason as a higher one".
        Written as "greater than", which is what this library shipped, a 0.0.9
        document is read under 0.1 rules it was never written to.
        """
        with self.assertRaises(VNXRefusal) as caught:
            read('{"vnx": "0.0.9"}')
        message = str(caught.exception)
        # The version itself, which SPEC 2.2 requires, and the SERIES, so the
        # reader a user needs is named rather than only that something is wrong.
        self.assertIn("0.0.9", message)
        self.assertIn("0.0 series", message)

    def test_a_higher_minor_is_refused(self) -> None:
        """The counterpart direction, so a fix cannot swap one for the other."""
        with self.assertRaises(VNXRefusal) as caught:
            read('{"vnx": "0.2.0"}')
        self.assertIn("0.2.0", str(caught.exception))

    def test_any_patch_within_the_series_is_accepted(self) -> None:
        """SPEC 2.2: PATCH never refuses, in either direction."""
        for version in ("0.1.0", "0.1.5", "0.1.99"):
            with self.subTest(version=version):
                self.assertEqual(
                    read(f'{{"vnx": "{version}"}}').document.vnx, version
                )

    def test_a_newer_major_is_refused(self) -> None:
        with self.assertRaises(VNXRefusal) as caught:
            read('{"vnx": "1.0.0"}')
        self.assertIn("a newer reader is needed", str(caught.exception))

    def test_four_components_are_refused(self) -> None:
        with self.assertRaises(VNXRefusal) as caught:
            read('{"vnx": "0.1.0.0"}')
        self.assertIn("MAJOR.MINOR.PATCH", str(caught.exception))

    def test_a_non_numeric_component_is_refused(self) -> None:
        for bad in ("0.1.x", "0.1.+1", "0.1.", "v0.1.0", " 0.1.0"):
            with self.subTest(version=bad), self.assertRaises(VNXRefusal):
                read(f'{{"vnx": "{bad}"}}')

    def test_a_non_string_version_is_refused(self) -> None:
        with self.assertRaises(VNXRefusal) as caught:
            read('{"vnx": 0.1}')
        self.assertIn("MAJOR.MINOR.PATCH", str(caught.exception))

    def test_the_declared_version_is_written_back(self) -> None:
        # SPEC 2.2: a 0.1.0 reader that reads a 0.1.3 document writes `0.1.3`.
        document = read('{"vnx": "0.1.3"}').document
        self.assertIn(b'"vnx":"0.1.3"', vnx.encode_bytes(document))

    def test_the_top_level_must_be_an_object(self) -> None:
        with self.assertRaises(VNXRefusal) as caught:
            read("[]")
        self.assertIn("not a VNX document", str(caught.exception))


class NonFinite(unittest.TestCase):
    """SPEC 4.2.  The corpus carries the two literals; an overflowing decimal
    literal is well-formed JSON that is still not a finite binary64."""

    def test_an_overflowing_literal_is_refused(self) -> None:
        with self.assertRaises(VNXRefusal) as caught:
            read('{"vnx": "0.1.0", "waypoints": [{"at": [1e999, 0]}]}')
        self.assertIn("non-finite", str(caught.exception))

    def test_a_byte_order_mark_is_refused(self) -> None:
        with self.assertRaises(VNXRefusal):
            read_bytes(("\ufeff" + '{"vnx": "0.1.0"}').encode("utf-8"))


class UnknownKeys(unittest.TestCase):
    """SPEC 6.2.  The corpus diagnoses one unknown key on one waypoint."""

    def test_an_unknown_top_level_key_is_diagnosed(self) -> None:
        result = read('{"vnx": "0.1.0", "netwroks": []}')
        self.assertEqual(messages(result), [("/netwroks", "unknown key")])

    def test_the_item_survives(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "areas": [{"name": "A", "colour": "red",'
            ' "polygon": [[0,0],[1,0],[1,1],[0,0]]}]}'
        )
        self.assertEqual(messages(result), [("/areas/0/colour", "unknown key")])
        self.assertEqual(len(result.document.areas or []), 1)

    def test_a_style_body_has_no_unknown_keys(self) -> None:
        """Ruled 2026-09-09, reversing this implementation's first reading.

        A style used to be key-checked here, because it is an object this format
        defines and SPEC 6.2 reaches one.  Ruled opaque instead: a style
        vocabulary grows faster than a structural one, so key sets would have
        four implementations diagnosing a valid document every time Viatrail
        adds a style key.  This test is the superseded ruling rewritten, not a
        guard removed -- the assertion it makes now is the opposite one, and it
        would still catch a reader that dropped what it did not recognise.
        """
        result = read(
            '{"vnx": "0.1.0", "waypoints": [{"at": [0,0],'
            ' "style": {"color": "#fff", "colour": "#fff"}}]}'
        )
        self.assertEqual(messages(result), [])
        assert result.document.waypoints is not None
        self.assertEqual(
            result.document.waypoints[0].style, {"color": "#fff", "colour": "#fff"}
        )

    def test_an_unrecognised_style_key_survives_a_round_trip(self) -> None:
        source = '{"vnx":"0.1.0","waypoints":[{"at":[0,0],"style":{"future-key":"value"}}]}'
        document = read(source).document
        self.assertEqual(
            tree.compare_documents(
                vnx.encode_bytes(document), source.encode(), _ROUND_TRIP
            ),
            "",
        )

    def test_a_style_that_is_not_an_object_is_dropped(self) -> None:
        result = read('{"vnx": "0.1.0", "waypoints": [{"at": [0,0], "style": 3}]}')
        self.assertEqual(messages(result), [("/waypoints/0/style", "a style must be an object")])

    def test_a_data_record_body_has_no_unknown_keys(self) -> None:
        # SPEC 5.1.2: a record body is opaque apart from @type.
        result = read(
            '{"vnx": "0.1.0", "waypoints": [{"at": [0,0], "data":'
            ' [{"@type": "com.example.x", "anything": {"at": "all"}}]}]}'
        )
        self.assertEqual(messages(result), [])


class Positions(unittest.TestCase):
    """SPEC 4.1.  The corpus has no malformed position anywhere."""

    def test_a_short_position_drops_the_segment_not_the_pip(self) -> None:
        """SPEC 6.2: the container that holds the geometry goes.

        Dropping the pip would leave a one-pip segment that is shorter,
        plausible and wrong, with nothing in the document to say so.  The
        surviving segment is what proves the drop is scoped rather than total.
        """
        result = read(
            '{"vnx": "0.1.0", "tracks": [{"segments": ['
            '{"pips": [[-3.6, 57.1], [-3.6]]},'
            ' {"pips": [[-3.5, 57.2], [-3.4, 57.3]]}]}]}'
        )
        self.assertEqual(len(result.diagnostics), 1, messages(result))
        self.assertEqual(
            result.diagnostics[0].path, "/tracks/0/segments/0/pips/1"
        )
        self.assertEqual(result.diagnostics[0].code, "position-unusable")
        assert result.document.tracks is not None
        segments = result.document.tracks[0].segments
        assert segments is not None
        self.assertEqual(len(segments), 1)
        assert segments[0].pips is not None
        self.assertEqual(len(segments[0].pips), 2)

    def test_a_null_longitude_drops_the_segment(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "tracks": [{"segments": [{"pips":'
            ' [[null, 57.1, 3]]}]}]}'
        )
        self.assertEqual(len(result.diagnostics), 1)
        self.assertEqual(result.diagnostics[0].code, "position-unusable")
        assert result.document.tracks is not None
        self.assertEqual(result.document.tracks[0].segments, [])

    def test_the_channel_index_is_named_but_the_path_is_the_positions(self) -> None:
        """A pointer no other reader reports is a pointer no case can pin.

        SPEC 6.2's offending element is the position, so the path stops there
        and the channel is named in the English, which nothing compares.
        """
        result = read('{"vnx": "0.1.0", "waypoints": [{"at": [-3.6, "57.1"]}]}')
        self.assertEqual(result.diagnostics[0].path, "/waypoints/0/at")
        self.assertIn("channel 1", result.diagnostics[0].message)

    def test_a_string_channel_drops_the_whole_waypoint(self) -> None:
        """SPEC 6.2: an unusable value in a REQUIRED key drops the ITEM.

        `at` is REQUIRED (SPEC 3.10), and dropping only the field would leave a
        waypoint that is nowhere.  Exactly one diagnostic: the position reports
        where it failed, and the item's disappearance is that same defect rather
        than a second one.
        """
        result = read('{"vnx": "0.1.0", "waypoints": [{"at": [-3.6, "57.1"]}]}')
        self.assertEqual(len(result.diagnostics), 1, messages(result))
        self.assertEqual(result.document.waypoints, [])

    def test_a_boolean_channel_is_not_a_number(self) -> None:
        result = read('{"vnx": "0.1.0", "waypoints": [{"at": [-3.6, true]}]}')
        self.assertEqual(len(result.diagnostics), 1)

    def test_mixed_arity_within_one_array_is_legal(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "tracks": [{"segments": [{"pips":'
            ' [[-3.6, 57.1], [-3.6, 57.1, 3], [-3.6, 57.1, null, 5]]}]}]}'
        )
        self.assertEqual(messages(result), [])

    def test_a_fifth_channel_is_ignored_and_not_written_back(self) -> None:
        """SPEC 4.1: a reader MUST ignore a channel this version does not
        define, and MUST NOT report a diagnostic for it.

        Ignoring is what the rule says.  No rule asks a writer to reproduce
        one, so this library does not carry it in the model.
        """
        result = read('{"vnx": "0.1.0", "waypoints": [{"at": [-3.6, 57.1, 3, 5, 7]}]}')
        self.assertEqual(messages(result), [])
        self.assertIn(b'"at":[-3.6,57.1,3.0,5.0]', vnx.encode_bytes(result.document))


class Polygons(unittest.TestCase):
    """SPEC 3.7.  The corpus carries a nested ring and nothing else."""

    def test_a_ring_that_does_not_close_drops_the_area(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "areas": [{"polygon": [[0,0],[1,0],[1,1],[0,1]]}]}'
        )
        self.assertEqual(len(result.diagnostics), 1)
        self.assertIn("close", result.diagnostics[0].message)
        self.assertEqual(result.document.areas, [])

    def test_a_ring_of_three_positions_drops_the_area(self) -> None:
        result = read('{"vnx": "0.1.0", "areas": [{"polygon": [[0,0],[1,0],[0,0]]}]}')
        self.assertEqual(len(result.diagnostics), 1)
        self.assertEqual(result.document.areas, [])


class Chains(unittest.TestCase):
    """SPEC 3.3.3 and the diagnose tier."""

    NETWORK = """
    {"vnx": "0.1.0", "networks": [{
      "nodes": {"a": [-3.68, 57.12], "b": [-3.66, 57.10],
                "c": [-3.20, 56.10], "d": [-3.19, 56.09]},
      "arcs": {
        "ab": {"from": "a", "to": "b",
               "pips": [[-3.68, 57.12], [-3.67, 57.11], [-3.66, 57.10]]},
        "ba": {"from": "b", "to": "a",
               "pips": [[-3.66, 57.10], [-3.67, 57.11], [-3.68, 57.12]]},
        "cd": {"from": "c", "to": "d",
               "pips": [[-3.20, 56.10], [-3.19, 56.09]]}
      },
      "chains": [{"links": %s}]
    }]}
    """

    def chain(self, links: str) -> vnx.ReadResult:
        return read(self.NETWORK % links)

    def test_one_dropped_link_reports_one_diagnostic(self) -> None:
        """A three-link chain whose MIDDLE link dangles.

        `corpus/diagnose/link-names-absent-arc.vnx` claims to test this -- its
        note says "dropping the second link must not then also report the chain
        as broken" -- but its chain has two links, so after the drop there is no
        consecutive pair left for any reader to compare.  The property is only
        observable from three links up, and only when the two survivors would
        NOT have joined each other: the walk went a -> b, then through the arc
        that is missing, and arrives somewhere else entirely.
        """
        result = self.chain(
            '[{"arc": "ab", "from": "a", "to": "b"},'
            ' {"arc": "ghost", "from": "b", "to": "a"},'
            ' {"arc": "cd", "from": "c", "to": "d"}]'
        )
        self.assertEqual(len(result.diagnostics), 1, messages(result))
        self.assertIn("not in this network", result.diagnostics[0].message)

    def test_a_break_is_reported_on_the_second_of_the_pair(self) -> None:
        result = self.chain(
            '[{"arc": "ab", "from": "a", "to": "b"},'
            ' {"arc": "cd", "from": "c", "to": "d"}]'
        )
        paths = [d.path for d in result.diagnostics if "connected walk" in d.message]
        self.assertEqual(paths, ["/networks/0/chains/0/links/1"])

    def test_a_node_key_and_an_inline_position_at_it_are_the_same_endpoint(self) -> None:
        # Not stated by SPEC 3.3.3, which scopes value-equality to two inline
        # positions.  Comparing by resolved value cannot manufacture a break.
        result = self.chain(
            '[{"arc": "ab", "from": "a", "to": "b"},'
            ' {"arc": "ba", "from": [-3.66, 57.10], "to": "a"}]'
        )
        self.assertEqual(messages(result), [])

    def test_an_inline_endpoint_off_its_arc_is_dropped(self) -> None:
        result = self.chain('[{"arc": "ab", "from": "a", "to": [-3.0, 57.0]}]')
        self.assertEqual(len(result.diagnostics), 1)
        self.assertIn("does not lie on arc", result.diagnostics[0].message)

    def test_an_inline_endpoint_within_tolerance_is_kept(self) -> None:
        # About 30 m off the arc: inside the 50 m tolerance of SPEC 3.3.3.
        result = self.chain('[{"arc": "ab", "from": "a", "to": [-3.6695, 57.11]}]')
        self.assertEqual(messages(result), [])

    def test_a_link_endpoint_naming_an_absent_node_is_diagnosed(self) -> None:
        result = self.chain('[{"arc": "ab", "from": "a", "to": "zz"}]')
        self.assertEqual(
            messages(result),
            [("/networks/0/chains/0/links/0/to", "not a node in this network")],
        )


class TerminalPips(unittest.TestCase):
    """SPEC 3.3.2: a terminal pip and its node are one fact written twice.

    This was a MUST with no fixture and no code when it went in, and the
    corpus gained `diagnose/terminal-pip-off-node.vnx` shortly after, which is
    where the code comes from.  These tests stay: the fixture pins one arc at
    one distance, and the boundary cases below are not in it.
    """

    def network(self, arc: str) -> vnx.ReadResult:
        return read(
            '{"vnx": "0.1.0", "networks": [{"nodes":'
            ' {"a": [-3.68, 57.12], "b": [-3.66, 57.10]},'
            ' "arcs": {"ab": %s}}]}' % arc
        )

    def test_a_node_far_from_its_terminal_pip_is_diagnosed(self) -> None:
        result = self.network(
            '{"from": "a", "to": "b", "pips": [[-3.2, 56.1], [-3.66, 57.10]]}'
        )
        self.assertEqual(
            [d.path for d in result.diagnostics], ["/networks/0/arcs/ab/from"]
        )
        self.assertIn("beyond the 50 m tolerance", result.diagnostics[0].message)
        self.assertEqual(result.diagnostics[0].code, "terminal-pip-off-node")

    def test_the_pips_stay_authoritative_and_both_are_kept(self) -> None:
        """Nothing is corrected: the arc survives with the geometry it wrote."""
        result = self.network(
            '{"from": "a", "to": "b", "pips": [[-3.2, 56.1], [-3.66, 57.10]]}'
        )
        networks = result.document.networks
        assert networks is not None
        arcs, nodes = networks[0].arcs, networks[0].nodes
        assert arcs is not None and nodes is not None
        assert arcs["ab"].pips is not None
        self.assertEqual(arcs["ab"].pips[0].lon, -3.2)
        self.assertEqual(nodes["a"].lon, -3.68)

    def test_a_node_within_tolerance_is_silent(self) -> None:
        # About 30 m south of node `a`, inside the tolerance SPEC 3.3.2 shares
        # with SPEC 3.3.3 -- deliberately the same 50 metres.
        result = self.network(
            '{"from": "a", "to": "b", "pips": [[-3.68, 57.11973], [-3.66, 57.10]]}'
        )
        self.assertEqual(messages(result), [])

    def test_an_absent_node_reports_once_not_twice(self) -> None:
        """There is no second position to compare against, so no second note."""
        result = self.network(
            '{"from": "ghost", "to": "b", "pips": [[-3.2, 56.1], [-3.66, 57.10]]}'
        )
        self.assertEqual(
            messages(result),
            [("/networks/0/arcs/ab/from", "not a node in this network")],
        )


class Timestamps(unittest.TestCase):
    """SPEC 4.3.  The corpus carries one offset timestamp on a track."""

    def test_fractional_seconds_are_accepted(self) -> None:
        result = read('{"vnx": "0.1.0", "created": "2026-08-31T06:45:12.5Z"}')
        self.assertEqual(messages(result), [])

    def test_a_bare_date_is_dropped(self) -> None:
        result = read('{"vnx": "0.1.0", "created": "2026-08-31"}')
        self.assertEqual(messages(result), [("/created", "must be UTC with a Z suffix")])
        self.assertIsNone(result.document.created)

    def test_a_lower_case_z_is_dropped(self) -> None:
        result = read('{"vnx": "0.1.0", "created": "2026-08-31T06:45:12z"}')
        self.assertEqual(len(result.diagnostics), 1)


class MovingTime(unittest.TestCase):
    """SPEC 3.5.  The corpus covers all-timed and never-timed tracks."""

    def track(self, segments: str) -> vnx.Track:
        result = read('{"vnx": "0.1.0", "tracks": [{"segments": %s}]}' % segments)
        assert result.document.tracks is not None
        return result.document.tracks[0]

    def test_a_single_pip_segment_contributes_nothing(self) -> None:
        track = self.track('[{"pips": [[0,0,null,1000]]}]')
        self.assertEqual(track.moving_time_ms(), 0.0)

    def test_a_segment_whose_END_PIPS_lack_times_is_unknown(self) -> None:
        """Ruled 2026-09-09: derivable only when every segment carries a time on
        its FIRST and LAST pip. This library previously took the first and last
        TIMED pip and answered 3000 here."""
        track = self.track('[{"pips": [[0,0], [0,0,null,1000], [0,0,null,4000], [0,0]]}]')
        self.assertIsNone(track.moving_time_ms())

    def test_one_untimed_segment_makes_the_whole_track_unknown(self) -> None:
        """A sum over the timed segments is indistinguishable from a complete
        one, and no consumer can tell which it was given."""
        track = self.track(
            '[{"pips": [[0,0,null,1000], [0,0,null,4000]]},'
            ' {"pips": [[0,0], [0,0]]}]'
        )
        self.assertIsNone(track.moving_time_ms())

    def test_every_segment_timed_still_sums(self) -> None:
        track = self.track(
            '[{"pips": [[0,0,null,1000], [0,0,null,4000]]},'
            ' {"pips": [[0,0,null,9000], [0,0,null,9500]]}]'
        )
        self.assertEqual(track.moving_time_ms(), 3500.0)

    def test_an_empty_segment_makes_it_unknown(self) -> None:
        track = self.track('[{"pips": [[0,0,null,1000], [0,0,null,4000]]}, {"pips": []}]')
        self.assertIsNone(track.moving_time_ms())

    def test_no_segments_is_unknown(self) -> None:
        self.assertIsNone(self.track("[]").moving_time_ms())


class HiddenAndAbsence(unittest.TestCase):
    """SPEC 3.1, 3.6 and 3.8.  Absence is not a default."""

    def test_hidden_false_survives_the_model(self) -> None:
        """A reader keeps the false it was given.

        A WRITER omits it, because absence already means visible (SPEC 3.8),
        and `test_the_writer_omits_a_false_hidden` below pins that half.
        """
        source = (
            '{"vnx":"0.1.0","areas":[{"hidden":false,'
            '"polygon":[[0,0],[1,0],[1,1],[0,0]]}]}'
        )
        document = read(source).document
        assert document.areas is not None
        self.assertIs(document.areas[0].hidden, False)

    def test_the_writer_omits_a_false_hidden(self) -> None:
        source = (
            '{"vnx":"0.1.0","areas":[{"hidden":false,'
            '"polygon":[[0,0],[1,0],[1,1],[0,0]]}]}'
        )
        self.assertNotIn(b"hidden", vnx.encode_bytes(read(source).document))

    def test_an_absent_hidden_is_not_false(self) -> None:
        document = read(RING).document
        assert document.areas is not None
        self.assertIsNone(document.areas[0].hidden)

    def test_an_absent_label_visible_is_not_false(self) -> None:
        document = read('{"vnx": "0.1.0", "waypoints": [{"at": [0,0], "style": {}}]}').document
        assert document.waypoints is not None
        self.assertNotIn("label-visible", document.waypoints[0].style or {})

    def test_an_empty_collection_is_not_an_absent_one_in_the_model(self) -> None:
        """SPEC 3.1 has a reader treat the two alike, and the model still tells
        them apart: `None` is absent, `[]` is present and empty."""
        self.assertEqual(read('{"vnx": "0.1.0", "areas": []}').document.areas, [])
        self.assertIsNone(read('{"vnx": "0.1.0"}').document.areas)

    def test_a_non_boolean_hidden_is_dropped(self) -> None:
        result = read(RING.replace('{"polygon"', '{"hidden": "yes", "polygon"'))
        self.assertEqual(len(result.diagnostics), 1)
        self.assertEqual(result.diagnostics[0].path, "/areas/0/hidden")
        self.assertEqual(result.diagnostics[0].code, "wrong-type-optional")
        assert result.document.areas is not None
        self.assertEqual(len(result.document.areas), 1, "the item is kept")


class Records(unittest.TestCase):
    """SPEC 5."""

    def test_a_record_without_a_type_is_dropped(self) -> None:
        result = read('{"vnx": "0.1.0", "waypoints": [{"at": [0,0], "data": [{"value": 1}]}]}')
        self.assertEqual(len(result.diagnostics), 1)
        assert result.document.waypoints is not None
        self.assertEqual(result.document.waypoints[0].data, [])

    def test_a_type_differing_only_by_case_is_a_different_type(self) -> None:
        # SPEC 5.3: a @type value is compared exactly everywhere except the
        # namespace test, so `App.viatrail.tag` is not `app.viatrail.tag`.
        document = read(
            '{"vnx": "0.1.0", "waypoints": [{"at": [0,0], "data":'
            ' [{"@type": "App.viatrail.tag", "value": "x"}]}]}'
        ).document
        assert document.waypoints is not None
        data = document.waypoints[0].data
        assert data is not None
        self.assertEqual(data[0].type, "App.viatrail.tag")

    def test_several_records_of_one_type_are_all_kept(self) -> None:
        # SPEC 5.1.1: data is a bag, not a map; three tags are three records.
        document = read(
            '{"vnx": "0.1.0", "waypoints": [{"at": [0,0], "data": ['
            '{"@type": "app.viatrail.tag", "value": "a"},'
            '{"@type": "app.viatrail.tag", "value": "b"},'
            '{"@type": "app.viatrail.tag", "value": "c"}]}]}'
        ).document
        assert document.waypoints is not None
        self.assertEqual(len(document.waypoints[0].data or []), 3)


class Identifiers(unittest.TestCase):
    """SPEC 4.4: identifiers are scoped to the object that contains them."""

    def test_two_networks_may_use_the_same_node_key(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "networks": ['
            '{"nodes": {"n1": [0,0]}, "arcs": {"a": {"from": "n1", "to": "n1",'
            ' "pips": [[0,0],[1,1],[0,0]]}}},'
            '{"nodes": {"n1": [9,9]}, "arcs": {"a": {"from": "n1", "to": "n1",'
            ' "pips": [[9,9],[8,8],[9,9]]}}}]}'
        )
        self.assertEqual(messages(result), [])
        networks = result.document.networks
        assert networks is not None
        first, second = networks[0].nodes, networks[1].nodes
        assert first is not None and second is not None
        self.assertEqual(first["n1"].lon, 0.0)
        self.assertEqual(second["n1"].lon, 9.0)

    def test_a_link_cannot_reach_another_networks_arc(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "networks": ['
            '{"nodes": {"n1": [0,0]}, "arcs": {"only-here": {"from": "n1",'
            ' "to": "n1", "pips": [[0,0],[1,1],[0,0]]}}},'
            '{"chains": [{"links": [{"arc": "only-here"}]}]}]}'
        )
        self.assertEqual(
            messages(result),
            [("/networks/1/chains/0/links/0", "not in this network: this link "
              "names no arc it can traverse")],
        )


class Containers(unittest.TestCase):
    """SPEC 7 and 9.  The corpus has four archives and no writer case."""

    def build(self, entries: list[tuple[str, bytes]]) -> bytes:
        import zipfile

        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as archive:
            for name, body in entries:
                archive.writestr(name, body)
        return buffer.getvalue()

    DOC = b'{"vnx": "0.1.0"}'

    def test_an_absolute_path_is_refused(self) -> None:
        with self.assertRaises(VNXRefusal) as caught:
            vnx.read_container_bytes(
                self.build([("doc.vnx", self.DOC), ("/etc/passwd", b"x")])
            )
        self.assertIn("escapes the archive", str(caught.exception))

    def test_a_drive_letter_is_refused(self) -> None:
        with self.assertRaises(VNXRefusal) as caught:
            vnx.read_container_bytes(
                self.build([("doc.vnx", self.DOC), ("C:/windows/x", b"x")])
            )
        self.assertIn("escapes the archive", str(caught.exception))

    def test_an_archive_with_no_vnx_entry_is_refused(self) -> None:
        with self.assertRaises(VNXRefusal) as caught:
            vnx.read_container_bytes(self.build([("readme.txt", b"x")]))
        self.assertIn("not a VNX document", str(caught.exception))

    def test_doc_vnx_wins_over_an_earlier_vnx_entry(self) -> None:
        # SPEC 7: prefer doc.vnx at the root; the fallback is only a fallback.
        result = vnx.read_container_bytes(
            self.build([
                ("aaa.vnx", b'{"vnx": "0.1.0", "areas": []}'),
                ("doc.vnx", b'{"vnx": "0.1.0", "folders": []}'),
            ])
        )
        self.assertEqual(result.document_name, "doc.vnx")
        self.assertEqual(result.document.folders, [])
        self.assertEqual(
            result.attachments,
            [vnx.Attachment("aaa.vnx", b'{"vnx": "0.1.0", "areas": []}')],
        )

    def test_the_match_is_on_the_full_entry_name(self) -> None:
        # A doc.vnx at depth is not the root document.  Matching on the base
        # name would match at any depth.
        result = vnx.read_container_bytes(
            self.build([
                ("nested/doc.vnx", b'{"vnx": "0.1.0", "areas": []}'),
                ("zzz.vnx", b'{"vnx": "0.1.0", "folders": []}'),
            ])
        )
        self.assertEqual(result.document_name, "nested/doc.vnx")

    def test_names_are_compared_case_insensitively(self) -> None:
        result = vnx.read_container_bytes(self.build([("DOC.VNX", self.DOC)]))
        self.assertEqual(result.document_name, "DOC.VNX")

    def test_a_written_container_reads_back(self) -> None:
        document = vnx.read_bytes(b'{"vnx": "0.1.0", "waypoints": [{"at": [1, 2]}]}').document
        data = vnx.encode_container_bytes(document, {"attachments/p.txt": b"x"})
        result = vnx.read_container_bytes(data)
        self.assertEqual(
            result.attachments, [vnx.Attachment("attachments/p.txt", b"x")]
        )
        assert result.document.waypoints is not None
        at = result.document.waypoints[0].at
        assert at is not None
        self.assertEqual(at.lon, 1.0)

    def test_a_written_container_sets_the_utf8_flag(self) -> None:
        import io
        import zipfile

        document = vnx.read_bytes(b'{"vnx": "0.1.0"}').document
        with zipfile.ZipFile(io.BytesIO(vnx.encode_container_bytes(document))) as archive:
            for info in archive.infolist():
                self.assertTrue(info.flag_bits & 0x800, info.filename)


class RuledStyleValues(unittest.TestCase):
    """SPEC 3.6's two ranges, ruled 2026-09-09.

    `importance` is normative and `icon-size`'s list of the sizes Viatrail uses
    is informative.  No corpus case reaches either.
    """

    def style(self, body: str) -> vnx.ReadResult:
        return read('{"vnx": "0.1.0", "waypoints": [{"at": [0,0], "style": %s}]}' % body)

    def test_importance_in_range_is_kept(self) -> None:
        for value in (1, 3, 5):
            with self.subTest(value=value):
                result = self.style('{"importance": %d}' % value)
                self.assertEqual(messages(result), [])

    def test_importance_out_of_range_is_dropped(self) -> None:
        for value in ("0", "6", "-1", "2.5", '"3"', "true", "null"):
            with self.subTest(value=value):
                result = self.style('{"importance": %s}' % value)
                self.assertEqual(
                    messages(result),
                    [("/waypoints/0/style/importance", "must be an integer from 1 to 5")],
                )
                assert result.document.waypoints is not None
                style = result.document.waypoints[0].style
                assert style is not None
                self.assertNotIn("importance", style)

    def test_the_item_and_the_rest_of_the_style_survive(self) -> None:
        result = self.style('{"importance": 9, "color": "#fff"}')
        assert result.document.waypoints is not None
        self.assertEqual(result.document.waypoints[0].style, {"color": "#fff"})

    def test_icon_size_is_informative_and_not_checked(self) -> None:
        # SPEC 3.6 names 16, 21, 27, 35 and 46 as "the values Viatrail uses".
        self.assertEqual(messages(self.style('{"icon-size": 19}')), [])


class RuledFolderScope(unittest.TestCase):
    """SPEC 3.2's "any item" does not reach a network's own points and areas.

    Ruled 2026-09-09, conditional on Viatrail's exporter not writing one.
    """

    def test_a_top_level_waypoint_may_carry_a_folder(self) -> None:
        self.assertEqual(
            messages(read('{"vnx": "0.1.0", "folders": [{"id": "f"}], "waypoints":'
                          ' [{"at": [0,0], "folder": "f"}]}')), []
        )

    def test_a_network_point_may_not(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "networks":'
            ' [{"points": [{"at": [0,0], "folder": "f"}]}]}'
        )
        self.assertEqual(
            messages(result), [("/networks/0/points/0/folder", "unknown key")]
        )

    def test_a_network_area_may_not(self) -> None:
        result = read('{"vnx": "0.1.0", "networks": [{"areas":'
                      ' [{"folder": "f", "polygon": [[0,0],[1,0],[1,1],[0,0]]}]}]}')
        self.assertEqual(
            messages(result), [("/networks/0/areas/0/folder", "unknown key")]
        )

    def test_a_top_level_area_may(self) -> None:
        self.assertEqual(
            messages(read(
                RING.replace('"areas"', '"folders": [{"id": "f"}], "areas"')
                    .replace('{"polygon"', '{"folder": "f", "polygon"')
            )), []
        )

    def test_the_two_sets_differ_by_exactly_folder(self) -> None:
        """Guards the ruling against a future edit that reunifies the sets.

        Without this, `WAYPOINT_IN_NETWORK = WAYPOINT` would leave every test
        above green except the two that name a network, and the ruling would
        quietly stop being implemented.
        """
        from vnx import keys

        self.assertEqual(keys.WAYPOINT - keys.WAYPOINT_IN_NETWORK, {"folder"})
        self.assertEqual(keys.AREA - keys.AREA_IN_NETWORK, {"folder"})


class RequiredKeys(unittest.TestCase):
    """SPEC 3.10's Required column, and SPEC 6.2's remedy for it.

    "A key marked REQUIRED must be present for the object to exist."  No corpus
    case omits one, so every assertion here is against the table rather than
    against a fixture.
    """

    def test_a_folder_without_an_id_is_dropped(self) -> None:
        result = read('{"vnx": "0.1.0", "folders": [{"name": "Nameless"}]}')
        self.assertEqual(messages(result), [("/folders/0/id", "a folder must carry `id`")])
        self.assertEqual(result.document.folders, [])

    def test_a_waypoint_without_a_position_is_dropped(self) -> None:
        result = read('{"vnx": "0.1.0", "waypoints": [{"name": "Nowhere"}]}')
        self.assertEqual(len(result.diagnostics), 1)
        self.assertEqual(result.document.waypoints, [])

    def test_an_area_without_a_polygon_is_dropped(self) -> None:
        result = read('{"vnx": "0.1.0", "areas": [{"name": "Shapeless"}]}')
        self.assertEqual(result.document.areas, [])

    def test_a_track_without_segments_is_dropped(self) -> None:
        result = read('{"vnx": "0.1.0", "tracks": [{"name": "No geometry"}]}')
        self.assertEqual(result.document.tracks, [])

    def test_an_arc_without_pips_is_dropped_and_the_network_survives(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "networks": [{"name": "N", "nodes": {"a": [0,0]},'
            ' "arcs": {"bad": {"from": "a", "to": "a"}}}]}'
        )
        assert result.document.networks is not None
        self.assertEqual(result.document.networks[0].arcs, {})
        self.assertEqual(len(result.document.networks), 1)

    def test_a_link_without_endpoints_is_dropped(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "networks": [{"nodes": {"a": [0,0], "b": [1,1]},'
            ' "arcs": {"x": {"from": "a", "to": "b", "pips": [[0,0],[1,1]]}},'
            ' "chains": [{"links": [{"arc": "x"}]}]}]}'
        )
        assert result.document.networks is not None
        chains = result.document.networks[0].chains
        assert chains is not None
        self.assertEqual(chains[0].links, [])

    def test_an_empty_required_array_is_present(self) -> None:
        """SPEC 3.10: requiring a key does not require a non-empty value."""
        result = read(
            '{"vnx": "0.1.0", "networks": [{"chains": [{"links": []}]}],'
            ' "tracks": [{"segments": []}]}'
        )
        self.assertEqual(messages(result), [])
        assert result.document.tracks is not None
        self.assertEqual(result.document.tracks[0].segments, [])

    def test_an_absent_key_and_an_unusable_one_each_report_once(self) -> None:
        """One defect, one diagnostic, whichever way the key fails."""
        absent = read('{"vnx": "0.1.0", "waypoints": [{}]}')
        unusable = read('{"vnx": "0.1.0", "waypoints": [{"at": "here"}]}')
        self.assertEqual(len(absent.diagnostics), 1, messages(absent))
        self.assertEqual(len(unusable.diagnostics), 1, messages(unusable))


class DanglingFolder(unittest.TestCase):
    """SPEC 6.2: a `folder` naming a folder that is not in the document."""

    def test_the_item_survives_at_the_top_level(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "waypoints": [{"at": [0,0], "folder": "ghost"}]}'
        )
        self.assertEqual(
            messages(result),
            [("/waypoints/0/folder", "no folder `ghost` in this document")],
        )
        assert result.document.waypoints is not None
        self.assertEqual(len(result.document.waypoints), 1)
        self.assertIsNone(result.document.waypoints[0].folder)

    def test_a_folder_written_after_the_item_that_names_it_is_found(self) -> None:
        # SPEC 4.4: element order carries no meaning, so the check is a second
        # pass rather than a running one.
        result = read(
            '{"vnx": "0.1.0", "waypoints": [{"at": [0,0], "folder": "f"}],'
            ' "folders": [{"id": "f"}]}'
        )
        self.assertEqual(messages(result), [])

    def test_a_dropped_folder_leaves_its_contents_at_the_top_level(self) -> None:
        """SPEC 6.2's worked case: a folder with no `id` is dropped, and the
        items that named it are then covered by the dangling-folder rule."""
        result = read(
            '{"vnx": "0.1.0", "folders": [{"name": "No id"}],'
            ' "tracks": [{"segments": [], "folder": "whatever"}]}'
        )
        self.assertEqual(len(result.diagnostics), 2, messages(result))
        assert result.document.tracks is not None
        self.assertEqual(len(result.document.tracks), 1)
        self.assertIsNone(result.document.tracks[0].folder)


class ContainerPathRules(unittest.TestCase):
    """SPEC 7's path and duplicate rules, at the edges no archive reaches.

    The corpus carries one `..` archive and one exact-duplicate archive. Every
    case below is outside both.
    """

    DOC = b'{"vnx": "0.1.0"}'

    def build(self, names: list[str]) -> bytes:
        import zipfile

        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as archive:
            archive.writestr("doc.vnx", self.DOC)
            for name in names:
                archive.writestr(name, b"x")
        return buffer.getvalue()

    def refuses(self, name: str) -> str:
        with self.assertRaises(vnx.VNXRefusal, msg=f"{name!r} was accepted") as caught:
            vnx.read_container_bytes(self.build([name]))
        return str(caught.exception)

    def test_a_dot_dot_segment_is_refused(self) -> None:
        self.assertIn("escapes the archive", self.refuses("../x"))

    def test_a_backslash_anywhere_is_refused(self) -> None:
        """Ruled 2026-09-09, and deliberately stricter than the ZIP appnote.

        No VNX archive has a legitimate use for a backslash in an entry name, and
        on a Windows-authored archive `..\\x` is a real escape. Refusing the
        character outright means "is a backslash a separator while hunting for
        the `..` segment" never has to be answered.

        This library first shipped the narrower reading, splitting on `\\` for
        the segment hunt alone, which accepted `weird\\name.jpg`.
        """
        for name in ("..\\x", "a\\..\\b", "weird\\name.jpg", "sub\\photo.jpg"):
            with self.subTest(name=name):
                self.assertIn("backslash", self.refuses(name))

    def test_the_backslash_and_segment_rules_do_not_collapse(self) -> None:
        """Two independent rules, so neither absorbs the other."""
        self.assertIn("backslash", self.refuses("a\\b.jpg"))
        self.assertIn("`..` path segment", self.refuses("../x"))
        result = vnx.read_container_bytes(self.build(["a..b/photo.jpg"]))
        self.assertEqual(
            result.attachments, [vnx.Attachment("a..b/photo.jpg", b"x")]
        )

    def test_the_writer_refuses_a_name_it_would_refuse_to_read(self) -> None:
        document = vnx.read_bytes(self.DOC).document
        for name in ("weird\\name.jpg", "../x", "/abs", "C:/x"):
            with self.subTest(name=name), self.assertRaises(vnx.VNXRefusal):
                vnx.encode_container_bytes(document, {name: b"x"})

    def test_a_dot_dot_substring_is_legal(self) -> None:
        """SPEC 7 names `a..b/photo.jpg` legal: `..` is a segment, not a
        substring. A reader that searched for the substring would refuse a
        document the format declares valid, and no archive in the corpus would
        catch it."""
        result = vnx.read_container_bytes(self.build(["a..b/photo.jpg"]))
        self.assertEqual(
            result.attachments, [vnx.Attachment("a..b/photo.jpg", b"x")]
        )

    def test_names_differing_only_by_case_are_duplicates(self) -> None:
        """SPEC 7 compares duplicate names case-insensitively. A
        case-insensitive filesystem makes these one file, so an archive carrying
        both would extract to different documents on different platforms."""
        import zipfile

        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as archive:
            archive.writestr("doc.vnx", self.DOC)
            archive.writestr("DOC.VNX", self.DOC)
        with self.assertRaises(vnx.VNXRefusal) as caught:
            vnx.read_container_bytes(buffer.getvalue())
        message = str(caught.exception)
        self.assertIn("duplicate entry", message)
        self.assertIn("only by case", message)

    def test_an_exact_duplicate_does_not_claim_a_case_difference(self) -> None:
        import zipfile

        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as archive:
            archive.writestr("a.txt", b"x")
            archive.writestr("a.txt", b"y")
            archive.writestr("doc.vnx", self.DOC)
        with self.assertRaises(vnx.VNXRefusal) as caught:
            vnx.read_container_bytes(buffer.getvalue())
        self.assertIn("duplicate entry", str(caught.exception))
        self.assertNotIn("only by case", str(caught.exception))


class ContainerCompression(unittest.TestCase):
    """SPEC 7's compression remedy, which depends on which entry it is.

    No corpus archive uses a method other than stored or deflate, so nothing
    here is reachable by the corpus.
    """

    DOC = b'{"vnx": "0.1.0"}'

    def archive(self, doc_method: int, attachment_method: int) -> bytes:
        import zipfile

        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as handle:
            for name, body, method in (
                ("doc.vnx", self.DOC, doc_method),
                ("attachments/photo.txt", b"x", attachment_method),
            ):
                info = zipfile.ZipInfo(name)
                info.compress_type = method
                handle.writestr(info, body)
        return buffer.getvalue()

    def test_stored_and_deflate_are_both_read(self) -> None:
        import zipfile

        for method in (zipfile.ZIP_STORED, zipfile.ZIP_DEFLATED):
            with self.subTest(method=method):
                result = vnx.read_container_bytes(self.archive(method, method))
                self.assertEqual(
                    result.attachments,
                    [vnx.Attachment("attachments/photo.txt", b"x")],
                )
                self.assertEqual(result.diagnostics, [])

    def test_an_unreadable_root_document_refuses_the_archive(self) -> None:
        import zipfile

        with self.assertRaises(vnx.VNXRefusal) as caught:
            vnx.read_container_bytes(
                self.archive(zipfile.ZIP_BZIP2, zipfile.ZIP_STORED)
            )
        self.assertIn("root document", str(caught.exception))

    def test_an_unreadable_attachment_is_dropped_and_reported(self) -> None:
        """Refusing a whole library over one photograph would be
        disproportionate, so the document is returned without it."""
        import zipfile

        result = vnx.read_container_bytes(
            self.archive(zipfile.ZIP_STORED, zipfile.ZIP_BZIP2)
        )
        self.assertEqual(result.attachments, [])
        self.assertEqual(len(result.diagnostics), 1)
        self.assertIn("attachments/photo.txt", result.diagnostics[0].message)
        self.assertEqual(result.diagnostics[0].code, "attachment-unreadable")
        # SPEC 6.3's separate `#/entries/` space, with the entry name as ONE
        # reference token -- so the slash inside it is escaped.
        self.assertEqual(
            result.diagnostics[0].path, "#/entries/attachments~1photo.txt"
        )
        self.assertIsNotNone(result.document)


class ContainerCorruptAttachment(unittest.TestCase):
    """SPEC 7: a usable METHOD whose BYTES will not come back.

    The same remedy as an unusable method -- drop the attachment, record a
    diagnostic, return the document -- and it is a MUST in both directions: the
    archive MUST NOT be refused, and the attachment MUST NOT come back empty.

    Three of the six implementations refused here, this one included, while the
    specification named the remedy for an unusable method alone.  `38679fb`
    settled it and `corpus/diagnose/container-corrupt-attachment.vnz` pins it;
    this test is the local witness, so the reason does not live only in a
    fixture.
    """

    DOC = b'{"vnx": "0.1.0", "waypoints": [{"at": [1, 2]}]}'

    def archive(self) -> bytes:
        """A deflated attachment whose stream is damaged, CRC untouched."""
        import zipfile

        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as handle:
            handle.writestr("doc.vnx", self.DOC)
            handle.writestr("attachments/photo.txt", b"a photograph" * 20)
        data = bytearray(buffer.getvalue())
        # Damage the deflate stream of the second entry, in place, so the
        # archive's structure is intact and only the payload is unrecoverable.
        with zipfile.ZipFile(io.BytesIO(bytes(data))) as handle:
            info = handle.getinfo("attachments/photo.txt")
        at = info.header_offset + 30 + len(info.filename.encode()) + len(info.extra)
        for offset in range(at + 2, at + info.compress_size):
            data[offset] ^= 0xFF
        return bytes(data)

    def test_a_corrupt_attachment_is_dropped_and_the_document_returned(self) -> None:
        result = vnx.read_container_bytes(self.archive())
        self.assertEqual(result.attachments, [])
        assert result.document.waypoints is not None
        self.assertEqual(len(result.document.waypoints), 1)
        self.assertEqual(len(result.diagnostics), 1)
        self.assertEqual(result.diagnostics[0].code, "attachment-unreadable")
        self.assertEqual(
            result.diagnostics[0].path, "#/entries/attachments~1photo.txt"
        )

    def test_a_corrupt_ROOT_document_still_refuses(self) -> None:
        """The one entry whose loss leaves nothing to return."""
        import zipfile

        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as handle:
            handle.writestr("doc.vnx", self.DOC * 20)
        data = bytearray(buffer.getvalue())
        with zipfile.ZipFile(io.BytesIO(bytes(data))) as handle:
            info = handle.getinfo("doc.vnx")
        at = info.header_offset + 30 + len(info.filename.encode()) + len(info.extra)
        for offset in range(at + 2, at + info.compress_size):
            data[offset] ^= 0xFF
        with self.assertRaises(vnx.VNXRefusal) as caught:
            vnx.read_container_bytes(bytes(data))
        self.assertIn("root document", str(caught.exception))


class ContainerWriterDeflates(unittest.TestCase):
    """SPEC 7.1: every entry is stored or deflated, and a writer picks.

    This one deflates, so an archive costs less than the bytes it carries.  The
    bytes of an archive are not otherwise constrained: two conforming writers
    are not expected to produce the same archive for one document.
    """

    def document(self) -> vnx.Document:
        path = vnx.__file__.rsplit("/vnx/", 1)[0]
        with open(f"{path}/../corpus/accept/example-network.vnx", "rb") as handle:
            return vnx.read_bytes(handle.read()).document

    def test_every_entry_is_deflated(self) -> None:
        import zipfile

        data = vnx.encode_container_bytes(
            self.document(), {"attachments/p.txt": b"x" * 200}
        )
        with zipfile.ZipFile(io.BytesIO(data)) as archive:
            for info in archive.infolist():
                with self.subTest(entry=info.filename):
                    self.assertEqual(info.compress_type, zipfile.ZIP_DEFLATED)

    def test_the_archive_is_smaller_than_the_document_it_holds(self) -> None:
        document = self.document()
        self.assertLess(
            len(vnx.encode_container_bytes(document)),
            len(vnx.encode_bytes(document)),
        )

    def test_the_archive_reads_back_through_this_library(self) -> None:
        document = self.document()
        data = vnx.encode_container_bytes(document, {"attachments/p.txt": b"x" * 200})
        result = vnx.read_container_bytes(data)
        self.assertEqual(result.diagnostics, [])
        self.assertEqual(
            result.attachments,
            [vnx.Attachment("attachments/p.txt", b"x" * 200)],
        )
        self.assertEqual(
            tree.compare_documents(
                vnx.encode_bytes(result.document), vnx.encode_bytes(document),
                tree.Options(),
            ),
            "",
        )

    def test_the_archive_is_byte_reproducible(self) -> None:
        document = vnx.read_bytes(b'{"vnx": "0.1.0"}').document
        first = vnx.encode_container_bytes(document, {"attachments/p.txt": b"x"})
        second = vnx.encode_container_bytes(document, {"attachments/p.txt": b"x"})
        self.assertEqual(first, second)

    def test_no_header_field_carries_the_writing_platform(self) -> None:
        """Nothing in SPEC 7.1 requires this; it is what makes one input give
        one output wherever it is written.

        `create_system` is 3 on macOS and Linux and 0 on Windows, and
        `writestr` stamps file mode 0o600 into the external attributes, so the
        same Python on two platforms emitted two different archives for one
        document until this was pinned.
        """
        import zipfile

        document = vnx.read_bytes(b'{"vnx": "0.1.0"}').document
        data = vnx.encode_container_bytes(document, {"a.txt": b"x"})
        with zipfile.ZipFile(io.BytesIO(data)) as archive:
            for info in archive.infolist():
                with self.subTest(entry=info.filename):
                    self.assertEqual(info.create_system, 0, "the writing OS leaked")
                    self.assertEqual(info.external_attr, 0, "a file mode leaked")
                    self.assertEqual(info.date_time, (1980, 1, 1, 0, 0, 0))

    def test_attachments_are_sorted_by_entry_name(self) -> None:
        """A library CHOICE, pinned so it does not drift -- not a rule.

        SPEC 7.1 requires the root document first and leaves attachment order
        to the writer. Sorting is what makes one input give one archive here.

        The names are supplied in REVERSE order on purpose. Two names already in
        sorted order cannot separate a writer that sorts from one that iterates
        the caller's mapping, and a test that supplies them sorted is decoration.
        """
        import zipfile

        document = vnx.read_bytes(b'{"vnx": "0.1.0"}').document
        supplied = {"zebra.txt": b"z", "middle.txt": b"m", "alpha.txt": b"a"}
        self.assertEqual(
            list(supplied), ["zebra.txt", "middle.txt", "alpha.txt"],
            "the mapping must preserve the reversed order or this proves nothing",
        )
        data = vnx.encode_container_bytes(document, supplied)
        with zipfile.ZipFile(io.BytesIO(data)) as archive:
            self.assertEqual(
                archive.namelist(),
                ["doc.vnx", "alpha.txt", "middle.txt", "zebra.txt"],
            )

    def test_the_document_comes_first_whatever_it_sorts_as(self) -> None:
        """This one IS a rule: SPEC 7.1 requires the root document first, so
        the fallback of SPEC 7 and the preferred-name rule agree."""
        import zipfile

        document = vnx.read_bytes(b'{"vnx": "0.1.0"}').document
        data = vnx.encode_container_bytes(document, {"aaa.txt": b"a"})
        with zipfile.ZipFile(io.BytesIO(data)) as archive:
            self.assertEqual(archive.namelist()[0], "doc.vnx")


class WrongTypedValues(unittest.TestCase):
    """SPEC 6.2's two remedies for a wrong JSON type, and its two codes.

    A library that always drops the item, and one that always drops the field,
    each pass a document carrying only one of the two. Both readings have shipped
    in this format at once.
    """

    def test_an_optional_key_drops_the_field_and_keeps_the_item(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "waypoints": [{"at": [0,0], "hidden": "yes"}]}'
        )
        self.assertEqual(len(result.diagnostics), 1)
        self.assertEqual(result.diagnostics[0].code, "wrong-type-optional")
        assert result.document.waypoints is not None
        self.assertEqual(len(result.document.waypoints), 1)
        self.assertIsNone(result.document.waypoints[0].hidden)

    def test_a_required_key_drops_the_item(self) -> None:
        result = read('{"vnx": "0.1.0", "waypoints": [{"at": "beside the burn"}]}')
        self.assertEqual(len(result.diagnostics), 1)
        self.assertEqual(result.diagnostics[0].code, "wrong-type-required")
        self.assertEqual(result.document.waypoints, [])

    def test_the_two_codes_are_not_interchangeable(self) -> None:
        """The assertion that makes the pair discriminate.

        A reader emitting one code for both cases passes a document carrying
        only one of them, which is why the corpus case carries both.
        """
        optional = read(
            '{"vnx": "0.1.0", "waypoints": [{"at": [0,0], "hidden": "yes"}]}'
        )
        required = read('{"vnx": "0.1.0", "waypoints": [{"at": "nowhere"}]}')
        self.assertNotEqual(
            optional.diagnostics[0].code, required.diagnostics[0].code
        )


class ContainerHeadersAgree(unittest.TestCase):
    """SPEC 7.1's fields, read as bytes rather than through a ZIP library.

    Every assertion this suite made about those fields went through
    `zipfile.infolist()`, which resolves from the CENTRAL DIRECTORY. Corrupting
    a local header's version and CRC leaves every one of them green and leaves
    the archive readable, because the local header is never consulted. Measured,
    not assumed.

    So a library-based check cannot witness a rule about a local header, and
    reading your own output back with a foreign library does not fix it: an
    independent reader is not automatically an independent instrument, because
    it can still consult the same field the defect is not in.
    """

    def archive(self) -> bytes:
        document = vnx.read_bytes(b'{"vnx": "0.1.0"}').document
        return vnx.encode_container_bytes(
            document, {"z.txt": b"zz", "a.txt": b"a"}
        )

    def test_the_two_headers_agree_on_every_shared_field(self) -> None:
        from .zipbytes import central_headers, local_headers

        data = self.archive()
        local, central = local_headers(data), central_headers(data)
        self.assertEqual(len(local), len(central))
        self.assertTrue(local, "no local headers found")
        for one, two in zip(local, central):
            with self.subTest(entry=one.name):
                self.assertEqual(one.name, two.name)
                self.assertEqual(one.version, two.version)
                self.assertEqual(one.flags, two.flags)
                self.assertEqual(one.method, two.method)
                self.assertEqual(one.crc, two.crc)
                self.assertEqual(one.compressed, two.compressed)
                self.assertEqual(one.uncompressed, two.uncompressed)

    def test_the_local_headers_carry_the_fields_7_1_fixes(self) -> None:
        from .zipbytes import local_headers

        for header in local_headers(self.archive()):
            with self.subTest(entry=header.name):
                self.assertEqual(header.version, 20, "deflate needs 2.0")
                self.assertEqual(header.flags, 0x0800, "UTF-8 set, bit 3 clear")
                self.assertEqual(header.method, 8, "deflated")
                self.assertEqual(header.extra_len, 0)
                self.assertNotEqual(header.crc, 0, "written, not deferred")

    def test_a_corrupted_local_header_is_caught_here_and_nowhere_else(self) -> None:
        """The red proof for this whole class, frozen.

        A library-based reader sees nothing wrong with this archive.
        """
        import struct
        import zipfile

        from .zipbytes import central_headers, local_headers

        data = bytearray(self.archive())
        struct.pack_into("<H", data, 4, 10)   # local version 20 -> 10
        struct.pack_into("<I", data, 14, 0)   # local CRC -> 0
        corrupted = bytes(data)

        with zipfile.ZipFile(io.BytesIO(corrupted)) as archive:
            self.assertTrue(
                all(info.extract_version == 20 for info in archive.infolist()),
                "the library saw the corruption; this test is no longer needed",
            )

        local, central = local_headers(corrupted), central_headers(corrupted)
        self.assertNotEqual(
            local[0].version, central[0].version, "the headers must now disagree"
        )
        self.assertNotEqual(local[0].crc, central[0].crc)
