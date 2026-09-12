package app.viatrail.vnx

/**
 * Why a diagnostic was reported.
 *
 * SPEC.md section 6.3 gives a diagnostic two fields, `path` and `message`, and
 * the conformance corpus matches `message` as an English substring. That makes
 * the English wording load-bearing for every implementation that did not write
 * it, which is a known defect: reason codes are the fix. So every diagnostic
 * this library reports carries a stable machine cause alongside its English
 * text, and [DiagnosticCause.corpusSubstring] is the table mapping each cause to
 * the substring the corpus expects today. When the corpus grows a reason-code
 * field, that table is the migration and nothing else has to change.
 *
 * `MessageWordingTest` asserts every non-null entry really is a substring of the
 * message this library emits for that cause, so the table cannot rot.
 */
enum class DiagnosticCause(
    /**
     * The reason code the corpus uses for this cause, where it has one.
     *
     * These arrived in the sidecars on 2026-09-09 as `code`, alongside the
     * English under the honest new name `message_advisory`. The runner now pairs
     * on the code and ignores the advisory text, which is what makes a
     * diagnostic's exact wording a reader's own business in fact and not only in
     * principle.
     */
    val corpusCode: String?,
    /**
     * The substring the corpus expected in this cause's message BEFORE reason
     * codes landed, kept so a sidecar that has not yet migrated still pairs, and
     * so `WordingTest` can keep asserting the English actually names the cause.
     */
    val corpusSubstring: String?,
) {
    /** An unrecognised key on an object this format defines. (6.2) */
    UNKNOWN_KEY("unknown-key", "unknown key"),

    /** A REQUIRED key is absent, so the object cannot exist. (3.10, 6.2) */
    MISSING_REQUIRED_KEY(null, null),

    /**
     * A value of the wrong JSON type in an OPTIONAL key: the field is dropped and
     * the item is kept. (6.2)
     */
    WRONG_TYPE_OPTIONAL("wrong-type-optional", null),

    /**
     * A value of the wrong JSON type in a REQUIRED key: the item is dropped,
     * because the object cannot exist without it. (6.2)
     *
     * Separate from [MISSING_REQUIRED_KEY], which takes the same remedy and is
     * not the same fact. And separate from [WRONG_TYPE_OPTIONAL] for the reason
     * the remedies differ: "a field is gone" and "the item is gone" differ in
     * whether the rest of the item survives, and a consumer told only that a type
     * was wrong would have to re-derive required-ness from section 3.10's table
     * to find out which happened — which is the lookup a code exists to save.
     */
    WRONG_TYPE_REQUIRED("wrong-type-required", null),

    /** A value outside a range or enumeration this format states. (6.2) */
    OUT_OF_RANGE(null, null),

    /**
     * A position that will not parse. (4.1, 6.2)
     *
     * The remedy drops the CONTAINER rather than the position: an arc, a segment
     * or an area, not the pip or the vertex. Geometry is continuous and structure
     * is not — a missing vertex leaves a shorter, plausible, wrong line with
     * nothing to indicate it, where a missing link leaves a gap the continuity
     * rule of 3.3.3 can see and report.
     */
    POSITION_UNUSABLE("position-unusable", null),

    /** A record-level timestamp carrying an offset other than `Z`. (4.3, 6.2) */
    TIMESTAMP_NOT_UTC("timestamp-not-utc", "must be UTC with a Z suffix"),

    /** A `polygon` carrying a second or nested ring. (3.7, 6.2) */
    POLYGON_NESTED_RINGS("polygon-nested-rings", "nested rings"),

    /** A `polygon` that does not close, or holds fewer than four positions. (3.7, 6.2) */
    POLYGON_NOT_A_CLOSED_RING(null, null),

    /** A link naming an arc that is absent from its network. (6.2) */
    LINK_ARC_ABSENT("link-arc-absent", "not in this network"),

    /** A link endpoint naming a node that is absent from its network. */
    LINK_NODE_ABSENT(null, null),

    /** An arc naming a node that is not in its network. (3.3.2, 6.2) */
    ARC_NODE_ABSENT(null, null),

    /** An inline endpoint that does not project onto its arc within 50 m. (3.3.3) */
    INLINE_ENDPOINT_BEYOND_TOLERANCE("endpoint-off-arc", "beyond the 50 m tolerance"),

    /** An inline endpoint on an arc with no vertices, which has no distance. (3.3.3) */
    INLINE_ENDPOINT_ON_EMPTY_ARC(null, null),

    /** Consecutive links in a chain that do not meet. (6.2) */
    CHAIN_NOT_CONNECTED("chain-discontinuity", "not one connected walk"),

    /** A `folder` naming a folder that is not present in the document. (6.2) */
    FOLDER_ABSENT(null, null),

    /** A data record with no `@type`, or one that is not a string. (5, 6.2) */
    DATA_RECORD_TYPE_MISSING(null, null),

    /**
     * An arc terminal pip further than 50 m from the node that end names. (3.3.2)
     *
     * An arc's pips are its COMPLETE point list, so the first pip states the
     * position of the node `from` names and the last states `to`'s: the two are
     * one fact written twice. Both are kept — the pips are authoritative for
     * geometry — and the disagreement is reported.
     */
    TERMINAL_PIP_OFF_NODE("terminal-pip-off-node", "beyond the 50 m tolerance"),

    /**
     * An attachment compressed by a method other than stored or deflate. (7)
     *
     * The one cause here whose `path` is not a pointer into the document: an
     * archive entry is not part of the document, so it lives in section 6.3's
     * separate `#/entries/<name>` space.
     */
    ATTACHMENT_UNREADABLE("attachment-unreadable", "neither stored nor deflate"),
    ;
}

