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
    try putArray(&o, "networks", d.networks) { try network($0) }
    try putArray(&o, "routes", d.routes) { try route($0) }
    putArray(&o, "tracks", d.tracks) { track($0) }
    putArray(&o, "waypoints", d.waypoints) { waypoint($0, allowsFolder: true) }
    try putArray(&o, "areas", d.areas) { try area($0, allowsFolder: true) }
    // SPEC 3.9: each application key verbatim, `null` and empty values
    // included. A key without a full stop, or held twice, is refused.
    for k in d.applicationKeys {
      guard Keys.isApplicationKey(k.key) else {
        throw ApplicationKeyError(
          key: k.key,
          message: "application key \"\(k.key)\" contains no full stop (SPEC 3.9)")
      }
      guard o[JSONKey(k.key)] == nil else {
        throw ApplicationKeyError(
          key: k.key,
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

  private static func area(_ a: Area, allowsFolder: Bool) throws -> JSONValue {
    try checkRing(a.polygon, name: a.name)
    var o: [JSONKey: JSONValue] = ["polygon": .array(a.polygon.map { position($0) })]
    put(&o, "name", a.name)
    put(&o, "notes", a.notes)
    if allowsFolder { put(&o, "folder", a.folder) }
    putHidden(&o, a.hidden)
    putStyle(&o, a.style)
    putData(&o, a.data)
    return .object(o)
  }

  /// Refuses a ring SPEC 3.7 would have a reader repair or drop: a writer
  /// emits a ring open, with at least three distinct positions.
  ///
  /// It does not open a closed ring itself. That would be the writer guessing
  /// that its caller meant the repeat, and the repeat is the caller's to fix.
  private static func checkRing(_ ring: [Position], name: String?) throws {
    if ring.count >= 2, let first = ring.first, let last = ring.last, first.isSamePlace(as: last) {
      throw WriteError(
        "area \(describe(name)): a ring is written open, and this one's last position repeats its first"
      )
    }
    let distinct = Set(ring.map { [$0.lon, $0.lat] }).count
    if distinct < 3 {
      throw WriteError(
        "area \(describe(name)): a ring needs at least 3 distinct positions, and this one has \(distinct)"
      )
    }
  }

  private static func network(_ n: Network) throws -> JSONValue {
    var o: [JSONKey: JSONValue] = [:]
    put(&o, "name", n.name)
    put(&o, "notes", n.notes)
    put(&o, "folder", n.folder)
    putHidden(&o, n.hidden)
    try putGraph(&o, nodes: n.nodes, arcs: n.arcs, owner: "network \(describe(n.name))")
    try putArray(&o, "chains", n.chains) { c in
      // A writer never emits a walk its own reader would drop.
      try checkWalk(c.walk, arcs: n.arcs ?? [:], route: false, owner: "chain \(describe(c.name))")
      return chain(c)
    }
    putArray(&o, "points", n.points) { waypoint($0, allowsFolder: false) }
    try putArray(&o, "areas", n.areas) { try area($0, allowsFolder: false) }
    putData(&o, n.data)
    return .object(o)
  }

  /// A network's or a route's `nodes` and `arcs`, after checking every arc:
  /// two different nodes, both present (SPEC 3.3.2).
  private static func putGraph(
    _ o: inout [JSONKey: JSONValue], nodes: [JSONKey: Vertex]?, arcs: [JSONKey: Arc]?,
    owner: String
  ) throws {
    for key in (arcs ?? [:]).keys.sorted(by: { codePointLess($0.string, $1.string) }) {
      let a = arcs![key]!
      if JSONKey(a.from) == JSONKey(a.to) {
        throw WriteError(
          "\(owner): arc \"\(key.string)\" runs from node \"\(a.from)\" to itself, and a self-loop cannot be written"
        )
      }
      for node in [a.from, a.to] where nodes?[JSONKey(node)] == nil {
        throw WriteError(
          "\(owner): arc \"\(key.string)\" names node \"\(node)\", which is not in its owner's nodes"
        )
      }
    }
    if let nodes {
      o["nodes"] = .object(nodes.mapValues { position($0.position) })
    }
    if let arcs {
      o["arcs"] = .object(arcs.mapValues { arc($0) })
    }
  }

  private static func arc(_ a: Arc) -> JSONValue {
    var o: [JSONKey: JSONValue] = [
      "from": .string(a.from),
      "to": .string(a.to),
      // Always written, empty for a straight arc: SPEC 3.10 makes the
      // key required.
      "pips": .array(a.pips.map { position($0.position) }),
    ]
    putData(&o, a.data)
    return .object(o)
  }

  /// The writer's check on a walk: every pass names an arc of its owner, and
  /// the walk follows SPEC 3.3.3's rules — the same `WalkCursor` the reader
  /// steps, so the two cannot disagree about what a valid walk is.
  private static func checkWalk(_ walk: [Pass], arcs: [JSONKey: Arc], route: Bool, owner: String)
    throws
  {
    var cursor = WalkCursor(last: walk.count - 1, route: route)
    for (i, p) in walk.enumerated() {
      guard let a = arcs[JSONKey(p.arc)] else {
        throw WriteError(
          "\(owner): pass \(i) names arc \"\(p.arc)\", which is not here (\(Cause.passArcAbsent.code))"
        )
      }
      do {
        _ = try cursor.step(i, p, arc: a)
      } catch let fault as WalkFault {
        let key = fault.key.map { " `\($0)`" } ?? ""
        throw WriteError(
          "\(owner): pass \(fault.pass)\(key): \(fault.message) (\(fault.cause.code))")
      }
    }
  }

  private static func chain(_ c: Chain) -> JSONValue {
    var o: [JSONKey: JSONValue] = ["walk": walk(c.walk)]
    put(&o, "name", c.name)
    put(&o, "notes", c.notes)
    putHidden(&o, c.hidden)
    putStyle(&o, c.style)
    putData(&o, c.data)
    return .object(o)
  }

  /// A walk. `from` and `to` appear where the model holds them — a walk that
  /// passed `checkWalk` holds them exactly where SPEC 3.3.3's rule puts them
  /// — and `pinned` only when it is `true`.
  private static func walk(_ passes: [Pass]) -> JSONValue {
    .array(
      passes.map { p in
        var o: [JSONKey: JSONValue] = ["arc": .string(p.arc)]
        if let from = p.from { o["from"] = endpoint(from) }
        if let to = p.to { o["to"] = endpoint(to) }
        if let turn = p.turn { o["turn"] = position(turn.position) }
        if p.pinned { o["pinned"] = .bool(true) }
        return .object(o)
      })
  }

  private static func endpoint(_ e: Endpoint) -> JSONValue {
    switch e {
    case .node(let k): return .string(k)
    case .position(let p): return position(p.position)
    }
  }

  private static func route(_ r: Route) throws -> JSONValue {
    let owner = "route \(describe(r.name))"
    try checkWalk(r.walk, arcs: r.arcs ?? [:], route: true, owner: owner)
    var o: [JSONKey: JSONValue] = ["walk": walk(r.walk)]
    try putGraph(&o, nodes: r.nodes, arcs: r.arcs, owner: owner)
    put(&o, "name", r.name)
    put(&o, "notes", r.notes)
    put(&o, "folder", r.folder)
    putHidden(&o, r.hidden)
    putStyle(&o, r.style)
    putData(&o, r.data)
    return .object(o)
  }

  private static func describe(_ name: String?) -> String {
    name.map { "\"\($0)\"" } ?? "(unnamed)"
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
    o["data"] = .array(
      data.map { record in
        var body = record.body
        body["@type"] = .string(record.type)
        return .object(body)
      })
  }

  private static func putArray<T>(
    _ o: inout [JSONKey: JSONValue], _ key: String, _ items: [T]?,
    _ encode: (T) throws -> JSONValue
  ) rethrows {
    guard let items else { return }
    o[JSONKey(key)] = .array(try items.map(encode))
  }
}
