import Foundation
import XCTest
@testable import VNX

/// Runs the conformance corpus at `corpus/`, which SPEC 8 makes the thing that
/// decides whether this implementation conforms.
///
/// The runner is deliberately strict in the three places the corpus README says
/// a lax runner passes a broken reader:
///
/// - A file with no sidecar is a FAILURE, not a skip.
/// - The accept tier compares the tree parsed from the file against the tree
///   parsed from `write(read(file))`. Never one decoded model against another,
///   which cannot see a reader that threw away what it read.
/// - Every `write/` input is asserted NOT to be the written form already, or a
///   writer that echoed its input would pass the whole tier.
enum Corpus {
    static let root: URL = {
        // .../vnx/swift/Tests/VNXTests/CorpusRunner.swift
        URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()   // VNXTests
            .deletingLastPathComponent()   // Tests
            .deletingLastPathComponent()   // swift
            .deletingLastPathComponent()   // vnx
            .appendingPathComponent("corpus")
    }()

    /// Reads a corpus case, dispatching on its extension.
    ///
    /// A `.vnz` decoded as JSON fails at byte 0 on the `P` of `PK`, which reads
    /// as a malformed document rather than as a runner that forgot a tier can
    /// hold an archive.
    static func read(_ url: URL) throws -> (document: Document, diagnostics: [Diagnostic], attachments: [Attachment]) {
        let bytes = try Data(contentsOf: url)
        if url.pathExtension == "vnz" {
            let container = try VNX.readContainer(bytes)
            return (container.document, container.diagnostics, container.attachments)
        }
        let result = try VNX.read(bytes)
        return (result.document, result.diagnostics, [])
    }

    static func cases(in tier: String) throws -> [URL] {
        let directory = root.appendingPathComponent(tier)
        let names = try FileManager.default.contentsOfDirectory(atPath: directory.path)
        return names.sorted()
            .filter { $0.hasSuffix(".vnx") || $0.hasSuffix(".vnz") }
            // `write/` is a pair tier: its halves are named `X.in.vnx` and
            // `X.written.vnx`, and neither half is a case on its own.
            .filter { !$0.hasSuffix(".in.vnx") && !$0.hasSuffix(".written.vnx")
                      && !$0.hasSuffix(".written.vnz") }
            .map { directory.appendingPathComponent($0) }
    }

    /// One case's `.expect.json` sidecar.
    struct Sidecar {
        let kind: String
        let note: String
        let counts: [String: Int]?
        let positions: [(pointer: String, lon: Double, lat: Double)]
        let tracks: [(name: String, movingTimeMilliseconds: Double?)]
        let attachments: [String]?
        /// Each attachment's BYTES, keyed by entry name, as UTF-8 text.
        ///
        /// The names listed here are exactly the ones `attachments` lists, and
        /// an entry a reader dropped appears in neither —
        /// `diagnose/container-unusable-method.vnz` is the case that says so:
        /// it carries two entries, lists one, and its other one is reported as
        /// a diagnostic instead. So the comparison is a whole-map comparison,
        /// which is what catches a reader that returns an attachment's bytes
        /// truncated, inflated twice, or taken from the neighbouring entry —
        /// none of which the name list can see.
        let attachmentContents: [String: String]?
        let refusal: String?
        /// The entry name a container write case expects its document under.
        let rootEntry: String?
        /// A sidecar's expected diagnostics, matched on `code` and nothing
        /// else (SPEC 6.3: "The English MUST NOT be compared").
        ///
        /// A sidecar naming no `code` is a hard error, not a case that falls
        /// back to its English — see the guard below. Measured 2026-09-09:
        /// every diagnostic-bearing sidecar carries `code` and none carries
        /// `message`, so the older English form is gone rather than tolerated.
        /// `message_advisory` is read only so a failure can quote what the case
        /// was about; by its name it is not a requirement, and nothing compares
        /// it.
        let diagnostics: [ExpectedDiagnostic]?

        struct ExpectedDiagnostic {
            let path: String
            let code: String?
            let advisory: String?
        }

