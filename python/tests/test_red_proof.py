"""Proof that the corpus runner would catch a defect.

"The corpus passes" is not evidence that the corpus discriminates.  Each test
here breaks one thing on purpose, runs the corpus check that is supposed to
notice, and asserts it goes red -- then unbreaks it.  A check that stays green
under its own defect is not a check, and this file is where that would show.

The defects are chosen to be the ones the corpus README says its fields exist to
catch, plus the ones this implementation had to decide for itself.
"""

from __future__ import annotations

import contextlib
import dataclasses
import io
import pathlib
import unittest
from typing import Any, Callable
from unittest import mock

import vnx
from vnx import decode, model
from vnx import walk as W
from vnx.model import Position

from . import test_corpus, test_reader, test_writer, tree

_MODULES = {"reader:": test_reader, "writer:": test_writer}


def _run(name: str) -> unittest.TestResult:
    module = test_corpus
    for prefix, target in _MODULES.items():
        if name.startswith(prefix):
            module, name = target, name.removeprefix(prefix)
            break
    suite = unittest.TestLoader().loadTestsFromName(name, module)
    runner = unittest.TextTestRunner(stream=io.StringIO(), verbosity=0)
    return runner.run(suite)


@contextlib.contextmanager
def _both(*patches: Any) -> Any:
    """Several `mock.patch` objects as one context manager.

    Deferred, not entered on construction: `assertGoesRed` runs the check green
    again after leaving the block, and a defect that was already live before the
    `with` would make that second run meaningless.
    """
    with contextlib.ExitStack() as stack:
        for patch in patches:
            stack.enter_context(patch)
        yield stack


def _round_positions(value: Any) -> Any:
    """Every position in an encoded tree, rounded the way SPEC 4.1 used to
    require.  A position is the only list of numbers a tree carries outside the
    opaque regions, and the write fixture has no opaque region."""
    if isinstance(value, dict):
        return {key: _round_positions(item) for key, item in value.items()}
    if isinstance(value, list):
        if len(value) >= 2 and all(
            isinstance(channel, float) or channel is None for channel in value
        ):
            out = list(value)
            for index, places in ((0, 6), (1, 6), (2, 1)):
                if index < len(out) and isinstance(out[index], float):
                    out[index] = float(f"{out[index]:.{places}f}")
            return out
        return [_round_positions(item) for item in value]
    return value


def _red_cases(result: unittest.TestResult) -> set[str]:
    """The corpus cases a run failed, by name.  A failure outside a subtest
    names the test method instead, so it is never mistaken for a case."""
    out: set[str] = set()
    for test, _ in result.failures + result.errors:
        params = getattr(test, "params", {})
        out.add(str(params.get("case", test.id())))
    return out


