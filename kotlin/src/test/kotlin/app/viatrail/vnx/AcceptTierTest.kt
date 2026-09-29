package app.viatrail.vnx

import java.io.File
import kotlin.test.Test
import kotlin.test.assertNotNull
import kotlin.test.assertNull

/**
 * The proof that the accept tier's check is worth what SPEC.md section 8.1
 * claims for it.
 *
 * THIS IS WHY THE TIER COMPARES AGAINST THE ORIGINAL FILE AND NOT AGAINST
 * ANOTHER MODEL. A reader that discarded every data record, every application
 * key, every style or every `notes` produces a model that compares equal to itself
 * and passes a model-versus-model tier GREEN. That is the entire extensibility
 * mechanism, all styling and all user prose, silently droppable by a
 * "conforming" implementation.
 *
 * Each case below is one of those readers, made by spoiling a document this
 * library read correctly. If any of them stops being caught, the tier has gone
 * back to proving nothing about the model.
 */
class AcceptTierTest {
    private val corpus = File(System.getProperty("vnx.corpus")!!)
    private val case = File(corpus, "accept/example-network.vnx")
    private val options = Tree.Options(emitBoundary = true)

    private fun spoiled(): List<Pair<String, (VnxDocument) -> VnxDocument>> =
        listOf(
            "drops every data record" to { d: VnxDocument ->
                d.copy(
                    waypoints = d.waypoints?.map { it.copy(data = null) },
                    tracks = d.tracks?.map { it.copy(data = null) },
                    routes = d.routes?.map { r -> r.copy(data = null, arcs = r.arcs?.mapValues { (_, a) -> a.copy(data = null) }) },
                    networks =
                        d.networks?.map { n ->
                            n.copy(
                                data = null,
                                arcs = n.arcs?.mapValues { (_, a) -> a.copy(data = null) },
                                highlights = n.highlights?.map { it.copy(data = null) },
                                points = n.points?.map { it.copy(data = null) },
                                areas = n.areas?.map { it.copy(data = null) },
                            )
                        },
                )
            },
            "drops every application key" to { d: VnxDocument -> d.copy(appKeys = emptyList()) },
            "drops every style" to { d: VnxDocument ->
                d.copy(
                    waypoints = d.waypoints?.map { it.copy(style = null) },
                    areas = d.areas?.map { it.copy(style = null) },
                    tracks = d.tracks?.map { it.copy(style = null) },
                    routes = d.routes?.map { it.copy(style = null) },
                    networks =
                        d.networks?.map { n ->
                            n.copy(
                                highlights = n.highlights?.map { it.copy(style = null) },
                                points = n.points?.map { it.copy(style = null) },
                                areas = n.areas?.map { it.copy(style = null) },
                            )
                        },
                )
            },
            "drops every notes" to { d: VnxDocument ->
                d.copy(
                    waypoints = d.waypoints?.map { it.copy(notes = null) },
                    areas = d.areas?.map { it.copy(notes = null) },
                    tracks = d.tracks?.map { it.copy(notes = null) },
                    routes = d.routes?.map { it.copy(notes = null) },
                    networks =
                        d.networks?.map { n ->
                            n.copy(notes = null, highlights = n.highlights?.map { it.copy(notes = null) })
                        },
                )
            },
        )

    @Test
    fun `the accept tier catches a reader that discards what it read`() {
        val original = case.readText()
        val document = Vnx.readDocument(original).document
        // LIVENESS. An unspoiled document must pass, or every case below would
        // be caught by whatever is really wrong.
        assertNull(
            Tree.compare(Vnx.write(document), original, options),
            "the unspoiled document must pass, or nothing below decides anything",
        )
        for ((name, spoil) in spoiled()) {
            assertNotNull(
                Tree.compare(Vnx.write(spoil(document)), original, options),
                "a reader that $name passed the accept tier",
            )
        }
    }

    /**
     * And the same four readers, compared MODEL AGAINST MODEL, all pass. This is
     * the measurement rather than the claim: it is what makes the test above a
     * property of the comparison and not of the fixture.
     */
    @Test
    fun `model against model lets all four through`() {
        val document = Vnx.readDocument(case.readText()).document
        for ((name, spoil) in spoiled()) {
            val model = spoil(document)
            assertNull(
                Tree.compare(Vnx.write(model), Vnx.write(model), options),
                "a reader that $name is caught by comparing its model against itself, " +
                    "which would mean this test no longer says what it says",
            )
        }
    }
}
