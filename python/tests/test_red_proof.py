"""Proof that the corpus runner would catch a defect.

"The corpus passes" is not evidence that the corpus discriminates.  Each test
here breaks one thing on purpose, runs the corpus check that is supposed to
notice, and asserts it goes red -- then unbreaks it.  A check that stays green
under its own defect is not a check, and this file is where that would show.

The defects are chosen to be the ones the corpus README says its fields exist to
catch, plus the ones this implementation had to decide for itself.
"""

from __future__ import annotations

import io
import unittest
from typing import Any, Callable
from unittest import mock

import vnx
from vnx import decode, geo, model, numbers
from vnx.model import Position

from . import test_corpus, test_numbers, test_reader, test_writer

_MODULES = {"reader:": test_reader, "numbers:": test_numbers,
            "writer:": test_writer}


def _run(name: str) -> unittest.TestResult:
    module = test_corpus
    for prefix, target in _MODULES.items():
        if name.startswith(prefix):
            module, name = target, name.removeprefix(prefix)
            break
    suite = unittest.TestLoader().loadTestsFromName(name, module)
    runner = unittest.TextTestRunner(stream=io.StringIO(), verbosity=0)
    return runner.run(suite)


class RedProof(unittest.TestCase):
    def assertGoesRed(self, defect: Any, check: str, because: str) -> None:
        with defect:
            result = _run(check)
        self.assertFalse(
            result.wasSuccessful(),
            f"{check} stayed green with a broken {because}; it is not checking it",
        )
        self.assertTrue(
            _run(check).wasSuccessful(), f"{check} did not come back green"
        )

    # -- the checks the corpus README names -----------------------------

    def test_transposed_lon_lat_is_caught(self) -> None:
        """`positions` is the only defence against a lon/lat transposition."""
        original = decode._Reader._position

        def transposing(self: Any, value: Any, path: str) -> Any:
            got = original(self, value, path)
            if got is None:
                return None
            channels = list(got.channels)
            channels[0], channels[1] = channels[1], channels[0]
            return Position(tuple(channels))

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "_position", transposing),
            "AcceptTier.test_accept",
            "position decoder that swaps longitude and latitude",
        )

    def test_a_vacuous_decoder_is_caught(self) -> None:
        """`counts` stops an accept file passing vacuously."""
        self.assertGoesRed(
            mock.patch.object(model.Document, "counts", lambda self: {
                key: 0 for key in test_corpus.COLLECTIONS + ("rulesets",)
            }),
            "AcceptTier.test_accept",
            "decoder that returns an empty document",
        )

    def test_an_invented_ruleset_is_caught(self) -> None:
        """`counts` completeness, ruled 2026-09-09.

        The defect this closes: a decoder that invents items in a collection no
        sidecar names.  Inventing a ruleset in every document that has none was
        invisible until `counts` had to name every collection its document
        carries -- `rulesets` was the one collection no accept sidecar pinned,
        and it is the collection whose contents nothing else inspects.
        """
        original = decode._Reader.document

        def inventive(self: Any, value: Any) -> Any:
            got = original(self, value)
            if not got.rulesets:
                got.rulesets = [{"invented": True}]
            return got

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "document", inventive),
            "AcceptTier.test_accept",
            "decoder that invents a ruleset",
        )

    def test_the_old_counts_rule_could_not_see_that(self) -> None:
        """The finding, frozen, so the weaker rule is not reintroduced.

        Under "an omitted collection is unchecked" the same defect is invisible.
        If this ever goes red, the weaker rule has come back.
        """
        original = decode._Reader.document

        def inventive(self: Any, value: Any) -> Any:
            got = original(self, value)
            if not got.rulesets:
                got.rulesets = [{"invented": True}]
            return got

        def named_keys_only(
            self: Any, expect: Any, document: Any, name: str
        ) -> None:
            counts = document.counts()
            for collection, want in (expect.get("counts") or {}).items():
                self.assertEqual(counts[collection], want)

        with mock.patch.object(decode._Reader, "document", inventive), \
                mock.patch.object(
                    test_corpus._CorpusCase, "_check_counts", named_keys_only
                ):
            self.assertTrue(_run("AcceptTier.test_accept").wasSuccessful())

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

    def test_a_tie_rounded_away_from_zero_is_caught(self) -> None:
        """The defect Kotlin shipped: a rendering tie taken away from zero.

        No corpus tier can reach this.  Every tier re-renders a document that
        already spells its numbers one way, so a renderer only ever confirms a
        spelling it was handed; a tie is only decided when a value is rendered
        from a double with no text behind it.  That is why SPEC 4.5.3 makes it a
        per-implementation test rather than a fixture.
        """
        original = numbers.render_number

        def away_from_zero(value: float) -> str:
            from decimal import Decimal

            pair = test_numbers.TiesRoundToEven._tie(float(value))
            if pair is None:
                return original(value)
            # Kotlin's shape: of the two equidistant spellings, take the one
            # further from zero.
            return max(pair, key=lambda spelling: abs(Decimal(spelling)))

        # `test_numbers` binds render_number at import, so patching only
        # `vnx.numbers` leaves the assertion calling the real one and the proof
        # reports a blind test that is not blind.  Patch the reference the
        # assertion actually reaches, and the library's, together.
        with mock.patch.object(numbers, "render_number", away_from_zero):
            self.assertGoesRed(
                mock.patch.object(test_numbers, "render_number", away_from_zero),
                "numbers:TiesRoundToEven.test_the_case_kotlin_got_wrong",
                "renderer taking a tie away from zero",
            )

    # -- the number-conformance table ------------------------------------

    def test_the_numbers_table_catches_a_half_up_rounder(self) -> None:
        """The table is an instrument, so it needs its own red proof.

        Zero disagreements across 5,315 rows means nothing unless a wrong rule
        puts disagreements back.
        """
        from decimal import Decimal, ROUND_HALF_UP

        def half_up(value: float, places: int = 0) -> float:
            return float(
                Decimal(value).quantize(Decimal(1).scaleb(-places), ROUND_HALF_UP)
            )

        self.assertGoesRed(
            mock.patch.object(vnx.writer, "round", half_up, create=True),
            "NumbersTier.test_every_row_renders_and_rounds_as_the_table_says",
            "writer rounding ties half up",
        )

    def test_the_numbers_table_catches_an_away_from_zero_renderer(self) -> None:
        from decimal import Decimal

        def away(value: float) -> str:
            pair = test_numbers.TiesRoundToEven._tie(float(value))
            if pair is None:
                return numbers.render_number(value)
            return max(pair, key=lambda spelling: abs(Decimal(spelling)))

        # The tier imports `render_number` from `vnx.numbers` inside the test,
        # so one patch reaches it. Patching `vnx.canonical` as well would leave
        # the second patch active during assertGoesRed's come-back-green run.
        self.assertGoesRed(
            mock.patch.object(numbers, "render_number", away),
            "NumbersTier.test_every_row_renders_and_rounds_as_the_table_says",
            "renderer taking a tie away from zero",
        )

    # -- the writer emit path -------------------------------------------

    def test_rounding_away_from_zero_is_caught(self) -> None:
        """The JavaScript seat's defect: `toFixed`-style away-from-zero.

        Unreachable by any corpus tier -- rounding only happens when a model is
        emitted, and every tier re-renders a document that already exists.
        """
        from decimal import Decimal, ROUND_HALF_UP

        def half_up(value: float, places: int = 0) -> float:
            return float(
                Decimal(value).quantize(Decimal(1).scaleb(-places), ROUND_HALF_UP)
            )

        self.assertGoesRed(
            mock.patch.object(vnx.writer, "round", half_up, create=True),
            "writer:RoundingTies.test_the_javascript_case",
            "writer rounding ties away from zero",
        )

    def test_a_writer_that_does_not_round_at_all_is_caught(self) -> None:
        self.assertGoesRed(
            mock.patch.object(vnx.writer, "round", lambda v, places=0: v, create=True),
            "writer:Rounding.test_places",
            "writer that does not round",
        )

    def test_a_created_negative_zero_reaching_the_bytes_is_caught(self) -> None:
        """If the renderer stopped collapsing negative zero, the writer would
        emit one that no document ever contained."""
        original = numbers.render_number

        def keep_the_sign(value: float) -> str:
            import math

            if float(value) == 0.0 and math.copysign(1.0, float(value)) < 0:
                return "-0"
            return original(value)

        self.assertGoesRed(
            mock.patch.object(vnx.canonical, "render_number", keep_the_sign),
            "writer:RoundingCreatesNegativeZero"
            ".test_a_created_negative_zero_never_reaches_the_bytes",
            "renderer that keeps a created negative zero",
        )

    def test_shortening_in_the_FORMATTER_is_caught(self) -> None:
        """The two emit paths must not converge.

        SPEC 4.1's shortest-spelling rule belongs to the writer; a formatter
        that also shortened would fail the accept tier, because
        `absent-elevation-and-time.vnx` is stored canonical with trailing nulls.
        """
        self.assertGoesRed(
            mock.patch.object(
                vnx.encode, "_positions",
                lambda items: None if items is None
                else [list(vnx.writer.round_position(p).channels) for p in items],
            ),
            "AcceptTier.test_accept_files_survive_the_model",
            "formatter that applies the writer's rules",
        )

    # -- canonical form -------------------------------------------------

    def test_python_number_rendering_is_caught(self) -> None:
        """Python's own rendering disagrees on both exponent boundaries."""
        self.assertGoesRed(
            mock.patch.object(vnx.canonical, "render_number", lambda v: repr(float(v))),
            "FormatTier.test_formatter_emits_the_canonical_bytes",
            "number renderer using Python's repr",
        )

    def test_negative_zero_is_caught(self) -> None:
        """SPEC 4.5.3: negative zero MUST render as `0`."""
        original = numbers.render_number

        def signed_zero(value: float) -> str:
            if float(value) == 0.0 and str(float(value))[0] == "-":
                return "-0"
            return original(value)

        self.assertGoesRed(
            mock.patch.object(vnx.canonical, "render_number", signed_zero),
            "FormatTier.test_formatter_emits_the_canonical_bytes",
            "renderer that keeps the sign of negative zero",
        )

    def test_plain_lexicographic_key_order_is_caught(self) -> None:
        """Scalars sort before containers; `sort_keys` puts `networks` first."""
        original = vnx.canonical.is_scalar
        self.assertGoesRed(
            mock.patch.object(vnx.canonical, "is_scalar", lambda value: True),
            "AcceptTier.test_accept_files_are_canonical",
            "serialiser using plain lexicographic key order",
        )
        self.assertIs(vnx.canonical.is_scalar, original)

    def test_multi_line_positions_are_caught(self) -> None:
        """SPEC 4.5.2: a number-and-null array is written on one line."""
        self.assertGoesRed(
            mock.patch.object(vnx.canonical, "_is_number_or_null", lambda value: False),
            "AcceptTier.test_accept_files_are_canonical",
            "serialiser that spreads positions over several lines",
        )

    def test_a_normalised_position_arity_is_caught(self) -> None:
        """A trailing `null` is not noise: dropping it changes the bytes."""
        original = decode._Reader._position

        def shortening(self: Any, value: Any, path: str) -> Any:
            got = original(self, value, path)
            if got is None:
                return None
            channels = list(got.channels)
            while len(channels) > 2 and channels[-1] is None:
                channels.pop()
            return Position(tuple(channels))

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "_position", shortening),
            "AcceptTier.test_accept_files_survive_the_model",
            "model that shortens a position on the way in",
        )

    def test_a_dropped_ruleset_is_caught(self) -> None:
        """SPEC 3.9: a ruleset is opaque and MUST survive read-modify-write."""
        self.assertGoesRed(
            mock.patch.object(vnx.encode, "encode_document", _without("rulesets")),
            "AcceptTier.test_accept_files_survive_the_model",
            "encoder that drops rulesets",
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
            "AcceptTier.test_accept_files_survive_the_model",
            "encoder that keeps a record's type but not its body",
        )

    def test_only_the_model_check_defends_a_data_record(self) -> None:
        """The two readings of SPEC 8.1's canonical check are not equivalent.

        SPEC 8.1 defines the accept-tier check as "canonicalising it reproduces
        it byte for byte", and `corpus/README.md` says canonicalise() is "bytes
        in and bytes out".  Taken literally that check never builds a model, so
        it cannot see a reader that discards what it read -- yet SPEC 8.1 also
        claims the check "proves that the whole document -- including data
        records of types no implementation recognises, whose bodies nothing
        else inspects -- survives parse and re-render at value level".

        It does not.  With every data record discarded on the way in, the
        literal check stays green and only the decode-and-re-encode check goes
        red.  Frozen here so that if SPEC 8.1 is corrected to require the model
        round-trip, this test goes red and stops describing a hole that is
        closed.
        """
        defect = mock.patch.object(decode._Reader, "_data", lambda self, o, p: None)
        with defect:
            self.assertTrue(
                _run("AcceptTier.test_accept_files_are_canonical").wasSuccessful(),
                "the literal SPEC 8.1 check has gained reach; revisit the note above",
            )
            self.assertFalse(
                _run("AcceptTier.test_accept_files_survive_the_model").wasSuccessful()
            )

    # -- refusals -------------------------------------------------------

    def test_a_reader_that_never_refuses_is_caught(self) -> None:
        self.assertGoesRed(
            mock.patch.object(decode._Reader, "_check_version", lambda self, v: None),
            "RejectTier.test_reject",
            "reader that never refuses a version",
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
            mock.patch.object(decode._Reader, "note", lambda self, path, message: None),
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
                decode._Reader, "note",
                lambda self, path, message, code=None: original(self, path, message),
            ),
            "DiagnoseTier.test_diagnose",
            "reader that reports no diagnostic codes",
        )

    def test_a_reader_that_emits_the_wrong_code_is_caught(self) -> None:
        original = decode._Reader.note

        self.assertGoesRed(
            mock.patch.object(
                decode._Reader, "note",
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
            decode._Reader, "note",
            lambda self, path, message, code=None: original(
                self, path, "xyzzy plugh", code
            ),
        ):
            self.assertTrue(
                _run("DiagnoseTier.test_diagnose").wasSuccessful(),
                "a diagnostic's English is being compared; it must not be",
            )

    def test_a_reader_with_no_continuity_check_is_caught(self) -> None:
        self.assertGoesRed(
            mock.patch.object(
                decode._Reader, "_check_continuity", lambda self, kept, path, net: None
            ),
            "DiagnoseTier.test_diagnose",
            "reader with no chain continuity check",
        )

    def test_a_reader_that_keeps_the_outer_ring_is_caught(self) -> None:
        """SPEC 3.7: drop the item, do not quietly keep ring 1."""
        original = decode._Reader._polygon

        def outer_ring(self: Any, value: Any, path: str) -> Any:
            if isinstance(value, list) and value and isinstance(value[0], list) \
                    and value[0] and isinstance(value[0][0], list):
                return original(self, value[0], path)
            return original(self, value, path)

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "_polygon", outer_ring),
            "DiagnoseTier.test_diagnose",
            "reader that flattens a nested polygon to its outer ring",
        )

    def test_a_second_diagnostic_from_one_defect_is_caught(self) -> None:
        """Dropping a dangling link must not then also report a broken chain.

        Red-proofed against a unit test, not against the corpus: the corpus
        case for this, `diagnose/link-names-absent-arc.vnx`, carries a two-link
        chain, so after the drop no reader has a consecutive pair left to
        misjudge.  The defect below leaves that case green.
        """
        original = decode._Reader._check_continuity

        def across_the_hole(self: Any, kept: Any, path: str, network: Any) -> None:
            renumbered = [(index, link) for index, (_, link) in enumerate(kept)]
            original(self, renumbered, path, network)

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "_check_continuity", across_the_hole),
            "reader:Chains.test_one_dropped_link_reports_one_diagnostic",
            "reader that checks continuity across a dropped link",
        )

    def test_the_corpus_alone_cannot_see_that_defect(self) -> None:
        """The finding above, frozen so it is not rediscovered.

        `diagnose/link-names-absent-arc.expect.json` says its point is that
        "dropping the second link must not then also report the chain as
        broken".  Its fixture cannot show that.  If the fixture ever gains a
        third link this goes red and the note stops overstating the case.
        """
        original = decode._Reader._check_continuity

        def across_the_hole(self: Any, kept: Any, path: str, network: Any) -> None:
            renumbered = [(index, link) for index, (_, link) in enumerate(kept)]
            original(self, renumbered, path, network)

        with mock.patch.object(
            decode._Reader, "_check_continuity", across_the_hole
        ):
            self.assertFalse(
                _run("DiagnoseTier.test_diagnose").wasSuccessful(),
                "the corpus stopped seeing this; has the chain been simplified?",
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

    def test_a_container_that_lists_its_document_as_an_attachment_is_caught(self) -> None:
        original = vnx.container.read_container_bytes

        def sloppy(data: bytes) -> Any:
            got = original(data)
            got.attachments = got.attachments + [got.document_name]
            return got

        self.assertGoesRed(
            mock.patch.object(vnx, "read_container_bytes", sloppy),
            "AcceptTier.test_accept",
            "container reader that counts the document as an attachment",
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


    # -- rules no corpus case reaches ------------------------------------

    def test_a_reader_that_skips_the_terminal_pip_check_is_caught(self) -> None:
        """SPEC 3.3.2 is a MUST that no fixture exercises.

        Which makes these unit tests the only guard, so they are the ones that
        have to be red-proofed: a rule with no fixture and no red proof is a
        rule nothing is holding.
        """
        self.assertGoesRed(
            mock.patch.object(
                decode._Reader, "_check_terminal_pips",
                lambda self, arc, path, nodes: None,
            ),
            "reader:TerminalPips.test_a_node_far_from_its_terminal_pip_is_diagnosed",
            "reader that never compares a terminal pip against its node",
        )

    def test_a_terminal_pip_check_that_always_fires_is_caught(self) -> None:
        """The other half: a check that reports everything guards nothing."""
        self.assertGoesRed(
            mock.patch.object(
                vnx.decode, "PROJECTION_TOLERANCE_M", 0.0
            ),
            "reader:TerminalPips.test_a_node_within_tolerance_is_silent",
            "tolerance of zero metres",
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
        """SPEC 6.3's invariant, red-proofed by restoring the arc-level path.

        `diagnose/terminal-pip-off-node.vnx` has BOTH terminals of one arc
        drifted, so a reader reporting at the arc rather than at the end emits
        two diagnostics with one path and one code -- and the set matcher would
        pass a reader that found one of the two faults twice.
        """
        def arc_level(
            self: Any, arc: Any, path: str, nodes: Any
        ) -> None:
            pips = arc.pips
            if not pips:
                return
            for node_key, pip in ((arc.from_, pips[0]), (arc.to, pips[-1])):
                if node_key is None:
                    continue
                node = nodes.get(node_key)
                if node is None:
                    continue
                far = geo.distance_between(
                    (pip.lon, pip.lat), (node.lon, node.lat)
                )
                if far > decode.PROJECTION_TOLERANCE_M:
                    self.note(path, "drifted", "terminal-pip-off-node")

        self.assertGoesRed(
            mock.patch.object(decode._Reader, "_check_terminal_pips", arc_level),
            "DiagnosticsAreDistinguishable."
            "test_every_corpus_document_reports_distinguishable_diagnostics",
            "reader reporting a drifted pip at the arc instead of at the end",
        )

    def test_a_walk_that_finds_nothing_is_caught(self) -> None:
        """The liveness half: an empty walk would satisfy the invariant."""
        self.assertGoesRed(
            mock.patch.object(
                test_corpus.DiagnosticsAreDistinguishable, "documents",
                lambda self: [],
            ),
            "DiagnosticsAreDistinguishable."
            "test_every_corpus_document_reports_distinguishable_diagnostics",
            "walk that reaches no documents",
        )


def _without(key: str) -> Callable[[Any], Any]:
    original = vnx.encode.encode_document

    def encode(document: Any) -> Any:
        obj = original(document)
        obj.pop(key, None)
        return obj

    return encode


if __name__ == "__main__":
    unittest.main()
