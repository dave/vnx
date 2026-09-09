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
 * This is a fifth conformance tier, and it exists because the corpus cannot
 * reach either of the two rules that decide these bytes.
 *
 * A RENDERING tie is a value, not a shape: nobody writes a fixture containing a
 * double whose shortest decimal is exactly halfway between two equal-length
 * candidates, and both spellings parse to the same double, so such a fixture
 * would pass either way at value level.
 *
 * A ROUNDING tie is worse. Rounding happens only when a WRITER emits a model,
 * and all four corpus tiers RE-RENDER an existing document — which section 8.1
 * requires precisely because a writer rounds. The corpus's reach ends at the
 * writer boundary, and an implementation can round every coordinate in the
 * format the wrong way and pass every tier green.
 *
 * Rendering and rounding are separate code paths here, and this test covers both
 * separately. Red-proof it by changing either tie rule to half-away-from-zero;
 * each column must go red on its own.
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
