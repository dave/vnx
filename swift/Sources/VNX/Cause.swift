import Foundation

/// The machine-readable cause of a refusal or a diagnostic.
///
/// Every refusal and every diagnostic this library produces carries one, beside
/// its English text, so that a caller can branch on what went wrong without
/// grepping prose.
///
/// SPEC 6.3 now gives a diagnostic a `code` and says the English MUST NOT be
/// compared. `Cause.code` produces that token, and `Cause.corpusCode` holds the
/// codes of SPEC 6.2's remedy table, every one of which the corpus pins.
///
/// A refusal has no code in the format yet, so the corpus still matches
/// `refusal` as an English substring — the corpus README calls that the
/// remaining place it compares English across implementations, and a known
/// defect. `Cause.corpusSubstring` therefore maps a cause to the substring the
/// corpus expects (for a refusal) or advises (`message_advisory`, for a
/// diagnostic), and `CauseTableTests` asserts it in both directions so it
/// cannot go stale on either side.
public enum Cause: String, Sendable, CaseIterable {

  // MARK: Refusals (SPEC 6.2, SPEC 7)

  /// The bytes are not JSON at all. `NaN` and `Infinity` arrive here: they
  /// are not JSON tokens, so a conforming parser never reaches the document.
  case malformedJSON
  /// The top level is not a JSON object, or carries no `vnx` key.
  case notAVNXDocument
  /// `vnx` is not three digit-runs separated by full stops.
  case versionMalformed
  /// `vnx` names a series this reader does not implement.
  ///
  /// NOT "too new", in either half of the rule, and the name says so. SPEC
  /// 2.2 selects the regime by the DOCUMENT's MAJOR, so a major either side
  /// of the reader's refuses; and through 0.x each MINOR is its own series,
  /// where "a lower minor is refused for the same reason as a higher one".
  /// One cause, because it is one reason — which direction it was is in the
  /// English, and only there.
  case versionUnsupportedSeries
  /// A number in the document is not finite. Unreachable through a `NaN`
  /// literal, which the parser refuses first, but reachable through a
  /// well-formed literal that overflows binary64, such as `1e400`.
  case nonFiniteNumber
  /// The container is not a readable ZIP archive.
  case containerMalformed
  /// An entry name is absolute, carries a drive letter, or has a `..`
  /// segment (SPEC 7). A security requirement, not a tidiness one.
  case containerPathEscapes
  /// Two entries share a name, compared case-insensitively (SPEC 7).
  case containerDuplicateEntry
  /// The archive carries no `.vnx` entry, so there is no document to return.
  case containerNoDocument
  /// The root document uses a compression method other than stored or
  /// deflate, so it cannot be read (SPEC 7).
  case containerRootCompressionUnsupported

  // MARK: Diagnostics (SPEC 6.2, SPEC 6.3)

