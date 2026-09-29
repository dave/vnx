package vnx

import "fmt"

// Severity ranks a Diagnostic. It is deliberately small: the format's reading
// rules (spec §6.2) have exactly two outcomes short of refusal — something was
// skipped, or something is worth knowing.
type Severity int

const (
	// Info records something a caller may want to know that cost nothing.
	Info Severity = iota
	// Warning records that an element or key was DROPPED. The document still
	// read, but it does not carry everything the file did.
	Warning
)

func (s Severity) String() string {
	switch s {
	case Info:
		return "info"
	case Warning:
		return "warning"
	}
	return fmt.Sprintf("Severity(%d)", int(s))
}

// Diagnostic is one thing a reader or a writer noticed. Path is a
// JSON-pointer-style location into the document ("/networks/0/arcs/arc-1"), so
// a caller — very often an agent — can go straight to the offending place
// rather than hunting for it. Message is short, English, and technical; it is
// not a user-facing string, and translating it is the bridge's job.
//
// ⚠ Code IS OPTIONAL. It is set on every decode diagnostic SPEC §6.2's remedy
// table names (see decodeCodes), and EMPTY on the decode sites that report a
// shape the table does not name, which go to `vnx validate` and to logs.
// WRITER diagnostics — the ones the GPX, KML and GeoJSON writers raise on the
// EXPORT path, which a user does see — always set it. A writer Code with no mapping in the bridge is a
// test failure, not a silence: see WriterCodes below.
type Diagnostic struct {
	Severity Severity
	// Code is a short, stable, English-free token identifying WHICH
	// diagnostic this is ("gpx.area-unsupported"). It is NOT a translation
	// key: this package is stdlib-only and knows nothing of translation. It is
	// the handle the bridge maps to a key, and the handle the public CLI
	// prints so an agent can match a diagnostic without parsing prose.
	Code string
	Path string
	// Args are the interpolation values for the translation key the bridge
	// maps Code to, positionally matched to the `params:` that key declares.
	//
	// ⚠ THEY LIVE ON THE DIAGNOSTIC BECAUSE THE DIAGNOSTIC SLICE IS THE ONLY
	// CHANNEL A WRITER HAS (spec 5b T3). vnxbridge.writerDiagnostic needs a
	// name to put in "GPX cannot hold areas, so X was not exported", and
	// pulling it back out of Message would be mapping on free English — the
	// one thing Code exists to stop. Like Code, they are EMPTY at every decode
	// site: those diagnostics have never reached a translated surface.
	//
	// ⚠ THEY ARE VALUES, NEVER SENTENCES. pkg/vnx is stdlib-only and knows
	// nothing of translation; an English fragment here would land untranslated
	// in the middle of a French string. Args[0] is the item's own Name — see
	// the conventions in writer.go.
	Args []string
	// Message is short, English and technical. It is deliberately NOT the
	// thing a caller maps to a key — free English cannot be mapped back — and
	// that is why Code exists.
	Message string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s %s: %s", d.Severity, d.Path, d.Message)
}

// writerCodes is every Code the writer packages can emit, in declaration
// order. It is filled by the writerCode declarations below.
//
// ⚠ IT IS THE REGISTRY THE BRIDGE'S MAPPING GUARD READS, and that direction is
// the whole point. A guard that enumerated the bridge's mapping TABLE and
// checked the table would pass vacuously — it would prove only that the
// entries somebody wrote are the entries somebody wrote. Enumerating from HERE
// means a writer that mints a Code and forgets the mapping reddens the suite,
// which is what stops an unmapped Code becoming a user-visible silence.
var writerCodes []string

// writerCode declares one stable writer Code and registers it.
//
// ⚠ THE DECLARATION IS THE REGISTRATION, the same shape vnxbridge's diag()
// uses for its templates and for the same reason: there is no code STRING at a
// writer's call site, only the identifier declared here, so a Code cannot be
// spelled at a site without existing in the registry, and deleting one stops
// every site using it from compiling.
func writerCode(s string) string {
	writerCodes = append(writerCodes, s)
	return s
}

