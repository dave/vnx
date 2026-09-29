import Foundation
import XCTest

@testable import VNX

/// SPEC 6.3 gives a diagnostic `path` and `message`, and the corpus matches
/// `message` and `refusal` as ENGLISH SUBSTRINGS. That is a known defect: a
/// caller that wants to branch on what went wrong has to grep prose, and a
/// reworded message stops matching with nothing to say so.
///
/// This library therefore carries a machine `Cause` beside every refusal and
/// every diagnostic, and `Cause.corpusSubstring` maps a cause to the substring
/// the corpus expects today. The table is asserted in BOTH directions, because
/// a forward-only check cannot see the two ways it goes stale: a table entry
/// for a cause the corpus stopped exercising, and a corpus expectation this
/// library satisfies with a cause the table never mentions.
final class CauseTableTests: XCTestCase {

  /// Runs the reject and diagnose tiers and records which cause satisfied
  /// each expectation. Independent of test ordering: this test owns the run.
  private func observe() throws -> [(cause: Cause, substring: String)] {
    var observed: [(cause: Cause, substring: String)] = []

    for url in try Corpus.cases(in: "reject") {
      let sidecar = try Corpus.Sidecar(
        url.deletingPathExtension().appendingPathExtension("expect.json"))
      guard let expected = sidecar.refusal else { continue }
      let bytes = try Data(contentsOf: url)
      do {
        if url.pathExtension == "vnz" {
          _ = try VNX.readContainer(bytes)
        } else {
          _ = try VNX.read(bytes)
        }
        XCTFail("\(url.lastPathComponent): expected a refusal")
      } catch let refusal as Refusal {
        XCTAssertTrue(refusal.message.contains(expected))
        observed.append((refusal.cause, expected))
      }
    }

    for url in try Corpus.cases(in: "diagnose") {
      let sidecar = try Corpus.Sidecar(
        url.deletingPathExtension().appendingPathExtension("expect.json"))
      let reported = try Corpus.read(url).diagnostics
      var used = [Bool](repeating: false, count: reported.count)
      for e in sidecar.diagnostics ?? [] {
        var found = false
        for (i, r) in reported.enumerated() where !used[i] {
          guard r.path == e.path, Corpus.satisfies(r, e) else { continue }
          used[i] = true
          // Deliberately NOT recorded, and this is a change: the
          // advisory used to be mapped here "to keep the table
          // honest". That put a diagnostic's English back into the
          // English table one layer down, and made this library assert
          // that its own prose matched another implementation's — the
          // exact coupling SPEC 6.3 removed when it renamed the field
          // `message_advisory` and said the English MUST NOT be
          // compared. Diagnostics are covered by
          // `testCorpusCodeTableMatchesTheSidecars`; the English table
          // is for refusals, which have no code yet.
          found = true
          break
        }
        XCTAssertTrue(found, "\(url.lastPathComponent): nothing matched \(e.path)")
      }
    }
    return observed
  }

  /// Forward: every REFUSAL cause the corpus exercises maps, in the table, to
  /// a set of substrings that holds exactly the one that corpus expectation
  /// used.
  func testEveryObservedCauseMapsToTheSubstringTheCorpusUsed() throws {
    for (cause, substring) in try observe() {
      XCTAssertTrue(
        Cause.corpusSubstring[cause]?.contains(substring) ?? false,
        "cause `\(cause.rawValue)` satisfied the corpus expectation \"\(substring)\", which the table does not record"
      )
    }
  }

  /// Backward: the table has no entry the corpus does not exercise. This is
  /// the direction that goes stale silently — a message reworded, an
  /// expectation deleted from a sidecar, a cause that stopped firing — and a
  /// forward-only check stays green through all three.
  func testTableHasNoEntryTheCorpusDoesNotExercise() throws {
    let observed = try observe()
    let observedCauses = Set(observed.map(\.cause))
    let tabled = Set(Cause.corpusSubstring.keys)
    XCTAssertEqual(
      tabled.subtracting(observedCauses), [],
      "these causes are in the table but no corpus case reaches them any more")
    XCTAssertEqual(
      observedCauses.subtracting(tabled), [],
      "these causes satisfy a corpus expectation but are missing from the table")

    // And per SUBSTRING, not only per cause. A cause carrying several —
    // `versionUnsupportedSeries` does — would otherwise keep a stale entry
    // alive on the strength of its sibling: the cause is still observed, so
    // the check above stays green while the substring nothing expects any
    // more sits in the table unnoticed.
    let observedSubstrings = Set(observed.map(\.substring))
    let tabledSubstrings = Set(Cause.corpusSubstring.values.flatMap { $0 })
    XCTAssertEqual(
      tabledSubstrings.subtracting(observedSubstrings), [],
      "these substrings are in the table but no corpus expectation uses them any more")
    XCTAssertEqual(
      observedSubstrings.subtracting(tabledSubstrings), [],
      "the corpus expects these substrings and the table does not record them")
  }

