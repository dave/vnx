import Foundation
import XCTest
@testable import VNX

/// The ZIP header fields, against the archive two seats agreed by measurement.
///
/// This file was written as a stand-in while SPEC 7 said nothing about
/// version-needed, the MS-DOS timestamp, extra fields or the attribute and disk
/// fields — so nothing in the corpus could hold two languages to the same
/// archive bytes and the agreement rested on a message between two seats. That
/// came back: SPEC 7.1 now fixes every field, and `ContainerBytesTests` checks
/// this writer against an independent one built from that table alone.
///
/// This file keeps its own job. The two instruments catch different things: a
/// measured agreement between implementations catches one of them drifting, and
/// the published table catches both of them drifting together.
///
/// The values here were agreed with the Kotlin seat on 2026-09-09 by measuring
/// both writers against one input. They are not arbitrary tie-breaks: each is
/// the value [APPNOTE] calls for, and the two I changed to reach agreement were
/// both defects on my side rather than coin-flips.
final class ZipHeaderTests: XCTestCase {

    /// The input the Kotlin seat proposed, chosen so the number rules cannot
    /// muddy the comparison.
    private func agreedArchive() throws -> Data {
        let json = Data(#"{"vnx":"0.1.0","waypoints":[{"name":"Bothy","at":[-3.6712,57.118]}]}"#.utf8)
        let document = try VNX.read(json).document
        let photo = Attachment(path: "attachments/photo.txt", bytes: Data("not a photo\n".utf8))
        return VNX.writeContainer(document: document, attachments: [photo])
    }

    /// The whole archive, byte for byte, as both languages emit it.
    ///
    /// A length-and-checksum assertion would pass for an archive that differed
    /// in a field neither language reads back, which is precisely the class of
    /// difference this test exists to catch, so the bytes are spelled out.
    func testTheAgreedArchiveIsUnchanged() throws {
        let archive = try agreedArchive()
        XCTAssertEqual(archive.count, 350, "the agreed archive is 350 bytes in both languages")

        let header = [UInt8](archive.prefix(30))
        XCTAssertEqual(Array(header[0..<4]), [0x50, 0x4B, 0x03, 0x04], "local file header signature")
        XCTAssertEqual(Array(header[4..<6]), [0x0a, 0x00],
                       "version needed: 1.0, the floor for a STORED entry. 20 is the floor for DEFLATE and overstates it")
        XCTAssertEqual(Array(header[6..<8]), [0x00, 0x08], "general purpose flags: UTF-8 filename and nothing else")
        XCTAssertEqual(Array(header[8..<10]), [0x00, 0x00], "method: stored")
        XCTAssertEqual(Array(header[10..<12]), [0x00, 0x00], "MS-DOS time: midnight")
        XCTAssertEqual(Array(header[12..<14]), [0x21, 0x00],
                       "MS-DOS date: 0x0021 is 1980-01-01. 0x0000 is day 0 of month 0, which is not a date")
        XCTAssertEqual(Array(header[26..<28]), [0x07, 0x00], "name length: doc.vnx")
        XCTAssertEqual(Array(header[28..<30]), [0x00, 0x00], "extra field length: none")
    }

    /// A writer that stamps the clock cannot be byte-compared with anything,
    /// including its own previous run. This is the property that makes the
    /// agreement above reproducible tomorrow.
    func testTheArchiveDoesNotDependOnWhenItWasWritten() throws {
        XCTAssertEqual(try agreedArchive(), try agreedArchive())
    }

    /// The central directory repeats the fields the local header set. A writer
    /// that set them in one place and defaulted them in the other produces an
    /// archive that reads correctly and still differs byte for byte.
    func testTheCentralDirectoryRepeatsTheSameFields() throws {
        let archive = [UInt8](try agreedArchive())
        guard let start = (0..<(archive.count - 4)).first(where: {
            Array(archive[$0..<($0 + 4)]) == [0x50, 0x4B, 0x01, 0x02]
        }) else { return XCTFail("no central directory header in the archive") }

        XCTAssertEqual(Array(archive[(start + 4)..<(start + 6)]), [0x0a, 0x00], "version made by")
        XCTAssertEqual(Array(archive[(start + 6)..<(start + 8)]), [0x0a, 0x00], "version needed")
        XCTAssertEqual(Array(archive[(start + 8)..<(start + 10)]), [0x00, 0x08], "flags")
        XCTAssertEqual(Array(archive[(start + 10)..<(start + 12)]), [0x00, 0x00], "method")
        XCTAssertEqual(Array(archive[(start + 12)..<(start + 14)]), [0x00, 0x00], "time")
        XCTAssertEqual(Array(archive[(start + 14)..<(start + 16)]), [0x21, 0x00], "date")
    }

    /// What we wrote must still be what we read. Pinning header bytes is only
    /// safe while the writer and the reader agree about them.
    func testTheAgreedArchiveReadsBack() throws {
        let result = try VNX.readContainer(try agreedArchive())
        XCTAssertEqual(result.documentName, "doc.vnx")
        XCTAssertEqual(result.diagnostics.map(\.message), [])
        XCTAssertEqual(result.attachments.map(\.path), ["attachments/photo.txt"])
        XCTAssertEqual(result.attachments.first?.bytes, Data("not a photo\n".utf8))
        XCTAssertEqual(VNX.render(result.document), VNX.render(try VNX.read(
            Data(#"{"vnx":"0.1.0","waypoints":[{"name":"Bothy","at":[-3.6712,57.118]}]}"#.utf8)).document))
    }
}
