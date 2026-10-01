import Foundation
import XCTest

@testable import VNX

/// What the corpus's comparison may and may not see.
///
/// The comparator is an instrument, and an instrument that answers fluently in
/// the wrong units passes everything. These pin the two axes it must be blind
/// to, and the liveness that stops the blindness becoming blanket agreement.
final class TreeComparisonTests: XCTestCase {

  private func doc(_ body: String) -> Data { Data("{\"vnx\":\"0.8.0\",\(body)}".utf8) }

  /// ⚠ A NUMBER COMPARES BY VALUE, NEVER BY ITS SPELLING. SPEC 4.2 defines the
  /// round trip at value level and the rewritten specification states no rule
  /// about how a number is written, so `1`, `1.0` and `1e0` are one number.
  /// Python spells an integral double `1.0` where Go spells it `1`, and a
  /// later task has every implementation read every other's output — a
  /// spelling-sensitive comparator fails that task and looks like an encoder
  /// bug rather than a comparator bug.
  ///
  /// Swift reaches this for free, and the reason is worth stating rather than
  /// relying on: `JSONValue` has ONE number case, carrying a `Double`, so
  /// there is no integer box that could compare unequal to a floating one.
  /// A comparison built on `JSONSerialization` would have `NSNumber`, which
  /// does have that trap.
  func testNumbersCompareByValueAndNotBySpelling() throws {
    for (a, b) in [
      ("1", "1.0"), ("1", "1e0"), ("340", "3.4e2"), ("0.1", "1e-1"),
      ("0", "-0"), ("1778914991000", "1.778914991e12"),
    ] {
      let x = doc("\"waypoints\":[{\"at\":[\(a),2]}]")
      let y = doc("\"waypoints\":[{\"at\":[\(b),2]}]")
      XCTAssertNil(
        try Tree.difference(got: x, want: y),
        "\(a) and \(b) are one number spelled two ways")
    }
  }

  /// LIVENESS. A comparator that returned `nil` for everything would pass the
  /// test above and every corpus tier with it.
  func testADifferentValueIsStillReported() throws {
    let x = doc("\"waypoints\":[{\"at\":[1,2]}]")
    let y = doc("\"waypoints\":[{\"at\":[1.0000001,2]}]")
    XCTAssertEqual(
      try Tree.difference(got: x, want: y), "/waypoints/0/at/0: got 1.0, want 1.0000001")
  }

  /// SPEC 3.1: an absent collection and an empty one carry the same content,
  /// and a reader MUST treat them alike — so the comparison does too, which is
  /// what keeps a writer that omits an empty collection conforming.
  func testAnAbsentCollectionAndAnEmptyOneCompareEqual() throws {
    XCTAssertNil(
      try Tree.difference(got: doc("\"waypoints\":[]"), want: Data("{\"vnx\":\"0.8.0\"}".utf8)))
    XCTAssertNil(
      try Tree.difference(
        got: doc("\"waypoints\":[{\"at\":[1,2],\"data\":[]}]"),
        want: doc("\"waypoints\":[{\"at\":[1,2]}]")))
  }

  /// And the two normalisations SPEC 8.1 permits are the ONLY two. A style is
  /// opaque, so an empty container inside one is content a third party meant
  /// to keep, and a trailing `null` inside one is not a position's channel to
  /// be dropped.
  func testNothingIsNormalisedInsideAnOpaqueRegion() throws {
    XCTAssertNotNil(
      try Tree.difference(
        got: doc("\"waypoints\":[{\"at\":[1,2],\"style\":{\"x\":[]}}]"),
        want: doc("\"waypoints\":[{\"at\":[1,2],\"style\":{}}]")))
    XCTAssertNotNil(
      try Tree.difference(
        got: doc("\"waypoints\":[{\"at\":[1,2],\"style\":{\"v\":[1,2,null]}}]"),
        want: doc("\"waypoints\":[{\"at\":[1,2],\"style\":{\"v\":[1,2]}}]"),
        Tree.Options(emitBoundary: true)))
  }

  /// The shortest-spelling rule is applied to BOTH SIDES in the accept tier,
  /// and to neither in the write tier — where it is the rule under test.
  func testTheEmitBoundaryNormalisationIsOptional() throws {
    let long = doc("\"waypoints\":[{\"at\":[1.00000005,2,340.25,null]}]")
    let short = doc("\"waypoints\":[{\"at\":[1.00000005,2,340.25]}]")
    XCTAssertNil(try Tree.difference(got: long, want: short, Tree.Options(emitBoundary: true)))
    XCTAssertNotNil(try Tree.difference(got: long, want: short))
  }

  /// ⚠ AND IT NORMALISES NO VALUE, at either setting. The comparator rounded
  /// channels 0–2 while SPEC 4.1 required a writer to round; with that rule
  /// gone, a comparator that still rounded would call a writer that lost
  /// digits equal to one that kept them.
  func testTheEmitBoundaryNormalisationDoesNotTouchValues() throws {
    let precise = doc("\"waypoints\":[{\"at\":[1.00000005,2,340.25]}]")
    let rounded = doc("\"waypoints\":[{\"at\":[1.0000001,2,340.2]}]")
    XCTAssertNotNil(
      try Tree.difference(got: precise, want: rounded, Tree.Options(emitBoundary: true)),
      "a lost digit is a difference, not a normalisation")
  }
}
