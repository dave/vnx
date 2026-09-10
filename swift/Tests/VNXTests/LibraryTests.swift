import Foundation
import XCTest
@testable import VNX

/// What the conformance corpus does not reach.
///
/// SPEC 1.3 says the corpus decides conformance, and the corpus README says
/// what it cannot check. Everything here is a rule this specification states in
/// prose that no corpus case exercises, so passing the corpus is not evidence
/// about any of it.
final class LibraryTests: XCTestCase {

    private func read(_ json: String) throws -> VNX.ReadResult {
        try VNX.read(Data(json.utf8))
    }

    private func minimal(_ body: String) -> String {
        "{\"vnx\":\"0.1.0\"\(body.isEmpty ? "" : ",")\(body)}"
    }

    // MARK: - Versions (SPEC 2.2)

    func testVersionComponentsAreCompletelyNumeric() throws {
        // "Each component is one or more decimal digits, with no bound on
        // length and no rule against a leading zero: `00.01.0000` is a legal
        // spelling of 0.1.0."
        let result = try read("{\"vnx\":\"00.01.0000\"}")
        XCTAssertEqual(result.document.vnx, "00.01.0000",
                       "a reader that re-encodes writes the version the document declared")
        XCTAssertEqual(try VNX.write(result.document), Data("{\"vnx\":\"00.01.0000\"}".utf8))
    }

    func testVersionsAcceptedAndRefused() throws {
        for accepted in ["0.1.0", "0.1.3", "0.0.9", "0.1.99", "00.1.0", "0.01.000000000000000000005"] {
            XCTAssertNoThrow(try read("{\"vnx\":\"\(accepted)\"}"), accepted)
        }
        for tooNew in ["0.2.0", "1.0.0", "9.9.9", "0.10.0"] {
            XCTAssertThrowsError(try read("{\"vnx\":\"\(tooNew)\"}"), tooNew) {
                XCTAssertEqual(($0 as? Refusal)?.cause, .versionTooNew, tooNew)
            }
        }
        // "A version string that is not three digit-runs separated by full
        // stops MUST be refused, and MUST NOT be coerced."
        for malformed in ["0.1", "0.1.0.0", "0.1.x", "0.1.+1", "", "0..1", "0.1.", " 0.1.0"] {
            XCTAssertThrowsError(try read("{\"vnx\":\"\(malformed)\"}"), malformed) {
                XCTAssertEqual(($0 as? Refusal)?.cause, .versionMalformed, malformed)
                XCTAssertTrue(($0 as! Refusal).message.contains("MAJOR.MINOR.PATCH"),
                              "the refusal must name the expected shape: that is what tells the holder what to produce instead")
            }
        }
    }

    /// SPEC 2.2, as corrected 2026-09-09: "The rules a reader applies are
    /// selected by the DOCUMENT's MAJOR component, not by the reader's."
    ///
    /// This reader implements 0.x, so every document it accepts is a 0.x
    /// document and the observable behaviour is unchanged — which is exactly why
    /// the rule has to be written down rather than inferred from a passing
    /// suite. The refusal message is what separates the two cases: a document in
    /// another major series is refused for being another series, not for having
    /// a higher minor.
    func testTheDocumentsMajorSelectsTheRules() throws {
        for other in ["1.0.0", "2.5.9", "9.9.9"] {
            XCTAssertThrowsError(try read("{\"vnx\":\"" + other + "\"}"), other) {
                let refusal = $0 as! Refusal
                XCTAssertEqual(refusal.cause, .versionTooNew, other)
                XCTAssertTrue(refusal.message.contains("MUST NOT read another one under its own rules"),
                              "the refusal names the series rule, not the minor rule: " + refusal.message)
            }
        }
        XCTAssertThrowsError(try read("{\"vnx\":\"0.2.0\"}")) {
            XCTAssertTrue(($0 as! Refusal).message.contains("breaking axis"))
        }
        XCTAssertNoThrow(try read("{\"vnx\":\"0.0.9\"}"))
        XCTAssertNoThrow(try read("{\"vnx\":\"0.1.99\"}"))
    }

    func testTopLevelMustBeAnObjectCarryingVNX() {
        XCTAssertThrowsError(try read("[]")) { XCTAssertEqual(($0 as? Refusal)?.cause, .notAVNXDocument) }
        XCTAssertThrowsError(try read("\"0.1.0\"")) { XCTAssertEqual(($0 as? Refusal)?.cause, .notAVNXDocument) }
        XCTAssertThrowsError(try read("{}")) { XCTAssertEqual(($0 as? Refusal)?.cause, .notAVNXDocument) }
        XCTAssertThrowsError(try read("{\"vnx\":10}")) { XCTAssertEqual(($0 as? Refusal)?.cause, .versionMalformed) }
    }

    /// SPEC 6.2: a plain document is refused for exactly four causes and a
    /// reader MUST NOT refuse one for any other. Everything here is damage a
    /// reader has to tolerate.
    func testDamageIsToleratedRatherThanRefused() throws {
        let damaged = minimal("""
        "waypoints":[{"at":"not a position"},{"at":[1,2],"style":7,"importance":9}],
        "tracks":[{"segments":"nope"}],
        "areas":[{"polygon":[[1,2],[3,4]]}],
        "folders":[{"name":"no id"}],
        "networks":[{"arcs":{"a":{"from":"x","to":"y","pips":[[0,0],[1,1]]}},"chains":[{"links":[{"arc":"ghost","from":"x","to":"y"}]}]}]
        """)
        let result = try read(damaged)
        XCTAssertGreaterThan(result.diagnostics.count, 5)
        XCTAssertEqual(result.document.waypoints?.count, 1, "the usable waypoint survives")
        XCTAssertEqual(result.document.folders?.count, 0)
        XCTAssertEqual(result.document.areas?.count, 0)
        XCTAssertEqual(result.document.tracks?.count, 0)
    }

    // MARK: - Positions (SPEC 4.1)

