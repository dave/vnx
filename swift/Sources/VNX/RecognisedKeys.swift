import Foundation

/// SPEC 3.10, verbatim. This is the set of keys the format defines, and SPEC
/// 3.10 says outright that SPEC 6.2's unrecognised-key rule "cannot be
/// implemented without it".
///
/// Two objects are deliberately absent, and their absence is the rule: a data
/// record body and everything under `rulesets` are opaque, and so is a style
/// object, so a reader must not apply the unrecognised-key rule inside any of
/// them.
enum Keys {
    struct Shape {
        let required: Set<String>
        let optional: Set<String>
        var all: Set<String> { required.union(optional) }
    }

    static let document = Shape(
        required: ["vnx"],
        optional: ["generator", "created", "folders", "networks",
                   "routes", "tracks", "waypoints", "areas", "rulesets"])

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
