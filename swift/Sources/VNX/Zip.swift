import Foundation

/// A minimal ZIP reader and writer, to the extent SPEC 7 requires.
///
/// Entries come from the central directory, in central directory order. SPEC 7
/// says the fallback root document is "the first entry with a `.vnx` extension
/// in archive order", and the central directory is the archive's own statement
/// of what it contains and in what order.
enum Zip {
    struct Entry {
        let name: String
        let method: UInt16
        let compressedSize: Int
        let uncompressedSize: Int
        let crc32: UInt32
        let localHeaderOffset: Int
    }

    struct MalformedArchive: Error { let message: String }

    static let methodStored: UInt16 = 0
    static let methodDeflate: UInt16 = 8

    // The three header fields SPEC 7 does not constrain, and which therefore
    // decide whether two languages' `.vnz` files are byte-identical. SPEC 4.5
    // requires byte-identical output from every conforming writer, but says it
    // of canonical JSON; SPEC 7 says nothing about the ZIP envelope. Reported
    // as a gap. Until it is ruled, these carry the values that are correct
    // against [APPNOTE] rather than the values a language's defaults produce,
    // and the Kotlin seat writes the same three.

    /// 1.0 — the minimum version for a stored entry. 2.0 (20) is the floor for
    /// DEFLATE, so a stored-only archive that stamps 20 overstates what a
    /// reader needs. Verified against the Kotlin seat 2026-09-09.
    static let versionNeededStored: UInt16 = 10

    /// 1980-01-01 00:00:00, the zero of the MS-DOS timestamp. `0x0000` in the
    /// date half is NOT that zero: it decodes as day 0 of month 0, which is not
    /// a date, and some tools reject it. The 16 bits are year-1980 (7) | month
    /// (4) | day (5), so 1980-01-01 is 0x0021.
    ///
    /// A fixed timestamp rather than the clock: a writer whose output depends
    /// on when it ran cannot be byte-compared with anything, including itself.
    static let dosTime: UInt16 = 0x0000
    static let dosDate: UInt16 = 0x0021

    // MARK: - Reading

    static func entries(in data: [UInt8]) throws -> [Entry] {
        let (directoryOffset, entryCount) = try locateCentralDirectory(data)
        var out: [Entry] = []
        var p = directoryOffset
        for _ in 0..<entryCount {
            guard p + 46 <= data.count, u32(data, p) == 0x02014B50 else {
                throw MalformedArchive(message: "central directory entry header not found")
            }
            let method = u16(data, p + 10)
            let crc = u32(data, p + 16)
            var compressed = Int(u32(data, p + 20))
            var uncompressed = Int(u32(data, p + 24))
            let nameLength = Int(u16(data, p + 28))
            let extraLength = Int(u16(data, p + 30))
            let commentLength = Int(u16(data, p + 32))
            var localOffset = Int(u32(data, p + 42))
            guard p + 46 + nameLength + extraLength + commentLength <= data.count else {
                throw MalformedArchive(message: "central directory entry runs past the end of the archive")
            }
            let nameBytes = Array(data[(p + 46)..<(p + 46 + nameLength)])
            guard let name = String(bytes: nameBytes, encoding: .utf8) else {
                throw MalformedArchive(message: "an entry name is not UTF-8")
            }
            // SPEC 7: a reader MUST read an archive that uses the ZIP64
            // extensions. The 0x0001 extra field replaces whichever of the
            // three fields is stored as all-ones, in this order.
            let extra = Array(data[(p + 46 + nameLength)..<(p + 46 + nameLength + extraLength)])
            var q = 0
            while q + 4 <= extra.count {
                let id = u16(extra, q)
                let size = Int(u16(extra, q + 2))
                guard q + 4 + size <= extra.count else { break }
                if id == 0x0001 {
                    var r = q + 4
                    if uncompressed == 0xFFFFFFFF, r + 8 <= q + 4 + size { uncompressed = Int(u64(extra, r)); r += 8 }
                    if compressed == 0xFFFFFFFF, r + 8 <= q + 4 + size { compressed = Int(u64(extra, r)); r += 8 }
                    if localOffset == 0xFFFFFFFF, r + 8 <= q + 4 + size { localOffset = Int(u64(extra, r)); r += 8 }
                }
                q += 4 + size
            }
            out.append(Entry(name: name, method: method, compressedSize: compressed,
                             uncompressedSize: uncompressed, crc32: crc, localHeaderOffset: localOffset))
            p += 46 + nameLength + extraLength + commentLength
        }
        return out
    }

