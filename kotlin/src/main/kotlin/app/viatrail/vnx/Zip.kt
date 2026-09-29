package app.viatrail.vnx

import java.util.zip.CRC32
import java.util.zip.Inflater

/**
 * A minimal ZIP READER, written here rather than taken from `java.util.zip`.
 * The writer below is `java.util.zip`; only the reader needs its own.
 *
 * SPEC.md section 7 asks questions of an archive that the JDK's own reader
 * answers on its own terms rather than the format's: it must see BOTH entries of
 * a duplicated name in archive order, it must inspect an entry's compression
 * method without reading the entry, and it must treat an unusable compression
 * method as a per-entry fact rather than as a stream that has stopped.
 * `ZipInputStream` throws on the first unreadable entry and takes the rest of the
 * archive with it, and `ZipFile` resolves a duplicated name before a caller can
 * see it. Both would decide, silently and differently between JDK versions,
 * questions the spec answers explicitly.
 *
 * Sizes and offsets come from the central directory, which is authoritative even
 * where a local header was written with a data descriptor. ZIP64 is read;
 * section 7 requires that of a reader and does not require it of a writer.
 */
internal object Zip {
    data class Entry(
        val name: String,
        /** The raw ZIP compression method: 0 stored, 8 deflate, anything else unusable. */
        val method: Int,
        private val bytes: ByteArray?,
        private val failure: String?,
    ) {
        val isUsable: Boolean get() = bytes != null
        val unusableBecause: String get() = failure ?: "readable"

        fun bytes(): ByteArray = bytes ?: throw IllegalStateException("entry $name is not usable: $failure")
    }

    /**
     * One ENTRY's payload could not be recovered: a stream that will not
     * decompress, or a CRC that disagrees with the bytes present.
     *
     * NOT a refusal, and that is section 7's distinction rather than a
     * convenience. Whether such an entry refuses the archive or drops one
     * attachment depends on WHICH entry it is, and only the container reader
     * knows that. Carrying the fact out as a failed [Entry] leaves the decision
     * where section 7 puts it — exactly as an unusable compression method
     * already did.
     *
     * It is an exception rather than a return type because the failure is found
     * several frames down, inside the inflater loop, and threading a nullable
     * back through would put a branch at every level that has nothing to decide.
     */
    private class Damaged(
        val why: String,
    ) : Exception(why)

    private fun damaged(why: String): Nothing = throw Damaged(why)

    private const val EOCD_SIGNATURE = 0x06054b50
    private const val ZIP64_LOCATOR_SIGNATURE = 0x07064b50
    private const val ZIP64_EOCD_SIGNATURE = 0x06064b50
    private const val CENTRAL_SIGNATURE = 0x02014b50
    private const val LOCAL_SIGNATURE = 0x04034b50
    private const val METHOD_STORED = 0
    private const val METHOD_DEFLATE = 8

    /** Reads every entry in central-directory order, duplicates included. */
    fun read(data: ByteArray): List<Entry> {
        val eocd = findEndOfCentralDirectory(data)
        var entryCount = u16(data, eocd + 10).toLong()
        var directoryOffset = u32(data, eocd + 16)

        // ZIP64: the 32-bit fields are saturated and the real ones live in the
        // ZIP64 end-of-central-directory record the locator points at.
        if (entryCount == 0xffffL || directoryOffset == 0xffffffffL) {
            val locator = eocd - 20
            if (locator < 0 || u32(data, locator) != ZIP64_LOCATOR_SIGNATURE.toLong()) {
                malformed("the archive saturates a 32-bit field but carries no ZIP64 locator")
            }
            val zip64 = u64(data, locator + 8).toInt()
            if (zip64 < 0 || zip64 + 56 > data.size || u32(data, zip64) != ZIP64_EOCD_SIGNATURE.toLong()) {
                malformed("the ZIP64 locator does not point at a ZIP64 end-of-central-directory record")
            }
            entryCount = u64(data, zip64 + 32)
            directoryOffset = u64(data, zip64 + 48)
        }

        val headers = ArrayList<Header>()
        var p = directoryOffset.toInt()
        for (i in 0 until entryCount) {
            if (p + 46 > data.size || u32(data, p) != CENTRAL_SIGNATURE.toLong()) {
                malformed("central directory entry ${i + 1} is truncated or has a bad signature")
            }
            val flags = u16(data, p + 8)
            val method = u16(data, p + 10)
            var compressedSize = u32(data, p + 20)
            var uncompressedSize = u32(data, p + 24)
            val nameLength = u16(data, p + 28)
            val extraLength = u16(data, p + 30)
            val commentLength = u16(data, p + 32)
            var localOffset = u32(data, p + 42)
            val name = decodeName(data, p + 46, nameLength, flags)

            if (uncompressedSize == 0xffffffffL || compressedSize == 0xffffffffL || localOffset == 0xffffffffL) {
                val extra =
                    readZip64Extra(
                        data,
                        p + 46 + nameLength,
                        extraLength,
                        uncompressedSize == 0xffffffffL,
                        compressedSize == 0xffffffffL,
                        localOffset == 0xffffffffL,
                    )
                extra.uncompressed?.let { uncompressedSize = it }
                extra.compressed?.let { compressedSize = it }
                extra.localOffset?.let { localOffset = it }
            }

            headers.add(Header(name, method, localOffset, compressedSize, uncompressedSize, u32(data, p + 16)))
            p += 46 + nameLength + extraLength + commentLength
        }

        // The bound on each payload takes every entry to know and every entry
        // needs it, so it is computed once the directory is complete.
        return headers.map { header ->
            val following = headers.map { it.localOffset }.filter { it > header.localOffset }.minOrNull()
            readEntry(data, header, following ?: directoryOffset)
        }
    }