    /// SPEC 4.1: a writer emits the SHORTEST spelling, and a reader takes any
    /// of them. So the arity a document happened to carry is not preserved —
    /// `[1, 2, null]` is read as a position with no elevation and written back
    /// as `[1, 2]`.
    ///
    /// An interior absence is the exception, and it is not a matter of taste:
    /// dropping the `null` from `[1, 2, null, 4]` would move the time into the
    /// elevation channel.
    func testAWriterEmitsTheShortestSpellingOfAPosition() throws {
        let json = minimal("""
        "waypoints":[{"at":[1,2]},{"at":[1,2,null]},{"at":[1,2,3]},{"at":[1,2,null,4]},{"at":[1,2,3,4]}]
        """)
        let document = try read(json).document
        XCTAssertEqual(document.waypoints?.map(\.at.elevation), [nil, nil, 3, nil, 3])
        XCTAssertEqual(document.waypoints?.map(\.at.timeMilliseconds), [nil, nil, nil, 4, 4])

        let written = String(decoding: try VNX.write(document), as: UTF8.self)
        XCTAssertTrue(written.contains("[1,2]"), "a trailing null is dropped")
        XCTAssertTrue(written.contains("[1,2,null,4]"), "an interior absence keeps its null")
        XCTAssertFalse(written.contains("[1,2,null]"), "and nothing carries a trailing null")
    }

    /// SPEC 4.1: "A position MAY carry more channels than this version defines.
    /// A reader MUST ignore them and MUST NOT report a diagnostic for them."
    ///
    /// Ignored, and not carried through a read-modify-write. The rule that asked
    /// a writer to preserve a channel it did not understand was dropped in
    /// September 2026: it was a formatter-era rule, and the reference
    /// implementation never satisfied it.
    func testChannelsAboveIndexThreeAreIgnoredWithoutADiagnostic() throws {
        let result = try read(minimal("\"waypoints\":[{\"at\":[1,2,3,4,5,null]}]"))
        XCTAssertEqual(result.diagnostics, [], "an unknown channel is not a diagnostic")
        XCTAssertEqual(result.document.waypoints?[0].at,
                       Position(lon: 1, lat: 2, elevation: 3, timeMilliseconds: 4))
        XCTAssertEqual(String(decoding: try VNX.write(result.document), as: UTF8.self)
            .contains("[1,2,3,4]"), true)
    }

    /// SPEC 4.1: a writer rounds longitude and latitude to 6 decimal places and
    /// elevation to 1, and emits the shortest spelling. A reader does neither —
    /// "a reader MUST NOT round. Decoding preserves full binary64 precision."
    func testAWriterRoundsAndShortensAndAReaderDoesNot() throws {
        let json = minimal("\"waypoints\":[{\"at\":[-3.6712345678901235,57.11812345678901,340.06,null]}]")
        let document = try read(json).document
        // BY BIT PATTERN. The message here used to claim the reader "keeps every
        // bit" while comparing by value, which is half a guard: two doubles one
        // ulp apart are not equal, but a value comparison that fails prints two
        // identical numbers and reads as a broken test.
        // `testTheParserConvertsAFullPrecisionLiteralCorrectly` is the guard
        // proper; this is the same claim made honestly where it already stood.
        XCTAssertEqual(document.waypoints?[0].at.lon.bitPattern, 0xc00d_5eb0_3aa8_431e,
                       "the reader keeps every bit")
        XCTAssertEqual(document.waypoints?[0].at.lat.bitPattern, 0x404c_8f1e_ab5f_e64e)
        XCTAssertTrue(String(decoding: try VNX.write(document), as: UTF8.self)
            .contains("[-3.671235,57.118123,340.1]"),
            "a writer rounds and drops the trailing null")
    }

    /// SPEC 4.1: "The rounded value is the value with at most that many decimal
    /// places nearest to the binary64 value the writer was handed, with a tie
    /// resolved to even. An elevation of `340.25` becomes `340.2`, not `340.3`."
    ///
    /// Swift's bare `.rounded()` rounds away from zero and gets every one of
    /// these wrong. Only `corpus/write/` and `corpus/numbers/` reach the rule
    /// from a fixture, so this is a third instrument on the shortest path.
    func testRoundingTiesGoToEven() throws {
        func written(_ p: Position) throws -> String {
            String(decoding: try VNX.write({ var d = Document(); d.waypoints = [Waypoint(at: p)]; return d }()),
                   as: UTF8.self)
        }
        XCTAssertTrue(try written(Position(lon: 1, lat: 2, elevation: 340.25)).contains("[1,2,340.2]"))
        XCTAssertTrue(try written(Position(lon: 1, lat: 2, elevation: 340.35)).contains("[1,2,340.4]"))
        XCTAssertTrue(try written(Position(lon: 1, lat: 2, elevation: -340.25)).contains("[1,2,-340.2]"))
        // The SPELLING here is Swift's shortest round-trip decimal, which uses
        // an exponent below 1e-4. SPEC 4.2 makes that the writer's own business
        // — the round trip is defined at value level — so the assertion is on
        // the text this library emits and not on a form the format requires.
        XCTAssertTrue(try written(Position(lon: 0.00000125, lat: 0.00000175)).contains("[1e-06,2e-06]"))
    }

    func testWriteKeepsAnInteriorNull() throws {
        var document = Document()
        document.waypoints = [Waypoint(at: Position(lon: 1.0, lat: 2.0, elevation: nil, timeMilliseconds: 1778914991000))]
        XCTAssertTrue(String(decoding: try VNX.write(document), as: UTF8.self)
            .contains("[1,2,null,1778914991000]"),
            "dropping the null would move the time into the elevation channel")
    }

    // MARK: - What a writer emits (SPEC 4.2, 4.4)

    /// SPEC 4.2: a number round-trips at VALUE level, not at byte level.
    ///
    /// ⚠ `JSONSerialization` WOULD FAIL THIS, and that is why this library
    /// encodes with `JSONEncoder`: it renders `-3.67` as `-3.6699999999999999`,
    /// which parses back to the same double and is still 16 digits of noise in
    /// every file. Measured on this platform rather than assumed.
    func testNumbersAreWrittenAsTheShortestSpellingThatRoundTrips() throws {
        var document = Document()
        document.waypoints = [Waypoint(at: Position(lon: -3.67, lat: 340, elevation: 0.1))]
        let written = String(decoding: try VNX.write(document), as: UTF8.self)
        XCTAssertTrue(written.contains("[-3.67,340,0.1]"), "got \(written)")
    }

