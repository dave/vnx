package app.viatrail.vnx

import java.io.File

/**
 * The VNX codec: reads and writes `.vnx` and `.vnz`, produces canonical form,
 * and reports diagnostics.
 *
 * That is the whole of what SPEC.md and its conformance corpus call a VNX
 * library. The GPX, KML and GeoJSON writers that live beside the reference
 * implementation are conversion tools, not part of the format, and are not here.
 */
object Vnx {

    /** The format version this library implements. */
    val version: Version get() = Version.SUPPORTED

    // -------------------------------------------------------------- reading

    /**
     * Reads a plain `.vnx` document.
     *
     * @throws VnxRefusal for one of the four causes SPEC.md section 6.2 permits.
     */
    fun readDocument(text: String): ReadResult = Decoder().decode(JsonParser.parse(text))

    /** Reads a plain `.vnx` document from UTF-8 bytes, tolerating a byte order mark. */
    fun readDocument(bytes: ByteArray): ReadResult = readDocument(decodeUtf8(bytes))

    /**
     * Reads a `.vnz` container: a ZIP archive carrying a VNX document and its
     * attachments (SPEC.md section 7).
     *
     * The returned [ReadResult.attachments] holds every entry that is not the
     * root document, in archive order.
     */
    fun readContainer(bytes: ByteArray): ReadResult {
        val entries = Zip.read(bytes)

        // Section 7: duplicate entry names MUST be refused, compared
        // case-insensitively. Language runtimes resolve a repeated name
        // differently, so one archive would otherwise yield different documents
        // in different languages — and that applies to `doc.vnx` beside
        // `DOC.VNX` exactly as it applies to an exact repeat.
        val seen = HashMap<String, String>()
        for (entry in entries) {
            val previous = seen.put(foldAscii(entry.name), entry.name)
            if (previous != null) {
                refuse(
                    RefusalCause.CONTAINER_DUPLICATE_ENTRY,
                    "the container carries a duplicate entry name: \"$previous\" and \"${entry.name}\" " +
                        "match when compared case-insensitively",
                )
            }
        }

        // Section 7 and section 9: an attachment path containing `..` or an
        // absolute path can direct an extractor to write outside its destination.
        // The check belongs at read time rather than being left to whoever
        // extracts later and may forget.
        for (entry in entries) checkPath(entry.name)

        val root = rootDocument(entries)
        if (!root.isUsable) {
            refuse(
                RefusalCause.CONTAINER_ROOT_COMPRESSION,
                "the root document \"${root.name}\" cannot be read: ${root.unusableBecause}",
            )
        }

        val result = readDocument(root.bytes())
        val diagnostics = ArrayList(result.diagnostics)
        val attachments = ArrayList<String>()
        for (entry in entries) {
            if (entry === root) continue
            if (!entry.isUsable) {
                // Refusing a whole library because one photograph used an unusual
                // method would be disproportionate (section 7).
                diagnostics.add(
                    Diagnostic(
                        "",
                        "the attachment \"${entry.name}\" is dropped: ${entry.unusableBecause}",
                        DiagnosticCause.ATTACHMENT_COMPRESSION_UNSUPPORTED,
                    ),
                )
                continue
            }
            attachments.add(entry.name)
        }
        return ReadResult(result.document, diagnostics, attachments)
    }

    /** Reads a `.vnx` or `.vnz` from disk, choosing by extension. */
    fun read(file: File): ReadResult =
        if (file.name.endsWith(".vnz", ignoreCase = true)) readContainer(file.readBytes())
        else readDocument(file.readBytes())

    // -------------------------------------------------------------- writing

    /**
     * RE-RENDERS a document in canonical form: the bytes for the document as it
     * was read, applying neither rounding nor position shortening, so arity and
     * precision survive (SPEC.md section 8.1).
     *
     * This is what the accept tier of the corpus requires, and it is not
     * [write].
     */
    fun render(document: VnxDocument): String = Canonical.render(Encoder(rounding = false).encode(document))

    /**
     * WRITES a document: canonical form with the emit-boundary rules applied —
     * coordinates rounded to 6 places and elevations to 1 (section 4.5.4), and
     * the shortest spelling of every position (section 4.1).
     *
     * A writer rounds, so writing what you read is legitimately not what you
     * read. Use [render] to reproduce a document you were handed.
     */
    fun write(document: VnxDocument): String = Canonical.render(Encoder(rounding = true).encode(document))

    /**
     * RE-RENDERS a document and its attachments as a `.vnz` container: the same
     * operation as [render], at the container boundary.
     *
     * The split matters here more than anywhere. Repackaging a container —
     * adding a photograph, renaming a folder — must not silently round every
     * coordinate in the document it is carrying, and a single container emit
     * path would do exactly that with nothing to show for it.
     *
     * The document is written as `doc.vnx`, the conventional name section 7 tells
     * a reader to prefer. Attachment paths are checked with the same rules a
     * reader applies, because a writer that can emit an archive its own reader
     * refuses is a writer with a hole in it.
     */
    fun renderContainer(document: VnxDocument, attachments: Map<String, ByteArray> = emptyMap()): ByteArray =
        container(render(document), attachments)

