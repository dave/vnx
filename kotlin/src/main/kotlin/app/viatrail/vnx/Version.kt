package app.viatrail.vnx

import java.math.BigInteger

/**
 * A VNX format version: three digit-runs separated by full stops
 * (SPEC.md section 2.2).
 *
 * There is no bound on a component's length and no rule against a leading zero,
 * so `00.01.0000` is a legal spelling of 0.1.0 and the comparison is numeric
 * rather than textual. [BigInteger] rather than [Int] because "no bound on
 * length" means exactly that.
 */
data class Version(
    val major: BigInteger,
    val minor: BigInteger,
    val patch: BigInteger,
) {
    override fun toString() = "$major.$minor.$patch"

    companion object {
        /** The version of this format that this library implements. */
        val SUPPORTED = Version(BigInteger.ZERO, BigInteger.TEN, BigInteger.valueOf(6))

        private val SHAPE = Regex("^([0-9]+)\\.([0-9]+)\\.([0-9]+)$")

        /** Parses a version string, or returns null if it is not three digit-runs. */
        fun parseOrNull(text: String): Version? {
            val m = SHAPE.matchEntire(text) ?: return null
            return Version(
                BigInteger(m.groupValues[1]),
                BigInteger(m.groupValues[2]),
                BigInteger(m.groupValues[3]),
            )
        }
    }

    /**
     * Whether a reader at [reader] supports this document's version.
     *
     * The rule is "the same series", not "not greater than" (SPEC.md section
     * 2.2). Through 0.x each MINOR is its own series: a 0.10.0 reader reads
     * 0.10.3, and refuses 0.11.0 AND 0.1.9. A lower minor is refused for the same
     * reason as a higher one — 0.1 is not an older spelling of 0.10, it is a
     * series this reader has no rules for. The PATCH component never refuses at
     * any version, because a patch increment is additive by definition.
     *
     * From 1.0 onward minor versions are additive and only a MAJOR increment
     * refuses. This reader is at 0.10, so the second branch is unreachable here;
     * it is written out because the rule is the format's, not this reader's.
     *
     * A reader that supported several series would hold a set of them and ask
     * this question once per member. This one supports exactly [SUPPORTED].
     */
    fun isSupportedBy(reader: Version): Boolean {
        if (major != reader.major) return false
        if (reader.major.signum() == 0) return minor == reader.minor
        return true
    }

    /**
     * Whether this version is above [reader] on the MAJOR.MINOR axis.
     *
     * Only for WORDING: a refusal that says "a newer reader is needed" must not
     * be said of a document from an older series. Nothing decides acceptance
     * here; [isSupportedBy] does that.
     */
    fun isAbove(reader: Version): Boolean {
        val byMajor = major.compareTo(reader.major)
        if (byMajor != 0) return byMajor > 0
        return minor > reader.minor
    }
}
