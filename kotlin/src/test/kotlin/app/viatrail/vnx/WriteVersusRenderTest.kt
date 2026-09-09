package app.viatrail.vnx

import java.io.File
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotEquals
import kotlin.test.assertTrue

/**
 * SPEC.md section 8.1 names WRITE and RE-RENDER as two operations, and the
 * accept tier requires the second. If they are the same function, one of the two
 * is wrong — so this proves they differ, on the cases where they must.
 */
class WriteVersusRenderTest {

    private val corpus = File(System.getProperty("vnx.corpus")!!)

    @Test
    fun `a writer rounds and a re-render does not`() {
        // accept/full-precision-coordinate.vnx is a READER-PRESERVATION case. Its
        // sidecar says so: no conforming writer emits these values any more,
        // because a writer rounds to 6 places, and encoding this document through
        // a writer gives -3.671235.
        val file = File(corpus, "accept/full-precision-coordinate.vnx")
        val document = Vnx.readDocument(file.readText()).document
        assertEquals(file.readText(), Vnx.render(document), "a re-render must reproduce the file")
        val written = Vnx.write(document)
        assertNotEquals(file.readText(), written, "a writer that did not round would make this tier vacuous")
        assertTrue(written.contains("[-3.671235, 57.118123]"), "got: $written")
    }

    @Test
    fun `a writer emits the shortest spelling of a position and a re-render keeps the arity`() {
        // accept/absent-elevation-and-time.vnx is stored canonical and carries the
        // LONGER spelling. A model that normalised it could not re-render the
        // corpus; a writer that did not shorten would not be a writer.
        val file = File(corpus, "accept/absent-elevation-and-time.vnx")
        val document = Vnx.readDocument(file.readText()).document
        assertEquals(file.readText(), Vnx.render(document))
        val written = Vnx.write(document)
        assertTrue(written.contains("[-3.6712, 57.118, 341]"), "the trailing null must go: $written")
        assertTrue(written.contains("[-3.6712, 57.118]"), "the waypoint's trailing null must go too: $written")
        assertTrue(!written.contains("null"), "no null should survive shortening here: $written")
    }

    @Test
    fun `an interior absence keeps its null through both operations`() {
        // Section 4.1: dropping it would move the time into the elevation channel.
        val document = Vnx.readDocument("""{"vnx":"0.1.0","waypoints":[{"at":[-3,57,null,1778914991000]}]}""").document
        assertTrue(Vnx.render(document).contains("[-3, 57, null, 1778914991000]"))
        assertTrue(Vnx.write(document).contains("[-3, 57, null, 1778914991000]"))
    }

    @Test
    fun `elevation rounds to one place and time is never rounded`() {
        val document = Vnx.readDocument("""{"vnx":"0.1.0","waypoints":[{"at":[-3.12345678,57.87654321,340.06,1778914991000]}]}""").document
        assertTrue(Vnx.write(document).contains("[-3.123457, 57.876543, 340.1, 1778914991000]"), Vnx.write(document))
    }

    @Test
    fun `a writer omits hidden false and a re-render reproduces it`() {
        // Section 3.8: a writer MUST emit the key only when its value is `true`,
        // and a document SHOULD NOT contain `"hidden": false` — but one that does
        // must still come back unchanged from a re-render.
        val json = "{\n  \"vnx\": \"0.1.0\",\n  \"networks\": [\n    {\n      \"hidden\": false\n    }\n  ]\n}\n"
        val document = Vnx.readDocument(json).document
        assertEquals(json, Vnx.render(document))
        assertEquals("{\n  \"vnx\": \"0.1.0\",\n  \"networks\": [\n    {}\n  ]\n}\n", Vnx.write(document))
    }

    @Test
    fun `writing is idempotent, so a written document is canonical`() {
        for (file in File(corpus, "accept").listFiles()!!.filter { it.name.endsWith(".vnx") }) {
            val document = Vnx.readDocument(file.readText()).document
            val once = Vnx.write(document)
            val twice = Vnx.write(Vnx.readDocument(once).document)
            assertEquals(once, twice, "${file.name}: writing what a writer wrote changed it again")
        }
    }
}