  /// An unrecognised key on an object this format defines (SPEC 3.10).
  case unknownKey
  /// A value of the wrong JSON type under a REQUIRED key, or a REQUIRED key
  /// that is absent, or a data record with no usable `@type`. The OBJECT
  /// goes, because it cannot exist without the key.
  ///
  /// One cause for the absent key and the wrong-typed one, because SPEC 6.2
  /// gives them one row and one code: the outcome for whoever holds the file
  /// is the same. Which of the two happened is in the English.
  case wrongTypeRequired
  /// A value of the wrong JSON type under an OPTIONAL key. The FIELD goes and
  /// the item stays.
  ///
  /// The distinction from `wrongTypeRequired` is the whole point of there
  /// being two codes: "`a field is gone` and `the item is gone` differ in
  /// whether the rest of the item survives, and a consumer told only that a
  /// type was wrong has to re-derive required-ness from section 3.10's table
  /// to find out which happened, which is the lookup a code exists to save."
  case wrongTypeOptional
  /// A position that will not parse. SPEC 6.2's table says which object
  /// goes: the node, the arc, the segment, the area, or the highlight.
  case positionUnusable
  /// A record-level timestamp carrying an offset other than `Z` (SPEC 4.3).
  case timestampNotUTC
  /// A `polygon` carrying a nested or additional ring (SPEC 3.7).
  case polygonNestedRings
  /// A ring whose last position repeats its first (SPEC 3.7). The repeat is
  /// dropped and the area kept.
  case ringClosed
  /// A ring with fewer than three distinct positions (SPEC 3.7). The area
  /// goes.
  case ringTooFewPoints
  /// A time on a node, or an elevation or a time on a walk's `from`, `to` or
  /// `turn` (SPEC 3.3.1, 3.3.3). The channel goes and the node or pass stays.
  case extraChannel
  /// A time on an arc pip (SPEC 3.3.2). Every time on the arc goes, the pips
  /// stay, and the diagnostic is reported ONCE, at the arc's `pips`.
  case arcPipTime
  /// An arc whose `from` and `to` name one node (SPEC 3.3.2). The arc goes.
  case selfLoopArc
  /// An arc naming a node its network or plan does not have (SPEC 3.3.2).
  /// The arc goes.
  case arcNodeAbsent
  /// A pass that is not an object, or that names no arc of its network or
  /// plan — including an arc another rule dropped (SPEC 3.3.3). The highlight
  /// goes; in a plan, the plan.
  case passArcAbsent
  /// A walk that breaks SPEC 3.3.3's rule for where `from` and `to` appear.
  case walkSpelling
  /// A walk that is not connected: valid-walk rules 3, 4 and 5 of SPEC 3.3.3.
  case walkNotConnected
  /// A pass carrying both `turn` and `pin` (SPEC 3.3.3).
  case pinnedTurn
  /// A position, a `turn` or a `pin` in a plan's walk (SPEC 3.4). The plan
  /// goes.
  case planPinOrPosition
  /// An item's `folder`, or a folder's `parent`, naming a folder that is not
  /// in the document. The key is treated as absent: the item sits at the top
  /// level, the folder at the root (SPEC 6.2).
  case folderNotPresent
  /// A folder whose `id` an earlier folder in the document already carries
  /// (SPEC 3.2). The later folder goes; items naming the id sit in the first.
  /// The path is the dropped folder's `id`, at its index in the file.
  case folderDuplicateID
  /// The first folder, in document order, of a parent loop (SPEC 3.2). Its
  /// `parent` is treated as absent, so it sits at the root; that breaks the
  /// loop, and the other folders of the loop keep their parents. A folder
  /// whose parents only lead into a loop is not reported.
  case folderParentLoop
  /// An attachment that cannot be recovered. The attachment goes and the
  /// document is returned (SPEC 7).
  ///
  /// The one cause here whose `path` is not a pointer into the document: an
  /// archive entry is not part of the document, so it lives in SPEC 6.3's
  /// separate `#/entries/<name>` space.
  case attachmentUnreadable

  /// A stable kebab-case reason code, safe to switch on and safe to log.
  ///
  /// For every cause SPEC 6.2's remedy table names, this is that table's
  /// code, so the two vocabularies cannot drift. For the rest it is this
  /// library's own.
  public var code: String {
    if let corpus = Cause.corpusCode[self] { return corpus }
    return Cause.ownCode[self] ?? rawValue
  }

  /// The code SPEC 6.2's remedy table gives each cause. The conformance
  /// corpus pins every one of them, and `CauseTableTests` asserts that the
  /// two sets are equal.
  public static let corpusCode: [Cause: String] = [
    .unknownKey: "unknown-key",
    .wrongTypeOptional: "wrong-type-optional",
    .wrongTypeRequired: "wrong-type-required",
    .timestampNotUTC: "timestamp-not-utc",
    .positionUnusable: "position-unusable",
    .extraChannel: "extra-channel",
    .arcPipTime: "arc-pip-time",
    .selfLoopArc: "self-loop-arc",
    .arcNodeAbsent: "arc-node-absent",
    .passArcAbsent: "pass-arc-absent",
    .walkSpelling: "walk-spelling",
    .walkNotConnected: "walk-not-connected",
    .pinnedTurn: "pinned-turn",
    .planPinOrPosition: "plan-pin-or-position",
    .polygonNestedRings: "polygon-nested-rings",
    .ringClosed: "ring-closed",
    .ringTooFewPoints: "ring-too-few-points",
    .attachmentUnreadable: "attachment-unreadable",
    .folderNotPresent: "folder-absent",
    .folderDuplicateID: "folder-duplicate-id",
    .folderParentLoop: "folder-parent-loop",
  ]

