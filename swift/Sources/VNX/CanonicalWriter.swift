import Foundation

/// The canonical serialiser of SPEC 4.5.
///
/// No stock encoder in any target language emits this layout (SPEC 4.5.6), and
/// on Apple platforms `JSONSerialization` is disqualified twice over: it writes
/// `" : "` as its separator and renders `-3.67` as `-3.6699999999999999`.
enum CanonicalWriter {
    static func render(_ value: JSONValue) -> Data {
        var out = String()
        out.reserveCapacity(4096)
        write(value, indent: 0, into: &out)
        out.append("\n") // SPEC 4.5.5: a newline at end of file.
        return Data(out.utf8)
    }

    private static func write(_ value: JSONValue, indent: Int, into out: inout String) {
        switch value {
        case .null: out.append("null")
        case .bool(let b): out.append(b ? "true" : "false")
        case .number(let d): out.append(ecmaScriptNumber(d))
        case .string(let s): writeString(s, into: &out)
        case .array(let a): writeArray(a, indent: indent, into: &out)
        case .object(let o): writeObject(o, indent: indent, into: &out)
        }
    }

    private static func writeArray(_ a: [JSONValue], indent: Int, into out: inout String) {
        if a.isEmpty { out.append("[]"); return }
        // SPEC 4.5.2: an array holding only numbers and `null` is written on
        // one line. `null` counts as a number for this rule, so a position does
        // not change shape when its elevation goes missing.
        let inline = a.allSatisfy {
            switch $0 {
            case .number, .null: return true
            default: return false
            }
        }
        if inline {
            out.append("[")
            for (i, e) in a.enumerated() {
                if i > 0 { out.append(", ") }
                write(e, indent: indent, into: &out)
            }
            out.append("]")
            return
        }
        let inner = indent + 2
        out.append("[\n")
        for (i, e) in a.enumerated() {
            out.append(String(repeating: " ", count: inner))
            write(e, indent: inner, into: &out)
            out.append(i == a.count - 1 ? "\n" : ",\n")
        }
        out.append(String(repeating: " ", count: indent))
        out.append("]")
    }

    private static func writeObject(_ o: [String: JSONValue], indent: Int, into out: inout String) {
        if o.isEmpty { out.append("{}"); return }
        // SPEC 4.5.1: scalars first in code-point order, then containers in
        // code-point order, classified by the value present in this object.
        var scalars: [String] = []
        var containers: [String] = []
        for (k, v) in o {
            if v.isScalar { scalars.append(k) } else { containers.append(k) }
        }
        scalars.sort(by: codePointLess)
        containers.sort(by: codePointLess)
        let keys = scalars + containers

        let inner = indent + 2
        out.append("{\n")
        for (i, k) in keys.enumerated() {
            out.append(String(repeating: " ", count: inner))
            writeString(k, into: &out)
            out.append(": ")
            write(o[k]!, indent: inner, into: &out)
            out.append(i == keys.count - 1 ? "\n" : ",\n")
        }
        out.append(String(repeating: " ", count: indent))
        out.append("}")
    }

    /// Order by Unicode code point.
    ///
    /// Swift's `<` on `String` is not this: it compares by normalised grapheme
    /// clusters, so `"e\u{301}"` and `"\u{e9}"` compare equal to it and do not
    /// compare equal here. SPEC 4.5.1 requires code-point order because a data
    /// record body may carry any key and the orders disagree above U+FFFF.
    static func codePointLess(_ a: String, _ b: String) -> Bool {
        var x = a.unicodeScalars.makeIterator()
        var y = b.unicodeScalars.makeIterator()
        while true {
            switch (x.next(), y.next()) {
            case (nil, nil): return false
            case (nil, _): return true
            case (_, nil): return false
            case (let l?, let r?):
                if l.value != r.value { return l.value < r.value }
            }
        }
    }

    /// SPEC 4.5.5: escape only what RFC 8259 requires — `"`, `\` and the
    /// control characters below U+0020. Everything else is written raw,
    /// including non-ASCII, `<`, `>`, `&`, U+2028 and U+2029.
    private static func writeString(_ s: String, into out: inout String) {
        out.append("\"")
        for scalar in s.unicodeScalars {
            switch scalar {
            case "\"": out.append("\\\"")
            case "\\": out.append("\\\\")
            case "\u{08}": out.append("\\b")
            case "\u{0C}": out.append("\\f")
            case "\n": out.append("\\n")
            case "\r": out.append("\\r")
            case "\t": out.append("\\t")
            default:
                if scalar.value < 0x20 {
                    out.append(String(format: "\\u%04x", scalar.value))
                } else {
                    out.unicodeScalars.append(scalar)
                }
            }
        }
        out.append("\"")
    }
}
