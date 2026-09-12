// Package vnx reads and writes Viatrail Network eXchange documents.
//
// VNX carries a Viatrail library — above all its networks: the graph of nodes
// and arcs, and the chains drawn over it as ordered traversals, where two
// routes sharing a trail share the same arc. The specification lives at
// ~/src/knowledge/features/export/format-spec.md.
//
// ⚠ THIS PACKAGE IMPORTS THE STANDARD LIBRARY AND NOTHING ELSE, INCLUDING NO
// OTHER VIATRAIL PACKAGE. It WAS extracted as the reference implementation, on
// 2026-09-06, and an extracted library cannot depend on the module it came
// from — so `locerr` is out too, stdlib-clean though it is.
// TestImportsAreStdlibOnly enforces this on every run, over source and test
// files alike. The consequence is that errors here are plain Go errors;
// translating them for a user surface is the bridge's job, not this package's.
//
// ⚠ WHERE THE COMMENTS IN THIS LIBRARY POINT. Many of them cite `vnxbridge`,
// `pkg/core/…` or `cmd/vnxdb` to explain why a rule here exists. Those are
// Viatrail's consumer packages, and since the extraction they live in a
// DIFFERENT repository — github.com/dave/viatrail, this library's first caller.
// The reasoning still holds and the counterpart still exists; it is only a
// `git grep` from here that will come up empty.
package vnx

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Version is the specification version this package implements. It is
// MAJOR.MINOR.PATCH; see checkVersion for what each component decides.
const Version = "0.1.0"

// Sentinel errors. A caller distinguishes a refusal (these) from a document
// that read with diagnostics — see Decode.
var (
	// ErrMalformed means the bytes are not JSON at all.
	ErrMalformed = errors.New("vnx: malformed JSON")
	// ErrNotVNX means the JSON is well-formed but carries no usable `vnx` key,
	// so it is not a VNX document.
	ErrNotVNX = errors.New("vnx: not a VNX document")
	// ErrUnsupportedVersion means the document declares a version this reader
	// does not implement, or declares something that is not a version at all.
	// Per spec §2.1, through 0.x a reader refuses anything ABOVE its own MAJOR
	// or MINOR rather than half-reading it; PATCH never refuses.
	ErrUnsupportedVersion = errors.New("vnx: unsupported version")
)

// Document is a decoded VNX document. Later tasks add the seven collections.
type Document struct {
	Version   string
	Profile   string
	Generator string
	Created   string

	Folders   []Folder
	Waypoints []Waypoint
	Areas     []Area
	Tracks    []Track
	Routes    []Route
	Networks  []Network

	// Rulesets is carried VERBATIM and never interpreted. Spec §3.7 defers the
	// subsystem: it is being redesigned around selection over data elements, and
	// the current shape — {field, operator, value} with no `equals`, per-kind
	// prefixed style overlays — is not worth freezing on its way out. A 0.1
	// reader preserves the block so a later one can read it, and validates
	// nothing inside it.
	Rulesets []any
}

// DocumentIsEmpty reports whether a document holds no library content at all.
//
// The header fields — Version, Profile, Generator, Created — are present in
// every document including this one, so they say nothing about whether it
// carries anything. Only the collections do.
//
// ⚠ IT LIVES HERE, IN pkg/vnx, BECAUSE THREE PLACES NEED ONE ANSWER (spec 5b,
// 2026-09-02). vnxbridge asks it to tell an honestly-empty export from a format
// that dropped everything, and `vnx convert` must draw exactly the same line —
// but cmd/vnx cannot import vnxbridge, by ruling: purity_test.go pins its
// import set, and pkg/core/db panics in an untagged init(). A second copy in
// the CLI would be a second answer to "is this document empty", drifting from
// the first the day either changes, which is the duplicated truth §2.4 exists
// to prevent. A shared rule that both a database path and the pure CLI must
// obey belongs in this package; that is the general form.
//
// ⚠ EVERY COLLECTION [Document] DECLARES MUST BE COUNTED HERE. A tenth one
// added to the document and forgotten here would make a file holding only that
// collection "empty", and the export carrying it would be refused as a failure.
// vnxbridge's TestDocumentIsEmptyCountsEveryCollectionTheDocumentDeclares
// reflects over the struct and fails if a slice field is not represented, so
// the omission is caught rather than trusted — it reaches this function through
// vnxbridge.documentIsEmpty, which delegates.
func DocumentIsEmpty(d *Document) bool {
	return len(d.Folders) == 0 &&
		len(d.Waypoints) == 0 &&
		len(d.Areas) == 0 &&
		len(d.Tracks) == 0 &&
		len(d.Routes) == 0 &&
		len(d.Networks) == 0 &&
		len(d.Rulesets) == 0
}

