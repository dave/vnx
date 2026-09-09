import Foundation

/// A JSON value.
///
/// VNX needs its own JSON tree rather than `Any`/`JSONSerialization` for three
/// reasons the specification states directly: `JSONSerialization` renders
/// `-3.67` as `-3.6699999999999999` (SPEC 4.5.6), a stock encoder cannot
/// produce canonical key order, and a document's opaque regions — data record
/// bodies, style objects, `rulesets` — must survive a read-modify-write at
/// value level (SPEC 5.1 rule 4).
public enum JSONValue: Equatable, Sendable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

    /// True for strings, numbers, booleans and nulls: group 1 of SPEC 4.5.1.
    var isScalar: Bool {
        switch self {
        case .null, .bool, .number, .string: return true
        case .array, .object: return false
        }
    }

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