        init(_ url: URL) throws {
            let value = try JSONParser.parse(try Data(contentsOf: url))
            guard let o = value.objectValue else {
                throw SidecarError("the sidecar at \(url.lastPathComponent) is not a JSON object")
            }
            guard let kind = o["kind"]?.stringValue else { throw SidecarError("no `kind` in \(url.lastPathComponent)") }
            guard let note = o["note"]?.stringValue else { throw SidecarError("no `note` in \(url.lastPathComponent)") }
            self.kind = kind
            self.note = note
            if let c = o["counts"]?.objectValue {
                self.counts = Dictionary(uniqueKeysWithValues: c.compactMap { k, v in
                    v.numberValue.map { (k.string, Int($0)) }
                })
            } else {
                self.counts = nil
            }
            self.positions = (o["positions"]?.arrayValue ?? []).compactMap { entry in
                guard let e = entry.objectValue,
                      let at = e["at"]?.stringValue,
                      let lon = e["lon"]?.numberValue,
                      let lat = e["lat"]?.numberValue else { return nil }
                return (at, lon, lat)
            }
            self.tracks = (o["tracks"]?.arrayValue ?? []).compactMap { entry in
                guard let e = entry.objectValue, let name = e["name"]?.stringValue else { return nil }
                // `moving_time_ms: null` means UNKNOWN, which is not zero.
                if let raw = e["moving_time_ms"], case .number(let d) = raw { return (name, d) }
                return (name, nil)
            }
            self.attachments = o["attachments"]?.arrayValue?.compactMap { $0.stringValue }
            if let c = o["attachment_contents"]?.objectValue {
                self.attachmentContents = try Dictionary(uniqueKeysWithValues: c.map { k, value in
                    guard let text = value.stringValue else {
                        throw SidecarError("an `attachment_contents` entry in \(url.lastPathComponent) is \(value.typeName); the corpus states each one as UTF-8 text")
                    }
                    return (k.string, text)
                })
            } else {
                self.attachmentContents = nil
            }
            self.refusal = o["refusal"]?.stringValue
            self.rootEntry = o["root"]?.stringValue
            if let d = o["diagnostics"]?.arrayValue {
                self.diagnostics = try d.map { entry in
                    guard let e = entry.objectValue, let path = e["path"]?.stringValue else {
                        throw SidecarError("a diagnostic in \(url.lastPathComponent) has no `path`")
                    }
                    let code = e["code"]?.stringValue
                    let advisory = e["message_advisory"]?.stringValue
                    if e["message"] != nil {
                        throw SidecarError("the diagnostic at \(path) in \(url.lastPathComponent) carries a `message`; SPEC 6.3 forbids comparing a diagnostic's English, and the corpus names its human-readable field `message_advisory` precisely so a field called `message` cannot read as a claim about what is compared")
                    }
                    // SPEC 6.3: a diagnostic a corpus case pins MUST carry a
                    // `code`, and the code is what the case compares.
                    guard code != nil else {
                        throw SidecarError("the diagnostic at \(path) in \(url.lastPathComponent) names no `code`; SPEC 6.3 requires one and forbids comparing the English, so there is nothing here a runner may assert")
                    }
                    return ExpectedDiagnostic(path: path, code: code, advisory: advisory)
                }
            } else {
                self.diagnostics = nil
            }
        }
    }

    struct SidecarError: Error, CustomStringConvertible {
        let description: String
        init(_ d: String) { description = d }
    }

    /// Whether a reported diagnostic satisfies a sidecar's expectation.
    ///
    /// On the `code` and nothing else. SPEC 6.3 requires a pinned diagnostic to
    /// carry one and says the English MUST NOT be compared; SPEC 8.1 was
    /// corrected on 2026-09-09 to stop contradicting it one section away.
    ///
    /// There is deliberately no English fallback here. An expectation with no
    /// `code` is not something to match loosely — it is a sidecar that pins
    /// nothing a runner may compare, and `Sidecar` refuses it outright.
    static func satisfies(_ reported: Diagnostic, _ expected: Sidecar.ExpectedDiagnostic) -> Bool {
        guard let code = expected.code else { return false }
        return reported.code == code
    }

    /// Every cause the corpus was observed to produce, recorded so that
    /// `CauseTableTests` can compare it against `Cause.corpusSubstring` in the
    /// direction a forward-only check cannot see.
    final class ObservedCauses: @unchecked Sendable {
        private let lock = NSLock()
        private var pairs: [(cause: Cause, substring: String)] = []

