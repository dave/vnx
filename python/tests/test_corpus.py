"""The conformance corpus (SPEC 8).

Passing every case here is the definition of a conforming implementation.  The
runner walks the four directories and runs each case against its sidecar; a case
file with no sidecar is a failure, not a skip, because the file everyone assumes
is covered is otherwise exactly the one that is not.
"""

from __future__ import annotations

import json
import pathlib
import unittest
from typing import Any

import vnx
from vnx.canonical import render_document
from vnx.jsonio import parse_json

from . import matching
from .matching import match_diagnostics
from .resolve import resolve

CORPUS = pathlib.Path(__file__).resolve().parents[2] / "corpus"

CASE_SUFFIXES = (".vnx", ".vnz")
COLLECTIONS = ("folders", "networks", "routes", "tracks", "waypoints", "areas")


def _cases(kind: str) -> list[pathlib.Path]:
    directory = CORPUS / kind
    return sorted(
        path
        for path in directory.iterdir()
        if path.suffix in CASE_SUFFIXES and not path.name.endswith(".expect.json")
    )


class CorpusStructure(unittest.TestCase):
    """The corpus is present and every case is paired with its sidecar."""

    def test_corpus_is_present(self) -> None:
        self.assertTrue(CORPUS.is_dir(), f"no corpus at {CORPUS}")
        for kind in ("accept", "reject", "diagnose", "format"):
            self.assertTrue((CORPUS / kind).is_dir(), f"no {kind}/ directory")

    def test_every_case_has_a_sidecar(self) -> None:
        for kind in ("accept", "reject", "diagnose"):
            cases = _cases(kind)
            self.assertTrue(cases, f"{kind}/ holds no cases")
            for path in cases:
                sidecar = path.with_suffix(".expect.json")
                self.assertTrue(
                    sidecar.is_file(), f"{path.name} has no sidecar: a failure, not a skip"
                )

    def test_every_sidecar_has_a_case(self) -> None:
        for kind in ("accept", "reject", "diagnose"):
            for sidecar in sorted((CORPUS / kind).glob("*.expect.json")):
                stem = sidecar.name[: -len(".expect.json")]
                found = [
                    suffix
                    for suffix in CASE_SUFFIXES
                    if (CORPUS / kind / (stem + suffix)).is_file()
                ]
                self.assertTrue(found, f"{sidecar.name} describes no case file")

    # Fields this runner compares.  `kind` and `note` are structural.
    CHECKED = {"kind", "note", "counts", "positions", "tracks", "attachments",
               "refusal", "diagnostics"}
    # Fields this runner knowingly does not compare, and why.  A sidecar field
    # that is in neither set fails the test below rather than being ignored:
    # an unasserted expectation is how a fixture quietly becomes the format.
    UNCHECKED = {
        # The JSON Schema moved to corpus/schema/ on 2026-09-09 so that every
        # implementation could reach it.  This runner still does not compare
        # the verdict, and the reason changed rather than went away: validating
        # draft 2020-12 needs a JSON Schema validator, and no target language
        # ships one in its standard library.  Checking it here would cost this
        # library its only real adoption property.  A schema verdict is about
        # the schema rather than about a reader, so nothing else here is weaker
        # for it -- but it is an expectation this runner does not compare, and
        # saying so is the whole point of this set.
        "schema",
        # A prose record of a limitation (SPEC 6.4), paired with an empty
        # `diagnostics` list which IS checked.
        "cannot_detect",
    }

    def test_no_unchecked_entry_has_outlived_its_field(self) -> None:
        """An entry in UNCHECKED must name a field some sidecar still carries.

        The set exists to make an uncompared expectation visible.  A stale entry
        does the opposite: it makes the mechanism look more thorough than it is,
        and it is invisible because nothing goes red when the field goes away.
        Same shape as the finding this whole set was built for.

        `schema` is due to leave the sidecars -- ruled 2026-09-09, since moving
        the JSON Schema into corpus/ made it reachable but not checkable, and a
        field most runners can only record as unchecked reads as an assertion
        and is not one.  When that lands, THIS TEST GOES RED, and the fix is to
        delete `schema` from UNCHECKED above.  That is the intended behaviour,
        not a break: it is how the follow-up announces itself rather than
        waiting to be remembered.
        """
        used: set[str] = set()
        for kind in ("accept", "reject", "diagnose"):
            for sidecar in sorted((CORPUS / kind).glob("*.expect.json")):
                used |= set(json.loads(sidecar.read_text()))
        for field in sorted(self.UNCHECKED):
            self.assertIn(
                field,
                used,
                f"UNCHECKED names {field!r} and no sidecar carries it any more; "
                "delete the entry",
            )

    def test_the_runner_knows_every_sidecar_field(self) -> None:
        for kind in ("accept", "reject", "diagnose"):
            for sidecar in sorted((CORPUS / kind).glob("*.expect.json")):
                for field in json.loads(sidecar.read_text()):
                    self.assertIn(
                        field,
                        self.CHECKED | self.UNCHECKED,
                        f"{sidecar.name}: this runner does not know {field!r}, so "
                        "the expectation would pass without being compared",
                    )

    def test_the_runner_knows_every_field_INSIDE_a_diagnostic(self) -> None:
        """The top-level sweep above is not enough, and that cost real coverage.

        When `diagnostics[].message` became `code` + `message_advisory` on
        2026-09-09, the top-level test stayed green -- `diagnostics` was still a
        known top-level key -- while the matcher, which looked for `message`,
        silently fell back to comparing paths alone. Every diagnose case passed
        while checking half of what it pins. A nested expectation needs a nested
        sweep.
        """
        known = matching.COMPARED_DIAGNOSTIC_FIELDS | matching.UNCOMPARED_DIAGNOSTIC_FIELDS
        for sidecar in sorted((CORPUS / "diagnose").glob("*.expect.json")):
            for expected in json.loads(sidecar.read_text()).get("diagnostics", []):
                for field in expected:
                    self.assertIn(
                        field,
                        known,
                        f"{sidecar.name}: the matcher does not know "
                        f"diagnostics[].{field!r}",
                    )

    def test_every_pinned_diagnostic_names_a_code(self) -> None:
        """SPEC 6.3: a diagnostic a corpus case pins MUST carry a code."""
        for sidecar in sorted((CORPUS / "diagnose").glob("*.expect.json")):
            for expected in json.loads(sidecar.read_text()).get("diagnostics", []):
                self.assertIn("code", expected, f"{sidecar.name}: pinned without a code")

    def test_every_sidecar_declares_its_kind_and_reason(self) -> None:
        for kind in ("accept", "reject", "diagnose"):
            for sidecar in sorted((CORPUS / kind).glob("*.expect.json")):
                expect = json.loads(sidecar.read_text())
                self.assertEqual(expect.get("kind"), kind, sidecar.name)
                self.assertTrue(expect.get("note"), f"{sidecar.name} has no note")


