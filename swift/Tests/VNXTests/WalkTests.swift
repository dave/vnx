import Foundation
import XCTest

@testable import VNX

/// Format 0.2's graph rules — nodes, arcs, walks and rings — one test per rule.
///
/// The corpus pins one case per code. These tests pin what one case cannot: each
/// branch of a rule, the order SPEC 6.2 checks a walk in, the cascade from a
/// dropped node to a dropped highlight, and the writer's refusals, which no corpus
/// tier reaches because every corpus case starts from bytes.
final class WalkTests: XCTestCase {

  private func read(_ body: String) throws -> VNX.ReadResult {
    try VNX.read(Data("{\"vnx\":\"0.9.0\",\(body)}".utf8))
  }

  /// The pairs a runner compares (SPEC 6.3), sorted so a failure reads as a
  /// list.
  private func reported(_ result: VNX.ReadResult) -> [String] {
    result.diagnostics.map { "\($0.code) \($0.path)" }.sorted()
  }

  /// Nodes a b c d along one parallel, and three arcs: ab, cb (drawn c to b)
  /// and cd. The same graph as the corpus's walk cases.
  private let graph = """
    "nodes":{"a":[-3.60,57.10],"b":[-3.59,57.10],"c":[-3.58,57.10],"d":[-3.57,57.10]},
    "arcs":{"ab":{"from":"a","to":"b","pips":[[-3.595,57.101]]},
            "cb":{"from":"c","to":"b","pips":[]},
            "cd":{"from":"c","to":"d","pips":[[-3.575,57.102],[-3.572,57.101]]}}
    """

  /// A network over `graph` carrying one highlight per walk.
  private func network(_ walks: String...) throws -> VNX.ReadResult {
    let highlights = walks.map { "{\"walk\":\($0)}" }.joined(separator: ",")
    return try read("\"networks\":[{\(graph),\"highlights\":[\(highlights)]}]")
  }

  /// A route over `graph`'s shape carrying `walk`.
  private func route(_ walk: String) throws -> VNX.ReadResult {
    try read("\"routes\":[{\(graph),\"walk\":\(walk)}]")
  }

  // MARK: - Walks that are valid (SPEC 3.3.3)

