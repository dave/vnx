import Foundation
@testable import VNX

/// JSON-tree comparison, which is what the corpus compares now that the bytes
/// of a document are not constrained (SPEC 8.1).
///
/// ⚠ IT IS NEVER MODEL-VERSUS-MODEL, and that is the finding the accept tier
/// rests on. A reader that discarded every data record, every ruleset, every
/// style and every `notes` produces a model that compares equal to itself and
/// passes an accept tier built that way green — that is the whole extensibility
/// mechanism, all styling and all user prose. Comparing the tree parsed from the
/// ORIGINAL FILE against the tree parsed from `write(read(file))` is what proves
/// a reader kept what it read. `AcceptTierProofTests` freezes the proof.
enum Tree {

    /// Which normalisations a comparison applies.
    ///
    /// ⚠ THE TWO TIERS NORMALISE DIFFERENTLY, AND MUST. The accept tier compares
    /// a document against a re-encoding of itself, so SPEC 4.1's precision and
    /// its shortest spelling are applied to both sides — otherwise the tier
    /// would require the file to have been written by this library. The write
    /// tier compares one writer's output against another's, where those two
    /// rules are the very thing under test: normalising them there would make
    /// the tier prove nothing.
    struct Options {
        /// Applies SPEC 4.1's writer rules — the precision, and dropping a
        /// trailing `null` channel — to both sides.
        var emitBoundary = false
    }

    struct ParseFailure: Error, CustomStringConvertible {
        let description: String
    }

    static func parse(_ data: Data, _ what: String) throws -> JSONValue {
        do {
            return try JSONParser.parse(data)
        } catch {
            throw ParseFailure(description: "\(what) is not JSON: \(error)")
        }
    }

    /// The first place two documents differ, in JSON Pointer form, or `nil` when
    /// they are equal.
    ///
    /// A PATH rather than a boolean, because a failing corpus case has to say
    /// which rule broke to be actionable.
    static func difference(got: Data, want: Data, _ options: Options = Options()) throws -> String? {
        let g = normaliseDocument(try parse(got, "the encoded document"), options)
        let w = normaliseDocument(try parse(want, "the expected document"), options)
        return diff(got: g, want: w, path: "")
    }

    // MARK: - Normalisation

    /// Walks a decoded VNX document and applies the two normalisations SPEC 8.1
    /// permits, and no others.
    ///
    /// SCHEMA-AWARE rather than a heuristic over shapes, because the two regions
    /// the format declares opaque — a data record's body and a style — may hold
    /// arrays of numbers that are not positions and must not be rounded, and may
    /// hold empty containers that a third party means to keep.
    static func normaliseDocument(_ v: JSONValue, _ o: Options) -> JSONValue {
        guard var obj = v.objectValue else { return v }
        normItems(&obj, "folders", o, normFolder)
        normItems(&obj, "waypoints", o, normWaypoint)
        normItems(&obj, "areas", o, normArea)
        normItems(&obj, "tracks", o, normTrack)
        normItems(&obj, "routes", o, normRoute)
        normItems(&obj, "networks", o, normNetwork)
        dropEmptyContainer(&obj, "rulesets")
        return .object(obj)
    }

    private static func normFolder(_ o: inout OpaqueObject, _ opt: Options) {
        dropEmptyContainer(&o, "data")
    }

    private static func normWaypoint(_ o: inout OpaqueObject, _ opt: Options) {
        if let at = o["at"] { o["at"] = normPosition(at, opt) }
        dropEmptyContainer(&o, "style")
        dropEmptyContainer(&o, "data")
    }

    private static func normArea(_ o: inout OpaqueObject, _ opt: Options) {
        normPositionList(&o, "polygon", opt)
        dropEmptyContainer(&o, "style")
        dropEmptyContainer(&o, "data")
    }

    private static func normTrack(_ o: inout OpaqueObject, _ opt: Options) {
        normItems(&o, "segments", opt) { s, opt in normPositionList(&s, "pips", opt) }
        dropEmptyContainer(&o, "style")
        dropEmptyContainer(&o, "data")
    }

    private static func normRoute(_ o: inout OpaqueObject, _ opt: Options) {
        normItems(&o, "arcs", opt, normArc)
        dropEmptyContainer(&o, "style")
        dropEmptyContainer(&o, "data")
    }

    private static func normArc(_ o: inout OpaqueObject, _ opt: Options) {
        normPositionList(&o, "pips", opt)
        dropEmptyContainer(&o, "data")
    }

    private static func normNetwork(_ o: inout OpaqueObject, _ opt: Options) {
        if let nodes = o["nodes"]?.objectValue {
            o["nodes"] = .object(nodes.mapValues { normPosition($0, opt) })
        }
        if let arcs = o["arcs"]?.objectValue {
            o["arcs"] = .object(arcs.mapValues { value in
                guard var a = value.objectValue else { return value }
                normArc(&a, opt)
                return .object(a)
            })
        }
        normItems(&o, "chains", opt, normChain)
        normItems(&o, "points", opt, normWaypoint)
        normItems(&o, "areas", opt, normArea)
        dropEmptyContainer(&o, "nodes")
        dropEmptyContainer(&o, "arcs")
        dropEmptyContainer(&o, "data")
    }