class _CorpusCase(unittest.TestCase):
    def _read(self, path: pathlib.Path) -> Any:
        if path.suffix == ".vnz":
            return vnx.read_container_file(path)
        return vnx.read_file(path)

    def _check_counts(self, expect: dict[str, Any], document: vnx.Document, name: str) -> None:
        """SPEC 8.1's `counts`, under the completeness rule ruled 2026-09-09.

        A sidecar must name every collection its document carries, `rulesets`
        included.  Before that, an omitted collection meant "unchecked", so a
        decoder could invent or drop items in a collection no sidecar named and
        stay green.  Enforcing it is the point: a rule most files satisfy by
        accident is one the next file breaks silently.
        """
        counts = document.counts()
        pinned = expect.get("counts") or {}
        for collection, want in pinned.items():
            self.assertIn(collection, counts, f"{name}: unknown collection {collection!r}")
            self.assertEqual(counts[collection], want, f"{name}: {collection} count")
        for collection, got in counts.items():
            if got:
                self.assertIn(
                    collection,
                    pinned,
                    f"{name}: the document carries {got} {collection} and the "
                    "sidecar does not name that collection",
                )

    def _check_positions(self, expect: dict[str, Any], document: vnx.Document, name: str) -> None:
        for pinned in expect.get("positions") or []:
            position = resolve(document, pinned["at"])
            self.assertIsInstance(
                position, vnx.Position, f"{name}: {pinned['at']} is not a position"
            )
            self.assertEqual(position.lon, pinned["lon"], f"{name}: {pinned['at']} lon")
            self.assertEqual(position.lat, pinned["lat"], f"{name}: {pinned['at']} lat")

    def _check_tracks(self, expect: dict[str, Any], document: vnx.Document, name: str) -> None:
        for pinned in expect.get("tracks") or []:
            matches = [t for t in (document.tracks or []) if t.name == pinned["name"]]
            self.assertEqual(len(matches), 1, f"{name}: no one track named {pinned['name']!r}")
            got = matches[0].moving_time_ms()
            want = pinned["moving_time_ms"]
            if want is None:
                self.assertIsNone(got, f"{name}: moving time must be unknown, not {got!r}")
            else:
                self.assertIsNotNone(got, f"{name}: moving time must be known")
                self.assertEqual(got, float(want), f"{name}: moving time")


