package app.viatrail.vnx

/**
 * Why a document or a container was refused.
 *
 * SPEC.md section 6.2 lists four causes for a plain document and section 7 the
 * causes for a container, and says a reader MUST NOT refuse for any other. This
 * enum is that closed list.
 *
 * The cause is carried alongside the English text on purpose. The conformance
 * corpus currently matches refusals as English substrings, which makes the
 * wording load-bearing for anyone who did not write it; a stable machine cause
 * beside it means a caller — and a future corpus that matches on reason codes —
 * never has to read the prose. [RefusalCause.corpusSubstrings] is the table that
 * makes that migration mechanical.
 */
enum class RefusalCause(
    /**
     * The substrings the conformance corpus expects in this cause's messages
     * today, empty where no corpus case exercises the cause.
     *
     * A LIST rather than one string, because a cause is not a sentence. The
     * version-series cause is refused from two directions and section 2.2 wants
     * the refusal to say which, so one cause carries two expectations and each
     * has to be provoked separately. WordingTest asserts the table against the
     * sidecars in both directions so it cannot go stale silently.
     *
     * When the corpus moves to reason codes, this table is what maps the old
     * expectations onto the new field.
     */
    vararg val corpusSubstrings: String,
) {
    /** The bytes are not JSON at all. `NaN` and `Infinity` land here. */
    MALFORMED_JSON("malformed JSON"),

    /** The top level is not a JSON object, or carries no `vnx` key. (2.2) */
    NOT_A_VNX_DOCUMENT("not a VNX document"),

    /** `vnx` is not three digit-runs separated by full stops. (2.2) */
    VERSION_MALFORMED("MAJOR.MINOR.PATCH"),

    /**
     * `vnx` names a MAJOR series, or through 0.x a MINOR series, that this
     * reader does not support. (2.2)
     *
     * NOT "too new". Through 0.x each MINOR is its own series, so 0.0.9 is
     * refused by a 0.1 reader for the same reason 0.2.0 is. The cause was named
     * VERSION_TOO_NEW until 2026-09-12, when it turned out the name had made
     * the lower direction easy to forget in six implementations at once.
     */
    VERSION_SERIES_UNSUPPORTED("a newer reader is needed", "0.0 series"),

    /** A number in the document is not finite. (4.2) */
    NON_FINITE_NUMBER(),

    /** The archive is not a readable ZIP. (7) */
    CONTAINER_MALFORMED(),

    /** Two entries whose names match case-insensitively. (7) */
    CONTAINER_DUPLICATE_ENTRY("duplicate entry"),

    /** An absolute path, a drive letter, or a `..` segment. (7, 9) */
    CONTAINER_PATH_ESCAPES("escapes the archive"),

    /** No `.vnx` entry anywhere in the archive. (7) */
    CONTAINER_NO_DOCUMENT(),

    /**
     * The root document's bytes cannot be recovered: an unusable compression
     * method, a stream that will not decompress, or a CRC that disagrees. (7)
     *
     * The same fault on an ATTACHMENT is not a refusal at all — section 7 drops
     * it with a diagnostic. The root document is the exception because there is
     * then no document to return. The cause was named CONTAINER_ROOT_COMPRESSION
     * until 2026-09-12, when the corrupt-bytes remedy joined it and the name
     * stopped covering what it decides.
     */
    CONTAINER_ROOT_UNREADABLE(),
    ;
}

/**
 * A document was refused. SPEC.md section 6.3 requires this to reach the caller
 * through a channel separate from the diagnostics, so that a refused document is
 * distinguishable from one that was read with elements dropped.
 *
 * UNCHECKED, and that is for the JAVA caller rather than the Kotlin one.
 * Kotlin has no checked exceptions, so extending [Exception] cost a Kotlin
 * caller nothing — but it made this a checked exception to `javac` while no
 * Kotlin signature emits a `throws` clause naming it. The result was the worst
 * of both: a Java caller was not forced to handle a refusal AND could not name
 * it, because `catch (VnxRefusal e)` around a `Vnx.readDocument` call is
 * rejected with "exception VnxRefusal is never thrown in body of corresponding
 * try statement". Catching [Exception] and downcasting was the only route.
 * Changed 2026-09-12; nothing in Kotlin had to move with it.
 */
class VnxRefusal(
    /** The stable machine cause, so a caller never has to read the English. */
    val refusal: RefusalCause,
    override val message: String,
) : RuntimeException(message) {
    override fun toString() = "VnxRefusal(${refusal.name}): $message"
}

internal fun refuse(cause: RefusalCause, message: String): Nothing = throw VnxRefusal(cause, message)
