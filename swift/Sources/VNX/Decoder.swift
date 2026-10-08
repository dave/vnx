import Foundation

/// Turns a parsed JSON tree into a `Document` and a list of diagnostics, or
/// refuses it.
///
/// SPEC 6.2 in one sentence: a reader is tolerant of damage and silent about
/// nothing. An unrecognised key is skipped and the item kept; an unusable value
/// drops the smallest element that can go — the field where the key is
/// OPTIONAL, the item where it is REQUIRED — and everything that goes is
/// reported. A document is refused for exactly four causes and no others.
final class DocumentDecoder {
  private(set) var diagnostics: [Diagnostic] = []

  /// The version this reader implements. SPEC 2.2's comparison is
  /// reader-relative, so it is a stored property rather than a constant —
  /// which is also the only way the 1.0-and-later branch of the rule can be
  /// exercised at all while the format is at 0.x. `VersionRuleTests` reads
  /// documents as a hypothetical 1.x reader for exactly that reason.
  let readerVersion: SemanticVersion

  init(readerVersion: SemanticVersion = SemanticVersion(VNX.version)!) {
    self.readerVersion = readerVersion
  }

  // MARK: - Entry point

  func decode(_ root: JSONValue) throws -> Document {
    guard case .object(let top) = root else {
      throw Refusal(
        .notAVNXDocument,
        "not a VNX document: the top level is \(root.typeName), not a JSON object")
    }
    guard let versionValue = top["vnx"] else {
      throw Refusal(.notAVNXDocument, "not a VNX document: no `vnx` key at the top level")
    }
    guard let versionText = versionValue.stringValue else {
      throw Refusal(
        .versionMalformed,
        "`vnx` is \(versionValue.typeName); expected a version string of the shape MAJOR.MINOR.PATCH"
      )
    }
    guard let version = SemanticVersion(versionText) else {
      throw Refusal(
        .versionMalformed,
        "`vnx` is \"\(versionText)\", which is not of the shape MAJOR.MINOR.PATCH: three runs of decimal digits separated by full stops"
      )
    }
    try refuseUnlessThisReaderImplementsTheSeries(version)

    // SPEC 4.2: a document containing a non-finite number MUST be refused.
    // `NaN` and `Infinity` literals never reach here — they are not JSON,
    // and the parser refused the bytes. What does reach here is a
    // well-formed literal that overflows binary64, such as `1e400`.
    try refuseIfAnyNumberIsNotFinite(root, at: "")

    report(unknownKeysIn: top, recognised: Keys.isDocumentKey, at: "")

    var document = Document(vnx: versionText)
    document.generator = optionalString(top, "generator", at: "")
    document.created = optionalTimestamp(top, "created", at: "")

    document.folders = optionalItems(top, "folders", at: "", decode: decodeFolder)
    document.networks = optionalItems(top, "networks", at: "", decode: decodeNetwork)
    document.plans = optionalItems(top, "plans", at: "", decode: decodePlan)
    document.tracks = optionalItems(top, "tracks", at: "", decode: decodeTrack)
    document.waypoints = optionalItems(top, "waypoints", at: "") { v, p in
      self.decodeWaypoint(v, at: p, shape: Keys.waypoint, allowsFolder: true)
    }
    document.areas = optionalItems(top, "areas", at: "") { v, p in
      self.decodeArea(v, at: p, shape: Keys.topLevelArea, allowsFolder: true)
    }
    // SPEC 3.9: every top-level key containing a full stop is an
    // application key. Its value is any JSON value, `null` included, kept
    // verbatim, never inspected and never reported. Sorted by code point,
    // the order the other implementations hold them in.
    document.applicationKeys = top.keys
      .filter { Keys.isApplicationKey($0.string) }
      .sorted { codePointLess($0.string, $1.string) }
      .map { ApplicationKey(key: $0.string, value: top[$0]!) }

    resolveFolderReferences(in: &document)
    return document
  }

