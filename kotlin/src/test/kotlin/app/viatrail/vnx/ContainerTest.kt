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
        // ARCHIVE ORDER IS A RULE HERE, and this is the only place section 7
        // makes it one: "Where there is none, the reader takes the first entry
        // with a `.vnx` extension in archive order." `second.vnx` being the
        // attachment is what says `first.vnx` was taken.
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
    fun `a backslash anywhere is refused, and refused BEFORE the dot-dot hunt`() {
        // Ruled 2026-09-09, deliberately stricter than the ZIP appnote. Section 7
        // says a `..` SEGMENT is refused and that `..` is a segment rather than a
        // substring, but never says whether `\` counts as a separator while
        // hunting for one — so a reader splitting on both refuses `..\x` and a
        // reader splitting on `/` alone accepts it, and NEITHER fails any tier. On
        // a Windows-authored archive that is a real escape. Refusing the backslash
        // unconditionally and first means the subtle question is never asked.
        for (name in listOf("..\\x", "a\\..\\b", "a\\b", "\\", "dir\\file.jpg", "ok/../..\\x")) {
            val e = assertFailsWith<VnxRefusal>("\"$name\" must be refused") {
                Vnx.readContainer(zip("doc.vnx" to doc, name to "x"))
            }
            assertEquals(RefusalCause.CONTAINER_PATH_ESCAPES, e.refusal, "for \"$name\"")
            assertTrue(
                e.message.contains("forward-slash separated"),
                "the backslash must be the stated reason, not the `..` segment: ${e.message}",
            )
        }
    }

    @Test
    fun `dot-dot is a segment, not a substring`() {
        // Section 7 in as many words: `a..b/photo.jpg` is a legal name and escapes
        // nothing. A reader checking for the substring refuses a valid archive.
        val r = Vnx.readContainer(zip("doc.vnx" to doc, "a..b/photo.jpg" to "x", "..hidden" to "y"))
        // A COLLECTION. The subject here is which names are accepted, and section
        // 7 states no rule about the order a reader reports them in.
        assertEquals(listOf("..hidden", "a..b/photo.jpg"), r.attachments!!.sorted())
    }

    @Test
    fun `an archive with no vnx entry is refused`() {
        val e = assertFailsWith<VnxRefusal> { Vnx.readContainer(zip("readme.txt" to "nothing here")) }
        assertEquals(RefusalCause.CONTAINER_NO_DOCUMENT, e.refusal)
    }

    /**
     * A ZIP header that lies, in the four ways it can.
     *
     * None of these is reachable from a corpus fixture — every committed archive
     * is well-formed — so they are built here. A malformed archive must be
     * REFUSED, and in particular must not crash the library: the sizes and the
     * CRC come from the archive, so an archive that is hostile or merely broken
     * can declare anything it likes about them.
     */
    private fun lying(
        name: String = "doc.vnx",
        body: ByteArray,
        method: Int,
        payload: ByteArray = body,
        declaredCompressed: Long = payload.size.toLong(),
        declaredUncompressed: Long = body.size.toLong(),
        crc: Long = java.util.zip.CRC32().apply { update(body) }.value,
    ): ByteArray {
        val n = name.toByteArray(Charsets.UTF_8)
        val out = java.io.ByteArrayOutputStream()
        fun le16(v: Int) { out.write(v and 0xff); out.write((v ushr 8) and 0xff) }
        fun le32(v: Long) { le16((v and 0xffff).toInt()); le16(((v ushr 16) and 0xffff).toInt()) }
        le32(0x04034b50); le16(10); le16(0x800); le16(method); le16(0); le16(0x21)
        le32(crc); le32(declaredCompressed); le32(declaredUncompressed); le16(n.size); le16(0)
        out.write(n); out.write(payload)
        val central = java.io.ByteArrayOutputStream()
        fun c16(v: Int) { central.write(v and 0xff); central.write((v ushr 8) and 0xff) }
        fun c32(v: Long) { c16((v and 0xffff).toInt()); c16(((v ushr 16) and 0xffff).toInt()) }
        c32(0x02014b50); c16(10); c16(10); c16(0x800); c16(method); c16(0); c16(0x21)
        c32(crc); c32(declaredCompressed); c32(declaredUncompressed)
        c16(n.size); c16(0); c16(0); c16(0); c16(0); c32(0); c32(0)
        central.write(n)
        val directoryOffset = out.size().toLong()
        val cd = central.toByteArray()
        out.write(cd)
        le32(0x06054b50); le16(0); le16(0); le16(1); le16(1); le32(cd.size.toLong()); le32(directoryOffset); le16(0)
        return out.toByteArray()
    }

    private fun deflated(body: ByteArray): ByteArray {
        val d = java.util.zip.Deflater(java.util.zip.Deflater.DEFAULT_COMPRESSION, true)
        d.setInput(body); d.finish()
        val out = java.io.ByteArrayOutputStream(); val buf = ByteArray(4096)
        while (!d.finished()) { val k = d.deflate(buf); if (k == 0 && d.needsInput()) break; out.write(buf, 0, k) }
        d.end()
        return out.toByteArray()
    }

    @Test
    fun `a header declaring a huge uncompressed size is refused, and does not exhaust the heap`() {
        // Measured 2026-09-10: allocating the DECLARED size up front turns a
        // twenty-byte archive claiming two gigabytes into an OutOfMemoryError —
        // an Error, so it escapes every catch a caller is likely to have written,
        // and it takes the process rather than the document. The declared size is
        // a limit, not an allocation.
        val doc = "{\n  \"vnx\": \"0.1.0\"\n}\n".toByteArray()
        val e = assertFailsWith<VnxRefusal> {
            Vnx.readContainer(
                lying(body = doc, method = 8, payload = deflated(doc), declaredUncompressed = 0x7FFFFFF0L),
            )
        }
        assertEquals(RefusalCause.CONTAINER_MALFORMED, e.refusal)
    }

    @Test
    fun `a stored entry whose two declared sizes differ is refused`() {
        // They are the same quantity by definition, so a header claiming
        // otherwise is lying about one of them. Left unchecked this read
        // SILENTLY, which is worse than the crash above.
        val doc = "{\n  \"vnx\": \"0.1.0\"\n}\n".toByteArray()
        val e = assertFailsWith<VnxRefusal> {
            Vnx.readContainer(lying(body = doc, method = 0, declaredUncompressed = 0xFFFFFFFEL))
        }
        assertEquals(RefusalCause.CONTAINER_MALFORMED, e.refusal)
    }

    @Test
    fun `a truncated deflate stream is refused rather than throwing`() {
        val doc = "{\n  \"vnx\": \"0.1.0\"\n}\n".toByteArray()
        val full = deflated(doc)
        val half = full.copyOfRange(0, full.size / 2)
        val e = assertFailsWith<VnxRefusal> {
            Vnx.readContainer(lying(body = doc, method = 8, payload = half, declaredCompressed = half.size.toLong()))
        }
        assertEquals(RefusalCause.CONTAINER_MALFORMED, e.refusal)
    }

    @Test
    fun `an entry whose bytes fail their CRC is refused`() {
        // Nothing in section 7 requires this, and a reader that skips it accepts
        // corrupt data in silence — which for a format whose whole argument is
        // that two libraries agree byte for byte is the wrong way to be lenient.
        val doc = "{\n  \"vnx\": \"0.1.0\"\n}\n".toByteArray()
        val e = assertFailsWith<VnxRefusal> {
            Vnx.readContainer(lying(body = doc, method = 0, crc = 0xDEADBEEFL))
        }
        assertEquals(RefusalCause.CONTAINER_MALFORMED, e.refusal)
        assertTrue(e.message.contains("CRC"), "the refusal must name what failed: ${e.message}")
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
