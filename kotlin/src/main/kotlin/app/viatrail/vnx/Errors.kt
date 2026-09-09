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
 * never has to read the prose. [RefusalCause.corpusSubstring] is the table that
 * makes that migration mechanical.
 */
enum class RefusalCause(
    /**
     * The substring the conformance corpus expects in this cause's message
     * today, or null where no corpus case exercises the cause.
     *
     * When the corpus moves to reason codes, this table is what maps the old
     * expectation onto the new field, and it is asserted against the sidecars
     * by RefusalWordingTest so it cannot go stale silently.
     */
    val corpusSubstring: String?,
) {
    /** The bytes are not JSON at all. `NaN` and `Infinity` land here. */
    MALFORMED_JSON("malformed JSON"),

    /** The top level is not a JSON object, or carries no `vnx` key. (2.2) */
    NOT_A_VNX_DOCUMENT("not a VNX document"),

    /** `vnx` is not three digit-runs separated by full stops. (2.2) */
    VERSION_MALFORMED("MAJOR.MINOR.PATCH"),

    /** `vnx` names a MAJOR or MINOR above the reader's. (2.2) */
    VERSION_TOO_NEW("a newer reader is needed"),

    /** A number in the document is not finite. (4.2) */
    NON_FINITE_NUMBER(null),

    /** The archive is not a readable ZIP. (7) */
    CONTAINER_MALFORMED(null),

    /** Two entries whose names match case-insensitively. (7) */
    CONTAINER_DUPLICATE_ENTRY("duplicate entry"),

    /** An absolute path, a drive letter, or a `..` segment. (7, 9) */
    CONTAINER_PATH_ESCAPES("escapes the archive"),

    /** No `.vnx` entry anywhere in the archive. (7) */
    CONTAINER_NO_DOCUMENT(null),

    /** The root document uses a compression method other than stored or deflate. (7) */
    CONTAINER_ROOT_COMPRESSION(null),
    ;
}

/**
 * A document was refused. SPEC.md section 6.3 requires this to reach the caller
 * through a channel separate from the diagnostics, so that a refused document is
 * distinguishable from one that was read with elements dropped.
 */
class VnxRefusal(
    /** The stable machine cause, so a caller never has to read the English. */
    val refusal: RefusalCause,
    override val message: String,
) : Exception(message) {
    override fun toString() = "VnxRefusal(${refusal.name}): $message"
}

internal fun refuse(cause: RefusalCause, message: String): Nothing = throw VnxRefusal(cause, message)