  private func refuseUnlessThisReaderImplementsTheSeries(_ version: SemanticVersion) throws {
    let majorOrder = SemanticVersion.compare(version.major, readerVersion.major)
    let minorOrder = SemanticVersion.compare(version.minor, readerVersion.minor)

    // SPEC 2.2: the rules a reader applies are selected by the DOCUMENT's
    // MAJOR component, not by the reader's. They describe the compatibility
    // guarantees the document's author was working under, and a newer reader
    // cannot grant itself later rules over an older document.
    //
    // So a reader that does not implement the document's major series
    // refuses it outright, rather than reading it under the rules of a
    // series it does implement — reading a 0.x document under 1.x's
    // ignore-what-you-do-not-recognise rule is a guess about guarantees the
    // document never carried.
    if majorOrder != 0 {
      // The direction is in the English only. A document from a later
      // series needs a newer reader; one from an earlier series does not,
      // and a refusal that told its holder to upgrade would send them
      // looking for a reader that will never exist.
      let direction =
        majorOrder > 0 ? "and a newer reader is needed" : "and this reader is too new for it"
      throw Refusal(
        .versionUnsupportedSeries,
        "this document is version \(version.display) \(direction); this reader implements the \(readerVersion.major).x series and MUST NOT read another one under its own rules"
      )
    }

    // Within the document's own major series. Through 0.x EACH MINOR IS ITS
    // OWN SERIES, so the test is "not the same series" rather than "greater
    // than": SPEC 2.2 says a 0.9.0 reader "refuses 0.10.0, and it refuses
    // 0.1.9 unless it declares support for the 0.1 series. A lower minor is
    // refused for the same reason as a higher one." A reader comparing with
    // `> 0` reads a 0.1.9 document under 0.9's rules, which is a guess about
    // guarantees that document never carried — and it is the shape this
    // library had until `corpus/reject/version-too-old.vnx` existed to
    // catch it.
    //
    // From 1.0 onward minor versions are additive and only a MAJOR
    // increment refuses. The PATCH component never refuses at any version,
    // which is why it appears in neither branch.
    //
    // Read from the DOCUMENT, per the rule above. Control only reaches
    // here when the two majors are numerically equal, so swapping this for
    // `readerVersion.major` is a provable no-op and a red proof of it comes
    // back green — which is a fact about the mutation, not a weak guard. The
    // rule is exercised where it is observable: `VersionRuleTests` reads as
    // a 1.x reader, where the two branches genuinely differ.
    let documentIsPreOne = SemanticVersion.compare(version.major, "0") == 0
    if documentIsPreOne && minorOrder != 0 {
      throw Refusal(
        .versionUnsupportedSeries,
        "this document is version \(version.display), which is in the \(version.major).\(version.minor) series; through 0.x each minor is its own series, and this reader implements only \(readerVersion.major).\(readerVersion.minor), so a document from another series is refused whether its minor is higher or lower"
      )
    }
  }

  private func refuseIfAnyNumberIsNotFinite(_ value: JSONValue, at path: String) throws {
    switch value {
    case .number(let d):
      if !d.isFinite {
        throw Refusal(
          .nonFiniteNumber,
          "the number at \(path.isEmpty ? "/" : path) is not finite; SPEC 4.2 forbids NaN and Infinity throughout a document"
        )
      }
    case .array(let a):
      for (i, e) in a.enumerated() { try refuseIfAnyNumberIsNotFinite(e, at: "\(path)/\(i)") }
    case .object(let o):
      for (k, v) in o { try refuseIfAnyNumberIsNotFinite(v, at: pointer(path, k.string)) }
    default:
      break
    }
  }

  // MARK: - Items

  private func decodeFolder(_ value: JSONValue, at path: String) -> Folder? {
    guard let o = object(value, at: path, what: "a folder") else { return nil }
    report(unknownKeysIn: o, against: Keys.folder, at: path)
    // SPEC 6.2: a folder with no `id` is dropped. Items that named it then
    // sit at the top level, so a folder's contents survive its loss.
    guard let id = requiredString(o, "id", at: path) else { return nil }
    var folder = Folder(id: id)
    folder.parent = optionalString(o, "parent", at: path)
    folder.name = optionalString(o, "name", at: path)
    folder.notes = optionalString(o, "notes", at: path)
    folder.hidden = optionalBool(o, "hidden", at: path)
    folder.data = dataRecords(o, at: path)
    return folder
  }

  private func decodeWaypoint(
    _ value: JSONValue, at path: String, shape: Keys.Shape, allowsFolder: Bool
  ) -> Waypoint? {
    guard let o = object(value, at: path, what: "a waypoint") else { return nil }
    report(unknownKeysIn: o, against: shape, at: path)
    guard let atValue = o["at"] else {
      diagnose(
        .wrongTypeRequired, at: pointer(path, "at"),
        "a waypoint with no `at` cannot exist and was dropped")
      return nil
    }
    guard let at = position(atValue, at: pointer(path, "at"), cause: .wrongTypeRequired) else {
      return nil
    }
    var w = Waypoint(at: at)
    w.name = optionalString(o, "name", at: path)
    w.notes = optionalString(o, "notes", at: path)
    if allowsFolder { w.folder = optionalString(o, "folder", at: path) }
    w.hidden = optionalBool(o, "hidden", at: path)
    w.style = style(o, at: path)
    w.data = dataRecords(o, at: path)
    return w
  }

  private func decodeArea(
    _ value: JSONValue, at path: String, shape: Keys.Shape, allowsFolder: Bool
  ) -> Area? {
    guard let o = object(value, at: path, what: "an area") else { return nil }
    report(unknownKeysIn: o, against: shape, at: path)
    guard let polygon = ring(o, at: path) else { return nil }
    var a = Area(polygon: polygon)
    a.name = optionalString(o, "name", at: path)
    a.notes = optionalString(o, "notes", at: path)
    if allowsFolder { a.folder = optionalString(o, "folder", at: path) }
    a.hidden = optionalBool(o, "hidden", at: path)
    a.style = style(o, at: path)
    a.data = dataRecords(o, at: path)
    return a
  }

