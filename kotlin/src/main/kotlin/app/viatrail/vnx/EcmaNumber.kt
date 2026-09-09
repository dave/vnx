package app.viatrail.vnx

import java.math.BigDecimal
import java.math.MathContext
import java.math.RoundingMode

/**
 * ECMAScript `Number::toString` for a binary64, which SPEC.md section 4.5.3
 * makes the canonical spelling of every number in a VNX document.
 *
 * Kotlin's own `Double.toString` is not it, and neither is Java's: `340.0`
 * renders as `340.0`, `1e21` as `1.0E21`, and `1e-7` as `1.0E-7`. SPEC.md
 * section 4.5.6 measured the same disagreement in Python and Swift and
 * concluded that every implementation outside Go and JavaScript writes this
 * itself. This is that renderer.
 *
 * ## Why not `Double.toString`, even reformatted
 *
 * The algorithm needs the SHORTEST decimal digit string that round-trips to the
 * same double. Java only guarantees that from JDK 19 (JDK-4511638); this library
 * targets JDK 17, where `Double.toString` may emit more digits than necessary.
 * A renderer built on it would therefore be correct on this JDK's successor and
 * wrong here, in a way that shows up as a byte difference against another
 * implementation rather than as a failure of its own. So the shortest form is
 * searched for directly, over the exact value of the double.
 */
internal object EcmaNumber {

    fun render(x: Double): String {
        require(!x.isNaN() && !x.isInfinite()) { "a non-finite number must never reach the serialiser" }
        // SPEC.md section 4.5.3: negative zero MUST render as `0`. `-0.0 == 0.0`
        // in Kotlin, so this catches both spellings of zero at once.
        if (x == 0.0) return "0"

        val negative = x < 0
        val digitsAndExponent = shortestDigits(Math.abs(x))
        val s = digitsAndExponent.first
        val n = digitsAndExponent.second
        val k = s.length

        val body = when {
            // ECMAScript step 6: k <= n <= 21
            n in k..21 -> s + "0".repeat(n - k)
            // step 7: 0 < n <= 21
            n in 1..21 -> s.substring(0, n) + "." + s.substring(n)
            // step 8: -6 < n <= 0
            n in -5..0 -> "0." + "0".repeat(-n) + s
            // steps 9 and 10: exponential
            else -> {
                val e = n - 1
                val sign = if (e >= 0) "+" else "-"
                val mantissa = if (k == 1) s else s.substring(0, 1) + "." + s.substring(1)
                mantissa + "e" + sign + Math.abs(e).toString()
            }
        }
        return if (negative) "-$body" else body
    }

    /**
     * The shortest decimal digit string `s` and exponent `n` with
     * `0.s * 10^n == x`, for a finite positive `x`.
     *
     * `BigDecimal(double)` is the exact binary value, so rounding it to `p`
     * significant digits is the correctly rounded `p`-digit decimal, and the
     * first `p` whose result parses back to the same double is the shortest
     * one. Seventeen digits always round-trip a binary64, so the loop always
     * terminates.
     *
     * Ties round to even. ECMAScript leaves the last digit of `s` explicitly
     * under-determined — "the least significant digit of s is not necessarily
     * uniquely determined by these criteria" — so the normative reference here is
     * what section 4.5.3 actually names: what `JSON.stringify` emits and what
     * Go's `encoding/json` reproduces. Measured 2026-09-09, both round a tie to
     * even: V8 and Go both write 1.4978982117220732e15 as
     * `1497898211722073.2`, where rounding half away from zero gives
     * `1497898211722073.3`. Both spellings are the same double, so nothing goes
     * red — it is a silent byte incompatibility, which is the exact class of
     * defect canonical form exists to prevent. Ties are reachable: about one
     * double in 4,200 of a uniform random sample, concentrated around 1e13 to
     * 1e15.
     */
    private fun shortestDigits(x: Double): Pair<String, Int> {
        val exact = BigDecimal(x)
        for (p in 1..17) {
            val candidate = exact.round(MathContext(p, RoundingMode.HALF_EVEN))
            if (candidate.toDouble() == x) {
                val stripped = candidate.stripTrailingZeros()
                val digits = stripped.unscaledValue().toString()
                // value == digits * 10^(-scale) == 0.digits * 10^(len - scale)
                return digits to (digits.length - stripped.scale())
            }
        }
        throw IllegalStateException("no decimal spelling of $x round-trips in 17 digits")
    }
}
