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

    private val doc = "{\n  \"vnx\": \"0.2.0\",\n  \"waypoints\": [\n    {\n      \"name\": \"In a container\"," +
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
        val r = Vnx.readContainer(zip("aaa.vnx" to """{"vnx":"0.2.0"}""", "doc.vnx" to doc))
        assertEquals("In a container", r.document.waypoints!![0].name)
        assertEquals(listOf("aaa.vnx"), r.attachments!!.map { it.path })
    }

    @Test
    fun `with no doc-vnx the FIRST vnx entry in archive order wins`() {
        // ARCHIVE ORDER IS A RULE HERE, and this is the only place section 7
        // makes it one: "Where there is none, the reader takes the first entry
        // with a `.vnx` extension in archive order." `second.vnx` being the
        // attachment is what says `first.vnx` was taken.
        val r = Vnx.readContainer(zip("first.vnx" to doc, "second.vnx" to """{"vnx":"0.2.0"}"""))
        assertEquals("In a container", r.document.waypoints!![0].name)
        assertEquals(listOf("second.vnx"), r.attachments!!.map { it.path })
    }

    @Test
    fun `the root-document match is on the full entry name, not the base name`() {
        // Section 7's note: an implementation matching on the base name alone
        // matches at any depth and lets the last match win rather than the first.
        val r = Vnx.readContainer(zip("first.vnx" to doc, "sub/doc.vnx" to """{"vnx":"0.2.0"}"""))
        assertEquals(
            "In a container", r.document.waypoints!![0].name,
            "sub/doc.vnx is not a root document, so the fallback takes the first .vnx entry",
        )
    }

    @Test
    fun `names are compared case-insensitively`() {
        val r = Vnx.readContainer(zip("aaa.vnx" to """{"vnx":"0.2.0"}""", "DOC.VNX" to doc))
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
        // The BYTES, not the name: a stored entry is the one shape where a
        // reader that never decompresses anything still returns the right length.
        assertEquals(listOf(Attachment("notes.txt", "hello".toByteArray())), r.attachments)
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
        assertEquals(listOf("..hidden", "a..b/photo.jpg"), r.attachments!!.map { it.path }.sorted())
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
     * is well-formed — so they are built here. The library must not crash on one:
     * the sizes and the CRC come from the archive, so an archive that is hostile
     * or merely broken can declare anything it likes about them.
     *
     * WHAT IT DOES INSTEAD depends on which entry carries the lie, which is
     * section 7's own distinction. These fixtures carry ONE entry and it is the
     * root document, so every one of them is refused; the attachment side of the
     * same faults is below, and the archives there carry two entries.
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
        val doc = "{\n  \"vnx\": \"0.2.0\"\n}\n".toByteArray()
        val e = assertFailsWith<VnxRefusal> {
            Vnx.readContainer(
                lying(body = doc, method = 8, payload = deflated(doc), declaredUncompressed = 0x7FFFFFF0L),
            )
        }
        assertEquals(RefusalCause.CONTAINER_ROOT_UNREADABLE, e.refusal)
    }

    @Test
    fun `a stored entry whose two declared sizes differ is read, not refused`() {
        // THIS TEST ONCE ASSERTED THE OPPOSITE. Comparing the two declared
        // fields is the natural thing to write — for a stored entry they are the
        // same quantity, so a disagreement looks like a truncated entry being
        // read as whole — and this reader did it and refused
        // accept/container-sizes-disagree.vnz. Section 7 settles it the other
        // way: a size field is not the authority on what an entry contains, the
        // checksum is, so a disagreement is neither a refusal nor a drop where
        // the CRC over the bytes present agrees.
        //
        // The declared uncompressed size here is absurd rather than merely
        // wrong, so nothing can quietly treat it as a length: the bytes present
        // are the whole payload and their CRC is correct.
        val doc = "{\n  \"vnx\": \"0.2.0\"\n}\n".toByteArray()
        val r = Vnx.readContainer(lying(body = doc, method = 0, declaredUncompressed = 0xFFFFFFFEL))
        // The document is checked for CONTENT, because a reader returning an
        // empty one would satisfy "did not throw" on its own.
        assertEquals("0.2.0", r.document.vnx)
        // Section 7 permits a diagnostic and does not require one, and the corpus
        // case expects none, so none is raised.
        assertEquals(emptyList(), r.diagnostics)
    }

    @Test
    fun `a stored entry declaring fewer bytes than it holds fails its CRC`() {
        // THIS IS THE GUARD THE DELETED SIZE COMPARISON WAS CREDITED WITH. A
        // short declaration would otherwise read a truncated entry as whole, and
        // removing the comparison did not open that door: the declared compressed
        // size bounds the read, so one byte fewer than the entry holds is taken
        // and its checksum does not match the one the archive states. Section 7's
        // prohibition does not reach here — it holds only where the CRC over the
        // bytes read agrees, and here it does not.
        val doc = "{\n  \"vnx\": \"0.2.0\"\n}\n".toByteArray()
        val e = assertFailsWith<VnxRefusal> {
            Vnx.readContainer(lying(body = doc, method = 0, declaredCompressed = doc.size - 1L))
        }
        assertEquals(RefusalCause.CONTAINER_ROOT_UNREADABLE, e.refusal)
        // The CRC message, not any refusal: a refusal for another reason would
        // not show that the CRC is what caught the truncation.
        assertTrue(e.message.contains("CRC"), "the refusal must name what failed: ${e.message}")
    }

    @Test
    fun `a truncated deflate stream is refused rather than throwing`() {
        val doc = "{\n  \"vnx\": \"0.2.0\"\n}\n".toByteArray()
        val full = deflated(doc)
        val half = full.copyOfRange(0, full.size / 2)
        val e = assertFailsWith<VnxRefusal> {
            Vnx.readContainer(lying(body = doc, method = 8, payload = half, declaredCompressed = half.size.toLong()))
        }
        assertEquals(RefusalCause.CONTAINER_ROOT_UNREADABLE, e.refusal)
    }

    @Test
    fun `a ROOT DOCUMENT whose bytes fail their CRC is refused`() {
        // Nothing in section 7 requires the CHECK, and a reader that skips it
        // accepts corrupt data in silence — which for a format whose whole
        // argument is that two libraries agree byte for byte is the wrong way to
        // be lenient. What section 7 does require is the REMEDY, and for the root
        // document it is a refusal: there is no document to return.
        val doc = "{\n  \"vnx\": \"0.2.0\"\n}\n".toByteArray()
        val e = assertFailsWith<VnxRefusal> {
            Vnx.readContainer(lying(body = doc, method = 0, crc = 0xDEADBEEFL))
        }
        assertEquals(RefusalCause.CONTAINER_ROOT_UNREADABLE, e.refusal)
        assertTrue(e.message.contains("CRC"), "the refusal must name what failed: ${e.message}")
    }

    // ------------------------------------ a damaged ATTACHMENT, which is dropped

    /**
     * An archive of two entries: an intact `doc.vnx`, and one attachment built
     * to a caller's specification so it can be damaged.
     *
     * Two entries rather than one is the whole point. Every fixture above
     * carries the lie on the root document, where section 7's remedy is a
     * refusal; the same fault on an attachment must drop that attachment and
     * return the document, and nothing proves the difference without an archive
     * that holds both.
     */
    private fun archiveWithDamaged(
        attachment: String = "attachments/photo.txt",
        body: ByteArray,
        method: Int,
        payload: ByteArray = body,
        declaredCompressed: Long = payload.size.toLong(),
        declaredUncompressed: Long = body.size.toLong(),
        crc: Long = java.util.zip.CRC32().apply { update(body) }.value,
    ): ByteArray = twoEntries(
        Raw("doc.vnx", doc.toByteArray(), 0, doc.toByteArray(), doc.length.toLong(), doc.length.toLong(),
            java.util.zip.CRC32().apply { update(doc.toByteArray()) }.value),
        Raw(attachment, body, method, payload, declaredCompressed, declaredUncompressed, crc),
    )

    private class Raw(
        val name: String,
        val body: ByteArray,
        val method: Int,
        val payload: ByteArray,
        val declaredCompressed: Long,
        val declaredUncompressed: Long,
        val crc: Long,
    )

    /** Two entries, written by hand so that either one's header may lie. */
    private fun twoEntries(vararg raws: Raw): ByteArray {
        val out = java.io.ByteArrayOutputStream()
        fun le16(o: java.io.ByteArrayOutputStream, v: Int) { o.write(v and 0xff); o.write((v ushr 8) and 0xff) }
        fun le32(o: java.io.ByteArrayOutputStream, v: Long) {
            le16(o, (v and 0xffff).toInt()); le16(o, ((v ushr 16) and 0xffff).toInt())
        }
        val offsets = ArrayList<Long>()
        for (raw in raws) {
            offsets.add(out.size().toLong())
            val n = raw.name.toByteArray(Charsets.UTF_8)
            le32(out, 0x04034b50); le16(out, 10); le16(out, 0x800); le16(out, raw.method); le16(out, 0); le16(out, 0x21)
            le32(out, raw.crc); le32(out, raw.declaredCompressed); le32(out, raw.declaredUncompressed)
            le16(out, n.size); le16(out, 0)
            out.write(n); out.write(raw.payload)
        }
        val central = java.io.ByteArrayOutputStream()
        for ((i, raw) in raws.withIndex()) {
            val n = raw.name.toByteArray(Charsets.UTF_8)
            le32(central, 0x02014b50); le16(central, 10); le16(central, 10); le16(central, 0x800)
            le16(central, raw.method); le16(central, 0); le16(central, 0x21)
            le32(central, raw.crc); le32(central, raw.declaredCompressed); le32(central, raw.declaredUncompressed)
            le16(central, n.size); le16(central, 0); le16(central, 0); le16(central, 0); le16(central, 0)
            le32(central, 0); le32(central, offsets[i])
            central.write(n)
        }
        val directoryOffset = out.size().toLong()
        val cd = central.toByteArray()
        out.write(cd)
        le32(out, 0x06054b50); le16(out, 0); le16(out, 0); le16(out, raws.size); le16(out, raws.size)
        le32(out, cd.size.toLong()); le32(out, directoryOffset); le16(out, 0)
        return out.toByteArray()
    }

    /** The document read back, with the one attachment dropped and reported. */
    private fun assertDropped(bytes: ByteArray, why: String) {
        val r = Vnx.readContainer(bytes)
        // The document is untouched by damage to an attachment, and section 7
        // says so: refusing it discards good geometry over a damaged photograph.
        assertEquals("In a container", r.document.waypoints!![0].name, why)
        // DROPPED, not returned empty: an attachment with no bytes cannot be told
        // apart from one the archive never carried.
        assertEquals(emptyList(), r.attachments, why)
        assertEquals(1, r.diagnostics.size, "$why: one diagnostic, for the one entry")
        assertEquals(DiagnosticCause.ATTACHMENT_UNREADABLE, r.diagnostics[0].cause, why)
        assertEquals("#/entries/attachments~1photo.txt", r.diagnostics[0].path, why)
    }

    @Test
    fun `an attachment that fails its CRC is dropped and reported, not refused`() {
        val body = "a photograph, or it was".toByteArray()
        assertDropped(
            archiveWithDamaged(body = body, method = 0, crc = 0xDEADBEEFL),
            "a stored attachment whose CRC disagrees",
        )
    }

    @Test
    fun `an attachment whose deflate stream will not inflate is dropped and reported`() {
        val body = "a photograph, or it was".toByteArray()
        val full = deflated(body)
        val half = full.copyOfRange(0, full.size / 2)
        assertDropped(
            archiveWithDamaged(body = body, method = 8, payload = half, declaredCompressed = half.size.toLong()),
            "a truncated deflate stream",
        )
    }

    @Test
    fun `a damaged attachment does not take an intact one with it`() {
        // The drop must be scoped to the entry that is damaged. A reader that
        // abandoned the archive at the first fault would return the document and
        // no attachments at all, and would pass the two tests above.
        val good = "this one is fine\n".toByteArray()
        val bad = "a photograph, or it was".toByteArray()
        val bytes = twoEntries(
            Raw("doc.vnx", doc.toByteArray(), 0, doc.toByteArray(), doc.length.toLong(), doc.length.toLong(),
                java.util.zip.CRC32().apply { update(doc.toByteArray()) }.value),
            Raw("attachments/photo.txt", bad, 0, bad, bad.size.toLong(), bad.size.toLong(), 0xDEADBEEFL),
            Raw("notes/kept.txt", good, 0, good, good.size.toLong(), good.size.toLong(),
                java.util.zip.CRC32().apply { update(good) }.value),
        )
        val r = Vnx.readContainer(bytes)
        assertEquals(listOf(Attachment("notes/kept.txt", good)), r.attachments)
        assertEquals(1, r.diagnostics.size)
        assertEquals("#/entries/attachments~1photo.txt", r.diagnostics[0].path)
    }

    @Test
    fun `bytes that are not a ZIP archive are refused, not read`() {
        val e = assertFailsWith<VnxRefusal> { Vnx.readContainer("not a zip at all".toByteArray()) }
        assertEquals(RefusalCause.CONTAINER_MALFORMED, e.refusal)
    }

    @Test
    fun `a fault in the document inside a container is the document's fault, reported as such`() {
        val e = assertFailsWith<VnxRefusal> { Vnx.readContainer(zip("doc.vnx" to """{"vnx":"9.9.9"}""")) }
        assertEquals(RefusalCause.VERSION_SERIES_UNSUPPORTED, e.refusal)
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