  /// SPEC 3.7: one OPEN ring of at least three distinct positions. A nested
  /// or additional ring drops the item — never silently keeping the outer
  /// ring, which would draw an area whose hole had vanished with nothing
  /// anywhere to say so.
  ///
  /// ⚠ A CLOSED RING IS REPAIRED, NOT DROPPED. Its last position repeating
  /// its first is one fact written twice, so dropping the repeat loses
  /// nothing, and the diagnostic is how a writer that closes rings is seen.
  /// The distinct positions are counted after that, so `[A, B, A]` is two
  /// points and its area goes, with both diagnostics at their two paths.
  private func ring(_ o: [JSONKey: JSONValue], at path: String) -> [Position]? {
    let polygonPath = pointer(path, "polygon")
    guard let raw = o["polygon"] else {
      diagnose(
        .wrongTypeRequired, at: polygonPath,
        "an area with no `polygon` cannot exist and was dropped")
      return nil
    }
    guard let elements = raw.arrayValue else {
      diagnose(
        .wrongTypeRequired, at: polygonPath,
        "`polygon` is \(raw.typeName); expected an array of positions, so the item was dropped")
      return nil
    }
    let hasNestedRing = elements.contains { element in
      guard let inner = element.arrayValue else { return false }
      return inner.contains { if case .array = $0 { return true } else { return false } }
    }
    if hasNestedRing {
      diagnose(
        .polygonNestedRings, at: polygonPath,
        "`polygon` carries nested rings; this format expresses one ring and cannot express holes, so the item was dropped"
      )
      return nil
    }
    // One unusable vertex drops the ITEM, for the same reason one unusable
    // pip drops its arc. Returning here also stops a second diagnostic: a
    // ring shortened by the dropped vertex may hold too few points, and
    // `ringTooFewPoints` would report a consequence as a second fault.
    var positions: [Position] = []
    for (i, e) in elements.enumerated() {
      guard let p = position(e, at: "\(polygonPath)/\(i)") else { return nil }
      positions.append(p)
    }
    if positions.count >= 2, let first = positions.first, let last = positions.last,
      first.isSamePlace(as: last)
    {
      diagnose(
        .ringClosed, at: "\(polygonPath)/\(positions.count - 1)",
        "a ring is stored open, and this last position repeats the first; the repeat was dropped and the area kept"
      )
      positions.removeLast()
    }
    let distinct = Set(positions.map { [$0.lon, $0.lat] }).count
    guard distinct >= 3 else {
      diagnose(
        .ringTooFewPoints, at: polygonPath,
        "the ring holds \(distinct) distinct position(s) and needs at least 3, so the area was dropped"
      )
      return nil
    }
    return positions
  }

  private func decodePlan(_ value: JSONValue, at path: String) -> Plan? {
    guard let o = object(value, at: path, what: "a plan") else { return nil }
    report(unknownKeysIn: o, against: Keys.plan, at: path)
    var plan = Plan(walk: [])
    plan.nodes = decodeNodes(o, at: path)
    plan.arcs = decodeArcs(o, at: path, nodes: plan.nodes ?? [:])
    plan.name = optionalString(o, "name", at: path)
    plan.notes = optionalString(o, "notes", at: path)
    plan.folder = optionalString(o, "folder", at: path)
    plan.hidden = optionalBool(o, "hidden", at: path)
    plan.style = style(o, at: path)
    plan.data = dataRecords(o, at: path)
    // SPEC 3.4: where a highlight would be dropped, the plan is. It IS the
    // highlight.
    guard let walk = decodeWalk(o, at: path, arcs: plan.arcs ?? [:], plan: true) else {
      return nil
    }
    plan.walk = walk
    return plan
  }

  private func decodeTrack(_ value: JSONValue, at path: String) -> Track? {
    guard let o = object(value, at: path, what: "a track") else { return nil }
    report(unknownKeysIn: o, against: Keys.track, at: path)
    guard let rawSegments = o["segments"] else {
      diagnose(
        .wrongTypeRequired, at: pointer(path, "segments"),
        "a track with no `segments` cannot exist and was dropped")
      return nil
    }
    guard let segmentValues = rawSegments.arrayValue else {
      diagnose(
        .wrongTypeRequired, at: pointer(path, "segments"),
        "`segments` is \(rawSegments.typeName); expected an array, so the item was dropped")
      return nil
    }
    var segments: [TrackSegment] = []
    for (i, v) in segmentValues.enumerated() {
      let segPath = "\(pointer(path, "segments"))/\(i)"
      guard let so = object(v, at: segPath, what: "a segment") else { continue }
      report(unknownKeysIn: so, against: Keys.segment, at: segPath)
      guard let pips = pips(so, at: segPath) else { continue }
      segments.append(TrackSegment(pips: pips))
    }
    var track = Track(segments: segments)
    track.name = optionalString(o, "name", at: path)
    track.notes = optionalString(o, "notes", at: path)
    track.folder = optionalString(o, "folder", at: path)
    track.hidden = optionalBool(o, "hidden", at: path)
    track.style = style(o, at: path)
    track.started = optionalTimestamp(o, "started", at: path)
    track.ended = optionalTimestamp(o, "ended", at: path)
    track.data = dataRecords(o, at: path)
    return track
  }

