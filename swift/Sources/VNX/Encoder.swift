import Foundation

/// Turns a `Document` into the JSON tree a writer emits.
///
/// This is a reconstruction from the model, not a reformat of the bytes the
/// reader was handed. SPEC 8.1 rules the second one out and says why: it "never
/// builds a model, so it cannot observe anything about what a model kept — a
/// reader that discarded every data record, every application key, every style and
/// every `notes` passes such a check". Everything below therefore has to come
/// out of the model or it is gone.
///
/// There is ONE emit path. The format defines no byte layout, so nothing here
/// wants an unrounded, unshortened emit: every caller turns a model into a
/// file, and SPEC 4.1's rules apply on the way out.
enum DocumentEncoder {
    static func encode(_ d: Document) throws -> JSONValue {
        var o: [JSONKey: JSONValue] = ["vnx": .string(d.vnx)]
        put(&o, "generator", d.generator)
        put(&o, "created", d.created)
        putArray(&o, "folders", d.folders) { folder($0) }
        putArray(&o, "networks", d.networks) { network($0) }
        putArray(&o, "routes", d.routes) { route($0) }
        putArray(&o, "tracks", d.tracks) { track($0) }
        putArray(&o, "waypoints", d.waypoints) { waypoint($0, allowsFolder: true) }
        putArray(&o, "areas", d.areas) { area($0, allowsFolder: true) }
        // SPEC 3.9: each application key verbatim, `null` and empty values
        // included. A key without a full stop, or held twice, is refused.
        for k in d.applicationKeys {
            guard Keys.isApplicationKey(k.key) else {
                throw ApplicationKeyError(key: k.key,
                    message: "application key \"\(k.key)\" contains no full stop (SPEC 3.9)")
            }
            guard o[JSONKey(k.key)] == nil else {
                throw ApplicationKeyError(key: k.key,
                    message: "application key \"\(k.key)\" appears twice")
            }
            o[JSONKey(k.key)] = k.value
        }
        return .object(o)
    }

    // MARK: - Items

    private static func folder(_ f: Folder) -> JSONValue {
        var o: [JSONKey: JSONValue] = ["id": .string(f.id)]
        put(&o, "parent", f.parent)
        put(&o, "name", f.name)
        put(&o, "notes", f.notes)
        putHidden(&o, f.hidden)
        putData(&o, f.data)
        return .object(o)
    }

    private static func waypoint(_ w: Waypoint, allowsFolder: Bool) -> JSONValue {
        var o: [JSONKey: JSONValue] = ["at": position(w.at)]
        put(&o, "name", w.name)
        put(&o, "notes", w.notes)
        if allowsFolder { put(&o, "folder", w.folder) }
        putHidden(&o, w.hidden)
        putStyle(&o, w.style)
        putData(&o, w.data)
        return .object(o)
    }

    private static func area(_ a: Area, allowsFolder: Bool) -> JSONValue {
        var o: [JSONKey: JSONValue] = ["polygon": .array(a.polygon.map { position($0) })]
        put(&o, "name", a.name)
        put(&o, "notes", a.notes)
        if allowsFolder { put(&o, "folder", a.folder) }
        putHidden(&o, a.hidden)
        putStyle(&o, a.style)
        putData(&o, a.data)
        return .object(o)
    }

    private static func network(_ n: Network) -> JSONValue {
        var o: [JSONKey: JSONValue] = [:]
        put(&o, "name", n.name)
        put(&o, "notes", n.notes)
        put(&o, "folder", n.folder)
        putHidden(&o, n.hidden)
        if let nodes = n.nodes {
            o["nodes"] = .object(nodes.mapValues { position($0) })
        }
        if let arcs = n.arcs {
            o["arcs"] = .object(arcs.mapValues { networkArc($0) })
        }
        putArray(&o, "chains", n.chains) { chain($0) }
        putArray(&o, "points", n.points) { waypoint($0, allowsFolder: false) }
        putArray(&o, "areas", n.areas) { area($0, allowsFolder: false) }
        putData(&o, n.data)
        return .object(o)
    }

    private static func networkArc(_ a: NetworkArc) -> JSONValue {
        var o: [JSONKey: JSONValue] = [
            "from": .string(a.from),
            "to": .string(a.to),
            "pips": .array(a.pips.map { position($0) }),
        ]
        putData(&o, a.data)
        return .object(o)
    }

