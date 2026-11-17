import Foundation

/// SPEC 3.10, verbatim. This is the set of keys the format defines, and SPEC
/// 3.10 says outright that SPEC 6.2's unrecognised-key rule "cannot be
/// implemented without it".
///
/// Two objects are deliberately absent, and their absence is the rule: a data
/// record body and the value of an application key are opaque, so a reader
/// must not apply the unrecognised-key rule inside either.
///
/// The document's key set is open: every top-level key containing a full stop
/// is an application key (SPEC 3.9), so the document is checked with
/// `isDocumentKey` rather than with a `Shape`. Every other object keeps its
/// fixed shape, so a dotted key on an item is still unrecognised.
enum Keys {
  struct Shape {
    let required: Set<JSONKey>
    let optional: Set<JSONKey>
    var all: Set<JSONKey> { required.union(optional) }
  }

  static let document = Shape(
    required: ["vnx"],
    optional: [
      "folders", "networks", "plans", "tracks", "points", "areas",
    ])

  /// SPEC 3.9: whether a top-level key is an application key. No key the
  /// format defines contains a full stop.
  ///
  /// ⚠ ON UNICODE SCALARS, NOT `Character`s. `String.contains(".")` asks
  /// for a grapheme, and a full stop followed by a combining mark is one
  /// grapheme that is not `"."`, so it would miss `"a.\u{301}b"`. SPEC 3.9's
  /// test is U+002E anywhere in the name.
  static func isApplicationKey(_ key: String) -> Bool {
    key.unicodeScalars.contains(".")
  }

  /// Whether a top-level key is recognised: one the format defines, or an
  /// application key. Top level only.
  static func isDocumentKey(_ key: String) -> Bool {
    document.all.contains(JSONKey(key)) || isApplicationKey(key)
  }

  static let folder = Shape(
    required: ["id"],
    optional: ["parent", "name", "hidden"])

  static let network = Shape(
    required: [],
    optional: [
      "name", "folder", "hidden", "pinned", "nodes", "arcs",
      "highlights", "points", "areas", "data",
    ])

  /// An arc, in a network or in a plan: one shape for both (SPEC 3.10).
  static let arc = Shape(
    required: ["from", "to", "pips"],
    optional: ["data"])

  /// A highlight has an optional `name`.
  static let highlight = Shape(
    required: ["walk"],
    optional: ["name", "hidden", "pinned", "data"])

  static let pass = Shape(
    required: ["arc"],
    optional: ["from", "to", "turn", "pin"])

  static let plan = Shape(
    required: ["walk"],
    optional: ["name", "folder", "hidden", "pinned", "nodes", "arcs", "data"])

  static let track = Shape(
    required: ["segments"],
    optional: [
      "name", "folder", "hidden", "pinned", "started",
      "ended", "data",
    ])

  static let segment = Shape(
    required: ["pips"],
    optional: [])

  /// A top-level point. It places itself with `folder`.
  static let topLevelPoint = Shape(
    required: ["at"],
    optional: ["name", "folder", "hidden", "pinned", "data"])

  /// A network's point. No `folder`: its network already contains it.
  static let networkPoint = Shape(
    required: ["at"],
    optional: ["name", "hidden", "pinned", "data"])

  static let topLevelArea = Shape(
    required: ["polygon"],
    optional: ["name", "folder", "hidden", "pinned", "data"])

  static let networkArea = Shape(
    required: ["polygon"],
    optional: ["name", "hidden", "pinned", "data"])
}
