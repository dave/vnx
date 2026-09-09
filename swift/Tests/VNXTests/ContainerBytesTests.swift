import Foundation
import XCTest
@testable import VNX

/// SPEC 7.1: "A container written by a conforming writer is byte-identical to
/// one written by any other conforming writer for the same document and
/// attachments."
///
/// The expected bytes below were produced by an INDEPENDENT writer written from
/// SPEC 7.1's table alone, in Python, sharing no code with this library — the
/// check the section says its rule delivers, on the input it names: a document
/// plus two attachments, one with a non-ASCII entry name. It agreed on all 537
/// bytes at the first attempt.
///
/// `ZipHeaderTests` pins the same property against the archive two seats agreed
/// by measurement; this file pins it against the published table. They are
/// different instruments: one catches a drift between implementations, the other
/// catches both implementations drifting together away from the spec.
final class ContainerBytesTests: XCTestCase {

    private static let expected = "UEsDBAoAAAgAAAAAIQB6uwngigAAAIoAAAAHAAAAZG9jLnZueHsKICAicHJvZmlsZSI6ICJmdWxsIiwKICAidm54IjogIjAuMS4wIiwKICAid2F5cG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9ClBLAwQKAAAIAAAAACEAQiql/RMAAAATAAAAFQAAAGF0dGFjaG1lbnRzL3Bob3RvLnR4dG5vdCByZWFsbHkgYSBwaG90bwpQSwMECgAACAAAAAAhAEo36JIOAAAADgAAAB4AAABhdHRhY2htZW50cy92dWUtZ8OpbsOpcmFsZS50eHRhY2NlbnRlZCBuYW1lClBLAQIKAAoAAAgAAAAAIQB6uwngigAAAIoAAAAHAAAAAAAAAAAAAAAAAAAAAABkb2Mudm54UEsBAgoACgAACAAAAAAhAEIqpf0TAAAAEwAAABUAAAAAAAAAAAAAAAAArwAAAGF0dGFjaG1lbnRzL3Bob3RvLnR4dFBLAQIKAAoAAAgAAAAAIQBKN+iSDgAAAA4AAAAeAAAAAAAAAAAAAAAAAPUAAABhdHRhY2htZW50cy92dWUtZ8OpbsOpcmFsZS50eHRQSwUGAAAAAAMAAwDEAAAAPwEAAAAA"

    private func archive() -> Data {
        var document = Document()
        document.profile = "full"
        document.waypoints = [Waypoint(at: Position(lon: -3.675, lat: 57.1201),
                                       name: "In a container", notes: nil, folder: nil,
                                       hidden: nil, style: nil, data: nil)]
        return VNX.writeContainer(document: document, attachments: [
            Attachment(path: "attachments/photo.txt", bytes: Data("not really a photo\n".utf8)),
            Attachment(path: "attachments/vue-générale.txt", bytes: Data("accented name\n".utf8)),
        ])
    }

    func testTheArchiveMatchesAWriterBuiltFromTheSpecTable() {
        XCTAssertEqual(archive(), Data(base64Encoded: Self.expected)!)
    }

    /// Every field of SPEC 7.1's table, read back by name, so a disagreement
    /// says which field rather than which byte. A byte comparison alone tells
    /// the next person that something moved and not what.
    func testEveryFieldOfTheSpecTable() throws {
        let bytes = [UInt8](archive())
        func u16(_ i: Int) -> UInt16 { UInt16(bytes[i]) | UInt16(bytes[i + 1]) << 8 }
        func u32(_ i: Int) -> UInt32 {
            UInt32(bytes[i]) | UInt32(bytes[i+1]) << 8 | UInt32(bytes[i+2]) << 16 | UInt32(bytes[i+3]) << 24
        }

        // Local header of the first entry.
        XCTAssertEqual(u32(0), 0x0403_4B50)
        XCTAssertEqual(u16(4), 10, "version needed: 1.0, which is what stored requires")
        XCTAssertEqual(u16(6), 0x0800, "the UTF-8 name flag, and nothing else")
        XCTAssertEqual(u16(8), 0, "compression method: stored, never deflate")
        XCTAssertEqual(u16(10), 0x0000, "modification time")
        XCTAssertEqual(u16(12), 0x0021, "modification date: 1980-01-01, not the all-zero non-date")
        XCTAssertEqual(u16(28), 0, "extra field length")

        // Central directory: same fields again, plus the ones only it carries.
        let entries = try Zip.entries(in: bytes)
        XCTAssertEqual(entries.map(\.name),
                       ["doc.vnx", "attachments/photo.txt", "attachments/vue-générale.txt"],
                       "entry order: the root document first, then attachments as the caller gave them")
        let start = Int(u32(bytes.count - 6))
        XCTAssertEqual(u32(start), 0x0201_4B50)
        XCTAssertEqual(u16(start + 4), 10, "version made by, low byte")
        XCTAssertEqual(bytes[start + 5], 0, "create system: 0, because it encodes the writing machine")
        XCTAssertEqual(u16(start + 6), 10, "version needed")
        XCTAssertEqual(u16(start + 8), 0x0800)
        XCTAssertEqual(u16(start + 10), 0, "compression method")
        XCTAssertEqual(u16(start + 12), 0x0000)
        XCTAssertEqual(u16(start + 14), 0x0021)
        XCTAssertEqual(u16(start + 30), 0, "extra field length")
        XCTAssertEqual(u16(start + 32), 0, "comment length")
        XCTAssertEqual(u16(start + 34), 0, "disk number start")
        XCTAssertEqual(u16(start + 36), 0, "internal attributes")
        XCTAssertEqual(u32(start + 38), 0, "external attributes: 0, because they carry a file mode")

        // End of central directory.
        XCTAssertEqual(u32(bytes.count - 22), 0x0605_4B50, "no archive comment, so the record is last")
        XCTAssertEqual(u16(bytes.count - 2), 0, "archive comment length")
        XCTAssertNil(bytes.firstRange(of: [0x50, 0x4B, 0x06, 0x06]), "no ZIP64 end-of-central-directory record")
        XCTAssertNil(bytes.firstRange(of: [0x50, 0x4B, 0x07, 0x06]), "no ZIP64 locator")
    }

