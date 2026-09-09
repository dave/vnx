import Foundation
import XCTest
@testable import VNX

final class NumberRenderingTests: XCTestCase {

    /// SPEC 4.5.3's own table, which is what the four target languages disagree
    /// about, plus the two cases the corpus's format tier exercises.
    func testSpecTable() {
        XCTAssertEqual(ecmaScriptNumber(340.0), "340")
        XCTAssertEqual(ecmaScriptNumber(0.000001), "0.000001")
        XCTAssertEqual(ecmaScriptNumber(1e-7), "1e-7")
        XCTAssertEqual(ecmaScriptNumber(1778914991000.0), "1778914991000")
        XCTAssertEqual(ecmaScriptNumber(1e21), "1e+21")
        XCTAssertEqual(ecmaScriptNumber(-3.67), "-3.67")
        XCTAssertEqual(ecmaScriptNumber(-3.6712345678901234), "-3.6712345678901235")
    }

    /// SPEC 4.5.3: negative zero MUST be rendered as `0`. Two writers
    /// disagreeing about the sign is a byte incompatibility carrying no
    /// information, and it is invisible to every value-level check because
    /// `-0.0 == 0.0`.
    func testNegativeZero() {
        XCTAssertEqual(ecmaScriptNumber(-0.0), "0")
        XCTAssertEqual(ecmaScriptNumber(0.0), "0")
        XCTAssertEqual(ecmaScriptNumber(-1e-400), "0", "an underflowed negative renders as 0, not -0")
    }

    /// Swift's own spellings, so a regression in `Double.description` shows up
    /// here as the cause rather than as a mystery in the format tier.
    func testSwiftDefaultsAreNotCanonical() {
        XCTAssertEqual(340.0.description, "340.0")
        XCTAssertEqual(0.000001.description, "1e-06")
        XCTAssertEqual(1e-7.description, "1e-07")
        XCTAssertEqual((-0.0).description, "-0.0")
    }

    /// The exponent boundaries of ECMAScript `Number::toString`, which is where
    /// Python and Swift diverge from Go and JavaScript.
    func testExponentBoundaries() {
        XCTAssertEqual(ecmaScriptNumber(1e-6), "0.000001")
        XCTAssertEqual(ecmaScriptNumber(1e-7), "1e-7")
        XCTAssertEqual(ecmaScriptNumber(1e20), "100000000000000000000")
        XCTAssertEqual(ecmaScriptNumber(1e21), "1e+21")
        XCTAssertEqual(ecmaScriptNumber(1.2345678901234568e20), "123456789012345680000")
        XCTAssertEqual(ecmaScriptNumber(5e-324), "5e-324")
        XCTAssertEqual(ecmaScriptNumber(1.7976931348623157e308), "1.7976931348623157e+308")
        XCTAssertEqual(ecmaScriptNumber(0.1), "0.1")
        XCTAssertEqual(ecmaScriptNumber(1.0 / 3.0), "0.3333333333333333")
    }

    /// Twenty doubles that sit EXACTLY midway between the two shortest decimal
    /// spellings that round-trip to them, so choosing between the spellings is
    /// a tie break. Every expected string here is what V8's `JSON.stringify`
    /// emits, measured 2026-09-09, and every one of them takes the EVEN last
    /// digit.
    ///
    /// This is the trap SPEC 4.5.3 cannot state, because ECMAScript's own prose
    /// does not decide ties: both spellings parse back to the same double, so no
    /// value-level check agrees or disagrees, and two implementations can differ
    /// here with nothing going red.
    ///
    /// It is NOT unreachable by the corpus, and an earlier version of this
    /// comment said it was. `format/number-tie` carries one spelling in and the
    /// other out, and two seats have measured that a renderer taking ties away
    /// from zero reddens the formatter tier. This test remains the sharper
    /// instrument — twenty ties against one — but it is not the only one.
    func testShortestDigitsBreakTiesToEven() {
        let cases: [(UInt64, String)] = [
            (0x405A91BB80000000, "106.27706909179688"),
            (0x42ED47B3DA801D64, "257550378598635.12"),
            (0x4163F374EDF80000, "10460071.436523438"),
            (0x42EE375F471DE39C, "265785364246300.88"),
            (0x3E70000000000000, "5.960464477539063e-8"),
            (0x42AE87E98AE71AA0, "16784543806349.312"),
            (0x40593BD680000000, "100.93496704101562"),
            (0x40821A58E0000000, "579.2933959960938"),
            (0x408AA201E0000000, "852.2509155273438"),
            (0x3F4BF80000000000, "0.0008535385131835938"),
            (0x3E88000000000000, "1.7881393432617188e-7"),
            (0x416B2E064FD80000, "14250034.495117188"),
            (0x406501D4C0000000, "168.05722045898438"),
            (0x3F5A4C0000000000, "0.0016050338745117188"),
            (0x414B03D087B00000, "3540897.0600585938"),
            (0x40275F1E00000000, "11.685775756835938"),
            (0x3EED800000000000, "0.000014066696166992188"),
            (0x3ECF000000000000, "0.0000036954879760742188"),
            (0x41C35A3210F68000, "649356321.9257812"),
            (0x4194200CE1160000, "84411192.27148438"),
        ]
        for (bits, expected) in cases {
            let x = Double(bitPattern: bits)
            XCTAssertEqual(ecmaScriptNumber(x), expected, "tie break for 0x\(String(bits, radix: 16))")
            // Both spellings are the same double, which is exactly why nothing
            // else can catch a disagreement here.
            XCTAssertEqual(Double(expected.replacingOccurrences(of: "e", with: "e")), x)
        }
    }