  // MARK: - Networks

  private func decodeNetwork(_ value: JSONValue, at path: String) -> Network? {
    guard let o = object(value, at: path, what: "a network") else { return nil }
    report(unknownKeysIn: o, against: Keys.network, at: path)

    var network = Network()
    network.name = optionalString(o, "name", at: path)
    network.notes = optionalString(o, "notes", at: path)
    network.folder = optionalString(o, "folder", at: path)
    network.hidden = optionalBool(o, "hidden", at: path)
    network.data = dataRecords(o, at: path)

    // Nodes, then arcs, then highlights: an arc is checked against the nodes
    // that survived, and a walk against the arcs that survived. That order
    // is the whole cascade of SPEC 6.2 — a dropped node drops each arc
    // naming it, and a dropped arc drops each highlight walking it, each with
    // its own diagnostic.
    network.nodes = decodeNodes(o, at: path)
    network.arcs = decodeArcs(o, at: path, nodes: network.nodes ?? [:])
    network.points = optionalItems(o, "points", at: path) { v, p in
      self.decodeWaypoint(v, at: p, shape: Keys.networkPoint, allowsFolder: false)
    }
    network.areas = optionalItems(o, "areas", at: path) { v, p in
      self.decodeArea(v, at: p, shape: Keys.networkArea, allowsFolder: false)
    }
    network.highlights = optionalItems(o, "highlights", at: path) { v, p in
      self.decodeHighlight(v, at: p, arcs: network.arcs ?? [:])
    }
    return network
  }

  /// A network's or a plan's `nodes`. A node that will not parse is dropped
  /// (SPEC 6.2), and each arc naming it then goes under `arcNodeAbsent`.
  private func decodeNodes(_ o: [JSONKey: JSONValue], at path: String) -> [JSONKey: Vertex]? {
    guard let raw = o["nodes"] else { return nil }
    guard let dict = raw.objectValue else {
      diagnose(
        .wrongTypeOptional, at: pointer(path, "nodes"),
        "`nodes` is \(raw.typeName); expected an object keyed by node identifier")
      return nil
    }
    var nodes: [JSONKey: Vertex] = [:]
    for (key, v) in dict {
      let nodePath = pointer(pointer(path, "nodes"), key.string)
      guard let p = position(v, at: nodePath) else { continue }
      // SPEC 3.3.1: a node carries no time. The channel goes and the node
      // stays.
      if p.timeMilliseconds != nil {
        diagnose(
          .extraChannel, at: nodePath,
          "a node carries no time; the time channel was dropped and the node kept")
      }
      nodes[key] = Vertex(lon: p.lon, lat: p.lat, elevation: p.elevation)
    }
    return nodes
  }

  /// A network's or a plan's `arcs`, checked against the nodes that
  /// survived.
  private func decodeArcs(_ o: [JSONKey: JSONValue], at path: String, nodes: [JSONKey: Vertex])
    -> [JSONKey: Arc]?
  {
    guard let raw = o["arcs"] else { return nil }
    guard let dict = raw.objectValue else {
      diagnose(
        .wrongTypeOptional, at: pointer(path, "arcs"),
        "`arcs` is \(raw.typeName); expected an object keyed by arc identifier")
      return nil
    }
    var arcs: [JSONKey: Arc] = [:]
    for (key, v) in dict {
      let arcPath = pointer(pointer(path, "arcs"), key.string)
      guard let ao = object(v, at: arcPath, what: "an arc") else { continue }
      report(unknownKeysIn: ao, against: Keys.arc, at: arcPath)
      guard let from = requiredString(ao, "from", at: arcPath),
        let to = requiredString(ao, "to", at: arcPath),
        let pips = pips(ao, at: arcPath)
      else { continue }
      // SPEC 3.3.2: an arc pip carries no time. Every time on the arc
      // goes, its pips stay, and the arc is reported ONCE, at `pips`,
      // however many pips carried one (SPEC 6.3's third rule).
      if pips.contains(where: { $0.timeMilliseconds != nil }) {
        diagnose(
          .arcPipTime, at: pointer(arcPath, "pips"),
          "an arc pip carries no time; every time channel on this arc was dropped and its pips kept"
        )
      }
      let data = dataRecords(ao, at: arcPath)
      // SPEC 3.3.2: a self-loop has no other end to leave by. Compared by
      // Unicode scalar, like every key: NFC and NFD spellings of one name
      // are two nodes (`accept/unicode-normalisation-keys`).
      if JSONKey(from) == JSONKey(to) {
        diagnose(
          .selfLoopArc, at: arcPath,
          "the arc runs from node \"\(from)\" to itself; a self-loop cannot be walked, so the arc was dropped"
        )
        continue
      }
      // Both ends are reported, each at its own key, before the arc goes.
      var absent = false
      for (field, name) in [("from", from), ("to", to)] where nodes[JSONKey(name)] == nil {
        absent = true
        diagnose(
          .arcNodeAbsent, at: pointer(arcPath, field),
          "`\(field)` names the node \"\(name)\", which is not here, so the arc was dropped")
      }
      if absent { continue }
      arcs[key] = Arc(
        from: from, to: to,
        pips: pips.map { Vertex(lon: $0.lon, lat: $0.lat, elevation: $0.elevation) },
        data: data)
    }
    return arcs
  }