class RedProof(unittest.TestCase):
    def assertRedCases(
        self, defect: Any, check: str, cases: set[str], because: str
    ) -> None:
        """Like `assertGoesRed`, and the defect reddens EXACTLY *cases*.

        corpus/README.md, step 8: a mutation that reddens three cases cannot
        say which rule it broke, and one that reddens a case other than its own
        says the case is not checking what its note claims.
        """
        with defect:
            result = _run(check)
        self.assertEqual(
            _red_cases(result),
            cases,
            f"{check} with a broken {because} reddened the wrong set of cases",
        )
        self.assertTrue(_run(check).wasSuccessful(), f"{check} did not come back green")

    def assertGoesRed(self, defect: Any, check: str, because: str) -> None:
        with defect:
            result = _run(check)
        self.assertFalse(
            result.wasSuccessful(),
            f"{check} stayed green with a broken {because}; it is not checking it",
        )
        self.assertTrue(_run(check).wasSuccessful(), f"{check} did not come back green")

    # -- the checks the corpus README names -----------------------------

    def test_transposed_lon_lat_is_caught(self) -> None:
        """`positions` is the only defence against a lon/lat transposition."""
        original = decode._Reader._position

        # ⚠ THIS DEFECT ONCE READ `got.channels`, WHICH `Position` DOES NOT
        # HAVE.  Every call raised, the accept tier went red on the exception,
        # and the proof passed without ever transposing anything.  The swap is
        # now real, and the second assertion below shows it moved a value.
        def transposing(self: Any, value: Any, path: str, code: Any = None) -> Any:
            got = original(self, value, path, code)
            if got is None:
                return None
            return dataclasses.replace(got, lon=got.lat, lat=got.lon)

        with mock.patch.object(decode._Reader, "_position", transposing):
            swapped = vnx.read_bytes(b'{"vnx": "0.2.0", "waypoints": [{"at": [1, 2]}]}')
        self.assertEqual(
            swapped.document.waypoints, [model.Waypoint(at=Position(2.0, 1.0))]
        )

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "_position", transposing),
            "AcceptTier.test_accept",
            "position decoder that swaps longitude and latitude",
        )

    def test_a_vacuous_decoder_is_caught(self) -> None:
        """`counts` stops an accept file passing vacuously."""
        self.assertGoesRed(
            mock.patch.object(
                model.Document,
                "counts",
                lambda self: {key: 0 for key in test_corpus.COLLECTIONS},
            ),
            "AcceptTier.test_accept",
            "decoder that returns an empty document",
        )

    def test_an_invented_item_is_caught(self) -> None:
        """`counts` completeness, ruled 2026-09-09.

        The defect this closes: a decoder that invents items in a collection no
        sidecar names.  Inventing an area in every document that has none is
        invisible unless `counts` has to name every collection its document
        carries: most accept sidecars pin no `areas` at all.
        """
        original = decode._Reader.document

        def inventive(self: Any, value: Any) -> Any:
            got = original(self, value)
            if not got.areas:
                got.areas = [model.Area(name="invented")]
            return got

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "document", inventive),
            "AcceptTier.test_accept",
            "decoder that invents an area",
        )

    def test_an_invented_application_key_is_caught(self) -> None:
        """SPEC 3.9: application keys have no `counts` entry, so the tree
        comparison is the only check on them, and it must see one invented."""
        original = decode._Reader.document

        def inventive(self: Any, value: Any) -> Any:
            got = original(self, value)
            got.app_keys = got.app_keys + [model.AppKey("com.example.invented", None)]
            return got

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "document", inventive),
            "AcceptTier.test_accept_files_survive_a_read_and_a_write",
            "decoder that invents an application key",
        )

    def test_the_old_counts_rule_could_not_see_that(self) -> None:
        """The finding, frozen, so the weaker rule is not reintroduced.

        Under "an omitted collection is unchecked" the same defect is invisible.
        If this ever goes red, the weaker rule has come back.
        """
        original = decode._Reader.document

        def inventive(self: Any, value: Any) -> Any:
            got = original(self, value)
            if not got.areas:
                got.areas = [model.Area(name="invented")]
            return got

        def named_keys_only(self: Any, expect: Any, document: Any, name: str) -> None:
            # The nested-container walk stays: what is frozen here is the
            # COMPLETENESS half of the rule -- "an omitted container is
            # unchecked" -- not the pointer vocabulary the sidecars address
            # containers with.  Without the walk this stub dies on a KeyError
            # for a `/...` key it cannot resolve, and a frozen finding that goes
            # red for a reason unrelated to the finding is the exact failure
            # mode that gets frozen tests deleted as flaky.  Leave it in.
            counts = document.counts()
            counts.update(test_corpus._container_counts_by_pointer(document))
            for collection, want in (expect.get("counts") or {}).items():
                self.assertEqual(counts[collection], want)

        with (
            mock.patch.object(decode._Reader, "document", inventive),
            mock.patch.object(
                test_corpus._CorpusCase, "_check_counts", named_keys_only
            ),
        ):
            self.assertTrue(_run("AcceptTier.test_accept").wasSuccessful())

    # -- the nested `counts` containers (D2) ----------------------------
    #
    # What these freeze.  SPEC 8.1 exempts the `diagnose/` tier from the round
    # trip, for the good reason that a diagnose case is a document the reader
    # is SUPPOSED to come back from with less than it was given.  That
    # exemption is the hole: with no round trip and only top-level counts,
    # nothing looked at how much less.  In 0.2 the scoped drop that matters is
    # one chain of several: a reader whose dropped chain takes its siblings
    # with it passes every top-level count, because the network is still there.

    @staticmethod
    def _over_broad_chain_drop() -> Any:
        """A reader whose scoped drop is over-broad: one dropped chain takes
        every chain of its network with it."""
        original = decode._Reader._chains

        def emptying(self: Any, obj: Any, path: str, arcs: Any) -> Any:
            chains = original(self, obj, path, arcs)
            if chains is not None and len(chains) < len(obj.get("chains") or []):
                return []
            return chains

        return mock.patch.object(decode._Reader, "_chains", emptying)

    def test_an_over_broad_scoped_drop_is_caught(self) -> None:
        """The nested `counts` containers, ruled 2026-09-14.

        Every case that drops one chain of two names
        `/networks/0/chains: 1`, and the over-broad reader leaves 0.

        ⚠ `positions` IS A SECOND GUARD HERE, and it is switched off so that
        this proof credits `counts` alone.  The 0.2 cases pin a position on
        the SURVIVING chain (`/networks/0/chains/0/walk/0/to`), which an
        over-broad reader no longer has.  Measured: with `counts` cut back to
        the top level, those pins still redden all six cases.
        """
        unchecked = mock.patch.object(
            test_corpus._CorpusCase,
            "_check_positions",
            lambda self, expect, document, name: None,
        )
        self.assertRedCases(
            _both(self._over_broad_chain_drop(), unchecked),
            "DiagnoseTier.test_diagnose",
            {
                "arc-names-absent-node.vnx",
                "pass-names-absent-arc.vnx",
                "pinned-turn.vnx",
                "position-out-of-range.vnx",
                "walk-from-not-an-end.vnx",
                "walk-last-pass-lacks-to.vnx",
                "walk-leaves-at-a-position-early.vnx",
                "walk-not-connected.vnx",
                "walk-position-unusable.vnx",
                "walk-spelling.vnx",
                "walk-to-forbidden.vnx",
                "walk-to-not-an-end.vnx",
            },
            "reader whose dropped chain takes its siblings with it",
        )

    def test_the_top_level_counts_rule_could_not_see_that(self) -> None:
        """The finding, frozen, so the weaker rule is not reintroduced.

        Counting only the six top-level collections, the over-broad drop above
        is invisible to `counts`: the document still carries one network.  And
        `diagnose/` has no round trip to catch what `counts` misses.  With
        `positions` also off -- see the note above -- nothing else sees it.  If
        this ever goes red, something new is guarding the scoped drop; say so.
        """

        def top_level_only(self: Any, expect: Any, document: Any, name: str) -> None:
            counts = document.counts()
            pinned = {
                key: want
                for key, want in (expect.get("counts") or {}).items()
                if not key.startswith("/")
            }
            for collection, want in pinned.items():
                self.assertEqual(counts[collection], want)
            for collection, got in counts.items():
                if got:
                    self.assertIn(collection, pinned)

        with (
            self._over_broad_chain_drop(),
            mock.patch.object(test_corpus._CorpusCase, "_check_counts", top_level_only),
            mock.patch.object(
                test_corpus._CorpusCase,
                "_check_positions",
                lambda self, expect, document, name: None,
            ),
        ):
            self.assertTrue(_run("DiagnoseTier.test_diagnose").wasSuccessful())

    def test_a_dropped_route_arc_was_already_caught_without_counts(self) -> None:
        """The correction, frozen: `counts` is not what catches this one.

        A reader that drops a route's arcs is caught in the accept tier by the
        decode-encode-tree comparison, with `counts` removed from the run
        entirely.  Said here because the claim that it was NOT caught is what
        made the nested containers look like an accept-tier fix; they are a
        `diagnose/`-tier fix, and a red proof that leaves `counts` switched on
        cannot tell the two guards apart.

        ⚠ THE GUARD IS `test_accept_files_survive_a_read_and_a_write`, NOT
        `test_accept`.  In this runner the round trip is its own test method, so
        `test_accept` -- with `counts` switched off -- stays GREEN under this
        defect.  Measured, having first aimed this proof at `test_accept` and
        watched it come back green.  The reference runner puts both in one
        subtest, which is exactly how a fault caught by one guard gets credited
        to the other.
        """
        original = decode._Reader.document

        def truncating(self: Any, value: Any) -> Any:
            got = original(self, value)
            for route in got.routes or []:
                if route.arcs:
                    first = sorted(route.arcs)[0]
                    route.arcs = {first: route.arcs[first]}
            return got

        def unchecked(self: Any, expect: Any, document: Any, name: str) -> None:
            return None

        with mock.patch.object(test_corpus._CorpusCase, "_check_counts", unchecked):
            self.assertGoesRed(
                mock.patch.object(decode._Reader, "document", truncating),
                "AcceptTier.test_accept_files_survive_a_read_and_a_write",
                "reader that drops a route's arcs, with `counts` switched off",
            )

    def test_moving_time_reported_as_zero_is_caught(self) -> None:
        """`moving_time_ms: null` means unknown, which is not zero."""
        self.assertGoesRed(
            mock.patch.object(model.Track, "moving_time_ms", lambda self: 0.0),
            "AcceptTier.test_accept",
            "track that answers 0 for a moving time it cannot know",
        )

    def test_moving_time_summed_across_the_pause_is_caught(self) -> None:
        """A pause is not walking: the wall-clock span is not the moving time."""

        def wall_clock(self: Any) -> Any:
            times = [
                pip.time_ms
                for segment in (self.segments or [])
                for pip in (segment.pips or [])
                if pip.time_ms is not None
            ]
            return times[-1] - times[0] if times else None

        self.assertGoesRed(
            mock.patch.object(model.Track, "moving_time_ms", wall_clock),
            "AcceptTier.test_accept",
            "track that sums first-to-last across the whole track",
        )

    # -- the writer emit path -------------------------------------------

    def test_a_writer_that_changes_a_value_is_caught(self) -> None:
        """SPEC 4.1 no longer constrains a writer's precision, so a writer that
        rounds is CONFORMING -- and still fails the write tier, because the tier
        compares what the writer produced against the document it was given.

        That is the assertion `corpus/write/emit-boundary` makes by holding
        four values at 7+ decimal places and changing none of them, and it is
        worth a red proof: after the rounding rules left, a comparison that
        quietly normalised precision on both sides would look exactly as green.
        """
        original = vnx.writer.encode_document

        def rounding(document: Any) -> Any:
            tree_ = original(document)
            return _round_positions(tree_)

        self.assertGoesRed(
            mock.patch.object(vnx.writer, "encode_document", rounding),
            "WriteTier.test_the_writer_emits_the_written_document",
            "writer that rounds a coordinate on the way out",
        )

    def test_a_writer_that_drops_an_interior_null_is_caught(self) -> None:
        """SPEC 4.1: `[lon, lat, null, time]` cannot be shortened, because
        dropping the null would move the time into the elevation channel."""
        original = vnx.encode._position

        def greedy(position: Any) -> Any:
            got = original(position)
            return [channel for channel in got if channel is not None]

        self.assertGoesRed(
            mock.patch.object(vnx.encode, "_position", greedy),
            "WriteTier.test_the_writer_emits_the_written_document",
            "writer that drops every null rather than the trailing ones",
        )

    def test_a_dropped_application_key_is_caught(self) -> None:
        """SPEC 3.9: an application key is opaque and MUST survive a
        read-modify-write."""
        # Patched where the WRITER reaches it: `writer.py` imported the name,
        # so patching `vnx.encode` would leave the writer calling the original
        # and report a blind test that is not blind.
        self.assertGoesRed(
            mock.patch.object(vnx.writer, "encode_document", _without_app_keys()),
            "AcceptTier.test_accept_files_survive_a_read_and_a_write",
            "encoder that drops application keys",
        )

    def test_an_emptied_application_key_is_caught(self) -> None:
        """SPEC 3.9: a key holding null or an empty value is content.  An
        encoder that skips such a key must go red, which it can only do if the
        tree comparison does not normalise the empty value away."""
        original = vnx.encode.encode_document

        def skips_empty(document: Any) -> Any:
            obj = original(document)
            for key in [k for k in obj if model.is_app_key(k)]:
                if obj[key] in (None, [], {}):
                    del obj[key]
            return obj

        self.assertGoesRed(
            mock.patch.object(vnx.writer, "encode_document", skips_empty),
            "AcceptTier.test_accept_files_survive_a_read_and_a_write",
            "encoder that skips an application key holding null or an empty value",
        )

    def test_a_flattened_data_record_is_caught(self) -> None:
        """SPEC 5.1.4: an unrecognised record survives at value level."""
        original = vnx.encode._records

        def emptied(items: Any) -> Any:
            got = original(items)
            if got is None:
                return None
            return [{"@type": record["@type"]} for record in got]

        self.assertGoesRed(
            mock.patch.object(vnx.encode, "_records", emptied),
            "AcceptTier.test_accept_files_survive_a_read_and_a_write",
            "encoder that keeps a record's type but not its body",
        )

    def test_the_accept_tier_catches_a_reader_that_discards_what_it_read(
        self,
    ) -> None:
        """⚠ THIS IS WHY THE TIER COMPARES AGAINST THE ORIGINAL FILE.

        A reader that discarded every data record, every application key, every style or
        every `notes` produces a model that compares equal to itself.  That is
        the whole extensibility mechanism, all styling and all user prose,
        silently droppable by a "conforming" implementation.

        Each case below is one of those readers.  If any of them stops being
        caught, the tier has gone back to proving nothing about the model.
        """
        for name, defect in _SPOILED_READERS:
            with self.subTest(reader=name):
                self.assertGoesRed(
                    defect(),
                    "AcceptTier.test_accept_files_survive_a_read_and_a_write",
                    f"reader that {name}",
                )

    def test_a_model_versus_model_comparison_would_see_none_of_them(self) -> None:
        """The other half, and the finding the check is built on.

        Repointed at a comparison of one decoded model against another, every
        spoiled reader above passes GREEN: it drops the same thing on both
        sides.  Measured here rather than argued, because the check that looks
        equivalent is the one an implementation reaches for.
        """
        files = sorted(CORPUS_ACCEPT.glob("*.vnx"))
        self.assertTrue(files, "the walk found no accept files")
        for name, defect in _SPOILED_READERS:
            with self.subTest(reader=name):
                blind = 0
                seen = 0
                for path in files:
                    source = path.read_bytes()
                    with defect():
                        document = vnx.read_bytes(source).document
                        once = vnx.encode_bytes(document)
                        twice = vnx.encode_bytes(vnx.read_bytes(once).document)
                        if tree.compare_documents(twice, once, tree.Options()) == "":
                            blind += 1
                        if (
                            tree.compare_documents(
                                once, source, tree.Options(emit_boundary=True)
                            )
                            != ""
                        ):
                            seen += 1
                self.assertEqual(
                    blind,
                    len(files),
                    "a model-versus-model comparison saw this reader on "
                    f"{len(files) - blind} file(s); if it can, say so and "
                    "revisit the note above",
                )
                self.assertGreater(
                    seen,
                    0,
                    "no accept file carries what this reader drops, so the "
                    "comparison against the file proves nothing about it",
                )

    # -- refusals -------------------------------------------------------

    def test_a_reader_that_never_refuses_is_caught(self) -> None:
        self.assertGoesRed(
            mock.patch.object(decode._Reader, "_check_version", lambda self, v: None),
            "RejectTier.test_reject",
            "reader that never refuses a version",
        )

    def test_a_version_check_that_only_looks_upward_is_caught(self) -> None:
        """The defect this library shipped, and five of the six had.

        SPEC 2.2 makes each 0.x MINOR its own series, so the test is "not the
        reader's series"; written as "above the reader's", a lower minor reads
        under rules it was never written to.  `reject/version-too-new.vnx`
        cannot see it -- that direction still refuses -- so the proof is that
        `reject/version-too-old.vnx` goes red on its own.
        """

        def upward_only(self: Any, version: str) -> None:
            match = decode._VERSION.match(version)
            if match is None:
                raise vnx.VNXRefusal(
                    f"unreadable version {version!r}: MAJOR.MINOR.PATCH"
                )
            pair = (int(match.group(1)), int(match.group(2)))
            if pair > decode.SUPPORTED_VERSION:
                raise vnx.VNXRefusal(
                    f"this document is version {version} and a newer reader is needed"
                )

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "_check_version", upward_only),
            "RejectTier.test_reject",
            "version check that only looks upward",
        )

    def test_a_permissive_json_parser_is_caught(self) -> None:
        """Some libraries offer a mode that accepts NaN.  It must not be used."""
        import json

        self.assertGoesRed(
            mock.patch(
                "vnx.jsonio.json.loads",
                lambda text, **kwargs: json.loads(
                    text, parse_int=float, parse_float=float
                ),
            ),
            "RejectTier.test_reject",
            "parser that accepts NaN and Infinity",
        )

    # -- diagnostics ----------------------------------------------------

    def test_a_reader_that_reports_nothing_is_caught(self) -> None:
        self.assertGoesRed(
            # `*args`: a defect with the wrong signature raises on every call,
            # and the tier goes red on the exception rather than the defect.
            mock.patch.object(decode._Reader, "note", lambda self, *args: None),
            "DiagnoseTier.test_diagnose",
            "reader that records no diagnostics",
        )

    def test_a_reader_that_emits_no_codes_is_caught(self) -> None:
        """SPEC 6.3: a diagnostic a corpus case pins MUST carry a code.

        Until 2026-09-09 the sidecars pinned English and this runner matched it
        as a substring.  When they moved to codes, the matcher looked for a
        `message` field that no longer existed and fell through to comparing
        paths alone -- every diagnose case green while checking half of what it
        pins.  Nothing went red, because a matcher that checks less always
        passes.  These three tests are what would have caught it.
        """
        original = decode._Reader.note

        self.assertGoesRed(
            mock.patch.object(
                decode._Reader,
                "note",
                lambda self, path, message, code=None: original(self, path, message),
            ),
            "DiagnoseTier.test_diagnose",
            "reader that reports no diagnostic codes",
        )

    def test_a_reader_that_emits_the_wrong_code_is_caught(self) -> None:
        original = decode._Reader.note

        self.assertGoesRed(
            mock.patch.object(
                decode._Reader,
                "note",
                lambda self, path, message, code=None: original(
                    self, path, message, "not-the-right-code"
                ),
            ),
            "DiagnoseTier.test_diagnose",
            "reader that reports the wrong diagnostic code",
        )

    def test_the_english_really_is_not_compared(self) -> None:
        """The other half, and the one that must STAY green.

        A reader whose wording shares nothing with the sidecar's still passes.
        If this ever goes red, English has crept back into the comparison and
        the corpus has started pinning translations of a rule.
        """
        original = decode._Reader.note

        with mock.patch.object(
            decode._Reader,
            "note",
            lambda self, path, message, code=None: original(
                self, path, "xyzzy plugh", code
            ),
        ):
            self.assertTrue(
                _run("DiagnoseTier.test_diagnose").wasSuccessful(),
                "a diagnostic's English is being compared; it must not be",
            )

    # -- the walk rules (SPEC 3.3.3, 6.2) --------------------------------
    #
    # One proof per check.  Each switches off exactly one rule and requires
    # EXACTLY its own corpus case to go red, then the unit tests that pin the
    # rule's other clauses, then the writer's refusal, because the writer runs
    # the same function (`vnx/walk.py`).

    def test_a_reader_with_no_continuity_check_is_caught(self) -> None:
        """Walk continuity: SPEC 3.3.3's valid-walk rules 3 to 5.

        The defect trusts every pass: it enters where the walk says and leaves
        by the other end, and never asks whether the arc touches either.
        """

        def trusting(at: Any, index: int, last: int, p: Any, arc: Any) -> Any:
            entry = p.from_ if p.from_ is not None else at
            if p.to is not None:
                return entry, p.to
            if p.turn is not None:
                return entry, entry
            return entry, arc.to if entry == arc.from_ else arc.from_

        defect = lambda: mock.patch.object(W, "connection", trusting)  # noqa: E731
        self.assertRedCases(
            defect(),
            "DiagnoseTier.test_diagnose",
            {
                "walk-from-not-an-end.vnx",
                "walk-leaves-at-a-position-early.vnx",
                "walk-not-connected.vnx",
                "walk-to-not-an-end.vnx",
            },
            "walk continuity check",
        )
        for test in (
            "test_8_a_from_node_that_is_not_an_end",
            "test_8_a_pass_that_does_not_join",
            "test_8_a_to_node_that_is_not_an_end",
            "test_8_a_position_exit_before_the_last_pass",
            "test_a_turn_leaves_by_the_node_it_entered",
        ):
            with self.subTest(test=test):
                self.assertGoesRed(
                    defect(), f"reader:Walks.{test}", "walk continuity check"
                )
        self.assertGoesRed(
            defect(),
            "writer:TheWriterRefusesWhatItsReaderDrops."
            "test_a_walk_that_breaks_a_rule_is_refused_naming_pass_and_code",
            "writer's walk continuity check",
        )

    def test_a_reader_with_no_spelling_rule_is_caught(self) -> None:
        defect = lambda: mock.patch.object(W, "spelling", lambda i, last, p: None)  # noqa: E731
        self.assertRedCases(
            defect(),
            "DiagnoseTier.test_diagnose",
            {
                "walk-last-pass-lacks-to.vnx",
                "walk-spelling.vnx",
                "walk-to-forbidden.vnx",
            },
            "rule for `from` and `to`",
        )
        for test in (
            "test_7_the_first_pass_lacks_from",
            "test_7_a_later_pass_carries_from",
            "test_7_the_last_pass_lacks_to",
            "test_7_a_middle_pass_carries_to",
            "test_7_a_position_from_needs_a_to",
        ):
            with self.subTest(test=test):
                self.assertGoesRed(
                    defect(), f"reader:Walks.{test}", "rule for `from` and `to`"
                )

    def test_a_reader_that_allows_a_pinned_turn_is_caught(self) -> None:
        defect = lambda: mock.patch.object(W, "pinned_turn", lambda p: None)  # noqa: E731
        self.assertRedCases(
            defect(),
            "DiagnoseTier.test_diagnose",
            {"pinned-turn.vnx"},
            "pinned-turn check",
        )
        self.assertGoesRed(
            defect(),
            "reader:Walks.test_a_pinned_turn_is_found_before_the_spelling",
            "pinned-turn check",
        )

    def test_a_reader_that_lets_a_route_carry_a_position_is_caught(self) -> None:
        defect = lambda: mock.patch.object(W, "route_rule", lambda p: None)  # noqa: E731
        self.assertRedCases(
            defect(),
            "DiagnoseTier.test_diagnose",
            {"route-pin-or-position.vnx"},
            "route rule",
        )
        self.assertGoesRed(
            defect(), "reader:Routes.test_each_forbidden_key_names_itself", "route rule"
        )
        self.assertGoesRed(
            defect(),
            "writer:TheWriterRefusesWhatItsReaderDrops."
            "test_a_route_carrying_a_position_is_refused",
            "writer's route rule",
        )

    def test_a_reader_that_trusts_every_arc_name_is_caught(self) -> None:
        """`pass-arc-absent`, and the cascade that ends in it.

        A pass naming a missing arc cannot be stepped at all, so the defect is
        the nearest one that runs: the chain is still dropped, and the
        diagnostic saying why is lost.  Four cases end in this code, three of
        them as the second half of a cascade.
        """
        original = decode._Reader._pass_arc

        def lenient(self: Any, item: Any, at: str, arcs: Any) -> Any:
            got = original(self, item, at, arcs)
            if got is None:
                self.diagnostics.pop()
            return got

        self.assertRedCases(
            mock.patch.object(decode._Reader, "_pass_arc", lenient),
            "DiagnoseTier.test_diagnose",
            {
                "arc-names-absent-node.vnx",
                "pass-names-absent-arc.vnx",
                "position-unusable.vnx",
                "self-loop-arc.vnx",
            },
            "check that a pass names an arc of its owner",
        )

    def test_only_the_first_fault_in_a_walk_is_reported(self) -> None:
        """SPEC 6.2: at the first check that drops the chain, nothing further
        in the walk is examined.  A reader that reported every fault it could
        find would be more informative and non-conforming, and no corpus case
        carries two faults in one walk, so the unit test is the guard."""
        original = W.Cursor.step

        def keeps_going(self: Any, index: int, p: Any, arc: Any) -> Any:
            got = original(self, index, p, arc)
            if isinstance(got, W.WalkFault) and got.code == "walk-spelling":
                # Report the connection fault behind the spelling fault too.
                self.at = p.from_ if isinstance(p.from_, str) else self.at
                leg = W.connection(self.at, index, self.last, p, arc)
                if isinstance(leg, W.Broken):
                    return W.WalkFault(
                        index, leg.key, "walk-not-connected", leg.message
                    )
            return got

        self.assertGoesRed(
            mock.patch.object(W.Cursor, "step", keeps_going),
            "reader:Walks.test_only_the_first_fault_is_reported",
            "reader that looks past the first fault in a walk",
        )

    # -- the graph and ring rules (SPEC 3.3.1, 3.3.2, 3.7) ---------------

    def test_a_reader_that_keeps_a_self_loop_is_caught(self) -> None:
        defect = lambda: mock.patch.object(W, "is_self_loop", lambda arc: False)  # noqa: E731
        self.assertRedCases(
            defect(),
            "DiagnoseTier.test_diagnose",
            {"self-loop-arc.vnx"},
            "self-loop check",
        )
        self.assertGoesRed(
            defect(),
            "writer:TheWriterRefusesWhatItsReaderDrops.test_a_self_loop_is_refused",
            "writer's self-loop check",
        )

    def test_a_reader_that_keeps_an_arc_naming_an_absent_node_is_caught(self) -> None:
        self.assertRedCases(
            mock.patch.object(
                decode._Reader, "_ends_present", lambda self, arc, here, nodes: True
            ),
            "DiagnoseTier.test_diagnose",
            {"arc-names-absent-node.vnx", "position-out-of-range.vnx"},
            "check that an arc's nodes are present",
        )

    def test_a_reader_that_keeps_a_time_on_an_arc_pip_is_caught(self) -> None:
        self.assertRedCases(
            mock.patch.object(
                decode._Reader, "_pip_times", lambda self, pips, here: pips
            ),
            "DiagnoseTier.test_diagnose",
            {"arc-pip-time.vnx"},
            "arc pip time check",
        )

    def test_a_reader_that_reports_an_arc_pip_time_and_keeps_it_is_caught(self) -> None:
        """The diagnose tier cannot see this: it never writes the document
        back.  write/arc-pip-time does, so a time left in the model reaches
        the writer."""
        keep = decode._Reader._pip_times

        def reports_and_keeps(self: Any, pips: Any, here: str) -> Any:
            keep(self, pips, here)
            return pips

        self.assertRedCases(
            mock.patch.object(decode._Reader, "_pip_times", reports_and_keeps),
            "WriteTier.test_the_writer_emits_the_written_document",
            {"arc-pip-time.in.vnx"},
            "arc pip time drop",
        )

    def test_a_reader_that_keeps_an_extra_channel_is_caught(self) -> None:
        self.assertRedCases(
            mock.patch.object(
                decode._Reader,
                "_extra_channel",
                lambda self, position, path, keep_ele: position,
            ),
            "DiagnoseTier.test_diagnose",
            {"extra-channel.vnx"},
            "extra-channel check",
        )

    def test_a_reader_with_no_coordinate_range_is_caught(self) -> None:
        self.assertRedCases(
            mock.patch.object(decode, "off_the_globe", lambda lon, lat: None),
            "DiagnoseTier.test_diagnose",
            {"position-out-of-range.vnx"},
            "longitude and latitude range check",
        )

    def test_a_reader_that_keeps_a_closed_ring_closed_is_caught(self) -> None:
        defect = lambda: mock.patch.object(W, "ring_is_closed", lambda ring: False)  # noqa: E731
        self.assertRedCases(
            defect(),
            "DiagnoseTier.test_diagnose",
            {"ring-closed.vnx"},
            "closed-ring check",
        )
        self.assertGoesRed(
            defect(),
            "writer:TheWriterRefusesWhatItsReaderDrops.test_a_closed_ring_is_refused_not_opened",
            "writer's closed-ring check",
        )

    def test_a_reader_that_keeps_a_short_ring_is_caught(self) -> None:
        defect = lambda: mock.patch.object(W, "distinct_lon_lat", lambda ring: 3)  # noqa: E731
        self.assertRedCases(
            defect(),
            "DiagnoseTier.test_diagnose",
            {"ring-distinct-by-lon-lat.vnx", "ring-too-few-points.vnx"},
            "distinct-positions check",
        )
        self.assertGoesRed(
            defect(),
            "reader:Polygons.test_a_repeated_interior_position_is_not_distinct",
            "distinct-positions check",
        )
        self.assertGoesRed(
            defect(),
            "writer:TheWriterRefusesWhatItsReaderDrops."
            "test_a_ring_of_two_distinct_positions_is_refused",
            "writer's distinct-positions check",
        )

    def test_a_reader_that_keeps_the_outer_ring_is_caught(self) -> None:
        """SPEC 3.7: drop the item, do not quietly keep ring 1."""
        original = decode._Reader._polygon

        def outer_ring(self: Any, value: Any, path: str) -> Any:
            if (
                isinstance(value, list)
                and value
                and isinstance(value[0], list)
                and value[0]
                and isinstance(value[0][0], list)
            ):
                return original(self, value[0], path)
            return original(self, value, path)

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "_polygon", outer_ring),
            "DiagnoseTier.test_diagnose",
            "reader that flattens a nested polygon to its outer ring",
        )

    def test_a_duplicate_key_diagnostic_would_go_red(self) -> None:
        """SPEC 6.4's limitation is recorded, not assumed permanent.

        `diagnose/duplicate-object-key.vnx` expects no diagnostic because no
        standard parser can see one.  If a reader ever gains that reach, this
        goes red and the limitation gets revisited rather than outliving its
        cause.
        """
        original = decode._Reader.document

        def noisy(self: Any, value: Any) -> Any:
            got = original(self, value)
            self.note("/networks/0/nodes/n1", "duplicate object key")
            return got

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "document", noisy),
            "DiagnoseTier.test_diagnose",
            "reader reporting a duplicate object key",
        )

    # -- containers -----------------------------------------------------

    def test_a_container_that_lists_its_document_as_an_attachment_is_caught(
        self,
    ) -> None:
        original = vnx.container.read_container_bytes

        def sloppy(data: bytes) -> Any:
            got = original(data)
            got.attachments = got.attachments + [vnx.Attachment(got.document_name, b"")]
            return got

        self.assertGoesRed(
            mock.patch.object(vnx, "read_container_bytes", sloppy),
            "AcceptTier.test_accept",
            "container reader that counts the document as an attachment",
        )

    def test_a_container_reader_that_returns_empty_attachments_is_caught(self) -> None:
        """`attachment_contents` is what pins a name to its BYTES.

        The name list alone cannot see a reader that hands back the right
        entries with the wrong -- or no -- content, which is the defect this
        library had in a stronger form: it returned names and no bytes at all.

        SPEC 7 forbids the emptied shape in as many words: an
        attachment whose bytes cannot be recovered is DROPPED with a
        diagnostic, and MUST NOT be returned with empty or partial content,
        because a caller cannot tell that from an attachment the archive never
        carried.  So this proof guards a MUST rather than a courtesy, and it is
        the only check that would see a reader taking the forbidden shortcut.
        """
        original = vnx.container.read_container_bytes

        def emptied(data: bytes) -> Any:
            got = original(data)
            got.attachments = [vnx.Attachment(a.path, b"") for a in got.attachments]
            return got

        self.assertGoesRed(
            mock.patch.object(vnx, "read_container_bytes", emptied),
            "DiagnoseTier.test_diagnose",
            "container reader that loses an attachment's bytes",
        )

    def test_a_container_reader_that_takes_the_last_vnx_entry_is_caught(self) -> None:
        """SPEC 7: the fallback is the FIRST .vnx entry in archive order."""
        # reject/container-duplicate-entries.vnz holds two doc.vnx entries; a
        # reader that did not refuse them would have to pick one.  The corpus
        # case that this defect breaks is the duplicate refusal.
        self.assertGoesRed(
            mock.patch.object(vnx.container, "_check_path", lambda name: None),
            "RejectTier.test_reject",
            "container reader that does not check attachment paths",
        )

    def test_a_reader_that_drops_the_pip_instead_of_the_container_is_caught(
        self,
    ) -> None:
        """SPEC 6.2: an unusable position drops the container, not the element.

        The element-dropping reader is the one that looks right -- it leaves a
        shorter, plausible line -- so it is the one worth proving against.
        """
        original = decode._Reader._positions

        def element_dropping(self: Any, obj: Any, key: str, path: str) -> Any:
            items = obj.get(key)
            if not isinstance(items, list):
                return original(self, obj, key, path)
            out = []
            for index, item in enumerate(items):
                got = self._position(item, f"{path}/{key}/{index}")
                if got is not None:
                    out.append(got)
            return out

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "_positions", element_dropping),
            "DiagnoseTier.test_diagnose",
            "reader that drops the offending pip instead of its container",
        )

    def test_two_reports_a_runner_cannot_tell_apart_are_caught(self) -> None:
        """SPEC 6.3's invariant, red-proofed by reporting per pip.

        `arc-pip-time` is reported ONCE for the arc, at its `pips`.
        `diagnose/arc-pip-time.vnx` has two timed pips on one arc, so a reader
        that reported once per timed pip at that path emits two diagnostics
        with one path and one code -- and the set matcher alone would pass a
        reader that found one fault twice.
        """

        def per_pip(self: Any, pips: Any, here: str) -> Any:
            for pip in pips:
                if pip.time_ms is not None:
                    self.note(f"{here}/pips", "a time", "arc-pip-time")
            return [Position(pip.lon, pip.lat, pip.ele) for pip in pips]

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "_pip_times", per_pip),
            "DiagnosticsAreDistinguishable."
            "test_every_corpus_document_reports_distinguishable_diagnostics",
            "reader reporting an arc's pip times once per pip",
        )

    def test_a_walk_that_finds_nothing_is_caught(self) -> None:
        """The liveness half: an empty walk would satisfy the invariant."""
        self.assertGoesRed(
            mock.patch.object(
                test_corpus.DiagnosticsAreDistinguishable,
                "documents",
                lambda self: [],
            ),
            "DiagnosticsAreDistinguishable."
            "test_every_corpus_document_reports_distinguishable_diagnostics",
            "walk that reaches no documents",
        )


