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
data class Version(val major: BigInteger, val minor: BigInteger, val patch: BigInteger) {

    override fun toString() = "$major.$minor.$patch"

    companion object {
        /** The version of this format that this library implements. */
        val SUPPORTED = Version(BigInteger.ZERO, BigInteger.ONE, BigInteger.ZERO)

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
     * Whether a reader at [SUPPORTED] must refuse a document at this version.
     *
     * Through 0.x the MINOR component is a breaking axis, so a 0.1.0 reader
     * refuses anything whose MAJOR or MINOR is above its own and accepts
     * anything at or below — 0.1.3 and 0.0.9 are read, 0.2.0 and 1.0.0 are not.
     * The PATCH component never refuses at any version, because a patch
     * increment is additive by definition.
     *
     * From 1.0 onward minor versions are additive and only a MAJOR increment
     * refuses. This reader is at 0.1, so every 1.x document is above its MAJOR
     * and is refused by the first branch regardless; the second branch is
     * written out because the rule is the format's, not this reader's.
     */
    fun isTooNewFor(reader: Version): Boolean {
        if (reader.major.signum() == 0) {
            val byMajor = major.compareTo(reader.major)
            if (byMajor != 0) return byMajor > 0
            return minor > reader.minor
        }
        return major > reader.major
    }
}
