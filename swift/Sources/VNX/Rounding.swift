import Foundation

/// SPEC 4.1's emit-boundary precision, computed from the EXACT decimal
/// expansion of the binary64 value, with ties to even.
///
/// The obvious route — multiply by a power of ten, round, divide back — is not
/// this, and the difference is not exotic. `0.15` as a binary64 is
/// 0.1499999999999999944…, so its exact expansion rounds to `0.1` at one place;
/// multiplying by ten gives exactly `1.5`, which is a tie, and a tie rounds to
/// even, giving `0.2`. Neither the multiply nor the tie rule is wrong on its own
/// — the multiply MANUFACTURED the tie. `0.45` goes the other way, exact `0.5`
/// against scaled `0.4`. Elevations to one place hit this constantly, and
/// longitudes to six places hit it too (`91.0304145` is exact `91.030415`,
/// scaled `91.030414`).
///
/// SPEC 4.1: the emitted value MUST be the one obtained by taking the
/// exact decimal expansion and rounding at the target place with ties to even,
/// and scale-multiply-and-divide is not a conforming route. Go's reference
/// `roundTo` used `math.Round`, which is half-away-from-zero, so two of five
/// implementations shipped a defect here.
///
/// This was unreachable by the corpus until `corpus/write/` landed on
/// 2026-09-09, and the comment here said so as though it were a property of the
/// format. It was not. It was a property of which tiers had been built: every
/// tier re-encoded what it had decoded, so nothing ever asked a library to
/// round a value that was not already rounded. The write tier now decodes a
/// document, calls WRITE, and compares, and it carries BOTH `340.25` and
/// `340.75`, because it takes the pair to identify the rule: `340.25` separates
/// half-to-even from half-away-from-zero and half-up, and `340.75` separates it
/// from half-toward-zero and half-down. Neither alone leaves only one rule
/// standing. An earlier version of this comment repeated the framing that
/// `340.75` was insurance against a reader rounding everything upward, which is
/// the framing that gets it deleted by whoever tidies the fixture next;
/// `RoundingTests.testOnlyHalfToEvenSurvivesThePair` states it as an assertion
/// instead.
///
/// The general lesson, from the tier's own README: an unreachability claim is a
/// claim about the current state of someone's ingenuity, not about the format.
/// `RoundingTests` and `corpus/numbers/` remain the finer instruments — one
/// fixture cannot cover 4,082 values — but they are no longer the only ones.
enum Rounding {

    /// The precision SPEC 4.1 requires of a writer: 6 decimal places for
    /// longitude and latitude, 1 for elevation.
    ///
    /// ⚠ THE NUMBERS ARE NOT A STYLE CHOICE. 6 decimal places of latitude is
    /// about 11 cm on the ground and a consumer GPS is accurate to ±3–5 m, so
    /// everything below the sixth place is the arithmetic's noise rather than
    /// the instrument's reading.
    static let coordinateDecimals = 6
    static let elevationDecimals = 1

    /// Rounds a longitude or latitude for the wire.
    static func coordinate(_ x: Double) -> Double { emitted(x, coordinateDecimals) }

    /// Rounds an elevation for the wire.
    static func elevation(_ x: Double) -> Double { emitted(x, elevationDecimals) }

    /// ⚠ ROUNDING CREATES NEGATIVE ZEROS THAT WERE NEVER IN THE MODEL.
    /// Rounding -0.04 to one place yields a zero with its sign bit set, as does
    /// -4e-08 to six places. That zero is the same value as `0` and carries no
    /// information, so the emit path collapses it — a writer that collapsed
    /// only the negative zeros it was HANDED would still emit `-0` for one it
    /// produced itself. `toDecimalPlaces` keeps the sign, because the sign is
    /// the honest answer to the arithmetic it performs; the collapse belongs
    /// here, at the boundary, which is also where `corpus/numbers/` measures it.
    private static func emitted(_ x: Double, _ places: Int) -> Double {
        let r = toDecimalPlaces(x, places)
        return r == 0 ? 0 : r
    }

