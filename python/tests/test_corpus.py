"""The conformance corpus (SPEC 8).

Passing every case here is the definition of a conforming implementation.  The
runner walks the four directories and runs each case against its sidecar; a case
file with no sidecar is a failure, not a skip, because the file everyone assumes
is covered is otherwise exactly the one that is not.
"""

from __future__ import annotations

import io
import json
import pathlib
import struct
import unittest
from typing import Any

import vnx

from . import matching, tree
from .matching import match_diagnostics
from .resolve import resolve

CORPUS = pathlib.Path(__file__).resolve().parents[2] / "corpus"

CASE_SUFFIXES = (".vnx", ".vnz")

# The write tier normalises ALMOST NOTHING.  The accept tier drops a trailing
# `null` from both sides, because it compares a document against a re-encoding
# of itself.  Doing that here would normalise away the rule under test.  The one
# normalisation left is the one that is not about the emit boundary at all:
# SPEC 3.1's absent-versus-empty.
_WRITE_TIER = tree.Options(emit_boundary=False)


def _bits(value: float) -> str:
    """A double's IEEE 754 encoding, as 16 hexadecimal digits."""
    return struct.pack(">d", value).hex()
COLLECTIONS = ("folders", "networks", "routes", "tracks", "waypoints", "areas")


def _container_counts_by_pointer(document: vnx.Document) -> dict[str, int]:
    """The number of children of every NESTED container in the document, keyed
    by a JSON Pointer in the wire's own key names -- the same vocabulary
    `positions` uses.

    ⚠ IT EXISTS BECAUSE `counts` COUNTED ONLY THE TOP-LEVEL COLLECTIONS.  A
    reader that dropped a chain's passes, or truncated an arc's pips, changed
    nothing a sidecar could see: the document still carried one network, one
    route, one track.  The accept tier's decode->encode->tree comparison catches
    most of that already, but the DIAGNOSE tier is exempt from the round trip
    (SPEC 8.1) -- and those are exactly the cases where a reader is supposed to
    drop something in a SCOPED way.  Measured on the pre-D2 corpus: a reader
    that dropped the REST OF THE CHAIN along with a dangling link passed every
    tier green.  Container (.vnz) accept cases are the other gap, because the
    round trip returns early for them.  `positions` does not close either: it
    pins the value at a few named indices and says nothing about how many there
    are.

    It is a PRODUCER rather than a pointer resolver, for the reason
    `positions_by_pointer` is in the reference: one traversal answers every
    pointer, so a case can pin a container deep inside a network without this
    helper growing a pointer parser.  A sidecar key this walk does not produce
    is a loud failure naming the key, never a skip.

    ⚠ ARC AND NODE CONTAINERS ARE KEYED BY THEIR OWN KEY, NOT BY AN INDEX.
    `/networks/0/arcs/arc-1/pips` -- SPEC 3.3 makes `arcs` and `nodes` objects,
    and an object has no order, so an index there would export this
    implementation's dict iteration order as a conformance requirement.

    A sidecar MAY name a container the document carries empty (see
    `corpus/diagnose/self-loop-arc.expect.json`, which records
    `/networks/0/chains: 0`).  Failing to name a NON-empty
    container is the error; naming a zero is legal.

    ⚠ EVERY CONTAINER IS EMITTED UNCONDITIONALLY, ZERO-COUNT ONES INCLUDED, and
    that is a conformance requirement rather than a taste.  `points` and `areas`
    briefly carried a non-empty guard here and in the Go reference.  A zero
    never triggers the completeness rule, so the guard bought nothing -- what it
    cost is that `/networks/0/points: 0` resolved in a runner that emitted
    unconditionally and was a hard "not a collection the document carries" error
    in one that did not.  One sidecar entry, legal in one conforming
    implementation and a failure in another, with nothing in the spec to
    separate them.  Ruled out across all five runners 2026-09-14.
    """
    out: dict[str, int] = {}
    for i, network in enumerate(document.networks or []):
        out[f"/networks/{i}/nodes"] = len(network.nodes or {})
        out[f"/networks/{i}/arcs"] = len(network.arcs or {})
        for key, arc in (network.arcs or {}).items():
            out[f"/networks/{i}/arcs/{key}/pips"] = len(arc.pips or [])
        out[f"/networks/{i}/chains"] = len(network.chains or [])
        for j, chain in enumerate(network.chains or []):
            out[f"/networks/{i}/chains/{j}/walk"] = len(chain.walk or [])
        out[f"/networks/{i}/points"] = len(network.points or [])
        out[f"/networks/{i}/areas"] = len(network.areas or [])
        for j, area in enumerate(network.areas or []):
            out[f"/networks/{i}/areas/{j}/polygon"] = len(area.polygon or [])
    for i, route in enumerate(document.routes or []):
        out[f"/routes/{i}/nodes"] = len(route.nodes or {})
        out[f"/routes/{i}/arcs"] = len(route.arcs or {})
        for key, arc in (route.arcs or {}).items():
            out[f"/routes/{i}/arcs/{key}/pips"] = len(arc.pips or [])
        out[f"/routes/{i}/walk"] = len(route.walk or [])
    for i, track in enumerate(document.tracks or []):
        out[f"/tracks/{i}/segments"] = len(track.segments or [])
        for j, segment in enumerate(track.segments or []):
            out[f"/tracks/{i}/segments/{j}/pips"] = len(segment.pips or [])
    for i, area in enumerate(document.areas or []):
        out[f"/areas/{i}/polygon"] = len(area.polygon or [])
    return out


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
        for kind in ("accept", "reject", "diagnose", "write"):
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
               "attachment_contents", "root", "refusal", "diagnostics"}
    # Fields this runner knowingly does not compare, and why.  A sidecar field
    # that is in neither set fails the test below rather than being ignored:
    # an unasserted expectation is how a fixture quietly becomes the format.
    UNCHECKED = {
        # A prose record of a limitation (SPEC 6.4), paired with an empty
        # `diagnostics` list which IS checked.
        "cannot_detect",
        # The verdict the JSON Schema reaches on this case.  It is about
        # `corpus/schema/vnx-0.2.schema.json`, not about any reader, so no
        # implementation runner compares it; `go/internal/vnxschema` is the one
        # thing that does, and it fails if a sidecar records no verdict.
        "schema",
    }

    def test_no_unchecked_entry_has_outlived_its_field(self) -> None:
        """An entry in UNCHECKED must name a field some sidecar still carries.

        The set exists to make an uncompared expectation visible.  A stale entry
        does the opposite: it makes the mechanism look more thorough than it is,
        and it is invisible because nothing goes red when the field goes away.
        Same shape as the finding this whole set was built for.
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

    def _check_attachments(self, expect: dict, result: Any, name: str) -> None:
        """The attachment NAMES, and the BYTES the sidecar pins for them.

        `attachments` alone cannot see a reader that hands back names with no
        way to reach a content -- which is what this library did until the
        container carried `Attachment(path, bytes)`.  `attachment_contents`
        pins the bytes, as UTF-8 text, so the names are provably attached to
        the right payloads.

        It is a SUBSET of the entries, not a second listing of them: a sidecar
        pins the content of the entries it cares to, and
        `diagnose/container-unusable-method.vnz` lists only the readable one
        because the other was dropped on its compression method.
        """
        attachments = getattr(result, "attachments", [])
        if "root" in expect:
            self.assertEqual(
                getattr(result, "document_name", None),
                expect["root"],
                f"{name}: the entry the document was read from",
            )
        if "attachments" in expect:
            self.assertEqual(
                sorted(a.path for a in attachments),
                sorted(expect["attachments"]),
                f"{name}: attachments",
            )
        if "attachment_contents" not in expect:
            return
        bodies = {a.path: a.bytes for a in attachments}
        for entry, text in expect["attachment_contents"].items():
            self.assertIn(entry, bodies, f"{name}: no attachment {entry!r}")
            self.assertEqual(
                bodies[entry],
                text.encode("utf-8"),
                f"{name}: the bytes of attachment {entry!r}",
            )

    def _check_counts(self, expect: dict[str, Any], document: vnx.Document, name: str) -> None:
        """SPEC 8.1's `counts`, under the completeness rule ruled 2026-09-09.

        A sidecar must name every collection its document carries.  An
        application key is not a collection and has no entry (SPEC 3.9); the
        tree comparison of `test_accept_files_survive_a_read_and_a_write` is
        what checks it.  Before that, an omitted collection meant "unchecked", so a
        decoder could invent or drop items in a collection no sidecar named and
        stay green.  Enforcing it is the point: a rule most files satisfy by
        accident is one the next file breaks silently.

        The same rule now covers the NESTED containers, addressed by the same
        JSON Pointer vocabulary `positions` uses: a bare name is a top-level
        collection, a name starting with `/` is a container inside one.  That is
        where the guarantee comes from -- see `_container_counts_by_pointer` for
        what it closes and what it does not.
        """
        counts = document.counts()
        counts.update(_container_counts_by_pointer(document))
        pinned = expect.get("counts") or {}
        for collection, want in pinned.items():
            self.assertIn(
                collection,
                counts,
                f"{name}: the sidecar counts {collection!r}, which is not a "
                "collection the document carries",
            )
            self.assertEqual(counts[collection], want, f"{name}: {collection} count")
        for collection, got in counts.items():
            if got:
                self.assertIn(
                    collection,
                    pinned,
                    f"{name}: the document carries {got} {collection} and the "
                    "sidecar does not name that container",
                )

    def _check_positions(self, expect: dict[str, Any], document: vnx.Document, name: str) -> None:
        """SPEC 8.1's `positions`, compared BY BIT PATTERN.

        A value comparison cannot see a one-ulp move between the parse and the
        model, and when it fails it prints two numbers that look identical, so
        the failure reads as a broken test rather than as a broken reader.

        It also cannot tell `-0.0` from `0.0`, because `-0.0 == 0.0` is True.
        Every tree comparison in this suite is by value, so negative zero has
        only one other instrument in the whole corpus -- the number table.

        ⚠ AND NO FIXTURE REACHES THAT HALF TODAY. Not one of the 19 pinned
        positions in accept/ and diagnose/ carries a zero longitude or latitude,
        so a reader flipping every `0.0` to `-0.0` passes this check green.
        Measured, not assumed. The comparison is bit-exact and would catch it;
        the corpus is what does not ask. Said here so the reach is not assumed
        by the next reader of this method.

        ⚠ IT CANNOT SEE A PARSER THAT IS ONE ULP OUT. The sidecar's expected
        value is read by the same parser, so such a parser is wrong on both
        sides and the two agree. This check is about anything that moves a value
        BETWEEN the parse and the model; a parser's own accuracy is a separate
        test's job.
        """
        for pinned in expect.get("positions") or []:
            position = resolve(document, pinned["at"])
            self.assertIsInstance(
                position, vnx.Position, f"{name}: {pinned['at']} is not a position"
            )
            for channel, got in (("lon", position.lon), ("lat", position.lat)):
                want = float(pinned[channel])
                self.assertEqual(
                    _bits(got), _bits(want),
                    f"{name}: {pinned['at']} {channel} is {got!r} "
                    f"({_bits(got)}), the sidecar says {want!r} ({_bits(want)})",
                )

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
                self._check_attachments(expect, result, path.name)

    def test_accept_files_survive_a_read_and_a_write(self) -> None:
        """SPEC 8.1's check: DECODE the file, ENCODE the model, and require the
        two JSON TREES equal.

        ⚠ IT IS NOT A COMPARISON OF ONE MODEL AGAINST ANOTHER.  A reader that
        discarded every data record, every application key, every style and every
        `notes` produces a model that compares equal to itself and passes such a
        tier green -- that is the whole extensibility mechanism, all styling and
        all user prose.  Comparing against the tree parsed from the ORIGINAL
        FILE is what proves a reader kept what it read, and
        `test_red_proof.py` freezes the proof.

        ⚠ AND IT IS NOT A COMPARISON OF BYTES.  Key order, number spelling and
        whitespace are the writer's own business, so the two sides are parsed
        and compared as trees, with the normalisations SPEC 8.1 permits.
        """
        for path in _cases("accept"):
            if path.suffix != ".vnx":
                continue
            with self.subTest(case=path.name):
                original = path.read_bytes()
                document = vnx.read_bytes(original).document
                difference = tree.compare_documents(
                    vnx.encode_bytes(document),
                    original,
                    tree.Options(emit_boundary=True),
                )
                self.assertEqual(
                    difference, "",
                    f"{path.name}: encoding the decoded document did not "
                    "reproduce the file. Either the reader dropped something it "
                    "read, or the writer invented something",
                )


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
                self._check_attachments(expect, result, path.name)


class DiagnosticsAreDistinguishable(unittest.TestCase):
    """SPEC 6.3: no two diagnostics may share both a `path` and a `code`.

    A different kind of assertion from every other check in this file. The rest
    compare a reader against an expectation; this one asks whether the reader's
    own output is well formed at all, so it runs over every corpus document
    rather than against a sidecar, and no sidecar can turn it off.

    The hazard it names is specific. A path and a code are the only two fields a
    conforming runner may compare, so two reports differing in neither are two a
    runner cannot tell apart -- and a set matcher pairing each expectation with
    one report would pass a reader that found ONE of two faults twice.

    SPEC 6.3 stated this inverted when the check went in -- "may share a `path`
    unless they also share a `code`", permitting exactly the duplicate its own
    next sentence forbade -- so the check was written against the reason rather
    than the letter. The sentence now says BOTH (`a824b1c`) and the two agree.
    """

    def documents(self) -> list[tuple[str, list[Any]]]:
        out: list[tuple[str, list[Any]]] = []
        for tier in ("accept", "diagnose", "reject", "write"):
            if not (CORPUS / tier).is_dir():
                continue
            for path in _cases(tier):
                data = path.read_bytes()
                try:
                    if path.suffix == ".vnz":
                        out.append(
                            (f"{tier}/{path.name}",
                             list(vnx.read_container_bytes(data).diagnostics))
                        )
                    else:
                        out.append(
                            (f"{tier}/{path.name}",
                             list(vnx.read_bytes(data).diagnostics))
                        )
                except vnx.VNXRefusal:
                    # A refused document has no diagnostic set to be well
                    # formed.  The reject tier already pins the refusal.
                    continue
        return out

    def test_every_corpus_document_reports_distinguishable_diagnostics(self) -> None:
        documents = self.documents()
        # Liveness: a check that walked nothing would pass silently, and the
        # corpus grows by files this test never names.
        self.assertGreater(len(documents), 20, "the walk found almost nothing")
        for name, diagnostics in documents:
            with self.subTest(case=name):
                seen: dict[tuple[str, Any], int] = {}
                for diagnostic in diagnostics:
                    key = (diagnostic.path, diagnostic.code)
                    seen[key] = seen.get(key, 0) + 1
                repeated = {key: n for key, n in seen.items() if n > 1}
                self.assertEqual(
                    repeated, {},
                    f"{name}: two reports a runner cannot tell apart",
                )


class WriteTier(unittest.TestCase):
    """Decode an input and call the WRITE path (SPEC 4.1).

    The only tier that reaches the emit boundary. The others re-encode what they
    decoded, which shortens positions that are already short, so the
    shortest-spelling rule is invisible to them.

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
            if (directory / f"{stem}.attachments.json").is_file():
                # A case carrying an attachment manifest writes a CONTAINER, so
                # its expected output is an archive.  See
                # `corpus/write/README.md`.
                continue
            pairs.append((source, directory / f"{stem}.written.vnx"))
        return pairs

    def _container_cases(
        self,
    ) -> list[tuple[pathlib.Path, pathlib.Path, pathlib.Path]]:
        directory = CORPUS / "write"
        if not directory.is_dir():
            return []
        cases = []
        for manifest in sorted(directory.glob("*.attachments.json")):
            stem = manifest.name[: -len(".attachments.json")]
            cases.append(
                (
                    directory / f"{stem}.in.vnx",
                    manifest,
                    directory / f"{stem}.written.vnz",
                )
            )
        return cases

    @staticmethod
    def _manifest(path: pathlib.Path) -> dict[str, bytes]:
        """The manifest's attachments, in the order it lists them.

        They are in a manifest rather than in real files beside the sidecar
        because one name holds U+1F600, and a filename is subject to the
        checkout filesystem's Unicode normalisation and case folding -- so a
        checked-out file could arrive under a name that is not the one the case
        means, and the fixture would test the filesystem.
        """
        listed = json.loads(path.read_text())["attachments"]
        return {a["name"]: a["body"].encode("utf-8") for a in listed}

    def _check_decode(self, source: pathlib.Path, result: "vnx.ReadResult") -> None:
        """The input's decode reports exactly the sidecar's `diagnostics`,
        compared as the diagnose tier compares them; absent, nothing.  A
        write case with diagnostics writes back what a reader repaired."""
        stem = source.name[: -len(".in.vnx")]
        expect = json.loads((CORPUS / "write" / f"{stem}.expect.json").read_text())
        problem = match_diagnostics(expect.get("diagnostics", []), result.diagnostics)
        self.assertIsNone(problem, f"{source.name}: {problem}")

    def test_every_input_has_its_written_bytes_and_sidecar(self) -> None:
        pairs = self._pairs()
        self.assertTrue(pairs, "write/ holds no cases")
        for source, written in pairs:
            stem = source.name[: -len(".in.vnx")]
            self.assertTrue(written.is_file(), f"{source.name} has no written form")
            sidecar = CORPUS / "write" / f"{stem}.expect.json"
            self.assertTrue(sidecar.is_file(), f"{source.name} has no sidecar")
            self.assertEqual(json.loads(sidecar.read_text()).get("kind"), "write")

    def test_the_writer_emits_the_written_document(self) -> None:
        for source, written in self._pairs():
            with self.subTest(case=source.name):
                # `subTest` already contains a refusal to this case, so the
                # other cases still report; measured, not assumed. Catching it
                # is for the MESSAGE: "was refused: ..." names the cause, where
                # an uncaught VNXRefusal names only its own text and leaves the
                # reader to work out which tier raised it.
                try:
                    result = vnx.read_bytes(source.read_bytes())
                    got = vnx.encode_bytes(result.document)
                except vnx.VNXRefusal as refusal:
                    self.fail(f"{source.name} was refused: {refusal}")
                self._check_decode(source, result)
                self.assertEqual(
                    tree.compare_documents(got, written.read_bytes(), _WRITE_TIER),
                    "",
                    f"{source.name}: the written document is not the one the "
                    "case expects",
                )

    def test_the_input_is_not_already_the_written_form(self) -> None:
        """Otherwise a library that echoed its input would pass."""
        for source, written in self._pairs():
            with self.subTest(case=source.name):
                self.assertNotEqual(
                    tree.compare_documents(
                        source.read_bytes(), written.read_bytes(), _WRITE_TIER
                    ),
                    "",
                    f"{source.name} is already the written form; this case "
                    "would pass vacuously",
                )

    def test_the_written_form_is_a_fixed_point(self) -> None:
        """Shortening is idempotent, so a case that fails this is a defect in
        the fixture rather than in the writer."""
        for _, written in self._pairs():
            with self.subTest(case=written.name):
                again = vnx.read_bytes(written.read_bytes())
                self.assertIsNone(match_diagnostics([], again.diagnostics),
                                  f"{written.name} must read cleanly")
                self.assertEqual(
                    tree.compare_documents(
                        vnx.encode_bytes(again.document), written.read_bytes(), _WRITE_TIER
                    ),
                    "",
                )

    def test_every_container_case_has_its_archive_and_sidecar(self) -> None:
        cases = self._container_cases()
        self.assertTrue(cases, "write/ holds no container case")
        for source, manifest, written in cases:
            stem = source.name[: -len(".in.vnx")]
            self.assertTrue(source.is_file(), f"{manifest.name} has no input")
            self.assertTrue(written.is_file(), f"{source.name} has no written archive")
            sidecar = CORPUS / "write" / f"{stem}.expect.json"
            self.assertTrue(sidecar.is_file(), f"{source.name} has no sidecar")
            expect = json.loads(sidecar.read_text())
            self.assertEqual(expect.get("kind"), "write")
            self.assertEqual(
                expect.get("root"),
                "doc.vnx",
                "this runner writes the document as doc.vnx",
            )

    def test_the_writer_emits_the_written_archive(self) -> None:
        """The attachments are fed REVERSED.

        A writer's output must not depend on the order it was handed, and
        feeding them in manifest order cannot see one that does.

        The archive's BYTES are not compared: SPEC 7.1 constrains five rules and
        leaves every other field to the writer, so two conforming writers are
        not expected to agree byte for byte.

        ⚠ AND ATTACHMENT ORDER IS ONE OF THE FIELDS IT LEAVES.  The root
        document being FIRST is a rule; where the attachments fall after it is
        not, and "Every other field of the archive is the writer's own
        business" says so.  So the attachments are compared as an
        ORDER-INSENSITIVE collection -- every name present, every body correct
        -- and a comparison of the entry list in archive order would enforce a
        rule the specification disclaims.
        """
        import zipfile

        for source, manifest, written in self._container_cases():
            with self.subTest(case=source.name):
                attachments = self._manifest(manifest)
                reversed_order = dict(reversed(list(attachments.items())))
                try:
                    result = vnx.read_bytes(source.read_bytes())
                    got = vnx.encode_container_bytes(result.document, reversed_order)
                except vnx.VNXRefusal as refusal:
                    self.fail(f"{source.name} was refused: {refusal}")
                self._check_decode(source, result)
                with zipfile.ZipFile(io.BytesIO(got)) as archive:
                    names = archive.namelist()
                    bodies = {name: archive.read(name) for name in names}
                with zipfile.ZipFile(io.BytesIO(written.read_bytes())) as archive:
                    want_names = archive.namelist()
                    want_root = archive.read(want_names[0])
                self.assertEqual(
                    names[0], "doc.vnx",
                    "SPEC 7.1 requires the root document first",
                )
                self.assertEqual(sorted(names[1:]), sorted(want_names[1:]))
                self.assertEqual(
                    tree.compare_documents(bodies[names[0]], want_root, _WRITE_TIER),
                    "",
                    f"{source.name}: the archive's root document is not the one "
                    "the case expects",
                )
                # An attachment is bytes and is compared as bytes.  Nothing in
                # this format interprets one.
                for name, body in attachments.items():
                    self.assertEqual(bodies[name], body, name)

    def test_the_container_input_is_not_already_the_written_form(self) -> None:
        for source, _, written in self._container_cases():
            with self.subTest(case=source.name):
                self.assertNotEqual(source.read_bytes(), written.read_bytes())

    def test_the_written_archive_is_a_fixed_point(self) -> None:
        for _, manifest, written in self._container_cases():
            with self.subTest(case=written.name):
                read = vnx.read_container_bytes(written.read_bytes())
                self.assertEqual(read.diagnostics, [])
                self.assertEqual(
                    {a.path: a.bytes for a in read.attachments},
                    self._manifest(manifest),
                )
                again = vnx.read_container_bytes(
                    vnx.encode_container_bytes(read.document, self._manifest(manifest))
                )
                self.assertEqual(
                    sorted(again.attachments, key=lambda a: a.path),
                    sorted(read.attachments, key=lambda a: a.path),
                )
                self.assertEqual(
                    tree.compare_documents(
                        vnx.encode_bytes(again.document),
                        vnx.encode_bytes(read.document),
                        _WRITE_TIER,
                    ),
                    "",
                )


if __name__ == "__main__":
    unittest.main()


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