// The writer Codes.
//
// ⚠ THEY ARE DECLARED HERE, IN pkg/vnx, NOT IN THE WRITER SUBPACKAGES, and
// that is what makes the guard possible. `writerCode` is unexported, so
// pkg/vnx/gpx cannot register a Code of its own; it names
// `vnx.CodeGPXAreaUnsupported` at its raise site. One list, and it is the list
// the bridge's mapping guard enumerates.
//
// ⚠ THE WHOLE SET IS DECLARED HERE IN ONE TASK (spec 5b T3), BEFORE ANY
// WRITER EXISTS, and that is a change of scope from the plan rather than an
// accident. T4, T5 and T6 are three agents working in parallel in ONE shared
// worktree; every Code lives in THIS file, so a writer that minted its own
// would be three agents editing one file at once. They reference; they do not
// declare.
//
// ⚠ THE PREFIX NAMES THE SCOPE OF THE RULE, NOT THE WRITER THAT HAPPENS TO
// RAISE IT. A format prefix — "gpx." — means only that format drops the
// thing. A noun prefix — "app-key.", "folder.", "item." — means every format
// does, and one Code there is one catalogue key and one English sentence
// instead of three identical ones under three names. The user already knows
// which format they asked for; the Code does not have to tell them again.
var (
	// CodeGPXNetworkUnsupported: GPX excludes networks entirely (§2.6). It is
	// a deliberate deferral with a named future, not an oversight — Dave,
	// 2026-09-02: "in future I might have some options for how you export
	// networks into GPX files, but for now, let's just exclude them
	// completely." Do NOT invent a flattening for GPX to be helpful.
	CodeGPXNetworkUnsupported = writerCode("gpx.network-unsupported")
	// CodeGPXAreaUnsupported: GPX has no polygon (§3.1).
	CodeGPXAreaUnsupported = writerCode("gpx.area-unsupported")
	// CodeAppKeyUnsupported: no target format can carry an application key
	// (SPEC §3.9), so all three raise this one, once per key, through
	// [AppKeyDiagnostics]. Args[0] is the key itself, verbatim: it is the
	// application key's only name, and it is what a caller maps to a kind of
	// content ("app.viatrail.rulesets" is style rules). The code is generic on
	// purpose — this package knows no application's keys.
	CodeAppKeyUnsupported = writerCode("app-key.unsupported")
	// CodeEmptyFolderDropped: an EMPTY folder is a casualty in the flattened
	// formats; a non-empty one is NOT (§2.7). If every item the folder held
	// carries the folder's name in its own, nothing was lost and a diagnostic
	// would be noise on the common case. A folder with no items anywhere
	// beneath it has nowhere to put its name and genuinely vanishes.
	//
	// KML does not raise this: it has real nested <Folder> elements and keeps
	// an empty folder as an empty folder.
	CodeEmptyFolderDropped = writerCode("folder.empty-dropped")
	// CodeFolderPathUnavailable: [FolderPath] could not build a path — the
	// item names a folder that is not in this document, or the parent chain
	// forms a cycle. The item is KEPT, under its bare name; losing a position
	// in a tree these formats cannot express is not worth dropping content
	// over, and it is not worth hiding either.
	CodeFolderPathUnavailable = writerCode("folder.path-unavailable")
	// CodeItemNoGeometry: the item carries too little geometry for any format
	// to place it — no pips at all, or a ring of fewer than four positions.
	//
	// ⚠ IT IS REACHABLE, and not only from a hand-written file. Decode drops
	// an ARC with fewer than two pips but does NOT drop a route with no arcs,
	// a track with no segments or a short area ring, so `vnx convert` on a
	// third-party .vnx reaches a writer with one. Emitting the item anyway
	// would produce a GeoJSON LineString with no positions, which RFC 7946
	// forbids — an invalid file written as a success. Declared here rather
	// than left to each writer precisely because all three meet it and none
	// of them meets it in the golden fixture.
	CodeItemNoGeometry = writerCode("item.no-geometry")

	// CodePartialGeometry is raised when an item IS exported but part of it
	// could not be placed — one degenerate segment of a multi-segment track, a
	// network arc under two pips, a highlight link naming an arc the network does
	// not hold.
	//
	// ⚠ IT EXISTS BECAUSE CodeItemNoGeometry'S ENGLISH IS WRONG FOR THIS CASE.
	// That one says "dropped", which is a false statement about an item the
	// reader can see in the file. Added 2026-09-02 after the GeoJSON writer
	// raised the whole-item code for partial losses and said so, and after the
	// KML writer was found dropping a degenerate segment SILENTLY — two
	// writers, one document, two different disclosures, neither right. The
	// silent one is the house rule's forbidden fallback; the loud one lied.
	//
	// ⚠ ALL WRITERS THAT CAN LOSE PART OF AN ITEM MUST RAISE THIS. Path names
	// the part (/tracks/0/segments/1); Args[0] is the ITEM's own name, per
	// writer.go's convention 3.
	CodePartialGeometry = writerCode("item.partial-geometry")
)

