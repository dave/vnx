import Foundation

/// A position: `[lon, lat, ele, time_ms]` with trailing channels optionally
/// omitted and `null` marking an absent value from index 2 upward (SPEC 4.1).
///
/// The channels are held as written, and that is the load-bearing decision in
/// this whole model. `[lon, lat, null]` and `[lon, lat]` state the same thing,
/// but SPEC 4.1 says shortening happens at the serialisation boundary and is
/// NOT part of canonical form, so a re-render has to reproduce the spelling the
/// document carried. A model that stores `ele: Double?` has already lost that
/// and cannot re-render `accept/absent-elevation-and-time.vnx`.
///
/// Holding the raw channels also carries a channel above index 3 through a
/// read-modify-write, which is what SPEC 4.1's "a reader MUST ignore channels
/// beyond those it knows, so that a future channel is additive" needs to mean
/// for the additivity to be real.
public struct Position: Equatable, Sendable {
    /// At least two entries. Index 0 and 1 are finite numbers; index 2 upward
    /// is a number or `null`.
    public let channels: [JSONValue]

    public init(channels: [JSONValue]) {
        precondition(channels.count >= 2)
        self.channels = channels
    }

    public init(lon: Double, lat: Double, elevation: Double? = nil, timeMilliseconds: Double? = nil) {
        var c: [JSONValue] = [.number(lon), .number(lat)]
        if let t = timeMilliseconds {
            c.append(elevation.map(JSONValue.number) ?? .null)
            c.append(.number(t))
        } else if let e = elevation {
            c.append(.number(e))
        }
        self.channels = c
    }

    public var lon: Double { channels[0].numberValue! }
    public var lat: Double { channels[1].numberValue! }
    public var elevation: Double? { channels.count > 2 ? channels[2].numberValue : nil }
    public var timeMilliseconds: Double? { channels.count > 3 ? channels[3].numberValue : nil }
    public var arity: Int { channels.count }

    /// Whether two positions name the same place.
    ///
    /// Longitude and latitude only. SPEC 6.2 says a node key and an inline
    /// position "meet when the position of the named node and the inline
    /// position are equal by value", and an inline endpoint carries no
    /// elevation by construction (SPEC 3.3.3: "elevation is taken from the
    /// polyline and is never stored on the endpoint") while a node usually
    /// does. Comparing the elevation channel would report every such join as a
    /// break.
    public func isSamePlace(as other: Position) -> Bool {
        lon == other.lon && lat == other.lat
    }
}

/// An opaque JSON object: a style, or a data record body. SPEC 3.10 forbids
/// applying the unrecognised-key rule inside either.
public typealias OpaqueObject = [String: JSONValue]

/// A data record (SPEC 5). Everything but `@type` is opaque and survives a
/// read-modify-write at value level.
public struct DataRecord: Equatable, Sendable {
    public var type: String
    public var body: OpaqueObject

    public init(type: String, body: OpaqueObject = [:]) {
        self.type = type
        self.body = body
    }

    /// SPEC 5.3: the `app.viatrail.` namespace is reserved without regard to
    /// case, so a near-miss produced by a capitalising keyboard is not
    /// classified as another program's extension. This is a rule about the
    /// namespace only — `@type` is compared exactly everywhere else, and
    /// `App.viatrail.tag` is a different type from `app.viatrail.tag`.
    public var isInViatrailNamespace: Bool {
        type.lowercased().hasPrefix("app.viatrail.")
    }
}

public struct Folder: Equatable, Sendable {
    public var id: String
    public var parent: String?
    public var name: String?
    public var notes: String?
    public var hidden: Bool?
    public var data: [DataRecord]?
}

/// A waypoint, or a network's point. SPEC 3.6 makes them one shape; `folder`
/// appears only on the top-level spelling (SPEC 3.10).
public struct Waypoint: Equatable, Sendable {
    public var at: Position
    public var name: String?
    public var notes: String?
    public var folder: String?
    public var hidden: Bool?
    public var style: OpaqueObject?
    public var data: [DataRecord]?
}

