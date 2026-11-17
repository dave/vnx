import Foundation
import XCTest

@testable import VNX

/// The frozen proof that the accept tier's check is worth what SPEC 8.1 claims
/// for it.
///
/// ⚠ THIS IS WHY THE TIER COMPARES AGAINST THE ORIGINAL FILE AND NOT AGAINST
/// ANOTHER MODEL. A reader that discarded every data record or every
/// application key produces a model that compares equal to itself and passes a
/// model-versus-model tier GREEN. That is the entire extensibility mechanism,
/// silently droppable by a "conforming" implementation. Measured in Python, re-proved in Go, and each
/// case below is one of those readers.
///
/// If any of them stops being caught, the tier has gone back to proving nothing
/// about the model.
final class AcceptTierProofTests: XCTestCase {

  private func exampleNetwork() throws -> Data {
    try Data(contentsOf: Corpus.root.appendingPathComponent("accept/example-network.vnx"))
  }

  func testTheAcceptTierCatchesAReaderThatDiscardsWhatItRead() throws {
    let cases: [(String, (inout Document) -> Void)] = [
      (
        "drops every data record",
        { d in
          for i in d.points?.indices ?? (0..<0) { d.points?[i].data = nil }
          for i in d.networks?.indices ?? (0..<0) {
            d.networks?[i].data = nil
            for key in d.networks?[i].arcs?.keys ?? [:].keys {
              d.networks?[i].arcs?[key]?.data = nil
            }
            for j in d.networks?[i].highlights?.indices ?? (0..<0) {
              d.networks?[i].highlights?[j].data = nil
            }
            for j in d.networks?[i].points?.indices ?? (0..<0) {
              d.networks?[i].points?[j].data = nil
            }
          }
        }
      ),
      ("drops every application key", { d in d.applicationKeys = [] }),
    ]

    let file = try exampleNetwork()
    for (name, spoil) in cases {
      var document = try VNX.read(file).document
      spoil(&document)
      let written = try VNX.write(document)

      // The spoiled reader must be CAUGHT by the tree comparison.
      let treeDiff = try Tree.difference(got: written, want: file, Tree.Options(emitBoundary: true))
      XCTAssertNotNil(
        treeDiff, "\(name): the tree comparison did not catch it, so the accept tier proves nothing"
      )

      // And the model-versus-model comparison the tier must NOT use is
      // what would have let it through. Asserting this rather than
      // describing it is what stops the tier drifting back.
      XCTAssertEqual(
        document, try VNX.read(written).document,
        "\(name): a model-versus-model tier passes this reader green")
    }
  }

  /// The unspoiled file passes, or the test above would be reporting a
  /// difference it did not cause.
  func testTheUnspoiledFilePasses() throws {
    let file = try exampleNetwork()
    let written = try VNX.write(try VNX.read(file).document)
    XCTAssertNil(try Tree.difference(got: written, want: file, Tree.Options(emitBoundary: true)))
  }
}