    /** One entry as the central directory states it, before its payload is read. */
    private data class Header(
        val name: String,
        val method: Int,
        val localOffset: Long,
        val compressedSize: Long,
        val uncompressedSize: Long,
        val crc: Long,
    )

    /**
     * One entry's bytes, verified against its CRC.
     *
     * [payloadBound] is where the payload cannot extend past: the nearest
     * following local header, or the central directory where there is none.
     *
     * THE DECLARED SIZE IS A CEILING, NOT A LENGTH, which is section 7's size
     * rule. An entry declaring more bytes than it holds is read as the bytes
     * that are there and the CHECKSUM decides whether that was the whole
     * payload; reading the declared count instead runs into the next local
     * header and fails the CRC on a conforming archive. A SHORT declaration
     * needs nothing of its own — the same CRC catches it.
     *
     * [compressedSize] clamped to the bound is therefore the WHOLE bound on a
     * stored entry's read. The declared uncompressed size is consulted only by
     * [inflate], and the two declared sizes are never compared with each other:
     * section 7 forbids refusing or dropping an entry because they disagree.
     */
    private fun readEntry(
        data: ByteArray,
        header: Header,
        payloadBound: Long,
    ): Entry {
        val name = header.name
        val method = header.method
        val uncompressedSize = header.uncompressedSize
        if (method != METHOD_STORED && method != METHOD_DEFLATE) {
            // Section 7: compression MUST be stored or deflate, because other
            // methods are not available in every target language. Whether that
            // refuses the archive or drops one attachment depends on WHICH entry
            // it is, so the decision is not taken here.
            return Entry(name, method, null, "compression method $method is neither stored nor deflate")
        }
        val start = header.localOffset.toInt()
        if (start < 0 || start + 30 > data.size || u32(data, start) != LOCAL_SIGNATURE.toLong()) {
            malformed("the local header for \"$name\" is missing or truncated")
        }
        val nameLength = u16(data, start + 26)
        val extraLength = u16(data, start + 28)
        val dataStart = start + 30 + nameLength + extraLength
        if (dataStart < 0 || dataStart > payloadBound) {
            malformed("the data for \"$name\" starts past where its payload must end")
        }
        val compressedSize = minOf(header.compressedSize, payloadBound - dataStart)
        val dataEnd = dataStart + compressedSize.toInt()
        if (dataEnd > data.size) malformed("the data for \"$name\" runs past the end of the archive")

        val raw = data.copyOfRange(dataStart, dataEnd)
        // Section 7: an entry whose method is stored or deflate but whose bytes
        // cannot be recovered takes the SAME remedy as an unusable method — the
        // reader drops an attachment and reports it, and refuses only where the
        // damaged entry is the root document. So this is a per-entry fact like
        // the method above, and not a refusal taken here.
        //
        // The CRC check is what DETECTS the damage and it stays. Section 7 does
        // not require a reader to look; it requires one that has looked to
        // report rather than to return the bytes it found. Only the remedy moved.
        return try {
            Entry(name, method, recover(method, raw, header, uncompressedSize), null)
        } catch (e: Damaged) {
            Entry(name, method, null, e.why)
        }
    }

