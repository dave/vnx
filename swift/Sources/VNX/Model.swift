import Foundation

/// A position: `[lon, lat, ele, time_ms]` on the wire (SPEC 4.1), and the four
/// values it carries here.
///
/// ⚠ IT DOES NOT RECORD HOW MANY CHANNELS THE DOCUMENT SPELLED. `[lon, lat,
/// null]` and `[lon, lat]` state the same thing, and a reader takes either; a
/// model that kept the difference would be keeping the document's SPELLING, and
/// a writer built on it would be reproducing bytes rather than content. A
/// writer emits the shortest spelling from these four values.
public struct Position: Equatable, Sendable {
  /// Longitude first, then latitude (SPEC 4.1).
  public let lon: Double
  public let lat: Double
  /// Metres, and absent is a first-class state rather than zero.
  public let elevation: Double?
  /// Unix milliseconds. Not rounded — there is nothing below its last digit.
  public let timeMilliseconds: Double?

  public init(lon: Double, lat: Double, elevation: Double? = nil, timeMilliseconds: Double? = nil) {
    self.lon = lon
    self.lat = lat
    self.elevation = elevation
    self.timeMilliseconds = timeMilliseconds
  }

  /// Whether two positions name the same place.
  ///
  /// Longitude and latitude only. SPEC 3.7 says two ring positions are
  /// distinct "when they differ in longitude or in latitude; no other channel
  /// takes part", and a closed ring is one whose last position equals its
  /// first "in longitude and latitude".
  public func isSamePlace(as other: Position) -> Bool {
    lon == other.lon && lat == other.lat
  }
}

/// An opaque JSON object: a style, or a data record body. SPEC 3.10 forbids
/// applying the unrecognised-key rule inside either.
public typealias OpaqueObject = [JSONKey: JSONValue]

/// A data record (SPEC 5). Everything but `@type` is opaque and survives a
/// read-modify-write at value level.
public struct DataRecord: Equatable, Sendable {
  public var type: String
  public var body: OpaqueObject

