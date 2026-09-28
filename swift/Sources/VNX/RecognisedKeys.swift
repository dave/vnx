import Foundation

/// SPEC 3.10, verbatim. This is the set of keys the format defines, and SPEC
/// 3.10 says outright that SPEC 6.2's unrecognised-key rule "cannot be
/// implemented without it".
///
/// Two objects are deliberately absent, and their absence is the rule: a data
/// record body and the value of an application key are opaque, and so is a
/// style object, so a reader must not apply the unrecognised-key rule inside
/// any of them.
///
/// The document's key set is open: every top-level key containing a full stop
/// is an application key (SPEC 3.9), so the document is checked with
/// `isDocumentKey` rather than with a `Shape`. Every other object keeps its
/// fixed shape, so a dotted key on an item is still unrecognised.
enum Keys {
    struct Shape {
        let required: Set<String>
        let optional: Set<String>
        var all: Set<String> { required.union(optional) }
    }

    static let document = Shape(
        required: ["vnx"],
        optional: ["generator", "created", "folders", "networks",
                   "routes", "tracks", "waypoints", "areas"])

    /// SPEC 3.9: whether a top-level key is an application key. No key the
    /// format defines contains a full stop.
    static func isApplicationKey(_ key: String) -> Bool {
        key.contains(".")
    }

    /// Whether a top-level key is recognised: one the format defines, or an
    /// application key. Top level only.
    static func isDocumentKey(_ key: String) -> Bool {
        document.all.contains(key) || isApplicationKey(key)
    }

    static let folder = Shape(
        required: ["id"],
        optional: ["parent", "name", "notes", "hidden", "data"])

    static let network = Shape(
        required: [],
        optional: ["name", "notes", "folder", "hidden", "nodes", "arcs",
                   "chains", "points", "areas", "data"])

    static let networkArc = Shape(
        required: ["from", "to", "pips"],
        optional: ["data"])

    static let chain = Shape(
        required: ["links"],
        optional: ["name", "notes", "hidden", "style", "data"])

    static let link = Shape(
        required: ["arc", "from", "to"],
        optional: [])

    static let route = Shape(
        required: ["arcs"],
        optional: ["name", "notes", "folder", "hidden", "style", "data"])

    static let routeArc = Shape(
        required: ["pips"],
        optional: ["data"])

    static let track = Shape(
        required: ["segments"],
        optional: ["name", "notes", "folder", "hidden", "style", "started",
                   "ended", "data"])

    static let segment = Shape(
        required: ["pips"],
        optional: [])

    /// A top-level waypoint. It places itself with `folder`.
    static let waypoint = Shape(
        required: ["at"],
        optional: ["name", "notes", "folder", "hidden", "style", "data"])

    /// A network's point. No `folder`: its network already contains it.
    static let networkPoint = Shape(
        required: ["at"],
        optional: ["name", "notes", "hidden", "style", "data"])

    static let topLevelArea = Shape(
        required: ["polygon"],
        optional: ["name", "notes", "folder", "hidden", "style", "data"])

    static let networkArea = Shape(
        required: ["polygon"],
        optional: ["name", "notes", "hidden", "style", "data"])
}
