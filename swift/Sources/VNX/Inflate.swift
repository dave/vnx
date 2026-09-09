import Foundation

/// RFC 1951 DEFLATE decompression.
///
/// Written out rather than delegated because the alternative on Apple platforms
/// is the `Compression` framework, which is not Foundation and does not exist
/// on Linux. A VNZ reader that only works on one platform is not a
/// conformance-corpus implementation.
enum Inflate {
    struct Error: Swift.Error { let message: String }

    static func inflate(_ input: [UInt8], expectedSize: Int) throws -> [UInt8] {
        var reader = BitReader(input)
        var out: [UInt8] = []
        out.reserveCapacity(expectedSize)

        while true {
            let final = try reader.bits(1)
            let type = try reader.bits(2)
            switch type {
            case 0:
                reader.alignToByte()
                let len = Int(try reader.bits(16))
                let nlen = Int(try reader.bits(16))
                guard len == (~nlen & 0xFFFF) else { throw Error(message: "stored block length check failed") }
                for _ in 0..<len { out.append(try reader.byte()) }
            case 1:
                try inflateBlock(&reader, &out, literals: HuffmanTable.fixedLiterals, distances: HuffmanTable.fixedDistances)
            case 2:
                let (literals, distances) = try readDynamicTables(&reader)
                try inflateBlock(&reader, &out, literals: literals, distances: distances)
            default:
                throw Error(message: "reserved deflate block type")
            }
            if final == 1 { break }
        }
        return out
    }

    // MARK: - Blocks

    private static let lengthBase = [3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31,
                                     35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258]
    private static let lengthExtra = [0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2,
                                      3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0]
    private static let distanceBase = [1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193,
                                       257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145,
                                       8193, 12289, 16385, 24577]
    private static let distanceExtra = [0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6,
                                        7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13]

    private static func inflateBlock(_ reader: inout BitReader, _ out: inout [UInt8],
                                     literals: HuffmanTable, distances: HuffmanTable) throws {
        while true {
            let symbol = try literals.decode(&reader)
            if symbol < 256 {
                out.append(UInt8(symbol))
                continue
            }
            if symbol == 256 { return }
            let li = symbol - 257
            guard li < lengthBase.count else { throw Error(message: "invalid length symbol") }
            let length = lengthBase[li] + Int(try reader.bits(lengthExtra[li]))
            let ds = try distances.decode(&reader)
            guard ds < distanceBase.count else { throw Error(message: "invalid distance symbol") }
            let distance = distanceBase[ds] + Int(try reader.bits(distanceExtra[ds]))
            guard distance <= out.count else { throw Error(message: "distance runs before the start of the output") }
            var from = out.count - distance
            for _ in 0..<length {
                out.append(out[from])
                from += 1
            }
        }
    }

    private static let codeLengthOrder = [16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15]

    private static func readDynamicTables(_ reader: inout BitReader) throws -> (HuffmanTable, HuffmanTable) {
        let hlit = Int(try reader.bits(5)) + 257
        let hdist = Int(try reader.bits(5)) + 1
        let hclen = Int(try reader.bits(4)) + 4

        var codeLengthLengths = [Int](repeating: 0, count: 19)
        for i in 0..<hclen { codeLengthLengths[codeLengthOrder[i]] = Int(try reader.bits(3)) }
        let codeLengthTable = try HuffmanTable(lengths: codeLengthLengths)

        var lengths: [Int] = []
        lengths.reserveCapacity(hlit + hdist)
        while lengths.count < hlit + hdist {
            let symbol = try codeLengthTable.decode(&reader)
            switch symbol {
            case 0...15:
                lengths.append(symbol)
            case 16:
                guard let previous = lengths.last else { throw Error(message: "repeat with no previous length") }
                let n = 3 + Int(try reader.bits(2))
                lengths.append(contentsOf: repeatElement(previous, count: n))
            case 17:
                let n = 3 + Int(try reader.bits(3))
                lengths.append(contentsOf: repeatElement(0, count: n))
            case 18:
                let n = 11 + Int(try reader.bits(7))
                lengths.append(contentsOf: repeatElement(0, count: n))
            default:
                throw Error(message: "invalid code length symbol")
            }
        }
        guard lengths.count == hlit + hdist else { throw Error(message: "code length table overran") }
        return (try HuffmanTable(lengths: Array(lengths[0..<hlit])),
                try HuffmanTable(lengths: Array(lengths[hlit...])))
    }

