import Foundation

/// The text of a JSON syntax error, with the byte offset it was found at.
struct JSONSyntaxError: Error {
    let message: String
    let offset: Int
}

/// A strict RFC 8259 parser.
///
/// Strictness is a requirement, not a preference. SPEC 4.2 forbids `NaN`,
/// `Infinity` and `-Infinity` throughout a document and notes that "some
/// language runtimes emit and accept them by default"; the corpus carries
/// `reject/nan-literal.vnx` and `reject/infinity-literal.vnx` for exactly that.
/// A permissive parser turns both of those files into accepted documents whose
/// coordinates poison every distance computed downstream.
///
/// Duplicate object keys resolve last-wins, which is what SPEC 6.4 describes as
/// the behaviour of "every mainstream JSON parser" and documents as a
/// limitation of the format rather than a rule.
struct JSONParser {
    private let bytes: [UInt8]
    private var i: Int = 0

    private init(_ bytes: [UInt8]) { self.bytes = bytes }

    static func parse(_ data: Data) throws -> JSONValue {
        var p = JSONParser([UInt8](data))
        p.skipWhitespace()
        let v = try p.parseValue(depth: 0)
        p.skipWhitespace()
        if p.i != p.bytes.count {
            throw JSONSyntaxError(message: "trailing content after the top-level value", offset: p.i)
        }
        return v
    }

    // MARK: - Scanning

    private var atEnd: Bool { i >= bytes.count }

    private mutating func skipWhitespace() {
        while i < bytes.count {
            switch bytes[i] {
            case 0x20, 0x09, 0x0A, 0x0D: i += 1
            default: return
            }
        }
    }

    private func fail(_ message: String) -> JSONSyntaxError {
        JSONSyntaxError(message: message, offset: i)
    }

    /// A depth cap. SPEC 9 asks a reader that accepts untrusted documents to
    /// bound what one document may consume; unbounded recursion on a nested
    /// array is the one place this parser could be made to crash rather than
    /// return an error.
    private static let maxDepth = 512

    private mutating func parseValue(depth: Int) throws -> JSONValue {
        if depth > Self.maxDepth { throw fail("nested too deeply") }
        guard !atEnd else { throw fail("unexpected end of input") }
        switch bytes[i] {
        case UInt8(ascii: "{"): return try parseObject(depth: depth)
        case UInt8(ascii: "["): return try parseArray(depth: depth)
        case UInt8(ascii: "\""): return .string(try parseString())
        case UInt8(ascii: "t"): try expect("true"); return .bool(true)
        case UInt8(ascii: "f"): try expect("false"); return .bool(false)
        case UInt8(ascii: "n"): try expect("null"); return .null
        case UInt8(ascii: "-"), UInt8(ascii: "0")...UInt8(ascii: "9"):
            return .number(try parseNumber())
        default:
            throw fail("unexpected character '\(Character(UnicodeScalar(bytes[i])))'")
        }
    }

    private mutating func expect(_ literal: String) throws {
        let l = [UInt8](literal.utf8)
        guard i + l.count <= bytes.count, Array(bytes[i..<(i + l.count)]) == l else {
            throw fail("expected `\(literal)`")
        }
        i += l.count
    }

    private mutating func parseObject(depth: Int) throws -> JSONValue {
        i += 1 // {
        var out: [String: JSONValue] = [:]
        skipWhitespace()
        if !atEnd, bytes[i] == UInt8(ascii: "}") { i += 1; return .object(out) }
        while true {
            skipWhitespace()
            guard !atEnd, bytes[i] == UInt8(ascii: "\"") else { throw fail("expected a key string") }
            let key = try parseString()
            skipWhitespace()
            guard !atEnd, bytes[i] == UInt8(ascii: ":") else { throw fail("expected `:` after a key") }
            i += 1
            skipWhitespace()
            // Last-wins on a duplicate key: SPEC 6.4.
            out[key] = try parseValue(depth: depth + 1)
            skipWhitespace()
            guard !atEnd else { throw fail("unexpected end of input inside an object") }
            if bytes[i] == UInt8(ascii: ",") { i += 1; continue }
            if bytes[i] == UInt8(ascii: "}") { i += 1; return .object(out) }
            throw fail("expected `,` or `}`")
        }
    }

    private mutating func parseArray(depth: Int) throws -> JSONValue {
        i += 1 // [
        var out: [JSONValue] = []
        skipWhitespace()
        if !atEnd, bytes[i] == UInt8(ascii: "]") { i += 1; return .array(out) }
        while true {
            skipWhitespace()
            out.append(try parseValue(depth: depth + 1))
            skipWhitespace()
            guard !atEnd else { throw fail("unexpected end of input inside an array") }
            if bytes[i] == UInt8(ascii: ",") { i += 1; continue }
            if bytes[i] == UInt8(ascii: "]") { i += 1; return .array(out) }
            throw fail("expected `,` or `]`")
        }
    }

