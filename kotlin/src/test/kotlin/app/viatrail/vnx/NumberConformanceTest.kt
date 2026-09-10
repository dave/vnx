package app.viatrail.vnx

import java.io.File
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import kotlin.test.fail

/**
 * The number-conformance table, `corpus/numbers/`.
 *
 * It gives BREADTH that fixtures cannot: five thousand doubles across the whole
 * binary64 range, each with its value rounded to six decimal places and to one.
 * A handful of hand-written cases can pin a rule; only a table can say the rule
 * holds everywhere.
 *
 * IT TESTS THE ROUNDING OPERATION, NOT A SPELLING. The table's `render` column
 * is not read: how a number is spelled is the writer's own business (section
 * 4.2), and comparing spellings across implementations is the rule this format
 * dropped.
 *
 * AND BY BIT PATTERN RATHER THAN BY TEXT. Comparing the rounded value as text
 * would put the rendering rule back into the comparison through the side door,
 * and would then pass or fail on how the JVM spells a double.
 */
class NumberConformanceTest {

    private val table = File(
        System.getProperty("vnx.numbers")
            ?: error("the number table's location must be passed as -Dvnx.numbers; see build.gradle.kts"),
    )

    private class Row(val line: Int, val bits: Long, val kind: String, val round6: String, val round1: String) {
        val value: Double get() = Double.fromBits(bits)
    }

