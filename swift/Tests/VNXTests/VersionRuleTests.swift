import Foundation
import XCTest

@testable import VNX

/// SPEC 2.2's version rules, exercised from BOTH sides of 1.0.
///
/// The library is a 0.x reader, so the 1.0-and-later branch of the rule — minor
/// versions are additive, and only a MAJOR increment refuses — is unreachable
/// through the public API. Every mutation of that branch therefore stays green
/// against a suite that only ever reads as a 0.x reader, which is how an
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
    XCTAssertEqual(r?.cause, .versionUnsupportedSeries)
    XCTAssertTrue(
      r?.message.contains("MUST NOT read another one under its own rules") ?? false,
      "refused for being another series: \(r?.message ?? "not refused at all")")
  }

  /// From 1.0 onward, minor versions are additive: a reader accepts any minor
  /// at or above its own, and only a MAJOR increment refuses. This branch does
  /// not exist for a 0.x reader.
  func testFromOneOnwardMinorVersionsAreAdditive() throws {
    for version in ["1.0.0", "1.0.99", "1.5.0", "1.99.3"] {
      XCTAssertNoThrow(try read(version, asReader: "1.0.0"), version)
    }
    XCTAssertEqual(
      try read("1.5.0", asReader: "1.0.0").vnx, "1.5.0",
      "and a reader that re-encodes writes the version the document declared")
    XCTAssertEqual(refusal("2.0.0", asReader: "1.0.0")?.cause, .versionUnsupportedSeries)
  }

  /// Through 0.x each MINOR is its own series, so the same document shape
  /// refuses where the 1.x rule would accept. The pair is the rule.
  func testThroughZeroEachMinorIsItsOwnSeries() {
    XCTAssertEqual(refusal("0.5.0", asReader: "0.1.0")?.cause, .versionUnsupportedSeries)
    XCTAssertTrue(
      refusal("0.5.0", asReader: "0.1.0")?.message.contains("0.5 series") ?? false,
      "the refusal names the DOCUMENT's series, which is the one the holder needs a reader for")
    XCTAssertNil(
      refusal("1.5.0", asReader: "1.1.0"),
      "the identical minor gap is fine once the document's major is 1")
  }

  /// SPEC 2.2: "It refuses 0.2.0, and it refuses 0.0.9 unless it declares
  /// support for the 0.0 series. A lower minor is refused for the same reason
  /// as a higher one."
  ///
  /// BOTH DIRECTIONS AND THE MIDDLE, in one test, because the defect this
  /// pins was a reader that had only the upward half: `minorOrder > 0` passed
  /// every case the suite held, and read a 0.0.9 document under 0.1's rules.
  /// A fix that swapped the comparison rather than widening it would pass the
  /// downward case and fail the other two, which is why the accepted version
  /// sits here beside them rather than in a test of its own.
  func testALowerMinorIsRefusedForTheSameReasonAsAHigherOne() {
    let lower = refusal("0.0.9", asReader: "0.1.0")
    XCTAssertEqual(lower?.cause, .versionUnsupportedSeries, "0.0.9 is not an older spelling of 0.1")
    XCTAssertTrue(
      lower?.message.contains("0.0 series") ?? false,
      "the refusal names the series: \(lower?.message ?? "not refused at all")")
    XCTAssertTrue(lower?.message.contains("0.0.9") ?? false, "and the version itself, per SPEC 2.2")

    let higher = refusal("0.2.0", asReader: "0.1.0")
    XCTAssertEqual(higher?.cause, .versionUnsupportedSeries)
    XCTAssertTrue(higher?.message.contains("0.2 series") ?? false)

    XCTAssertNil(
      refusal("0.1.5", asReader: "0.1.0"),
      "a PATCH above the reader's is inside the reader's own series and is read")
  }

  /// The two directions differ in their English and nowhere else. A holder of
  /// a 0.0.9 document told "a newer reader is needed" goes looking for a
  /// reader that will never exist.
  func testTheRefusalDoesNotTellTheHolderOfAnOlderDocumentToUpgrade() {
    XCTAssertFalse(refusal("0.0.9", asReader: "0.1.0")?.message.contains("newer reader") ?? true)
    XCTAssertFalse(refusal("0.9.0", asReader: "1.0.0")?.message.contains("newer reader") ?? true)
    XCTAssertTrue(
      refusal("9.9.9", asReader: "0.1.0")?.message.contains("a newer reader is needed") ?? false)
  }

  /// PATCH never refuses, at any version, on either side of 1.0.
  func testPatchNeverRefuses() {
    for (version, reader) in [
      ("0.1.99", "0.1.0"), ("1.0.99", "1.0.0"), ("0.1.0", "0.1.99"), ("1.0.0", "1.0.99"),
    ] {
      XCTAssertNil(refusal(version, asReader: reader), "\(version) read by \(reader)")
    }
  }

  /// The shipped reader is a 0.2 reader, and that is not an accident of
  /// configuration.
  func testTheShippedReaderIsTheOneTheLibraryDeclares() {
    XCTAssertEqual(DocumentDecoder().readerVersion.display, VNX.version)
    XCTAssertEqual(VNX.version, "0.2.0")
  }
}
