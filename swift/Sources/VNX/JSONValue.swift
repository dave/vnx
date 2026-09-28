import Foundation

/// A JSON value.
///
/// A tree of its own rather than `Any`: a document's opaque regions — data
/// record bodies, style objects, application keys — must survive a read-modify-write
/// at value level (SPEC 5.1 rule 4), and `Any` cannot be compared, hashed or
/// carried across a concurrency boundary. `JSONEncoder` renders it; `JSONParser`
/// reads it.
public enum JSONValue: Equatable, Sendable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

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

    public var objectValue: [String: JSONValue]? {
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

/// What `JSONEncoder` needs to render this tree.
///
/// ⚠ `JSONSerialization` IS NOT AN ALTERNATIVE HERE: it renders `-3.67` as
/// `-3.6699999999999999`. `JSONEncoder` writes the shortest decimal that
/// round-trips, measured, which is what SPEC 4.2's value-level round-trip
/// guarantee needs.
extension JSONValue: Encodable {
    private struct Key: CodingKey {
        let stringValue: String
        var intValue: Int? { nil }
        init(_ s: String) { stringValue = s }
        init?(stringValue: String) { self.stringValue = stringValue }
        init?(intValue: Int) { return nil }
    }

    public func encode(to encoder: Swift.Encoder) throws {
        switch self {
        case .null:
            var c = encoder.singleValueContainer()
            try c.encodeNil()
        case .bool(let b):
            var c = encoder.singleValueContainer()
            try c.encode(b)
        case .number(let d):
            var c = encoder.singleValueContainer()
            // SPEC 4.2 forbids a non-finite number on the wire, and
            // `JSONEncoder` refuses one by default. That refusal is left to
            // reach the caller: the alternative to failing is writing a `NaN`
            // literal every conforming reader then rejects, which turns a
            // writer's bug into a file the user only discovers is unreadable
            // later.
            try c.encode(d)
        case .string(let s):
            var c = encoder.singleValueContainer()
            try c.encode(s)
        case .array(let a):
            var c = encoder.unkeyedContainer()
            for element in a { try c.encode(element) }
        case .object(let o):
            var c = encoder.container(keyedBy: Key.self)
            for (k, v) in o { try c.encode(v, forKey: Key(k)) }
        }
    }
}
