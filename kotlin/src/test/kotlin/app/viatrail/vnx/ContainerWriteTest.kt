package app.viatrail.vnx

import java.io.ByteArrayInputStream
import java.util.zip.ZipInputStream
import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

/**
 * Writing a `.vnz` container (SPEC.md section 7).
 *
 * The container has the same two emit paths as a plain document, for the reason
 * that matters most at this boundary: repackaging an archive — adding a
 * photograph, renaming a folder — must not silently round every coordinate in
 * the document it carries.
 */
class ContainerWriteTest {

    private val fullPrecision = """{"vnx":"0.1.0","waypoints":[{"name":"Precise","at":[-3.6712345678901235,57.11812345678901]}]}"""

    private fun documentOf(json: String) = Vnx.readDocument(json).document

    @Test
    fun `a container round-trips through this library's own reader`() {
        val document = documentOf("""{"vnx":"0.1.0","waypoints":[{"name":"Bothy","at":[-3.6712,57.118]}]}""")
        val bytes = Vnx.renderContainer(document, linkedMapOf("attachments/photo.txt" to "not a photo\n".toByteArray()))
        val back = Vnx.readContainer(bytes)
        assertEquals(emptyList(), back.diagnostics)
        assertEquals("Bothy", back.document.waypoints!![0].name)
        assertEquals(listOf("attachments/photo.txt"), back.attachments)
    }

    /**
     * The guard that matters: a reader and a writer written by the same person
     * can be wrong in the same way and agree perfectly. `java.util.zip` is an
     * independent implementation of the container format, and it did not read
     * this specification.
     */
    @Test
    fun `the archive is a valid ZIP to a reader that is not mine`() {
        val bytes = Vnx.renderContainer(
            documentOf("""{"vnx":"0.1.0"}"""),
            linkedMapOf("a.txt" to "hello".toByteArray(), "deep/b.bin" to ByteArray(5000) { it.toByte() }),
        )
        val seen = LinkedHashMap<String, ByteArray>()
        ZipInputStream(ByteArrayInputStream(bytes)).use { zin ->
            while (true) {
                val entry = zin.nextEntry ?: break
                seen[entry.name] = zin.readBytes()
            }
        }
        assertEquals(listOf("doc.vnx", "a.txt", "deep/b.bin"), seen.keys.toList())
        assertEquals("hello", String(seen["a.txt"]!!))
        assertContentEquals(ByteArray(5000) { it.toByte() }, seen["deep/b.bin"])
        assertTrue(String(seen["doc.vnx"]!!).endsWith("}\n"))
    }

    @Test
    fun `rendering a container preserves precision and writing one rounds it`() {
        val document = documentOf(fullPrecision)
        val rendered = String(inner(Vnx.renderContainer(document)))
        val written = String(inner(Vnx.writeContainer(document)))
        assertTrue(
            rendered.contains("[-3.6712345678901235, 57.11812345678901]"),
            "repackaging must not round the document it carries: $rendered",
        )
        assertTrue(written.contains("[-3.671235, 57.118123]"), "a writer rounds: $written")
        assertEquals(Vnx.render(document), rendered)
        assertEquals(Vnx.write(document), written)
    }

    @Test
    fun `a container this library wrote is byte-identical every time`() {
        // A format whose whole purpose is that two libraries emit identical files
        // should not lose that at the container boundary over a clock reading.
        // SPEC.md does not require this; it is raised rather than assumed.
        val document = documentOf(fullPrecision)
        val once = Vnx.renderContainer(document, linkedMapOf("a.txt" to "hello".toByteArray()))
        Thread.sleep(5)
        val twice = Vnx.renderContainer(document, linkedMapOf("a.txt" to "hello".toByteArray()))
        assertContentEquals(once, twice)
    }

    @Test
    fun `a writer refuses every path its own reader would refuse`() {
        // A writer that can emit an archive its own reader rejects is a writer
        // with a hole in it.
        for (name in listOf("../secret", "a/../../secret", "/etc/passwd", "C:/windows", "a\\b", "", "dir/")) {
            val e = assertFailsWith<VnxRefusal>("\"$name\" must be refused") {
                Vnx.renderContainer(documentOf("""{"vnx":"0.1.0"}"""), linkedMapOf(name to "x".toByteArray()))
            }
            assertEquals(RefusalCause.CONTAINER_PATH_ESCAPES, e.refusal, "for \"$name\"")
        }
    }

    @Test
    fun `an attachment may not collide with the root document, case-insensitively`() {
        for (name in listOf("doc.vnx", "DOC.VNX", "Doc.Vnx")) {
            val e = assertFailsWith<VnxRefusal>("\"$name\" must collide") {
                Vnx.renderContainer(documentOf("""{"vnx":"0.1.0"}"""), linkedMapOf(name to "x".toByteArray()))
            }
            assertEquals(RefusalCause.CONTAINER_DUPLICATE_ENTRY, e.refusal, "for \"$name\"")
        }
    }

    @Test
    fun `a non-ASCII attachment name survives, which is the UTF-8 filename flag working`() {
        // This reader refuses a non-ASCII name in an archive that does not set the
        // flag, rather than guessing between UTF-8 and CP437 — so this round trip
        // failing is exactly how a missing flag would announce itself.
        val name = "photos/caf\u00e9-\u5c71.jpg"
        val bytes = Vnx.renderContainer(documentOf("""{"vnx":"0.1.0"}"""), linkedMapOf(name to "x".toByteArray()))
        assertEquals(listOf(name), Vnx.readContainer(bytes).attachments)
    }

    @Test
    fun `an empty attachment and a large one both survive`() {
        val big = ByteArray(300_000) { (it * 31).toByte() }
        val bytes = Vnx.renderContainer(
            documentOf("""{"vnx":"0.1.0"}"""),
            linkedMapOf("empty.bin" to ByteArray(0), "big.bin" to big),
        )
        assertEquals(listOf("empty.bin", "big.bin"), Vnx.readContainer(bytes).attachments)
        ZipInputStream(ByteArrayInputStream(bytes)).use { zin ->
            var checked = 0
            while (true) {
                val entry = zin.nextEntry ?: break
                val body = zin.readBytes()
                if (entry.name == "big.bin") {
                    assertContentEquals(big, body)
                    checked++
                }
                if (entry.name == "empty.bin") {
                    assertEquals(0, body.size)
                    checked++
                }
            }
            assertEquals(2, checked, "the independent reader did not see both attachments")
        }
    }

    /** The bytes of the `doc.vnx` entry, read back out of an archive. */
    private fun inner(bytes: ByteArray): ByteArray {
        ZipInputStream(ByteArrayInputStream(bytes)).use { zin ->
            while (true) {
                val entry = zin.nextEntry ?: break
                if (entry.name == "doc.vnx") return zin.readBytes()
            }
        }
        error("the archive carries no doc.vnx")
    }
}
