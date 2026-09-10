import Foundation

/// The machine-readable cause of a refusal or a diagnostic.
///
/// Every refusal and every diagnostic this library produces carries one, beside
/// its English text, so that a caller can branch on what went wrong without
/// grepping prose.
///
/// SPEC 6.3 now gives a diagnostic a `code` and says the English MUST NOT be
/// compared. `Cause.code` produces that token, and `Cause.corpusCode` holds the
/// six the corpus pins today.
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
    /// `vnx` names a MAJOR or MINOR above the reader's.
    case versionTooNew
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
    /// A value of the wrong JSON type under a REQUIRED key. The ITEM goes,
    /// because it cannot exist without the key.
    ///
    /// Separate from `requiredKeyMissing`, and the corpus note for
    /// `diagnose/wrong-typed-value` says why: "a missing required key and a
    /// wrong-typed required key take the same remedy but are not the same fact".
    /// A reader that reports one code for both makes a consumer guess which
    /// happened.
    case wrongTypeRequired
    /// A REQUIRED key is absent. Same remedy, different fact.
    case requiredKeyMissing
    /// A value of the wrong JSON type under an OPTIONAL key. The FIELD goes and
    /// the item stays.
    ///
    /// The distinction from `wrongTypeRequired` is the whole point of there
    /// being two codes: "`a field is gone` and `the item is gone` differ in
    /// whether the rest of the item survives, and a consumer told only that a
    /// type was wrong has to re-derive required-ness from section 3.10's table
    /// to find out which happened, which is the lookup a code exists to save."
    case wrongTypeOptional
    /// A position that will not parse.
    case positionUnusable
    /// `importance` outside 1 to 5 (SPEC 3.6), the one normative range this
    /// format states for a style key.
    case importanceOutOfRange
    /// A record-level timestamp carrying an offset other than `Z` (SPEC 4.3).
    case timestampNotUTC
    /// A data record with no `@type`, or one that is not a string (SPEC 5).
    case dataRecordTypeUnusable
    /// A `polygon` carrying a nested or additional ring (SPEC 3.7).
    case polygonNestedRings
    /// A `polygon` that does not close, or holds fewer than four positions.
    case polygonUnclosed
    /// A link naming an arc that is not in its network (SPEC 6.2).
    case linkArcNotInNetwork
    /// A link endpoint naming a node that is not in its network.
    case linkNodeNotInNetwork
    /// An arc naming a node that is not in its network. The arc is kept: its
    /// geometry is real (SPEC 6.2's remedy table).
    case arcNodeNotInNetwork
    /// An inline link endpoint further from its arc than the 50 m tolerance.
    case inlineEndpointBeyondTolerance
    /// An inline link endpoint on an arc with no vertices, which has no
    /// distance to measure (SPEC 3.3.3).
    case inlineEndpointOnEmptyArc
    /// Consecutive links in a chain that do not meet (SPEC 6.2).
    case chainNotConnected
    /// A `folder` or `parent` naming a folder that is not in the document.
    case folderNotPresent
    /// An attachment compressed by a method other than stored or deflate. The
    /// attachment goes and the document is returned (SPEC 7).
    ///
    /// The one cause here whose `path` is not a pointer into the document: an
    /// archive entry is not part of the document, so it lives in SPEC 6.3's
    /// separate `#/entries/<name>` space.
    case attachmentUnreadable
    /// An arc terminal pip further from the node that end names than SPEC
    /// 3.3.3's tolerance allows (SPEC 3.3.2). Both are kept: the pips are
    /// authoritative for geometry, and the node position and the terminal pip
    /// are one fact written twice.
    case terminalPipOffNode

    /// A stable kebab-case reason code, safe to switch on and safe to log.
    ///
    /// For the six causes the corpus names a `code` for, this is exactly the
    /// corpus's spelling, so the two vocabularies cannot drift. For the rest it
    /// is this library's own, because no other vocabulary exists yet.
    public var code: String {
        if let corpus = Cause.corpusCode[self] { return corpus }
        return Cause.ownCode[self] ?? rawValue
    }

    /// The `code` the conformance corpus expects for this cause.
    ///
    /// The corpus gained reason codes on 2026-09-09, in the working tree ahead
    /// of SPEC 6.3, which still lists a diagnostic's fields as `path` and
    /// `message` alone. The runner reads whichever shape a sidecar carries.
    public static let corpusCode: [Cause: String] = [
        .unknownKey: "unknown-key",
        .wrongTypeOptional: "wrong-type-optional",
        .wrongTypeRequired: "wrong-type-required",
        .linkArcNotInNetwork: "link-arc-absent",
        .chainNotConnected: "chain-discontinuity",
        .polygonNestedRings: "polygon-nested-rings",
        .timestampNotUTC: "timestamp-not-utc",
        .inlineEndpointBeyondTolerance: "endpoint-off-arc",
        .positionUnusable: "position-unusable",
        .terminalPipOffNode: "terminal-pip-off-node",
        .attachmentUnreadable: "attachment-unreadable",
    ]

    /// Codes for the causes the corpus has no vocabulary for yet. Kept
    /// separate from `corpusCode` so that adopting a corpus spelling is a
    /// visible move from one table to the other.
    static let ownCode: [Cause: String] = [
        .malformedJSON: "malformed-json",
        .notAVNXDocument: "not-a-vnx-document",
        .versionMalformed: "version-malformed",
        .versionTooNew: "version-too-new",
        .nonFiniteNumber: "non-finite-number",
        .containerMalformed: "container-malformed",
        .containerPathEscapes: "container-path-escapes",
        .containerDuplicateEntry: "container-duplicate-entry",
        .containerNoDocument: "container-no-document",
        .containerRootCompressionUnsupported: "container-root-compression-unsupported",
        .requiredKeyMissing: "missing-required-key",
        .importanceOutOfRange: "importance-out-of-range",
        .dataRecordTypeUnusable: "data-record-type-unusable",
        .polygonUnclosed: "polygon-unclosed",
        .linkNodeNotInNetwork: "link-node-absent",
        .arcNodeNotInNetwork: "arc-node-absent",
        .inlineEndpointOnEmptyArc: "endpoint-on-empty-arc",
        .folderNotPresent: "folder-absent",
    ]

    /// The English substring the corpus expects for this cause today, for
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
    public static let corpusSubstring: [Cause: String] = [
        .malformedJSON: "malformed JSON",
        .notAVNXDocument: "not a VNX document",
        .versionMalformed: "MAJOR.MINOR.PATCH",
        .versionTooNew: "a newer reader is needed",
        .containerPathEscapes: "escapes the archive",
        .containerDuplicateEntry: "duplicate entry",
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