class AcceptTier(_CorpusCase):
    """Files a reader MUST read, with no diagnostics."""

    def test_accept(self) -> None:
        for path in _cases("accept"):
            with self.subTest(case=path.name):
                expect = json.loads(path.with_suffix(".expect.json").read_text())
                result = self._read(path)
                self.assertEqual(
                    [(d.path, d.message) for d in result.diagnostics],
                    [],
                    f"{path.name}: an accept file must read with no diagnostics",
                )
                self._check_counts(expect, result.document, path.name)
                self._check_positions(expect, result.document, path.name)
                self._check_tracks(expect, result.document, path.name)
                if "attachments" in expect:
                    self.assertEqual(
                        sorted(getattr(result, "attachments", [])),
                        sorted(expect["attachments"]),
                        f"{path.name}: attachments",
                    )

    def test_accept_files_are_canonical(self) -> None:
        """SPEC 8.1: canonicalising an accept file reproduces it byte for byte."""
        for path in _cases("accept"):
            if path.suffix != ".vnx":
                continue
            with self.subTest(case=path.name):
                original = path.read_bytes()
                self.assertEqual(
                    vnx.canonicalise(original), original, f"{path.name}: not canonical"
                )

    def test_accept_files_survive_the_model(self) -> None:
        """Decode and re-encode reproduces the file byte for byte.

        Stronger than the canonicalise check above, which never leaves the JSON
        tree.  This is what proves an opaque data record body, a ruleset, a
        `hidden` and a position's arity all survive a read-modify-write.
        """
        for path in _cases("accept"):
            if path.suffix != ".vnx":
                continue
            with self.subTest(case=path.name):
                original = path.read_bytes()
                document = vnx.read_bytes(original).document
                self.assertEqual(vnx.reformat_bytes(document), original, path.name)


class RejectTier(_CorpusCase):
    """Files a reader MUST refuse, naming the cause."""

    def test_reject(self) -> None:
        for path in _cases("reject"):
            with self.subTest(case=path.name):
                expect = json.loads(path.with_suffix(".expect.json").read_text())
                with self.assertRaises(vnx.VNXRefusal) as caught:
                    self._read(path)
                self.assertIn(
                    expect["refusal"],
                    str(caught.exception),
                    f"{path.name}: the refusal must name its cause",
                )


class DiagnoseTier(_CorpusCase):
    """Files a reader MUST read while reporting exactly the listed diagnostics."""

    def test_diagnose(self) -> None:
        for path in _cases("diagnose"):
            with self.subTest(case=path.name):
                expect = json.loads(path.with_suffix(".expect.json").read_text())
                result = self._read(path)
                problem = match_diagnostics(expect["diagnostics"], result.diagnostics)
                self.assertIsNone(problem, f"{path.name}: {problem}")
                self._check_counts(expect, result.document, path.name)
                self._check_positions(expect, result.document, path.name)
                self._check_tracks(expect, result.document, path.name)


