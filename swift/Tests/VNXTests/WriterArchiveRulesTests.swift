import Foundation
import XCTest
@testable import VNX

/// SPEC 7.1's five rules, against the archive this library writes.
///
/// ⚠ THE BYTES ARE NOT THE SUBJECT ANY MORE. This file used to compare a whole
/// archive against one built by an independent Python writer, because 7.1 once
/// said "a container written by a conforming writer is byte-identical to one
/// written by any other". It no longer says that — "every other field of the
/// archive is the writer's own business. Two conforming writers are not expected
/// to produce the same bytes for the same document." Keeping the byte
/// comparison would hold this writer to a rule the format dropped, and would go
/// red the first time zlib changed a compression level.
///
/// So what is asserted here is the five rules themselves, each named, plus the
/// two properties this writer chooses for itself: a fixed entry order and a
/// fixed timestamp, which together make one document produce one archive.
final class WriterArchiveRulesTests: XCTestCase {

    private func document() -> Document {
        var document = Document()
        document.profile = "full"
        document.waypoints = [Waypoint(at: Position(lon: -3.675, lat: 57.1201),
                                       name: "In a container", notes: nil, folder: nil,
                                       hidden: nil, style: nil, data: nil)]
        return document
    }

    private func archive() throws -> Data {
        try VNX.writeContainer(document: document(), attachments: [
            Attachment(path: "attachments/photo.txt", bytes: Data("not really a photo\n".utf8)),
            Attachment(path: "attachments/vue-générale.txt", bytes: Data("accented name\n".utf8)),
        ])
    }

    /// Rule 1: the root document is `doc.vnx`, at the archive root, first.
    func testTheRootDocumentIsFirstAndNamedDocVnx() throws {
        let entries = try Zip.entries(in: [UInt8](try archive()))
        XCTAssertEqual(entries.first?.name, "doc.vnx")
    }

    /// Rules 2 and 5, read out of the raw bytes rather than through a ZIP
    /// library.
    ///
    /// ⚠ A LIBRARY CAN NEVER BE THE INSTRUMENT FOR A LOCAL-HEADER RULE. Every
    /// ZIP library in every language resolves an entry from the CENTRAL
    /// DIRECTORY, on purpose, because that is where the authority lives — so it
    /// reports the two records agree whatever the local header says. Another
    /// seat measured Go's writer as setting bit 3 and omitting the UTF-8 flag,
    /// and Go's own gap test could not see any of it.
    func testEveryEntryCarriesTheUTF8FlagAndIsDeflated() throws {
        let bytes = [UInt8](try archive())
        func u16(_ i: Int) -> UInt16 { UInt16(bytes[i]) | UInt16(bytes[i + 1]) << 8 }
        func u32(_ i: Int) -> UInt32 {
            UInt32(bytes[i]) | UInt32(bytes[i+1]) << 8 | UInt32(bytes[i+2]) << 16 | UInt32(bytes[i+3]) << 24
        }

        var central = Int(u32(bytes.count - 6))
        let count = Int(u16(bytes.count - 12))
        XCTAssertEqual(count, 3)

        for index in 0..<count {
            XCTAssertEqual(u32(central), 0x0201_4B50, "central header \(index)")
            let nameLength = Int(u16(central + 28))
            let extraLength = Int(u16(central + 30))
            let commentLength = Int(u16(central + 32))
            let local = Int(u32(central + 42))
            let name = String(decoding: bytes[(central + 46)..<(central + 46 + nameLength)], as: UTF8.self)

            XCTAssertEqual(u32(local), 0x0403_4B50, "\(name): local header signature")
            let localNameLength = Int(u16(local + 26))
            let localName = String(decoding: bytes[(local + 30)..<(local + 30 + localNameLength)], as: UTF8.self)
            XCTAssertEqual(localName, name, "entry name")

            // Rule 2: the UTF-8 filename flag, on every entry.
            XCTAssertEqual(u16(local + 6) & 0x0800, 0x0800, "\(name): the UTF-8 name flag must be set")
            XCTAssertEqual(u16(central + 8) & 0x0800, 0x0800, "\(name): and in the central directory")
            // Rule 5: stored or deflate, and this writer chooses deflate.
            XCTAssertEqual(u16(local + 8), 8, "\(name): compression method: deflate")
            XCTAssertEqual(u16(central + 10), 8, "\(name): and in the central directory")
            XCTAssertEqual(u16(local + 4), 20, "\(name): version needed: 2.0, the floor for deflate")

            // The fields that appear in both records must carry the same value.
            // A writer that set them in one place and defaulted them in the
            // other produces an archive that reads correctly and still confuses
            // whatever reads it forward-only.
            for (label, localOffset, centralOffset, width) in [
                ("version needed", local + 4, central + 6, 2),
                ("general purpose flags", local + 6, central + 8, 2),
                ("compression method", local + 8, central + 10, 2),
                ("modification time", local + 10, central + 12, 2),
                ("modification date", local + 12, central + 14, 2),
                ("crc", local + 14, central + 16, 4),
                ("compressed size", local + 18, central + 20, 4),
                ("uncompressed size", local + 22, central + 24, 4),
                ("name length", local + 26, central + 28, 2),
                ("extra field length", local + 28, central + 30, 2),
            ] {
                let l = width == 2 ? UInt32(u16(localOffset)) : u32(localOffset)
                let c = width == 2 ? UInt32(u16(centralOffset)) : u32(centralOffset)
                XCTAssertEqual(l, c, "\(name): \(label) differs between the local header and the central directory")
            }

            let flags = u16(local + 6)
            XCTAssertEqual(flags & 0x0008, 0,
                           "\(name): bit 3 defers the sizes to a data descriptor, and this writer knows them")
            XCTAssertEqual(flags, 0x0800, "\(name): the UTF-8 flag, and nothing else")
            XCTAssertNotEqual(u32(local + 14), 0, "\(name): the CRC must be written in the local header")

            central += 46 + nameLength + extraLength + commentLength
        }
        XCTAssertEqual(u32(central), 0x0605_4B50, "the central directory ends where the EOCD begins")
    }

