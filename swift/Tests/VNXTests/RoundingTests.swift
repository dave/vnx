import Foundation
import XCTest
@testable import VNX

/// SPEC 4.5.4's rounding is the one rule in this format that NO corpus tier can
/// reach. Rounding happens only when a writer emits a model, and the accept,
/// diagnose and format tiers all re-render a document they were handed. These
/// tests are the whole guard.
final class RoundingTests: XCTestCase {

    /// Against an exact oracle computed with rational arithmetic.
    func testAgainstAnExactOracle() {
        var checked = 0
        for (bits, places, expected) in RoundingOracle.rows {
            let x = Double(bitPattern: bits)
            let produced = Rounding.toDecimalPlaces(x, places)
            XCTAssertEqual(produced.bitPattern, expected,
                           "rounding \(x) to \(places) place(s): got \(produced), expected \(Double(bitPattern: expected))")
            checked += 1
        }
        XCTAssertEqual(checked, 4082, "the oracle has changed size; that is a decision, not a drift")
    }

    /// That every arm of the 128-bit shift inside `Rounding` is actually taken
    /// by some row of the oracle.
    ///
    /// Found by red-proofing: deleting the `shift == 64` arm of `Wide`, and
    /// setting the subnormal exponent to -1075 instead of -1074, BOTH left the
    /// suite green against the first version of the oracle. A table that does
    /// not reach a branch is not a test of it.
    func testTheOracleReachesEveryShiftArm() {
        var below64 = 0, exactly64 = 0, above64 = 0, atLeast128 = 0, identity = 0, subnormal = 0
        for (bits, places, _) in RoundingOracle.rows {
            let x = Double(bitPattern: bits)
            guard x != 0 else { continue }
            let magnitude = x.magnitude.bitPattern
            let exponentBits = Int((magnitude >> 52) & 0x7FF)
            let exponent = exponentBits == 0 ? -1074 : exponentBits - 1075
            if exponentBits == 0 { subnormal += 1 }
            let shift = -(exponent + places)
            if shift <= 0 { identity += 1 }
            else if shift >= 128 { atLeast128 += 1 }
            else if shift < 64 { below64 += 1 }
            else if shift == 64 { exactly64 += 1 }
            else { above64 += 1 }
        }
        XCTAssertGreaterThan(below64, 0, "shift < 64")
        XCTAssertGreaterThan(exactly64, 0, "shift == 64, the arm a red proof caught missing")
        XCTAssertGreaterThan(above64, 0, "64 < shift < 128")
        XCTAssertGreaterThan(atLeast128, 0, "shift >= 128, the short-circuit to zero")
        XCTAssertGreaterThan(identity, 0, "already on the grid")
        XCTAssertGreaterThan(subnormal, 0, "subnormal, whose exponent is not derived from the exponent field")
    }

    /// Every subnormal rounds to zero at every place this format uses, so the
    /// subnormal exponent is unobservable THROUGH THIS FUNCTION — which is a
    /// measurement, not an excuse for the branch. Stated here so that the branch
    /// existing and being untestable are two separate facts on the record.
    func testEverySubnormalRoundsToZero() {
        for fraction: UInt64 in [1, 2, 0x7FFF_FFFF_FFFFF, 0x8_0000_0000_0000, 0xF_FFFF_FFFF_FFFF] {
            let x = Double(bitPattern: fraction)
            XCTAssertFalse(x.isNormal)
            for places in [1, 6] {
                XCTAssertEqual(Rounding.toDecimalPlaces(x, places), 0, "\(x) at \(places)")
                XCTAssertEqual(Rounding.toDecimalPlaces(-x, places), 0)
            }
        }
    }

