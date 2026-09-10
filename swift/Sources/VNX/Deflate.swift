import CZlib
import Foundation

/// RFC 1951 DEFLATE, and the CRC-32 of RFC 1952, through zlib.
///
/// zlib rather than a hand-written codec: it ships with the macOS SDK and with
/// every Linux distribution, so it costs no package dependency, and a
/// compressor written here would be a second algorithm to maintain for no
/// conformance end. `Compression.framework` is the Apple-only alternative and
/// is not available on Linux, which a conformance-corpus implementation must
/// build on.
///
/// RAW deflate, never the zlib wrapper: a ZIP entry's payload is a bare
/// DEFLATE stream with no 2-byte header and no Adler-32 trailer, which is what
/// the negative window bits below select.
enum Deflate {
    struct Failure: Error, CustomStringConvertible {
        let description: String
    }

    private static let rawWindowBits: Int32 = -15

    static func compress(_ input: [UInt8]) throws -> [UInt8] {
        var stream = z_stream()
        var status = deflateInit2_(&stream, Z_DEFAULT_COMPRESSION, Z_DEFLATED, rawWindowBits, 8,
                                   Z_DEFAULT_STRATEGY, ZLIB_VERSION, Int32(MemoryLayout<z_stream>.size))
        guard status == Z_OK else {
            throw Failure(description: "zlib could not start a deflate stream (\(status))")
        }
        defer { deflateEnd(&stream) }

        var out = [UInt8]()
        out.reserveCapacity(max(64, input.count / 2))
        var buffer = [UInt8](repeating: 0, count: 64 * 1024)
        var source = input
        try source.withUnsafeMutableBufferPointer { inputBytes in
            stream.next_in = inputBytes.baseAddress
            stream.avail_in = uInt(inputBytes.count)
            repeat {
                try buffer.withUnsafeMutableBufferPointer { outputBytes in
                    stream.next_out = outputBytes.baseAddress
                    stream.avail_out = uInt(outputBytes.count)
                    status = deflate(&stream, Z_FINISH)
                    guard status == Z_OK || status == Z_STREAM_END || status == Z_BUF_ERROR else {
                        throw Failure(description: "zlib refused to deflate (\(status))")
                    }
                    out.append(contentsOf: outputBytes.prefix(outputBytes.count - Int(stream.avail_out)))
                }
            } while status != Z_STREAM_END
        }
        return out
    }

    /// Inflates one raw DEFLATE stream.
    ///
    /// `expectedSize` sizes the first buffer and is NOT trusted as a limit: the
    /// central directory's declared size is a ceiling rather than a length
    /// (SPEC 7), and the CRC over what came out is what decides whether the
    /// entry is intact.
    static func decompress(_ input: [UInt8], expectedSize: Int) throws -> [UInt8] {
        var stream = z_stream()
        var status = inflateInit2_(&stream, rawWindowBits, ZLIB_VERSION, Int32(MemoryLayout<z_stream>.size))
        guard status == Z_OK else {
            throw Failure(description: "zlib could not start an inflate stream (\(status))")
        }
        defer { inflateEnd(&stream) }

        var out = [UInt8]()
        out.reserveCapacity(expectedSize)
        var buffer = [UInt8](repeating: 0, count: max(64 * 1024, min(expectedSize, 1 << 20)))
        var source = input
        try source.withUnsafeMutableBufferPointer { inputBytes in
            stream.next_in = inputBytes.baseAddress
            stream.avail_in = uInt(inputBytes.count)
            repeat {
                try buffer.withUnsafeMutableBufferPointer { outputBytes in
                    stream.next_out = outputBytes.baseAddress
                    stream.avail_out = uInt(outputBytes.count)
                    status = inflate(&stream, Z_NO_FLUSH)
                    guard status == Z_OK || status == Z_STREAM_END || status == Z_BUF_ERROR else {
                        throw Failure(description: "the deflate stream is not readable (\(status))")
                    }
                    out.append(contentsOf: outputBytes.prefix(outputBytes.count - Int(stream.avail_out)))
                }
                // Z_BUF_ERROR with no input left is the end of what this entry
                // holds. Looping on it would spin.
                if status == Z_BUF_ERROR && stream.avail_in == 0 { break }
            } while status != Z_STREAM_END
        }
        return out
    }

    /// The CRC-32 a ZIP entry carries, over the UNCOMPRESSED bytes.
    static func crc32(_ bytes: [UInt8]) -> UInt32 {
        var value = CZlib.crc32(0, nil, 0)
        bytes.withUnsafeBufferPointer { buffer in
            if let base = buffer.baseAddress {
                value = CZlib.crc32(value, base, uInt(buffer.count))
            }
        }
        return UInt32(truncatingIfNeeded: value)
    }
}