    /**
     * One entry's payload, decompressed and checked against its CRC, or
     * [Damaged].
     *
     * Every message here is written to follow "the attachment X is dropped:" and
     * "the root document X cannot be read:", so it starts at "it" and never
     * repeats the entry name. The caller knows which entry it asked about.
     */
    private fun recover(
        method: Int,
        raw: ByteArray,
        header: Header,
        uncompressedSize: Long,
    ): ByteArray {
        val body =
            if (method == METHOD_STORED) {
                // NOTHING COMPARES THE TWO DECLARED SIZES. For a stored entry they are
                // the same quantity, so a disagreement is a defect in the archive —
                // but section 7 forbids refusing an entry on that ground where the CRC
                // over the bytes present agrees, and forbids dropping it. A size field
                // is not the authority on what an entry contains; the checksum is.
                //
                // So `raw` is already the whole of a stored read: readEntry took the
                // declared compressed size clamped to the room left before the
                // payload bound, and that is all that bounds it. The declared
                // uncompressed size bounds nothing here — a stored entry has no
                // decompression for it to limit — and the CRC below decides whether
                // the bytes taken were the whole payload.
                raw
            } else {
                inflate(raw, uncompressedSize)
            }

        // Verify the CRC. Nothing in section 7 requires this, and a reader that
        // skips it accepts corrupt data in silence — which for a format whose
        // whole argument is that two libraries must agree byte for byte is the
        // wrong way to be lenient. It also catches a header lying about a size
        // that happens to survive the checks above.
        val actual = CRC32().apply { update(body) }.value
        if (actual != header.crc) {
            damaged(
                "it fails its CRC: the archive says ${"%08x".format(header.crc)} " +
                    "and the bytes are ${"%08x".format(actual)}",
            )
        }
        return body
    }

    /**
     * Inflates into a growing buffer rather than into one sized from the header.
     *
     * The declared uncompressed size comes from the archive, and an archive that
     * is malformed or hostile can declare any size it likes. Allocating it up
     * front turns a twenty-byte file claiming two gigabytes into an
     * OutOfMemoryError — an `Error`, so it escapes every `catch` a caller is
     * likely to have written, and it takes the process rather than the document.
     * Measured 2026-09-10 against exactly that archive.
     *
     * So the declared size is used as a LIMIT rather than as an allocation, and
     * a stream that exceeds it or falls short of it is damage to that entry.
     * Section 9's
     * resource bound for a merely large HONEST archive is a separate matter and
     * this library still does not implement one.
     */
    private fun inflate(
        raw: ByteArray,
        declaredSize: Long,
    ): ByteArray {
        val inflater = Inflater(true)
        try {
            inflater.setInput(raw)
            val out = java.io.ByteArrayOutputStream(minOf(declaredSize, raw.size.toLong() * 4 + 1024).toInt())
            val chunk = ByteArray(16 * 1024)
            var written = 0L
            while (!inflater.finished()) {
                val n = inflater.inflate(chunk)
                if (n == 0 && (inflater.needsInput() || inflater.needsDictionary())) break
                written += n
                if (written > declaredSize) {
                    damaged("it inflates to more than the $declaredSize bytes its header declares")
                }
                out.write(chunk, 0, n)
            }
            if (written != declaredSize) {
                damaged("it inflated to $written bytes and its header declares $declaredSize")
            }
            return out.toByteArray()
        } catch (e: java.util.zip.DataFormatException) {
            damaged("it is not valid deflate data: ${e.message}")
        } finally {
            inflater.end()
        }
    }

    private class Zip64Extra(
        val uncompressed: Long?,
        val compressed: Long?,
        val localOffset: Long?,
    )

    private fun readZip64Extra(
        data: ByteArray,
        offset: Int,
        length: Int,
        wantUncompressed: Boolean,
        wantCompressed: Boolean,
        wantOffset: Boolean,
    ): Zip64Extra {
        var p = offset
        val end = offset + length
        while (p + 4 <= end) {
            val id = u16(data, p)
            val size = u16(data, p + 2)
            if (id == 0x0001) {
                var q = p + 4
                var uncompressed: Long? = null
                var compressed: Long? = null
                var localOffset: Long? = null
                // The ZIP64 extra carries only the fields that were saturated, in
                // this order and with no tags of their own.
                if (wantUncompressed && q + 8 <= end) {
                    uncompressed = u64(data, q)
                    q += 8
                }
                if (wantCompressed && q + 8 <= end) {
                    compressed = u64(data, q)
                    q += 8
                }
                if (wantOffset && q + 8 <= end) {
                    localOffset = u64(data, q)
                }
                return Zip64Extra(uncompressed, compressed, localOffset)
            }
            p += 4 + size
        }
        malformed("a ZIP64 field is saturated but the entry carries no ZIP64 extra field")
    }