    private mutating func parseString() throws -> String {
        i += 1 // opening quote
        var scalars = String.UnicodeScalarView()
        var pendingHighSurrogate: UInt32? = nil

        func flushLoneSurrogate() {
            if pendingHighSurrogate != nil {
                // An unpaired surrogate is not a scalar value. U+FFFD is what
                // every mainstream parser substitutes; refusing here would
                // refuse documents Go, Python and JavaScript all read.
                scalars.append(UnicodeScalar(0xFFFD)!)
                pendingHighSurrogate = nil
            }
        }

        while true {
            guard !atEnd else { throw fail("unterminated string") }
            let b = bytes[i]
            if b == UInt8(ascii: "\"") {
                i += 1
                flushLoneSurrogate()
                return String(scalars)
            }
            if b < 0x20 {
                throw fail("unescaped control character U+\(String(format: "%04X", Int(b))) in a string")
            }
            if b == UInt8(ascii: "\\") {
                i += 1
                guard !atEnd else { throw fail("unterminated escape") }
                let e = bytes[i]
                i += 1
                switch e {
                case UInt8(ascii: "u"):
                    let u = try parseHex4()
                    if u >= 0xD800 && u <= 0xDBFF {
                        flushLoneSurrogate()
                        pendingHighSurrogate = u
                    } else if u >= 0xDC00 && u <= 0xDFFF {
                        if let hi = pendingHighSurrogate {
                            pendingHighSurrogate = nil
                            let cp = 0x10000 + ((hi - 0xD800) << 10) + (u - 0xDC00)
                            scalars.append(UnicodeScalar(cp)!)
                        } else {
                            scalars.append(UnicodeScalar(0xFFFD)!)
                        }
                    } else {
                        flushLoneSurrogate()
                        scalars.append(UnicodeScalar(u)!)
                    }
                    continue
                default:
                    flushLoneSurrogate()
                    switch e {
                    case UInt8(ascii: "\""): scalars.append("\"")
                    case UInt8(ascii: "\\"): scalars.append("\\")
                    case UInt8(ascii: "/"): scalars.append("/")
                    case UInt8(ascii: "b"): scalars.append(UnicodeScalar(0x08))
                    case UInt8(ascii: "f"): scalars.append(UnicodeScalar(0x0C))
                    case UInt8(ascii: "n"): scalars.append("\n")
                    case UInt8(ascii: "r"): scalars.append("\r")
                    case UInt8(ascii: "t"): scalars.append("\t")
                    default: throw fail("invalid escape `\\\(Character(UnicodeScalar(e)))`")
                    }
                    continue
                }
            }
            flushLoneSurrogate()
            // Raw UTF-8. Decode a whole sequence so that a scalar above U+FFFF
            // arrives as one scalar; SPEC 4.5.1 orders keys by code point, and
            // that only means anything if the scalars are intact.
            let len = Self.utf8SequenceLength(b)
            guard len > 0, i + len <= bytes.count else { throw fail("malformed UTF-8 in a string") }
            let seq = Array(bytes[i..<(i + len)])
            guard let s = String(bytes: seq, encoding: .utf8), s.unicodeScalars.count == 1 else {
                throw fail("malformed UTF-8 in a string")
            }
            scalars.append(s.unicodeScalars.first!)
            i += len
        }
    }

    private static func utf8SequenceLength(_ b: UInt8) -> Int {
        if b < 0x80 { return 1 }
        if b >= 0xC2 && b <= 0xDF { return 2 }
        if b >= 0xE0 && b <= 0xEF { return 3 }
        if b >= 0xF0 && b <= 0xF4 { return 4 }
        return 0
    }

    private mutating func parseHex4() throws -> UInt32 {
        guard i + 4 <= bytes.count else { throw fail("truncated \\u escape") }
        var v: UInt32 = 0
        for _ in 0..<4 {
            let c = bytes[i]
            let d: UInt32
            switch c {
            case UInt8(ascii: "0")...UInt8(ascii: "9"): d = UInt32(c - UInt8(ascii: "0"))
            case UInt8(ascii: "a")...UInt8(ascii: "f"): d = UInt32(c - UInt8(ascii: "a")) + 10
            case UInt8(ascii: "A")...UInt8(ascii: "F"): d = UInt32(c - UInt8(ascii: "A")) + 10
            default: throw fail("invalid hex digit in a \\u escape")
            }
            v = v << 4 | d
            i += 1
        }
        return v
    }

    /// JSON's number grammar exactly: no leading `+`, no leading zero on a
    /// multi-digit integer part, no bare `.5`, no `5.`, no hex, and no `NaN`
    /// or `Infinity` — those are not numbers here, they are unexpected
    /// characters, which is why the corpus expects "malformed JSON" for them.
    private mutating func parseNumber() throws -> Double {
        let start = i
        if !atEnd, bytes[i] == UInt8(ascii: "-") { i += 1 }
        guard !atEnd, isDigit(bytes[i]) else { throw fail("expected a digit") }
        if bytes[i] == UInt8(ascii: "0") {
            i += 1
            if !atEnd, isDigit(bytes[i]) { throw fail("a number may not have a leading zero") }
        } else {
            while !atEnd, isDigit(bytes[i]) { i += 1 }
        }
        if !atEnd, bytes[i] == UInt8(ascii: ".") {
            i += 1
            guard !atEnd, isDigit(bytes[i]) else { throw fail("expected a digit after the decimal point") }
            while !atEnd, isDigit(bytes[i]) { i += 1 }
        }
        if !atEnd, bytes[i] == UInt8(ascii: "e") || bytes[i] == UInt8(ascii: "E") {
            i += 1
            if !atEnd, bytes[i] == UInt8(ascii: "+") || bytes[i] == UInt8(ascii: "-") { i += 1 }
            guard !atEnd, isDigit(bytes[i]) else { throw fail("expected a digit in the exponent") }
            while !atEnd, isDigit(bytes[i]) { i += 1 }
        }
        let text = String(decoding: bytes[start..<i], as: UTF8.self)
        guard let d = Double(text) else { throw fail("`\(text)` is not a number") }
        return d
    }

    private func isDigit(_ b: UInt8) -> Bool { b >= UInt8(ascii: "0") && b <= UInt8(ascii: "9") }
}
