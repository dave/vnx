package app.viatrail.vnx

import java.io.ByteArrayInputStream
import java.io.File
import java.util.zip.ZipInputStream
import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

/**
 * Writing a `.vnz` container (SPEC.md section 7).
 */
class ContainerWriteTest {

    private val fullPrecision = """{"vnx":"0.1.0","waypoints":[{"name":"Precise","at":[-3.6712345678901235,57.11812345678901]}]}"""

    private fun documentOf(json: String) = Vnx.readDocument(json).document

    @Test
    fun `the archive does not depend on the order the attachments were handed over`() {
        // ENTRY ORDER IS NOT A RULE. Section 7.1 states five rules and then says
        // every other field of the archive is the writer's own business, so only
        // the root document's place is fixed. What is asserted here is that the
        // SAME archive comes out whichever order a caller iterated its
        // attachments in — a property of this writer, so that an export is
        // diffable, and not a claim about conformance.
        val emoji = "\uD83D\uDE00.txt"
        val replacement = "\uFFFD.txt"
        val bodies = mapOf(
            "zzz/last.txt" to "z", emoji to "e", replacement to "r", "aaa/first.txt" to "a",
        )
        val one = LinkedHashMap<String, ByteArray>()
        for (name in bodies.keys) one[name] = bodies.getValue(name).toByteArray()
        val other = LinkedHashMap<String, ByteArray>()
        for (name in bodies.keys.reversed()) other[name] = bodies.getValue(name).toByteArray()

        val document = documentOf("""{"vnx":"0.1.0"}""")
        assertContentEquals(
            Vnx.writeContainer(document, one), Vnx.writeContainer(document, other),
            "the archive changed when the caller iterated its attachments the other way",
        )
        val bytes = Vnx.writeContainer(document, one)
        assertEquals("doc.vnx", rawEntries(bytes).first().name, "the root document goes first")
        assertEquals(
            bodies.keys.sorted(), Vnx.readContainer(bytes).attachments!!.sorted(),
            "every attachment must survive, whatever order it sits in",
        )
    }

