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