    /// The uncompressed bytes of one entry.
    ///
    /// The sizes come from the central directory rather than from the local
    /// header, because an entry written with a data descriptor (general purpose
    /// bit 3) carries zeros in its local header and the central directory is
    /// the authority in that case.
    static func contents(of entry: Entry, in data: [UInt8]) throws -> [UInt8] {
        let p = entry.localHeaderOffset
        guard p + 30 <= data.count, u32(data, p) == 0x04034B50 else {
            throw MalformedArchive(message: "local header not found for \(entry.name)")
        }
        let nameLength = Int(u16(data, p + 26))
        let extraLength = Int(u16(data, p + 28))
        let start = p + 30 + nameLength + extraLength
        guard start + entry.compressedSize <= data.count else {
            throw MalformedArchive(message: "entry data runs past the end of the archive")
        }
        let raw = Array(data[start..<(start + entry.compressedSize)])
        let bytes: [UInt8]
        switch entry.method {
        case methodStored: bytes = raw
        case methodDeflate: bytes = try Inflate.inflate(raw, expectedSize: entry.uncompressedSize)
        default: throw MalformedArchive(message: "unsupported compression method \(entry.method)")
        }
        guard CRC32.checksum(bytes) == entry.crc32 else {
            throw MalformedArchive(message: "CRC mismatch for \(entry.name)")
        }
        return bytes
    }

    private static func locateCentralDirectory(_ data: [UInt8]) throws -> (offset: Int, count: Int) {
        // End of central directory, scanned backwards over the comment field.
        var eocd = -1
        var i = data.count - 22
        let limit = max(0, data.count - 22 - 0xFFFF)
        while i >= limit {
            if i >= 0, u32(data, i) == 0x06054B50 { eocd = i; break }
            i -= 1
        }
        guard eocd >= 0 else { throw MalformedArchive(message: "no end-of-central-directory record") }
        var count = Int(u16(data, eocd + 10))
        var offset = Int(u32(data, eocd + 16))

        // ZIP64: the locator sits immediately before the EOCD record.
        let locator = eocd - 20
        if locator >= 0, u32(data, locator) == 0x07064B50 {
            let zip64End = Int(u64(data, locator + 8))
            if zip64End >= 0, zip64End + 56 <= data.count, u32(data, zip64End) == 0x06064B50 {
                count = Int(u64(data, zip64End + 32))
                offset = Int(u64(data, zip64End + 48))
            }
        }
        guard offset >= 0, offset <= data.count else {
            throw MalformedArchive(message: "central directory offset is out of range")
        }
        return (offset, count)
    }

    private static func u16(_ d: [UInt8], _ i: Int) -> UInt16 {
        UInt16(d[i]) | UInt16(d[i + 1]) << 8
    }
    private static func u32(_ d: [UInt8], _ i: Int) -> UInt32 {
        UInt32(d[i]) | UInt32(d[i + 1]) << 8 | UInt32(d[i + 2]) << 16 | UInt32(d[i + 3]) << 24
    }
    private static func u64(_ d: [UInt8], _ i: Int) -> UInt64 {
        var v: UInt64 = 0
        for k in (0..<8).reversed() { v = v << 8 | UInt64(d[i + k]) }
        return v
    }

    // MARK: - Writing

    /// Writes a plain (non-ZIP64) archive with every entry stored.
    ///
    /// SPEC 7 permits it: "Compression MUST be stored or deflate", and "a
    /// writer MAY write a plain archive without [ZIP64]". Stored rather than
    /// deflate because a deflate compressor is a second algorithm this library
    /// would have to carry to no conformance end — nothing in the corpus, and
    /// nothing in SPEC 7, asks a writer to compress.
    static func archive(entries: [(name: String, body: [UInt8])]) -> Data {
        var out: [UInt8] = []
        var central: [UInt8] = []
        for (name, body) in entries {
            let nameBytes = [UInt8](name.utf8)
            let crc = CRC32.checksum(body)
            let offset = out.count
            out += [0x50, 0x4B, 0x03, 0x04]
            out += le16(versionNeededStored)
            out += le16(0x0800)          // UTF-8 filename flag, per SPEC 7
            out += le16(methodStored)
            out += le16(dosTime) + le16(dosDate)
            out += le32(crc)
            out += le32(UInt32(body.count))
            out += le32(UInt32(body.count))
            out += le16(UInt16(nameBytes.count))
            out += le16(0)
            out += nameBytes
            out += body

            central += [0x50, 0x4B, 0x01, 0x02]
            central += le16(versionNeededStored) + le16(versionNeededStored)
            central += le16(0x0800)
            central += le16(methodStored)
            central += le16(dosTime) + le16(dosDate)
            central += le32(crc)
            central += le32(UInt32(body.count))
            central += le32(UInt32(body.count))
            central += le16(UInt16(nameBytes.count))
            central += le16(0) + le16(0)
            central += le16(0) + le16(0)
            central += le32(0)
            central += le32(UInt32(offset))
            central += nameBytes
        }
        let centralOffset = out.count
        out += central
        out += [0x50, 0x4B, 0x05, 0x06]
        out += le16(0) + le16(0)
        out += le16(UInt16(entries.count)) + le16(UInt16(entries.count))
        out += le32(UInt32(central.count))
        out += le32(UInt32(centralOffset))
        out += le16(0)
        return Data(out)
    }

    private static func le16(_ v: UInt16) -> [UInt8] { [UInt8(v & 0xFF), UInt8(v >> 8)] }
    private static func le32(_ v: UInt32) -> [UInt8] {
        [UInt8(v & 0xFF), UInt8((v >> 8) & 0xFF), UInt8((v >> 16) & 0xFF), UInt8((v >> 24) & 0xFF)]
    }
}