    /// Rounds to `places` decimal places, ties to even, exactly.
    ///
    /// Exact in the arithmetic, and exact in the result for every value this
    /// format carries: the returned `Double` is the nearest binary64 to the
    /// exactly-rounded decimal, and that decimal is itself a binary64 whenever
    /// it needs 15 significant digits or fewer — which covers every longitude
    /// and latitude at 6 places (at most 9 digits) and every elevation at 1
    /// place. `RoundingTests.testTheRepresentabilityBoundary` pins where that
    /// stops being true, which is far outside any coordinate.
    static func toDecimalPlaces(_ x: Double, _ places: Int) -> Double {
        precondition(places >= 0 && places <= 8, "no channel in this format rounds beyond 8 places")
        guard x.isFinite, x != 0 else { return x }

        let negative = x < 0
        // magnitude = m * 2^exponent, exactly.
        let bits = x.magnitude.bitPattern
        let exponentBits = Int((bits >> 52) & 0x7FF)
        let fraction = bits & 0x000F_FFFF_FFFF_FFFF
        let m: UInt64
        let exponent: Int
        if exponentBits == 0 {
            m = fraction                    // subnormal: no implicit leading bit
            exponent = -1074
        } else {
            m = fraction | (1 << 52)
            exponent = exponentBits - 1075
        }

        // Where the DOUBLE grid is coarser than the decimal grid, rounding is
        // the identity — and that is a rule, not an optimisation.
        //
        // The spacing of doubles at this magnitude is 2^exponent. When that
        // exceeds 10^-places, the exactly-rounded decimal lies within half a
        // spacing of x, so the nearest binary64 to it IS x. (It cannot be
        // exactly half a spacing away: that would need 10^-places to be a power
        // of two.) Returning x is therefore the correctly-rounded answer, and
        // the shortest spelling of x has at most `places` decimals, because a
        // decimal that fine is guaranteed to round-trip.
        //
        // This branch is also what keeps the arithmetic below exact. Past it,
        // 2^exponent <= 10^-places, so m * 10^places < 2^53 and the rounded
        // integer fits a binary64 with nothing to spare — which is why
        // `rounded.asDouble` is exact and the single division that follows is
        // the only rounding in the function.
        //
        // Getting this condition wrong is not theoretical: an earlier version
        // tested `exponent + places >= 0`, which is 2^places rather than
        // 10^places, and the oracle caught four values near 2.25e15 where the
        // rounded integer exceeded 2^53 and the answer came back one double
        // wrong.
        if exponent >= 0 { return x }
        let spacing = -exponent
        if spacing < 64, powerOfTenInteger[places] > (UInt64(1) << UInt64(spacing)) { return x }

        let shift = spacing - places
        if shift <= 0 { return x }
        let a = m.multipliedFullWidth(by: fivePower[places])   // < 2^67
        var rounded: Wide
        if shift >= 128 {
            // a / 2^shift is far below one half, so the result is zero.
            rounded = Wide(high: 0, low: 0)
        } else {
            let quotient = Wide(high: a.high, low: a.low).shiftedRight(by: shift)
            let remainder = Wide(high: a.high, low: a.low).lowBits(shift)
            let half = Wide.powerOfTwo(shift - 1)
            switch remainder.compared(to: half) {
            case .orderedDescending:
                rounded = quotient.incremented()
            case .orderedAscending:
                rounded = quotient
            case .orderedSame:
                // The tie, and the only tie this function ever sees: the exact
                // expansion really does end at a 5 in the place below the
                // target. Round to even.
                rounded = quotient.low & 1 == 0 ? quotient : quotient.incremented()
            }
        }

        let value = rounded.asDouble / powerOfTen[places]
        // The sign survives this function: rounding -0.04 to one place produces
        // a zero with its sign bit set, and that is the honest answer to the
        // arithmetic here. `emitted` is what collapses it at the boundary.
        return negative ? -value : value
    }

    private static let fivePower: [UInt64] = [1, 5, 25, 125, 625, 3125, 15625, 78125, 390625]
    private static let powerOfTen: [Double] = [1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8]
    private static let powerOfTenInteger: [UInt64] = [1, 10, 100, 1000, 10_000, 100_000, 1_000_000, 10_000_000, 100_000_000]

    /// A 128-bit unsigned integer, only as far as this file needs one.
    /// Swift 5.9 has no `UInt128`, and `m * 5^6` needs 67 bits.
    struct Wide {
        var high: UInt64
        var low: UInt64

        func shiftedRight(by n: Int) -> Wide {
            if n == 0 { return self }
            if n < 64 { return Wide(high: high >> n, low: (low >> n) | (high << (64 - n))) }
            if n == 64 { return Wide(high: 0, low: high) }
            return Wide(high: 0, low: high >> (n - 64))
        }

        func lowBits(_ n: Int) -> Wide {
            if n == 0 { return Wide(high: 0, low: 0) }
            if n < 64 { return Wide(high: 0, low: low & ((1 << UInt64(n)) - 1)) }
            if n == 64 { return Wide(high: 0, low: low) }
            return Wide(high: high & ((1 << UInt64(n - 64)) - 1), low: low)
        }

        static func powerOfTwo(_ n: Int) -> Wide {
            if n < 64 { return Wide(high: 0, low: 1 << UInt64(n)) }
            return Wide(high: 1 << UInt64(n - 64), low: 0)
        }

        func compared(to other: Wide) -> ComparisonResult {
            if high != other.high { return high < other.high ? .orderedAscending : .orderedDescending }
            if low != other.low { return low < other.low ? .orderedAscending : .orderedDescending }
            return .orderedSame
        }

        func incremented() -> Wide {
            let (sum, overflow) = low.addingReportingOverflow(1)
            return Wide(high: overflow ? high + 1 : high, low: sum)
        }

        var asDouble: Double { Double(high) * 0x1p64 + Double(low) }
    }
}