  public init(type: String, body: OpaqueObject = [:]) {
    self.type = type
    self.body = body
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

/// A top-level point, or a network's point. SPEC 3.6 makes them one shape; `folder`
/// appears only on the top-level spelling (SPEC 3.10).
public struct Point: Equatable, Sendable {
  public var at: Position
  public var name: String?
  public var notes: String?
  public var folder: String?
  public var hidden: Bool?
  /// The time the user pinned the item (SPEC 3.8), held as written for the
  /// reason `Track.started` is. `nil` is not pinned.
  public var pinned: String?
  public var style: OpaqueObject?
  public var data: [DataRecord]?
}

public struct Area: Equatable, Sendable {
  /// One OPEN ring of at least three distinct positions (SPEC 3.7): its last
  /// position does not repeat its first, and the boundary runs from the last
  /// back to the first. Holes and multiple rings are not expressible.
  public var polygon: [Position]
  public var name: String?
  public var notes: String?
  public var folder: String?
  public var hidden: Bool?
  /// The time the user pinned the item (SPEC 3.8), held as written for the
  /// reason `Track.started` is. `nil` is not pinned.
  public var pinned: String?
  public var style: OpaqueObject?
  public var data: [DataRecord]?
}

/// A point that carries no time: a node, or an arc pip (SPEC 3.3.1, 3.3.2).
///
/// A type of its own rather than a `Position`, so that a time on a node or an
/// arc pip cannot be built and so cannot be written. The reader drops such a
/// time with a diagnostic, and nothing downstream has to remember to.
public struct Vertex: Equatable, Sendable {
  public var lon: Double
  public var lat: Double
  /// Metres, and absent is a first-class state rather than zero.
  public var elevation: Double?

  public init(lon: Double, lat: Double, elevation: Double? = nil) {
    self.lon = lon
    self.lat = lat
    self.elevation = elevation
  }

  /// The vertex as a wire position, with no time.
  public var position: Position { Position(lon: lon, lat: lat, elevation: elevation) }
}

/// A position in a walk: a pass's `from`, `to` or `turn` (SPEC 3.3.3).
///
/// Longitude and latitude only. Its elevation is the arc line's at the point it
/// identifies, so storing one would be a second copy of a fact the arc already
/// holds.
public struct LonLat: Equatable, Sendable {
  public var lon: Double
  public var lat: Double

  public init(lon: Double, lat: Double) {
    self.lon = lon
    self.lat = lat
  }

  /// The walk position as a wire position.
  public var position: Position { Position(lon: lon, lat: lat) }
}

/// An arc of a network or of a plan (SPEC 3.3.2). Its line runs from the
/// position of its `from` node, through `pips`, to the position of its `to`
/// node. `pips` holds the INTERIOR points only and may be empty, and `from` and
/// `to` name two different nodes of the arc's owner.
public struct Arc: Equatable, Sendable {
  public var from: String
  public var to: String
  public var pips: [Vertex]
  public var data: [DataRecord]?

  public init(from: String, to: String, pips: [Vertex] = [], data: [DataRecord]? = nil) {
    self.from = from
    self.to = to
    self.pips = pips
    self.data = data
  }
}

/// Where a pass enters or leaves its arc: a node key, or a position on the arc.
public enum Endpoint: Equatable, Sendable {
  case node(String)
  case position(LonLat)
}

/// One entry of a walk (SPEC 3.3.3): the arc it covers, and where on that arc
/// it enters, turns and leaves.
///
/// ⚠ `from` AND `to` ARE PRESENT ONLY WHERE SPEC 3.3.3's RULE PUTS THEM: on the
/// first pass, on the last pass, and on a pass whose `from` is a position.
/// Everywhere else the entry is the node the walk is at and the exit is
/// derived. A `nil` here is that absence, not "unknown".
public struct Pass: Equatable, Sendable {
  public var arc: String
  public var from: Endpoint?
  public var to: Endpoint?
  public var turn: LonLat?
  /// The pass is pinned when this is set: the user fixed it with a handle at
  /// this position. The pin leaves what the pass covers unchanged (SPEC 3.3.3).
  public var pin: LonLat?

  public init(
    arc: String, from: Endpoint? = nil, to: Endpoint? = nil, turn: LonLat? = nil,
    pin: LonLat? = nil
  ) {
    self.arc = arc
    self.from = from
    self.to = to
    self.turn = turn
    self.pin = pin
  }
}

/// A highlight: a lightweight container for data objects along a walk over arcs
/// its network owns (SPEC 3.3.3). It has an optional name, and no notes or
/// style. The highlight does not own the arcs: two highlights over one trail
/// name the same arc.
public struct Highlight: Equatable, Sendable {
  public var walk: [Pass]
  public var name: String?
  public var hidden: Bool?
  /// The time the user pinned the item (SPEC 3.8), held as written for the
  /// reason `Track.started` is. `nil` is not pinned.
  public var pinned: String?
  public var data: [DataRecord]?
}

public struct Network: Equatable, Sendable {
  public var name: String?
  public var notes: String?
  public var folder: String?
  public var hidden: Bool?
  /// The time the user pinned the item (SPEC 3.8), held as written for the
  /// reason `Track.started` is. `nil` is not pinned.
  public var pinned: String?
  public var nodes: [JSONKey: Vertex]?
  public var arcs: [JSONKey: Arc]?
  public var highlights: [Highlight]?
  public var points: [Point]?
  public var areas: [Area]?
  public var data: [DataRecord]?
}

/// A top-level plan (SPEC 3.4). It owns a graph of the network's shape and
/// carries one walk over it, which runs node to node: no position, no turn and
/// no pin. Nothing outside the plan can reference its nodes or arcs.
public struct Plan: Equatable, Sendable {
  public var nodes: [JSONKey: Vertex]?
  public var arcs: [JSONKey: Arc]?
  public var walk: [Pass]
  public var name: String?
  public var notes: String?
  public var folder: String?
  public var hidden: Bool?
  /// The time the user pinned the item (SPEC 3.8), held as written for the
  /// reason `Track.started` is. `nil` is not pinned.
  public var pinned: String?
  public var style: OpaqueObject?
  public var data: [DataRecord]?
}

// In extensions, so that the memberwise initialisers stay available to this
// module's tests.
extension Highlight {
  public init(walk: [Pass]) {
    self.init(walk: walk, name: nil)
  }
}

extension Plan {
  public init(walk: [Pass]) {
    self.init(nodes: nil, arcs: nil, walk: walk)
  }
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
  /// The time the user pinned the item (SPEC 3.8), held as written for the
  /// reason `Track.started` is. `nil` is not pinned.
  public var pinned: String?
  public var style: OpaqueObject?
  /// Held as written. SPEC 4.3 pins the spelling — ISO 8601 in UTC with a
  /// literal `Z` and optional fractional seconds — and a writer must not
  /// change a timestamp it did not change, which a parsed `Date` cannot
  /// promise: it has no way to say whether the text carried fractional
  /// seconds.
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
  public var generator: String?
  public var created: String?

  /// All six collections are OPTIONAL, and `nil` and `[]` are both held.
  /// SPEC 3.1 has a READER treat an absent collection and an empty one alike,
  /// so either spelling is conforming and a writer may emit either; this model
  /// writes back what it was given, which is what makes a read-write-read a
  /// fixed point.
  public var folders: [Folder]?
  public var networks: [Network]?
  public var plans: [Plan]?
  public var tracks: [Track]?
  public var points: [Point]?
  public var areas: [Area]?
  /// The application keys (SPEC 3.9): every top-level key containing a full
  /// stop, each value opaque and preserved through a read-modify-write. A
  /// list rather than a dictionary; the reader leaves it sorted by code
  /// point, and the writer writes each key it holds. A JSON `null` is a
  /// present key whose value is `.null`, and an empty value stays empty.
  public var applicationKeys: [ApplicationKey] = []

  public init(vnx: String = VNX.version) {
    self.vnx = vnx
  }
}

/// One application key (SPEC 3.9). `value` is any JSON value; `.null` is a key
/// whose value is `null`, not an absent key.
public struct ApplicationKey: Equatable, Sendable {
  public var key: String
  public var value: JSONValue

  public init(key: String, value: JSONValue) {
    self.key = key
    self.value = value
  }
}

/// Thrown by `VNX.write` for an application key it cannot write: one whose
/// name contains no full stop, which a reader would take for an unrecognised
/// key (SPEC 3.9), or one held twice, where which value a reader keeps would be
/// its JSON parser's choice (SPEC 6.4).
public struct ApplicationKeyError: Error, Equatable, Sendable, CustomStringConvertible {
  public let key: String
  public let message: String
  public var description: String { message }
}

/// Thrown by `VNX.write` for a document its own reader would repair or drop:
/// a closed ring or one with fewer than three distinct positions (SPEC 3.7), an
/// arc that is a self-loop or names an absent node (SPEC 3.3.2), or a walk that
/// is not valid (SPEC 3.3.3). A writer never emits one of these; the model is
/// the caller's to fix.
public struct WriteError: Error, Equatable, Sendable, CustomStringConvertible {
  public let message: String
  public var description: String { message }

  init(_ message: String) {
    self.message = message
  }
}