/**
 * One thing the reader could not use as written: a key it skipped, an element it
 * dropped, or a relationship it could not resolve.
 *
 * SPEC.md section 6.3: a diagnostic carries no severity, because every
 * diagnostic this format defines reports the same kind of fact.
 */
data class Diagnostic(
    /** A JSON Pointer (RFC 6901) locating the element the diagnostic is about. */
    val path: String,
    /** A short description in English. */
    val message: String,
    /** The stable machine cause behind the English text. */
    val cause: DiagnosticCause,
) {
    /**
     * The format's reason code for this diagnostic, where the corpus defines
     * one. A caller matching on this never has to read the English.
     */
    val code: String? get() = cause.corpusCode

    override fun toString() = "$path: [${code ?: cause.name}] $message"
}

/**
 * What a reader returns: the document, and the diagnostics beside it.
 *
 * SPEC.md section 6.3 requires both. A reader that returns a document alone has
 * discarded the only evidence that anything was wrong.
 */
data class ReadResult(
    val document: VnxDocument,
    val diagnostics: List<Diagnostic>,
    /**
     * The attachments of the container this came from, in archive order, or
     * null when the source was a plain `.vnx` document.
     *
     * NAMES ALONE WOULD NOT BE AN ANSWER. A container exists to carry
     * attachments, and a caller handed a list of names it cannot open has been
     * told what it is missing rather than given it.
     */
    val attachments: List<Attachment>? = null,
)

/**
 * One file inside a `.vnz` container that is not the root document (SPEC.md
 * section 7).
 *
 * [path] is the archive entry name: a relative, forward-slash separated path,
 * checked at read time by the rules of sections 7 and 9. [bytes] is the entry's
 * content, decompressed. Nothing in this format interprets an attachment, so
 * the bytes come back as they are.
 *
 * EQUALITY IS BY VALUE, written out rather than generated. A `data class`
 * holding a [ByteArray] gets `equals` by REFERENCE for that member, so two
 * attachments with the same path and the same content would compare unequal,
 * and a test asserting on a list of them would fail for a reason that has
 * nothing to do with what it is testing.
 */
class Attachment(val path: String, val bytes: ByteArray) {
    override fun equals(other: Any?): Boolean =
        this === other || (other is Attachment && path == other.path && bytes.contentEquals(other.bytes))

    override fun hashCode(): Int = 31 * path.hashCode() + bytes.contentHashCode()

    /** The bytes are not printed: an attachment is arbitrary content and may be large. */
    override fun toString() = "Attachment($path, ${bytes.size} bytes)"
}

/** Builds RFC 6901 JSON Pointers as the decoder descends. */
internal class Pointer private constructor(private val text: String) {
    companion object {
        val ROOT = Pointer("")
        private fun escape(token: String) = token.replace("~", "~0").replace("/", "~1")
    }

    fun child(token: String) = Pointer("$text/${escape(token)}")
    fun child(index: Int) = Pointer("$text/$index")
    override fun toString() = text
}

/**
 * Section 6.3's `#/entries/` space, which is where a diagnostic about an archive
 * ENTRY lives: an entry is not part of the document, so no pointer into the
 * document can locate one.
 *
 * THE MEMBER NAME IS ONE RFC 6901 REFERENCE TOKEN, and the escaping ORDER
 * decides the answer: `~` becomes `~0` BEFORE `/` becomes `~1`. Reversed, the
 * second pass escapes the `~1` the first pass just wrote, so `photos/a~b.jpg`
 * comes out `photos~01a~0b.jpg`. An attachment name is the one place in this
 * format where a member name routinely holds a slash.
 */
internal fun entryPointer(name: String): String =
    "#/entries/" + name.replace("~", "~0").replace("/", "~1")
