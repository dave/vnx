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

    /**
     * A REQUIRED key is absent, so the object cannot exist. (3.10, 6.2)
     *
     * Section 6.2's table gives it the code of [WRONG_TYPE_REQUIRED]: the
     * remedy is the same, and so is what a caller can do about it. The cause
     * stays separate because the English says which of the two happened.
     */
    MISSING_REQUIRED_KEY("wrong-type-required", null),

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

    /**
     * A value outside a range or enumeration this format states, which section
     * 6.2 makes an unusable value in an optional key: the field is dropped and
     * the item kept. It takes the code of [WRONG_TYPE_OPTIONAL] because the
     * outcome is the same; the English says which fact it was. (6.2)
     */
    OUT_OF_RANGE("wrong-type-optional", null),

    /**
     * A position that will not parse. (4.1, 6.2)
     *
     * The remedy drops the OWNER rather than the position: a node, an arc, a
     * segment, an area, or the chain whose walk carries it. A missing vertex
     * would leave a shorter, plausible, wrong line with nothing to indicate it.
     */
    POSITION_UNUSABLE("position-unusable", null),

    /** A record-level timestamp carrying an offset other than `Z`. (4.3, 6.2) */
    TIMESTAMP_NOT_UTC("timestamp-not-utc", "must be UTC with a Z suffix"),

    /** A `polygon` carrying a second or nested ring. (3.7, 6.2) */
    POLYGON_NESTED_RINGS("polygon-nested-rings", "nested rings"),

    /**
     * A ring whose last position repeats its first. (3.7, 6.2)
     *
     * The repeat is dropped and the area kept: it is one fact written twice, so
     * nothing is lost. The path names the repeat by its index as written.
     */
    RING_CLOSED("ring-closed", null),

    /** A ring with fewer than three distinct positions. The area is dropped. (3.7, 6.2) */
    RING_TOO_FEW_POINTS("ring-too-few-points", null),

    /**
     * A time on a node, or an elevation or a time on a walk's `from`, `to` or
     * `turn`. The channel is dropped and its owner kept. (3.3.1, 3.3.3, 6.2)
     */
    EXTRA_CHANNEL("extra-channel", null),

    /**
     * A time on an arc pip. Every time channel on the arc is dropped and its
     * pips kept, and the fact is reported ONCE, at the arc's `pips`, however
     * many pips carried one. (3.3.2, 6.3 rule 3)
     */
    ARC_PIP_TIME("arc-pip-time", null),

    /** An arc whose `from` and `to` name one node. The arc is dropped. (3.3.2) */
    SELF_LOOP_ARC("self-loop-arc", null),

    /**
     * An arc whose `from` or `to` names a node that is not in its network or
     * route. There is no line without the node, so the arc is dropped. (3.3.2)
     */
    ARC_NODE_ABSENT("arc-node-absent", null),

    /**
     * A pass that is not an object, or that names no arc of its owner — an arc
     * another rule dropped included. The chain, or the route, is dropped.
     * (3.3.3, 6.2)
     */
    PASS_ARC_ABSENT("pass-arc-absent", null),

    /** A walk that breaks section 3.3.3's rule for `from` and `to`. (6.2) */
    WALK_SPELLING("walk-spelling", null),

    /**
     * A node `from` or `to` that is not an end of its arc, a pass not entered at
     * an end of its arc, or a pass other than the last leaving at a position:
     * valid-walk rules 3, 4 and 5 of section 3.3.3. (6.2)
     */
    WALK_NOT_CONNECTED("walk-not-connected", null),

    /** A pass carrying both `turn` and `pinned`. (3.3.3, 6.2) */
    PINNED_TURN("pinned-turn", null),

    /** A position, a `turn` or `pinned` in a route's walk. The route is dropped. (3.4, 6.2) */
    ROUTE_PIN_OR_POSITION("route-pin-or-position", null),

    /** A `folder` naming a folder that is not present in the document. (6.2) */
    FOLDER_ABSENT(null, null),

    /**
     * A data record with no `@type`, or one that is not a string. The record is
     * dropped. (5, 6.2)
     *
     * Section 6.2's table gives it the code of [WRONG_TYPE_REQUIRED]: `@type` is
     * the record's one required key, and the record is what goes.
     */
    DATA_RECORD_TYPE_MISSING("wrong-type-required", null),

    /**
     * An attachment whose bytes this reader could not return, so it was dropped
     * and the document returned without it. (7)
     *
     * TWO FAULTS, ONE CODE, because section 7 gives them one remedy: a
     * compression method other than stored or deflate, and bytes that cannot be
     * recovered though the method was usable — a stream that will not
     * decompress, or a CRC that disagrees with the bytes present. Refusing a
     * whole trail library because one photograph is damaged is the same
     * disproportion as refusing it because one photograph used an unusual
     * method, and the document is untouched either way. The English says which
     * of the two it was; the code does not, and a caller's remedy is the same.
     *
     * The advisory wording is therefore the part both messages share. It was
     * "neither stored nor deflate" until the corrupt-bytes rule arrived, which
     * would have quietly stopped covering half of what the cause now reports.
     *
     * The one cause here whose `path` is not a pointer into the document: an
     * archive entry is not part of the document, so it lives in section 6.3's
     * separate `#/entries/<name>` space.
     */
    ATTACHMENT_UNREADABLE("attachment-unreadable", "is dropped"),
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