// Decode reads a VNX document.
//
// It returns three things and the distinction between the last two matters: a
// document, the diagnostics gathered while reading it, and an error. An error
// means the document was REFUSED and no document is returned. Diagnostics mean
// it was read, with things reported along the way — an unknown key skipped, an
// unusable element dropped. A caller that ignores diagnostics has thrown away
// the only evidence anything was wrong.
//
// Decoding goes through a generic JSON tree rather than struct tags. That is
// deliberate and load-bearing: per-key diagnostics carrying a document path are
// impossible with encoding/json's struct decoding, whose DisallowUnknownFields
// is fatal, reports one error, and carries no path.
func Decode(data []byte) (*Document, []Diagnostic, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrMalformed, err)
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("%w: top level is %s, not an object", ErrNotVNX, jsonKind(raw))
	}

	declared, ok := obj["vnx"].(string)
	if !ok {
		if v, present := obj["vnx"]; present {
			return nil, nil, fmt.Errorf("%w: `vnx` is %s, not a string", ErrNotVNX, jsonKind(v))
		}
		return nil, nil, fmt.Errorf("%w: no `vnx` key", ErrNotVNX)
	}
	if err := checkVersion(declared); err != nil {
		return nil, nil, err
	}

	var diags []Diagnostic
	doc := &Document{Version: declared}
	doc.Profile, diags = stringField(obj, "profile", diags)
	doc.Generator, diags = stringField(obj, "generator", diags)
	doc.Created, diags = stringField(obj, "created", diags)
	if v, present := obj["folders"]; present {
		if arr, ok := v.([]any); ok {
			doc.Folders, diags = decodeFolders(arr, diags)
		} else {
			diags = append(diags, Diagnostic{Severity: Warning, Path: "/folders",
				Message: fmt.Sprintf("`folders` is %s, expected an array — ignored", jsonKind(v))})
		}
	}
	if arr, ok := collection(obj, "waypoints", &diags); ok {
		doc.Waypoints, diags = decodeWaypoints(arr, "/waypoints", diags)
	}
	if arr, ok := collection(obj, "networks", &diags); ok {
		doc.Networks, diags = decodeNetworks(arr, "/networks", diags)
	}
	if arr, ok := collection(obj, "routes", &diags); ok {
		doc.Routes, diags = decodeRoutes(arr, "/routes", diags)
	}
	if arr, ok := collection(obj, "tracks", &diags); ok {
		doc.Tracks, diags = decodeTracks(arr, "/tracks", diags)
	}
	if arr, ok := collection(obj, "areas", &diags); ok {
		doc.Areas, diags = decodeAreas(arr, "/areas", diags)
	}
	if arr, ok := collection(obj, "rulesets", &diags); ok {
		doc.Rulesets = arr
	}
	diags = reportUnknownKeys(obj, "", documentKeys, diags)
	return doc, diags, nil
}

// collection reads one of the seven top-level arrays. Absence is normal; a key
// present but holding something other than an array is a diagnostic.
func collection(obj map[string]any, key string, diags *[]Diagnostic) ([]any, bool) {
	v, present := obj[key]
	if !present {
		return nil, false
	}
	arr, ok := v.([]any)
	if !ok {
		*diags = append(*diags, Diagnostic{Severity: Warning, Path: "/" + key,
			Message: fmt.Sprintf("`%s` is %s, expected an array — ignored", key, jsonKind(v))})
		return nil, false
	}
	return arr, true
}

// documentKeys is every key this reader knows at the top level. The seven
// collections are listed even though F4-F8 have not built them yet: reporting a
// key the format defines as "unknown" would train a caller to ignore
// diagnostics, and that is the one habit this channel cannot survive.
var documentKeys = map[string]bool{
	"vnx": true, "profile": true, "generator": true, "created": true,
	"folders": true, "networks": true, "routes": true,
	"tracks": true, "waypoints": true, "areas": true, "rulesets": true,
}

// reportUnknownKeys implements the §6.2 rule for an unknown key on a core
// object: record a diagnostic, skip the key, KEEP THE ITEM.
//
// The precedent is deliberate and Dave named it — importer/gpx.go:210-214,
// where a coordinate that will not parse increments InvalidPointCount and
// continues. The bad point is dropped, the count is surfaced, and one bad
// character does not kill the file. The house rule was never about
// intolerance; it is about silence. This is the tolerant half, and the
// diagnostic is what stops it being silent.
//
// Granularity differs from the precedent for a reason: gpx.go drops the POINT
// because the value is unusable, whereas an unknown key is merely unrecognised,
// so only the key goes.
func reportUnknownKeys(obj map[string]any, path string, known map[string]bool, diags []Diagnostic) []Diagnostic {
	// Sorted so that a document with several unknown keys produces diagnostics
	// in a stable order; Go map iteration would otherwise vary run to run and
	// make a caller's output nondeterministic.
	unknown := make([]string, 0)
	for k := range obj {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		diags = append(diags, Diagnostic{Severity: Warning, Path: path + "/" + k,
			Code: CodeUnknownKey, Message: "unknown key — ignored"})
	}
	return diags
}

