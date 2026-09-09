package app.viatrail.vnx

/**
 * Pairs expected diagnostics with reported ones.
 *
 * SPEC.md section 6.3 makes emission order insignificant and requires a runner
 * to compare the two lists AS A SET. A set matcher is exactly the shape that
 * decays into "anything passes": every diagnose case in the corpus expects at
 * most one diagnostic, so a matcher that checked nothing would keep the whole
 * tier green with nothing going red. Three properties therefore have to hold,
 * and `DiagnosticMatcherTest` freezes one wrong matcher per property:
 *
 *  1. Every expectation is satisfied.
 *  2. Every reported diagnostic is accounted for — a reader that finds a defect
 *     the case did not list has found something, and that is a failure, not a
 *     bonus.
 *  3. Each expectation pairs with a DISTINCT reported diagnostic. Letting one
 *     report satisfy two expectations would pass a reader that found half the
 *     defects.
 */
object DiagnosticMatcher {

    /** Null on success; otherwise a description of how the two lists differ. */
    fun mismatch(expected: List<Sidecar.ExpectedDiagnostic>, reported: List<Diagnostic>): String? {
        if (expected.size != reported.size) {
            return "expected ${expected.size} diagnostic(s) but the reader reported ${reported.size}\n" +
                "  expected: ${expected.joinToString("; ")}\n" +
                "  reported: ${reported.joinToString("; ")}"
        }
        // `path` is matched exactly. The cause is matched by REASON CODE where
        // the sidecar carries one, and only by English substring where it does
        // not — which is the pre-migration shape and is on its way out. Matching
        // a code rather than prose is what makes "exact wording is a reader's own
        // business" true in fact rather than only in principle.
        fun satisfies(e: Sidecar.ExpectedDiagnostic, r: Diagnostic): Boolean {
            if (r.path != e.path) return false
            e.code?.let { return r.code == it }
            return r.message.contains(e.substring!!)
        }

        val taken = BooleanArray(reported.size)
        if (!assign(expected, reported, 0, taken, ::satisfies)) {
            return "no one-to-one pairing exists between the expected and reported diagnostics\n" +
                "  expected: ${expected.joinToString("; ")}\n" +
                "  reported: ${reported.joinToString("; ")}"
        }
        return null
    }

    /** Exhaustive matching. The lists are tiny, so nothing cleverer earns its keep. */
    private fun assign(
        expected: List<Sidecar.ExpectedDiagnostic>,
        reported: List<Diagnostic>,
        index: Int,
        taken: BooleanArray,
        satisfies: (Sidecar.ExpectedDiagnostic, Diagnostic) -> Boolean,
    ): Boolean {
        if (index == expected.size) return true
        for (j in reported.indices) {
            if (taken[j] || !satisfies(expected[index], reported[j])) continue
            taken[j] = true
            if (assign(expected, reported, index + 1, taken, satisfies)) return true
            taken[j] = false
        }
        return false
    }
}
