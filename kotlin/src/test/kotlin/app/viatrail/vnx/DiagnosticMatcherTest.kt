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
 */
class DiagnosticMatcherTest {

    private fun expect(vararg pairs: Pair<String, String>) =
        pairs.map { Sidecar.ExpectedDiagnostic(it.first, null, it.second) }

    private fun report(vararg pairs: Pair<String, String>) =
        pairs.map { Diagnostic(it.first, it.second, DiagnosticCause.UNKNOWN_KEY) }

    private fun expectCode(vararg pairs: Pair<String, String>) =
        pairs.map { Sidecar.ExpectedDiagnostic(it.first, it.second, null) }

    private fun reportCause(vararg pairs: Pair<String, DiagnosticCause>) =
        pairs.map { Diagnostic(it.first, "whatever this reader happens to say", it.second) }

    @Test
    fun `a correct pairing passes, in either emission order`() {
        // Section 6.3: a reader that resolves cross-references in a second pass
        // emits its link diagnostics after its key diagnostics, and is correct.
        val expected = expect("/a" to "unknown key", "/b" to "not in this network")
        assertNull(DiagnosticMatcher.mismatch(expected, report("/a" to "unknown key `x`", "/b" to "not in this network")))
        assertNull(DiagnosticMatcher.mismatch(expected, report("/b" to "not in this network", "/a" to "unknown key `x`")))
    }

    @Test
    fun `one report may not satisfy two expectations`() {
        // The failure this guards: a reader that found half the defects passing
        // because both expectations matched the same reported diagnostic.
        val expected = expect("/a" to "unknown key", "/a" to "unknown key")
        assertNotNull(
            DiagnosticMatcher.mismatch(expected, report("/a" to "unknown key `x`")),
            "two expectations were satisfied by one report",
        )
        assertNull(DiagnosticMatcher.mismatch(expected, report("/a" to "unknown key `x`", "/a" to "unknown key `y`")))
    }

    @Test
    fun `a reported diagnostic the case did not list is a failure`() {
        // Finding a defect the case did not name is not a bonus. The corpus for
        // link-names-absent-arc says so in as many words: dropping a link must not
        // then also report the chain as broken.
        assertNotNull(
            DiagnosticMatcher.mismatch(
                expect("/a" to "unknown key"),
                report("/a" to "unknown key `x`", "/b" to "not one connected walk"),
            ),
        )
    }

    @Test
    fun `an expectation with no report is a failure`() {
        assertNotNull(DiagnosticMatcher.mismatch(expect("/a" to "unknown key"), report()))
    }

    @Test
    fun `the path is compared exactly and the message as a substring`() {
        assertNotNull(
            DiagnosticMatcher.mismatch(expect("/a" to "unknown key"), report("/a/b" to "unknown key `x`")),
            "a diagnostic at the wrong path must not satisfy an expectation",
        )
        assertNotNull(
            DiagnosticMatcher.mismatch(expect("/a" to "unknown key"), report("/a" to "something else entirely")),
            "a diagnostic that does not name the cause must not satisfy an expectation",
        )
        assertNull(
            DiagnosticMatcher.mismatch(
                expect("/a" to "unknown key"),
                report("/a" to "unknown key `nmae`, skipped"),
            ),
            "exact wording is a reader's own business",
        )
    }

    @Test
    fun `a reason code pairs regardless of what the reader says in English`() {
        // The whole point of the migration: a conforming reader may word its
        // diagnostic any way it likes, and must still pair.
        assertNull(
            DiagnosticMatcher.mismatch(
                expectCode("/waypoints/0/nmae" to "unknown-key"),
                reportCause("/waypoints/0/nmae" to DiagnosticCause.UNKNOWN_KEY),
            ),
        )
        // And a reader reporting the WRONG cause at the right path must not pair,
        // which is the property English substrings were bad at.
        assertNotNull(
            DiagnosticMatcher.mismatch(
                expectCode("/waypoints/0/nmae" to "unknown-key"),
                reportCause("/waypoints/0/nmae" to DiagnosticCause.WRONG_TYPE),
            ),
        )
    }

    @Test
    fun `a cause with no reason code can never satisfy a coded expectation`() {
        // A reader whose cause has no code yet must fail loudly rather than pair
        // on a null equal to a null.
        assertNotNull(
            DiagnosticMatcher.mismatch(
                expectCode("/a" to "unknown-key"),
                reportCause("/a" to DiagnosticCause.FOLDER_ABSENT),
            ),
        )
    }

    @Test
    fun `a greedy pairing that would fail is still found`() {
        // The first expectation matches both reports; the second matches only one.
        // A matcher that took the first match and moved on would fail this.
        val expected = expect("/a" to "", "/a" to "specific")
        assertNull(DiagnosticMatcher.mismatch(expected, report("/a" to "general", "/a" to "specific")))
    }
}
