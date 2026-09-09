import Foundation

/// The two operations SPEC 8.1 distinguishes, both of which produce a document.
public enum EmitMode: Sendable {
    /// Turn a document a reader was handed back into that document, applying
    /// neither rounding nor position shortening, so arity and precision
    /// survive. This is what the accept tier requires: `re-render(decode(file))
    /// == file`, and a runner MUST NOT use its writer for that check, because
    /// writing what was read is legitimately not what was read.
    case reRender
    /// Turn a model into a document, applying the emit-boundary rules of SPEC
    /// 4.5.4 and SPEC 4.1: round longitude and latitude to 6 decimal places and
    /// elevation to 1, and emit the shortest spelling of every position.
    case write
}

/// Turns a `Document` back into a JSON tree.
///
/// This is a reconstruction from the model, not a reformat of the bytes the
/// reader was handed. SPEC 8.1 rules the second one out and says why: it "never
/// builds a model, so it cannot observe anything about what a model kept — a
/// reader that discarded every data record, every ruleset, every style and
/// every `notes` passes such a check". Everything below therefore has to come
/// out of the model or it is gone.
enum DocumentEncoder {
    static func encode(_ d: Document, mode: EmitMode) -> JSONValue {
        var o: [String: JSONValue] = ["vnx": .string(d.vnx)]
        put(&o, "profile", d.profile)
        put(&o, "generator", d.generator)
        put(&o, "created", d.created)
        putArray(&o, "folders", d.folders) { folder($0, mode) }
        putArray(&o, "networks", d.networks) { network($0, mode) }
        putArray(&o, "routes", d.routes) { route($0, mode) }
        putArray(&o, "tracks", d.tracks) { track($0, mode) }
        putArray(&o, "waypoints", d.waypoints) { waypoint($0, mode, allowsFolder: true) }
        putArray(&o, "areas", d.areas) { area($0, mode, allowsFolder: true) }
        if let r = d.rulesets { o["rulesets"] = .array(r) }
        return .object(o)
    }

    // MARK: - Items

    private static func folder(_ f: Folder, _ mode: EmitMode) -> JSONValue {
        var o: [String: JSONValue] = ["id": .string(f.id)]
        put(&o, "parent", f.parent)
        put(&o, "name", f.name)
        put(&o, "notes", f.notes)
        putHidden(&o, f.hidden)
        putData(&o, f.data)
        return .object(o)
    }

    private static func waypoint(_ w: Waypoint, _ mode: EmitMode, allowsFolder: Bool) -> JSONValue {
        var o: [String: JSONValue] = ["at": position(w.at, mode)]
        put(&o, "name", w.name)
        put(&o, "notes", w.notes)
        if allowsFolder { put(&o, "folder", w.folder) }
        putHidden(&o, w.hidden)
        putStyle(&o, w.style)
        putData(&o, w.data)
        return .object(o)
    }

    private static func area(_ a: Area, _ mode: EmitMode, allowsFolder: Bool) -> JSONValue {
        var o: [String: JSONValue] = ["polygon": .array(a.polygon.map { position($0, mode) })]
        put(&o, "name", a.name)
        put(&o, "notes", a.notes)
        if allowsFolder { put(&o, "folder", a.folder) }
        putHidden(&o, a.hidden)
        putStyle(&o, a.style)
        putData(&o, a.data)
        return .object(o)
    }

    private static func network(_ n: Network, _ mode: EmitMode) -> JSONValue {
        var o: [String: JSONValue] = [:]
        put(&o, "name", n.name)
        put(&o, "notes", n.notes)
        put(&o, "folder", n.folder)
        putHidden(&o, n.hidden)
        if let nodes = n.nodes {
            o["nodes"] = .object(nodes.mapValues { position($0, mode) })
        }
        if let arcs = n.arcs {
            o["arcs"] = .object(arcs.mapValues { networkArc($0, mode) })
        }
        putArray(&o, "chains", n.chains) { chain($0, mode) }
        putArray(&o, "points", n.points) { waypoint($0, mode, allowsFolder: false) }
        putArray(&o, "areas", n.areas) { area($0, mode, allowsFolder: false) }
        putData(&o, n.data)
        return .object(o)
    }

    private static func networkArc(_ a: NetworkArc, _ mode: EmitMode) -> JSONValue {
        var o: [String: JSONValue] = [
            "from": .string(a.from),
            "to": .string(a.to),
            "pips": .array(a.pips.map { position($0, mode) }),
        ]
        putData(&o, a.data)
        return .object(o)
    }

    private static func chain(_ c: Chain, _ mode: EmitMode) -> JSONValue {
        var o: [String: JSONValue] = ["links": .array(c.links.map { link($0, mode) })]
        put(&o, "name", c.name)
        put(&o, "notes", c.notes)
        putHidden(&o, c.hidden)
        putStyle(&o, c.style)
        putData(&o, c.data)
        return .object(o)
    }

    private static func link(_ l: Link, _ mode: EmitMode) -> JSONValue {
        .object([
            "arc": .string(l.arc),
            "from": endpoint(l.from, mode),
            "to": endpoint(l.to, mode),
        ])
    }