    /// SPEC 4.4: nothing about a document's whitespace is constrained, and this
    /// writer emits none. A 200,000-pip track is 25.5 MB indented and 8.3 MB
    /// compact, which is the whole argument.
    func testTheOutputIsCompact() throws {
        var document = Document()
        document.waypoints = [Waypoint(at: Position(lon: 1, lat: 2), name: "x", notes: nil,
                                       folder: nil, hidden: nil, style: nil, data: nil)]
        let written = String(decoding: try VNX.write(document), as: UTF8.self)
        XCTAssertFalse(written.contains("\n"), "no newline")
        XCTAssertFalse(written.contains(": "), "no space after a colon")
    }

    /// A data record body and a style are opaque and survive a write at VALUE
    /// level (SPEC 5.1 rule 4), whatever they hold.
    func testAnOpaqueBodySurvivesAWriteWhateverItHolds() throws {
        let document = try read(minimal("""
        "waypoints":[{"at":[1,2],"data":[{"@type":"com.example.x","v":[1,null,2],"w":[1,"a"],"deep":{"k":{}}}]}]
        """)).document
        let back = try VNX.read(try VNX.write(document)).document
        XCTAssertEqual(back.waypoints?[0].data, document.waypoints?[0].data)
    }

    func testAnEmptyCollectionIsWrittenAsAnEmptyCollection() throws {
        // SPEC 3.1 has a reader treat an absent collection and an empty one
        // alike, so either spelling is conforming. This writer emits back what
        // the model holds, which is what makes the round trip a fixed point.
        XCTAssertEqual(try VNX.write(try read(minimal("\"waypoints\":[]")).document),
                       Data("{\"vnx\":\"0.1.0\",\"waypoints\":[]}".utf8))
        XCTAssertEqual(try VNX.write(try read("{\"vnx\":\"0.1.0\"}").document),
                       Data("{\"vnx\":\"0.1.0\"}".utf8))
    }

    // MARK: - Data records (SPEC 5)

    /// SPEC 5.3: the `app.viatrail.` prefix is matched case-insensitively for
    /// the purposes of profile classification, and NOWHERE else. `@type` itself
    /// is compared exactly.
    func testNamespaceIsCaseInsensitiveButTypeIdentityIsNot() {
        XCTAssertTrue(DataRecord(type: "app.viatrail.tag").isInViatrailNamespace)
        XCTAssertTrue(DataRecord(type: "App.viatrail.x").isInViatrailNamespace)
        XCTAssertTrue(DataRecord(type: "APP.VIATRAIL.X").isInViatrailNamespace)
        XCTAssertFalse(DataRecord(type: "com.example.tag").isInViatrailNamespace)
        XCTAssertNotEqual(DataRecord(type: "App.viatrail.tag"), DataRecord(type: "app.viatrail.tag"))
    }

    /// SPEC 6.2: "a reader MUST NOT check the type, range or enumeration of a
    /// field inside" a data record body. Checking them would break the
    /// mechanism by which an unrecognised record survives a read-modify-write.
    func testRecordBodiesAreNotValidated() throws {
        let json = minimal("""
        "waypoints":[{"at":[1,2],"data":[{"@type":"app.viatrail.camp","flat":"maybe","capacity":null,"importance":99}]}]
        """)
        let result = try read(json)
        XCTAssertEqual(result.diagnostics, [], "nothing inside a record body is a reader's business")
        XCTAssertEqual(result.document.waypoints?[0].data?.count, 1)
    }

    func testRecordWithNoTypeIsDropped() throws {
        let result = try read(minimal("\"waypoints\":[{\"at\":[1,2],\"data\":[{\"x\":1},{\"@type\":7},{\"@type\":\"ok\"}]}]"))
        XCTAssertEqual(result.document.waypoints?[0].data?.map(\.type), ["ok"])
        XCTAssertEqual(result.diagnostics.filter { $0.cause == .dataRecordTypeUnusable }.count, 2)
    }

    // MARK: - Style (SPEC 3.10, 3.6, 6.2)

    /// A style object is opaque, with one exception the specification states
    /// twice: `importance` is "an integer from 1 to 5, and that is normative",
    /// and SPEC 6.2 names it as the worked example of a value outside a stated
    /// range.
    func testImportanceIsTheOnlyCheckedStyleKey() throws {
        let result = try read(minimal("""
        "waypoints":[{"at":[1,2],"style":{"importance":9,"icon-size":999,"invented-key":{"deep":1}}}]
        """))
        XCTAssertEqual(result.diagnostics.map(\.cause), [.importanceOutOfRange])
        XCTAssertNil(result.document.waypoints?[0].style?["importance"], "the field goes")
        XCTAssertNotNil(result.document.waypoints?[0].style?["invented-key"], "the item, and the rest of the style, stay")
        XCTAssertNotNil(result.document.waypoints?[0].style?["icon-size"])
    }

    func testImportanceInRangeIsKept() throws {
        for value in [1, 2, 3, 4, 5] {
            let result = try read(minimal("\"waypoints\":[{\"at\":[1,2],\"style\":{\"importance\":\(value)}}]"))
            XCTAssertEqual(result.diagnostics, [], "importance \(value)")
        }
    }

    // MARK: - Hidden (SPEC 3.8)

    func testHiddenAbsenceMeansVisibleAndIsNotInvented() throws {
        let document = try read(minimal("\"waypoints\":[{\"at\":[1,2]},{\"at\":[3,4],\"hidden\":true}]")).document
        XCTAssertNil(document.waypoints?[0].hidden, "absence means visible; it is not `false`")
        XCTAssertEqual(document.waypoints?[1].hidden, true)
        XCTAssertFalse(String(decoding: try VNX.write(document), as: UTF8.self).contains("\"hidden\":false"))
    }

    // MARK: - Folders (SPEC 6.2)

    func testFolderReferenceThatDoesNotResolveKeepsTheItemAtTheTopLevel() throws {
        let result = try read(minimal("\"waypoints\":[{\"at\":[1,2],\"folder\":\"nowhere\"}]"))
        XCTAssertEqual(result.diagnostics.map(\.cause), [.folderNotPresent])
        XCTAssertEqual(result.diagnostics.map(\.path), ["/waypoints/0/folder"])
        XCTAssertEqual(result.document.waypoints?.count, 1, "dropping the item would lose content over a lost grouping")
        XCTAssertNil(result.document.waypoints?[0].folder)
    }

