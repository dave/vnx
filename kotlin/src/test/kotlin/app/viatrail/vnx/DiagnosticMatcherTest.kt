package app.viatrail.vnx

import kotlin.test.Test
import kotlin.test.assertNotNull
import kotlin.test.assertNull

/**
 * Freezes one wrong matcher per way a set comparison can stop discriminating.
 *
 * Every diagnose case in the corpus expects at most one diagnostic, so a matcher
 * that checked nothing would keep the whole tier green. The matcher is therefore
 * the one part of the runner that cannot be trusted to fail on its own, and
 * these are the cases that prove it still can.
 *
 * Since 2026-09-09 the pairing is on `path` plus reason `code`, and no English
 * is read at all.
 */
class DiagnosticMatcherTest {

    private fun expect(vararg pairs: Pair<String, String>) =
        pairs.map { Sidecar.ExpectedDiagnostic(it.first, it.second) }

    /** The message is deliberately unhelpful: nothing may depend on it. */
    private fun report(vararg pairs: Pair<String, DiagnosticCause>) =
        pairs.map { Diagnostic(it.first, "whatever this reader happens to say", it.second) }

    @Test
    fun `a correct pairing passes, in either emission order`() {
        // Section 6.3: a reader that resolves cross-references in a second pass
        // emits its link diagnostics after its key diagnostics, and is correct.
        val expected = expect("/a" to "unknown-key", "/b" to "link-arc-absent")
        assertNull(
            DiagnosticMatcher.mismatch(
                expected,
                report("/a" to DiagnosticCause.UNKNOWN_KEY, "/b" to DiagnosticCause.LINK_ARC_ABSENT),
            ),
        )
        assertNull(
            DiagnosticMatcher.mismatch(
                expected,
                report("/b" to DiagnosticCause.LINK_ARC_ABSENT, "/a" to DiagnosticCause.UNKNOWN_KEY),
            ),
        )
    }

    @Test
    fun `the English a reader chooses is never read`() {
        // The whole point of the migration. Two readers wording the same finding
        // completely differently must both pass, and the fixture's own
        // `message_advisory` must not creep back into the comparison.
        assertNull(
            DiagnosticMatcher.mismatch(
                expect("/waypoints/0/nmae" to "unknown-key"),
                listOf(Diagnostic("/waypoints/0/nmae", "clé inconnue", DiagnosticCause.UNKNOWN_KEY)),
            ),
        )
    }

    @Test
    fun `the wrong cause at the right path is a failure`() {
        // This is the hole English substrings could not see: a reader that
        // reports the right path with plausible prose and the wrong finding.
        assertNotNull(
            DiagnosticMatcher.mismatch(
                expect("/waypoints/0/nmae" to "unknown-key"),
                report("/waypoints/0/nmae" to DiagnosticCause.WRONG_TYPE),
            ),
        )
    }

    @Test
    fun `a cause carrying no code can never satisfy a coded expectation`() {
        // Section 6.3 lets a reader leave `code` absent where no case pins one.
        // A matcher comparing null to null would then pass everything.
        assertNotNull(
            DiagnosticMatcher.mismatch(
                expect("/a" to "unknown-key"),
                report("/a" to DiagnosticCause.FOLDER_ABSENT),
            ),
        )
    }

    @Test
    fun `one report may not satisfy two expectations`() {
        // The failure this guards: a reader that found half the defects passing
        // because both expectations matched the same reported diagnostic.
        val expected = expect("/a" to "unknown-key", "/a" to "unknown-key")
        assertNotNull(
            DiagnosticMatcher.mismatch(expected, report("/a" to DiagnosticCause.UNKNOWN_KEY)),
            "two expectations were satisfied by one report",
        )
        assertNull(
            DiagnosticMatcher.mismatch(
                expected,
                report("/a" to DiagnosticCause.UNKNOWN_KEY, "/a" to DiagnosticCause.UNKNOWN_KEY),
            ),
        )
    }

    @Test
    fun `a reported diagnostic the case did not list is a failure`() {
        // Finding a defect the case did not name is not a bonus. The corpus for
        // link-names-absent-arc says so in as many words: dropping a link must not
        // then also report the chain as broken.
        assertNotNull(
            DiagnosticMatcher.mismatch(
                expect("/a" to "unknown-key"),
                report("/a" to DiagnosticCause.UNKNOWN_KEY, "/b" to DiagnosticCause.CHAIN_NOT_CONNECTED),
            ),
        )
    }

    @Test
    fun `an expectation with no report is a failure`() {
        assertNotNull(DiagnosticMatcher.mismatch(expect("/a" to "unknown-key"), emptyList()))
    }

    @Test
    fun `the path is compared exactly`() {
        assertNotNull(
            DiagnosticMatcher.mismatch(
                expect("/a" to "unknown-key"),
                report("/a/b" to DiagnosticCause.UNKNOWN_KEY),
            ),
            "a diagnostic at the wrong path must not satisfy an expectation",
        )
    }

    @Test
    fun `a greedy pairing that would fail is still found`() {
        // The first report matches both expectations; the second matches only one.
        // A matcher that took the first match and moved on would fail this.
        val expected = expect("/a" to "unknown-key", "/a" to "link-arc-absent")
        assertNull(
            DiagnosticMatcher.mismatch(
                expected,
                report("/a" to DiagnosticCause.LINK_ARC_ABSENT, "/a" to DiagnosticCause.UNKNOWN_KEY),
            ),
        )
    }
}
