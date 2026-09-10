import Foundation

/// Order by Unicode code point.
///
/// Swift's `<` on `String` is not this: it compares by normalised grapheme
/// clusters, so `"e\u{301}"` and `"\u{e9}"` compare equal to it and do not
/// compare equal here, and U+212B ANGSTROM SIGN sorts BELOW U+00C6 for Swift
/// where code point puts it far above.
///
/// Nothing in the format constrains an order any more. This exists so that the
/// two places this library orders strings by itself — the entries of an archive
/// it writes, and the keys it walks when reporting one diagnostic per
/// unrecognised key — are the same from run to run and from machine to machine.
/// A locale-sensitive or normalising comparison is neither.
func codePointLess(_ a: String, _ b: String) -> Bool {
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
