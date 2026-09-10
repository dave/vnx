import Foundation
import XCTest
@testable import VNX

/// The shared number-conformance table at `corpus/numbers/`: doubles as IEEE
/// 754 hex, with the value after rounding to 6 places and after rounding to 1.
///
/// A conformance tier in everything but its name. `corpus/write/` reaches SPEC
/// 4.1's precision rule inside a document, and this table reaches it per value,
/// on a set of values each chosen to separate a wrong rule from the right one.
///
/// ⚠ IT TESTS THE ROUNDING OPERATION, NOT A SPELLING. How a number is spelled
/// is the writer's own business (SPEC 4.2), and comparing spellings across
/// implementations is the rule this format dropped.
///
/// ⚠ AND BY BIT PATTERN RATHER THAN BY TEXT. Comparing the rounded value as
/// text would put the rendering rule back into the comparison through the side
/// door, and would then pass or fail on how Swift spells a double.
///
/// The answers come from Go, which is the reference implementation. Checking
/// against it is not this library marking its own homework, which is what a
/// locally generated table would be — that is what `RoundingOracle` is for, and
/// it is generated from Python's exact rational arithmetic instead.
final class NumberConformanceTests: XCTestCase {

    /// A missing table is a FAILURE. This file exists to run it, so a run that
    /// silently checked nothing would be worse than none.
    private static func locate() -> URL? {
        let root = Corpus.root.deletingLastPathComponent()
        for candidate in ["corpus/numbers/numbers.tsv", "number-conformance/numbers.tsv"] {
            let url = root.appendingPathComponent(candidate)
            if FileManager.default.fileExists(atPath: url.path) { return url }
        }
        return nil
    }

    struct Row {
        let bits: UInt64
        let kind: String
        let round6: Double
        let round1: Double
    }

    private func rows() throws -> [Row] {
        guard let url = Self.locate() else {
            XCTFail("the number-conformance table is at neither corpus/numbers/numbers.tsv nor number-conformance/numbers.tsv; this suite cannot check SPEC 4.1's precision rule without it")
            return []
        }
        let text = String(decoding: try Data(contentsOf: url), as: UTF8.self)
        var out: [Row] = []
        for line in text.split(separator: "\n", omittingEmptySubsequences: true) {
            if line.hasPrefix("#") { continue }
            let f = line.split(separator: "\t", omittingEmptySubsequences: false).map(String.init)
            guard f.count == 4 else {
                XCTFail("a row of the number table has \(f.count) columns, not 4: \(line)")
                continue
            }
            if f[0] == "bits" { continue } // the header row, if the table gains one
            guard let bits = UInt64(f[0], radix: 16) else {
                XCTFail("`\(f[0])` is not 16 hexadecimal digits")
                continue
            }
            guard let round6 = Double(f[2]), let round1 = Double(f[3]) else {
                XCTFail("row \(f[0]): the rounding columns are \(f[2]) and \(f[3]), which are not both numbers")
                continue
            }
            out.append(Row(bits: bits, kind: f[1], round6: round6, round1: round1))
        }
        return out
    }

    /// The row count and the per-kind counts the header declares.
    ///
    /// The generator refuses to write a header that disagrees with its body, so
    /// reading it is what makes a table that lost a whole category a failure
    /// here rather than a silence. A hardcoded list of the categories this
    /// suite depends on ages badly, and the newest category is always the one
    /// nothing pins.
    private func header() throws -> (rows: Int, counts: [String: Int]) {
        guard let url = Self.locate() else { return (-1, [:]) }
        let text = String(decoding: try Data(contentsOf: url), as: UTF8.self)
        var rows = -1
        var counts: [String: Int] = [:]
        for line in text.split(separator: "\n", omittingEmptySubsequences: true) {
            guard line.hasPrefix("#") else { break }
            if line.hasPrefix("# rows=") {
                rows = Int(line.dropFirst("# rows=".count).trimmingCharacters(in: .whitespaces)) ?? -1
            }
            if line.hasPrefix("# counts:") {
                for pair in line.dropFirst("# counts:".count).split(separator: " ", omittingEmptySubsequences: true) {
                    let parts = pair.split(separator: "=")
                    if parts.count == 2, let n = Int(parts[1]) { counts[String(parts[0])] = n }
                }
            }
        }
        return (rows, counts)
    }

    /// Both rounding columns, over the whole table.
    func testBothRoundingColumnsOfTheSharedTable() throws {
        let rows = try rows()
        let declared = try header()
        XCTAssertEqual(rows.count, declared.rows,
                       "the body and the header disagree on the row count")
        var kinds: Set<String> = []
        var reported = 0
        for row in rows {
            let x = Double(bitPattern: row.bits)
            kinds.insert(row.kind)
            for (what, got, want) in [("round6", Rounding.coordinate(x), row.round6),
                                      ("round1", Rounding.elevation(x), row.round1)] {
                // Bit patterns, so the comparison is over the value and not over
                // how either side spells it. -0 and 0 are DIFFERENT bit patterns
                // and the same value; the emit boundary normalises the sign, so
                // a mismatch here is real.
                guard got.bitPattern != want.bitPattern else { continue }
                reported += 1
                if reported <= 10 {
                    XCTFail("\(String(row.bits, radix: 16)): \(what) = \(got) (\(String(got.bitPattern, radix: 16))), the table says \(want) (\(String(want.bitPattern, radix: 16))) (kind \(row.kind))")
                }
            }
        }
        if reported > 10 {
            XCTFail("... and \(reported - 10) further disagreements")
        }
        // The header names every category the table carries. Asserting against
        // it is what stops a table that lost its discriminating rows from
        // passing as a full one.
        XCTAssertFalse(declared.counts.isEmpty, "the header declares no per-kind counts")
        XCTAssertEqual(kinds, Set(declared.counts.keys),
                       "the body and the header disagree on the kinds present")
    }

    /// The tie rows alone, so a failure names the rule rather than a bit
    /// pattern. The table's README asks for exactly this red proof: change the
    /// tie rule to half-away-from-zero and confirm both rounding columns go red.
    func testTheTieRowsSpecifically() throws {
        var round6 = 0, round1 = 0
        for row in try rows() {
            let x = Double(bitPattern: row.bits)
            switch row.kind {
            case "round6-tie":
                XCTAssertEqual(Rounding.coordinate(x).bitPattern, row.round6.bitPattern,
                               "6-place rounding tie 0x\(String(row.bits, radix: 16))")
                round6 += 1
            case "round1-tie":
                XCTAssertEqual(Rounding.elevation(x).bitPattern, row.round1.bitPattern,
                               "1-place rounding tie 0x\(String(row.bits, radix: 16))")
                round1 += 1
            default:
                break
            }
        }
        XCTAssertGreaterThan(round6, 0)
        XCTAssertGreaterThan(round1, 0)
    }
}
