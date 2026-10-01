import Foundation
import XCTest

@testable import VNX

/// SPEC 3.9: a top-level key containing a full stop is an application key. Its
/// value is any JSON value, kept verbatim and never reported. The corpus
/// round-trips a file holding several; these pin the rule directly.
final class ApplicationKeyTests: XCTestCase {

  func testApplicationKeysArePreservedSortedAndUnreported() throws {
    let src = Data(
      """
      {"vnx":"0.8.0",
       "org.example.z": {"anything":{"at":["all"]}},
       "app.viatrail.rulesets": [{"name":"Hide minor"}],
       "com.example.null": null,
       "com.example.empty": [],
       "com.example.string": "text"}
      """.utf8)
    let read = try VNX.read(src)
    XCTAssertEqual(read.diagnostics, [], "an application key must not be reported")
    XCTAssertEqual(
      read.document.applicationKeys.map(\.key),
      [
        "app.viatrail.rulesets", "com.example.empty", "com.example.null",
        "com.example.string", "org.example.z",
      ])
    XCTAssertEqual(
      read.document.applicationKeys[2].value, .null,
      "a null value is a present key holding null")
    XCTAssertEqual(read.document.applicationKeys[1].value, .array([]))
  }

  /// The rule is TOP-LEVEL ONLY. A dotted key on any other object is an
  /// unrecognised key; an undotted unknown key at the top level still is too.
  func testADottedKeyBelowTheTopLevelIsReported() throws {
    let read = try VNX.read(
      Data(
        """
        {"vnx":"0.8.0","rulesets":[],"waypoints":[{"at":[1,2],"app.viatrail.x":1}]}
        """.utf8))
    let got = Set(read.diagnostics.map { "\($0.path) \($0.code)" })
    XCTAssertEqual(got, ["/rulesets unknown-key", "/waypoints/0/app.viatrail.x unknown-key"])
  }

  /// A null stays null and an empty value stays empty through a
  /// read-modify-write: SPEC 3.9 exempts an application key from SPEC 8.1's
  /// absent-equals-empty normalisation.
  func testApplicationKeysSurviveAReadModifyWrite() throws {
    let src = Data(
      """
      {"vnx":"0.8.0","app.viatrail.rulesets":[{"zebra":1,"apple":[2,{"nested":true}]}],
       "com.example.null":null,"com.example.empty-array":[],"com.example.empty-object":{}}
      """.utf8)
    let written = try VNX.write(try VNX.read(src).document)
    XCTAssertNil(try Tree.difference(got: written, want: src))
  }

  /// The comparison must not normalise an application key's empty value
  /// away, or the round trip above could not fail on a writer that dropped it.
  func testTheTreeComparisonSeesADroppedEmptyApplicationKey() throws {
    XCTAssertNotNil(
      try Tree.difference(
        got: Data(#"{"vnx":"0.8.0"}"#.utf8),
        want: Data(#"{"vnx":"0.8.0","com.example.empty":[]}"#.utf8)))
  }

  /// A key with no full stop would be read back as an unrecognised key and
  /// dropped, and a key held twice leaves the surviving value to the reader's
  /// parser (SPEC 6.4). The writer refuses both.
  func testWriteRefusesAnApplicationKeyItCannotWrite() {
    let cases: [(String, [ApplicationKey], String)] = [
      ("no full stop", [ApplicationKey(key: "rulesets", value: .array([]))], "no full stop"),
      ("a format key", [ApplicationKey(key: "folders", value: .array([]))], "no full stop"),
      ("empty", [ApplicationKey(key: "", value: .null)], "no full stop"),
      (
        "twice",
        [
          ApplicationKey(key: "a.b", value: .number(1)),
          ApplicationKey(key: "a.b", value: .number(2)),
        ], "twice"
      ),
    ]
    for (name, keys, want) in cases {
      var d = Document()
      d.applicationKeys = keys
      XCTAssertThrowsError(try VNX.write(d), name) { error in
        guard let e = error as? ApplicationKeyError else {
          return XCTFail("\(name): threw \(error), not ApplicationKeyError")
        }
        XCTAssertTrue(e.message.contains(want), "\(name): \(e.message)")
      }
    }
  }
}