    /// "A folder with no `id` is dropped under the rule above. Items that named
    /// it are then covered by the previous paragraph and sit at the top level,
    /// so a folder's contents survive the loss of the folder."
    func testAFoldersContentsSurviveTheLossOfTheFolder() throws {
        let result = try read(minimal("""
        "folders":[{"name":"no id here"}],"waypoints":[{"at":[1,2],"folder":"gone"}]
        """))
        XCTAssertEqual(result.document.folders, [])
        XCTAssertEqual(result.document.waypoints?.count, 1)
        XCTAssertEqual(Set(result.diagnostics.map(\.cause)), [.requiredKeyMissing, .folderNotPresent])
    }

    // MARK: - Networks (SPEC 3.3, 6.2)

    func testArcNamingAnAbsentNodeIsKept() throws {
        let result = try read(minimal("""
        "networks":[{"nodes":{"a":[0,0]},"arcs":{"x":{"from":"a","to":"missing","pips":[[0,0],[1,1]]}}}]
        """))
        XCTAssertEqual(result.document.networks?[0].arcs?.count, 1, "the arc is kept; its geometry is real")
        XCTAssertEqual(result.diagnostics.map(\.cause), [.arcNodeNotInNetwork])
    }

    /// SPEC 6.2's remedy table: "keep the link; exclude that end from the
    /// continuity comparison". The link still names an arc, so it still carries
    /// geometry; only the junction is unknowable.
    func testLinkNamingAnAbsentNodeIsKeptAndLeavesTheContinuityComparison() throws {
        let result = try read(minimal("""
        "networks":[{"nodes":{"a":[0,0],"b":[1,1]},
        "arcs":{"x":{"from":"a","to":"b","pips":[[0,0],[1,1]]}},
        "chains":[{"links":[{"arc":"x","from":"a","to":"missing"},{"arc":"x","from":"b","to":"a"}]}]}]
        """))
        XCTAssertEqual(result.document.networks?[0].chains?[0].links.count, 2)
        XCTAssertEqual(result.diagnostics.map(\.cause), [.linkNodeNotInNetwork],
                       "no break is reported across the unknowable end: one defect must not become two")
    }

    /// SPEC 6.2: "Two distinct node keys at the same position therefore meet:
    /// comparing the keys alone would report a continuous walk as broken."
    func testTwoDistinctNodeKeysAtOnePositionMeet() throws {
        let result = try read(minimal("""
        "networks":[{"nodes":{"a":[0,0],"b":[1,1],"b2":[1,1],"c":[2,2]},
        "arcs":{"x":{"from":"a","to":"b","pips":[[0,0],[1,1]]},"y":{"from":"b2","to":"c","pips":[[1,1],[2,2]]}},
        "chains":[{"links":[{"arc":"x","from":"a","to":"b"},{"arc":"y","from":"b2","to":"c"}]}]}]
        """))
        XCTAssertEqual(result.diagnostics, [])
    }

    /// SPEC 3.3.3: "A polyline of no vertices has no distance: the reader MUST
    /// record a diagnostic and drop the link rather than invent a number."
    func testInlineEndpointOnAnArcWithNoVerticesIsDroppedNotMeasured() throws {
        let result = try read(minimal("""
        "networks":[{"arcs":{"x":{"from":"a","to":"b","pips":[]}},
        "chains":[{"links":[{"arc":"x","from":[0,0],"to":[1,1]}]}]}]
        """))
        XCTAssertEqual(result.diagnostics.filter { $0.cause == .inlineEndpointOnEmptyArc }.count, 1)
        XCTAssertEqual(result.document.networks?[0].chains?[0].links, [])
    }

    /// SPEC 3.3.3: "A polyline of one vertex is that point."
    func testSingleVertexPolylineIsThatPoint() {
        let one = [Position(lon: -3, lat: 57)]
        XCTAssertEqual(Projection.distance(from: Position(lon: -3, lat: 57), toPolyline: one), 0)
        let d = Projection.distance(from: Position(lon: -3.001, lat: 57), toPolyline: one)!
        XCTAssertEqual(d, 0.001 * .pi / 180 * Projection.earthRadiusMetres * cos(57 * .pi / 180), accuracy: 1e-6)
    }

    /// SPEC 3.3.3: "A position exactly 50 metres from its arc is within
    /// tolerance and is accepted."
    ///
    /// The spec adds that no conformance case can pin this and none should be
    /// attempted, because a distance at exactly the threshold is not reachable
    /// by a fixture — the computed value depends on the model and no two models
    /// land on 50.000. So the comparison is a named predicate, and this is the
    /// only place it is checked. `<` in place of `<=` refuses a legal document
    /// and nothing in any corpus would say so.
    func testTheToleranceBoundaryIsInclusive() {
        XCTAssertTrue(Projection.isWithinTolerance(50.0))
        XCTAssertTrue(Projection.isWithinTolerance(50.0.nextDown))
        XCTAssertTrue(Projection.isWithinTolerance(0))
        XCTAssertFalse(Projection.isWithinTolerance(50.0.nextUp))
        XCTAssertFalse(Projection.isWithinTolerance(50.000001))
    }

    /// The two corpus tolerance cases, checked as numbers rather than as
    /// verdicts, so a model that is wrong by a hair shows the hair.
    func testProjectionReadsTheCorpusTolerancePairExactly() {
        let ascending = [Position(lon: -3, lat: 55), Position(lon: -3, lat: 68)]
        XCTAssertEqual(Projection.distance(from: Position(lon: -2.998942483187279, lat: 67.5),
                                           toPolyline: ascending)!, 45.0, accuracy: 0.001)
        let descending = [Position(lon: -3, lat: 68), Position(lon: -3, lat: 55)]
        XCTAssertEqual(Projection.distance(from: Position(lon: -2.999115464096596, lat: 56),
                                           toPolyline: descending)!, 55.0, accuracy: 0.001)
    }

