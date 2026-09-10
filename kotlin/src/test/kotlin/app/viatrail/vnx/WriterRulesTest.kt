package app.viatrail.vnx

import java.io.File
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * The emit-boundary rules of SPEC.md section 4.1, at this library's own writer.
 *
 * There is ONE emit operation. A writer rounds and shortens, always, so what it
 * writes is legitimately not the bytes it read — which is why section 8.1
 * compares documents as JSON trees.
 */
class WriterRulesTest {

    private val corpus = File(System.getProperty("vnx.corpus")!!)

    @Test
    fun `a writer rounds a coordinate to six places`() {
        // accept/full-precision-coordinate.vnx is a READER-PRESERVATION case: the
        // reader must not round, and the writer must.
        val file = File(corpus, "accept/full-precision-coordinate.vnx")
        val document = Vnx.readDocument(file.readText()).document
        assertEquals(-3.6712345678901234, document.waypoints!![0].at.lon, "the reader must not round")
        assertTrue(Vnx.write(document).contains("[-3.671235,57.118123]"), Vnx.write(document))
    }

    @Test
    fun `a writer emits the shortest spelling of a position`() {
        // accept/absent-elevation-and-time.vnx carries the LONGER spelling, and
        // says the same thing as the shorter one.
        val file = File(corpus, "accept/absent-elevation-and-time.vnx")
        val written = Vnx.write(Vnx.readDocument(file.readText()).document)
        assertTrue(written.contains("[-3.6712,57.118,341]"), "the trailing null must go: $written")
        assertTrue(written.contains("[-3.6712,57.118]"), "the waypoint's trailing null must go too: $written")
        assertTrue(!written.contains("null"), "no null should survive shortening here: $written")
    }

    @Test
    fun `an interior absence keeps its null`() {
        // Section 4.1: dropping it would move the time into the elevation channel.
        val document = Vnx.readDocument("""{"vnx":"0.1.0","waypoints":[{"at":[-3,57,null,1778914991000]}]}""").document
        assertTrue(Vnx.write(document).contains("[-3,57,null,1778914991000]"), Vnx.write(document))
    }

    @Test
    fun `elevation rounds to one place and time is never rounded`() {
        val document = Vnx.readDocument("""{"vnx":"0.1.0","waypoints":[{"at":[-3.12345678,57.87654321,340.06,1778914991000]}]}""").document
        assertTrue(Vnx.write(document).contains("[-3.123457,57.876543,340.1,1778914991000]"), Vnx.write(document))
    }

    @Test
    fun `a writer omits hidden false`() {
        // Section 3.8: a writer emits the key only when its value is `true`, and a
        // document SHOULD NOT contain `"hidden": false`.
        val document = Vnx.readDocument("""{"vnx":"0.1.0","networks":[{"hidden":false},{"hidden":true}]}""").document
        assertEquals("""{"vnx":"0.1.0","networks":[{},{"hidden":true}]}""" + "\n", Vnx.write(document))
    }

    @Test
    fun `a writer emits compact JSON with no whitespace but the closing newline`() {
        // Nothing about a document's bytes is constrained, and indentation cost
        // about three times the file where the reference implementation measured
        // it. Pretty-printing is a tool somebody may want; it is not a rule.
        val written = Vnx.write(Vnx.readDocument("""{"vnx":"0.1.0"}""").document)
        assertEquals("{\"vnx\":\"0.1.0\"}\n", written)
    }

    @Test
    fun `writing is idempotent, so a written document is a fixed point`() {
        for (file in File(corpus, "accept").listFiles()!!.filter { it.name.endsWith(".vnx") }) {
            val once = Vnx.write(Vnx.readDocument(file.readText()).document)
            val twice = Vnx.write(Vnx.readDocument(once).document)
            assertEquals(once, twice, "${file.name}: writing what a writer wrote changed it again")
        }
    }

    @Test
    fun `a whole number below two to the fifty-third is written as an integer`() {
        // A `time_ms` is a count of milliseconds, and `1778914991000` is a better
        // spelling of it than `1.778914991E12`. Both are the same double, so this
        // is legibility rather than conformance — and the bound is not decoration,
        // because `Double.toLong()` saturates.
        val document = Vnx.readDocument(
            """{"vnx":"0.1.0","waypoints":[{"at":[-3,57],"data":[{"@type":"x","t":1778914991000,"huge":1e19}]}]}""",
        ).document
        val written = Vnx.write(document)
        assertTrue(written.contains("\"t\":1778914991000"), written)
        assertTrue(!written.contains("9223372036854775807"), "a saturated Long would be a different value: $written")
        assertEquals(1e19, (JsonParser.parse(written).let { it as JsonObject }
            .let { ((it["waypoints"] as JsonArray).elements[0] as JsonObject) }
            .let { ((it["data"] as JsonArray).elements[0] as JsonObject) }["huge"] as JsonNumber).value)
    }
}
