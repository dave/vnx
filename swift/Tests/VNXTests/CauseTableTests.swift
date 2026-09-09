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
            let sidecar = try Corpus.Sidecar(url.deletingPathExtension().appendingPathExtension("expect.json"))
            guard let expected = sidecar.refusal else { continue }
            let bytes = try Data(contentsOf: url)
            do {
                if url.pathExtension == "vnz" { _ = try VNX.readContainer(bytes) } else { _ = try VNX.read(bytes) }
                XCTFail("\(url.lastPathComponent): expected a refusal")
            } catch let refusal as Refusal {
                XCTAssertTrue(refusal.message.contains(expected))
                observed.append((refusal.cause, expected))
            }
        }

        for url in try Corpus.cases(in: "diagnose") {
            let sidecar = try Corpus.Sidecar(url.deletingPathExtension().appendingPathExtension("expect.json"))
            let reported = try VNX.read(try Data(contentsOf: url)).diagnostics
            var used = [Bool](repeating: false, count: reported.count)
            for e in sidecar.diagnostics ?? [] {
                var found = false
                for (i, r) in reported.enumerated() where !used[i] {
                    guard r.path == e.path, Corpus.satisfies(r, e) else { continue }
                    used[i] = true
                    // `message_advisory` is what the older corpus matched on,
                    // and it is what this library's table records. Nothing
                    // ASSERTS it — SPEC 6.3 forbids comparing a diagnostic's
                    // English — but keeping the mapping honest is what stops
                    // the table drifting away from the wording it describes.
                    if let substring = e.advisory { observed.append((r.cause, substring)) }
                    found = true
                    break
                }
                XCTAssertTrue(found, "\(url.lastPathComponent): nothing matched \(e.path)")
            }
        }
        return observed
    }

    /// Forward: every cause the corpus exercises maps, in the table, to exactly
    /// the substring that corpus expectation used.
    func testEveryObservedCauseMapsToTheSubstringTheCorpusUsed() throws {
        for (cause, substring) in try observe() {
            XCTAssertEqual(Cause.corpusSubstring[cause], substring,
                           "cause `\(cause.rawValue)` satisfied the corpus expectation \"\(substring)\", which the table does not record")
        }
    }

    /// Backward: the table has no entry the corpus does not exercise. This is
    /// the direction that goes stale silently — a message reworded, an
    /// expectation deleted from a sidecar, a cause that stopped firing — and a
    /// forward-only check stays green through all three.
    func testTableHasNoEntryTheCorpusDoesNotExercise() throws {
        let observedCauses = Set(try observe().map(\.cause))
        let tabled = Set(Cause.corpusSubstring.keys)
        XCTAssertEqual(tabled.subtracting(observedCauses), [],
                       "these causes are in the table but no corpus case reaches them any more")
        XCTAssertEqual(observedCauses.subtracting(tabled), [],
                       "these causes satisfy a corpus expectation but are missing from the table")
    }

    /// Every message a cause can produce actually contains the substring the
    /// table promises. Asserted on live output rather than on a template, so a
    /// message rewritten in the decoder cannot drift away from the table
    /// without this going red.
    func testTabledSubstringsAppearInLiveMessages() throws {
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
        for (cause, substring) in Cause.corpusSubstring {
            guard let messages = seen[cause] else {
                XCTFail("no corpus case produced cause `\(cause.rawValue)`")
                continue
            }
            for message in messages {
                XCTAssertTrue(message.contains(substring),
                              "a `\(cause.rawValue)` message does not contain the tabled substring \"\(substring)\": \(message)")
            }
        }
    }

    /// The `code` vocabulary, in both directions against the corpus. The
    /// corpus adopted codes on 2026-09-09, ahead of SPEC 6.3, which still lists
    /// only `path` and `message`.
    func testCorpusCodeTableMatchesTheSidecars() throws {
        var sidecarCodes: Set<String> = []
        for url in try Corpus.cases(in: "diagnose") {
            let sidecar = try Corpus.Sidecar(url.deletingPathExtension().appendingPathExtension("expect.json"))
            for e in sidecar.diagnostics ?? [] {
                guard let code = e.code else { continue }
                sidecarCodes.insert(code)
            }
        }
        guard !sidecarCodes.isEmpty else {
            // The corpus has gone back to English-only expectations. That is a
            // real change and this test says so rather than passing silently.
            XCTFail("no sidecar names a `code`; the corpus code vocabulary has been withdrawn and `Cause.corpusCode` is now stale")
            return
        }
        XCTAssertEqual(Set(Cause.corpusCode.values), sidecarCodes,
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
        XCTAssertEqual(Set(Cause.allCases.map(\.rawValue)), [
            "malformedJSON", "notAVNXDocument", "versionMalformed", "versionTooNew",
            "nonFiniteNumber", "containerMalformed", "containerPathEscapes",
            "containerDuplicateEntry", "containerNoDocument",
            "containerRootCompressionUnsupported",
            "unknownKey", "requiredValueUnusable", "optionalValueUnusable",
            "positionUnusable", "importanceOutOfRange", "timestampNotUTC",
            "dataRecordTypeUnusable", "polygonNestedRings", "polygonUnclosed",
            "linkArcNotInNetwork", "linkNodeNotInNetwork", "arcNodeNotInNetwork",
            "inlineEndpointBeyondTolerance", "inlineEndpointOnEmptyArc",
            "chainNotConnected", "folderNotPresent",
            "attachmentCompressionUnsupported",
        ])
    }
}
