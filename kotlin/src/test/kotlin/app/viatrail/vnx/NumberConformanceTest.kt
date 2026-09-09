package app.viatrail.vnx

import java.io.File
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import kotlin.test.fail

/**
 * The number-conformance table: what every implementation must emit for a given
 * double, generated from the reference SPEC.md section 4.5.3 names.
 *
 * This is a fifth conformance tier, for the rules the corpus does not reach.
 *
 * The ROUNDING tie is the one it cannot reach even in principle. Rounding
 * happens only when a WRITER emits a model, and all four corpus tiers RE-RENDER
 * an existing document — which section 8.1 requires precisely because a writer
 * rounds. The corpus's reach ends at the writer boundary, and an implementation
 * can round every coordinate in the format the wrong way and pass every tier
 * green. Measured: reverting the rounding tie rule reddens the two round columns
 * here and nothing else in this suite.
 *
 * The RENDERING tie is NOT out of reach, and I argued that it was. My reasoning
 * was that a tie is a value rather than a shape, so no fixture could carry one —
 * and the spec seat then hand-wrote `format/number-tie`, whose input spells the
 * value one way and whose expected output spells it the other. Falsified, and
 * the rule that replaced mine is better because it says why: a value-level
 * property is reachable by a re-render whenever the two readings produce
 * different TEXT for the same value. Rounding is not, because a re-render never
 * rounds; the tie is, because a re-render always re-spells.
 *
 * `the formatter tier reaches the rendering tie` below is what keeps that
 * correction honest. An argument whose conclusion is an ABSENCE goes stale in
 * silence when the thing it claims is absent from grows, so it is asserted as a
 * presence instead.
 *
 * What is left to this table beyond the rounding tie: the shortest-spelling rule
 * across the whole double range, the exponent boundaries, and a negative zero
 * that a writer CREATES by rounding towards it, which no fixture can hand it.
 */
class NumberConformanceTest {

    private val table = File(
        System.getProperty("vnx.numbers")
            ?: error("the number table's location must be passed as -Dvnx.numbers; see build.gradle.kts"),
    )

    private class Row(val line: Int, val bits: Long, val kind: String, val render: String, val round6: String, val round1: String) {
        val value: Double get() = Double.fromBits(bits)
    }

    private fun rows(): List<Row> {
        assertTrue(table.isFile, "the number table is missing at ${table.absolutePath}")
        val out = ArrayList<Row>()
        for ((index, line) in table.readLines().withIndex()) {
            if (line.isBlank() || line.startsWith("#")) continue
            val f = line.split("\t")
            check(f.size == 5) { "${table.name}:${index + 1}: expected 5 tab-separated fields, got ${f.size}" }
            out.add(Row(index + 1, java.lang.Long.parseUnsignedLong(f[0], 16), f[1], f[2], f[3], f[4]))
        }
        return out
    }

    @Test
    fun `the table is present, complete, and discriminating`() {
        val rows = rows()
        assertTrue(rows.size > 2000, "the table has shrunk to ${rows.size} rows")
        // The categories that do the discriminating. If one is gone the tier
        // still passes, which is the failure that would not announce itself.
        for (kind in listOf("named", "boundary", "negative-zero", "render-tie", "round1-tie", "round6-tie", "uniform", "in-range")) {
            assertTrue(rows.any { it.kind == kind }, "the table carries no `$kind` rows any more")
        }
        // And the tie rows must really be ties: a second, equally short spelling
        // that also round-trips for render, and an exact halfway value for the
        // two rounding columns.
        assertTrue(
            rows.filter { it.kind == "render-tie" }.all { tieRuleIsVisible(it.value) },
            "a `render-tie` row does not discriminate the tie rule, so the render column proves nothing",
        )
        assertTrue(
            rows.filter { it.kind == "round1-tie" }.all { isExactHalfway(it.value, 1) },
            "a `round1-tie` row is not an exact halfway value",
        )
        assertTrue(
            rows.filter { it.kind == "round6-tie" }.all { isExactHalfway(it.value, 6) },
            "a `round6-tie` row is not an exact halfway value",
        )
    }