        func record(_ cause: Cause, matching substring: String) {
            lock.lock(); defer { lock.unlock() }
            pairs.append((cause, substring))
        }

        var all: [(cause: Cause, substring: String)] {
            lock.lock(); defer { lock.unlock() }
            return pairs
        }
    }

    static let observed = ObservedCauses()
}

extension XCTestCase {
    /// Runs one corpus case, turning any error that escapes it into a failure
    /// that NAMES the case, and letting the run continue.
    ///
    /// Without this, a `try` inside a `for` loop in a `throws` test method takes
    /// the whole loop with it. Every case after the first failure is never run,
    /// and the report is one error where the truth might be one defect or might
    /// be all of them — the two are indistinguishable from the output.
    ///
    /// The hole is invisible until the day it matters, because it only opens
    /// when something throws in a tier where nothing is expected to. The
    /// JavaScript seat hit it: a refusal raised inside a `format/` case took 34
    /// passing cases down with it, and the hole had been there since its first
    /// commit. This runner had the same hole in four tiers.
    func runCase(_ name: String, _ body: () throws -> Void) {
        do {
            try body()
        } catch let refusal as Refusal {
            XCTFail("\(name): the reader REFUSED a document this tier requires it to read — \(refusal.cause.code): \(refusal.message)")
        } catch {
            XCTFail("\(name): \(error)")
        }
    }
}

// MARK: - Resolving a sidecar `at` pointer against the decoded MODEL

extension Document {
    /// Resolves one of the corpus's `positions` pointers.
    ///
    /// Against the decoded model, never against the raw JSON. The corpus README
    /// is blunt about why: "Resolving it against the file compares the file to
    /// itself and cannot see a decoder that swapped two channels, which is the
    /// entire point of the check."
    func position(atPointer pointer: String) -> Position? {
        var tokens = pointer.split(separator: "/", omittingEmptySubsequences: true).map {
            $0.replacingOccurrences(of: "~1", with: "/").replacingOccurrences(of: "~0", with: "~")
        }
        func next() -> String? { tokens.isEmpty ? nil : tokens.removeFirst() }
        func index() -> Int? { next().flatMap(Int.init) }

        switch next() {
        case "waypoints":
            guard let i = index(), let list = waypoints, list.indices.contains(i), next() == "at" else { return nil }
            return list[i].at
        case "areas":
            guard let i = index(), let list = areas, list.indices.contains(i),
                  next() == "polygon", let k = index(), list[i].polygon.indices.contains(k) else { return nil }
            return list[i].polygon[k]
        case "routes":
            guard let i = index(), let list = routes, list.indices.contains(i), next() == "arcs",
                  let a = index(), list[i].arcs.indices.contains(a), next() == "pips",
                  let p = index(), list[i].arcs[a].pips.indices.contains(p) else { return nil }
            return list[i].arcs[a].pips[p]
        case "tracks":
            guard let i = index(), let list = tracks, list.indices.contains(i), next() == "segments",
                  let s = index(), list[i].segments.indices.contains(s), next() == "pips",
                  let p = index(), list[i].segments[s].pips.indices.contains(p) else { return nil }
            return list[i].segments[s].pips[p]
        case "networks":
            guard let i = index(), let list = networks, list.indices.contains(i) else { return nil }
            let network = list[i]
            switch next() {
            case "nodes":
                guard let key = next() else { return nil }
                return network.nodes?[JSONKey(key)]
            case "arcs":
                guard let key = next(), let arc = network.arcs?[JSONKey(key)], next() == "pips",
                      let p = index(), arc.pips.indices.contains(p) else { return nil }
                return arc.pips[p]
            case "points":
                guard let p = index(), let points = network.points, points.indices.contains(p),
                      next() == "at" else { return nil }
                return points[p].at
            case "areas":
                guard let a = index(), let list = network.areas, list.indices.contains(a),
                      next() == "polygon", let k = index(), list[a].polygon.indices.contains(k) else { return nil }
                return list[a].polygon[k]
            case "chains":
                guard let c = index(), let chains = network.chains, chains.indices.contains(c),
                      next() == "links", let l = index(), chains[c].links.indices.contains(l),
                      let field = next() else { return nil }
                let link = chains[c].links[l]
                let endpoint = field == "from" ? link.from : link.to
                if case .inline(let p) = endpoint { return p }
                if case .node(let key) = endpoint { return network.nodes?[JSONKey(key)] }
                return nil
            default:
                return nil
            }
        default:
            return nil
        }
    }