    // MARK: - Bit reader

    struct BitReader {
        private let bytes: [UInt8]
        private var position = 0
        private var bitBuffer: UInt32 = 0
        private var bitCount = 0

        init(_ bytes: [UInt8]) { self.bytes = bytes }

        mutating func bits(_ n: Int) throws -> UInt32 {
            if n == 0 { return 0 }
            while bitCount < n {
                guard position < bytes.count else { throw Error(message: "unexpected end of deflate stream") }
                bitBuffer |= UInt32(bytes[position]) << UInt32(bitCount)
                position += 1
                bitCount += 8
            }
            let value = bitBuffer & ((1 << UInt32(n)) - 1)
            bitBuffer >>= UInt32(n)
            bitCount -= n
            return value
        }

        mutating func alignToByte() {
            bitBuffer = 0
            bitCount = 0
        }

        mutating func byte() throws -> UInt8 {
            guard position < bytes.count else { throw Error(message: "unexpected end of deflate stream") }
            defer { position += 1 }
            return bytes[position]
        }
    }

    // MARK: - Huffman

    struct HuffmanTable {
        /// Canonical Huffman, decoded one bit at a time: counts per length and
        /// the symbols in canonical order, which is the compact formulation
        /// from RFC 1951 section 3.2.2.
        private let counts: [Int]
        private let symbols: [Int]

        init(lengths: [Int]) throws {
            var counts = [Int](repeating: 0, count: 16)
            for l in lengths where l > 0 { counts[l] += 1 }
            var offsets = [Int](repeating: 0, count: 16)
            for l in 1..<15 { offsets[l + 1] = offsets[l] + counts[l] }
            var symbols = [Int](repeating: 0, count: lengths.filter { $0 > 0 }.count)
            for (symbol, l) in lengths.enumerated() where l > 0 {
                symbols[offsets[l]] = symbol
                offsets[l] += 1
            }
            self.counts = counts
            self.symbols = symbols
        }

        func decode(_ reader: inout BitReader) throws -> Int {
            var code = 0, first = 0, index = 0
            for length in 1...15 {
                code |= Int(try reader.bits(1))
                let count = counts[length]
                if code - first < count { return symbols[index + (code - first)] }
                index += count
                first = (first + count) << 1
                code <<= 1
            }
            throw Error(message: "invalid Huffman code")
        }

        static let fixedLiterals: HuffmanTable = {
            var lengths = [Int](repeating: 8, count: 288)
            for i in 144..<256 { lengths[i] = 9 }
            for i in 256..<280 { lengths[i] = 7 }
            return try! HuffmanTable(lengths: lengths)
        }()

        static let fixedDistances: HuffmanTable = {
            try! HuffmanTable(lengths: [Int](repeating: 5, count: 30))
        }()
    }
}

/// The CRC-32 of ZIP and RFC 1952, used to verify an entry after inflating it.
enum CRC32 {
    private static let table: [UInt32] = {
        (0..<256).map { i -> UInt32 in
            var c = UInt32(i)
            for _ in 0..<8 { c = (c & 1) != 0 ? (0xEDB88320 ^ (c >> 1)) : (c >> 1) }
            return c
        }
    }()

    static func checksum(_ bytes: [UInt8]) -> UInt32 {
        var c: UInt32 = 0xFFFFFFFF
        for b in bytes { c = table[Int((c ^ UInt32(b)) & 0xFF)] ^ (c >> 8) }
        return c ^ 0xFFFFFFFF
    }
}
