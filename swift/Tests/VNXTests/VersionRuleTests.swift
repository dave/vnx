import Foundation
import XCTest
@testable import VNX

/// SPEC 2.2's version rules, exercised from BOTH sides of 1.0.
///
/// The library is a 0.1 reader, so the 1.0-and-later branch of the rule — minor
/// versions are additive, and only a MAJOR increment refuses — is unreachable
/// through the public API. Every mutation of that branch therefore stays green
/// against a suite that only ever reads as a 0.1 reader, which is how an
/// untestable claim gets mistaken for a tested one. Two red proofs came back
/// green before these tests existed, and both mutations turned out to be
/// provably no-ops for a 0.x reader rather than weak guards.
///
/// These tests read as a hypothetical 1.0 reader instead.
final class VersionRuleTests: XCTestCase {

    private func read(_ version: String, asReader reader: String) throws -> Document {
        let decoder = DocumentDecoder(readerVersion: SemanticVersion(reader)!)
        return try decoder.decode(try JSONParser.parse(Data("{\"vnx\":\"\(version)\"}".utf8)))
    }

    private func refusal(_ version: String, asReader reader: String) -> Refusal? {
        do {
            _ = try read(version, asReader: reader)
            return nil
        } catch let r as Refusal {
            return r
        } catch {
            return nil
        }
    }

    /// "The rules a reader applies are selected by the DOCUMENT's MAJOR
    /// component, not by the reader's."
    ///
    /// A 1.0 reader handed a 0.9 document does NOT get to read it under 1.x's
    /// rule that a reader ignores what it does not recognise. That would be a
    /// guess about guarantees the document never carried.
    func testANewerReaderMayNotApplyItsOwnRulesToAnOlderSeries() {
        let r = refusal("0.9.0", asReader: "1.0.0")
        XCTAssertEqual(r?.cause, .versionTooNew)
        XCTAssertTrue(r?.message.contains("MUST NOT read another one under its own rules") ?? false,
                      "refused for being another series: \(r?.message ?? "not refused at all")")
    }

    /// From 1.0 onward, minor versions are additive: a reader accepts any minor
    /// at or above its own, and only a MAJOR increment refuses. This branch does
    /// not exist for a 0.x reader.
    func testFromOneOnwardMinorVersionsAreAdditive() throws {
        for version in ["1.0.0", "1.0.99", "1.5.0", "1.99.3"] {
            XCTAssertNoThrow(try read(version, asReader: "1.0.0"), version)
        }
        XCTAssertEqual(try read("1.5.0", asReader: "1.0.0").vnx, "1.5.0",
                       "and a reader that re-encodes writes the version the document declared")
        XCTAssertEqual(refusal("2.0.0", asReader: "1.0.0")?.cause, .versionTooNew)
    }

    /// Through 0.x the MINOR number is a breaking axis, so the same document
    /// shape refuses where the 1.x rule would accept. The pair is the rule.
    func testThroughZeroTheMinorIsABreakingAxis() {
        XCTAssertEqual(refusal("0.5.0", asReader: "0.1.0")?.cause, .versionTooNew)
        XCTAssertTrue(refusal("0.5.0", asReader: "0.1.0")?.message.contains("breaking axis") ?? false)
        XCTAssertNil(refusal("1.5.0", asReader: "1.1.0"),
                     "the identical minor gap is fine once the document's major is 1")
    }

    /// PATCH never refuses, at any version, on either side of 1.0.
    func testPatchNeverRefuses() {
        for (version, reader) in [("0.1.99", "0.1.0"), ("1.0.99", "1.0.0"), ("0.1.0", "0.1.99"), ("1.0.0", "1.0.99")] {
            XCTAssertNil(refusal(version, asReader: reader), "\(version) read by \(reader)")
        }
    }

    /// The shipped reader is a 0.1 reader, and that is not an accident of
    /// configuration.
    func testTheShippedReaderIsTheOneTheLibraryDeclares() {
        XCTAssertEqual(DocumentDecoder().readerVersion.display, VNX.version)
        XCTAssertEqual(VNX.version, "0.1.0")
    }
}
