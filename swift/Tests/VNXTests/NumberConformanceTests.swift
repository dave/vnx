import Foundation
import XCTest
@testable import VNX

/// The shared number-conformance table: 5,315 doubles as IEEE 754 hex with the
/// canonical spelling, the spelling after rounding to 6 places, and the spelling
/// after rounding to 1 place.
///
/// A conformance tier in everything but its name. `corpus/write/` now reaches
/// SPEC 4.5.4 as a rule; this table reaches it as a population — 5,315 values
/// against one fixture's two elevation ties — and the same for SPEC 4.5.3, which
/// `format/number-tie` reaches exactly once.
///
/// Its answers come from Go's `strconv`, which SPEC 4.5.3 names as the
/// reference. Checking against it is not this library marking its own homework,
/// which is what a locally generated table would be.
final class NumberConformanceTests: XCTestCase {

    /// The table's announced destination is `corpus/numbers/`; it currently
    /// lives at the repository root because the spec has not folded it in yet.
    /// Both are checked, and a missing table is a FAILURE — this file exists to
    /// run it, so a run that silently checked nothing would be worse than none.
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
        let render: String
        let round6: String
        let round1: String
    }

    private func rows() throws -> [Row] {
        guard let url = Self.locate() else {
            XCTFail("the number-conformance table is at neither corpus/numbers/numbers.tsv nor number-conformance/numbers.tsv; this suite cannot check SPEC 4.5.4 without it")
            return []
        }
        let text = String(decoding: try Data(contentsOf: url), as: UTF8.self)
        var out: [Row] = []
        for line in text.split(separator: "\n", omittingEmptySubsequences: true) {
            if line.hasPrefix("#") { continue }
            let f = line.split(separator: "\t", omittingEmptySubsequences: false).map(String.init)
            guard f.count >= 5 else {
                XCTFail("a row of the number table has \(f.count) columns, not 5: \(line)")
                continue
            }
            if f[0] == "bits" { continue } // the header row, if the table gains one
            guard let bits = UInt64(f[0], radix: 16) else {
                XCTFail("`\(f[0])` is not 16 hexadecimal digits")
                continue
            }
            out.append(Row(bits: bits, kind: f[1], render: f[2], round6: f[3], round1: f[4]))
        }
        return out
    }

    /// All three columns. They are three different code paths — the renderer,
    /// and the renderer composed with rounding at each of the format's two
    /// places — and only the first has any chance of being right by accident.
    func testEveryColumnOfTheSharedTable() throws {
        let rows = try rows()
        XCTAssertGreaterThan(rows.count, 5000, "the table is far smaller than its README describes")
        var kinds: Set<String> = []
        for row in rows {
            let x = Double(bitPattern: row.bits)
            kinds.insert(row.kind)
            XCTAssertEqual(ecmaScriptNumber(x), row.render,
                           "render 0x\(String(row.bits, radix: 16)) (\(row.kind))")
            XCTAssertEqual(ecmaScriptNumber(Rounding.toDecimalPlaces(x, 6)), row.round6,
                           "round6 0x\(String(row.bits, radix: 16)) (\(row.kind))")
            XCTAssertEqual(ecmaScriptNumber(Rounding.toDecimalPlaces(x, 1)), row.round1,
                           "round1 0x\(String(row.bits, radix: 16)) (\(row.kind))")
        }
        // The README names the categories the generator refuses to leave empty.
        // Asserting them here is what stops a table that lost its discriminating
        // rows from passing as a full one.
        for required in ["render-tie", "round6-tie", "round1-tie", "negative-zero", "boundary"] {
            XCTAssertTrue(kinds.contains(required), "the table carries no `\(required)` rows any more")
        }
    }

    /// The tie rows alone, so a failure names the rule rather than a bit
    /// pattern. The table's README asks for exactly this red proof: change the
    /// tie rule to half-away-from-zero and confirm BOTH the render column and
    /// the two round columns go red.
    func testTheTieRowsSpecifically() throws {
        var render = 0, round6 = 0, round1 = 0
        for row in try rows() {
            let x = Double(bitPattern: row.bits)
            switch row.kind {
            case "render-tie":
                XCTAssertEqual(ecmaScriptNumber(x), row.render, "render tie 0x\(String(row.bits, radix: 16))")
                render += 1
            case "round6-tie":
                XCTAssertEqual(ecmaScriptNumber(Rounding.toDecimalPlaces(x, 6)), row.round6,
                               "6-place rounding tie 0x\(String(row.bits, radix: 16))")
                round6 += 1
            case "round1-tie":
                XCTAssertEqual(ecmaScriptNumber(Rounding.toDecimalPlaces(x, 1)), row.round1,
                               "1-place rounding tie 0x\(String(row.bits, radix: 16))")
                round1 += 1
            default:
                break
            }
        }
        XCTAssertGreaterThan(render, 0)
        XCTAssertGreaterThan(round6, 0)
        XCTAssertGreaterThan(round1, 0)
    }
}