    /// SPEC 3.5, as corrected 2026-09-09: moving time is derivable only when
    /// EVERY segment carries a time on its first and its last pip. Otherwise a
    /// reader reports unknown, and MUST NOT report a sum over the segments that
    /// do carry times.
    ///
    /// "A partial sum is worse than zero rather than better: it is
    /// indistinguishable from a complete one, so no consumer can tell which it
    /// was given, where zero is at least recognisable as suspicious."
    func testMovingTimeRefusesToReportAPartialSum() throws {
        let mixed = try read(minimal("""
        "tracks":[{"name":"Half timed","segments":[
          {"pips":[[0,0,null,1000],[1,1,null,4000]]},
          {"pips":[[2,2],[3,3]]}]}]
        """))
        XCTAssertNil(mixed.document.tracks?[0].movingTimeMilliseconds,
                     "3000 would be a partial sum, indistinguishable from a complete one")

        let empty = try read(minimal("\"tracks\":[{\"name\":\"Nothing\",\"segments\":[]}]"))
        XCTAssertNil(empty.document.tracks?[0].movingTimeMilliseconds)

        let whole = try read(minimal("""
        "tracks":[{"name":"Timed","segments":[
          {"pips":[[0,0,null,1000],[1,1,null,4000]]},
          {"pips":[[2,2,null,9000],[3,3,null,10000]]}]}]
        """))
        XCTAssertEqual(whole.document.tracks?[0].movingTimeMilliseconds, 4000)
    }

    /// SPEC 6.2: "A reader MUST compare a node key by its resolved position
    /// rather than refusing to compare it, or a walk that is continuous is
    /// reported as broken."
    func testANodeKeyMeetsAnInlinePositionAtTheSamePlace() throws {
        let result = try read(minimal("""
        "networks":[{"nodes":{"a":[0,0,500],"b":[1,1]},
        "arcs":{"x":{"from":"a","to":"b","pips":[[0,0],[1,1]]}},
        "chains":[{"links":[{"arc":"x","from":"a","to":"b"},{"arc":"x","from":[1,1],"to":"a"}]}]}]
        """))
        XCTAssertEqual(result.diagnostics, [], "keys and inline positions must be comparable to each other")
    }

    /// The node in that test carries an elevation and the inline position
    /// cannot, so comparing the elevation channel would report every such join
    /// as a break.
    func testMeetingIgnoresTheElevationChannel() {
        XCTAssertTrue(Position(lon: 1, lat: 2, elevation: 500).isSamePlace(as: Position(lon: 1, lat: 2)))
        XCTAssertFalse(Position(lon: 1, lat: 2).isSamePlace(as: Position(lon: 1, lat: 2.0000001)))
    }

    /// `corpus/diagnose/link-names-absent-arc.expect.json` insists on exactly
    /// one diagnostic. That case drops the LAST link; this is the case it does
    /// not cover, where the dropped link is in the middle and closing the gap
    /// would manufacture a second defect out of the first.
    func testADroppedMiddleLinkDoesNotManufactureABreak() throws {
        let result = try read(minimal("""
        "networks":[{"nodes":{"a":[0,0],"b":[1,1],"c":[2,2]},
        "arcs":{"ab":{"from":"a","to":"b","pips":[[0,0],[1,1]]},"ca":{"from":"c","to":"a","pips":[[2,2],[0,0]]}},
        "chains":[{"links":[
          {"arc":"ab","from":"a","to":"b"},
          {"arc":"ghost","from":"b","to":"c"},
          {"arc":"ca","from":"c","to":"a"}]}]}]
        """))
        XCTAssertEqual(result.diagnostics.map(\.cause), [.linkArcNotInNetwork],
                       "one defect must not become two")
        XCTAssertEqual(result.document.networks?[0].chains?[0].links.count, 2)
    }

    // MARK: - Areas (SPEC 3.7)

    func testRingMustHaveFourPositionsAndClose() throws {
        for bad in ["[[0,0],[1,0],[0,0]]", "[[0,0],[1,0],[1,1],[0,1]]"] {
            let result = try read(minimal("\"areas\":[{\"polygon\":\(bad)}]"))
            XCTAssertEqual(result.document.areas, [], bad)
            XCTAssertEqual(result.diagnostics.map(\.cause), [.polygonUnclosed], bad)
        }
        let good = try read(minimal("\"areas\":[{\"polygon\":[[0,0],[1,0],[1,1],[0,0]]}]"))
        XCTAssertEqual(good.diagnostics, [])
    }

    // MARK: - Timestamps (SPEC 4.3)

    /// SPEC 4.3, as corrected 2026-09-09: a record-level timestamp is checked
    /// for SHAPE only, and a reader MUST NOT refuse or diagnose one on calendar
    /// grounds. `2026-13-45T99:99:99Z` matches the grammar and is accepted.
    ///
    /// The reason is convergence rather than leniency: a reader built on a date
    /// library gets the calendar check for free and one parsing the grammar
    /// directly does not, so silence here would make the two disagree.
    func testTimestampsAreCheckedForShapeOnlyAndNeverForCalendar() throws {
        for impossible in ["2026-13-45T99:99:99Z", "0000-00-00T00:00:00Z", "9999-99-99T99:99:99.000Z"] {
            let result = try read(minimal("\"created\":\"" + impossible + "\""))
            XCTAssertEqual(result.diagnostics, [], impossible)
            XCTAssertEqual(result.document.created, impossible, impossible)
        }
    }

    func testTimestampShapes() throws {
        for good in ["2026-05-12T07:03:11Z", "2026-05-12T07:03:11.5Z", "2026-05-12T07:03:11.123456789Z"] {
            XCTAssertEqual(try read(minimal("\"created\":\"\(good)\"")).diagnostics, [], good)
        }
        for bad in ["2026-05-12T07:03:11+01:00", "2026-05-12T07:03:11", "2026-05-12", "2026-05-12T07:03:11.Z", "now"] {
            let result = try read(minimal("\"created\":\"\(bad)\""))
            XCTAssertEqual(result.diagnostics.map(\.cause), [.timestampNotUTC], bad)
            XCTAssertNil(result.document.created, bad)
        }
    }

    // MARK: - Duplicate object keys (SPEC 6.4)