    private static func endpoint(_ e: LinkEndpoint, _ mode: EmitMode) -> JSONValue {
        switch e {
        case .node(let k): return .string(k)
        case .inline(let p): return position(p, mode)
        }
    }

    private static func route(_ r: Route, _ mode: EmitMode) -> JSONValue {
        var o: [String: JSONValue] = ["arcs": .array(r.arcs.map { routeArc($0, mode) })]
        put(&o, "name", r.name)
        put(&o, "notes", r.notes)
        put(&o, "folder", r.folder)
        putHidden(&o, r.hidden)
        putStyle(&o, r.style)
        putData(&o, r.data)
        return .object(o)
    }

    private static func routeArc(_ a: RouteArc, _ mode: EmitMode) -> JSONValue {
        var o: [String: JSONValue] = ["pips": .array(a.pips.map { position($0, mode) })]
        putData(&o, a.data)
        return .object(o)
    }

    private static func track(_ t: Track, _ mode: EmitMode) -> JSONValue {
        var o: [String: JSONValue] = [
            "segments": .array(t.segments.map { .object(["pips": .array($0.pips.map { position($0, mode) })]) })
        ]
        put(&o, "name", t.name)
        put(&o, "notes", t.notes)
        put(&o, "folder", t.folder)
        putHidden(&o, t.hidden)
        putStyle(&o, t.style)
        put(&o, "started", t.started)
        put(&o, "ended", t.ended)
        putData(&o, t.data)
        return .object(o)
    }

    // MARK: - Positions

    private static func position(_ p: Position, _ mode: EmitMode) -> JSONValue {
        switch mode {
        case .reRender:
            // SPEC 4.1: shortening happens where rounding happens, at the
            // serialisation boundary, and is NOT part of canonical form. A
            // formatter re-renders the spelling the document carries.
            return .array(p.channels)
        case .write:
            return .array(shortened(p))
        }
    }

    /// SPEC 4.5.4 rounding, then SPEC 4.1's shortest spelling.
    private static func shortened(_ p: Position) -> [JSONValue] {
        var channels: [JSONValue] = [
            .number(round(p.lon, places: 6)),
            .number(round(p.lat, places: 6)),
        ]
        let elevation: JSONValue = p.elevation.map { .number(round($0, places: 1)) } ?? .null
        // Time is an integer count of milliseconds and is not rounded.
        let time: JSONValue = p.timeMilliseconds.map { .number($0) } ?? .null
        // Channels above index 3 are not defined by 0.1. Carrying them keeps
        // SPEC 4.1's "a future channel is additive" true through a write as
        // well as through a re-render.
        let extra = p.channels.count > 4 ? Array(p.channels[4...]) : []

        if !extra.isEmpty {
            channels.append(elevation)
            channels.append(time)
            channels.append(contentsOf: extra)
            return channels
        }
        if case .number = time {
            // An interior absence keeps its `null`: dropping it would move the
            // time into the elevation channel.
            channels.append(elevation)
            channels.append(time)
            return channels
        }
        if case .number = elevation {
            channels.append(elevation)
            return channels
        }
        return channels
    }

    /// SPEC 4.5.4: a tie rounds to EVEN. An elevation of `340.25` becomes
    /// `340.2`, not `340.3` — these are exact doubles and a quarter-metre grid
    /// produces them. Swift's bare `.rounded()` is `.toNearestOrAwayFromZero`
    /// and gets every one of them wrong.
    ///
    /// No re-render can detect a disagreement here, because rounding happens
    /// only when a writer emits a model, and every corpus tier re-renders. This
    /// rule has no fixture anywhere and is guarded only by a unit test.
    private static func round(_ x: Double, places: Int) -> Double {
        let scale = pow(10.0, Double(places))
        let r = (x * scale).rounded(.toNearestOrEven) / scale
        // -0 is the same value as 0 and renders as `0`; normalising here keeps
        // a model comparison from distinguishing them either.
        return r == 0 ? 0 : r
    }

    // MARK: - Field helpers

    private static func put(_ o: inout [String: JSONValue], _ key: String, _ value: String?) {
        if let value { o[key] = .string(value) }
    }

    /// SPEC 3.8: absence means visible, and a writer emits `hidden` only when
    /// its value is `true`. A document read with `"hidden": false` in it keeps
    /// that spelling through a re-render, because the model holds what the
    /// document said.
    private static func putHidden(_ o: inout [String: JSONValue], _ hidden: Bool?) {
        if let hidden { o["hidden"] = .bool(hidden) }
    }

    private static func putStyle(_ o: inout [String: JSONValue], _ style: OpaqueObject?) {
        if let style { o["style"] = .object(style) }
    }

    private static func putData(_ o: inout [String: JSONValue], _ data: [DataRecord]?) {
        guard let data else { return }
        o["data"] = .array(data.map { record in
            var body = record.body
            body["@type"] = .string(record.type)
            return .object(body)
        })
    }

    private static func putArray<T>(_ o: inout [String: JSONValue], _ key: String, _ items: [T]?,
                                    _ encode: (T) -> JSONValue) {
        guard let items else { return }
        o[key] = .array(items.map(encode))
    }
}