    /// Rule 3: entry names are unique. Rule 4: no attachment path escapes.
    /// Both are checked by this library's own reader, which refuses either.
    ///
    /// The attachments are compared as a COLLECTION. This test is about rules 3
    /// and 4, and a list comparison here would smuggle in an order rule §7.1
    /// does not state — the defect is easy to leave behind, because it survives
    /// fixing the comparison one layer up in the corpus runner.
    func testTheArchiveSatisfiesTheReadersOwnRules() throws {
        let result = try VNX.readContainer(try archive())
        XCTAssertEqual(result.documentName, "doc.vnx")
        XCTAssertEqual(result.diagnostics, [])
        XCTAssertEqual(result.attachments.map(\.path).sorted(),
                       ["attachments/photo.txt", "attachments/vue-générale.txt"])
        XCTAssertEqual(result.attachments.first { $0.path.hasSuffix("générale.txt") }?.bytes,
                       Data("accented name\n".utf8))
        XCTAssertEqual(result.document, document())
    }

    /// Deflate is why the container exists, so the archive must be smaller than
    /// what it holds. A writer switched back to stored would pass every rule
    /// above and fail this.
    func testTheArchiveIsSmallerThanItsContents() throws {
        var document = Document()
        // A repetitive document, because 200 bytes of JSON plus a ZIP envelope
        // is dominated by the envelope whatever the compressor does.
        document.waypoints = (0..<200).map {
            Waypoint(at: Position(lon: -3.675, lat: 57.1201 + Double($0) / 10000),
                     name: "Bothy", notes: "the same note over and over", folder: nil,
                     hidden: nil, style: nil, data: nil)
        }
        let plain = try VNX.write(document)
        let archive = try VNX.writeContainer(document: document)
        XCTAssertLessThan(archive.count, plain.count,
                          "a deflated container holds a document in fewer bytes than the document takes")
    }

    /// This writer's own choice, NOT A RULE: 7.1 constrains the root document's
    /// position and says every other field of the archive is the writer's own
    /// business, and it was ruled on 2026-09-10 that attachment order is not
    /// compared anywhere. Sorting is kept for run-to-run determinism, so that
    /// caller order — one exporter iterating a dictionary, another a list — is
    /// not a property of the archive. `corpus/write/container-cross-language`
    /// passes with this writer's attachments emitted in any order; measured.
    ///
    /// Four attachments passed deliberately out of order, because with one
    /// attachment every ordering rule agrees. The question to ask of an ordering
    /// check is not whether it passed but WHICH ORDERS THE INPUT COULD TELL
    /// APART.
    private func unsortedArchive() throws -> Data {
        try VNX.writeContainer(document: document(), attachments: [
            Attachment(path: "zz-last.txt", bytes: Data("z\n".utf8)),
            Attachment(path: "\u{212B}angstrom.txt", bytes: Data("angstrom\n".utf8)),
            Attachment(path: "aaa/earlier.txt", bytes: Data("a\n".utf8)),
            Attachment(path: "\u{00C6}ae.txt", bytes: Data("ae\n".utf8)),
        ])
    }

    func testAttachmentsAreSortedByEntryName() throws {
        let names = try Zip.entries(in: [UInt8](try unsortedArchive())).map(\.name)
        XCTAssertEqual(names, ["doc.vnx", "aaa/earlier.txt", "zz-last.txt",
                               "\u{00C6}ae.txt", "\u{212B}angstrom.txt"],
                       "the root document first, then attachments sorted by code point")
    }

    /// That the sort uses code point and not Swift's `<`.
    ///
    /// Measured: Swift places `U+212B` BELOW `U+00C6`, because ANGSTROM SIGN
    /// canonically decomposes to `U+00C5`; code point places it far above. The
    /// two orders differ on exactly this input, which is what makes the
    /// comparator choice load-bearing rather than a matter of taste.
    func testTheSortIsByCodePointNotSwiftsCollation() throws {
        let names = ["zz-last.txt", "\u{212B}angstrom.txt", "aaa/earlier.txt", "\u{00C6}ae.txt"]
        let byCodePoint = names.sorted(by: { codePointLess($0, $1) })
        XCTAssertNotEqual(names.sorted(), byCodePoint, "if these agreed, this test would be proving nothing")
        XCTAssertTrue("\u{212B}" < "\u{00C6}", "Swift normalises")
        XCTAssertFalse(codePointLess("\u{212B}", "\u{00C6}"), "code point does not")

        let emitted = try Zip.entries(in: [UInt8](try unsortedArchive())).map(\.name)
        XCTAssertEqual(Array(emitted.dropFirst()), byCodePoint)
        XCTAssertNotEqual(Array(emitted.dropFirst()), names.sorted(),
                          "Swift's own order would have produced a different archive")
    }

    /// A writer that stamped the clock could not be compared with anything,
    /// including its own previous run.
    func testTheArchiveDoesNotDependOnWhenItWasWritten() throws {
        XCTAssertEqual(try archive(), try archive())
    }
}
