import Foundation
import XCTest

@testable import VNX

/// Self-tests for the two places the corpus runner refuses rather than tolerates.
///
/// SPEC 6.3: a diagnostic a corpus case pins MUST carry a `code`, and "The
/// English MUST NOT be compared". A runner that fell back to an English
/// substring when a sidecar named no code would still be green on today's
/// corpus — every sidecar carries a code, measured 2026-09-09 — and would
/// silently start comparing translations of a rule the day somebody added one
/// that did not. A tolerance nothing exercises is not a tolerance; it is a
/// latent behaviour change.
///
/// These build sidecars on disk rather than asserting about the corpus, because
/// a guard tested only against inputs that satisfy it has not been tested. The
/// corpus itself is never written to.
final class SidecarStrictnessTests: XCTestCase {

  private var directory: URL!

  override func setUpWithError() throws {
    directory = URL(fileURLWithPath: NSTemporaryDirectory())
      .appendingPathComponent("vnx-sidecar-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
  }

  override func tearDownWithError() throws {
    try? FileManager.default.removeItem(at: directory)
  }

  private func sidecar(_ json: String) throws -> URL {
    let url = directory.appendingPathComponent("case.expect.json")
    try Data(json.utf8).write(to: url)
    return url
  }

  /// The shape every real sidecar has. Without this the two refusals below
  /// prove only that the parser rejects something, not that it rejects the
  /// thing they name.
  func testAWellFormedSidecarIsAccepted() throws {
    let url = try sidecar(
      """
      {"kind": "diagnose", "note": "a control, so the refusals below discriminate",
       "diagnostics": [{"path": "/points/0/nmae", "code": "unknown-key",
                        "message_advisory": "unknown key"}]}
      """)
    let parsed = try Corpus.Sidecar(url)
    XCTAssertEqual(parsed.diagnostics?.count, 1)
    XCTAssertEqual(parsed.diagnostics?.first?.code, "unknown-key")
    XCTAssertEqual(parsed.diagnostics?.first?.advisory, "unknown key")
  }

  /// A pinned diagnostic naming no `code` has nothing a runner may assert.
  func testASidecarWithNoCodeIsRefused() throws {
    let url = try sidecar(
      """
      {"kind": "diagnose", "note": "no code",
       "diagnostics": [{"path": "/points/0/nmae", "message_advisory": "unknown key"}]}
      """)
    XCTAssertThrowsError(try Corpus.Sidecar(url)) { error in
      XCTAssertTrue(
        "\(error)".contains("names no `code`"),
        "the refusal must name the missing code, got: \(error)")
    }
  }

  /// A field called `message` reads as a claim about what is compared. The
  /// corpus renamed it `message_advisory` for exactly that reason, so one
  /// reappearing is a sidecar written against the older rule.
  func testASidecarCarryingMessageIsRefused() throws {
    let url = try sidecar(
      """
      {"kind": "diagnose", "note": "the older English form",
       "diagnostics": [{"path": "/points/0/nmae", "code": "unknown-key",
                        "message": "unknown key"}]}
      """)
    XCTAssertThrowsError(try Corpus.Sidecar(url)) { error in
      XCTAssertTrue(
        "\(error)".contains("carries a `message`"),
        "the refusal must name the offending field, got: \(error)")
    }
  }

  /// The state of the corpus this strictness assumes. If a sidecar ever
  /// carries `message` again, this goes red HERE — naming the corpus — rather
  /// than as an opaque parse failure in whichever tier happened to load it.
  func testNoSidecarInTheCorpusCarriesMessage() throws {
    for tier in ["accept", "reject", "diagnose"] {
      for url in try Corpus.cases(in: tier) {
        let expect = url.deletingPathExtension().appendingPathExtension("expect.json")
        guard FileManager.default.fileExists(atPath: expect.path) else { continue }
        let text = String(decoding: try Data(contentsOf: expect), as: UTF8.self)
        XCTAssertFalse(
          text.contains("\"message\""),
          "\(expect.lastPathComponent) carries a `message` field; SPEC 6.3 forbids comparing a diagnostic's English"
        )
      }
    }
  }
}