    @Test
    fun `every canonical rendering matches the reference`() {
        val failures = ArrayList<String>()
        for (row in rows()) {
            val mine = EcmaNumber.render(row.value)
            if (mine != row.render) failures.add("line ${row.line} [${row.kind}]: expected ${row.render}, rendered $mine")
        }
        report("render", failures)
    }

    @Test
    fun `every coordinate rounded to six places matches the reference`() {
        val failures = ArrayList<String>()
        for (row in rows()) {
            val mine = EcmaNumber.render(Rounding.toPlaces(row.value, 6))
            if (mine != row.round6) failures.add("line ${row.line} [${row.kind}]: expected ${row.round6}, got $mine")
        }
        report("round6", failures)
    }

    @Test
    fun `every elevation rounded to one place matches the reference`() {
        val failures = ArrayList<String>()
        for (row in rows()) {
            val mine = EcmaNumber.render(Rounding.toPlaces(row.value, 1))
            if (mine != row.round1) failures.add("line ${row.line} [${row.kind}]: expected ${row.round1}, got $mine")
        }
        report("round1", failures)
    }

    @Test
    fun `the formatter tier reaches the rendering tie, which I said it could not`() {
        // Re-pointed from an absence to a presence. If `format/number-tie` is ever
        // removed, this goes red and the claim gets revisited, rather than the
        // suite quietly going on asserting something that stopped being true.
        val format = File(System.getProperty("vnx.corpus")!!, "format")
        val discriminating = format.listFiles()!!
            .filter { it.name.endsWith(".in.vnx") }
            .filter { input -> numbersIn(input.readText()).any { tieRuleIsVisible(it) } }
        assertTrue(
            discriminating.isNotEmpty(),
            "no format case carries a number whose tie rule is visible, so the formatter tier no longer " +
                "reaches the rendering tie — which it did on 2026-09-09 via format/number-tie",
        )
        // And the case must actually distinguish the two spellings in its BYTES,
        // or it would pass for a formatter that echoed its input.
        for (input in discriminating) {
            val canonical = File(format, input.name.removeSuffix(".in.vnx") + ".canonical.vnx")
            assertTrue(
                input.readText() != canonical.readText(),
                "${input.name} is already canonical, so it cannot decide anything",
            )
        }
    }

    /** Every JSON number literal in a document, as doubles. */
    private fun numbersIn(text: String): List<Double> =
        Regex("-?\\d+(?:\\.\\d+)?(?:[eE][-+]?\\d+)?").findAll(text)
            .mapNotNull { it.value.toDoubleOrNull() }
            .filter { it.isFinite() }
            .toList()

    @Test
    fun `the two traps that make a tie test lie, both directions`() {
        // From the Python seat, 2026-09-09, and both are traps about DECIMAL
        // INTUITION rather than about rounding.
        //
        // First: `0.0000005` is not a binary64 value. The nearest double is
        // 4.99999999999999977...e-7, which is BELOW the halfway point, so it
        // rounds down at six places under every rule and the obvious six-place
        // tie test asserts nothing at all. The smallest real six-place tie is
        // 1/128, because 5e-7 is not a dyadic rational and j/128 is the only
        // family whose exact expansion ends in a 5 at the seventh place.
        assertEquals("0", EcmaNumber.render(Rounding.toPlaces(0.0000005, 6)))
        assertTrue(!isExactHalfway(0.0000005, 6), "0.0000005 is not a tie, which is the trap")
        assertTrue(isExactHalfway(0.0078125, 6), "1/128 is the smallest real six-place tie")
        assertEquals("0.007812", EcmaNumber.render(Rounding.toPlaces(0.0078125, 6)))

        // Second, and the more dangerous direction: `3.05` correctly rounds DOWN
        // to 3, because the literal is 3.04999999999999982. A test written from
        // decimal intuition expects 3.1 and calls a correct writer broken.
        assertTrue(!isExactHalfway(3.05, 1), "3.05 is not a tie either")
        assertEquals("3", EcmaNumber.render(Rounding.toPlaces(3.05, 1)))
        // Its neighbour in intuition, which IS above the halfway point.
        assertEquals("2.7", EcmaNumber.render(Rounding.toPlaces(2.675, 1)))
        // And the real one-place tie, where the rule finally shows.
        assertTrue(isExactHalfway(340.25, 1))
        assertEquals("340.2", EcmaNumber.render(Rounding.toPlaces(340.25, 1)))
    }