  /// Codes for the causes SPEC 6.2's table does not name. Kept separate from
  /// `corpusCode` so that adopting a table spelling is a visible move from
  /// one table to the other.
  static let ownCode: [Cause: String] = [
    .malformedJSON: "malformed-json",
    .notAVNXDocument: "not-a-vnx-document",
    .versionMalformed: "version-malformed",
    .versionUnsupportedSeries: "version-unsupported-series",
    .nonFiniteNumber: "non-finite-number",
    .containerMalformed: "container-malformed",
    .containerPathEscapes: "container-path-escapes",
    .containerDuplicateEntry: "container-duplicate-entry",
    .containerNoDocument: "container-no-document",
    .containerRootCompressionUnsupported: "container-root-compression-unsupported",
  ]

  /// The English substrings the corpus expects for this cause today, for
  /// REFUSALS ONLY.
  ///
  /// Diagnostics used to be here too, matched on `message`. They are not any
  /// more, and the removal is the point rather than a tidy-up: SPEC 6.3 gives a
  /// pinned diagnostic a `code`, makes the code what a case compares, and says
  /// the English MUST NOT be compared — which is why the corpus field is now
  /// named `message_advisory`. Keeping those six entries would have rebuilt the
  /// coupling the spec had just removed, one layer down, and would have made
  /// this library assert that its own diagnostic prose matches another
  /// implementation's. `Cause.corpusCode` covers diagnostics.
  ///
  /// A refusal has no code in the format yet, so this is the surface where the
  /// corpus still compares English across implementations — its README calls
  /// that a known defect, and typed refusal codes are the fix.
  ///
  /// Only the causes the corpus actually exercises appear here. A cause with
  /// no entry is one no corpus case reaches, and `CauseTableTests` proves
  /// that claim by running the corpus and comparing the set of causes it
  /// observed against this table's keys.
  ///
  /// One entry is a wording this library chose to match rather than a fact
  /// about the format: `malformedJSON: "malformed JSON"`. The corpus README
  /// names it as a Go artefact — a strict parser refuses `NaN` before any
  /// format rule runs, a permissive one reaches SPEC 4.2 and refuses for the
  /// right reason in different words, and only the first wording passes. This
  /// library takes the strict-parser route and distinguishes the two causes
  /// itself, so a caller can tell them apart even where the corpus cannot.
  /// A SET per cause, not one string, and `versionUnsupportedSeries` is why:
  /// the corpus refuses `9.9.9` and `0.1.9` for the one reason and pins a
  /// different substring for each, because the English a holder needs differs
  /// — one of them should go and find a newer reader and the other should
  /// not. A cause with a single entry is asserted exactly as strictly as
  /// before.
  public static let corpusSubstring: [Cause: Set<String>] = [
    .malformedJSON: ["malformed JSON"],
    .notAVNXDocument: ["not a VNX document"],
    .versionMalformed: ["MAJOR.MINOR.PATCH"],
    .versionUnsupportedSeries: ["a newer reader is needed", "0.1 series"],
    .containerPathEscapes: ["escapes the archive"],
    .containerDuplicateEntry: ["duplicate entry"],
  ]
}

/// One thing a reader could not use as written (SPEC 6.3).
public struct Diagnostic: Equatable, Sendable {
  /// A JSON Pointer (RFC 6901) locating the element this is about.
  public let path: String
  /// A short description in English.
  public let message: String
  /// The same fact, machine-readable. Not part of SPEC 6.3's two fields.
  public let cause: Cause
  /// The stable reason code, in the corpus's vocabulary where it has one.
  public var code: String { cause.code }

  public init(path: String, message: String, cause: Cause) {
    self.path = path
    self.message = message
    self.cause = cause
  }
}

/// A document a reader refused (SPEC 6.2). Reported through a separate channel
/// from diagnostics, so a caller can tell a refused document from one that was
/// read with elements dropped.
public struct Refusal: Error, Equatable, Sendable {
  public let message: String
  public let cause: Cause

  public init(_ cause: Cause, _ message: String) {
    self.cause = cause
    self.message = message
  }
}

extension Refusal: CustomStringConvertible {
  public var description: String { message }
}