    func testDuplicateObjectKeysResolveLastWinsWithNoDiagnostic() throws {
        let result = try read("{\"vnx\":\"0.1.0\",\"waypoints\":[{\"at\":[1,2]}],\"waypoints\":[{\"at\":[3,4]},{\"at\":[5,6]}]}")
        XCTAssertEqual(result.document.waypoints?.count, 2)
        XCTAssertEqual(result.diagnostics, [], "a documented limitation, not a rule")
    }

    // MARK: - The parser

    func testTheParserIsStrict() {
        for bad in ["{\"vnx\":\"0.1.0\",}", "{'vnx':'0.1.0'}", "{\"vnx\":\"0.1.0\"} trailing",
                    "{\"vnx\":NaN}", "{\"vnx\":Infinity}", "{\"vnx\":-Infinity}", "{\"vnx\":01}",
                    "{\"vnx\":.5}", "{\"vnx\":+1}", "{\"vnx\":\"a\\u00\"}", "// c\n{}",
                    "{\"vnx\":\"tab\there\"}"] {
            XCTAssertThrowsError(try read(bad), bad) {
                XCTAssertEqual(($0 as? Refusal)?.cause, .malformedJSON, bad)
                XCTAssertTrue(($0 as! Refusal).message.contains("malformed JSON"), bad)
            }
        }
    }

    /// ⚠ THE PARSER'S CONVERSION IS CORRECTLY ROUNDED, AND NO CORPUS CASE CAN
    /// SEE THAT ANY MORE.
    ///
    /// `accept/full-precision-coordinate.vnx` was the guard while the tier
    /// compared bytes. It is not one now: the accept tier applies §4.1's
    /// precision to both sides before comparing, so `-3.6712345678901235`
    /// becomes `-3.671235` on each side and a one-bit parse error is normalised
    /// away before anything looks at it. Its sidecar cannot close the hole
    /// either — the expected `lon` is a JSON number in a file THIS PARSER reads,
    /// so that check compares the parser against itself.
    ///
    /// The exposure is narrow and this seat is in it: Go, Python and JavaScript
    /// convert through standard libraries that are correctly rounded by
    /// specification, and the two seats that reach for a conversion of their own
    /// are this one and Kotlin. The Rust lane would have shipped a reader one
    /// bit out with a green suite — `serde_json` without `float_roundtrip` gives
    /// `c00d5eb03aa8431d` where the standard library gives `…431e`.
    ///
    /// BIT PATTERNS, not values: two doubles one bit apart print identically, so
    /// a value comparison that failed would read as a broken test rather than a
    /// broken parser. The hex constant is here as a third opinion — comparing
    /// only against `Double(String)` would agree with itself if the platform's
    /// conversion were the thing that broke.
    func testTheParserConvertsAFullPrecisionLiteralCorrectly() throws {
        let document = try read(minimal("\"waypoints\":[{\"at\":[-3.6712345678901235,57.11812345678901]}]")).document
        let at = try XCTUnwrap(document.waypoints?[0].at)
        XCTAssertEqual(at.lon.bitPattern, Double("-3.6712345678901235")!.bitPattern)
        XCTAssertEqual(at.lon.bitPattern, 0xc00d_5eb0_3aa8_431e)
        XCTAssertEqual(at.lat.bitPattern, Double("57.11812345678901")!.bitPattern)
        XCTAssertEqual(at.lat.bitPattern, 0x404c_8f1e_ab5f_e64e)

        // The shorter spelling is the SAME double, which is why the corpus
        // sidecar can carry either and why no comparison of values could ever
        // have caught a one-bit error here.
        XCTAssertEqual(Double("-3.6712345678901234")!.bitPattern, at.lon.bitPattern)
    }

    func testTheParserAcceptsWhatJSONAllows() throws {
        let result = try read("{\"vnx\":\"0.1.0\",\"generator\":\"\\u00e9\\ud83d\\ude00 \\\"q\\\" \\/ \\b\\f\\n\\r\\t\"}")
        XCTAssertEqual(result.document.generator, "é😀 \"q\" / \u{08}\u{0C}\n\r\t")
    }

    // MARK: - Containers (SPEC 7)

    func testContainerRoundTripsThroughThisLibrarysOwnWriter() throws {
        var document = Document()
        document.waypoints = [Waypoint(at: Position(lon: -3.675, lat: 57.1201))]
        let archive = try VNX.writeContainer(document: document,
                                             attachments: [Attachment(path: "attachments/photo.txt", bytes: Data("hello\n".utf8))])
        let read = try VNX.readContainer(archive)
        XCTAssertEqual(read.documentName, "doc.vnx")
        XCTAssertEqual(read.document, document)
        XCTAssertEqual(read.attachments.map(\.path), ["attachments/photo.txt"])
        XCTAssertEqual(read.attachments[0].bytes, Data("hello\n".utf8))
    }

    /// SPEC 7's closing note, which no corpus case exercises: "an
    /// implementation matching the root document on the base name alone matches
    /// at any depth and lets the last match win rather than the first. The
    /// match is on the full entry name."
    func testRootDocumentMatchesTheFullEntryNameNotTheBaseName() throws {
        let read = try VNX.readContainer(ZipFixtures.bytes(ZipFixtures.nestedDocDecoy))
        XCTAssertEqual(read.documentName, "a-export.vnx",
                       "`nested/doc.vnx` is not `doc.vnx` at the archive root")
        XCTAssertEqual(read.attachments.map(\.path), ["nested/doc.vnx"])
    }

    /// "the reader takes the FIRST entry with a `.vnx` extension in archive
    /// order" — first, not last.
    func testFallbackTakesTheFirstVNXInArchiveOrder() throws {
        let read = try VNX.readContainer(ZipFixtures.bytes(ZipFixtures.twoCandidates))
        XCTAssertEqual(read.documentName, "first.vnx")
    }

    func testDuplicateEntryNamesAreComparedCaseInsensitively() {
        XCTAssertThrowsError(try VNX.readContainer(ZipFixtures.bytes(ZipFixtures.caseDuplicate))) {
            XCTAssertEqual(($0 as? Refusal)?.cause, .containerDuplicateEntry)
        }
    }