class WriteTier(unittest.TestCase):
    """Decode an input and call the WRITE path (SPEC 4.5.4).

    The only tier that reaches the writer boundary. The other four re-render, and
    a re-render neither rounds nor re-spells, so rounding, the shortest-spelling
    rule, both tie directions and a negative zero the writer created are all
    invisible to them.

    A library exposing no write path fails here by name rather than by a
    confusing byte diff.
    """

    def _pairs(self) -> list[tuple[pathlib.Path, pathlib.Path]]:
        directory = CORPUS / "write"
        if not directory.is_dir():
            return []
        pairs = []
        for source in sorted(directory.glob("*.in.vnx")):
            stem = source.name[: -len(".in.vnx")]
            pairs.append((source, directory / f"{stem}.written.vnx"))
        return pairs

    def test_every_input_has_its_written_bytes_and_sidecar(self) -> None:
        pairs = self._pairs()
        self.assertTrue(pairs, "write/ holds no cases")
        for source, written in pairs:
            stem = source.name[: -len(".in.vnx")]
            self.assertTrue(written.is_file(), f"{source.name} has no written form")
            sidecar = CORPUS / "write" / f"{stem}.expect.json"
            self.assertTrue(sidecar.is_file(), f"{source.name} has no sidecar")
            self.assertEqual(json.loads(sidecar.read_text()).get("kind"), "write")

    def test_the_writer_emits_the_written_bytes(self) -> None:
        for source, written in self._pairs():
            with self.subTest(case=source.name):
                # `subTest` already contains a refusal to this case, so the
                # other cases still report; measured, not assumed. Catching it
                # is for the MESSAGE: "was refused: ..." names the cause, where
                # an uncaught VNXRefusal names only its own text and leaves the
                # reader to work out which tier raised it.
                try:
                    document = vnx.read_bytes(source.read_bytes()).document
                    got = vnx.encode_bytes(document)
                except vnx.VNXRefusal as refusal:
                    self.fail(f"{source.name} was refused: {refusal}")
                self.assertEqual(got, written.read_bytes())

    def test_the_input_is_not_already_the_written_form(self) -> None:
        """Otherwise a library that echoed its input would pass."""
        for source, written in self._pairs():
            with self.subTest(case=source.name):
                self.assertNotEqual(source.read_bytes(), written.read_bytes())

    def test_the_written_form_is_a_fixed_point(self) -> None:
        for _, written in self._pairs():
            with self.subTest(case=written.name):
                document = vnx.read_bytes(written.read_bytes()).document
                self.assertEqual(vnx.encode_bytes(document), written.read_bytes())


class FormatTier(unittest.TestCase):
    """Non-canonical input, and exactly the bytes a formatter MUST emit for it.

    These cases carry no `.expect.json`: the pairing of `X.in.vnx` with
    `X.canonical.vnx` is the expectation.  SPEC 8.1's rule that a file with no
    sidecar is a failure is written without an exception for this directory.
    """

    def _pairs(self) -> list[tuple[pathlib.Path, pathlib.Path]]:
        directory = CORPUS / "format"
        pairs = []
        for source in sorted(directory.glob("*.in.vnx")):
            stem = source.name[: -len(".in.vnx")]
            pairs.append((source, directory / f"{stem}.canonical.vnx"))
        return pairs

    def test_every_input_has_its_canonical_bytes(self) -> None:
        pairs = self._pairs()
        self.assertTrue(pairs, "format/ holds no cases")
        for source, expected in pairs:
            self.assertTrue(expected.is_file(), f"{source.name} has no canonical form")
        stems = {p.name[: -len(".in.vnx")] for p, _ in pairs}
        for other in sorted((CORPUS / "format").glob("*.canonical.vnx")):
            stem = other.name[: -len(".canonical.vnx")]
            self.assertIn(stem, stems, f"{other.name} pairs with no input")

    def test_input_is_not_already_canonical(self) -> None:
        """Otherwise a formatter that echoed its input would pass."""
        for source, expected in self._pairs():
            with self.subTest(case=source.name):
                self.assertNotEqual(
                    source.read_bytes(),
                    expected.read_bytes(),
                    f"{source.name} is already canonical and tests nothing",
                )

    def test_formatter_emits_the_canonical_bytes(self) -> None:
        for source, expected in self._pairs():
            with self.subTest(case=source.name):
                self.assertEqual(
                    vnx.canonicalise(source.read_bytes()), expected.read_bytes()
                )

    def test_canonical_form_is_a_fixed_point(self) -> None:
        for _, expected in self._pairs():
            with self.subTest(case=expected.name):
                original = expected.read_bytes()
                self.assertEqual(vnx.canonicalise(original), original)


if __name__ == "__main__":
    unittest.main()


