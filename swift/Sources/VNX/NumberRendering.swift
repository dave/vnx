import Foundation

/// ECMAScript `Number::toString` for a finite `Double`, which is what SPEC
/// 4.5.3 anchors canonical number text to and what `JSON.stringify` emits.
///
/// Two halves, and only the second is written here.
///
/// The digits come from Swift's `Double.description`, which produces the
/// shortest decimal that round-trips. Swift's layout around those digits is
/// wrong for VNX in three ways — `340.0` for `340`, `1e-06` for `0.000001`,
/// `1e-07` for `1e-7` (SPEC 4.5.3's table says so) — so this function keeps the
/// digits and rewrites the layout.
///
/// The digits themselves have to agree with V8's, including where two shortest
/// spellings are exactly equidistant from the double and the choice is a
/// round-half-to-even tie break. Nothing about that is guaranteed by Swift's
/// documentation, so it is measured rather than assumed: see
/// `NumberRenderingTests.testShortestDigitsBreakTiesToEven`, which pins twenty
/// doubles that are exact ties at their shortest length.
///
/// This tie IS reachable by a corpus case, and `format/number-tie` is that case:
/// it carries one spelling as input and the other as expected output, so it
/// cannot pass for a formatter that echoes its input. No VALUE-level check can
/// see it — both spellings parse to the same double — which is why the accept
/// tier, which re-renders bytes that are already canonical, is blind to it.
///
/// The distinction generalises, and it is worth keeping straight: a value-level
/// property is reachable by a re-render whenever the two readings produce
/// different TEXT for the same value, because a re-render always re-spells. The
/// rounding of SPEC 4.5.4 is NOT reachable that way, because a re-render never
/// rounds — see `Rounding`, whose only guard is a unit test.
func ecmaScriptNumber(_ x: Double) -> String {
    precondition(x.isFinite, "SPEC 4.2 forbids non-finite numbers on the wire")
    // SPEC 4.5.3: negative zero renders as `0`. Two writers disagreeing about
    // the sign is a byte incompatibility carrying no information.
    if x == 0 { return "0" }

    var (negative, digits, pointPosition) = shortestDecimal(x)
    if digits.isEmpty { return "0" }

    // ECMAScript Number::toString, step by step, with `s` = digits (k of them)
    // and `n` = pointPosition, so that x = s * 10^(n-k).
    let k = digits.count
    let n = pointPosition
    var out: String
    if k <= n && n <= 21 {
        out = digits + String(repeating: "0", count: n - k)
    } else if 0 < n && n <= 21 {
        let split = digits.index(digits.startIndex, offsetBy: n)
        out = String(digits[..<split]) + "." + String(digits[split...])
    } else if -6 < n && n <= 0 {
        out = "0." + String(repeating: "0", count: -n) + digits
    } else {
        let e = n - 1
        let head = String(digits.removeFirst())
        let tail = digits.isEmpty ? "" : "." + digits
        out = head + tail + "e" + (e < 0 ? "-" : "+") + String(e.magnitude)
    }
    return negative ? "-" + out : out
}

/// Decomposes `Double.description` into a sign, a digit string with no leading
/// or trailing zeros, and the position of the decimal point relative to the
/// start of those digits.
private func shortestDecimal(_ x: Double) -> (negative: Bool, digits: String, pointPosition: Int) {
    let text = x.description
    var s = Substring(text)
    var negative = false
    if s.hasPrefix("-") { negative = true; s = s.dropFirst() }

    var mantissa = s
    var exponent = 0
    if let e = s.firstIndex(where: { $0 == "e" || $0 == "E" }) {
        mantissa = s[s.startIndex..<e]
        exponent = Int(s[s.index(after: e)...]) ?? 0
    }

    var digits: String
    var pointPosition: Int
    if let dot = mantissa.firstIndex(of: ".") {
        let integerPart = mantissa[mantissa.startIndex..<dot]
        let fractionPart = mantissa[mantissa.index(after: dot)...]
        digits = String(integerPart) + String(fractionPart)
        pointPosition = integerPart.count
    } else {
        digits = String(mantissa)
        pointPosition = mantissa.count
    }
    pointPosition += exponent

    var leading = 0
    for c in digits {
        if c == "0" { leading += 1 } else { break }
    }
    digits.removeFirst(leading)
    pointPosition -= leading
    while digits.hasSuffix("0") { digits.removeLast() }
    return (negative, digits, pointPosition)
}