// WriterCodes is every Code the writer packages can emit.
//
// It returns a copy: a caller that appended to the shared slice would be
// registering a Code that no writer raises and no mapping needs.
func WriterCodes() []string {
	out := make([]string, len(writerCodes))
	copy(out, writerCodes)
	return out
}

// decodeCodes is every Code a DECODE diagnostic can carry, in declaration order.
//
// It is ONE CODE PER ROW OF SPEC §6.2's REMEDY TABLE, and no more. Every
// problem that table names is a coded diagnostic with a local remedy, never a
// refusal, so a caller — Viatrail's importer among them — can tell a user what
// was dropped without parsing English. The remaining uncoded decode sites
// report shapes the table does not name (an item that is not an object, a
// collection that is not an array) to `vnx validate` and to logs.
//
// A code here with no corpus case, or a corpus case naming a code that is not
// here, is a test failure — see TestEveryDecodeCodeIsPinnedByTheCorpus.
var decodeCodes []string

func decodeCode(s string) string {
	decodeCodes = append(decodeCodes, s)
	return s
}

// The decode Codes the conformance corpus pins.
var (
	// CodeUnknownKey: an unrecognised key on an object this format defines
	// (§6.2). The key is skipped and the item kept.
	CodeUnknownKey = decodeCode("unknown-key")
	// CodePolygonNestedRings: a polygon carrying more than one ring (§3.7).
	// The item is dropped.
	CodePolygonNestedRings = decodeCode("polygon-nested-rings")
	// CodeTimestampNotUTC: a record-level timestamp with an offset other than
	// `Z` (§4.3). The timestamp is dropped and the item kept.
	CodeTimestampNotUTC = decodeCode("timestamp-not-utc")
	// CodeWrongTypeOptional: a value of the wrong JSON type in an OPTIONAL key
	// (§6.2). The field is dropped and the item is kept.
	CodeWrongTypeOptional = decodeCode("wrong-type-optional")
	// CodeWrongTypeRequired: a value of the wrong JSON type in a REQUIRED key
	// (§6.2). The ITEM is dropped.
	//
	// ⚠ THE TWO ARE SEPARATE CODES BECAUSE THE OUTCOMES DIFFER FOR WHOEVER
	// HOLDS THE FILE, not because the remedies differ in this package. "A field
	// is gone" is recoverable from the rest of the item and "the item is gone"
	// is not, so a caller has to tell them apart without parsing English.
	CodeWrongTypeRequired = decodeCode("wrong-type-required")
	// CodePositionUnusable: a position inside an array will not parse (§4.1).
	// The object owning the array is dropped, and WHICH object that is depends
	// on the container — §6.2's table has a row for each. One code rather than
	// three, because the code plus that table already tells a consumer what was
	// lost, which leaves `path` free to name the cause.
	CodePositionUnusable = decodeCode("position-unusable")
	// CodeExtraChannel: a time on a node, or an elevation or a time on a
	// walk's `from`, `to` or `turn` (§3.3.1, §3.3.3). The channel is dropped
	// and the node or pass kept. Its path is the node or the pass key.
	CodeExtraChannel = decodeCode("extra-channel")
	// CodeArcPipTime: a time on an arc pip (§3.3.2). Every time channel on the
	// arc is dropped and its pips kept, and the diagnostic is reported ONCE,
	// at the arc's `pips`, however many pips carried one.
	CodeArcPipTime = decodeCode("arc-pip-time")
	// CodeSelfLoopArc: an arc whose `from` and `to` name one node (§3.3.2).
	// The arc is dropped, and every highlight walking it with it.
	CodeSelfLoopArc = decodeCode("self-loop-arc")
	// CodeArcNodeAbsent: an arc naming a node its owner does not have
	// (§3.3.2). The arc is dropped, and every highlight walking it with it.
	CodeArcNodeAbsent = decodeCode("arc-node-absent")
	// CodePassArcAbsent: a pass that is not an object or names no arc of its
	// owner (§3.3.3), including an arc another rule dropped. The highlight — or
	// the route, which is its own highlight — is dropped.
	CodePassArcAbsent = decodeCode("pass-arc-absent")
	// CodeWalkSpelling: a walk that breaks §3.3.3's rule for where `from` and
	// `to` appear. The highlight is dropped.
	CodeWalkSpelling = decodeCode("walk-spelling")
	// CodeWalkNotConnected: a node `from` or `to` that is not an end of its
	// arc, a pass not entered at an end of its arc, or a pass other than the
	// last leaving at a position (§3.3.3). The highlight is dropped.
	CodeWalkNotConnected = decodeCode("walk-not-connected")
	// CodePinnedTurn: a pass carrying both `turn` and `pinned` (§3.3.3). The
	// highlight is dropped.
	CodePinnedTurn = decodeCode("pinned-turn")
	// CodeRoutePinOrPosition: a position, a `turn` or `pinned` in a route's
	// walk (§3.4). The route is dropped.
	CodeRoutePinOrPosition = decodeCode("route-pin-or-position")
	// CodeRingClosed: a ring whose last position repeats its first (§3.7). The
	// repeated position is dropped and the area kept: nothing is lost, and the
	// diagnostic is how a writer that closes rings is seen.
	CodeRingClosed = decodeCode("ring-closed")
	// CodeRingTooFewPoints: a ring with fewer than three distinct positions
	// (§3.7). The area is dropped.
	CodeRingTooFewPoints = decodeCode("ring-too-few-points")
	// CodeAttachmentUnreadable: a VNZ attachment this reader cannot recover
	// (§7) — either compressed by a method that is neither stored nor deflate,
	// or carried in a stored/deflate entry whose bytes will not come back (a
	// stream that will not inflate, a CRC that disagrees). The attachment is
	// dropped and the document is returned.
	//
	// ⚠ ONE CODE FOR BOTH, which is §7's own ruling rather than a shortcut. The
	// remedy is identical and what a caller can DO about it is identical: the
	// attachment is not there. Splitting the code would make six
	// implementations agree on a distinction the format does not draw.
	//
	// ⚠ ITS PATH IS NOT A POINTER INTO THE DOCUMENT. An archive entry is not part
	// of the document, so this is the one code whose `path` lives in §6.3's
	// separate `#/entries/<name>` space, with the entry name escaped per RFC 6901.
	// It is also the only code here about the CONTAINER rather than about the JSON
	// inside it.
	CodeAttachmentUnreadable = decodeCode("attachment-unreadable")
)