    private static func normChain(_ o: inout OpaqueObject, _ opt: Options) {
        normItems(&o, "links", opt) { l, opt in
            // An endpoint is a node key or an inline position (SPEC 3.3.3).
            // Only the second is a position, and `normPosition` leaves a string
            // alone.
            for side in ["from", "to"] {
                if let v = l[side] { l[side] = normPosition(v, opt) }
            }
        }
        dropEmptyContainer(&o, "style")
        dropEmptyContainer(&o, "data")
    }

    /// Normalises an array of objects under `key`, and drops the key when the
    /// array is empty.
    private static func normItems(_ o: inout OpaqueObject, _ key: String, _ opt: Options,
                                  _ each: (inout OpaqueObject, Options) -> Void) {
        guard let arr = o[key]?.arrayValue else { return }
        if arr.isEmpty {
            o.removeValue(forKey: key)
            return
        }
        o[key] = .array(arr.map { item in
            guard var obj = item.objectValue else { return item }
            each(&obj, opt)
            return .object(obj)
        })
    }

    private static func normPositionList(_ o: inout OpaqueObject, _ key: String, _ opt: Options) {
        guard let arr = o[key]?.arrayValue else { return }
        if arr.isEmpty {
            o.removeValue(forKey: key)
            return
        }
        o[key] = .array(arr.map { normPosition($0, opt) })
    }

    /// Applies what SPEC 8.1 lets a comparison ignore about a position: the
    /// precision of SPEC 4.1, and a trailing `null`. Both are emit-boundary
    /// rules and are applied only for the accept tier; see `Options`.
    ///
    /// ⚠ A CHANNEL THIS VERSION DOES NOT NAME IS NOT NORMALISED AWAY, and no
    /// corpus file carries one. SPEC 4.1's preservation rule is gone, so a
    /// fixture asking a writer to reproduce a fifth channel would be asking for
    /// a rule the format does not state — the fixture is what changes, not the
    /// comparison.
    private static func normPosition(_ v: JSONValue, _ opt: Options) -> JSONValue {
        guard let arr = v.arrayValue, opt.emitBoundary else { return v }
        var out: [JSONValue] = []
        for (i, c) in arr.enumerated() {
            guard let n = c.numberValue else {
                out.append(c)
                continue
            }
            switch i {
            case 0, 1: out.append(.number(Rounding.coordinate(n)))
            case 2: out.append(.number(Rounding.elevation(n)))
            default: out.append(.number(n))
            }
        }
        while out.count > 2, out.last == .null { out.removeLast() }
        return .array(out)
    }

    /// SPEC 3.1: an absent collection and an empty one carry the same content,
    /// and a reader MUST treat them alike. So the comparison does too, which is
    /// what keeps a writer that omits an empty collection conforming.
    private static func dropEmptyContainer(_ o: inout OpaqueObject, _ key: String) {
        switch o[key] {
        case .array(let a) where a.isEmpty: o.removeValue(forKey: key)
        case .object(let m) where m.isEmpty: o.removeValue(forKey: key)
        default: break
        }
    }

    // MARK: - Difference

    private static func diff(got: JSONValue, want: JSONValue, path: String) -> String? {
        switch want {
        case .object(let w):
            guard let g = got.objectValue else {
                return "\(pathOrRoot(path)): got \(describe(got)), want an object"
            }
            for key in w.keys.sorted(by: codePointLess) {
                guard let gv = g[key] else {
                    return "\(pathOrRoot(path)): key \"\(key)\" is missing"
                }
                if let d = diff(got: gv, want: w[key]!, path: path + "/" + escapePointer(key)) { return d }
            }
            let extra = g.keys.filter { w[$0] == nil }.sorted(by: codePointLess)
            if !extra.isEmpty {
                return "\(pathOrRoot(path)): unexpected key(s) \(extra)"
            }
        case .array(let w):
            guard let g = got.arrayValue else {
                return "\(pathOrRoot(path)): got \(describe(got)), want an array"
            }
            if g.count != w.count {
                return "\(pathOrRoot(path)): got \(g.count) element(s), want \(w.count)"
            }
            for i in w.indices {
                if let d = diff(got: g[i], want: w[i], path: "\(path)/\(i)") { return d }
            }
        default:
            // A number compares by VALUE (SPEC 4.2), which is what `-0 == 0`
            // means here and what makes the comparison blind to how either side
            // spelled it.
            if got != want {
                return "\(pathOrRoot(path)): got \(describe(got)), want \(describe(want))"
            }
        }
        return nil
    }

    private static func pathOrRoot(_ p: String) -> String { p.isEmpty ? "the document" : p }

    private static func escapePointer(_ k: String) -> String {
        k.replacingOccurrences(of: "~", with: "~0").replacingOccurrences(of: "/", with: "~1")
    }

    private static func describe(_ v: JSONValue) -> String {
        switch v {
        case .null: return "null"
        case .bool(let b): return b ? "true" : "false"
        case .number(let d): return String(d)
        case .string(let s): return "\"\(s)\""
        case .array: return "an array"
        case .object: return "an object"
        }
    }
}