    /**
     * Section 7: the UTF-8 filename flag is set. An archive that does not set it
     * is refused rather than decoded by guesswork — the historical alternative is
     * CP437, and guessing between the two silently changes an attachment's name.
     */
    private fun decodeName(
        data: ByteArray,
        offset: Int,
        length: Int,
        flags: Int,
    ): String {
        if (flags and 0x800 == 0) {
            // Names that are pure ASCII mean the same thing under either
            // encoding, so only a non-ASCII name is ambiguous.
            for (i in offset until offset + length) {
                if (data[i].toInt() and 0x80 != 0) {
                    malformed("an entry name is not ASCII and the archive does not set the UTF-8 filename flag")
                }
            }
        }
        return String(data, offset, length, Charsets.UTF_8)
    }

    private fun findEndOfCentralDirectory(data: ByteArray): Int {
        // The record is 22 bytes plus a comment of up to 65535.
        val earliest = maxOf(0, data.size - 22 - 0xffff)
        var p = data.size - 22
        while (p >= earliest) {
            if (u32(data, p) == EOCD_SIGNATURE.toLong()) return p
            p--
        }
        malformed("no end-of-central-directory record, so this is not a ZIP archive")
    }

    private fun malformed(why: String): Nothing = refuse(RefusalCause.CONTAINER_MALFORMED, "the container could not be read: $why")

    private fun u16(
        b: ByteArray,
        i: Int,
    ): Int {
        if (i + 2 > b.size) malformed("a header runs past the end of the archive")
        return (b[i].toInt() and 0xff) or ((b[i + 1].toInt() and 0xff) shl 8)
    }

    private fun u32(
        b: ByteArray,
        i: Int,
    ): Long {
        if (i < 0 || i + 4 > b.size) malformed("a header runs past the end of the archive")
        return (b[i].toLong() and 0xff) or
            ((b[i + 1].toLong() and 0xff) shl 8) or
            ((b[i + 2].toLong() and 0xff) shl 16) or
            ((b[i + 3].toLong() and 0xff) shl 24)
    }

    private fun u64(
        b: ByteArray,
        i: Int,
    ): Long {
        if (i < 0 || i + 8 > b.size) malformed("a header runs past the end of the archive")
        var v = 0L
        for (k in 7 downTo 0) v = (v shl 8) or (b[i + k].toLong() and 0xff)
        return v
    }

    // ------------------------------------------------------------- writing

    /** One entry to write, in the order it will appear in the archive. */
    class ToWrite(
        val name: String,
        val body: ByteArray,
    )

    /**
     * Writes a ZIP archive with `java.util.zip`.
     *
     * DEFLATED. SPEC.md section 7.1 no longer constrains the bytes of an
     * archive, so the reason a writer had to store — that deflate has no
     * reproducible output and no two languages produce the same bytes for the same
     * input — is gone with the rule it served. What is left is that attachments
     * are photographs and a document is JSON, and JSON compresses.
     *
     * Deterministic. Every entry gets the same fixed local timestamp, so the
     * same document and attachments produce the same bytes every time. Nothing
     * requires that; it is cheap, and a writer whose output depends on the clock
     * is a writer nobody can diff. `setTimeLocal` rather than `setTime`, because
     * `setTime` converts through the default time zone and would put the machine
     * back into the bytes by another route.
     *
     * The name is UTF-8 and `ZipOutputStream` sets section 7.1's UTF-8 filename
     * flag for it. A deflated entry gets a data descriptor, which section 7.1
     * permits and which this library's reader never consults: sizes come from
     * the central directory, which is authoritative.
     *
     * Entry ORDER is the caller's, at Vnx.kt. Nothing here reorders anything.
     */
    fun write(entries: List<ToWrite>): ByteArray {
        val out = java.io.ByteArrayOutputStream()
        java.util.zip.ZipOutputStream(out, Charsets.UTF_8).use { zip ->
            zip.setMethod(java.util.zip.ZipOutputStream.DEFLATED)
            for (entry in entries) {
                val e = java.util.zip.ZipEntry(entry.name)
                e.setTimeLocal(FIXED_TIME)
                zip.putNextEntry(e)
                zip.write(entry.body)
                zip.closeEntry()
            }
        }
        return out.toByteArray()
    }

    /** 1980-01-01 00:00:00, the earliest an MS-DOS timestamp can state. */
    private val FIXED_TIME: java.time.LocalDateTime = java.time.LocalDateTime.of(1980, 1, 1, 0, 0, 0)
}
