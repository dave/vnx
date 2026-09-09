package app.viatrail.vnx

import java.util.zip.CRC32
import java.util.zip.Inflater

/**
 * A minimal ZIP reader, written here rather than taken from `java.util.zip`.
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

        val entries = ArrayList<Entry>()
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
                val extra = readZip64Extra(
                    data, p + 46 + nameLength, extraLength,
                    uncompressedSize == 0xffffffffL,
                    compressedSize == 0xffffffffL,
                    localOffset == 0xffffffffL,
                )
                extra.uncompressed?.let { uncompressedSize = it }
                extra.compressed?.let { compressedSize = it }
                extra.localOffset?.let { localOffset = it }
            }

            entries.add(readEntry(data, name, method, localOffset, compressedSize, uncompressedSize))
            p += 46 + nameLength + extraLength + commentLength
        }
        return entries
    }

    private fun readEntry(
        data: ByteArray,
        name: String,
        method: Int,
        localOffset: Long,
        compressedSize: Long,
        uncompressedSize: Long,
    ): Entry {
        if (method != METHOD_STORED && method != METHOD_DEFLATE) {
            // Section 7: compression MUST be stored or deflate, because other
            // methods are not available in every target language. Whether that
            // refuses the archive or drops one attachment depends on WHICH entry
            // it is, so the decision is not taken here.
            return Entry(name, method, null, "compression method $method is neither stored nor deflate")
        }
        val start = localOffset.toInt()
        if (start < 0 || start + 30 > data.size || u32(data, start) != LOCAL_SIGNATURE.toLong()) {
            malformed("the local header for \"$name\" is missing or truncated")
        }
        val nameLength = u16(data, start + 26)
        val extraLength = u16(data, start + 28)
        val dataStart = start + 30 + nameLength + extraLength
        val dataEnd = dataStart + compressedSize.toInt()
        if (dataStart < 0 || dataEnd > data.size) malformed("the data for \"$name\" runs past the end of the archive")

        val raw = data.copyOfRange(dataStart, dataEnd)
        if (method == METHOD_STORED) return Entry(name, method, raw, null)

        val inflater = Inflater(true)
        try {
            inflater.setInput(raw)
            val out = ByteArray(uncompressedSize.toInt())
            var written = 0
            while (written < out.size) {
                val n = inflater.inflate(out, written, out.size - written)
                if (n == 0) {
                    if (inflater.finished() || inflater.needsInput()) break
                }
                written += n
            }
            if (written != out.size) malformed("\"$name\" did not inflate to its declared size")
            return Entry(name, method, out, null)
        } catch (e: java.util.zip.DataFormatException) {
            malformed("\"$name\" is not valid deflate data: ${e.message}")
        } finally {
            inflater.end()
        }
    }

    private class Zip64Extra(val uncompressed: Long?, val compressed: Long?, val localOffset: Long?)

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
                if (wantUncompressed && q + 8 <= end) { uncompressed = u64(data, q); q += 8 }
                if (wantCompressed && q + 8 <= end) { compressed = u64(data, q); q += 8 }
                if (wantOffset && q + 8 <= end) { localOffset = u64(data, q) }
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
    private fun decodeName(data: ByteArray, offset: Int, length: Int, flags: Int): String {
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

    private fun malformed(why: String): Nothing =
        refuse(RefusalCause.CONTAINER_MALFORMED, "the container could not be read: $why")

    private fun u16(b: ByteArray, i: Int): Int {
        if (i + 2 > b.size) malformed("a header runs past the end of the archive")
        return (b[i].toInt() and 0xff) or ((b[i + 1].toInt() and 0xff) shl 8)
    }

    private fun u32(b: ByteArray, i: Int): Long {
        if (i < 0 || i + 4 > b.size) malformed("a header runs past the end of the archive")
        return (b[i].toLong() and 0xff) or
            ((b[i + 1].toLong() and 0xff) shl 8) or
            ((b[i + 2].toLong() and 0xff) shl 16) or
            ((b[i + 3].toLong() and 0xff) shl 24)
    }

    private fun u64(b: ByteArray, i: Int): Long {
        if (i < 0 || i + 8 > b.size) malformed("a header runs past the end of the archive")
        var v = 0L
        for (k in 7 downTo 0) v = (v shl 8) or (b[i + k].toLong() and 0xff)
        return v
    }

    // ------------------------------------------------------------- writing

    /** One entry to write, in the order it will appear in the archive. */
    class ToWrite(val name: String, val body: ByteArray)

    /**
     * Writes a ZIP archive: local headers, then the central directory, then the
     * end-of-central-directory record.
     *
     * Deterministic. Every entry gets the same fixed timestamp, so the same
     * document and attachments produce the same bytes every time. SPEC.md does
     * not require that of a container the way section 4.5 requires it of a
     * document, but a format whose entire purpose is that two libraries emit
     * identical files should not lose the property at the container boundary
     * over a clock reading. Raised with Dave rather than assumed.
     *
     * Deflate, with the UTF-8 filename flag set, and no ZIP64: section 7 requires
     * a reader to understand ZIP64 and explicitly allows a writer to omit it, so
     * a small export stays readable by the widest range of tools.
     */
    fun write(entries: List<ToWrite>): ByteArray {
        val out = java.io.ByteArrayOutputStream()
        val directory = java.io.ByteArrayOutputStream()
        var count = 0

        for (entry in entries) {
            val name = entry.name.toByteArray(Charsets.UTF_8)
            val crc = CRC32().apply { update(entry.body) }.value
            // STORED, always. Not a size heuristic and not deflate.
            //
            // Deflate has no canonical output. Measured 2026-09-09: for the very
            // document inside `corpus/accept/container-root-doc.vnz`, Go's
            // compress/flate emits 116 bytes and zlib emits 108, and NO zlib
            // level from 0 to 9 reproduces the committed bytes. Java and Python
            // agree with each other because both are zlib; Go is a clean-room
            // implementation and agrees with neither. So a deflated archive is
            // byte-reproducible only by the language that wrote it, whatever a
            // writer pins in its headers — which would make a container case in
            // the `format/` tier impossible, since that tier compares bytes.
            //
            // Stored has no freedom at all. It also costs an export almost
            // nothing in practice: attachments are photographs, which are already
            // compressed.
            //
            // A reader still accepts deflate; section 7 requires that, and this is
            // the same asymmetry canonical form has for a document — required of
            // what this library writes, not of what it will read.
            val payload = entry.body
            val method = METHOD_STORED
            val localOffset = out.size()

            out.writeLE32(LOCAL_SIGNATURE)
            out.writeLE16(10) // version needed: 1.0, stored
            out.writeLE16(0x800) // the UTF-8 filename flag of section 7
            out.writeLE16(method)
            out.writeLE16(FIXED_TIME)
            out.writeLE16(FIXED_DATE)
            out.writeLE32(crc.toInt())
            out.writeLE32(payload.size)
            out.writeLE32(entry.body.size)
            out.writeLE16(name.size)
            out.writeLE16(0) // no extra field
            out.write(name)
            out.write(payload)

            directory.writeLE32(CENTRAL_SIGNATURE)
            directory.writeLE16(10) // version made by
            directory.writeLE16(10) // version needed
            directory.writeLE16(0x800)
            directory.writeLE16(method)
            directory.writeLE16(FIXED_TIME)
            directory.writeLE16(FIXED_DATE)
            directory.writeLE32(crc.toInt())
            directory.writeLE32(payload.size)
            directory.writeLE32(entry.body.size)
            directory.writeLE16(name.size)
            directory.writeLE16(0) // extra
            directory.writeLE16(0) // comment
            directory.writeLE16(0) // disk number
            directory.writeLE16(0) // internal attributes
            directory.writeLE32(0) // external attributes
            directory.writeLE32(localOffset)
            directory.write(name)
            count++
        }

        val directoryOffset = out.size()
        val directoryBytes = directory.toByteArray()
        out.write(directoryBytes)
        out.writeLE32(EOCD_SIGNATURE)
        out.writeLE16(0) // this disk
        out.writeLE16(0) // disk with the central directory
        out.writeLE16(count)
        out.writeLE16(count)
        out.writeLE32(directoryBytes.size)
        out.writeLE32(directoryOffset)
        out.writeLE16(0) // no archive comment
        return out.toByteArray()
    }

    /** MS-DOS 1980-01-01 00:00:00, so the bytes do not depend on when they were written. */
    private const val FIXED_TIME = 0
    private const val FIXED_DATE = 0x21

    private fun java.io.ByteArrayOutputStream.writeLE16(v: Int) {
        write(v and 0xff)
        write((v ushr 8) and 0xff)
    }

    private fun java.io.ByteArrayOutputStream.writeLE32(v: Int) {
        write(v and 0xff)
        write((v ushr 8) and 0xff)
        write((v ushr 16) and 0xff)
        write((v ushr 24) and 0xff)
    }
}