    /// SPEC 4.5.3's own worked tie, added when the rule was written down:
    /// "`1497898211722073.25` renders `1497898211722073.2`, not `…073.3`."
    /// About one double in 4,200 is a tie, concentrated between 1e13 and 1e15,
    /// which is where `time_ms` lives.
    func testTheSpecsWorkedTie() {
        XCTAssertEqual(ecmaScriptNumber(1497898211722073.25), "1497898211722073.2")
    }

    /// Why `format/number-tie` can see a tie at all, stated as a property of
    /// the fixture rather than as a belief about it: its two files carry
    /// DIFFERENT TEXT for the SAME double. That is what makes the case
    /// impossible to pass by echoing, and impossible to pass by any check that
    /// compares values.
    func testTheFormatTiersTieCaseIsTwoSpellingsOfOneDouble() throws {
        let directory = Corpus.root.appendingPathComponent("format")
        let input = try Data(contentsOf: directory.appendingPathComponent("number-tie.in.vnx"))
        let expected = try Data(contentsOf: directory.appendingPathComponent("number-tie.canonical.vnx"))
        XCTAssertNotEqual(input, expected, "the two files must differ in text")

        func tieValue(_ data: Data) throws -> Double {
            let record = try VNX.read(data).document.waypoints![0].data![0]
            return record.body["value"]!.numberValue!
        }
        XCTAssertEqual(try tieValue(input).bitPattern, try tieValue(expected).bitPattern,
                       "and agree in value, or the case would be testing something else")
        XCTAssertEqual(ecmaScriptNumber(try tieValue(input)), "1497898211722073.2")
    }

    /// Whatever this function emits must parse back to the double it was given,
    /// for every double. A shortest-representation renderer that drops a digit
    /// fails here immediately.
    func testRoundTripsOverManyDoubles() {
        var state: UInt64 = 0x9E3779B97F4A7C15
        func nextBits() -> UInt64 {
            state ^= state << 13; state ^= state >> 7; state ^= state << 17
            return state
        }
        var checked = 0
        for _ in 0..<200_000 {
            let x = Double(bitPattern: nextBits())
            guard x.isFinite else { continue }
            let text = ecmaScriptNumber(x)
            guard let back = Double(text) else {
                XCTFail("`\(text)` does not parse back as a number")
                continue
            }
            // -0 renders as 0 by design; every other value is exact.
            if x == 0 {
                XCTAssertEqual(back, 0)
            } else {
                XCTAssertEqual(back.bitPattern, x.bitPattern, "round trip via `\(text)`")
            }
            checked += 1
        }
        XCTAssertGreaterThan(checked, 100_000)
    }

    /// The canonical writer never asks the renderer for a non-finite number,
    /// because the parser refuses `NaN` and `Infinity` and the decoder refuses
    /// any number that overflows binary64 (SPEC 4.2).
    func testNonFiniteNeverReachesTheRenderer() throws {
        let overflow = Data(#"{"vnx":"0.1.0","waypoints":[{"at":[1e400,2]}]}"#.utf8)
        XCTAssertThrowsError(try VNX.read(overflow)) { error in
            XCTAssertEqual((error as? Refusal)?.cause, .nonFiniteNumber)
        }
    }
}
