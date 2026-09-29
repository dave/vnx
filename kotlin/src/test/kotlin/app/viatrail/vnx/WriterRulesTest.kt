package app.viatrail.vnx

import java.io.File
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

/**
 * The emit-boundary rule of SPEC.md section 4.1, at this library's own writer.
 *
 * There is ONE emit operation, and it SHORTENS a position and never rounds one.
 * What it writes is still legitimately not the bytes it read — key order, number
 * spelling and `hidden: false` all move — which is why section 8.1 compares
 * documents as JSON trees.
 */
class WriterRulesTest {
    private val corpus = File(System.getProperty("vnx.corpus")!!)

    @Test
    fun `neither the reader nor the writer rounds a coordinate`() {
        // accept/full-precision-coordinate.vnx is the PRESERVATION case. Section
        // 4.1 requires a reader to keep the binary64 value the document states,
        // and no longer asks a writer to round it: rounding is advice there,
        // mirroring RFC 7946 section 11.2, and carries no requirement.
        val file = File(corpus, "accept/full-precision-coordinate.vnx")
        val document = Vnx.readDocument(file.readText()).document
        assertEquals(-3.6712345678901234, document.waypoints!![0].at.lon, "the reader must not round")
        val written = Vnx.write(document)
        assertTrue(written.contains("[-3.6712345678901235,57.11812345678901]"), written)
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
        val document = Vnx.readDocument("""{"vnx":"0.2.0","waypoints":[{"at":[-3,57,null,1778914991000]}]}""").document
        assertTrue(Vnx.write(document).contains("[-3,57,null,1778914991000]"), Vnx.write(document))
    }

    @Test
    fun `a value passes through the writer unchanged in every channel`() {
        // The trap this replaces: `Math.round(x * 10) / 10` on the elevation, and
        // a six-place round on the coordinates. Every channel here would move
        // under the rule that left the format, and none may move now.
        val document = Vnx.readDocument("""{"vnx":"0.2.0","waypoints":[{"at":[-3.12345678,57.87654321,340.25,1778914991000]}]}""").document
        assertTrue(Vnx.write(document).contains("[-3.12345678,57.87654321,340.25,1778914991000]"), Vnx.write(document))
    }

    @Test
    fun `a writer omits hidden false`() {
        // Section 3.8: a writer emits the key only when its value is `true`, and a
        // document SHOULD NOT contain `"hidden": false`.
        val document = Vnx.readDocument("""{"vnx":"0.2.0","networks":[{"hidden":false},{"hidden":true}]}""").document
        assertEquals("""{"vnx":"0.2.0","networks":[{},{"hidden":true}]}""" + "\n", Vnx.write(document))
    }

    @Test
    fun `a writer emits compact JSON with no whitespace but the closing newline`() {
        // Nothing about a document's bytes is constrained, and indentation cost
        // about three times the file where the reference implementation measured
        // it. Pretty-printing is a tool somebody may want; it is not a rule.
        val written = Vnx.write(Vnx.readDocument("""{"vnx":"0.2.0"}""").document)
        assertEquals("{\"vnx\":\"0.2.0\"}\n", written)
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
        val document =
            Vnx
                .readDocument(
                    """{"vnx":"0.2.0","waypoints":[{"at":[-3,57],"data":[{"@type":"x","t":1778914991000,"huge":1e19}]}]}""",
                ).document
        val written = Vnx.write(document)
        assertTrue(written.contains("\"t\":1778914991000"), written)
        assertTrue(!written.contains("9223372036854775807"), "a saturated Long would be a different value: $written")
        assertEquals(
            1e19,
            (
                JsonParser
                    .parse(written)
                    .let { it as JsonObject }
                    .let { ((it["waypoints"] as JsonArray).elements[0] as JsonObject) }
                    .let { ((it["data"] as JsonArray).elements[0] as JsonObject) }["huge"] as JsonNumber
            ).value,
        )
    }

    // ------------------------------------------------ what a writer refuses

    private val nodes = mapOf("a" to Vertex(-3.0, 57.0), "b" to Vertex(-2.0, 57.0))
    private val ab = mapOf("ab" to Arc("a", "b", emptyList()))

    private fun network(
        walk: List<Pass>,
        arcs: Map<String, Arc> = ab,
    ) = VnxDocument(vnx = "0.2.0", networks = listOf(Network(nodes = nodes, arcs = arcs, highlights = listOf(Highlight(walk)))))

    private fun refused(document: VnxDocument): String = assertFailsWith<IllegalArgumentException> { Vnx.write(document) }.message!!