// checkVersion implements spec §2.1.
//
// A version is MAJOR.MINOR.PATCH and ALL THREE COMPONENTS ARE REQUIRED. "0.1"
// is not "0.1.0" and is refused as not a version. Reading it as 0.1.0 would be
// a guess — the two-component spelling predates the patch component, so nothing
// in the file says which patch it was written against — and a guess here is
// exactly the silent half-read the rest of this rule exists to prevent. No VNX
// file exists outside this repository and Dave's own test exports; anyone
// holding a two-component one re-exports.
//
// Only TWO of the three components decide acceptance:
//
//   - MAJOR and MINOR are the breaking axes through 0.x, so a reader refuses
//     anything above its own and says so plainly; it does not attempt a partial
//     read. A 0.1.x reader cannot know whether 0.2.0 moved something
//     load-bearing.
//   - PATCH IS NOT A BREAKING AXIS AT ANY VERSION. A patch bump is additive by
//     definition, so a 0.1.0 reader ACCEPTS 0.1.3 and reads it with the rules it
//     has; whatever 0.1.3 added surfaces through the ordinary unknown-key
//     diagnostics of §6.2, which is the tolerant path this format already takes
//     for anything it does not recognise. It still refuses 0.2.0 and 1.0.0.
//
// From 1.0 the rule relaxes to additive minors — a reader accepts any minor and
// only a MAJOR bump is a hard refusal — which is a change this function will
// need when that day comes. PATCH's rule does not change then; it is already
// the permissive one, and 1.0 merely gives MINOR the same treatment.
func checkVersion(declared string) error {
	dMaj, dMin, _, err := parseVersion(declared)
	if err != nil {
		return fmt.Errorf("%w: %q is not a version — expected MAJOR.MINOR.PATCH, three numbers, like %q (%s)",
			ErrUnsupportedVersion, declared, Version, err)
	}
	oMaj, oMin, _, err := parseVersion(Version)
	if err != nil {
		return fmt.Errorf("vnx: BUG: package version %q does not parse", Version)
	}
	// PATCH is deliberately absent from this comparison: see the doc comment.
	if dMaj > oMaj || (dMaj == oMaj && dMin > oMin) {
		return fmt.Errorf("%w: document is %s, this reader implements %s — a newer reader is needed",
			ErrUnsupportedVersion, declared, Version)
	}
	// ⚠ A LOWER MINOR IS REFUSED TOO, THROUGH 0.x. Spec §2.2 makes each 0.x
	// minor its OWN SERIES, because the minor number is a breaking axis there:
	// a construct may have CHANGED MEANING between 0.0 and 0.1, not merely been
	// added. So a 0.1 reader can no more assume it understands 0.0 than 0.2,
	// and the symmetry is the point — there is no argument that makes a
	// downgrade safe while an upgrade is not.
	//
	// This reader declares support for one series, so it refuses every other.
	// A reader MAY support several, and one that did would apply each
	// document's own series rules.
	if dMaj == 0 && oMaj == 0 && dMin < oMin {
		return fmt.Errorf("%w: document is %s and this reader implements %s — through 0.x each minor is a separate series, and this reader does not implement the %d.%d series",
			ErrUnsupportedVersion, declared, Version, dMaj, dMin)
	}
	return nil
}

// parseVersion splits MAJOR.MINOR.PATCH. Exactly three components, each one or
// more decimal digits — so "0.1", "0.1.0.0" and "0.1.x" are all errors, and so
// are the shapes strconv.Atoi would otherwise wave through ("+1", "-1").
func parseVersion(s string) (major, minor, patch int, err error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("has %d component(s), want 3", len(parts))
	}
	out := [3]int{}
	for i, p := range parts {
		if p == "" {
			return 0, 0, 0, fmt.Errorf("component %d is empty", i+1)
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return 0, 0, 0, fmt.Errorf("component %d (%q) is not a number", i+1, p)
			}
		}
		n, convErr := strconv.Atoi(p)
		if convErr != nil {
			return 0, 0, 0, fmt.Errorf("component %d (%q): %w", i+1, p, convErr)
		}
		out[i] = n
	}
	return out[0], out[1], out[2], nil
}

// stringField reads an optional string. Per spec, only `vnx` is required;
// absence is normal and means "not known", never a default. A key that IS
// present but holds the wrong type is a diagnostic, not a silent zero.
func stringField(obj map[string]any, key string, diags []Diagnostic) (string, []Diagnostic) {
	v, present := obj[key]
	if !present {
		return "", diags
	}
	s, ok := v.(string)
	if !ok {
		return "", append(diags, Diagnostic{
			Severity: Warning,
			Path:     "/" + key,
			Message:  fmt.Sprintf("expected a string, found %s — value ignored", jsonKind(v)),
		})
	}
	return s, diags
}

func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "a boolean"
	case float64:
		return "a number"
	case string:
		return "a string"
	case []any:
		return "an array"
	case map[string]any:
		return "an object"
	}
	return "an unknown value"
}
