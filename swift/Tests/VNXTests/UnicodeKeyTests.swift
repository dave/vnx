import Foundation
import XCTest
@testable import VNX

/// JSON member names are compared by code point. Swift's `String` equality is
/// canonical equivalence, so a `[String: _]` would merge `"\u{e9}"` (NFC) and
/// `"e\u{301}"` (NFD) into one member and lose the other with no diagnostic.
/// `JSONKey` stores them apart; these pin that everywhere an object is held.
final class UnicodeKeyTests: XCTestCase {
    private let nfc = "\u{e9}"
    private let nfd = "e\u{301}"

    func testThePremiseTheTwoSpellingsAreEqualSwiftStrings() {
        // If this ever fails, Swift stopped normalising and the rest of this
        // file guards a hazard that has gone.
        XCTAssertEqual(nfc, nfd)
        XCTAssertNotEqual(JSONKey(nfc), JSONKey(nfd))
    }

    func testBothSpellingsSurviveInARecordBodyAndAnApplicationKey() throws {
        let src = Data("""
        {"vnx":"0.2.0",
         "waypoints":[{"at":[1,2],"data":[{"@type":"com.example.x","\(nfc)":"precomposed","\(nfd)":"decomposed"}]}],
         "com.example.v":{"\(nfc)":1,"\(nfd)":2}}
        """.utf8)
        let first = try VNX.read(src)
        XCTAssertEqual(first.diagnostics, [])
        let back = try VNX.read(try VNX.write(first.document))
        let body = try XCTUnwrap(back.document.waypoints?[0].data?[0].body)
        XCTAssertEqual(body[JSONKey(nfc)], .string("precomposed"))
        XCTAssertEqual(body[JSONKey(nfd)], .string("decomposed"))
        let value = try XCTUnwrap(back.document.applicationKeys.first?.value.objectValue)
        XCTAssertEqual(value[JSONKey(nfc)], .number(1))
        XCTAssertEqual(value[JSONKey(nfd)], .number(2))
        XCTAssertEqual(value.count, 2)
    }

    func testBothSpellingsSurviveAsNetworkNodeKeys() throws {
        let src = Data("""
        {"vnx":"0.2.0","networks":[{"nodes":{"\(nfc)":[1,1],"\(nfd)":[1,2]}}]}
        """.utf8)
        let first = try VNX.read(src)
        let back = try VNX.read(try VNX.write(first.document))
        XCTAssertEqual(back.document.networks?[0].nodes?.count, 2)
        XCTAssertEqual(back.document.networks?[0].nodes?[JSONKey(nfd)]?.lat, 2)
    }

    /// Two application keys that differ only in normalisation are two keys, so
    /// the writer's held-twice refusal must not fire on them.
    func testTwoApplicationKeysDifferingInNormalisationAreNotADuplicate() throws {
        var document = Document()
        document.applicationKeys = [
            ApplicationKey(key: "com.example.\(nfc)", value: .number(1)),
            ApplicationKey(key: "com.example.\(nfd)", value: .number(2)),
        ]
        let back = try VNX.read(try VNX.write(document))
        XCTAssertEqual(back.document.applicationKeys.count, 2)
    }

    /// SPEC 3.9's test is U+002E among the name's code points. A full stop
    /// followed by a combining mark is one `Character` that is not ".", and a
    /// test on `Character`s would miss it.
    func testAFullStopBeforeACombiningMarkStillMakesAnApplicationKey() throws {
        let read = try VNX.read(Data("{\"vnx\":\"0.2.0\",\"a.\u{301}b\":1}".utf8))
        XCTAssertEqual(read.diagnostics, [])
        XCTAssertEqual(read.document.applicationKeys.map(\.key), ["a.\u{301}b"])
    }

    func testTheWriterEscapesWhatJSONRequiresAndNothingElse() throws {
        let s = "q\"b\\n\nt\tc\u{1}/é"
        let out = try JSONWriter.render(.object([JSONKey(s): .string(s)]))
        XCTAssertEqual(String(decoding: out, as: UTF8.self),
                       #"{"q\"b\\n\nt\tc\u0001/é":"q\"b\\n\nt\tc\u0001/é"}"#)
        XCTAssertEqual(try JSONParser.parse(out), .object([JSONKey(s): .string(s)]))
    }

    func testTheWriterRefusesANonFiniteNumber() {
        XCTAssertThrowsError(try JSONWriter.render(.array([.number(.nan)])))
        XCTAssertThrowsError(try JSONWriter.render(.number(.infinity)))
    }
}
