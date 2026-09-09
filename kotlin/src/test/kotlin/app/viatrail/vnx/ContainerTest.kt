package app.viatrail.vnx

import java.io.ByteArrayOutputStream
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

/** SPEC.md section 7, beyond the four container cases the corpus carries. */
class ContainerTest {

    private val doc = "{\n  \"vnx\": \"0.1.0\",\n  \"waypoints\": [\n    {\n      \"name\": \"In a container\"," +
        "\n      \"at\": [-3.675, 57.1201]\n    }\n  ]\n}\n"

    private fun zip(vararg entries: Pair<String, String>, stored: Boolean = false): ByteArray {
        val out = ByteArrayOutputStream()
        ZipOutputStream(out).use { zo ->
            for ((name, body) in entries) {
                val bytes = body.toByteArray()
                val entry = ZipEntry(name)
                if (stored) {
                    entry.method = ZipEntry.STORED
                    entry.size = bytes.size.toLong()
                    entry.compressedSize = bytes.size.toLong()
                    val crc = java.util.zip.CRC32()
                    crc.update(bytes)
                    entry.crc = crc.value
                }
                zo.putNextEntry(entry)
                zo.write(bytes)
                zo.closeEntry()
            }
        }
        return out.toByteArray()
    }

    @Test
    fun `doc-vnx at the root wins over an earlier vnx entry`() {
        val r = Vnx.readContainer(zip("aaa.vnx" to """{"vnx":"0.1.0"}""", "doc.vnx" to doc))
        assertEquals("In a container", r.document.waypoints!![0].name)
        assertEquals(listOf("aaa.vnx"), r.attachments)
    }

    @Test
    fun `with no doc-vnx the FIRST vnx entry in archive order wins`() {
        val r = Vnx.readContainer(zip("first.vnx" to doc, "second.vnx" to """{"vnx":"0.1.0"}"""))
        assertEquals("In a container", r.document.waypoints!![0].name)
        assertEquals(listOf("second.vnx"), r.attachments)
    }

    @Test
    fun `the root-document match is on the full entry name, not the base name`() {
        // Section 7's note: an implementation matching on the base name alone
        // matches at any depth and lets the last match win rather than the first.
        val r = Vnx.readContainer(zip("first.vnx" to doc, "sub/doc.vnx" to """{"vnx":"0.1.0"}"""))
        assertEquals(
            "In a container", r.document.waypoints!![0].name,
            "sub/doc.vnx is not a root document, so the fallback takes the first .vnx entry",
        )
    }

    @Test
    fun `names are compared case-insensitively`() {
        val r = Vnx.readContainer(zip("aaa.vnx" to """{"vnx":"0.1.0"}""", "DOC.VNX" to doc))
        assertEquals("In a container", r.document.waypoints!![0].name)
        val s = Vnx.readContainer(zip("only.VNX" to doc))
        assertEquals("In a container", s.document.waypoints!![0].name)
    }

    @Test
    fun `doc-vnx beside DOC-VNX is a duplicate`() {
        // The comparison matches the root-document rule for exactly this reason.
        val e = assertFailsWith<VnxRefusal> { Vnx.readContainer(zip("doc.vnx" to doc, "DOC.VNX" to doc)) }
        assertEquals(RefusalCause.CONTAINER_DUPLICATE_ENTRY, e.refusal)
    }

    @Test
    fun `a stored entry reads as well as a deflated one`() {
        val r = Vnx.readContainer(zip("doc.vnx" to doc, "notes.txt" to "hello", stored = true))
        assertEquals("In a container", r.document.waypoints!![0].name)
        assertEquals(listOf("notes.txt"), r.attachments)
    }

    @Test
    fun `every escaping path shape is refused`() {
        for (name in listOf("../secret", "a/../../secret", "/etc/passwd", "C:/windows", "a\\b")) {
            val e = assertFailsWith<VnxRefusal>("\"$name\" must be refused") {
                Vnx.readContainer(zip("doc.vnx" to doc, name to "x"))
            }
            assertEquals(RefusalCause.CONTAINER_PATH_ESCAPES, e.refusal, "for \"$name\"")
            assertTrue(e.message.contains("escapes the archive"))
        }
    }

    @Test
    fun `dot-dot is a segment, not a substring`() {
        // Section 7 in as many words: `a..b/photo.jpg` is a legal name and escapes
        // nothing. A reader checking for the substring refuses a valid archive.
        val r = Vnx.readContainer(zip("doc.vnx" to doc, "a..b/photo.jpg" to "x", "..hidden" to "y"))
        assertEquals(listOf("a..b/photo.jpg", "..hidden"), r.attachments)
    }

    @Test
    fun `an archive with no vnx entry is refused`() {
        val e = assertFailsWith<VnxRefusal> { Vnx.readContainer(zip("readme.txt" to "nothing here")) }
        assertEquals(RefusalCause.CONTAINER_NO_DOCUMENT, e.refusal)
    }

    @Test
    fun `bytes that are not a ZIP archive are refused, not read`() {
        val e = assertFailsWith<VnxRefusal> { Vnx.readContainer("not a zip at all".toByteArray()) }
        assertEquals(RefusalCause.CONTAINER_MALFORMED, e.refusal)
    }

    @Test
    fun `a fault in the document inside a container is the document's fault, reported as such`() {
        val e = assertFailsWith<VnxRefusal> { Vnx.readContainer(zip("doc.vnx" to """{"vnx":"9.9.9"}""")) }
        assertEquals(RefusalCause.VERSION_TOO_NEW, e.refusal)
    }

    @Test
    fun `the corpus containers hold the document the corpus says they do`() {
        val corpus = java.io.File(System.getProperty("vnx.corpus")!!)
        for (name in listOf("accept/container-root-doc.vnz", "accept/container-named-document.vnz")) {
            val r = Vnx.readContainer(java.io.File(corpus, name).readBytes())
            assertEquals("In a container", r.document.waypoints!![0].name, name)
            assertEquals(emptyList(), r.diagnostics, name)
        }
    }
}
