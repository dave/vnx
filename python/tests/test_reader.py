"""Unit tests for what the corpus does not reach.

Each test says what the corpus cannot see, because a test that duplicates a
corpus case adds nothing.
"""

from __future__ import annotations

import io
import unittest

import vnx
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

    def test_an_older_minor_is_accepted(self) -> None:
        # SPEC 2.2 in as many words: "A 0.1.0 reader accepts 0.1.3 and 0.0.9".
        self.assertEqual(read('{"vnx": "0.0.9"}').document.vnx, "0.0.9")

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
        self.assertIn(b'"vnx": "0.1.3"', vnx.reformat_bytes(document))

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
        source = (
            '{\n  "vnx": "0.1.0",\n  "waypoints": [\n    {\n'
            '      "at": [0, 0],\n'
            '      "style": {\n        "future-key": "value"\n      }\n'
            '    }\n  ]\n}\n'
        )
        document = read(source).document
        self.assertEqual(vnx.reformat_bytes(document).decode(), source)

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

    def test_a_short_position_is_dropped_and_diagnosed(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "tracks": [{"segments": [{"pips":'
            ' [[-3.6, 57.1], [-3.6]]}]}]}'
        )
        self.assertEqual(len(result.diagnostics), 1)
        assert result.document.tracks is not None
        segments = result.document.tracks[0].segments
        assert segments is not None and segments[0].pips is not None
        self.assertEqual(len(segments[0].pips), 1)

    def test_a_null_longitude_is_dropped(self) -> None:
        result = read(
            '{"vnx": "0.1.0", "tracks": [{"segments": [{"pips":'
            ' [[null, 57.1, 3]]}]}]}'
        )
        self.assertEqual(len(result.diagnostics), 1)

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

    def test_a_fifth_channel_is_ignored_but_kept(self) -> None:
        # SPEC 4.1: a reader MUST ignore channels beyond those it knows, so
        # that a future channel is additive.  Ignoring is not discarding: the
        # document has to survive a read-modify-write.
        source = '{\n  "vnx": "0.1.0",\n  "waypoints": [\n    {\n      "at": [-3.6, 57.1, 3, 5, 7]\n    }\n  ]\n}\n'
        result = read(source)
        self.assertEqual(messages(result), [])
        self.assertEqual(vnx.reformat_bytes(result.document).decode(), source)


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
      "nodes": {"a": [-3.68, 57.12], "b": [-3.66, 57.10]},
      "arcs": {
        "ab": {"from": "a", "to": "b",
               "pips": [[-3.68, 57.12], [-3.67, 57.11], [-3.66, 57.10]]},
        "ba": {"from": "b", "to": "a",
               "pips": [[-3.66, 57.10], [-3.67, 57.11], [-3.68, 57.12]]},
        "cd": {"from": "a", "to": "b",
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
            ' {"arc": "cd", "from": "a", "to": "b"}]'
        )
        self.assertEqual(len(result.diagnostics), 1, messages(result))
        self.assertIn("not in this network", result.diagnostics[0].message)

    def test_a_break_is_reported_on_the_second_of_the_pair(self) -> None:
        result = self.chain(
            '[{"arc": "ab", "from": "a", "to": "b"},'
            ' {"arc": "cd", "from": "a", "to": "b"}]'
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

    def test_hidden_false_survives(self) -> None:
        source = (
            '{\n  "vnx": "0.1.0",\n  "areas": [\n    {\n      "hidden": false,\n'
            '      "polygon": [\n        [0, 0],\n        [1, 0],\n        [1, 1],\n'
            '        [0, 0]\n      ]\n    }\n  ]\n}\n'
        )
        self.assertEqual(vnx.canonicalise(doc(source)).decode(), source)
        document = read(source).document
        assert document.areas is not None
        self.assertIs(document.areas[0].hidden, False)
        self.assertEqual(vnx.reformat_bytes(document).decode(), source)

    def test_an_absent_hidden_is_not_false(self) -> None:
        document = read(RING).document
        assert document.areas is not None
        self.assertIsNone(document.areas[0].hidden)

    def test_an_absent_label_visible_is_not_false(self) -> None:
        document = read('{"vnx": "0.1.0", "waypoints": [{"at": [0,0], "style": {}}]}').document
        assert document.waypoints is not None
        self.assertNotIn("label-visible", document.waypoints[0].style or {})

    def test_an_empty_collection_is_not_an_absent_one(self) -> None:
        for source in ('{\n  "vnx": "0.1.0",\n  "areas": []\n}\n', '{\n  "vnx": "0.1.0"\n}\n'):
            with self.subTest(source=source):
                document = read(source).document
                self.assertEqual(vnx.reformat_bytes(document).decode(), source)

    def test_a_non_boolean_hidden_is_dropped(self) -> None:
        result = read(RING.replace('{"polygon"', '{"hidden": "yes", "polygon"'))
        self.assertEqual(messages(result), [("/areas/0/hidden", "must be a boolean")])


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
            ' "pips": [[0,0],[1,1]]}}},'
            '{"nodes": {"n1": [9,9]}, "arcs": {"a": {"from": "n1", "to": "n1",'
            ' "pips": [[9,9],[8,8]]}}}]}'
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
            ' "to": "n1", "pips": [[0,0],[1,1]]}}},'
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
        self.assertEqual(result.attachments, ["aaa.vnx"])

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
        self.assertEqual(result.attachments, ["attachments/p.txt"])
        assert result.document.waypoints is not None
        at = result.document.waypoints[0].at
        assert at is not None
        self.assertEqual(at.lon, 1.0)

    def test_the_container_paths_differ_exactly_as_the_plain_ones_do(self) -> None:
        """Ruled 2026-09-09: repackaging must not silently lose precision."""
        import zipfile

        document = vnx.read_bytes(
            b'{"vnx": "0.1.0", "waypoints": [{"at": [-3.6712345678901235, 57.118]}]}'
        ).document
        def doc_of(data: bytes) -> bytes:
            with zipfile.ZipFile(io.BytesIO(data)) as archive:
                return archive.read("doc.vnx")
        kept = doc_of(vnx.reformat_container_bytes(document))
        rounded = doc_of(vnx.encode_container_bytes(document))
        self.assertIn(b"-3.6712345678901235", kept)
        self.assertIn(b"-3.671235", rounded)
        self.assertNotEqual(kept, rounded)

    def test_a_written_container_sets_the_utf8_flag(self) -> None:
        import io
        import zipfile

        document = vnx.read_bytes(b'{"vnx": "0.1.0"}').document
        with zipfile.ZipFile(io.BytesIO(vnx.encode_container_bytes(document))) as archive:
            for info in archive.infolist():
                self.assertTrue(info.flag_bits & 0x800, info.filename)


class CanonicalLayout(unittest.TestCase):
    """SPEC 4.5.  Shapes the corpus does not contain."""

    def round(self, source: str) -> str:
        return vnx.canonicalise(doc(source)).decode()

    def test_an_empty_array_and_object_stay_on_one_line(self) -> None:
        # SPEC 4.5.2 does not say what an empty array does; it holds only
        # numbers and null vacuously, so it takes the inline form.
        self.assertEqual(
            self.round('{"vnx":"0.1.0","waypoints":[],"folders":[{}]}'),
            '{\n  "vnx": "0.1.0",\n  "folders": [\n    {}\n  ],\n  "waypoints": []\n}\n',
        )

    def test_a_string_array_is_not_inlined(self) -> None:
        self.assertEqual(
            self.round('{"vnx":"0.1.0","rulesets":["a","b"]}'),
            '{\n  "vnx": "0.1.0",\n  "rulesets": [\n    "a",\n    "b"\n  ]\n}\n',
        )

    def test_a_boolean_array_is_not_inlined(self) -> None:
        # SPEC 4.5.2 says numbers and null.  A boolean is neither, even though
        # Python calls it an int.
        self.assertIn(
            '"rulesets": [\n    true,\n    false\n  ]',
            self.round('{"vnx":"0.1.0","rulesets":[true,false]}'),
        )

    def test_a_number_array_nested_in_a_record_is_inlined(self) -> None:
        self.assertIn(
            '"xs": [1, 2, 3]',
            self.round('{"vnx":"0.1.0","waypoints":[{"data":[{"@type":"x","xs":[1,2,3]}]}]}'),
        )

    def test_a_control_character_is_escaped(self) -> None:
        self.assertIn(
            r'"\u0001\n"', self.round('{"vnx":"0.1.0","generator":"\\u0001\\n"}')
        )

    def test_non_ascii_is_emitted_as_utf8(self) -> None:
        # SPEC 4.5.5 gives the encoding and names no further escaping.  This is
        # unspecified: Go's encoding/json would also escape < > & U+2028 U+2029.
        self.assertIn("Bràigh", self.round('{"vnx":"0.1.0","generator":"Bràigh"}'))
        self.assertIn("<a>", self.round('{"vnx":"0.1.0","generator":"<a>"}'))

    def test_a_solidus_is_not_escaped(self) -> None:
        self.assertIn('"a/b"', self.round('{"vnx":"0.1.0","generator":"a/b"}'))

    def test_canonicalise_keeps_an_unknown_key(self) -> None:
        # A formatter renders bytes.  It is not a reader, so it does not skip
        # what a reader would diagnose, and it does not round (SPEC 4.5.4).
        self.assertIn(
            '"nmae"',
            self.round('{"vnx":"0.1.0","waypoints":[{"nmae":"typo"}]}'),
        )

    def test_canonicalise_does_not_round(self) -> None:
        self.assertIn(
            "-3.6712345678901235",
            self.round('{"vnx":"0.1.0","waypoints":[{"at":[-3.6712345678901235,57.1]}]}'),
        )

    def test_canonicalise_refuses_a_non_finite_number(self) -> None:
        with self.assertRaises(VNXRefusal):
            self.round('{"vnx":"0.1.0","waypoints":[{"at":[NaN,57.1]}]}')


if __name__ == "__main__":
    unittest.main()


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


class EscapingTable(unittest.TestCase):
    """SPEC 4.5.5's escaping rule, which no corpus file reaches.

    Five control characters take their short escape; every other control
    character is \\u00xx with LOWERCASE hex; nothing else is escaped at all,
    including the five that Go's encoding/json escapes by default.
    """

    def test_the_five_short_escapes(self) -> None:
        from vnx.canonical import render_string

        for raw, want in (
            ("\b", '"\\b"'),
            ("\f", '"\\f"'),
            ("\n", '"\\n"'),
            ("\r", '"\\r"'),
            ("\t", '"\\t"'),
        ):
            with self.subTest(char=repr(raw)):
                self.assertEqual(render_string(raw), want)

    def test_every_other_control_character_is_lower_case_u00xx(self) -> None:
        from vnx.canonical import render_string

        for code, want in ((0x00, '"\\u0000"'), (0x07, '"\\u0007"'),
                           (0x0B, '"\\u000b"'), (0x1F, '"\\u001f"')):
            with self.subTest(code=hex(code)):
                self.assertEqual(render_string(chr(code)), want)

    def test_upper_case_hex_would_be_wrong(self) -> None:
        from vnx.canonical import render_string

        self.assertEqual(render_string(chr(0x1F)), '"\\u001f"')
        self.assertNotEqual(render_string(chr(0x1F)), '"\\u001F"')

    def test_the_two_characters_json_itself_requires(self) -> None:
        from vnx.canonical import render_string

        self.assertEqual(render_string('"'), '"\\""')
        self.assertEqual(render_string("\\"), '"\\\\"')

    def test_the_five_go_escapes_by_default_are_written_raw(self) -> None:
        from vnx.canonical import render_string

        for char in ("<", ">", "&", " ", " "):
            with self.subTest(char=repr(char)):
                self.assertEqual(render_string(char), f'"{char}"')

    def test_nothing_above_007f_is_ever_escaped(self) -> None:
        from vnx.canonical import render_string

        for code in (0x80, 0xE0, 0x2028, 0xFFFD, 0x1F600):
            with self.subTest(code=hex(code)):
                self.assertNotIn("\\u", render_string(chr(code)))

    def test_a_solidus_is_not_escaped(self) -> None:
        from vnx.canonical import render_string

        self.assertEqual(render_string("a/b"), '"a/b"')


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
        self.assertEqual(result.attachments, ["a..b/photo.jpg"])

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
        self.assertEqual(result.attachments, ["a..b/photo.jpg"])

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
                self.assertEqual(result.attachments, ["attachments/photo.txt"])
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
        self.assertIsNone(
            result.diagnostics[0].code, "no corpus case pins this diagnostic"
        )
        self.assertIsNotNone(result.document)


class ContainerWriterUsesStored(unittest.TestCase):
    """A writer emits STORED for every entry; a reader still accepts deflate.

    Ruled 2026-09-09 because deflate has no canonical output: for one corpus
    document Go's `compress/flate` emits 116 bytes and zlib emits 108, and no
    zlib level from 0 to 9 reproduces the committed bytes. A deflated archive is
    byte-reproducible only by the language that wrote it.

    Python is the seat least likely to notice the rule is needed, because
    `zipfile` defaults to stored: this library satisfied the rule by accident
    before it existed, in the same way its number rendering satisfies others.
    """

    def test_every_entry_is_stored(self) -> None:
        import zipfile

        document = vnx.read_bytes(b'{"vnx": "0.1.0"}').document
        data = vnx.encode_container_bytes(document, {"attachments/p.txt": b"x"})
        with zipfile.ZipFile(io.BytesIO(data)) as archive:
            for info in archive.infolist():
                with self.subTest(entry=info.filename):
                    self.assertEqual(info.compress_type, zipfile.ZIP_STORED)

    def test_the_archive_is_byte_reproducible(self) -> None:
        document = vnx.read_bytes(b'{"vnx": "0.1.0"}').document
        first = vnx.encode_container_bytes(document, {"attachments/p.txt": b"x"})
        second = vnx.encode_container_bytes(document, {"attachments/p.txt": b"x"})
        self.assertEqual(first, second)