    /// SPEC 7: "`..` is a path segment, not a substring: `a..b/photo.jpg` is a
    /// legal name and escapes nothing."
    func testDotsInsideANameAreNotAnEscape() throws {
        let read = try VNX.readContainer(ZipFixtures.bytes(ZipFixtures.dotsInName))
        XCTAssertEqual(read.attachments.map(\.path), ["a..b/photo.jpg"])
        XCTAssertNil(Container.unsafePath("a..b/photo.jpg"))
        XCTAssertNotNil(Container.unsafePath("a/../b"))
        XCTAssertNotNil(Container.unsafePath("../b"))
        XCTAssertNotNil(Container.unsafePath("/etc/passwd"))
        XCTAssertNotNil(Container.unsafePath("C:/windows"))
        XCTAssertNotNil(Container.unsafePath("a\\b"))
        XCTAssertNil(Container.unsafePath("..b/photo.jpg"))

        // Ruled 2026-09-09: a backslash is refused UNCONDITIONALLY and BEFORE
        // the `..` segment hunt, so the question the JavaScript seat found —
        // whether `\` counts as a separator while hunting for a `..` segment —
        // never has to be answered. A reader splitting on both refuses `..\x`
        // and one splitting on `/` only accepts it, and neither fails any tier.
        // On a Windows-authored archive that difference is a real escape.
        XCTAssertNotNil(Container.unsafePath("..\\x"))
        XCTAssertNotNil(Container.unsafePath("a\\..\\b"))
        XCTAssertNotNil(Container.unsafePath("\\etc\\passwd"))
    }

    /// SPEC 7: "A reader MUST read an archive that uses the ZIP64 extensions."
    /// Nothing in the corpus does.
    ///
    /// Two fixtures, because the first one alone was a test that passed for the
    /// wrong reason: `zipfile`'s `force_zip64` masks the sizes in the LOCAL
    /// header and leaves the central directory in 32-bit form, and this reader
    /// takes its sizes from the central directory, so disabling the extra-field
    /// parser entirely left that case green. The second is hand-built with the
    /// sizes AND the local header offset masked to 0xFFFFFFFF in the central
    /// directory, a ZIP64 extra field carrying the real values, and a ZIP64
    /// end-of-central-directory record and locator.
    func testZIP64ArchiveIsRead() throws {
        let viaLocalHeader = try VNX.readContainer(ZipFixtures.bytes(ZipFixtures.zip64Deflate))
        XCTAssertEqual(viaLocalHeader.documentName, "doc.vnx")
        XCTAssertEqual(viaLocalHeader.document.waypoints?.count, 1)
        XCTAssertEqual(viaLocalHeader.attachments.map(\.path), ["attachments/photo.txt"])

        let viaCentralDirectory = try VNX.readContainer(ZipFixtures.bytes(ZipFixtures.zip64CentralDirectory))
        XCTAssertEqual(viaCentralDirectory.documentName, "doc.vnx")
        XCTAssertEqual(viaCentralDirectory.document.waypoints?.count, 1)
        XCTAssertEqual(viaCentralDirectory.attachments, [])

        // Third fixture, and the reason for it belongs to the Rust seat: a lost
        // offset that happens to be zero is not a test. The first two put the
        // root document's local header at offset 0, so a reader that ignored
        // the ZIP64 offset field entirely — reading the masked 0xFFFFFFFF as
        // nothing and starting at the beginning — would still find it. This one
        // carries a 192-byte prefix and a filler entry ahead of the document, so
        // the offset has to be read from the extra field to land anywhere near
        // it.
        let padded = try VNX.readContainer(ZipFixtures.bytes(ZipFixtures.zip64PaddedCentralDirectory))
        XCTAssertEqual(padded.documentName, "doc.vnx")
        XCTAssertEqual(padded.document.waypoints?.count, 1)
        XCTAssertEqual(padded.attachments.map(\.path), ["filler.txt"])
    }

    /// This WRITER sorts its entries. That must not leak into the reader.
    ///
    /// A reader that refused an out-of-order archive, or that silently reordered
    /// what it returned, would be applying a writer's choice to something it was
    /// given — the same asymmetry as stored-versus-deflate and
    /// plain-versus-ZIP64, and the shape this format keeps everywhere: constrain
    /// what a library writes, never what it will read.
    ///
    /// ⚠ THIS ONE IS ORDER-SENSITIVE ON PURPOSE, and it is the only container
    /// test that is. The order it pins belongs to the FIXTURE — those bytes were
    /// built out of sequence deliberately — and what is asserted is a READER
    /// property: it hands back what the archive holds, in the order the archive
    /// holds it. Nothing here says a writer must emit that order; §7.1 says the
    /// opposite, that every field but the root document's position is the
    /// writer's own business.
    func testAReaderAcceptsAnArchiveWhoseEntriesAreNotSorted() throws {
        let result = try VNX.readContainer(ZipFixtures.bytes(ZipFixtures.unsortedOnTheWire))
        XCTAssertEqual(result.documentName, "doc.vnx")
        XCTAssertEqual(result.document.waypoints?.count, 1)
        XCTAssertEqual(result.diagnostics, [], "entry order is a writer's rule; a reader has nothing to say about it")
        XCTAssertEqual(result.attachments.map(\.path),
                       ["\u{00C6}ae.txt", "zz-last.txt", "aaa/earlier.txt"],
                       "and the attachments come back IN THE ARCHIVE'S ORDER, not re-sorted")
    }