    private static func chain(_ c: Chain) -> JSONValue {
        var o: [JSONKey: JSONValue] = ["links": .array(c.links.map { link($0) })]
        put(&o, "name", c.name)
        put(&o, "notes", c.notes)
        putHidden(&o, c.hidden)
        putStyle(&o, c.style)
        putData(&o, c.data)
        return .object(o)
    }

    private static func link(_ l: Link) -> JSONValue {
        .object([
            "arc": .string(l.arc),
            "from": endpoint(l.from),
            "to": endpoint(l.to),
        ])
    }

    private static func endpoint(_ e: LinkEndpoint) -> JSONValue {
        switch e {
        case .node(let k): return .string(k)
        case .inline(let p): return position(p)
        }
    }

    private static func route(_ r: Route) -> JSONValue {
        var o: [JSONKey: JSONValue] = ["arcs": .array(r.arcs.map { routeArc($0) })]
        put(&o, "name", r.name)
        put(&o, "notes", r.notes)
        put(&o, "folder", r.folder)
        putHidden(&o, r.hidden)
        putStyle(&o, r.style)
        putData(&o, r.data)
        return .object(o)
    }

    private static func routeArc(_ a: RouteArc) -> JSONValue {
        var o: [JSONKey: JSONValue] = ["pips": .array(a.pips.map { position($0) })]
        putData(&o, a.data)
        return .object(o)
    }

    private static func track(_ t: Track) -> JSONValue {
        var o: [JSONKey: JSONValue] = [
            "segments": .array(t.segments.map { .object(["pips": .array($0.pips.map { position($0) })]) })
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

    /// SPEC 4.1's emit boundary: the shortest spelling of a position, and
    /// nothing else.
    ///
    /// ⚠ NO VALUE CHANGES HERE. This format does not constrain how precisely a
    /// writer states a coordinate, so a value passes through exactly as the
    /// model holds it. The rule that remains is about ARITY, not precision:
    /// `[lon, lat, null]` writes as `[lon, lat]`, and `[lon, lat, ele, null]`
    /// loses only its trailing null.
    ///
    /// This library used to round longitude and latitude to 6 decimal places
    /// and elevation to 1, because SPEC 4.1 required it. That requirement is
    /// gone: a rounded coordinate and an unrounded one are both conforming
    /// documents that every reader reads identically, so it was never a rule
    /// about interchange. The advice survives in SPEC 4.1 as an informative
    /// paragraph mirroring RFC 7946 Section 11.2, and a producer that wants
    /// smaller files may round its own data — but not on this path, which the
    /// write tier compares byte-for-value against the document it was given.
    private static func position(_ p: Position) -> JSONValue {
        var channels: [JSONValue] = [.number(p.lon), .number(p.lat)]
        if let t = p.timeMilliseconds {
            // An interior absence keeps its `null`: dropping it would move the
            // time into the elevation channel.
            channels.append(p.elevation.map { .number($0) } ?? .null)
            channels.append(.number(t))
        } else if let e = p.elevation {
            channels.append(.number(e))
        }
        return .array(channels)
    }

    // MARK: - Field helpers

    private static func put(_ o: inout [JSONKey: JSONValue], _ key: String, _ value: String?) {
        if let value { o[JSONKey(key)] = .string(value) }
    }

    /// SPEC 3.8: "Absence means visible. A writer MUST emit the key only when
    /// its value is `true`, and a document SHOULD NOT contain
    /// `"hidden": false`."
    ///
    /// So a `false` the reader was handed is read, kept in the model, and NOT
    /// written back. Pinned by `corpus/write/emit-boundary`, whose input
    /// carries one.
    private static func putHidden(_ o: inout [JSONKey: JSONValue], _ hidden: Bool?) {
        guard let hidden, hidden else { return }
        o["hidden"] = .bool(true)
    }

    private static func putStyle(_ o: inout [JSONKey: JSONValue], _ style: OpaqueObject?) {
        if let style { o["style"] = .object(style) }
    }

    private static func putData(_ o: inout [JSONKey: JSONValue], _ data: [DataRecord]?) {
        guard let data else { return }
        o["data"] = .array(data.map { record in
            var body = record.body
            body["@type"] = .string(record.type)
            return .object(body)
        })
    }

    private static func putArray<T>(_ o: inout [JSONKey: JSONValue], _ key: String, _ items: [T]?,
                                    _ encode: (T) -> JSONValue) {
        guard let items else { return }
        o[JSONKey(key)] = .array(items.map(encode))
    }
}
