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
        "{\"vnx\":\"0.2.0\"\(body.isEmpty ? "" : ",")\(body)}"
    }

    // MARK: - Versions (SPEC 2.2)

    func testVersionComponentsAreCompletelyNumeric() throws {
        // "Each component is one or more decimal digits, with no bound on
        // length and no rule against a leading zero: `00.01.0000` is a legal
        // spelling of 0.1.0." The same holds of 0.2.0, the series this reader
        // implements.
        let result = try read("{\"vnx\":\"00.02.0000\"}")
        XCTAssertEqual(result.document.vnx, "00.02.0000",
                       "a reader that re-encodes writes the version the document declared")
        XCTAssertEqual(try VNX.write(result.document), Data("{\"vnx\":\"00.02.0000\"}".utf8))
    }

    func testVersionsAcceptedAndRefused() throws {
        // Every accepted spelling is in the 0.2 series. A long or
        // leading-zeroed component is a SPELLING of a series, not another one.
        for accepted in ["0.2.0", "0.2.3", "0.2.99", "00.2.0", "0.02.000000000000000000005"] {
            XCTAssertNoThrow(try read("{\"vnx\":\"\(accepted)\"}"), accepted)
        }
        // Another series, in either direction: SPEC 2.2 refuses a lower minor
        // for the same reason as a higher one, so `0.0.9` belongs in this list
        // and not the one above. A 0.2 reader does not read 0.1.
        for otherSeries in ["0.1.0", "0.1.99", "0.0.9", "0.0.0", "0.3.0", "1.0.0", "9.9.9", "0.20.0"] {
            XCTAssertThrowsError(try read("{\"vnx\":\"\(otherSeries)\"}"), otherSeries) {
                XCTAssertEqual(($0 as? Refusal)?.cause, .versionUnsupportedSeries, otherSeries)
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
                XCTAssertEqual(refusal.cause, .versionUnsupportedSeries, other)
                XCTAssertTrue(refusal.message.contains("MUST NOT read another one under its own rules"),
                              "the refusal names the series rule, not the minor rule: " + refusal.message)
            }
        }
        // A minor either side of the reader's is refused for the MINOR rule,
        // which names the series rather than the major regime.
        for otherMinor in ["0.3.0", "0.1.0"] {
            XCTAssertThrowsError(try read("{\"vnx\":\"" + otherMinor + "\"}"), otherMinor) {
                let refusal = $0 as! Refusal
                XCTAssertTrue(refusal.message.contains("each minor is its own series"),
                              "the refusal names the minor rule, not the series rule: " + refusal.message)
                XCTAssertFalse(refusal.message.contains("MUST NOT read another one under its own rules"))
            }
        }
        XCTAssertNoThrow(try read("{\"vnx\":\"0.2.99\"}"))
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
        "networks":[{"arcs":{"a":{"from":"x","to":"y","pips":[[0,0],[1,1]]}},"chains":[{"walk":[{"arc":"ghost","from":"x","to":"y"}]}]}]
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
    /// Ignored, and not carried through a read-modify-write. No rule asks a
    /// writer to preserve a channel it does not understand.
    func testChannelsAboveIndexThreeAreIgnoredWithoutADiagnostic() throws {
        let result = try read(minimal("\"waypoints\":[{\"at\":[1,2,3,4,5,null]}]"))
        XCTAssertEqual(result.diagnostics, [], "an unknown channel is not a diagnostic")
        XCTAssertEqual(result.document.waypoints?[0].at,
                       Position(lon: 1, lat: 2, elevation: 3, timeMilliseconds: 4))
        XCTAssertEqual(String(decoding: try VNX.write(result.document), as: UTF8.self)
            .contains("[1,2,3,4]"), true)
    }

    /// SPEC 4.1: a writer emits the shortest spelling of a position, and states
    /// a coordinate to whatever precision it holds — the format constrains
    /// neither the reader's precision nor the writer's. "A reader MUST NOT
    /// round. Decoding preserves the binary64 value the document states", and
    /// nothing on the write path narrows it either.
    func testAWriterShortensAndNeitherSideRounds() throws {
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
            .contains("[-3.6712345678901235,57.11812345678901,340.06]"),
            "a writer drops the trailing null and changes no value")
    }

    /// SPEC 4.2: how a number is SPELLED is the writer's own business, and this
    /// library spells it as Swift's shortest round-tripping decimal, which uses
    /// an exponent below 1e-4. `1e-06` and `0.000001` are one value.
    ///
    /// This test used to assert ties to even at `340.25`, because SPEC 4.1
    /// required a writer to round elevation to one decimal place. It does not
    /// any more, so the values below are carried through untouched and the only
    /// claim left is about spelling.
    func testNumberSpellingIsTheWritersOwnBusiness() throws {
        func written(_ p: Position) throws -> String {
            String(decoding: try VNX.write({ var d = Document(); d.waypoints = [Waypoint(at: p)]; return d }()),
                   as: UTF8.self)
        }
        XCTAssertTrue(try written(Position(lon: 1, lat: 2, elevation: 340.25)).contains("[1,2,340.25]"))
        XCTAssertTrue(try written(Position(lon: 1, lat: 2, elevation: -340.25)).contains("[1,2,-340.25]"))
        XCTAssertTrue(try written(Position(lon: 0.00000125, lat: 0.00000175)).contains("[1.25e-06,1.75e-06]"))
    }

    /// ⚠ THE WRITE PATH NARROWS NO VALUE, and this is the guard that says so.
    /// The library rounded longitude and latitude to 6 places and elevation to
    /// 1 for as long as SPEC 4.1 required it; with that requirement gone, a
    /// writer that still rounded would silently discard nine digits a caller
    /// handed it. Asserted on the TEXT, because the value comparison a
    /// round-trip makes would also pass for a writer that never wrote the
    /// digits at all and a reader that read what it wrote.
    func testTheWritePathPreservesEveryDigitItIsHanded() throws {
        let source = Data("""
        {"vnx":"0.2.0","waypoints":[{"at":[-3.6712345678901235,57.11812345678901,340.06,1778914991000]}]}
        """.utf8)
        let written = String(decoding: try VNX.write(try VNX.read(source).document), as: UTF8.self)
        XCTAssertTrue(written.contains("[-3.6712345678901235,57.11812345678901,340.06,1778914991000]"),
                      "got \(written)")
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
                       Data("{\"vnx\":\"0.2.0\",\"waypoints\":[]}".utf8))
        XCTAssertEqual(try VNX.write(try read("{\"vnx\":\"0.2.0\"}").document),
                       Data("{\"vnx\":\"0.2.0\"}".utf8))
    }

    // MARK: - Data records (SPEC 5)

    /// SPEC 5: `@type` is compared exactly, including case.
    func testTypeIdentityIsCaseSensitive() {
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
        XCTAssertEqual(result.diagnostics.filter { $0.cause == .wrongTypeRequired }.map(\.path),
                       ["/waypoints/0/data/0", "/waypoints/0/data/1"])
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
        XCTAssertEqual(Set(result.diagnostics.map(\.cause)), [.wrongTypeRequired, .folderNotPresent])
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
        let result = try read("{\"vnx\":\"0.2.0\",\"waypoints\":[{\"at\":[1,2]}],\"waypoints\":[{\"at\":[3,4]},{\"at\":[5,6]}]}")
        XCTAssertEqual(result.document.waypoints?.count, 2)
        XCTAssertEqual(result.diagnostics, [], "a documented limitation, not a rule")
    }

    // MARK: - The parser

    func testTheParserIsStrict() {
        for bad in ["{\"vnx\":\"0.2.0\",}", "{'vnx':'0.1.0'}", "{\"vnx\":\"0.2.0\"} trailing",
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
        let result = try read("{\"vnx\":\"0.2.0\",\"generator\":\"\\u00e9\\ud83d\\ude00 \\\"q\\\" \\/ \\b\\f\\n\\r\\t\"}")
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
        XCTAssertThrowsError(try VNX.readContainer(Data("{\"vnx\":\"0.2.0\"}".utf8))) {
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
                                     nodes: ["a": Vertex(lon: 0, lat: 0, elevation: 10), "b": Vertex(lon: 1, lat: 1)],
                                     arcs: ["x": Arc(from: "a", to: "b", pips: [Vertex(lon: 0.5, lat: 0.5, elevation: 12)])],
                                     chains: [Chain(walk: [Pass(arc: "x", from: .node("a"), to: .position(LonLat(lon: 0.7, lat: 0.7)))])],
                                     points: nil, areas: nil, data: nil)]
        document.routes = [Route(nodes: ["a": Vertex(lon: 2, lat: 2), "b": Vertex(lon: 3, lat: 3)],
                                 arcs: ["y": Arc(from: "a", to: "b")],
                                 walk: [Pass(arc: "y", from: .node("b"), to: .node("a"))],
                                 name: "R")]
        document.areas = [Area(polygon: [Position(lon: 0, lat: 0), Position(lon: 1, lat: 0), Position(lon: 1, lat: 1)],
                               name: "A")]
        document.tracks = [Track(segments: [TrackSegment(pips: [
            Position(lon: 0, lat: 0, elevation: 1, timeMilliseconds: 1000),
            Position(lon: 1, lat: 1, elevation: 2, timeMilliseconds: 4000)])],
            name: "T", notes: nil, folder: nil, hidden: nil, style: nil,
            started: "2026-09-09T12:00:00Z", ended: "2026-09-09T12:00:04Z", data: nil)]
        document.applicationKeys = [
            ApplicationKey(key: "app.viatrail.rulesets", value: .array([.object(["name": .string("r")])])),
            ApplicationKey(key: "com.example.empty", value: .array([])),
            ApplicationKey(key: "com.example.null", value: .null),
        ]

        let bytes = try VNX.write(document)
        let read = try VNX.read(bytes)
        XCTAssertEqual(read.diagnostics, [])
        XCTAssertEqual(read.document, document)
        XCTAssertEqual(try VNX.write(read.document), bytes)
        XCTAssertEqual(read.document.tracks?[0].movingTimeMilliseconds, 3000)
    }
}