    /// SPEC 7: a reader MUST accept general purpose bit 3, where the local
    /// header carries zeros for the CRC and both sizes, a data descriptor
    /// follows the payload, and only the central directory has the truth.
    ///
    /// A writer MUST NOT set it — for a STORED entry the payload can itself
    /// contain `PK\u{5C}u{03}\u{5C}u{04}`, so a data descriptor makes forward-only
    /// parsing genuinely ambiguous — and this reader never does. But it reads
    /// one, because it takes its sizes from the central directory, which is the
    /// authority in that case.
    func testAReaderAcceptsADataDescriptor() throws {
        let bytes = [UInt8](ZipFixtures.bytes(ZipFixtures.dataDescriptorBit3))
        // The local header really is empty of the three fields.
        func u32(_ i: Int) -> UInt32 {
            UInt32(bytes[i]) | UInt32(bytes[i+1]) << 8 | UInt32(bytes[i+2]) << 16 | UInt32(bytes[i+3]) << 24
        }
        XCTAssertEqual(UInt16(bytes[6]) | UInt16(bytes[7]) << 8, 0x0808, "bit 3 is set")
        XCTAssertEqual(u32(14), 0, "local CRC is zero")
        XCTAssertEqual(u32(18), 0, "local compressed size is zero")
        XCTAssertEqual(u32(22), 0, "local uncompressed size is zero")

        let result = try VNX.readContainer(ZipFixtures.bytes(ZipFixtures.dataDescriptorBit3))
        XCTAssertEqual(result.documentName, "doc.vnx")
        XCTAssertEqual(result.document.waypoints?.count, 1)
        XCTAssertEqual(result.diagnostics, [])
        XCTAssertEqual(result.attachments.map(\.path), ["attachments/note.txt"])
        XCTAssertEqual(result.attachments[0].bytes, Data("hello\n".utf8))
    }

    /// The corpus's four archives hold 138-byte entries, which deflate as one
    /// small block. This one is large and repetitive enough to exercise
    /// back-references and the dynamic Huffman path.
    func testDeflateHandlesALargerEntry() throws {
        let bytes = [UInt8](ZipFixtures.bytes(ZipFixtures.largeDeflate))
        let entry = try Zip.entries(in: bytes)[0]
        XCTAssertEqual(entry.method, Zip.methodDeflate)
        XCTAssertEqual(Data(try Zip.contents(of: entry, in: bytes)), Data(ZipFixtures.largeDeflateBody.utf8))
    }

    /// SPEC 7: the remedy depends on which entry it is. An attachment goes and
    /// the document is returned; the root document going means there is no
    /// document, so the archive is refused.
    func testUnsupportedCompressionIsAnAttachmentDiagnosticAndARootRefusal() throws {
        let read = try VNX.readContainer(ZipFixtures.bytes(ZipFixtures.bzip2Attachment))
        XCTAssertEqual(read.document.waypoints?.count, 1, "refusing a library over one photograph would be disproportionate")
        XCTAssertEqual(read.attachments, [])
        XCTAssertEqual(read.diagnostics.map(\.cause), [.attachmentUnreadable])
        // SPEC 6.3's separate `#/entries/` space: an archive entry is not part
        // of the document, so no pointer into the document can locate one.
        XCTAssertEqual(read.diagnostics.map(\.path), ["#/entries/notes.bz2.txt"])

        XCTAssertThrowsError(try VNX.readContainer(ZipFixtures.bytes(ZipFixtures.bzip2Root))) {
            XCTAssertEqual(($0 as? Refusal)?.cause, .containerRootCompressionUnsupported)
        }
    }

    func testArchiveWithNoDocumentIsRefused() {
        XCTAssertThrowsError(try VNX.readContainer(ZipFixtures.bytes(ZipFixtures.noDocument))) {
            XCTAssertEqual(($0 as? Refusal)?.cause, .containerNoDocument)
        }
    }

    func testNotAZipAtAll() {
        XCTAssertThrowsError(try VNX.readContainer(Data("{\"vnx\":\"0.1.0\"}".utf8))) {
            XCTAssertEqual(($0 as? Refusal)?.cause, .containerMalformed)
        }
    }

    // MARK: - Round trip

    /// Writing is a FIXED POINT over every accept file: read, write, read the
    /// output, write again, and the two outputs are the same bytes.
    ///
    /// The precision rule and the shortest spelling are both idempotent, so a
    /// second pass must change nothing. This is what the accept tier's tree
    /// comparison cannot see on its own — it compares against the file, and the
    /// file is only ever written once.
    func testWritingIsAFixedPoint() throws {
        for url in try Corpus.cases(in: "accept") where url.pathExtension == "vnx" {
            let once = try VNX.write(try VNX.read(try Data(contentsOf: url)).document)
            XCTAssertEqual(try VNX.write(try VNX.read(once).document), once, url.lastPathComponent)
        }
    }

    /// A model built by hand, written, read back, and compared. This is the
    /// half the corpus cannot test at all: every corpus case starts from bytes.
    func testWriteThenReadIsAFixedPointOverAModel() throws {
        var document = Document()
        document.generator = "VNX Swift tests"
        document.created = "2026-09-09T12:00:00Z"
        document.folders = [Folder(id: "f", parent: nil, name: "F", notes: nil, hidden: true, data: nil)]
        document.waypoints = [Waypoint(at: Position(lon: -3.675, lat: 57.1201, elevation: 320),
                                       name: "Car park", notes: "note", folder: "f", hidden: nil,
                                       style: ["color": .string("#444444"), "importance": .number(2)],
                                       data: [DataRecord(type: "com.example.x", body: ["value": .bool(true)])])]
        document.networks = [Network(name: "N", notes: nil, folder: "f", hidden: nil,
                                     nodes: ["a": Position(lon: 0, lat: 0), "b": Position(lon: 1, lat: 1)],
                                     arcs: ["x": NetworkArc(from: "a", to: "b",
                                                            pips: [Position(lon: 0, lat: 0), Position(lon: 1, lat: 1)],
                                                            data: nil)],
                                     chains: [Chain(links: [Link(arc: "x", from: .node("a"), to: .inline(Position(lon: 1, lat: 1)))])],
                                     points: nil, areas: nil, data: nil)]
        document.tracks = [Track(segments: [TrackSegment(pips: [
            Position(lon: 0, lat: 0, elevation: 1, timeMilliseconds: 1000),
            Position(lon: 1, lat: 1, elevation: 2, timeMilliseconds: 4000)])],
            name: "T", notes: nil, folder: nil, hidden: nil, style: nil,
            started: "2026-09-09T12:00:00Z", ended: "2026-09-09T12:00:04Z", data: nil)]
        document.rulesets = [.object(["name": .string("r")])]

        let bytes = try VNX.write(document)
        let read = try VNX.read(bytes)
        XCTAssertEqual(read.diagnostics, [])
        XCTAssertEqual(read.document, document)
        XCTAssertEqual(try VNX.write(read.document), bytes)
        XCTAssertEqual(read.document.tracks?[0].movingTimeMilliseconds, 3000)
    }
}