  private func decodeHighlight(_ value: JSONValue, at path: String, arcs: [JSONKey: Arc])
    -> Highlight?
  {
    guard let o = object(value, at: path, what: "a highlight") else { return nil }
    report(unknownKeysIn: o, against: Keys.highlight, at: path)
    var highlight = Highlight(walk: [])
    highlight.name = optionalString(o, "name", at: path)
    highlight.notes = optionalString(o, "notes", at: path)
    highlight.hidden = optionalBool(o, "hidden", at: path)
    highlight.style = style(o, at: path)
    highlight.data = dataRecords(o, at: path)
    guard let walk = decodeWalk(o, at: path, arcs: arcs, plan: false) else { return nil }
    highlight.walk = walk
    return highlight
  }

  /// Reads a highlight's or a plan's REQUIRED `walk`, and checks it in SPEC
  /// 6.2's order. Nil means the owner is dropped: a missing or wrong-typed
  /// `walk`, or the first pass that breaks a rule, which is reported alone —
  /// nothing after it in the walk is examined.
  ///
  /// ⚠ A WALK IS USED WHOLE OR NOT AT ALL (SPEC 6.2). Dropping one pass would
  /// hand the caller a different walk that reads as valid — two arcs joined
  /// that do not meet, a direction reversed by a lost turn, a start moved to
  /// a node — so any fault drops the owner, and the diagnostic names the pass
  /// and the rule.
  private func decodeWalk(
    _ o: [JSONKey: JSONValue], at owner: String, arcs: [JSONKey: Arc], plan: Bool
  ) -> [Pass]? {
    let walkPath = pointer(owner, "walk")
    let ownerName = plan ? "plan" : "highlight"
    guard let raw = o["walk"] else {
      diagnose(
        .wrongTypeRequired, at: walkPath,
        "no `walk`; the \(ownerName) cannot exist without it and was dropped")
      return nil
    }
    guard let items = raw.arrayValue else {
      diagnose(
        .wrongTypeRequired, at: walkPath,
        "`walk` is \(raw.typeName); expected an array of passes, so the \(ownerName) was dropped")
      return nil
    }
    func drop(_ f: WalkFault) -> [Pass]? {
      var at = "\(walkPath)/\(f.pass)"
      if let key = f.key { at = pointer(at, key) }
      diagnose(f.cause, at: at, "\(f.message); the \(ownerName) was dropped")
      return nil
    }
    var cursor = WalkCursor(last: items.count - 1, plan: plan)
    var walk: [Pass] = []
    for (i, item) in items.enumerated() {
      let passPath = "\(walkPath)/\(i)"
      // 1. The pass is an object naming an arc of its owner. When the arc
      // is absent nothing about the pass resolves, so the pass is named.
      guard let po = item.objectValue else {
        return drop(
          WalkFault(
            pass: i, key: nil, cause: .passArcAbsent,
            message: "the pass is \(item.typeName), expected an object"))
      }
      guard let arcName = po["arc"]?.stringValue, let arc = arcs[JSONKey(arcName)] else {
        return drop(
          WalkFault(
            pass: i, key: nil, cause: .passArcAbsent,
            message: "the pass names no arc that is here"))
      }
      // 2. Unrecognised keys.
      report(unknownKeysIn: po, against: Keys.pass, at: passPath)
      var pass = Pass(arc: arcName)
      // 3. `from`, `to`, `turn` and `pin`, in that order, each parse.
      for key in ["from", "to", "turn", "pin"] {
        guard let v = po[JSONKey(key)] else { continue }
        if key == "from" || key == "to", let node = v.stringValue {
          if key == "from" { pass.from = .node(node) } else { pass.to = .node(node) }
          continue
        }
        let parsed: Position
        switch parsePosition(v) {
        case .success(let p):
          parsed = p
        case .failure(let problem):
          return drop(
            WalkFault(pass: i, key: key, cause: .positionUnusable, message: problem.message))
        }
        // SPEC 3.3.3: longitude and latitude only. The other channels
        // go and the pass stays.
        if parsed.elevation != nil || parsed.timeMilliseconds != nil {
          diagnose(
            .extraChannel, at: pointer(passPath, key),
            "a walk position carries longitude and latitude only; its other channels were dropped")
        }
        let at = LonLat(lon: parsed.lon, lat: parsed.lat)
        switch key {
        case "from": pass.from = .position(at)
        case "to": pass.to = .position(at)
        case "turn": pass.turn = at
        default: pass.pin = at
        }
      }
      // 4 to 7: a plan's rule, pin and turn, spelling, connection.
      do {
        _ = try cursor.step(i, pass, arc: arc)
      } catch let fault as WalkFault {
        return drop(fault)
      } catch {
        preconditionFailure("WalkCursor.step throws only WalkFault, and threw \(error)")
      }
      walk.append(pass)
    }
    return walk
  }

  // MARK: - Folder references