  /// Every message a cause can produce actually contains the substring the
  /// table promises. Asserted on live output rather than on a template, so a
  /// message rewritten in the decoder cannot drift away from the table
  /// without this going red.
  func testTabledSubstringsAppearInLiveRefusals() throws {
    var seen: [Cause: [String]] = [:]
    for tier in ["reject", "diagnose", "accept"] {
      for url in try Corpus.cases(in: tier) {
        let bytes = try Data(contentsOf: url)
        do {
          let diagnostics: [Diagnostic]
          if url.pathExtension == "vnz" {
            diagnostics = try VNX.readContainer(bytes).diagnostics
          } else {
            diagnostics = try VNX.read(bytes).diagnostics
          }
          for d in diagnostics { seen[d.cause, default: []].append(d.message) }
        } catch let refusal as Refusal {
          seen[refusal.cause, default: []].append(refusal.message)
        }
      }
    }
    for (cause, substrings) in Cause.corpusSubstring {
      guard let messages = seen[cause] else {
        XCTFail("no corpus case produced cause `\(cause.rawValue)`")
        continue
      }
      // Every message this cause produces carries one of the tabled
      // substrings, AND every tabled substring is carried by one of them.
      // The second half is what stops a cause with two spellings passing
      // while only ever emitting the first.
      for message in messages {
        XCTAssertTrue(
          substrings.contains(where: message.contains),
          "a `\(cause.rawValue)` message contains none of the tabled substrings \(substrings.sorted()): \(message)"
        )
      }
      for substring in substrings {
        XCTAssertTrue(
          messages.contains { $0.contains(substring) },
          "no live `\(cause.rawValue)` message contains the tabled substring \"\(substring)\"")
      }
    }
  }

  /// The `code` vocabulary, in both directions against the corpus. SPEC 6.2's
  /// remedy table names every code, and the corpus pins each one.
  func testCorpusCodeTableMatchesTheSidecars() throws {
    var sidecarCodes: Set<String> = []
    for url in try Corpus.cases(in: "diagnose") {
      let sidecar = try Corpus.Sidecar(
        url.deletingPathExtension().appendingPathExtension("expect.json"))
      for e in sidecar.diagnostics ?? [] {
        guard let code = e.code else { continue }
        sidecarCodes.insert(code)
      }
    }
    guard !sidecarCodes.isEmpty else {
      // The corpus has gone back to English-only expectations. That is a
      // real change and this test says so rather than passing silently.
      XCTFail(
        "no sidecar names a `code`; the corpus code vocabulary has been withdrawn and `Cause.corpusCode` is now stale"
      )
      return
    }
    XCTAssertEqual(
      Set(Cause.corpusCode.values), sidecarCodes,
      "`Cause.corpusCode` and the corpus disagree about which codes exist")
  }

  /// Every cause has a code, and no two causes share one. A reason code that
  /// collides is worse than no reason code: it invites a caller to branch on
  /// it and then sends two different faults down one branch.
  func testCodesAreUniqueAndTotal() {
    let codes = Cause.allCases.map(\.code)
    XCTAssertEqual(Set(codes).count, codes.count, "two causes share a code")
    for cause in Cause.allCases {
      XCTAssertFalse(cause.code.isEmpty)
      XCTAssertEqual(cause.code, cause.code.lowercased(), "\(cause.rawValue): codes are kebab-case")
    }
  }

  /// A cause is a stable identifier a caller may switch on, so its spelling
  /// is part of the API surface. Pinned so that renaming one is a decision
  /// rather than a refactor.
  func testCauseNamesArePinned() {
    XCTAssertEqual(
      Set(Cause.allCases.map(\.rawValue)),
      [
        "malformedJSON", "notAVNXDocument", "versionMalformed",
        "versionUnsupportedSeries",
        "nonFiniteNumber", "containerMalformed", "containerPathEscapes",
        "containerDuplicateEntry", "containerNoDocument",
        "containerRootCompressionUnsupported",
        "unknownKey", "wrongTypeRequired", "wrongTypeOptional",
        "positionUnusable", "importanceOutOfRange", "timestampNotUTC",
        "polygonNestedRings", "ringClosed", "ringTooFewPoints",
        "extraChannel", "arcPipTime", "selfLoopArc", "arcNodeAbsent",
        "passArcAbsent", "walkSpelling", "walkNotConnected", "pinnedTurn",
        "routePinOrPosition", "folderNotPresent", "attachmentUnreadable",
      ])
  }
}
