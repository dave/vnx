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
// ⚠ Code IS OPTIONAL, and that is a ruling rather than an oversight (spec 5b
// §4.3, 2026-09-02). It is EMPTY when no stable code has been assigned, which
// today is every one of the 60 DECODE sites in this package: those report
// malformed input to `vnx validate` and to logs, they have never reached a
// translated surface, and inventing 60 codes to make a field look full would
// be 60 wire commitments nobody asked for. WRITER diagnostics — the ones spec
// 5b's GPX, KML and GeoJSON writers raise on the EXPORT path, which a user
// does see — always set it. A writer Code with no mapping in the bridge is a
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
// thing. A noun prefix — "ruleset.", "folder.", "item." — means every format
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
	// CodeRulesetUnsupported: no target format can carry a ruleset, so all
	// three raise this one. Its diagnostics pass NO args — Document.Rulesets
	// is []any, carried verbatim and never interpreted, so there is no name to
	// pass.
	CodeRulesetUnsupported = writerCode("ruleset.unsupported")
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
	// network arc under two pips, a chain link naming an arc the network does
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
// ⚠ IT IS DELIBERATELY SHORT, and it stays short. Spec 5b ruled that the sixty
// decode sites in this package do NOT get codes — they report malformed input
// to `vnx validate` and to logs, they have never reached a translated surface,
// and inventing sixty codes to make a field look full would be sixty wire
// commitments nobody asked for. That ruling stands.
//
// What changed on 2026-09-09 is narrower: a conformance corpus case cannot pin
// a diagnostic by its ENGLISH. Five implementations matching substrings of
// free prose is the same defect as the refusal wordings, and worse, because
// there are more of them and no prose behind any. So spec §6.3 now requires a
// code for exactly the diagnostics a corpus case pins, and this is that list.
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
	// CodeLinkArcAbsent: a link naming an arc that is not in its network
	// (§6.2). The link is dropped.
	CodeLinkArcAbsent = decodeCode("link-arc-absent")
	// CodeChainDiscontinuity: consecutive links in a chain that do not meet
	// (§3.3.3). Both links are kept and the break is reported.
	CodeChainDiscontinuity = decodeCode("chain-discontinuity")
	// CodePolygonNestedRings: a polygon carrying more than one ring (§3.7).
	// The item is dropped.
	CodePolygonNestedRings = decodeCode("polygon-nested-rings")
	// CodeTimestampNotUTC: a record-level timestamp with an offset other than
	// `Z` (§4.3). The timestamp is dropped and the item kept.
	CodeTimestampNotUTC = decodeCode("timestamp-not-utc")
	// CodeEndpointOffArc: an inline chain endpoint that does not project onto
	// its arc within tolerance (§3.3.3). The link is dropped.
	CodeEndpointOffArc = decodeCode("endpoint-off-arc")
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
	// CodeTerminalPipOffNode: an arc's first or last pip is further from the
	// node that end names than §3.3.3's tolerance allows (§3.3.2). Both are
	// kept: the pips are authoritative for geometry, and the node position and
	// the terminal pip are one fact written twice.
	CodeTerminalPipOffNode = decodeCode("terminal-pip-off-node")
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
