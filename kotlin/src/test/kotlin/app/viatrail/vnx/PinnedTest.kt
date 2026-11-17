package app.viatrail.vnx

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * Section 3.8: every item but a folder carries `pinned`, a record-level
 * timestamp (section 4.3) that is kept verbatim and written only when set.
 */
class PinnedTest {
    private val ring = listOf(Position(1.0, 1.0), Position(2.0, 1.0), Position(2.0, 2.0))
    private val nodes = mapOf("n1" to Vertex(1.0, 1.0), "n2" to Vertex(1.0, 2.0))
    private val arcs = mapOf("a1" to Arc("n1", "n2", emptyList()))
    private val walk = listOf(Pass("a1", from = Endpoint.Node("n1"), to = Endpoint.Node("n2")))

    /** One item of every kind that carries `pinned`, each with its own value, and one unpinned top-level point. */
    private val document =
        VnxDocument(
            vnx = "0.10.0",
            networks =
                listOf(
                    Network(
                        name = "network",
                        pinned = "2026-10-20T08:15:00Z",
                        nodes = nodes,
                        arcs = arcs,
                        highlights = listOf(Highlight(walk, pinned = "2026-10-21T09:00:00.250Z")),
                        points = listOf(Point(Position(1.0, 1.5), name = "point", pinned = "2026-10-22T10:30:45.5Z")),
                        areas = listOf(Area(ring, name = "network area", pinned = "2026-10-23T11:00:00Z")),
                    ),
                ),
            plans = listOf(Plan(walk, nodes, arcs, name = "plan", pinned = "2026-10-24T12:00:00Z")),
            tracks = listOf(Track(listOf(Segment(ring)), name = "track", pinned = "2026-10-25T13:00:00.001Z")),
            points =
                listOf(
                    Point(Position(3.0, 3.0), name = "top-level point", pinned = "2026-10-26T14:00:00Z"),
                    Point(Position(4.0, 4.0), name = "unpinned"),
                ),
            areas = listOf(Area(ring, name = "area", pinned = "2026-13-45T99:99:99Z")),
        )

    private fun pinnedByName(d: VnxDocument): Map<String?, String?> {
        val out = mutableMapOf<String?, String?>()
        d.networks?.forEach { n ->
            out[n.name] = n.pinned
            n.highlights?.forEachIndexed { i, h -> out["${n.name} highlight $i"] = h.pinned }
            n.points?.forEach { out[it.name] = it.pinned }
            n.areas?.forEach { out[it.name] = it.pinned }
        }
        d.plans?.forEach { out[it.name] = it.pinned }
        d.tracks?.forEach { out[it.name] = it.pinned }
        d.points?.forEach { out[it.name] = it.pinned }
        d.areas?.forEach { out[it.name] = it.pinned }
        return out
    }

    @Test
    fun `every kind of item keeps its pinned through a write and a read, verbatim`() {
        val read = Vnx.readDocument(Vnx.write(document))
        assertEquals(emptyList(), read.diagnostics)
        val want = pinnedByName(document)
        assertEquals(9, want.size)
        assertEquals(want, pinnedByName(read.document))
    }

    @Test
    fun `an unpinned item is written without pinned`() {
        val written = JsonParser.parse(Vnx.write(document)) as JsonObject
        val points = (written["points"] as JsonArray).elements.map { it as JsonObject }
        assertEquals("2026-10-26T14:00:00Z", (points[0]["pinned"] as JsonString).value)
        assertFalse("pinned" in points[1].keys, "an unpinned point carries no pinned")
        assertTrue("pinned" in points[0].keys)
    }
}
