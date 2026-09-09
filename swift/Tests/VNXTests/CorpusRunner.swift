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
/// - The accept tier is `re-render(decode(file)) == file`. Not `write(...)`,
///   which rounds, and not a bytes-to-bytes reformat, which never builds a
///   model and so cannot see a reader that threw away what it read.
/// - Every `format/` input is asserted NOT to be canonical already, or a
///   formatter that echoed its input would pass the whole tier.
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

    static func cases(in tier: String) throws -> [URL] {
        let directory = root.appendingPathComponent(tier)
        let names = try FileManager.default.contentsOfDirectory(atPath: directory.path)
        return names.sorted()
            .filter { $0.hasSuffix(".vnx") || $0.hasSuffix(".vnz") }
            .filter { !$0.hasSuffix(".in.vnx") && !$0.hasSuffix(".canonical.vnx") }
            .map { directory.appendingPathComponent($0) }
    }

    /// One case's `.expect.json` sidecar.
    struct Sidecar {
        let kind: String
        let note: String
        /// SPEC 8.2's per-case schema verdict. Recorded, and used only for
        /// the one direction a reader can check (`not-json`); this library
        /// carries no JSON Schema validator.
        let schema: String?
        let counts: [String: Int]?
        let positions: [(pointer: String, lon: Double, lat: Double)]
        let tracks: [(name: String, movingTimeMilliseconds: Double?)]
        let attachments: [String]?
        let refusal: String?
        /// A sidecar's expected diagnostics. `code` is the vocabulary the
        /// corpus adopted on 2026-09-09; `message` is the older English
        /// substring, and some sidecars still carry only that. A case is
        /// matched on whichever it gives, and `message_advisory` is recorded
        /// but is, by its name, not a requirement.
        let diagnostics: [ExpectedDiagnostic]?

        struct ExpectedDiagnostic {
            let path: String
            let code: String?
            let message: String?
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
            self.schema = o["schema"]?.stringValue
            if let c = o["counts"]?.objectValue {
                self.counts = c.compactMapValues { $0.numberValue.map(Int.init) }
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
            self.refusal = o["refusal"]?.stringValue
            if let d = o["diagnostics"]?.arrayValue {
                self.diagnostics = try d.map { entry in
                    guard let e = entry.objectValue, let path = e["path"]?.stringValue else {
                        throw SidecarError("a diagnostic in \(url.lastPathComponent) has no `path`")
                    }
                    let code = e["code"]?.stringValue
                    let message = e["message"]?.stringValue
                    let advisory = e["message_advisory"]?.stringValue
                    // SPEC 6.3: a diagnostic a corpus case pins MUST carry a
                    // `code`, and the code is what the case compares.
                    guard code != nil else {
                        throw SidecarError("the diagnostic at \(path) in \(url.lastPathComponent) names no `code`; SPEC 6.3 requires one and forbids comparing the English, so there is nothing here a runner may assert")
                    }
                    return ExpectedDiagnostic(path: path, code: code, message: message, advisory: advisory)
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
                return network.nodes?[key]
            case "arcs":
                guard let key = next(), let arc = network.arcs?[key], next() == "pips",
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
                if case .node(let key) = endpoint { return network.nodes?[key] }
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
            "rulesets": rulesets?.count ?? 0,
        ]
    }
}
