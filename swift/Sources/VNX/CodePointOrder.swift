import Foundation

/// Order by Unicode code point.
///
/// Swift's `<` on `String` is not this: it compares by normalised grapheme
/// clusters, so `"e\u{301}"` and `"\u{e9}"` compare equal to it and do not
/// compare equal here, and U+212B ANGSTROM SIGN sorts BELOW U+00C6 for Swift
/// where code point puts it far above.
///
/// ⚠ THE DECODER IS WHY THIS FILE EXISTS, not the archive writer. `Decoder`
/// walks an object's keys to report one diagnostic per unrecognised key, and
/// SPEC 6.3 makes the ORDER of diagnostics insignificant but a suite that
/// compares them needs the same list twice — so the walk needs an order that is
/// the same from run to run and from machine to machine, and Swift's `<` is not
/// one: it normalises. Deleting this because the container sort no longer needs
/// it would take the diagnostics with it.
///
/// `Container` also sorts the entries it writes with it. That sort is this
/// library's own choice and not conformance — it was ruled on 2026-09-10 that
/// attachment order is compared nowhere — so the comparator would still be here
/// if the sort went tomorrow.
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