    /// That the WRITE path uses exact rounding, not just that `Rounding` can.
    ///
    /// Found by red-proofing: swapping `Encoder.round` back to
    /// scale-multiply-and-divide left every other test in this file green,
    /// because they all call `Rounding.toDecimalPlaces` directly. A rule is only
    /// guarded where it is reached from the path that ships.
    func testTheWritePathRoundsExactly() throws {
        func emitted(lon: Double, lat: Double, elevation: Double) -> String {
            var document = Document()
            document.waypoints = [Waypoint(at: Position(lon: lon, lat: lat, elevation: elevation))]
            let text = String(decoding: VNX.write(document), as: UTF8.self)
            let start = text.range(of: "\"at\": [")!.upperBound
            return String(text[start..<text.range(of: "]", range: start..<text.endIndex)!.lowerBound])
        }
        // Scaling would give 91.030414, 58.813652 and 0.2 for these three.
        XCTAssertEqual(emitted(lon: 91.0304145, lat: 58.8136515, elevation: 0.15),
                       "91.030415, 58.813651, 0.1")
        // Scaling would give -70.427978 and 0.4.
        XCTAssertEqual(emitted(lon: -70.4279775, lat: 113.2614425, elevation: 0.45),
                       "-70.427977, 113.261443, 0.5")
        // A genuine tie, where both routes agree, so it is not the discriminator
        // — kept beside them so the pair reads as one rule.
        XCTAssertEqual(emitted(lon: 0, lat: 0, elevation: 340.25), "0, 0, 340.2")
    }

    /// The named cases where scaling by a power of ten and rounding the product
    /// gives a different answer from rounding the exact expansion. Every one of
    /// these is a value two conforming libraries could disagree about while
    /// every test in both stayed green.
    func testWhereScalingDivergesFromExactRounding() {
        // The multiply manufactures a tie that the exact expansion does not
        // have: 0.15 is really 0.1499999999999999944…, so it rounds DOWN.
        XCTAssertEqual(Rounding.toDecimalPlaces(0.15, 1), 0.1)
        XCTAssertEqual(Rounding.toDecimalPlaces(0.35, 1), 0.3)
        // And it can destroy one: 0.45 is really 0.450000000000000011…, so it
        // rounds UP, where the scaled product is a tie and goes to even.
        XCTAssertEqual(Rounding.toDecimalPlaces(0.45, 1), 0.5)
        XCTAssertEqual(Rounding.toDecimalPlaces(8401.15, 1), 8401.1)
        // SPEC 4.5.4's third worked value: 0.05 is really 0.05000000000000000277…,
        // so it rounds UP to 0.1 where the scaled route gives 0.
        XCTAssertEqual(Rounding.toDecimalPlaces(0.05, 1), 0.1)
        XCTAssertEqual(Rounding.toDecimalPlaces(-6507.95, 1), -6507.9)
        // Six places, on a longitude anyone could hold.
        XCTAssertEqual(Rounding.toDecimalPlaces(91.0304145, 6), 91.030415)
        XCTAssertEqual(Rounding.toDecimalPlaces(58.8136515, 6), 58.813651)
        XCTAssertEqual(Rounding.toDecimalPlaces(-70.4279775, 6), -70.427977)
    }

    /// A genuine tie — where the exact expansion really does end at a 5 in the
    /// place below the target — rounds to even. SPEC 4.5.4's own example.
    func testGenuineTiesRoundToEven() {
        XCTAssertEqual(Rounding.toDecimalPlaces(340.25, 1), 340.2)
        XCTAssertEqual(Rounding.toDecimalPlaces(340.35, 1), 340.4)
        XCTAssertEqual(Rounding.toDecimalPlaces(-340.25, 1), -340.2)
        XCTAssertEqual(Rounding.toDecimalPlaces(0.25, 1), 0.2)
        XCTAssertEqual(Rounding.toDecimalPlaces(0.75, 1), 0.8)
    }

    /// SPEC 4.5.4: "A writer applies the negative-zero rule of Section 4.5.3
    /// AFTER rounding, not before. Rounding can CREATE a negative zero that was
    /// never in the model."
    func testRoundingCanCreateANegativeZeroAndTheRendererCollapsesIt() {
        let created = Rounding.toDecimalPlaces(-0.04, 1)
        XCTAssertEqual(created, 0)
        XCTAssertEqual(created.sign, .minus, "the sign bit is set: this is the value the spec warns about")
        XCTAssertEqual(ecmaScriptNumber(created), "0", "and the renderer, not the rounder, is what collapses it")

        var document = Document()
        // Longitude and latitude round to 6 places, elevation to 1, so each channel
        // needs its own value small enough to be rounded away.
        document.waypoints = [Waypoint(at: Position(lon: -0.00000004, lat: -0.0000004, elevation: -0.04))]
        let written = String(decoding: VNX.write(document), as: UTF8.self)
        XCTAssertTrue(written.contains("[0, 0, 0]"), "no `-0` reaches the wire; got \(written)")
    }

