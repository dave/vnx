import Foundation

/// A `MAJOR.MINOR.PATCH` version string (SPEC 2.2).
///
/// Components are compared numerically and have no length bound and no rule
/// against a leading zero, so `00.01.0000` is a legal spelling of 0.1.0. They
/// are therefore held as digit strings and compared as numbers of arbitrary
/// size rather than parsed into `Int`, which would refuse a long component that
/// the format permits.
struct SemanticVersion {
    let major: String
    let minor: String
    let patch: String

    /// Returns nil where the text is not three digit-runs separated by full
    /// stops. `"0.1"`, `"0.1.0.0"`, `"0.1.x"` and `"0.1.+1"` are all nil, and
    /// SPEC 2.2 forbids coercing any of them.
    init?(_ text: String) {
        let parts = text.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count == 3 else { return nil }
        for p in parts {
            guard !p.isEmpty, p.allSatisfy({ $0.isASCII && $0.isNumber }) else { return nil }
        }
        major = String(parts[0])
        minor = String(parts[1])
        patch = String(parts[2])
    }

    /// Numeric comparison of two digit-runs of any length.
    static func compare(_ a: String, _ b: String) -> Int {
        let x = Substring(a).drop(while: { $0 == "0" })
        let y = Substring(b).drop(while: { $0 == "0" })
        if x.count != y.count { return x.count < y.count ? -1 : 1 }
        if x == y { return 0 }
        return x.lexicographicallyPrecedes(y) ? -1 : 1
    }

    var display: String { "\(major).\(minor).\(patch)" }
}