    /// That the local header and the central directory AGREE, field by field,
    /// both parsed from raw bytes.
    ///
    /// This is the assertion nobody had, and the reason is the instrument: every
    /// ZIP library in every language resolves an entry from the CENTRAL
    /// DIRECTORY, on purpose, because that is where the authority lives. So a
    /// library can never be the instrument for a local-header rule — it will
    /// report the two agree whatever the local header says. Another seat
    /// measured Go's writer as setting bit 3 and omitting the UTF-8 flag, four
    /// non-conforming fields rather than the two that were known, and Go's own
    /// gap test could not see any of it because `archive/zip` builds its header
    /// struct from the central directory.
    ///
    /// Nothing below goes through `Zip.entries`.
    func testLocalAndCentralHeadersAgreeFieldByField() {
        let bytes = [UInt8](archive())
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

            // The fields that appear in both records must carry the same value.
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

            // And the rules that are about the local header specifically.
            let flags = u16(local + 6)
            XCTAssertEqual(flags & 0x0008, 0,
                           "\(name): bit 3 must NOT be set — for a STORED entry the payload can contain `PK\u{5C}u{03}\u{5C}u{04}`, so a data descriptor makes forward-only parsing ambiguous")
            XCTAssertEqual(flags & 0x0800, 0x0800, "\(name): the UTF-8 name flag must be set")
            XCTAssertEqual(flags, 0x0800, "\(name): the UTF-8 flag, and nothing else")
            XCTAssertNotEqual(u32(local + 14), 0, "\(name): the CRC must be written in the local header, not deferred")
            XCTAssertGreaterThan(u32(local + 22), 0, "\(name): the uncompressed size must be written in the local header")

            central += 46 + nameLength + extraLength + commentLength
        }
        XCTAssertEqual(u32(central), 0x0605_4B50, "the central directory ends where the EOCD begins")
    }

    /// SPEC 7.1: attachments are SORTED BY ENTRY NAME, compared by Unicode code
    /// point as in SPEC 4.5.1 — not emitted in the order the caller supplied.
    ///
    /// The archive above cannot witness this rule, and that is worth saying
    /// rather than leaving to be discovered: its two attachment names happen to
    /// be in sorted order already, so caller order and sorted order are the same
    /// bytes. It certifies nine fields of 7.1's table and is silent on the
    /// tenth. The four-implementation byte-identity result that prompted two of
    /// my header fixes had the same hole for a stronger reason — its input
    /// carried exactly ONE attachment, and with one attachment every ordering
    /// rule agrees.
    ///
    /// So the question to ask of a byte-identity result is not whether the bytes
    /// matched. It is WHICH FIELDS THE INPUT COULD VARY.
    ///
    /// This input varies the tenth: four attachments passed deliberately out of
    /// order. Expected bytes from the same independent Python writer built from
    /// 7.1's table.
    private static let unsortedExpected = "UEsDBAoAAAgAAAAAIQB6uwngigAAAIoAAAAHAAAAZG9jLnZueHsKICAicHJvZmlsZSI6ICJmdWxsIiwKICAidm54IjogIjAuMS4wIiwKICAid2F5cG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9ClBLAwQKAAAIAAAAACEAB6Hq3QIAAAACAAAADwAAAGFhYS9lYXJsaWVyLnR4dGEKUEsDBAoAAAgAAAAAIQCdatx0AgAAAAIAAAALAAAAenotbGFzdC50eHR6ClBLAwQKAAAIAAAAACEA+Q4LTgMAAAADAAAACAAAAMOGYWUudHh0YWUKUEsDBAoAAAgAAAAAIQA1YL4VCQAAAAkAAAAPAAAA4oSrYW5nc3Ryb20udHh0YW5nc3Ryb20KUEsBAgoACgAACAAAAAAhAHq7CeCKAAAAigAAAAcAAAAAAAAAAAAAAAAAAAAAAGRvYy52bnhQSwECCgAKAAAIAAAAACEAB6Hq3QIAAAACAAAADwAAAAAAAAAAAAAAAACvAAAAYWFhL2VhcmxpZXIudHh0UEsBAgoACgAACAAAAAAhAJ1q3HQCAAAAAgAAAAsAAAAAAAAAAAAAAAAA3gAAAHp6LWxhc3QudHh0UEsBAgoACgAACAAAAAAhAPkOC04DAAAAAwAAAAgAAAAAAAAAAAAAAAAACQEAAMOGYWUudHh0UEsBAgoACgAACAAAAAAhADVgvhUJAAAACQAAAA8AAAAAAAAAAAAAAAAAMgEAAOKEq2FuZ3N0cm9tLnR4dFBLBQYAAAAABQAFAB4BAABoAQAAAAA="

    private func unsortedArchive() -> Data {
        var document = Document()
        document.profile = "full"
        document.waypoints = [Waypoint(at: Position(lon: -3.675, lat: 57.1201),
                                       name: "In a container", notes: nil, folder: nil,
                                       hidden: nil, style: nil, data: nil)]
        return VNX.writeContainer(document: document, attachments: [
            Attachment(path: "zz-last.txt", bytes: Data("z\n".utf8)),
            Attachment(path: "\u{212B}angstrom.txt", bytes: Data("angstrom\n".utf8)),
            Attachment(path: "aaa/earlier.txt", bytes: Data("a\n".utf8)),
            Attachment(path: "\u{00C6}ae.txt", bytes: Data("ae\n".utf8)),
        ])
    }

    func testAttachmentsAreSortedByEntryName() throws {
        let names = try Zip.entries(in: [UInt8](unsortedArchive())).map(\.name)
        XCTAssertEqual(names, ["doc.vnx", "aaa/earlier.txt", "zz-last.txt",
                               "\u{00C6}ae.txt", "\u{212B}angstrom.txt"],
                       "the root document first, then attachments sorted by code point")
        XCTAssertEqual(unsortedArchive(), Data(base64Encoded: Self.unsortedExpected)!,
                       "and byte-identical to a writer built from SPEC 7.1's table")
    }

    /// That the sort uses SPEC 4.5.1's comparator and not Swift's `<`.
    ///
    /// Measured: Swift's `<` places `U+212B` BELOW `U+00C6`, because ANGSTROM
    /// SIGN canonically decomposes to `U+00C5`; code point places it far above.
    /// The two orders differ on exactly this input, which is what makes the
    /// comparator choice load-bearing rather than a matter of taste. Same trap
    /// as canonical key ordering one file over, and the third time a Swift
    /// default has turned out to be a third order in this library.
    func testTheSortIsByCodePointNotSwiftsCollation() throws {
        let names = ["zz-last.txt", "\u{212B}angstrom.txt", "aaa/earlier.txt", "\u{00C6}ae.txt"]
        XCTAssertNotEqual(names.sorted(), names.sorted(by: CanonicalWriter.codePointLess),
                          "if these agreed, this test would be proving nothing")
        XCTAssertTrue("\u{212B}" < "\u{00C6}", "Swift normalises")
        XCTAssertFalse(CanonicalWriter.codePointLess("\u{212B}", "\u{00C6}"), "code point does not")

        let emitted = try Zip.entries(in: [UInt8](unsortedArchive())).map(\.name)
        XCTAssertEqual(Array(emitted.dropFirst()), names.sorted(by: CanonicalWriter.codePointLess))
        XCTAssertNotEqual(Array(emitted.dropFirst()), names.sorted(),
                          "Swift's own order would have produced a different archive")
    }

    /// A writer that stamped the clock could not be byte-compared with anything,
    /// including its own previous run.
    func testTheArchiveDoesNotDependOnWhenItWasWritten() {
        XCTAssertEqual(archive(), archive())
    }

    /// And what this library writes, it reads.
    func testItReadsBack() throws {
        let result = try VNX.readContainer(archive())
        XCTAssertEqual(result.documentName, "doc.vnx")
        XCTAssertEqual(result.diagnostics, [])
        XCTAssertEqual(result.attachments.map(\.path),
                       ["attachments/photo.txt", "attachments/vue-générale.txt"])
        XCTAssertEqual(result.attachments[1].bytes, Data("accented name\n".utf8))
    }
}