    @Test
    fun `the rounding guard covers negatives, because one wrong rule agrees on half of them`() {
        // From the JavaScript seat, refined by measurement here. `toFixed` is
        // symmetric — it strips the sign first, so it rounds away from zero on
        // both — but `Math.round` is not, in JavaScript and in Java identically:
        // it rounds a tie towards POSITIVE INFINITY, so `Math.round(x * 10) / 10`
        // gives 340.3 for 340.25 and -340.2 for -340.25. That AGREES with the
        // correct rule on every negative tie and disagrees on every positive one,
        // so a guard testing only positives passes on half the failures.
        //
        // `Math.rint` is the other near-miss: it is half-to-even, but applied to
        // the scaled value, and scaling by a power of ten is not exact.
        for (places in listOf(1, 6)) {
            for (magnitude in listOf(0.25, 340.25, 1296.75, 0.0078125, 3.0 + 1.0 / 128)) {
                val positive = EcmaNumber.render(Rounding.toPlaces(magnitude, places))
                val negative = EcmaNumber.render(Rounding.toPlaces(-magnitude, places))
                assertEquals(
                    if (positive == "0") "0" else "-$positive", negative,
                    "rounding must be symmetric about zero: $magnitude at $places places",
                )
            }
        }
        assertEquals("-340.2", EcmaNumber.render(Rounding.toPlaces(-340.25, 1)))
        assertEquals("-0.007812", EcmaNumber.render(Rounding.toPlaces(-0.0078125, 6)))
    }

    @Test
    fun `a rounded negative value that collapses to zero renders as zero`() {
        // The only way a writer meets negative zero is by rounding towards it,
        // which is a path no fixture takes — the corpus's format tier can only
        // hand a formatter a `-0` that was already written down.
        assertEquals("0", EcmaNumber.render(Rounding.toPlaces(-1e-9, 6)))
        assertEquals("0", EcmaNumber.render(Rounding.toPlaces(-0.04, 1)))
        assertTrue(!Vnx.write(Vnx.readDocument("""{"vnx":"0.1.0","waypoints":[{"at":[-1e-9,-1e-9,-0.04]}]}""").document).contains("-0"))
    }

    private fun report(column: String, failures: List<String>) {
        if (failures.isEmpty()) return
        fail(
            "the $column column disagrees with the reference on ${failures.size} value(s):\n" +
                failures.take(15).joinToString("\n") +
                if (failures.size > 15) "\n  ... and ${failures.size - 15} more" else "",
        )
    }

    /**
     * Whether the choice of tie rule is VISIBLE for this value: whether the
     * shortest round-tripping decimal differs between ties-to-even and
     * ties-away-from-zero.
     *
     * Computed here independently of [EcmaNumber], because a row that cannot
     * discriminate is worse than no row. The generator's first version asked the
     * weaker question "does a second, equally short spelling round-trip" — true
     * whenever the rounding interval spans two candidates, not only at a tie —
     * and produced 60 rows that all stayed green when the tie rule was reverted.
     */
    private fun tieRuleIsVisible(v: Double): Boolean =
        shortestDigits(v, java.math.RoundingMode.HALF_EVEN) != shortestDigits(v, java.math.RoundingMode.HALF_UP)

    private fun shortestDigits(v: Double, mode: java.math.RoundingMode): String {
        val exact = java.math.BigDecimal(Math.abs(v))
        for (p in 1..17) {
            val candidate = exact.round(java.math.MathContext(p, mode))
            if (candidate.toDouble() == Math.abs(v)) return candidate.stripTrailingZeros().toString()
        }
        return "?"
    }

    /** Whether the exact binary value sits exactly on the halfway point at [places]. */
    private fun isExactHalfway(v: Double, places: Int): Boolean {
        val scaled = java.math.BigDecimal(v).movePointRight(places)
        val fraction = scaled.subtract(java.math.BigDecimal(scaled.toBigInteger())).abs()
        return fraction.compareTo(java.math.BigDecimal("0.5")) == 0
    }
}
