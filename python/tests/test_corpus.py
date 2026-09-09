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