  /// SPEC 6.2: a `folder` naming a folder that is not present is not an
  /// unusable value. The key is treated as absent, so the item sits at the
  /// top level, and a diagnostic is recorded. Dropping the item would lose
  /// content over a lost grouping.
  private func resolveFolderReferences(in document: inout Document) {
    let ids = Set((document.folders ?? []).map(\.id))

    func check(_ value: String?, at path: String, key: String) -> String? {
      guard let value else { return nil }
      if ids.contains(value) { return value }
      diagnose(
        .folderNotPresent, at: pointer(path, key),
        "`\(key)` names the folder \"\(value)\", which is not in this document; the item was kept at the top level"
      )
      return nil
    }

    for i in document.folders?.indices ?? [].indices {
      document.folders![i].parent = check(
        document.folders![i].parent, at: "/folders/\(i)", key: "parent")
    }
    for i in document.networks?.indices ?? [].indices {
      document.networks![i].folder = check(
        document.networks![i].folder, at: "/networks/\(i)", key: "folder")
    }
    for i in document.plans?.indices ?? [].indices {
      document.plans![i].folder = check(
        document.plans![i].folder, at: "/plans/\(i)", key: "folder")
    }
    for i in document.tracks?.indices ?? [].indices {
      document.tracks![i].folder = check(
        document.tracks![i].folder, at: "/tracks/\(i)", key: "folder")
    }
    for i in document.waypoints?.indices ?? [].indices {
      document.waypoints![i].folder = check(
        document.waypoints![i].folder, at: "/waypoints/\(i)", key: "folder")
    }
    for i in document.areas?.indices ?? [].indices {
      document.areas![i].folder = check(document.areas![i].folder, at: "/areas/\(i)", key: "folder")
    }
  }

  // MARK: - Field helpers

  private func diagnose(_ cause: Cause, at path: String, _ message: String) {
    diagnostics.append(Diagnostic(path: path, message: message, cause: cause))
  }

  private func pointer(_ base: String, _ token: String) -> String {
    // RFC 6901 escaping: `~` becomes `~0` and `/` becomes `~1`.
    let escaped = token.replacingOccurrences(of: "~", with: "~0")
      .replacingOccurrences(of: "/", with: "~1")
    return base + "/" + escaped
  }

  private func report(
    unknownKeysIn o: [JSONKey: JSONValue], against shape: Keys.Shape, at path: String
  ) {
    let known = shape.all
    report(unknownKeysIn: o, recognised: { known.contains(JSONKey($0)) }, at: path)
  }

  private func report(
    unknownKeysIn o: [JSONKey: JSONValue], recognised: (String) -> Bool, at path: String
  ) {
    for key in o.keys.map(\.string).sorted(by: codePointLess) where !recognised(key) {
      // SPEC 6.2: record a diagnostic, skip the key, keep the item. An
      // unrecognised key is not an unusable value.
      diagnose(
        .unknownKey, at: pointer(path, key),
        "unknown key `\(key)`; this format does not define it here, so it was skipped")
    }
  }

  private func object(_ value: JSONValue, at path: String, what: String) -> [JSONKey: JSONValue]? {
    guard let o = value.objectValue else {
      diagnose(
        .wrongTypeRequired, at: path,
        "expected \(what) but found \(value.typeName); the item was dropped")
      return nil
    }
    return o
  }

  private func optionalString(_ o: [JSONKey: JSONValue], _ key: String, at path: String) -> String?
  {
    guard let raw = o[JSONKey(key)] else { return nil }
    guard let s = raw.stringValue else {
      diagnose(
        .wrongTypeOptional, at: pointer(path, key),
        "`\(key)` is \(raw.typeName); expected a string, so the field was dropped and the item kept"
      )
      return nil
    }
    return s
  }

  private func requiredString(_ o: [JSONKey: JSONValue], _ key: String, at path: String) -> String?
  {
    guard let raw = o[JSONKey(key)] else {
      diagnose(
        .wrongTypeRequired, at: pointer(path, key),
        "no `\(key)`; the item cannot exist without it and was dropped")
      return nil
    }
    guard let s = raw.stringValue else {
      diagnose(
        .wrongTypeRequired, at: pointer(path, key),
        "`\(key)` is \(raw.typeName); expected a string, so the item was dropped")
      return nil
    }
    return s
  }

  private func optionalBool(_ o: [JSONKey: JSONValue], _ key: String, at path: String) -> Bool? {
    guard let raw = o[JSONKey(key)] else { return nil }
    guard let b = raw.boolValue else {
      diagnose(
        .wrongTypeOptional, at: pointer(path, key),
        "`\(key)` is \(raw.typeName); expected a boolean, so the field was dropped and the item kept"
      )
      return nil
    }
    return b
  }

  private func optionalItems<T>(
    _ o: [JSONKey: JSONValue], _ key: String, at path: String,
    decode: (JSONValue, String) -> T?
  ) -> [T]? {
    guard let raw = o[JSONKey(key)] else { return nil }
    guard let array = raw.arrayValue else {
      diagnose(
        .wrongTypeOptional, at: pointer(path, key),
        "`\(key)` is \(raw.typeName); expected an array, so the field was dropped and the item kept"
      )
      return nil
    }
    var out: [T] = []
    for (i, v) in array.enumerated() {
      if let item = decode(v, "\(pointer(path, key))/\(i)") { out.append(item) }
    }
    return out
  }