class NumbersTier(unittest.TestCase):
    """`corpus/numbers/numbers.tsv`: what every implementation must emit for a
    given double (SPEC 4.5.3 and 4.5.4).

    A tier in standing and not in shape. A case in the four document tiers is a
    document; a row here is a value, and its three answer columns are the
    expectation, so there is no sidecar.

    It reaches the rounding of 4.5.4 for the same reason `write/` does, and it
    reaches it per value rather than per document, which is what makes it the
    instrument for a rule that only fails on particular doubles.
    """

    TABLE = CORPUS / "numbers" / "numbers.tsv"

    def rows(self) -> list[tuple[str, str, str, str, str]]:
        if not self.TABLE.is_file():
            return []
        out = []
        for line in self.TABLE.read_text().splitlines():
            if not line or line.startswith("#"):
                continue
            bits, kind, render, round6, round1 = line.split("\t")
            out.append((bits, kind, render, round6, round1))
        return out

    def test_the_table_is_present_and_substantial(self) -> None:
        rows = self.rows()
        self.assertTrue(rows, f"no table at {self.TABLE}")
        self.assertGreater(len(rows), 1000, "a short table pins little")

    def test_every_row_renders_and_rounds_as_the_table_says(self) -> None:
        import struct

        from vnx.model import Position
        from vnx.numbers import render_number
        from vnx.writer import round_position

        failures: list[str] = []
        for bits, kind, render, round6, round1 in self.rows():
            value = struct.unpack(">d", bytes.fromhex(bits))[0]
            got_render = render_number(value)
            # Rounding is defined at the emit boundary, so it is exercised
            # through the writer rather than through a private helper: a test
            # calling something the writer does not call proves nothing about
            # what this library emits.
            lonlat = round_position(Position((value, 0.0)))
            elevation = round_position(Position((0.0, 0.0, value)))
            got6 = render_number(lonlat.lon)
            ele = elevation.ele
            got1 = "0" if ele is None else render_number(ele)
            for label, got, want in (
                ("render", got_render, render),
                ("round6", got6, round6),
                ("round1", got1, round1),
            ):
                if got != want:
                    failures.append(f"{bits} ({kind}) {label}: got {got}, want {want}")
        self.assertEqual(failures[:10], [], f"{len(failures)} of {len(self.rows())} rows differ")

    def test_the_table_reaches_the_rounding_tie(self) -> None:
        """The table is only worth running if reverting a rule reddens it.

        A conformance table nobody has seen fail is a green light with no bulb
        in it, so this asserts the rows exist that would catch a writer rounding
        ties away from zero.
        """
        import struct

        ties = [
            row for row in self.rows()
            if self._is_one_place_tie(struct.unpack(">d", bytes.fromhex(row[0]))[0])
        ]
        self.assertTrue(ties, "no row in the table can separate the tie rules")

    @staticmethod
    def _is_one_place_tie(value: float) -> bool:
        from decimal import Decimal

        exact = Decimal(value)
        scaled = exact * 10
        return scaled == scaled.to_integral_value() + Decimal("0.5") or \
            scaled == scaled.to_integral_value() - Decimal("0.5")


class ExpectationsResolveByName(unittest.TestCase):
    """An expectation resolves by NAME, never by index.

    `corpus/README.md` requires it, and the reason is that a sidecar which has
    drifted from its fixture is caught by a name lookup and sailed straight past
    by an index lookup, which finds *a* track at position 0 and compares the
    wrong one.

    Asserted directly rather than by mutating a sidecar and running the tier. An
    indirect probe of this took two attempts to get right and reported the
    opposite of the truth on the first, so the direct call is the instrument.
    """

    def case(self) -> _CorpusCase:
        class Probe(_CorpusCase):
            def runTest(self) -> None:  # pragma: no cover - never run as a test
                pass

        return Probe()

    def document(self) -> vnx.Document:
        return vnx.read_file(CORPUS / "accept" / "track-with-pause.vnx").document

    def test_a_track_expectation_naming_nothing_is_caught(self) -> None:
        probe, document = self.case(), self.document()
        for name in ("a name no track in this fixture has", ""):
            with self.subTest(name=name), self.assertRaises(AssertionError):
                probe._check_tracks(
                    {"tracks": [{"name": name, "moving_time_ms": 167000}]},
                    document,
                    "probe",
                )

    def test_the_correct_name_still_passes(self) -> None:
        """The control. Without it the test above proves only that something
        raises, not that it raises for the reason it names."""
        probe, document = self.case(), self.document()
        assert document.tracks is not None
        probe._check_tracks(
            {"tracks": [{"name": document.tracks[0].name, "moving_time_ms": 167000}]},
            document,
            "probe",
        )

    def test_a_counts_expectation_naming_no_collection_is_caught(self) -> None:
        probe, document = self.case(), self.document()
        with self.assertRaises(AssertionError):
            probe._check_counts({"counts": {"sprockets": 1}}, document, "probe")

    def test_a_positions_pointer_that_resolves_to_nothing_is_caught(self) -> None:
        probe, document = self.case(), self.document()
        with self.assertRaises((AssertionError, KeyError, IndexError)):
            probe._check_positions(
                {"positions": [{"at": "/waypoints/0/at", "lon": 0.0, "lat": 0.0}]},
                document,
                "probe",
            )
