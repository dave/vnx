import Foundation

/// A member name in a JSON object.
///
/// ⚠ IT COMPARES BY UNICODE SCALARS, AND THAT IS THE WHOLE REASON IT EXISTS.
/// Swift's `String` equality is canonical equivalence, so a `[String: _]`
/// merges `"\u{e9}"` and `"e\u{301}"` into one member and silently loses the
/// other. JSON, and every other implementation of this format, treats them as
/// two different keys, so a record body or an application key's value holding
/// both must come back holding both. The same goes for the format's own checks:
/// SPEC 3.9's full-stop test is on code points, and a lookup by name must not
/// find a key that is merely canonically equivalent to the one asked for.
public struct JSONKey: Hashable, Sendable, ExpressibleByStringLiteral, CustomStringConvertible {
    public let string: String

    public init(_ string: String) { self.string = string }
    public init(stringLiteral value: String) { self.string = value }

    public static func == (a: JSONKey, b: JSONKey) -> Bool {
        a.string.unicodeScalars.elementsEqual(b.string.unicodeScalars)
    }

    public func hash(into hasher: inout Hasher) {
        var n = 0
        for u in string.unicodeScalars {
            hasher.combine(u.value)
            n += 1
        }
        hasher.combine(n)
    }

    public var description: String { string }
}

/// A JSON value.
///
/// A tree of its own rather than `Any`: a document's opaque regions — data
/// record bodies, style objects, application keys — must survive a read-modify-write
/// at value level (SPEC 5.1 rule 4), and `Any` cannot be compared, hashed or
/// carried across a concurrency boundary. `JSONParser` reads it and
/// `JSONWriter` renders it.
public enum JSONValue: Equatable, Sendable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([JSONValue])
    case object([JSONKey: JSONValue])

    public var numberValue: Double? {
        if case .number(let d) = self { return d }
        return nil
    }

    public var stringValue: String? {
        if case .string(let s) = self { return s }
        return nil
    }

    public var boolValue: Bool? {
        if case .bool(let b) = self { return b }
        return nil
    }

    public var arrayValue: [JSONValue]? {
        if case .array(let a) = self { return a }
        return nil
    }

    public var objectValue: [JSONKey: JSONValue]? {
        if case .object(let o) = self { return o }
        return nil
    }

    /// The name of this value's JSON type, for diagnostic prose.
    var typeName: String {
        switch self {
        case .null: return "null"
        case .bool: return "a boolean"
        case .number: return "a number"
        case .string: return "a string"
        case .array: return "an array"
        case .object: return "an object"
        }
    }
}

/// A number this format cannot spell: SPEC 4.2 forbids NaN and Infinity on the
/// wire. The writer refuses it rather than writing a literal every conforming
/// reader then rejects.
public struct NonFiniteNumberError: Error, Equatable, Sendable, CustomStringConvertible {
    public let value: Double
    public var description: String { "the number \(value) is not finite, and JSON cannot spell it (SPEC 4.2)" }
}

/// Renders a `JSONValue` as compact JSON.
///
/// ⚠ `JSONEncoder` IS NOT USED, because it cannot carry two keys that are
/// canonically equivalent: its keyed container and `.sortedKeys` both work on
/// `String`, which merges them (see `JSONKey`). This renderer writes the
/// punctuation itself. Members are sorted by code point, so the output is the
/// same on every run; key order is not significant to a reader (SPEC 4.4).
///
/// Numbers are spelled as `Double.description` with a trailing `.0` removed,
/// which is the shortest spelling that reads back as the same double and is
/// byte-for-byte what `JSONEncoder` wrote (measured over integral, tiny, huge,
/// negative-zero and 2^53-class values). `JSONSerialization` is no alternative:
/// it renders `-3.67` as `-3.6699999999999999`.
enum JSONWriter {
    static func render(_ value: JSONValue) throws -> Data {
        var out = [UInt8]()
        out.reserveCapacity(1 << 12)
        try write(value, into: &out)
        return Data(out)
    }

    private static func write(_ value: JSONValue, into out: inout [UInt8]) throws {
        switch value {
        case .null:
            out.append(contentsOf: Array("null".utf8))
        case .bool(let b):
            out.append(contentsOf: Array((b ? "true" : "false").utf8))
        case .number(let d):
            guard d.isFinite else { throw NonFiniteNumberError(value: d) }
            var s = d.description
            if s.hasSuffix(".0") { s.removeLast(2) }
            out.append(contentsOf: Array(s.utf8))
        case .string(let s):
            writeString(s, into: &out)
        case .array(let a):
            out.append(UInt8(ascii: "["))
            for (i, e) in a.enumerated() {
                if i > 0 { out.append(UInt8(ascii: ",")) }
                try write(e, into: &out)
            }
            out.append(UInt8(ascii: "]"))
        case .object(let o):
            out.append(UInt8(ascii: "{"))
            let keys = o.keys.sorted { codePointLess($0.string, $1.string) }
            for (i, k) in keys.enumerated() {
                if i > 0 { out.append(UInt8(ascii: ",")) }
                writeString(k.string, into: &out)
                out.append(UInt8(ascii: ":"))
                try write(o[k]!, into: &out)
            }
            out.append(UInt8(ascii: "}"))
        }
    }

    /// RFC 8259 section 7: `"` and `\` and the controls below U+0020 are
    /// escaped, and everything else is written as its own UTF-8.
    private static func writeString(_ s: String, into out: inout [UInt8]) {
        out.append(UInt8(ascii: "\""))
        for u in s.unicodeScalars {
            switch u.value {
            case 0x22: out.append(contentsOf: Array("\\\"".utf8))
            case 0x5C: out.append(contentsOf: Array("\\\\".utf8))
            case 0x0A: out.append(contentsOf: Array("\\n".utf8))
            case 0x0D: out.append(contentsOf: Array("\\r".utf8))
            case 0x09: out.append(contentsOf: Array("\\t".utf8))
            case 0x08: out.append(contentsOf: Array("\\b".utf8))
            case 0x0C: out.append(contentsOf: Array("\\f".utf8))
            case 0..<0x20:
                let hex = String(u.value, radix: 16)
                out.append(contentsOf: Array(("\\u" + String(repeating: "0", count: 4 - hex.count) + hex).utf8))
            default:
                UTF8.encode(u) { out.append($0) }
            }
        }
        out.append(UInt8(ascii: "\""))
    }
}
