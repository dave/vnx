import Foundation
import XCTest

@testable import VNX

/// The sidecar field guard.
///
/// A sidecar field this runner does not read is a field that quietly stops
/// being tested: the expectation sits in the corpus looking like coverage and
/// nothing compares it. Python's runner and Kotlin's have failed on one since
/// they were written; this one did not, and on 2026-09-13 a new `schema` field
/// landed in thirty sidecars and passed here in silence while both of those
/// went red.
///
/// So the sets below are not documentation. They are the claim that every field
/// in every sidecar is either read by this runner or knowingly skipped, and the
/// two tests are what make the claim checkable.
final class SidecarFieldsTests: XCTestCase {
  /// Fields this runner compares. `kind` and `note` are structural.
  static let checked: Set<String> = [
    "kind", "note", "counts", "positions", "tracks",
    "attachments", "attachment_contents", "root", "refusal", "diagnostics",
  ]

  /// Fields this runner knowingly does not compare, and why.
  static let unchecked: Set<String> = [
    // A prose record of a limitation (SPEC 6.4), paired with an empty
    // `diagnostics` list which IS checked.
    "cannot_detect",
    // The verdict corpus/schema/vnx-0.9.schema.json reaches on this case.
    // It is about the schema rather than about any reader, so no
    // implementation runner compares it — go/internal/vnxschema is the one
    // thing that does, and it fails if a case records no verdict.
    "schema",
  ]

  private func sidecars() throws -> [(name: String, fields: [String])] {
    var out: [(String, [String])] = []
    for kind in ["accept", "reject", "diagnose", "write"] {
      let dir = Corpus.root.appendingPathComponent(kind)
      guard let names = try? FileManager.default.contentsOfDirectory(atPath: dir.path) else {
        continue
      }
      for name in names.sorted() where name.hasSuffix(".expect.json") {
        let value = try JSONParser.parse(try Data(contentsOf: dir.appendingPathComponent(name)))
        guard let object = value.objectValue else {
          XCTFail("\(name) is not a JSON object")
          continue
        }
        out.append((name, object.keys.map(\.string)))
      }
    }
    return out
  }

  func testTheRunnerKnowsEverySidecarField() throws {
    let found = try sidecars()
    // The control. A sweep that read no sidecars would report a clean result.
    XCTAssertGreaterThanOrEqual(
      found.count, 20,
      "read only \(found.count) sidecars — dead probe, not a clean sweep")
    let known = Self.checked.union(Self.unchecked)
    for (name, fields) in found {
      for field in fields where !known.contains(field) {
        XCTFail(
          "\(name): this runner does not know '\(field)', so the expectation would pass without being compared"
        )
      }
    }
  }

  func testNoDeclaredFieldHasOutlivedTheSidecarsThatCarriedIt() throws {
    // A stale entry is worse than a missing one: it makes the mechanism
    // look more thorough than it is, and nothing goes red when the field
    // goes away.
    var used = Set<String>()
    for (_, fields) in try sidecars() { used.formUnion(fields) }
    for field in Self.checked.union(Self.unchecked).sorted() {
      XCTAssertTrue(
        used.contains(field),
        "'\(field)' is declared here and no sidecar carries it any more; delete the entry")
    }
  }
}