    /**
     * WRITES a document and its attachments as a `.vnz` container: coordinates
     * rounded and positions shortened, as [write] does.
     */
    fun writeContainer(document: VnxDocument, attachments: Map<String, ByteArray> = emptyMap()): ByteArray =
        container(write(document), attachments)

    private fun container(documentText: String, attachments: Map<String, ByteArray>): ByteArray {
        val entries = ArrayList<Zip.ToWrite>()
        entries.add(Zip.ToWrite(ROOT_DOCUMENT, documentText.toByteArray(Charsets.UTF_8)))
        val seen = HashMap<String, String>()
        seen[foldAscii(ROOT_DOCUMENT)] = ROOT_DOCUMENT
        // Section 7.1: the root document first, then attachments SORTED by entry
        // name under section 4.5.1's comparator — not in the order the caller
        // supplied them, because caller order is not a property of the content.
        // Two exporters using this one library, one iterating a hash map and one a
        // sorted list, would otherwise emit different archives for the same
        // document while satisfying every other field in that table.
        val sorted = attachments.entries.sortedWith(compareBy(CodePointOrder.comparator) { it.key })
        for ((name, body) in sorted) {
            checkPath(name)
            if (name.isEmpty() || name.endsWith("/")) {
                refuse(
                    RefusalCause.CONTAINER_PATH_ESCAPES,
                    "the entry \"$name\" is not an attachment: an attachment name is a file path",
                )
            }
            val previous = seen.put(foldAscii(name), name)
            if (previous != null) {
                refuse(
                    RefusalCause.CONTAINER_DUPLICATE_ENTRY,
                    "the container would carry a duplicate entry name: \"$previous\" and \"$name\" " +
                        "match when compared case-insensitively",
                )
            }
            entries.add(Zip.ToWrite(name, body))
        }
        return Zip.write(entries)
    }

    /**
     * Canonicalises a document's bytes: decode, then re-render. This is the
     * `fmt` operation, and it never rounds — canonicalising re-renders the bytes
     * another program wrote, and canonical form constrains key order and number
     * spelling, not precision (section 4.5.4).
     */
    fun canonicalise(text: String): String = render(readDocument(text).document)

    // ------------------------------------------------------------- internals

    /**
     * Section 7: a reader MUST prefer an entry named `doc.vnx` at the archive
     * root. Where there is none, it takes the FIRST entry with a `.vnx`
     * extension in archive order. Names are compared case-insensitively.
     *
     * The match is on the full entry name. Matching on the base name alone would
     * match at any depth, and letting the last match win rather than the first is
     * the other half of the same mistake.
     */
    private const val ROOT_DOCUMENT = "doc.vnx"

    private fun rootDocument(entries: List<Zip.Entry>): Zip.Entry {
        entries.firstOrNull { it.name.equals(ROOT_DOCUMENT, ignoreCase = true) }?.let { return it }
        entries.firstOrNull { it.name.endsWith(".vnx", ignoreCase = true) }?.let { return it }
        refuse(
            RefusalCause.CONTAINER_NO_DOCUMENT,
            "the container carries no entry with a .vnx extension, so there is no document to return",
        )
    }

    /**
     * Section 7: attachment paths MUST be relative and forward-slash separated.
     * An absolute path, a path carrying a drive letter, or a path with a `..`
     * SEGMENT is refused. `..` is a segment, not a substring: `a..b/photo.jpg` is
     * a legal name and escapes nothing.
     */
    private fun checkPath(name: String) {
        fun bad(why: String): Nothing = refuse(
            RefusalCause.CONTAINER_PATH_ESCAPES,
            "the entry \"$name\" escapes the archive: $why",
        )
        if (name.startsWith("/")) bad("it is an absolute path")
        if (name.contains('\\')) bad("paths are forward-slash separated")
        if (name.length >= 2 && name[1] == ':' && name[0].isAsciiLetter()) bad("it carries a drive letter")
        for (segment in name.split('/')) {
            if (segment == "..") bad("it carries a `..` path segment")
        }
    }

    private fun Char.isAsciiLetter() = this in 'a'..'z' || this in 'A'..'Z'

    /**
     * ASCII case folding. `lowercase()` folds under the JVM's default locale, and
     * in a Turkish locale it turns `I` into a dotless `ı`, which is not the same
     * string as `i`. Every case-insensitive comparison in this format is over
     * ASCII vocabulary, so folding ASCII and nothing else is both correct and
     * locale-independent.
     */
    private fun foldAscii(s: String): String {
        val sb = StringBuilder(s.length)
        for (c in s) sb.append(if (c in 'A'..'Z') c + 32 else c)
        return sb.toString()
    }

    /** SPEC.md section 4.5.5 forbids a writer emitting a BOM; a reader still meets one. */
    private fun decodeUtf8(bytes: ByteArray): String {
        val text = String(bytes, Charsets.UTF_8)
        return if (text.startsWith('\uFEFF')) text.substring(1) else text
    }
}