    @Test
    fun `a container round-trips through this library's own reader`() {
        val document = documentOf("""{"vnx":"0.1.0","waypoints":[{"name":"Bothy","at":[-3.6712,57.118]}]}""")
        val bytes = Vnx.writeContainer(document, linkedMapOf("attachments/photo.txt" to "not a photo\n".toByteArray()))
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
        val bytes = Vnx.writeContainer(
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
    fun `the document inside the archive is the document this library writes`() {
        val document = documentOf(fullPrecision)
        val written = String(inner(Vnx.writeContainer(document)))
        assertTrue(written.contains("[-3.671235,57.118123]"), "a writer rounds: $written")
        assertEquals(Vnx.write(document), written)
    }

    @Test
    fun `an archive is smaller than the document it holds`() {
        // Section 7.1 lets a writer deflate, and this is the whole reason to.
        val document = documentOf(File(System.getProperty("vnx.corpus")!!, "accept/example-network.vnx").readText())
        val plain = Vnx.write(document).toByteArray(Charsets.UTF_8)
        val archive = Vnx.writeContainer(document)
        assertTrue(
            archive.size < plain.size,
            "the archive is ${archive.size} bytes and the document it holds is ${plain.size}",
        )
    }

    @Test
    fun `a container this library wrote is byte-identical every time`() {
        // A format whose whole purpose is that two libraries emit identical files
        // should not lose that at the container boundary over a clock reading.
        // SPEC.md does not require this; it is raised rather than assumed.
        val document = documentOf(fullPrecision)
        val once = Vnx.writeContainer(document, linkedMapOf("a.txt" to "hello".toByteArray()))
        Thread.sleep(5)
        val twice = Vnx.writeContainer(document, linkedMapOf("a.txt" to "hello".toByteArray()))
        assertContentEquals(once, twice)
    }

    @Test
    fun `a writer refuses every path its own reader would refuse`() {
        // A writer that can emit an archive its own reader rejects is a writer
        // with a hole in it.
        for (name in listOf(
            "../secret", "a/../../secret", "/etc/passwd", "C:/windows", "", "dir/",
            // The backslash ruling binds a writer for the same reason: this ZIP
            // writer exists so that it cannot emit an archive its own reader
            // refuses.
            "a\\b", "..\\x", "dir\\file.jpg",
        )) {
            val e = assertFailsWith<VnxRefusal>("\"$name\" must be refused") {
                Vnx.writeContainer(documentOf("""{"vnx":"0.1.0"}"""), linkedMapOf(name to "x".toByteArray()))
            }
            assertEquals(RefusalCause.CONTAINER_PATH_ESCAPES, e.refusal, "for \"$name\"")
        }
    }

    @Test
    fun `an attachment may not collide with the root document, case-insensitively`() {
        for (name in listOf("doc.vnx", "DOC.VNX", "Doc.Vnx")) {
            val e = assertFailsWith<VnxRefusal>("\"$name\" must collide") {
                Vnx.writeContainer(documentOf("""{"vnx":"0.1.0"}"""), linkedMapOf(name to "x".toByteArray()))
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
        val bytes = Vnx.writeContainer(documentOf("""{"vnx":"0.1.0"}"""), linkedMapOf(name to "x".toByteArray()))
        assertEquals(listOf(name), Vnx.readContainer(bytes).attachments)
    }

    @Test
    fun `an empty attachment and a large one both survive`() {
        val big = ByteArray(300_000) { (it * 31).toByte() }
        val bytes = Vnx.writeContainer(
            documentOf("""{"vnx":"0.1.0"}"""),
            linkedMapOf("empty.bin" to ByteArray(0), "big.bin" to big),
        )
        assertEquals(listOf("big.bin", "empty.bin"), Vnx.readContainer(bytes).attachments!!.sorted())
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

    /**
     * What SPEC.md section 7.1 still fixes, read RAW out of both headers.
     *
     * Not through `java.util.zip`, and this is the point rather than a
     * preference: every ZIP library in every language resolves an entry from the
     * CENTRAL DIRECTORY, on purpose. So no library-based reader can witness a
     * rule about a LOCAL header, and a local header that disagrees with its
     * central twin is invisible to it. A live VNX writer was found on 2026-09-09
     * zeroing the CRC and both sizes in its local headers, and the test written
     * to pin its known gaps could not see it, because that test read the output
     * back through a ZIP reader.
     *
     * Section 7.1 is now three rules — the UTF-8 filename flag, the root document
     * first, and no duplicate names — so most of what this test used to assert
     * has gone with the rules it was checking. Deflate brings the data-descriptor
     * bit with it, which 7.1 permits and which is why the crc and the two sizes
     * are read from the CENTRAL directory alone.
     */
    @Test
    fun `what section 7-1 still fixes, read raw from both headers`() {
        val bytes = Vnx.writeContainer(
            documentOf(fullPrecision),
            linkedMapOf("attachments/photo.txt" to "not a photo\n".toByteArray()),
        )
        val entries = rawEntries(bytes)
        assertEquals(2, entries.size)
        for (entry in entries) {
            assertEquals(
                0x0800, entry.local["flags"]!! and 0x0800,
                "${entry.name}: section 7.1 requires the UTF-8 filename flag",
            )
            assertEquals(8, entry.local["method"], "${entry.name}: DEFLATED")
            assertEquals(8, entry.central["method"], "${entry.name}: DEFLATED")
            assertEquals(0x0000, entry.local["dos_time"], "${entry.name}: 1980-01-01 00:00:00")
            assertEquals(0x0021, entry.local["dos_date"], "${entry.name}: 1980-01-01 00:00:00")

            // The assertion no ZIP library can make.
            for (field in listOf("flags", "method", "dos_time", "dos_date")) {
                assertEquals(
                    entry.local[field], entry.central[field],
                    "${entry.name}: local and central disagree on $field",
                )
            }
            assertTrue(entry.central["crc"]!! != 0, "${entry.name}: the central directory carries a real CRC")
            assertTrue(entry.central["usize"]!! > 0, "${entry.name}: the central directory carries a real size")
        }
        assertEquals("doc.vnx", entries[0].name, "the document goes first, so section 7's fallback finds it")
    }

    private class RawEntry(val name: String, val local: Map<String, Int>, val central: Map<String, Int>)

    /** A minimal raw reader. Twenty lines, and the only instrument that works. */
    private fun rawEntries(data: ByteArray): List<RawEntry> {
        fun u16(i: Int) = (data[i].toInt() and 0xff) or ((data[i + 1].toInt() and 0xff) shl 8)
        fun u32(i: Int) = u16(i) or (u16(i + 2) shl 16)

        var eocd = data.size - 22
        while (eocd >= 0 && u32(eocd) != 0x06054b50) eocd--
        check(eocd >= 0) { "no end-of-central-directory record" }
        val count = u16(eocd + 10)
        var p = u32(eocd + 16)
        val out = ArrayList<RawEntry>()
        repeat(count) {
            check(u32(p) == 0x02014b50) { "bad central signature" }
            val nameLen = u16(p + 28)
            val name = String(data, p + 46, nameLen, Charsets.UTF_8)
            val central = mapOf(
                "version_made_by_low" to (data[p + 4].toInt() and 0xff),
                "create_system" to (data[p + 5].toInt() and 0xff),
                "version_needed" to u16(p + 6), "flags" to u16(p + 8), "method" to u16(p + 10),
                "dos_time" to u16(p + 12), "dos_date" to u16(p + 14), "crc" to u32(p + 16),
                "csize" to u32(p + 20), "usize" to u32(p + 24), "comment_len" to u16(p + 32),
                "disk_start" to u16(p + 34), "internal_attr" to u16(p + 36), "external_attr" to u32(p + 38),
            )
            val offset = u32(p + 42)
            check(u32(offset) == 0x04034b50) { "bad local signature for $name" }
            val local = mapOf(
                "version_needed" to u16(offset + 4), "flags" to u16(offset + 6), "method" to u16(offset + 8),
                "dos_time" to u16(offset + 10), "dos_date" to u16(offset + 12), "crc" to u32(offset + 14),
                "csize" to u32(offset + 18), "usize" to u32(offset + 22), "extra_len" to u16(offset + 28),
            )
            out.add(RawEntry(name, local, central))
            p += 46 + nameLen + u16(p + 30) + u16(p + 32)
        }
        return out
    }

    @Test
    fun `a reader accepts the data-descriptor bit even though a writer must not set it`() {
        // Section 7.1 forbids a writer setting bit 3 and requires a reader to
        // accept it. java.util.zip's ZipOutputStream sets it, which makes it a
        // convenient source of exactly the archive this reader must tolerate.
        val out = java.io.ByteArrayOutputStream()
        java.util.zip.ZipOutputStream(out).use { zo ->
            zo.putNextEntry(java.util.zip.ZipEntry("doc.vnx"))
            zo.write(Vnx.write(documentOf("""{"vnx":"0.1.0","waypoints":[{"name":"Bit three","at":[-3,57]}]}""")).toByteArray())
            zo.closeEntry()
        }
        val result = Vnx.readContainer(out.toByteArray())
        assertEquals("Bit three", result.document.waypoints!![0].name)
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