  /// SPEC 4.3: `created`, `started` and `ended` are ISO 8601 in UTC with a
  /// literal `Z` and OPTIONAL fractional seconds. An offset other than `Z` is
  /// an unusable value in an optional key, so the field goes and the item —
  /// every pip of a track, in the corpus case — stays.
  private func optionalTimestamp(_ o: [JSONKey: JSONValue], _ key: String, at path: String)
    -> String?
  {
    guard let raw = o[JSONKey(key)] else { return nil }
    guard let text = raw.stringValue else {
      diagnose(
        .wrongTypeOptional, at: pointer(path, key),
        "`\(key)` is \(raw.typeName); expected a timestamp string, so the field was dropped and the item kept"
      )
      return nil
    }
    guard isUTCTimestamp(text) else {
      diagnose(
        .timestampNotUTC, at: pointer(path, key),
        "`\(key)` is \"\(text)\"; a record-level timestamp must be UTC with a Z suffix, so the field was dropped and the item kept"
      )
      return nil
    }
    return text
  }

  /// SPEC 4.3, checked for SHAPE ONLY.
  ///
  /// `2026-13-45T99:99:99Z` matches the grammar and is accepted. A reader MUST
  /// NOT refuse or diagnose a record-level timestamp on calendar grounds, by
  /// the same rule of SPEC 6.2 that leaves `icon-size` unchecked: a reader
  /// checks a value only where this document states a normative range or
  /// enumeration. Silence would diverge here rather than converge, because a
  /// reader built on a date library gets the calendar check for free and one
  /// parsing the grammar directly does not.
  private func isUTCTimestamp(_ s: String) -> Bool {
    // YYYY-MM-DDTHH:MM:SS(.fraction)?Z, checked without NSRegularExpression
    // so the shape is visible rather than encoded in a pattern.
    let c = Array(s)
    guard c.count >= 20, c.last == "Z" else { return false }
    func digits(_ range: Range<Int>) -> Bool {
      range.allSatisfy { c[$0].isASCII && c[$0].isNumber }
    }
    guard digits(0..<4), c[4] == "-", digits(5..<7), c[7] == "-", digits(8..<10),
      c[10] == "T", digits(11..<13), c[13] == ":", digits(14..<16), c[16] == ":",
      digits(17..<19)
    else { return false }
    if c.count == 20 { return true }
    guard c[19] == "." else { return false }
    return digits(20..<(c.count - 1)) && c.count > 21
  }

  /// SPEC 3.10 and 5.1: a style object is opaque, and a reader must carry any
  /// key it does not know through a read-modify-write without reporting it.
  ///
  /// The one exception is `importance`. SPEC 3.6 states a normative range for
  /// it — "an integer from 1 to 5, and that is normative" — and SPEC 6.2
  /// names it as the worked example of a value outside a stated range, so it
  /// is checked even though it lives inside an otherwise opaque object.
  /// Nothing else in a style is checked, and no key in a data record body is
  /// checked at all.
  private func style(_ o: [JSONKey: JSONValue], at path: String) -> OpaqueObject? {
    guard let raw = o["style"] else { return nil }
    guard var s = raw.objectValue else {
      diagnose(
        .wrongTypeOptional, at: pointer(path, "style"),
        "`style` is \(raw.typeName); expected an object, so the field was dropped and the item kept"
      )
      return nil
    }
    if let importance = s["importance"] {
      let d = importance.numberValue
      let usable = d != nil && d! >= 1 && d! <= 5 && d! == d!.rounded()
      if !usable {
        diagnose(
          .importanceOutOfRange, at: pointer(pointer(path, "style"), "importance"),
          "`importance` must be an integer from 1 to 5, so the field was dropped and the item kept")
        s.removeValue(forKey: "importance")
      }
    }
    return s
  }

  /// SPEC 5: a record body is opaque apart from `@type`. SPEC 6.2 is explicit
  /// that a reader MUST NOT check the type, range or enumeration of a field
  /// inside one — doing so would break the mechanism by which a record of an
  /// unrecognised type survives a read-modify-write.
  private func dataRecords(_ o: [JSONKey: JSONValue], at path: String) -> [DataRecord]? {
    guard let raw = o["data"] else { return nil }
    guard let array = raw.arrayValue else {
      diagnose(
        .wrongTypeOptional, at: pointer(path, "data"),
        "`data` is \(raw.typeName); expected an array of records, so the field was dropped and the item kept"
      )
      return nil
    }
    var out: [DataRecord] = []
    for (i, v) in array.enumerated() {
      let recordPath = "\(pointer(path, "data"))/\(i)"
      guard var body = v.objectValue else {
        diagnose(
          .wrongTypeRequired, at: recordPath,
          "a data record is \(v.typeName); expected an object, so the record was dropped")
        continue
      }
      guard let type = body["@type"]?.stringValue else {
        diagnose(
          .wrongTypeRequired, at: recordPath,
          "a data record has no `@type`, or one that is not a string, so the record was dropped")
        continue
      }
      body.removeValue(forKey: "@type")
      out.append(DataRecord(type: type, body: body))
    }
    return out
  }