    @Test
    fun `a writer emits a ring open, and refuses a closed one rather than opening it`() {
        // Section 3.7. Opening the ring would be the writer guessing that its
        // caller meant the repeat.
        val open = listOf(Position(-3.0, 57.0), Position(-2.0, 57.0), Position(-2.0, 58.0))
        val written = Vnx.write(VnxDocument(vnx = "0.2.0", areas = listOf(Area(open))))
        assertEquals("""{"vnx":"0.2.0","areas":[{"polygon":[[-3,57],[-2,57],[-2,58]]}]}""" + "\n", written)
        val message = refused(VnxDocument(vnx = "0.2.0", areas = listOf(Area(open + Position(-3.0, 57.0, 5.0)))))
        assertTrue(message.contains("repeats its first"), message)
    }

    @Test
    fun `a writer refuses a ring of fewer than three distinct positions`() {
        val ring = listOf(Position(-3.0, 57.0), Position(-2.0, 57.0), Position(-2.0, 57.0, 9.0))
        assertTrue(refused(VnxDocument(vnx = "0.2.0", areas = listOf(Area(ring)))).contains("3 distinct"))
    }

    @Test
    fun `a writer refuses a self-loop arc, and an arc naming an absent node`() {
        assertTrue(refused(network(emptyList(), mapOf("aa" to Arc("a", "a", emptyList())))).contains("to itself"))
        assertTrue(refused(network(emptyList(), mapOf("ax" to Arc("a", "x", emptyList())))).contains("\"x\""))
    }

    @Test
    fun `a writer refuses a walk its own reader would drop, naming the pass and the code`() {
        val cases =
            listOf(
                listOf(Pass("ghost", from = Endpoint.Node("a"), to = Endpoint.Node("b"))) to "pass-arc-absent",
                listOf(Pass("ab", to = Endpoint.Node("b"))) to "walk-spelling",
                listOf(Pass("ab", from = Endpoint.Node("a"), to = Endpoint.Node("x"))) to "walk-not-connected",
                listOf(Pass("ab", from = Endpoint.Node("a"), turn = LonLat(-2.5, 57.0), pinned = true, to = Endpoint.Node("a"))) to
                    "pinned-turn",
            )
        for ((walk, code) in cases) {
            val message = refused(network(walk))
            assertTrue(message.contains("pass 0") && message.contains(code), "$walk: $message")
        }
    }

    @Test
    fun `a writer refuses a route whose walk is not node to node`() {
        val route =
            Route(
                walk = listOf(Pass("ab", from = Endpoint.Node("a"), to = Endpoint.At(LonLat(-2.5, 57.0)))),
                nodes = nodes,
                arcs = ab,
            )
        assertTrue(refused(VnxDocument(vnx = "0.2.0", routes = listOf(route))).contains("route-pin-or-position"))
    }

    @Test
    fun `a writer emits pinned only when true, and from and to only where the pass carries them`() {
        // Section 3.3.3: a writer MUST emit `pinned` only when its value is true.
        val nodes = nodes + ("c" to Vertex(-1.0, 57.0))
        val arcs = ab + ("bc" to Arc("b", "c", listOf(Vertex(-1.5, 57.1, 40.0))))
        val walk =
            listOf(
                Pass("ab", from = Endpoint.At(LonLat(-2.5, 57.0)), to = Endpoint.Node("b")),
                Pass("bc", pinned = true),
                Pass("bc", turn = LonLat(-1.2, 57.05)),
                Pass("bc"),
                Pass("ab", pinned = false, to = Endpoint.Node("a")),
            )
        val document =
            VnxDocument(
                vnx = "0.2.0",
                networks = listOf(Network(nodes = nodes, arcs = arcs, highlights = listOf(Highlight(walk)))),
            )
        val written = Vnx.write(document)
        assertTrue(
            written.contains(
                """"walk":[{"arc":"ab","from":[-2.5,57],"to":"b"},{"arc":"bc","pinned":true},""" +
                    """{"arc":"bc","turn":[-1.2,57.05]},{"arc":"bc"},{"arc":"ab","to":"a"}]""",
            ),
            written,
        )
        assertTrue(written.contains(""""bc":{"from":"b","to":"c","pips":[[-1.5,57.1,40]]}"""), written)
        // And what was written reads back clean, to the same model.
        val back = Vnx.readDocument(written)
        assertEquals(emptyList(), back.diagnostics)
        assertEquals(document, back.document)
    }
}