    /// Values already on the grid come back unchanged, bit for bit, including
    /// values so large that they are integers.
    func testValuesOnTheGridAreUntouched() {
        // 5e-324 is deliberately NOT here: it is far below the 6-place grid and
        // rounds to zero, which the oracle agrees with.
        for x in [0.0, -0.0, 1.0, 340.0, -3.671235, 57.118123, 1e300, 1778914991000.0] {
            XCTAssertEqual(Rounding.toDecimalPlaces(x, 6).bitPattern, x.bitPattern, "\(x) at 6 places")
        }
        XCTAssertEqual(Rounding.toDecimalPlaces(340.1, 1), 340.1)
    }

    /// Where the double grid becomes coarser than the decimal grid, which is
    /// where rounding turns into the identity.
    ///
    /// This is the boundary an earlier version of this file described wrongly.
    /// It said the result was "the nearest double rather than the decimal
    /// itself" past some absurd magnitude, and put that magnitude at ten
    /// trillion degrees. The oracle disagreed: the implementation was one double
    /// wrong at 2.25e15 rounding to one place, because the rounded integer had
    /// passed 2^53. The fix moved the identity branch from `2^places` to
    /// `10^places`, which is where the true boundary is, and past it the
    /// arithmetic is exact with nothing to spare.
    ///
    /// Both sides of the boundary are pinned here, so the claim in `Rounding`'s
    /// documentation is a measurement rather than an argument.
    func testTheIdentityBoundary() {
        // One place: the grid is 0.1, and doubles are spaced 2^-4 = 0.0625 just
        // below 2^49 and 2^-3 = 0.125 just above. So rounding still does work at
        // the smaller magnitude and is the identity at the larger.
        let below = Double(bitPattern: 0x42F0_0000_0000_0001)   // just above 2^48
        let above = Double(bitPattern: 0x4310_0000_0000_0001)   // just above 2^49
        XCTAssertNotEqual(Rounding.toDecimalPlaces(below, 1).bitPattern, below.bitPattern)
        XCTAssertEqual(Rounding.toDecimalPlaces(above, 1).bitPattern, above.bitPattern)

        // The four values the oracle caught, checked as values rather than as
        // table rows so the regression is named where a reader will find it.
        XCTAssertEqual(Rounding.toDecimalPlaces(2251799813685247.8, 1), 2251799813685247.8)
        XCTAssertEqual(Rounding.toDecimalPlaces(-2251799813685247.8, 1), -2251799813685247.8)
        XCTAssertEqual(Rounding.toDecimalPlaces(1125899906842623.9, 1), 1125899906842623.9)
        XCTAssertEqual(Rounding.toDecimalPlaces(-1125899906842623.9, 1), -1125899906842623.9)

        // Past the boundary the value's own shortest spelling already has at
        // most `places` decimals, so the identity is not a dodge.
        for x in [2251799813685247.8, 1125899906842623.9, above] {
            XCTAssertLessThanOrEqual(decimalPlaces(ecmaScriptNumber(x)), 1, "\(x)")
        }
        // And every coordinate this format carries is far on the other side,
        // where real rounding happens.
        for magnitude in [180.0, 90.0, 179.9999994, 8848.94] {
            XCTAssertLessThanOrEqual(decimalPlaces(ecmaScriptNumber(Rounding.toDecimalPlaces(magnitude, 6))), 6)
        }
    }

    private func decimalPlaces(_ text: String) -> Int {
        guard !text.contains("e"), let dot = text.firstIndex(of: ".") else { return 0 }
        return text.distance(from: text.index(after: dot), to: text.endIndex)
    }

    /// The shared table has landed, at `number-conformance/numbers.tsv`, and
    /// `NumberConformanceTests` runs it. `RoundingOracle` stays beside it and is
    /// not redundant: the shared table is the CROSS-IMPLEMENTATION instrument,
    /// generated from Go, while the local oracle is chosen to take every branch
    /// of this implementation's 128-bit arithmetic, which no shared table would
    /// know about. This test is what stops the local one from being mistaken for
    /// the shared one.
    func testTheSharedTableIsPresentAndSeparate() {
        let root = Corpus.root.deletingLastPathComponent()
        let candidates = ["corpus/numbers/numbers.tsv", "number-conformance/numbers.tsv"]
        XCTAssertTrue(candidates.contains { FileManager.default.fileExists(atPath: root.appendingPathComponent($0).path) },
                      "the shared number table has moved again; NumberConformanceTests.locate needs its new path")
    }
}