  private func pips(_ o: [JSONKey: JSONValue], at path: String) -> [Position]? {
    let pipsPath = pointer(path, "pips")
    guard let raw = o["pips"] else {
      diagnose(
        .wrongTypeRequired, at: pipsPath,
        "no `pips`; the item cannot exist without geometry and was dropped")
      return nil
    }
    guard let array = raw.arrayValue else {
      diagnose(
        .wrongTypeRequired, at: pipsPath,
        "`pips` is \(raw.typeName); expected an array of positions, so the item was dropped")
      return nil
    }
    // SPEC 6.2: ONE UNUSABLE PIP DROPS THE CONTAINER, not the pip. Geometry
    // is continuous and structure is not — a missing vertex leaves a
    // shorter, plausible, wrong line with nothing to indicate it, while a
    // missing arc is seen by every highlight that walks it, and a missing
    // segment is a pause (SPEC 3.5).
    var out: [Position] = []
    for (i, v) in array.enumerated() {
      guard let p = position(v, at: "\(pipsPath)/\(i)") else { return nil }
      out.append(p)
    }
    return out
  }

  /// Why a position will not parse. `isShape` marks the two faults that
  /// concern the value as a whole — not an array, or fewer than two channels
  /// — which a caller may report under a cause of its own.
  struct PositionProblem: Error {
    let isShape: Bool
    let message: String
  }

  /// SPEC 4.1, with no diagnostic: the caller decides what the failure costs.
  ///
  /// Longitude first, then latitude — the single most likely transposition
  /// bug in this format, and one that parses cleanly in either order.
  private func parsePosition(_ value: JSONValue) -> Result<Position, PositionProblem> {
    guard let channels = value.arrayValue else {
      return .failure(
        PositionProblem(
          isShape: true,
          message:
            "a position is \(value.typeName); expected a position array [lon, lat, ele, time_ms]"))
    }
    guard channels.count >= 2 else {
      return .failure(
        PositionProblem(
          isShape: false,
          message:
            "a position carries \(channels.count) channel(s); it MUST carry at least lon and lat"))
    }
    // THE PATH IS THE POSITION, NOT THE CHANNEL. SPEC 6.3 names the smallest
    // element no part of which could be used, and a position with a null
    // longitude is unusable whole — the latitude beside it locates nothing
    // on its own. The channel index goes in the message, which nothing
    // compares.
    guard let lon = channels[0].numberValue else {
      return .failure(
        PositionProblem(
          isShape: false, message: "longitude is \(channels[0].typeName); expected a number"))
    }
    guard let lat = channels[1].numberValue else {
      return .failure(
        PositionProblem(
          isShape: false, message: "latitude is \(channels[1].typeName); expected a number"))
    }
    if let problem = offTheGlobe(lon: lon, lat: lat) {
      return .failure(PositionProblem(isShape: false, message: problem))
    }
    for i in 2..<channels.count {
      switch channels[i] {
      case .number, .null: continue
      default:
        return .failure(
          PositionProblem(
            isShape: false,
            message:
              "channel \(i) is \(channels[i].typeName); from index 2 upward a channel is a number or null"
          ))
      }
    }
    // Channels above index 3 are read and discarded. SPEC 4.1: "A reader
    // MUST ignore them and MUST NOT report a diagnostic for them" — and a
    // model that carried them would be carrying the document's SPELLING,
    // which this format stopped constraining.
    return .success(
      Position(
        lon: lon, lat: lat,
        elevation: channels.count > 2 ? channels[2].numberValue : nil,
        timeMilliseconds: channels.count > 3 ? channels[3].numberValue : nil))
  }

  /// SPEC 4.1, reporting a failure. `cause` is what gets dropped, not what is
  /// wrong.
  ///
  /// The same malformed position is a different fact depending on where it
  /// sits: under a waypoint's REQUIRED `at` the item goes, so it is
  /// `wrongTypeRequired`; as one pip of a `pips` array the arc goes, so it is
  /// `positionUnusable`. SPEC 6.2 gives the remedies; a reader that reported
  /// one code for all of them would make a consumer re-derive which happened.
  private func position(
    _ value: JSONValue, at path: String,
    cause: Cause = .positionUnusable
  ) -> Position? {
    switch parsePosition(value) {
    case .success(let p):
      return p
    case .failure(let problem):
      diagnose(problem.isShape ? cause : .positionUnusable, at: path, problem.message)
      return nil
    }
  }
}

/// SPEC 4.1's normative ranges, both bounds included. `nil` when in range.
func offTheGlobe(lon: Double, lat: Double) -> String? {
  if !(-180...180).contains(lon) { return "longitude \(lon) is off the globe" }
  if !(-90...90).contains(lat) { return "latitude \(lat) is off the globe" }
  return nil
}