    /// The counts the corpus asks for, per top-level collection.
    var collectionCounts: [String: Int] {
        [
            "folders": folders?.count ?? 0,
            "networks": networks?.count ?? 0,
            "routes": routes?.count ?? 0,
            "tracks": tracks?.count ?? 0,
            "waypoints": waypoints?.count ?? 0,
            "areas": areas?.count ?? 0,
        ]
    }

    /// The number of children of every NESTED container in the document, keyed
    /// by a JSON Pointer in the wire's own key names — the same vocabulary
    /// `positions` uses.
    ///
    /// It exists because `counts` counted only the top-level collections.
    /// A reader that dropped every route's arcs, truncated an arc's pips, or
    /// lost a chain's links changed nothing a sidecar could see: the document
    /// still carried one route, one network, one track. That matters most in
    /// the DIAGNOSE tier, which SPEC 8.1 exempts from the round-trip — exactly
    /// the tier where a reader is meant to drop something in a SCOPED way, and
    /// where an unscoped drop was measured to pass the whole corpus green.
    ///
    /// It is a producer rather than a pointer resolver, for the same reason
    /// `position(atPointer:)`'s counterpart in Go is: one traversal answers
    /// every pointer, so a case can pin a container deep inside a network
    /// without the harness growing a second pointer parser.
    ///
    /// EVERY CONTAINER IS EMITTED UNCONDITIONALLY, INCLUDING AN EMPTY ONE.
    /// A zero never triggers the completeness rule in either direction, so a
    /// "only when non-empty" guard buys nothing — and it costs the one thing
    /// six independent runners exist to prevent: `/networks/0/points: 0` would
    /// resolve in a runner that emitted unconditionally and be a hard "not a
    /// container the document carries" error in one that did not, with no
    /// section of the spec to separate them.
    ///
    /// ARC AND NODE CONTAINERS ARE KEYED BY THEIR OWN KEY, NOT BY AN INDEX.
    /// `/networks/0/arcs/arc-1/pips` — SPEC 3.3 makes `arcs` and `nodes`
    /// objects, and an object has no order, so an index there would export this
    /// implementation's dictionary iteration order as a conformance rule.
    var nestedContainerCounts: [String: Int] {
        var out: [String: Int] = [:]
        for (i, network) in (networks ?? []).enumerated() {
            out["/networks/\(i)/nodes"] = network.nodes?.count ?? 0
            out["/networks/\(i)/arcs"] = network.arcs?.count ?? 0
            for (key, arc) in network.arcs ?? [:] {
                out["/networks/\(i)/arcs/\(key)/pips"] = arc.pips.count
            }
            out["/networks/\(i)/chains"] = network.chains?.count ?? 0
            for (j, chain) in (network.chains ?? []).enumerated() {
                out["/networks/\(i)/chains/\(j)/links"] = chain.links.count
            }
            out["/networks/\(i)/points"] = network.points?.count ?? 0
            out["/networks/\(i)/areas"] = network.areas?.count ?? 0
            for (j, area) in (network.areas ?? []).enumerated() {
                out["/networks/\(i)/areas/\(j)/polygon"] = area.polygon.count
            }
        }
        for (i, route) in (routes ?? []).enumerated() {
            out["/routes/\(i)/arcs"] = route.arcs.count
            for (j, arc) in route.arcs.enumerated() {
                out["/routes/\(i)/arcs/\(j)/pips"] = arc.pips.count
            }
        }
        for (i, track) in (tracks ?? []).enumerated() {
            out["/tracks/\(i)/segments"] = track.segments.count
            for (j, segment) in track.segments.enumerated() {
                out["/tracks/\(i)/segments/\(j)/pips"] = segment.pips.count
            }
        }
        for (i, area) in (areas ?? []).enumerated() {
            out["/areas/\(i)/polygon"] = area.polygon.count
        }
        return out
    }

    /// The top-level collections and the nested containers together, which is
    /// what a sidecar's `counts` is compared against.
    var corpusCounts: [String: Int] {
        collectionCounts.merging(nestedContainerCounts) { _, nested in nested }
    }
}