public struct Area: Equatable, Sendable {
    /// One closed ring of at least four positions (SPEC 3.7). Holes and
    /// multiple rings are not expressible.
    public var polygon: [Position]
    public var name: String?
    public var notes: String?
    public var folder: String?
    public var hidden: Bool?
    public var style: OpaqueObject?
    public var data: [DataRecord]?
}

/// An arc inside a network. Its `pips` are its complete point list, including
/// both endpoint node positions (SPEC 3.3.2).
public struct NetworkArc: Equatable, Sendable {
    public var from: String
    public var to: String
    public var pips: [Position]
    public var data: [DataRecord]?
}

/// A chain link endpoint: a node key, or a position on the named arc.
public enum LinkEndpoint: Equatable, Sendable {
    case node(String)
    case inline(Position)
}

public struct Link: Equatable, Sendable {
    public var arc: String
    public var from: LinkEndpoint
    public var to: LinkEndpoint
}

public struct Chain: Equatable, Sendable {
    public var links: [Link]
    public var name: String?
    public var notes: String?
    public var hidden: Bool?
    public var style: OpaqueObject?
    public var data: [DataRecord]?
}

public struct Network: Equatable, Sendable {
    public var name: String?
    public var notes: String?
    public var folder: String?
    public var hidden: Bool?
    public var nodes: [String: Position]?
    public var arcs: [String: NetworkArc]?
    public var chains: [Chain]?
    public var points: [Waypoint]?
    public var areas: [Area]?
    public var data: [DataRecord]?
}

/// An arc inside a top-level route. It owns its geometry and carries no
/// identifier, because nothing can reference it (SPEC 3.4).
public struct RouteArc: Equatable, Sendable {
    public var pips: [Position]
    public var data: [DataRecord]?
}

public struct Route: Equatable, Sendable {
    public var arcs: [RouteArc]
    public var name: String?
    public var notes: String?
    public var folder: String?
    public var hidden: Bool?
    public var style: OpaqueObject?
    public var data: [DataRecord]?
}

public struct TrackSegment: Equatable, Sendable {
    public var pips: [Position]
}

public struct Track: Equatable, Sendable {
    public var segments: [TrackSegment]
    public var name: String?
    public var notes: String?
    public var folder: String?
    public var hidden: Bool?
    public var style: OpaqueObject?
    /// Held as written. SPEC 4.3 pins the spelling — ISO 8601 in UTC with a
    /// literal `Z` and optional fractional seconds — and a re-render has to
    /// reproduce the text, which a parsed `Date` cannot promise.
    public var started: String?
    public var ended: String?
    public var data: [DataRecord]?

    /// Moving time in milliseconds, or `nil` for unknown (SPEC 3.5).
    ///
    /// The sum over segments of the span between each segment's first and last
    /// pip times. Deliberately not the wall-clock span between `started` and
    /// `ended`, which pauses lengthen.
    ///
    /// `nil` where the pips carry no times. SPEC 3.5 is explicit that this must
    /// not be reported as zero, which is indistinguishable from a track on
    /// which nobody moved.
    public var movingTimeMilliseconds: Double? {
        if segments.isEmpty { return nil }
        var total: Double = 0
        for segment in segments {
            guard let first = segment.pips.first, let last = segment.pips.last else { continue }
            guard let t0 = first.timeMilliseconds, let t1 = last.timeMilliseconds else { return nil }
            total += t1 - t0
        }
        return total
    }
}

/// A VNX document (SPEC 3.1).
public struct Document: Equatable, Sendable {
    /// The version the document declared. A reader that re-encodes writes this
    /// back, not its own (SPEC 2.2).
    public var vnx: String
    public var profile: String?
    public var generator: String?
    public var created: String?

    /// All seven collections are OPTIONAL. `nil` and `[]` are different
    /// documents: SPEC 3.10 says an empty array is distinct from an absent one,
    /// and a re-render has to reproduce which one the document carried.
    public var folders: [Folder]?
    public var networks: [Network]?
    public var routes: [Route]?
    public var tracks: [Track]?
    public var waypoints: [Waypoint]?
    public var areas: [Area]?
    /// Opaque through 0.1.x (SPEC 3.9), preserved through a read-modify-write.
    public var rulesets: [JSONValue]?

    public init(vnx: String = VNX.version) {
        self.vnx = vnx
    }
}
