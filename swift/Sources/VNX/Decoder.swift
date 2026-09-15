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
    /// exercised at all while the format is at 0.1. `VersionRuleTests` reads
    /// documents as a hypothetical 1.x reader for exactly that reason.
    let readerVersion: SemanticVersion

    init(readerVersion: SemanticVersion = SemanticVersion(VNX.version)!) {
        self.readerVersion = readerVersion
    }

    // MARK: - Entry point

    func decode(_ root: JSONValue) throws -> Document {
        guard case .object(let top) = root else {
            throw Refusal(.notAVNXDocument,
                          "not a VNX document: the top level is \(root.typeName), not a JSON object")
        }
        guard let versionValue = top["vnx"] else {
            throw Refusal(.notAVNXDocument, "not a VNX document: no `vnx` key at the top level")
        }
        guard let versionText = versionValue.stringValue else {
            throw Refusal(.versionMalformed,
                          "`vnx` is \(versionValue.typeName); expected a version string of the shape MAJOR.MINOR.PATCH")
        }
        guard let version = SemanticVersion(versionText) else {
            throw Refusal(.versionMalformed,
                          "`vnx` is \"\(versionText)\", which is not of the shape MAJOR.MINOR.PATCH: three runs of decimal digits separated by full stops")
        }
        try refuseUnlessThisReaderImplementsTheSeries(version)

        // SPEC 4.2: a document containing a non-finite number MUST be refused.
        // `NaN` and `Infinity` literals never reach here — they are not JSON,
        // and the parser refused the bytes. What does reach here is a
        // well-formed literal that overflows binary64, such as `1e400`.
        try refuseIfAnyNumberIsNotFinite(root, at: "")

        report(unknownKeysIn: top, against: Keys.document, at: "")

        var document = Document(vnx: versionText)
        document.generator = optionalString(top, "generator", at: "")
        document.created = optionalTimestamp(top, "created", at: "")

        document.folders = optionalItems(top, "folders", at: "", decode: decodeFolder)
        document.networks = optionalItems(top, "networks", at: "", decode: decodeNetwork)
        document.routes = optionalItems(top, "routes", at: "", decode: decodeRoute)
        document.tracks = optionalItems(top, "tracks", at: "", decode: decodeTrack)
        document.waypoints = optionalItems(top, "waypoints", at: "") { v, p in
            self.decodeWaypoint(v, at: p, shape: Keys.waypoint, allowsFolder: true)
        }
        document.areas = optionalItems(top, "areas", at: "") { v, p in
            self.decodeArea(v, at: p, shape: Keys.topLevelArea, allowsFolder: true)
        }
        // SPEC 3.9: rulesets are not defined in 0.1, and a 0.1.x reader treats
        // the contents as opaque and preserves them through a
        // read-modify-write.
        if let raw = top["rulesets"] {
            if let array = raw.arrayValue {
                document.rulesets = array
            } else {
                diagnose(.wrongTypeOptional, at: "/rulesets",
                         "`rulesets` is \(raw.typeName); expected an array")
            }
        }

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
            let direction = majorOrder > 0 ? "and a newer reader is needed" : "and this reader is too new for it"
            throw Refusal(.versionUnsupportedSeries,
                          "this document is version \(version.display) \(direction); this reader implements the \(readerVersion.major).x series and MUST NOT read another one under its own rules")
        }

        // Within the document's own major series. Through 0.x EACH MINOR IS ITS
        // OWN SERIES, so the test is "not the same series" rather than "greater
        // than": SPEC 2.2 says a reader "refuses 0.2.0, and it refuses 0.0.9
        // unless it declares support for the 0.0 series. A lower minor is
        // refused for the same reason as a higher one." A reader comparing with
        // `> 0` reads a 0.0.9 document under 0.1's rules, which is a guess about
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
            throw Refusal(.versionUnsupportedSeries,
                          "this document is version \(version.display), which is in the \(version.major).\(version.minor) series; through 0.x each minor is its own series, and this reader implements only \(readerVersion.major).\(readerVersion.minor), so a document from another series is refused whether its minor is higher or lower")
        }
    }

    private func refuseIfAnyNumberIsNotFinite(_ value: JSONValue, at path: String) throws {
        switch value {
        case .number(let d):
            if !d.isFinite {
                throw Refusal(.nonFiniteNumber,
                              "the number at \(path.isEmpty ? "/" : path) is not finite; SPEC 4.2 forbids NaN and Infinity throughout a document")
            }
        case .array(let a):
            for (i, e) in a.enumerated() { try refuseIfAnyNumberIsNotFinite(e, at: "\(path)/\(i)") }
        case .object(let o):
            for (k, v) in o { try refuseIfAnyNumberIsNotFinite(v, at: pointer(path, k)) }
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

    private func decodeWaypoint(_ value: JSONValue, at path: String, shape: Keys.Shape, allowsFolder: Bool) -> Waypoint? {
        guard let o = object(value, at: path, what: "a waypoint") else { return nil }
        report(unknownKeysIn: o, against: shape, at: path)
        guard let atValue = o["at"] else {
            diagnose(.requiredKeyMissing, at: path, "a waypoint with no `at` cannot exist and was dropped")
            return nil
        }
        guard let at = position(atValue, at: pointer(path, "at"), cause: .wrongTypeRequired) else { return nil }
        var w = Waypoint(at: at)
        w.name = optionalString(o, "name", at: path)
        w.notes = optionalString(o, "notes", at: path)
        if allowsFolder { w.folder = optionalString(o, "folder", at: path) }
        w.hidden = optionalBool(o, "hidden", at: path)
        w.style = style(o, at: path)
        w.data = dataRecords(o, at: path)
        return w
    }

    private func decodeArea(_ value: JSONValue, at path: String, shape: Keys.Shape, allowsFolder: Bool) -> Area? {
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

    /// SPEC 3.7: one closed ring of at least four positions. A nested or
    /// additional ring drops the item — never silently keeping the outer ring,
    /// which would draw an area whose hole had vanished with nothing anywhere
    /// to say so.
    private func ring(_ o: [String: JSONValue], at path: String) -> [Position]? {
        let polygonPath = pointer(path, "polygon")
        guard let raw = o["polygon"] else {
            diagnose(.requiredKeyMissing, at: path, "an area with no `polygon` cannot exist and was dropped")
            return nil
        }
        guard let elements = raw.arrayValue else {
            diagnose(.wrongTypeRequired, at: polygonPath, "`polygon` is \(raw.typeName); expected an array of positions, so the item was dropped")
            return nil
        }
        let hasNestedRing = elements.contains { element in
            guard let inner = element.arrayValue else { return false }
            return inner.contains { if case .array = $0 { return true } else { return false } }
        }
        if hasNestedRing {
            diagnose(.polygonNestedRings, at: polygonPath,
                     "`polygon` carries nested rings; this format expresses one closed ring and cannot express holes, so the item was dropped")
            return nil
        }
        // One unusable vertex drops the ITEM, for the same reason one unusable
        // pip drops its arc. Returning here rather than falling through also
        // stops a second diagnostic: a ring shortened by the dropped vertex is
        // no longer closed, and `polygonUnclosed` would report a consequence as
        // though it were a second fault.
        var positions: [Position] = []
        for (i, e) in elements.enumerated() {
            guard let p = position(e, at: "\(polygonPath)/\(i)") else { return nil }
            positions.append(p)
        }
        guard positions.count >= 4, let first = positions.first, let last = positions.last,
              first.isSamePlace(as: last) else {
            diagnose(.polygonUnclosed, at: polygonPath,
                     "`polygon` is not one closed ring of at least four positions, so the item was dropped")
            return nil
        }
        return positions
    }

    private func decodeRoute(_ value: JSONValue, at path: String) -> Route? {
        guard let o = object(value, at: path, what: "a route") else { return nil }
        report(unknownKeysIn: o, against: Keys.route, at: path)
        guard let rawArcs = o["arcs"] else {
            diagnose(.requiredKeyMissing, at: path, "a route with no `arcs` cannot exist and was dropped")
            return nil
        }
        guard let arcValues = rawArcs.arrayValue else {
            diagnose(.wrongTypeRequired, at: pointer(path, "arcs"),
                     "`arcs` is \(rawArcs.typeName); expected an array, so the item was dropped")
            return nil
        }
        var arcs: [RouteArc] = []
        for (i, v) in arcValues.enumerated() {
            let arcPath = "\(pointer(path, "arcs"))/\(i)"
            guard let ao = object(v, at: arcPath, what: "an arc") else { continue }
            report(unknownKeysIn: ao, against: Keys.routeArc, at: arcPath)
            guard let pips = pips(ao, at: arcPath) else { continue }
            arcs.append(RouteArc(pips: pips, data: dataRecords(ao, at: arcPath)))
        }
        var route = Route(arcs: arcs)
        route.name = optionalString(o, "name", at: path)
        route.notes = optionalString(o, "notes", at: path)
        route.folder = optionalString(o, "folder", at: path)
        route.hidden = optionalBool(o, "hidden", at: path)
        route.style = style(o, at: path)
        route.data = dataRecords(o, at: path)
        return route
    }

    private func decodeTrack(_ value: JSONValue, at path: String) -> Track? {
        guard let o = object(value, at: path, what: "a track") else { return nil }
        report(unknownKeysIn: o, against: Keys.track, at: path)
        guard let rawSegments = o["segments"] else {
            diagnose(.requiredKeyMissing, at: path, "a track with no `segments` cannot exist and was dropped")
            return nil
        }
        guard let segmentValues = rawSegments.arrayValue else {
            diagnose(.wrongTypeRequired, at: pointer(path, "segments"),
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

        network.nodes = decodeNodes(o, at: path)
        network.arcs = decodeArcs(o, at: path, nodes: network.nodes ?? [:])
        network.points = optionalItems(o, "points", at: path) { v, p in
            self.decodeWaypoint(v, at: p, shape: Keys.networkPoint, allowsFolder: false)
        }
        network.areas = optionalItems(o, "areas", at: path) { v, p in
            self.decodeArea(v, at: p, shape: Keys.networkArea, allowsFolder: false)
        }
        network.chains = optionalItems(o, "chains", at: path) { v, p in
            self.decodeChain(v, at: p, nodes: network.nodes ?? [:], arcs: network.arcs ?? [:])
        }
        return network
    }

    private func decodeNodes(_ o: [String: JSONValue], at path: String) -> [String: Position]? {
        guard let raw = o["nodes"] else { return nil }
        guard let dict = raw.objectValue else {
            diagnose(.wrongTypeOptional, at: pointer(path, "nodes"),
                     "`nodes` is \(raw.typeName); expected an object keyed by node identifier")
            return nil
        }
        var nodes: [String: Position] = [:]
        for (key, v) in dict {
            if let p = position(v, at: pointer(pointer(path, "nodes"), key)) { nodes[key] = p }
        }
        return nodes
    }

    private func decodeArcs(_ o: [String: JSONValue], at path: String, nodes: [String: Position]) -> [String: NetworkArc]? {
        guard let raw = o["arcs"] else { return nil }
        guard let dict = raw.objectValue else {
            diagnose(.wrongTypeOptional, at: pointer(path, "arcs"),
                     "`arcs` is \(raw.typeName); expected an object keyed by arc identifier")
            return nil
        }
        var arcs: [String: NetworkArc] = [:]
        for (key, v) in dict {
            let arcPath = pointer(pointer(path, "arcs"), key)
            guard let ao = object(v, at: arcPath, what: "an arc") else { continue }
            report(unknownKeysIn: ao, against: Keys.networkArc, at: arcPath)
            guard let from = requiredString(ao, "from", at: arcPath),
                  let to = requiredString(ao, "to", at: arcPath),
                  let pips = pips(ao, at: arcPath) else { continue }
            // SPEC 6.2's remedy table: an arc naming a node that is not in its
            // network keeps the arc, because its geometry is real. Reported all
            // the same — nothing tolerated here is silent.
            for (field, name) in [("from", from), ("to", to)] where nodes[name] == nil {
                diagnose(.arcNodeNotInNetwork, at: pointer(arcPath, field),
                         "`\(field)` names the node \"\(name)\", which is not in this network; the arc was kept because its geometry is real")
            }
            // SPEC 3.3.2: an arc's pips are its COMPLETE point list, so the
            // first pip is the position of the node `from` names and the last is
            // `to`'s. The two are one fact written twice; both are kept, the
            // pips are authoritative, and the disagreement is reported.
            //
            // THE PATH NAMES THE END, NOT THE ARC. SPEC 6.3 names the smallest
            // element, and the arc's other end and its interior pips are all
            // usable — while a reader naming the arc emits two diagnostics at
            // one identical path, and 6.3 forbids telling them apart by their
            // English.
            for (field, name, terminal) in [("from", from, pips.first), ("to", to, pips.last)] {
                guard let node = nodes[name], let terminal,
                      let metres = Projection.distance(from: node, toPolyline: [terminal]),
                      !Projection.isWithinTolerance(metres) else { continue }
                diagnose(.terminalPipOffNode, at: pointer(arcPath, field),
                         "the node \"\(name)\" is \(Int(metres.rounded())) m from the \(field) pip, beyond the 50 m tolerance; both are kept and the pips are authoritative")
            }
            arcs[key] = NetworkArc(from: from, to: to, pips: pips, data: dataRecords(ao, at: arcPath))
        }
        return arcs
    }

    private func decodeChain(_ value: JSONValue, at path: String, nodes: [String: Position], arcs: [String: NetworkArc]) -> Chain? {
        guard let o = object(value, at: path, what: "a chain") else { return nil }
        report(unknownKeysIn: o, against: Keys.chain, at: path)
        guard let rawLinks = o["links"] else {
            diagnose(.requiredKeyMissing, at: path, "a chain with no `links` cannot exist and was dropped")
            return nil
        }
        guard let linkValues = rawLinks.arrayValue else {
            diagnose(.wrongTypeRequired, at: pointer(path, "links"),
                     "`links` is \(rawLinks.typeName); expected an array, so the item was dropped")
            return nil
        }

        // Each link keeps the index it had in the document, so a diagnostic
        // addresses the element the reader was handed rather than the element
        // that survived.
        var surviving: [(index: Int, link: Link)] = []
        for (i, v) in linkValues.enumerated() {
            let linkPath = "\(pointer(path, "links"))/\(i)"
            guard let link = decodeLink(v, at: linkPath, nodes: nodes, arcs: arcs) else { continue }
            surviving.append((i, link))
        }

        reportBreaks(in: surviving, at: pointer(path, "links"), nodes: nodes)

        var chain = Chain(links: surviving.map(\.link))
        chain.name = optionalString(o, "name", at: path)
        chain.notes = optionalString(o, "notes", at: path)
        chain.hidden = optionalBool(o, "hidden", at: path)
        chain.style = style(o, at: path)
        chain.data = dataRecords(o, at: path)
        return chain
    }

    private func decodeLink(_ value: JSONValue, at path: String, nodes: [String: Position], arcs: [String: NetworkArc]) -> Link? {
        guard let o = object(value, at: path, what: "a link") else { return nil }
        report(unknownKeysIn: o, against: Keys.link, at: path)
        guard let arcName = requiredString(o, "arc", at: path) else { return nil }
        guard let arc = arcs[arcName] else {
            // SPEC 6.2: a link's whole meaning is the arc it traverses, so
            // there is no geometry to keep.
            diagnose(.linkArcNotInNetwork, at: path,
                     "`arc` names \"\(arcName)\", which is not in this network; the link was dropped")
            return nil
        }
        guard let from = endpoint(o, "from", at: path, arcName: arcName, arc: arc, nodes: nodes),
              let to = endpoint(o, "to", at: path, arcName: arcName, arc: arc, nodes: nodes) else { return nil }
        return Link(arc: arcName, from: from, to: to)
    }

    private func endpoint(_ o: [String: JSONValue], _ key: String, at path: String, arcName: String, arc: NetworkArc, nodes: [String: Position]) -> LinkEndpoint? {
        let fieldPath = pointer(path, key)
        guard let raw = o[key] else {
            diagnose(.requiredKeyMissing, at: path, "a link with no `\(key)` cannot exist and was dropped")
            return nil
        }
        if let name = raw.stringValue {
            // SPEC 6.2's remedy table: keep the link, and exclude that end from
            // the continuity comparison. The link still names an arc, so it
            // still carries geometry; only the junction is unknowable.
            if nodes[name] == nil {
                diagnose(.linkNodeNotInNetwork, at: fieldPath,
                         "`\(key)` names the node \"\(name)\", which is not in this network; the link was kept and this end takes no part in the continuity comparison")
            }
            return .node(name)
        }
        guard case .array = raw else {
            diagnose(.wrongTypeRequired, at: fieldPath,
                     "`\(key)` is \(raw.typeName); expected a node key or an inline position, so the link was dropped")
            return nil
        }
        guard let p = position(raw, at: fieldPath, cause: .wrongTypeRequired) else { return nil }
        // SPEC 3.3.3: an inline position MUST lie on the named arc.
        guard let distance = Projection.distance(from: p, toPolyline: arc.pips) else {
            diagnose(.inlineEndpointOnEmptyArc, at: fieldPath,
                     "the arc \"\(arcName)\" has no vertices, so an inline endpoint on it has no distance to measure; the link was dropped")
            return nil
        }
        if !Projection.isWithinTolerance(distance) {
            diagnose(.inlineEndpointBeyondTolerance, at: fieldPath,
                     "the inline endpoint is \(rounded(distance)) m from the arc \"\(arcName)\", beyond the 50 m tolerance; the link was dropped")
            return nil
        }
        return .inline(p)
    }

    /// SPEC 6.2: a chain's links MUST form one connected walk, and the
    /// diagnostic addresses the second link of the pair.
    ///
    /// Only pairs that were adjacent IN THE DOCUMENT and both survived are
    /// compared. `corpus/diagnose/link-names-absent-arc.expect.json` expects
    /// exactly one diagnostic and says why: "dropping the second link must not
    /// then also report the chain as broken, or one defect becomes two and the
    /// reader cannot tell how much is actually wrong." Closing the gap left by
    /// a dropped link and comparing across it would manufacture a second
    /// defect out of the first.
    private func reportBreaks(in surviving: [(index: Int, link: Link)], at linksPath: String, nodes: [String: Position]) {
        guard surviving.count > 1 else { return }
        for i in 1..<surviving.count {
            let previous = surviving[i - 1], current = surviving[i]
            guard previous.index + 1 == current.index else { continue }
            if !meets(previous.link.to, current.link.from, nodes: nodes) {
                diagnose(.chainNotConnected, at: "\(linksPath)/\(current.index)",
                         "this link does not begin where the previous one ended, so the chain is not one connected walk; both links were kept")
            }
        }
    }

    private func meets(_ a: LinkEndpoint, _ b: LinkEndpoint, nodes: [String: Position]) -> Bool {
        // SPEC 6.2: two node keys meet when the keys are equal; a node key and
        // an inline position meet when the node's position and the inline
        // position are equal by value. A reader that refused to compare the two
        // kinds would report a continuous walk as broken.
        // Key equality is a valid fast path, but only a fast path: two
        // DISTINCT node keys at the same position also meet, and comparing keys
        // alone would report a continuous walk as broken.
        if case .node(let x) = a, case .node(let y) = b, x == y { return true }
        guard let pa = resolve(a, nodes: nodes), let pb = resolve(b, nodes: nodes) else {
            // An end that names a node this network does not have takes no part
            // in the comparison (SPEC 6.2). Reporting a break here would turn
            // one defect into two.
            return true
        }
        return pa.isSamePlace(as: pb)
    }

    private func resolve(_ e: LinkEndpoint, nodes: [String: Position]) -> Position? {
        switch e {
        case .node(let k): return nodes[k]
        case .inline(let p): return p
        }
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
            diagnose(.folderNotPresent, at: pointer(path, key),
                     "`\(key)` names the folder \"\(value)\", which is not in this document; the item was kept at the top level")
            return nil
        }

        for i in document.folders?.indices ?? [].indices {
            document.folders![i].parent = check(document.folders![i].parent, at: "/folders/\(i)", key: "parent")
        }
        for i in document.networks?.indices ?? [].indices {
            document.networks![i].folder = check(document.networks![i].folder, at: "/networks/\(i)", key: "folder")
        }
        for i in document.routes?.indices ?? [].indices {
            document.routes![i].folder = check(document.routes![i].folder, at: "/routes/\(i)", key: "folder")
        }
        for i in document.tracks?.indices ?? [].indices {
            document.tracks![i].folder = check(document.tracks![i].folder, at: "/tracks/\(i)", key: "folder")
        }
        for i in document.waypoints?.indices ?? [].indices {
            document.waypoints![i].folder = check(document.waypoints![i].folder, at: "/waypoints/\(i)", key: "folder")
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

    /// A distance for a diagnostic's English, which nothing compares (SPEC 6.3).
    private func rounded(_ d: Double) -> String {
        String((d * 1000).rounded() / 1000)
    }

    private func report(unknownKeysIn o: [String: JSONValue], against shape: Keys.Shape, at path: String) {
        let known = shape.all
        for key in o.keys.sorted(by: codePointLess) where !known.contains(key) {
            // SPEC 6.2: record a diagnostic, skip the key, keep the item. An
            // unrecognised key is not an unusable value.
            diagnose(.unknownKey, at: pointer(path, key),
                     "unknown key `\(key)`; this format does not define it here, so it was skipped")
        }
    }

    private func object(_ value: JSONValue, at path: String, what: String) -> [String: JSONValue]? {
        guard let o = value.objectValue else {
            diagnose(.wrongTypeRequired, at: path, "expected \(what) but found \(value.typeName); the item was dropped")
            return nil
        }
        return o
    }

    private func optionalString(_ o: [String: JSONValue], _ key: String, at path: String) -> String? {
        guard let raw = o[key] else { return nil }
        guard let s = raw.stringValue else {
            diagnose(.wrongTypeOptional, at: pointer(path, key),
                     "`\(key)` is \(raw.typeName); expected a string, so the field was dropped and the item kept")
            return nil
        }
        return s
    }

    private func requiredString(_ o: [String: JSONValue], _ key: String, at path: String) -> String? {
        guard let raw = o[key] else {
            diagnose(.requiredKeyMissing, at: path, "no `\(key)`; the item cannot exist without it and was dropped")
            return nil
        }
        guard let s = raw.stringValue else {
            diagnose(.wrongTypeRequired, at: pointer(path, key),
                     "`\(key)` is \(raw.typeName); expected a string, so the item was dropped")
            return nil
        }
        return s
    }

    private func optionalBool(_ o: [String: JSONValue], _ key: String, at path: String) -> Bool? {
        guard let raw = o[key] else { return nil }
        guard let b = raw.boolValue else {
            diagnose(.wrongTypeOptional, at: pointer(path, key),
                     "`\(key)` is \(raw.typeName); expected a boolean, so the field was dropped and the item kept")
            return nil
        }
        return b
    }

    private func optionalItems<T>(_ o: [String: JSONValue], _ key: String, at path: String,
                                  decode: (JSONValue, String) -> T?) -> [T]? {
        guard let raw = o[key] else { return nil }
        guard let array = raw.arrayValue else {
            diagnose(.wrongTypeOptional, at: pointer(path, key),
                     "`\(key)` is \(raw.typeName); expected an array, so the field was dropped and the item kept")
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
    private func optionalTimestamp(_ o: [String: JSONValue], _ key: String, at path: String) -> String? {
        guard let raw = o[key] else { return nil }
        guard let text = raw.stringValue else {
            diagnose(.wrongTypeOptional, at: pointer(path, key),
                     "`\(key)` is \(raw.typeName); expected a timestamp string, so the field was dropped and the item kept")
            return nil
        }
        guard isUTCTimestamp(text) else {
            diagnose(.timestampNotUTC, at: pointer(path, key),
                     "`\(key)` is \"\(text)\"; a record-level timestamp must be UTC with a Z suffix, so the field was dropped and the item kept")
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
        func digits(_ range: Range<Int>) -> Bool { range.allSatisfy { c[$0].isASCII && c[$0].isNumber } }
        guard digits(0..<4), c[4] == "-", digits(5..<7), c[7] == "-", digits(8..<10),
              c[10] == "T", digits(11..<13), c[13] == ":", digits(14..<16), c[16] == ":",
              digits(17..<19) else { return false }
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
    private func style(_ o: [String: JSONValue], at path: String) -> OpaqueObject? {
        guard let raw = o["style"] else { return nil }
        guard var s = raw.objectValue else {
            diagnose(.wrongTypeOptional, at: pointer(path, "style"),
                     "`style` is \(raw.typeName); expected an object, so the field was dropped and the item kept")
            return nil
        }
        if let importance = s["importance"] {
            let d = importance.numberValue
            let usable = d != nil && d! >= 1 && d! <= 5 && d! == d!.rounded()
            if !usable {
                diagnose(.importanceOutOfRange, at: pointer(pointer(path, "style"), "importance"),
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
    private func dataRecords(_ o: [String: JSONValue], at path: String) -> [DataRecord]? {
        guard let raw = o["data"] else { return nil }
        guard let array = raw.arrayValue else {
            diagnose(.wrongTypeOptional, at: pointer(path, "data"),
                     "`data` is \(raw.typeName); expected an array of records, so the field was dropped and the item kept")
            return nil
        }
        var out: [DataRecord] = []
        for (i, v) in array.enumerated() {
            let recordPath = "\(pointer(path, "data"))/\(i)"
            guard var body = v.objectValue else {
                diagnose(.dataRecordTypeUnusable, at: recordPath,
                         "a data record is \(v.typeName); expected an object, so the record was dropped")
                continue
            }
            guard let type = body["@type"]?.stringValue else {
                diagnose(.dataRecordTypeUnusable, at: recordPath,
                         "a data record has no `@type`, or one that is not a string, so the record was dropped")
                continue
            }
            body.removeValue(forKey: "@type")
            out.append(DataRecord(type: type, body: body))
        }
        return out
    }

    private func pips(_ o: [String: JSONValue], at path: String) -> [Position]? {
        let pipsPath = pointer(path, "pips")
        guard let raw = o["pips"] else {
            diagnose(.requiredKeyMissing, at: path, "no `pips`; the item cannot exist without geometry and was dropped")
            return nil
        }
        guard let array = raw.arrayValue else {
            diagnose(.wrongTypeRequired, at: pipsPath,
                     "`pips` is \(raw.typeName); expected an array of positions, so the item was dropped")
            return nil
        }
        // SPEC 6.2: ONE UNUSABLE PIP DROPS THE CONTAINER, not the pip. Geometry
        // is continuous and structure is not — a missing vertex leaves a
        // shorter, plausible, wrong line with nothing to indicate it, while a
        // missing arc or segment leaves a gap the continuity rule of 3.3.3 can
        // see and report.
        var out: [Position] = []
        for (i, v) in array.enumerated() {
            guard let p = position(v, at: "\(pipsPath)/\(i)") else { return nil }
            out.append(p)
        }
        return out
    }

    /// SPEC 4.1. Longitude first, then latitude — the single most likely
    /// transposition bug in this format, and one that parses cleanly in either
    /// order.
    /// `cause` is what gets dropped, not what is wrong.
    ///
    /// The same malformed position is a different fact depending on where it
    /// sits: under a waypoint's REQUIRED `at` the item goes, so it is
    /// `wrongTypeRequired`; as one pip of a `pips` array only the pip goes, so it
    /// is `positionUnusable` and the arc survives. SPEC 6.2 gives the remedies
    /// and the corpus pins the first two codes; a reader that reported one code
    /// for all of them would make a consumer re-derive which happened.
    private func position(_ value: JSONValue, at path: String,
                          cause: Cause = .positionUnusable) -> Position? {
        guard let channels = value.arrayValue else {
            diagnose(cause, at: path,
                     "a position is \(value.typeName); expected a position array [lon, lat, ele, time_ms]" +
                     (cause == .wrongTypeRequired ? " — item dropped" : ""))
            return nil
        }
        guard channels.count >= 2 else {
            diagnose(cause, at: path,
                     "a position carries \(channels.count) channel(s); it MUST carry at least lon and lat")
            return nil
        }
        // THE PATH IS THE POSITION, NOT THE CHANNEL. SPEC 6.3 names the smallest
        // element no part of which could be used, and a position with a null
        // longitude is unusable whole — the latitude beside it locates nothing
        // on its own. The channel index goes in the message, which nothing
        // compares.
        guard channels[0].numberValue != nil else {
            diagnose(.positionUnusable, at: path, "longitude is \(channels[0].typeName); expected a number")
            return nil
        }
        guard channels[1].numberValue != nil else {
            diagnose(.positionUnusable, at: path, "latitude is \(channels[1].typeName); expected a number")
            return nil
        }
        for i in 2..<channels.count {
            switch channels[i] {
            case .number, .null: continue
            default:
                diagnose(.positionUnusable, at: path,
                         "channel \(i) is \(channels[i].typeName); from index 2 upward a channel is a number or null")
                return nil
            }
        }
        // Channels above index 3 are read and discarded. SPEC 4.1: "A reader
        // MUST ignore them and MUST NOT report a diagnostic for them" — and a
        // model that carried them would be carrying the document's SPELLING,
        // which this format stopped constraining.
        return Position(lon: channels[0].numberValue!, lat: channels[1].numberValue!,
                        elevation: channels.count > 2 ? channels[2].numberValue : nil,
                        timeMilliseconds: channels.count > 3 ? channels[3].numberValue : nil)
    }
}