    private fun rows(): List<Row> {
        assertTrue(table.isFile, "the number table is missing at ${table.absolutePath}")
        val out = ArrayList<Row>()
        for ((index, line) in table.readLines().withIndex()) {
            if (line.isBlank() || line.startsWith("#")) continue
            val f = line.split("\t")
            check(f.size == 5) { "${table.name}:${index + 1}: expected 5 tab-separated fields, got ${f.size}" }
            out.add(Row(index + 1, java.lang.Long.parseUnsignedLong(f[0], 16), f[1], f[3], f[4]))
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
        // And the tie rows must really be ties: an exact halfway value for each
        // of the two rounding columns.
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
    fun `every rounded value matches the reference, by bit pattern`() {
        val rows = rows()
        val failures = ArrayList<String>()
        for (row in rows) {
            for ((what, got, want) in listOf(
                Triple("round6", Rounding.toPlaces(row.value, 6), row.round6.toDouble()),
                Triple("round1", Rounding.toPlaces(row.value, 1), row.round1.toDouble()),
            )) {
                // Bit patterns, so the comparison is over the value and not over
                // how either side spells it. -0 and 0 are DIFFERENT bit patterns
                // and the same value; section 4.1 has rounding normalise the
                // sign, so the table and this library agree on +0 and a mismatch
                // here is real.
                if (got.toRawBits() != want.toRawBits()) {
                    failures.add("line ${row.line} [${row.kind}] $what: got $got, table says $want")
                }
            }
        }
        report("rounding", failures)
        // LIVENESS. A table that failed to load, or a loop that skipped every
        // row, is the same green as agreement on all of them.
        assertTrue(rows.size >= 1000, "only ${rows.size} rows compared; this passed vacuously")
    }

    @Test
    fun `the two traps that make a tie test lie, both directions`() {
        // Both are traps about DECIMAL INTUITION rather than about rounding.
        //
        // First: `0.0000005` is not a binary64 value. The nearest double is
        // 4.99999999999999977...e-7, which is BELOW the halfway point, so it
        // rounds down at six places under every rule and the obvious six-place
        // tie test asserts nothing at all. The smallest real six-place tie is
        // 1/128, because 5e-7 is not a dyadic rational and j/128 is the only
        // family whose exact expansion ends in a 5 at the seventh place.
        assertEquals(0.0, Rounding.toPlaces(0.0000005, 6))
        assertTrue(!isExactHalfway(0.0000005, 6), "0.0000005 is not a tie, which is the trap")
        assertTrue(isExactHalfway(0.0078125, 6), "1/128 is the smallest real six-place tie")
        assertEquals(0.007812, Rounding.toPlaces(0.0078125, 6))

        // Second, and the more dangerous direction: `3.05` correctly rounds DOWN
        // to 3, because the literal is 3.04999999999999982. A test written from
        // decimal intuition expects 3.1 and calls a correct writer broken.
        assertTrue(!isExactHalfway(3.05, 1), "3.05 is not a tie either")
        assertEquals(3.0, Rounding.toPlaces(3.05, 1))
        // Its neighbour in intuition, which IS above the halfway point.
        assertEquals(2.7, Rounding.toPlaces(2.675, 1))
        // And the real one-place tie, where the rule finally shows.
        assertTrue(isExactHalfway(340.25, 1))
        assertEquals(340.2, Rounding.toPlaces(340.25, 1))
    }

    @Test
    fun `rounding is by formatting and never by scaling`() {
        // Section 4.1 states this as a rule and gives these three values. The
        // scaled product is itself a binary64 and is not always the exact scaled
        // value, so multiplying manufactures a tie the true decimal never had and
        // destroys one it did. Measured in Go: the scaling route disagreed with
        // this table on 175 of its 5,329 rows.
        //
        // The contrast is `Math.rint`, which is the SAME TIE RULE — half to even
        // — applied to the product instead of to the value. That is what makes
        // these three rows about scaling and nothing else. `Math.round` is a
        // different rule as well as a different operation, and the test below it
        // is the one that separates that.
        for ((value, want, scaled) in listOf(
            Triple(0.15, 0.1, 0.2),
            Triple(0.05, 0.1, 0.0),
            Triple(0.45, 0.5, 0.4),
        )) {
            assertEquals(want, Rounding.toPlaces(value, 1), "rounding $value by formatting")
            assertEquals(scaled, Math.rint(value * 10) / 10.0, "the scaling route on $value, for contrast")
        }
    }

    @Test
    fun `Math dot round is half toward positive infinity, and parity decides where it differs`() {
        // Measured in Java and in JavaScript, and both give these four. Sign does
        // not decide where they part. The parity of the LOWER candidate does:
        // `Math.round(x * 10) / 10` differs from this rule exactly where the rule
        // rounds towards negative infinity.
        val roundedHere = mapOf(340.25 to 340.2, 340.75 to 340.8, -340.25 to -340.2, -340.75 to -340.8)
        val differs = setOf(340.25, -340.75)
        for ((value, expected) in roundedHere) {
            assertEquals(expected, Rounding.toPlaces(value, 1), "rounding $value")
            val viaMathRound = Math.round(value * 10) / 10.0
            assertEquals(
                value in differs, viaMathRound != expected,
                "Math.round(x*10)/10 on $value: expected it to " +
                    (if (value in differs) "differ" else "agree") + ", and it gave $viaMathRound",
            )
        }
        // The consequence that matters for a fixture: one carrying only 340.75
        // passes a Math.round implementation, and 340.25 catches it unaided. Both
        // ties are load-bearing, and neither is load-bearing because of its sign.
        assertEquals(340.8, Math.round(340.75 * 10) / 10.0, "340.75 alone cannot catch it")
        assertEquals(340.3, Math.round(340.25 * 10) / 10.0, "340.25 alone can")
    }

    @Test
    fun `rounding is symmetric about zero`() {
        // This is NOT the Math.round guard — that one is above, and 340.25 does
        // it alone. What symmetry catches is an implementation that treats the
        // sign as a special case, `if (x < 0) -round(-x) else round(x)` with a
        // helper that is not quite the same on both branches. A real failure
        // mode, and a different one from any single wrong tie rule.
        for (places in listOf(1, 6)) {
            for (magnitude in listOf(0.25, 0.75, 340.25, 340.75, 1296.75, 0.0078125, 3.0 + 1.0 / 128)) {
                val positive = Rounding.toPlaces(magnitude, places)
                val negative = Rounding.toPlaces(-magnitude, places)
                assertTrue(
                    negative == -positive,
                    "rounding must be symmetric about zero: $magnitude at $places places gave " +
                        "$positive and $negative",
                )
            }
        }
    }

    @Test
    fun `a rounded negative value that collapses to zero is written as zero`() {
        // The only way a writer meets negative zero is by rounding towards it.
        assertEquals(0.0.toRawBits(), Rounding.toPlaces(-1e-9, 6).toRawBits())
        assertEquals(0.0.toRawBits(), Rounding.toPlaces(-0.04, 1).toRawBits())
        val written = Vnx.write(Vnx.readDocument("""{"vnx":"0.1.0","waypoints":[{"at":[-1e-9,-1e-9,-0.04]}]}""").document)
        assertTrue(!written.contains("-0"), written)
    }

    private fun report(column: String, failures: List<String>) {
        if (failures.isEmpty()) return
        fail(
            "the $column columns disagree with the reference on ${failures.size} value(s):\n" +
                failures.take(15).joinToString("\n") +
                if (failures.size > 15) "\n  ... and ${failures.size - 15} more" else "",
        )
    }

    /** Whether the exact binary value sits exactly on the halfway point at [places]. */
    private fun isExactHalfway(v: Double, places: Int): Boolean {
        val scaled = java.math.BigDecimal(v).movePointRight(places)
        val fraction = scaled.subtract(java.math.BigDecimal(scaled.toBigInteger())).abs()
        return fraction.compareTo(java.math.BigDecimal("0.5")) == 0
    }
}
