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

# The write tier normalises ALMOST NOTHING.  The accept tier rounds both sides
# and drops a trailing `null` from both, because it compares a document against
# a re-encoding of itself.  Doing that here would normalise away the two rules
# under test.  The one normalisation left is the one that is not about the emit
# boundary at all: SPEC 3.1's absent-versus-empty.
_WRITE_TIER = tree.Options(emit_boundary=False)


def _bits(value: float) -> str:
    """A double's IEEE 754 encoding, as 16 hexadecimal digits."""
    return struct.pack(">d", value).hex()
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
        discarded every data record, every ruleset, every style and every
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
    decoded, which applies rounding to values that already satisfy it and
    shortening to positions already short, so rounding, the shortest-spelling
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
                    document = vnx.read_bytes(source.read_bytes()).document
                    got = vnx.encode_bytes(document)
                except vnx.VNXRefusal as refusal:
                    self.fail(f"{source.name} was refused: {refusal}")
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
        """Rounding and shortening are both idempotent, so a case that fails
        this is a defect in the fixture rather than in the writer."""
        for _, written in self._pairs():
            with self.subTest(case=written.name):
                document = vnx.read_bytes(written.read_bytes()).document
                self.assertEqual(
                    tree.compare_documents(
                        vnx.encode_bytes(document), written.read_bytes(), _WRITE_TIER
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
                    document = vnx.read_bytes(source.read_bytes()).document
                    got = vnx.encode_container_bytes(document, reversed_order)
                except vnx.VNXRefusal as refusal:
                    self.fail(f"{source.name} was refused: {refusal}")
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


class NumbersTier(unittest.TestCase):
    """`corpus/numbers/numbers.tsv`: the rounding every implementation must
    perform on a given double (SPEC 4.1).

    A tier in standing and not in shape. A case in the document tiers is a
    document; a row here is a value, and its answer columns are the
    expectation, so there is no sidecar.

    It reaches the rounding of SPEC 4.1 for the same reason `write/` does, and
    it reaches it per value rather than per document, which is what makes it the
    instrument for a rule that only fails on particular doubles.
    """

    TABLE = CORPUS / "numbers" / "numbers.tsv"

    def rows(self) -> list[tuple[str, str, str, str]]:
        if not self.TABLE.is_file():
            return []
        out = []
        for line in self.TABLE.read_text().splitlines():
            if not line or line.startswith("#"):
                continue
            bits, kind, round6, round1 = line.split("\t")
            out.append((bits, kind, round6, round1))
        return out

    def header(self) -> tuple[int, dict[str, int]]:
        """The declared row count and the declared count per kind.

        The generator refuses to write a header that disagrees with its body, so
        reading it is what makes a table that lost a category a failure here
        rather than a silence.  A hardcoded list of the categories this test
        depends on would go stale the moment one is added, and the newest
        category is always the one nothing pins.
        """
        rows, counts = -1, {}
        for line in self.TABLE.read_text().splitlines():
            if not line.startswith("#"):
                break
            if line.startswith("# rows="):
                rows = int(line[len("# rows="):].strip())
            if line.startswith("# counts:"):
                for pair in line[len("# counts:"):].split():
                    kind, _, n = pair.partition("=")
                    counts[kind] = int(n)
        return rows, counts

    def test_the_table_matches_the_header_it_declares(self) -> None:
        rows = self.rows()
        self.assertTrue(rows, f"no table at {self.TABLE}")
        declared_rows, declared_counts = self.header()
        self.assertEqual(len(rows), declared_rows, "the body and the header disagree on the row count")
        self.assertTrue(declared_counts, "the header declares no per-kind counts")
        seen: dict[str, int] = {}
        for _bits, kind, _round6, _round1 in rows:
            seen[kind] = seen.get(kind, 0) + 1
        self.assertEqual(seen, declared_counts, "the body and the header disagree on which kinds are present")

    def test_every_row_rounds_as_the_table_says(self) -> None:
        """⚠ IT TESTS THE ROUNDING OPERATION, NOT A SPELLING.

        How a number is spelled is the writer's own business (SPEC 4.2), and
        comparing spellings across implementations is the rule this format
        dropped.  What is compared is the rounded VALUE, by BIT PATTERN --
        comparing it as text would put a rendering rule back into the comparison
        through the side door, and would then pass or fail on how Python spells
        a double.
        """
        import struct

        from vnx.model import Position
        from vnx.writer import round_position

        failures: list[str] = []
        for bits, kind, round6, round1 in self.rows():
            value = struct.unpack(">d", bytes.fromhex(bits))[0]
            # Rounding is defined at the emit boundary, so it is exercised
            # through the writer rather than through a private helper: a test
            # calling something the writer does not call proves nothing about
            # what this library emits.
            got6 = round_position(Position(value, 0.0)).lon
            ele = round_position(Position(0.0, 0.0, value)).ele
            got1 = 0.0 if ele is None else ele
            for label, got, want in (
                ("round6", got6, round6),
                ("round1", got1, round1),
            ):
                # -0 and 0 are DIFFERENT bit patterns and the same value;
                # SPEC 4.1 has rounding normalise the sign, so the table and the
                # library agree on +0 and a mismatch here is real.
                if _bits(got) != _bits(float(want)):
                    failures.append(
                        f"{bits} ({kind}) {label}: got {got!r} "
                        f"({_bits(got)}), table says {want} "
                        f"({_bits(float(want))})"
                    )
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