CORPUS_ACCEPT = pathlib.Path(vnx.__file__).resolve().parents[2] / "corpus" / "accept"


def _drops_data() -> Any:
    """A reader that discards every data record (SPEC 5)."""
    return mock.patch.object(decode._Reader, "_data", lambda self, obj, path: None)


def _drops_app_keys() -> Any:
    """A reader that discards every application key (SPEC 3.9)."""
    original = decode._Reader.document

    def forgetful(self: Any, value: Any) -> Any:
        got = original(self, value)
        got.app_keys = []
        return got

    return mock.patch.object(decode._Reader, "document", forgetful)


def _drops_styles() -> Any:
    """A reader that discards every style (SPEC 3.6)."""
    return mock.patch.object(decode._Reader, "_style", lambda self, obj, path: None)


def _drops_notes() -> Any:
    """A reader that discards every `notes` field (SPEC 3.2)."""
    original = decode._Reader._string

    def forgetful(self: Any, obj: Any, key: str, path: str, *args: Any) -> Any:
        if key == "notes":
            return None
        return original(self, obj, key, path, *args)

    return mock.patch.object(decode._Reader, "_string", forgetful)


_SPOILED_READERS = (
    ("discards every data record", _drops_data),
    ("discards every application key", _drops_app_keys),
    ("discards every style", _drops_styles),
    ("discards every `notes`", _drops_notes),
)


def _without_app_keys() -> Callable[[Any], Any]:
    original = vnx.encode.encode_document

    def encode(document: Any) -> Any:
        obj = original(document)
        for key in [k for k in obj if model.is_app_key(k)]:
            del obj[key]
        return obj

    return encode


if __name__ == "__main__":
    unittest.main()