  func testEveryWalkFormReadsCleanAndWritesBackEqual() throws {
    let result = try network(
      #"[{"arc":"ab","from":[-3.597,57.1005],"to":"b"},{"arc":"cb","pin":[-3.5853,57.1001]},{"arc":"cd","turn":[-3.574,57.102]},{"arc":"cb"},{"arc":"ab","to":[-3.598,57.1002]}]"#,
      #"[{"arc":"cd","from":[-3.576,57.1015],"to":[-3.573,57.1012]}]"#,
      #"[{"arc":"cd","from":[-3.575,57.102],"turn":[-3.579,57.1003],"to":"d"}]"#,
      #"[{"arc":"ab","from":[-3.595,57.101],"to":[-3.595,57.101]}]"#,
      #"[]"#)
    XCTAssertEqual(reported(result), [])
    let walk = try XCTUnwrap(result.document.networks?[0].highlights?[0].walk)
    XCTAssertEqual(
      walk,
      [
        Pass(arc: "ab", from: .position(LonLat(lon: -3.597, lat: 57.1005)), to: .node("b")),
        Pass(arc: "cb", pin: LonLat(lon: -3.5853, lat: 57.1001)),
        Pass(arc: "cd", turn: LonLat(lon: -3.574, lat: 57.102)),
        Pass(arc: "cb"),
        Pass(arc: "ab", to: .position(LonLat(lon: -3.598, lat: 57.1002))),
      ])
    XCTAssertEqual(result.document.networks?[0].highlights?.map(\.walk.count), [5, 1, 1, 1, 0])

    let again = try VNX.read(try VNX.write(result.document))
    XCTAssertEqual(again.diagnostics, [])
    XCTAssertEqual(again.document, result.document)
  }

  /// Valid-walk rule 4, and the rule for what a pass covers: a pass without
  /// `to` leaves by the other end of its arc, or, with a `turn`, by the node
  /// it entered by. Continuity is decided from that exit, so each half of the
  /// rule is a walk that is connected under it and broken under the other.
  func testAWalkIsConnectedAtTheNodeThePreviousPassLeftBy() throws {
    // ab entered at a, no turn: it leaves at b, and cb touches b.
    let result = try network(
      #"[{"arc":"ab","from":"a"},{"arc":"cb","to":"c"}]"#,
      // ab entered at a, with a turn: it leaves at a, and cb does not
      // touch a.
      #"[{"arc":"ab","from":"a","turn":[-3.595,57.101]},{"arc":"cb","to":"c"}]"#,
      // ab entered at b, no turn: it leaves at a, and cb does not touch a.
      #"[{"arc":"ab","from":"b"},{"arc":"cb","to":"c"}]"#,
      // ab entered at b, with a turn: it leaves at b again.
      #"[{"arc":"ab","from":"b","turn":[-3.595,57.101]},{"arc":"cb","to":"c"}]"#)
    XCTAssertEqual(
      reported(result),
      [
        "walk-not-connected /networks/0/highlights/1/walk/1",
        "walk-not-connected /networks/0/highlights/2/walk/1",
      ])
    XCTAssertEqual(result.document.networks?[0].highlights?.count, 2)
  }

  // MARK: - `pin` (SPEC 3.3.3)

  func testAPinIsReadAsAPositionAndWrittenBack() throws {
    let result = try network(#"[{"arc":"ab","from":"a","to":"b","pin":[-3.596,57.1003]}]"#)
    XCTAssertEqual(reported(result), [])
    XCTAssertEqual(
      result.document.networks?[0].highlights?[0].walk[0].pin, LonLat(lon: -3.596, lat: 57.1003))
    XCTAssertTrue(
      String(decoding: try VNX.write(result.document), as: UTF8.self).contains(
        "\"pin\":[-3.596,57.1003]"))
  }

  /// SPEC 6.2 step 3: a `pin` is a position only. One that will not parse
  /// drops the highlight, named at `pin`.
  func testAPinThatIsNotAPositionDropsItsHighlightAtThePin() throws {
    let result = try network(
      #"[{"arc":"ab","from":"a","to":"b","pin":"b"}]"#,
      #"[{"arc":"ab","from":"a","to":"b","pin":true}]"#,
      #"[{"arc":"ab","from":"a","to":"b","pin":[-3.596]}]"#)
    XCTAssertEqual(
      reported(result),
      [
        "position-unusable /networks/0/highlights/0/walk/0/pin",
        "position-unusable /networks/0/highlights/1/walk/0/pin",
        "position-unusable /networks/0/highlights/2/walk/0/pin",
      ])
    XCTAssertEqual(result.document.networks?[0].highlights, [])
  }

  /// SPEC 6.2 step 2: `pinned` is an unknown key. It is skipped, the
  /// highlight stays, and the pass is not pinned.
  func testPinnedIsAnUnknownKeyAndPinsNothing() throws {
    let result = try network(#"[{"arc":"ab","from":"a","to":"b","pinned":true}]"#)
    XCTAssertEqual(reported(result), ["unknown-key /networks/0/highlights/0/walk/0/pinned"])
    XCTAssertEqual(
      result.document.networks?.first?.highlights?.first?.walk,
      [Pass(arc: "ab", from: .node("a"), to: .node("b"))])
  }

  // MARK: - Channels a node, a pip or a walk position cannot carry

  func testATimeOnANodeIsDroppedAndTheNodeKept() throws {
    let result = try read(#""networks":[{"nodes":{"a":[1,2,3,4],"b":[5,6,null,7],"c":[8,9,10]}}]"#)
    XCTAssertEqual(
      reported(result), ["extra-channel /networks/0/nodes/a", "extra-channel /networks/0/nodes/b"])
    XCTAssertEqual(
      result.document.networks?[0].nodes,
      [
        "a": Vertex(lon: 1, lat: 2, elevation: 3), "b": Vertex(lon: 5, lat: 6),
        "c": Vertex(lon: 8, lat: 9, elevation: 10),
      ])
  }

  /// SPEC 6.3's third rule: ONE diagnostic for the arc, at its `pips`,
  /// however many pips carried a time.
  func testATimeOnArcPipsIsReportedOncePerArc() throws {
    let result = try read(
      #""networks":[{"nodes":{"a":[0,0],"b":[1,1]},"arcs":{"x":{"from":"a","to":"b","pips":[[0.2,0.2,5,100],[0.4,0.4],[0.6,0.6,null,300]]}}}]"#
    )
    XCTAssertEqual(reported(result), ["arc-pip-time /networks/0/arcs/x/pips"])
    XCTAssertEqual(
      result.document.networks?[0].arcs?["x"]?.pips,
      [
        Vertex(lon: 0.2, lat: 0.2, elevation: 5), Vertex(lon: 0.4, lat: 0.4),
        Vertex(lon: 0.6, lat: 0.6),
      ])
  }

  /// SPEC 3.3.3: a walk position carries longitude and latitude only. The
  /// channel goes, the pass stays, and the diagnostic names the key.
  func testAWalkPositionKeepsLongitudeAndLatitudeOnly() throws {
    let result = try network(
      #"[{"arc":"cd","from":[-3.576,57.1015,400],"turn":[-3.574,57.102,null,9],"to":[-3.573,57.1012,null]}]"#
    )
    XCTAssertEqual(
      reported(result),
      [
        "extra-channel /networks/0/highlights/0/walk/0/from",
        "extra-channel /networks/0/highlights/0/walk/0/turn",
      ], "`[lon, lat, null]` carries no extra channel")
    XCTAssertEqual(
      result.document.networks?[0].highlights?[0].walk[0].from,
      .position(LonLat(lon: -3.576, lat: 57.1015)))
  }

  // MARK: - Arcs (SPEC 3.3.2) and the cascade (SPEC 6.2)

  func testASelfLoopArcIsDroppedAndTakesEveryHighlightWalkingItWithIt() throws {
    let result = try read(
      """
      "networks":[{"nodes":{"a":[0,0],"b":[1,1]},
      "arcs":{"ab":{"from":"a","to":"b","pips":[]},"loop":{"from":"a","to":"a","pips":[[0.5,0.5]]}},
      "highlights":[{"walk":[{"arc":"ab","from":"a","to":"b"}]},{"walk":[{"arc":"loop","from":"a","to":"a"}]}]}]
      """)
    XCTAssertEqual(
      reported(result),
      [
        "pass-arc-absent /networks/0/highlights/1/walk/0",
        "self-loop-arc /networks/0/arcs/loop",
      ])
    XCTAssertEqual(result.document.networks?[0].arcs?.keys.map(\.string), ["ab"])
    XCTAssertEqual(
      result.document.networks?[0].highlights?.count, 1, "only the highlight walking the loop goes")
  }

  func testAnArcNamingAnAbsentNodeIsDroppedAndEachEndIsReported() throws {
    let result = try read(
      """
      "networks":[{"nodes":{"a":[0,0]},"arcs":{"x":{"from":"ghost","to":"phantom","pips":[]},"y":{"from":"a","to":"nowhere","pips":[]}}}]
      """)
    XCTAssertEqual(
      reported(result),
      [
        "arc-node-absent /networks/0/arcs/x/from",
        "arc-node-absent /networks/0/arcs/x/to",
        "arc-node-absent /networks/0/arcs/y/to",
      ])
    XCTAssertEqual(result.document.networks?[0].arcs, [:])
  }

  /// A node that will not parse goes; each arc naming it then goes under
  /// arc-node-absent, and each highlight walking that arc under pass-arc-absent.
  /// Three diagnostics for three things lost, and the route beside it — its
  /// own graph, with the same keys — is untouched.
  func testADroppedNodeCascadesToItsArcAndToItsHighlight() throws {
    let result = try read(
      """
      "networks":[{"nodes":{"a":["x",0],"b":[1,1]},"arcs":{"ab":{"from":"a","to":"b","pips":[]}},
      "highlights":[{"walk":[{"arc":"ab","from":"b","to":"a"}]}]}],
      "routes":[{"nodes":{"a":[0,0],"b":[1,1]},"arcs":{"ab":{"from":"a","to":"b","pips":[]}},
      "walk":[{"arc":"ab","from":"b","to":"a"}]}]
      """)
    XCTAssertEqual(
      reported(result),
      [
        "arc-node-absent /networks/0/arcs/ab/from",
        "pass-arc-absent /networks/0/highlights/0/walk/0",
        "position-unusable /networks/0/nodes/a",
      ])
    XCTAssertEqual(result.document.networks?[0].highlights, [])
    XCTAssertEqual(result.document.routes?.count, 1)
  }

  func testAPassThatIsNotAnObjectOrNamesNoArcDropsItsHighlight() throws {
    let result = try network(
      #"[{"arc":"ab","from":"a"}, "ab"]"#,
      #"[{"from":"a","to":"b"}]"#,
      #"[{"arc":7,"from":"a","to":"b"}]"#,
      #"[{"arc":"zz","from":"a","to":"b"}]"#)
    XCTAssertEqual(
      reported(result),
      [
        "pass-arc-absent /networks/0/highlights/0/walk/1",
        "pass-arc-absent /networks/0/highlights/1/walk/0",
        "pass-arc-absent /networks/0/highlights/2/walk/0",
        "pass-arc-absent /networks/0/highlights/3/walk/0",
      ])
    XCTAssertEqual(result.document.networks?[0].highlights, [])
  }

  // MARK: - The rules a walk is checked against (SPEC 3.3.3, 6.2)

  /// Each branch of the rule for `from` and `to`. A key the rule requires and
  /// the pass lacks is named at the pass; a key it forbids, at that key.
  func testTheRuleForFromAndTo() throws {
    let result = try network(
      // 1. The first pass has `from`.
      #"[{"arc":"ab","to":"b"}]"#,
      // 2. The last pass has `to`.
      #"[{"arc":"ab","from":"a"}]"#,
      // 3. A pass whose `from` is a position has `to`.
      #"[{"arc":"ab","from":[-3.597,57.1005]},{"arc":"cb","to":"c"}]"#,
      // 4. Only the first pass has `from`...
      #"[{"arc":"ab","from":"a"},{"arc":"cb","from":"b","to":"c"}]"#,
      // ...and a `to` only where rules 2 and 3 put one.
      #"[{"arc":"ab","from":"a","to":"b"},{"arc":"cb","to":"c"}]"#)
    XCTAssertEqual(
      reported(result),
      [
        "walk-spelling /networks/0/highlights/0/walk/0",
        "walk-spelling /networks/0/highlights/1/walk/0",
        "walk-spelling /networks/0/highlights/2/walk/0",
        "walk-spelling /networks/0/highlights/3/walk/1/from",
        "walk-spelling /networks/0/highlights/4/walk/0/to",
      ])
    XCTAssertEqual(result.document.networks?[0].highlights, [])
  }

  /// Valid-walk rules 3, 4 and 5, each at the path SPEC 6.2 step 8 gives it.
  func testAWalkThatIsNotConnected() throws {
    let result = try network(
      // Rule 3: a `from` node that is not an end of its arc.
      #"[{"arc":"ab","from":"c","to":"b"}]"#,
      // Rule 4: a pass not entered at an end of its arc.
      #"[{"arc":"ab","from":"a"},{"arc":"cd","to":"d"}]"#,
      // Rule 3: a `to` node that is not an end of its arc.
      #"[{"arc":"ab","from":"a","to":"d"}]"#,
      // Rule 5: only the last pass leaves at a position.
      #"[{"arc":"ab","from":[-3.597,57.1005],"to":[-3.596,57.1006]},{"arc":"cb","to":"c"}]"#)
    XCTAssertEqual(
      reported(result),
      [
        "walk-not-connected /networks/0/highlights/0/walk/0/from",
        "walk-not-connected /networks/0/highlights/1/walk/1",
        "walk-not-connected /networks/0/highlights/2/walk/0/to",
        "walk-not-connected /networks/0/highlights/3/walk/0/to",
      ])
  }

  func testAPassCarryingBothTurnAndPinDropsItsHighlight() throws {
    let result = try network(
      #"[{"arc":"ab","from":"a","turn":[-3.595,57.101],"pin":[-3.596,57.1003],"to":"a"}]"#)
    XCTAssertEqual(reported(result), ["pinned-turn /networks/0/highlights/0/walk/0"])
  }

  func testAFromToOrTurnThatWillNotParseDropsItsHighlightAtThatKey() throws {
    let result = try network(
      #"[{"arc":"ab","from":7,"to":"b"}]"#,
      #"[{"arc":"ab","from":"a","to":[1]}]"#,
      #"[{"arc":"ab","from":"a","turn":"a","to":"a"}]"#,
      #"[{"arc":"ab","from":"a","turn":[null,57],"to":"a"}]"#)
    XCTAssertEqual(
      reported(result),
      [
        "position-unusable /networks/0/highlights/0/walk/0/from",
        "position-unusable /networks/0/highlights/1/walk/0/to",
        "position-unusable /networks/0/highlights/2/walk/0/turn",
        "position-unusable /networks/0/highlights/3/walk/0/turn",
      ])
  }

  /// SPEC 3.4: a route's walk runs node to node. Each of `from`, `to`,
  /// `turn` and `pin` is named at its key, and the ROUTE goes.
  func testARouteWalkCarriesNoPositionTurnOrPin() throws {
    for (walk, key) in [
      (#"[{"arc":"ab","from":[-3.597,57.1005],"to":"b"}]"#, "from"),
      (#"[{"arc":"ab","from":"a","to":[-3.596,57.1006]}]"#, "to"),
      (#"[{"arc":"ab","from":"a","turn":[-3.595,57.101],"to":"a"}]"#, "turn"),
      (#"[{"arc":"ab","from":"a","to":"b","pin":[-3.596,57.1003]}]"#, "pin"),
    ] {
      let result = try route(walk)
      XCTAssertEqual(reported(result), ["route-pin-or-position /routes/0/walk/0/\(key)"], walk)
      XCTAssertEqual(result.document.routes, [], walk)
    }
    let kept = try route(#"[{"arc":"ab","from":"b"},{"arc":"ab","to":"b"}]"#)
    XCTAssertEqual(reported(kept), [])
    XCTAssertEqual(kept.document.routes?.first?.walk.count, 2)
  }

  /// SPEC 6.2: a route is its own highlight, so a route's walk takes every
  /// highlight rule too, and where a highlight would go the route goes.
  func testARouteIsDroppedWhereAHighlightWouldBe() throws {
    let result = try route(#"[{"arc":"ab","from":"a"},{"arc":"cd","to":"d"}]"#)
    XCTAssertEqual(reported(result), ["walk-not-connected /routes/0/walk/1"])
    XCTAssertEqual(result.document.routes, [])
  }

  /// SPEC 6.2's order: the passes in order, and each pass through the list
  /// in order. At the first check that drops the highlight nothing further in
  /// the walk is examined — but a diagnostic that does not drop the highlight,
  /// met before it, is still recorded.
  func testOnlyTheFirstFaultInAWalkIsReported() throws {
    let result = try network(
      // Pass 0: an unknown key (kept), then pin and turn (dropped).
      // Pass 1 names an absent arc, and is never reached.
      #"[{"arc":"ab","from":"a","turn":[-3.595,57.101],"pin":[-3.596,57.1003],"x":1},{"arc":"zz","to":"b"}]"#,
      // Within one pass, step 5 (pinned-turn) comes before step 6
      // (spelling): this first pass also lacks `from`.
      #"[{"arc":"ab","turn":[-3.595,57.101],"pin":[-3.596,57.1003],"to":"a"}]"#,
      // Step 3 (a position that will not parse) comes before step 5.
      #"[{"arc":"ab","from":"a","turn":[],"pin":[-3.596,57.1003],"to":"a"}]"#,
      // Step 6 (spelling) comes before step 7 (connection): `from` on a
      // later pass that is also not an end of its arc.
      #"[{"arc":"ab","from":"a"},{"arc":"cd","from":"a","to":"d"}]"#)
    XCTAssertEqual(
      reported(result),
      [
        "pinned-turn /networks/0/highlights/0/walk/0",
        "pinned-turn /networks/0/highlights/1/walk/0",
        "position-unusable /networks/0/highlights/2/walk/0/turn",
        "unknown-key /networks/0/highlights/0/walk/0/x",
        "walk-spelling /networks/0/highlights/3/walk/1/from",
      ])
  }

  /// In a route, step 4 comes before step 5: a pass with a turn and a pin is
  /// a route rule broken, not a pinned turn.
  func testInARouteThePinOrPositionRuleComesBeforePinnedTurn() throws {
    let result = try route(
      #"[{"arc":"ab","from":"a","turn":[-3.595,57.101],"pin":[-3.596,57.1003],"to":"a"}]"#)
    XCTAssertEqual(reported(result), ["route-pin-or-position /routes/0/walk/0/turn"])
  }

  /// A missing or wrong-typed `walk` is a REQUIRED key gone, named at the
  /// key, and the owner goes.
  func testAMissingOrWrongTypedWalkDropsItsOwner() throws {
    let result = try read(
      """
      "networks":[{"highlights":[{"name":"none"},{"walk":{}}]}],
      "routes":[{"name":"none"},{"walk":"ab"}]
      """)
    XCTAssertEqual(
      reported(result),
      [
        "wrong-type-required /networks/0/highlights/0/walk",
        "wrong-type-required /networks/0/highlights/1/walk",
        "wrong-type-required /routes/0/walk",
        "wrong-type-required /routes/1/walk",
      ])
  }

  // MARK: - Rings (SPEC 3.7)

  func testAClosedRingDropsOnlyItsRepeat() throws {
    let result = try read(#""areas":[{"polygon":[[0,0],[1,0],[1,1],[0,1],[0,0,99]]}]"#)
    XCTAssertEqual(
      reported(result), ["ring-closed /areas/0/polygon/4"],
      "equal in longitude and latitude is closed; the elevation takes no part")
    XCTAssertEqual(result.document.areas?[0].polygon.count, 4)
  }

  func testARingWithFewerThanThreeDistinctPositionsIsDropped() throws {
    let result = try read(
      """
      "areas":[{"polygon":[[0,0],[1,0]]},{"polygon":[[0,0],[1,0],[1,0,50]]},{"polygon":[[0,0],[1,0],[1,1]]}]
      """)
    XCTAssertEqual(
      reported(result),
      ["ring-too-few-points /areas/0/polygon", "ring-too-few-points /areas/1/polygon"])
    XCTAssertEqual(result.document.areas?.count, 1)
  }

  /// SPEC 6.2: "A ring that repeats its first position and holds too few
  /// distinct positions takes both", at their two paths.
  func testAClosedRingOfTooFewPointsTakesBothDiagnostics() throws {
    let result = try read(#""networks":[{"areas":[{"polygon":[[0,0],[1,0],[0,0]]}]}]"#)
    XCTAssertEqual(
      reported(result),
      [
        "ring-closed /networks/0/areas/0/polygon/2",
        "ring-too-few-points /networks/0/areas/0/polygon",
      ])
    XCTAssertEqual(result.document.networks?[0].areas, [])
  }

  // MARK: - A required key that is absent (SPEC 6.2)

  /// One code for absent and wrong-typed, named at the key in both cases,
  /// because the remedy and the outcome are the same.
  func testAnAbsentRequiredKeyIsNamedAtTheKey() throws {
    let result = try read(
      """
      "waypoints":[{"name":"w"}],
      "networks":[{"nodes":{"a":[0,0],"b":[1,1]},"arcs":{"x":{"to":"b","pips":[]},"y":{"from":"a","to":"b"}}}]
      """)
    XCTAssertEqual(
      reported(result),
      [
        "wrong-type-required /networks/0/arcs/x/from",
        "wrong-type-required /networks/0/arcs/y/pips",
        "wrong-type-required /waypoints/0/at",
      ])
  }

  // MARK: - The writer refuses what its reader would repair or drop

  private func document(network: Network? = nil, route: Route? = nil, area: Area? = nil) -> Document
  {
    var d = Document()
    d.networks = network.map { [$0] }
    d.routes = route.map { [$0] }
    d.areas = area.map { [$0] }
    return d
  }

  private let nodes: [JSONKey: Vertex] = [
    "a": Vertex(lon: 0, lat: 0), "b": Vertex(lon: 1, lat: 0), "c": Vertex(lon: 2, lat: 0),
  ]
  private let arcs: [JSONKey: Arc] = ["ab": Arc(from: "a", to: "b"), "bc": Arc(from: "b", to: "c")]

  private func assertRefused(
    _ d: Document, _ fragment: String, file: StaticString = #filePath, line: UInt = #line
  ) {
    XCTAssertThrowsError(try VNX.write(d), file: file, line: line) { error in
      guard let e = error as? WriteError else {
        return XCTFail("expected a WriteError, got \(error)", file: file, line: line)
      }
      XCTAssertTrue(
        e.message.contains(fragment), "\(e.message) does not name \(fragment)", file: file,
        line: line)
    }
  }

  func testTheWriterRefusesARingItsReaderWouldRepairOrDrop() {
    let square = [Position(lon: 0, lat: 0), Position(lon: 1, lat: 0), Position(lon: 1, lat: 1)]
    assertRefused(
      document(area: Area(polygon: square + [Position(lon: 0, lat: 0)], name: "closed")), "open")
    assertRefused(
      document(
        area: Area(
          polygon: [
            Position(lon: 0, lat: 0), Position(lon: 1, lat: 0),
            Position(lon: 1, lat: 0, elevation: 3),
          ], name: "flat")),
      "3 distinct")
    XCTAssertNoThrow(try VNX.write(document(area: Area(polygon: square))))
  }

  func testTheWriterRefusesAnArcItsReaderWouldDrop() {
    var n = Network()
    n.nodes = nodes
    n.arcs = ["loop": Arc(from: "a", to: "a")]
    assertRefused(document(network: n), "self-loop")
    n.arcs = ["x": Arc(from: "a", to: "ghost")]
    assertRefused(document(network: n), "ghost")
    n.arcs = ["x": Arc(from: "a", to: "b")]
    n.nodes = nil
    assertRefused(document(network: n), "not in its owner's nodes")
  }

  /// The writer steps the reader's own `WalkCursor`, so each code the reader
  /// would report is a refusal here, and the refusal names it.
  func testTheWriterRefusesAWalkItsReaderWouldDrop() {
    var n = Network()
    n.nodes = nodes
    n.arcs = arcs
    for (walk, code) in [
      ([Pass(arc: "zz", from: .node("a"), to: .node("b"))], "pass-arc-absent"),
      ([Pass(arc: "ab", to: .node("b"))], "walk-spelling"),
      (
        [Pass(arc: "ab", from: .node("a")), Pass(arc: "bc", from: .node("b"), to: .node("c"))],
        "walk-spelling"
      ),
      ([Pass(arc: "ab", from: .node("c"), to: .node("b"))], "walk-not-connected"),
      ([Pass(arc: "ab", from: .node("b")), Pass(arc: "bc", to: .node("c"))], "walk-not-connected"),
      (
        [
          Pass(
            arc: "ab", from: .node("a"), to: .node("a"), turn: LonLat(lon: 0.5, lat: 0),
            pin: LonLat(lon: 0.4, lat: 0))
        ], "pinned-turn"
      ),
    ] {
      n.highlights = [Highlight(walk: walk)]
      assertRefused(document(network: n), code)
    }
    n.highlights = [
      Highlight(walk: [Pass(arc: "ab", from: .node("a")), Pass(arc: "bc", to: .node("c"))])
    ]
    XCTAssertNoThrow(try VNX.write(document(network: n)))
  }

  func testTheWriterRefusesARouteWalkWithAPositionTurnOrPin() {
    var r = Route(walk: [Pass(arc: "ab", from: .node("a"), to: .position(LonLat(lon: 0.5, lat: 0)))]
    )
    r.nodes = nodes
    r.arcs = arcs
    assertRefused(document(route: r), "route-pin-or-position")
    r.walk = [Pass(arc: "ab", from: .node("a"), to: .node("b"), pin: LonLat(lon: 0.5, lat: 0))]
    assertRefused(document(route: r), "route-pin-or-position")
    r.walk = [Pass(arc: "ab", from: .node("a"), to: .node("b"))]
    XCTAssertNoThrow(try VNX.write(document(route: r)))
  }

  /// SPEC 3.3.3: a pinned pass is written with its `pin`, a position, and a
  /// pass without one is written without it.
  func testTheWriterEmitsAPinAsAPosition() throws {
    var n = Network()
    n.nodes = nodes
    n.arcs = arcs
    n.highlights = [
      Highlight(walk: [
        Pass(arc: "ab", from: .node("a"), pin: LonLat(lon: 0.5, lat: 0)),
        Pass(arc: "bc", to: .node("c")),
      ])
    ]
    let written = String(decoding: try VNX.write(document(network: n)), as: UTF8.self)
    XCTAssertEqual(written.components(separatedBy: "\"pin\"").count - 1, 1)
    XCTAssertTrue(written.contains("\"pin\":[0.5,0]"))
  }
}
