package app.viatrail.vnx

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotEquals
import kotlin.test.assertTrue

/**
 * SPEC.md section 4.5.3 and its table in section 4.5.6.
 *
 * The corpus reaches these through one fixture. This freezes them directly, so a
 * defect in the renderer names itself instead of arriving as a byte diff in a
 * file about something else.
 */
class EcmaNumberTest {

    private fun render(x: Double) = EcmaNumber.render(x)

    @Test
    fun `the four cases section 4-5-3 names`() {
        assertEquals("340", render(340.0))
        assertEquals("0.000001", render(0.000001))
        assertEquals("1e-7", render(1e-7))
        assertEquals("1778914991000", render(1778914991000.0))
    }

    @Test
    fun `negative zero collapses to zero`() {
        // Two writers disagreeing about the sign is a byte incompatibility
        // carrying no information.
        assertEquals("0", render(-0.0))
        assertEquals("0", render(0.0))
    }

    @Test
    fun `the exponent boundaries in both directions`() {
        // Section 4.5.3 fixes the boundaries at n <= 21 and n > -6, and those are
        // where every language's default disagrees.
        assertEquals("1e+21", render(1e21))
        assertEquals("100000000000000000000", render(1e20))
        assertEquals("0.000001", render(1e-6))
        assertEquals("1e-7", render(1e-7))
        assertEquals("1.5e-7", render(1.5e-7))
        assertEquals("1.2345e+21", render(1.2345e21))
    }

    @Test
    fun `the shortest spelling that round-trips is the one emitted`() {
        // -3.6712345678901234 and -3.6712345678901235 are the same binary64, and
        // canonical form emits the shorter of the two. `format/messy-spellings`
        // is the corpus case; this says what it is testing.
        assertEquals(
            "-3.6712345678901235".toDouble(), "-3.6712345678901234".toDouble(),
            "the fixture depends on these being one double",
        )
        assertEquals("-3.6712345678901235", render("-3.6712345678901234".toDouble()))
        assertEquals("57.11812345678901", render(57.11812345678901))
    }

    @Test
    fun `every rendered number parses back to the value it came from`() {
        val values = doubleArrayOf(
            0.0, 1.0, -1.0, 340.0, 0.1, 1e-7, 1e-6, 1e21, 1e20, 1e-320,
            -3.6712345678901235, 57.118, 1778914991000.0, Double.MAX_VALUE, Double.MIN_VALUE,
            4.9e-324, 2.2250738585072014e-308, 9007199254740993.0, 123456.789,
        )
        for (v in values) {
            assertEquals(v, render(v).toDouble(), "${render(v)} does not parse back to $v")
        }
    }

    @Test
    fun `a large random sample round-trips, and no shorter spelling would have`() {
        val random = java.util.Random(20260909)
        repeat(20000) {
            val v = Double.fromBits(random.nextLong())
            if (v.isNaN() || v.isInfinite() || v == 0.0) return@repeat
            val text = EcmaNumber.render(v)
            assertEquals(v, text.toDouble(), "$text does not parse back to $v")

            // Shortest is the property, and it is the one Java 17 does not
            // guarantee. Count the significant digits emitted and prove that one
            // fewer would not have round-tripped.
            val digits = text.substringBefore('e').filter { it in '0'..'9' }.trimStart('0').trimEnd('0')
            val used = maxOf(digits.length, 1)
            if (used > 1) {
                val shorter = java.math.BigDecimal(v)
                    .round(java.math.MathContext(used - 1, java.math.RoundingMode.HALF_UP))
                assertTrue(
                    shorter.toDouble() != v,
                    "$v was written with $used digits but ${used - 1} would have round-tripped: $text",
                )
            }
        }
    }

    @Test
    fun `the JDK's own spelling is not canonical form, which is why this renderer exists`() {
        // Red proof for the renderer itself: if this ever stops failing, the
        // renderer has become redundant and should be deleted, not kept.
        assertNotEquals("340", 340.0.toString())
        assertNotEquals("1e+21", 1e21.toString())
        assertNotEquals("1e-7", 1e-7.toString())
        assertNotEquals("0.000001", 0.000001.toString())
    }
}
