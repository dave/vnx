// Package vnx reads and writes Viatrail Network eXchange documents.
//
// VNX carries a Viatrail library — above all its networks: the graph of nodes
// and arcs, and the highlights drawn over it as ordered traversals, where two
// plans sharing a trail share the same arc. The specification lives at
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
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Version is the specification version this package implements. It is
// MAJOR.MINOR.PATCH; see checkVersion for what each component decides.
const Version = "0.10.6"

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

	// The three reasons a version is refused. Each wraps ErrUnsupportedVersion,
	// so errors.Is matches both the reason and ErrUnsupportedVersion; a caller
	// that needs only "refused for its version" matches the parent.

	// ErrNewerVersion means the document's MAJOR or MINOR is above this
	// reader's. A newer reader can read it.
	ErrNewerVersion = fmt.Errorf("%w: newer than this reader", ErrUnsupportedVersion)
	// ErrOlderVersion means the document is in an earlier series than this
	// reader's, which spec §2.2 refuses through 0.x. A newer reader does not
	// help; a reader of that series is needed.
	ErrOlderVersion = fmt.Errorf("%w: older series than this reader", ErrUnsupportedVersion)
	// ErrInvalidVersion means the `vnx` key is a string but not a
	// MAJOR.MINOR.PATCH version, so the document says nothing about which
	// reader it needs.
	ErrInvalidVersion = fmt.Errorf("%w: not a version", ErrUnsupportedVersion)
)

// Document is a decoded VNX document: the header fields, the six collections,
// and the application keys.
type Document struct {
	Version   string
	Generator string
	Created   Timestamp // "" is absent

	Folders   []Folder
	Waypoints []Waypoint
	Areas     []Area
	Tracks    []Track
	Plans     []Plan
	Networks  []Network

	// AppKeys are the document's application keys (SPEC §3.9): every
	// top-level key containing a full stop, each value carried VERBATIM and
	// never interpreted.
	//
	// ⚠ A LIST, NOT A MAP, so that the reflection guards over Document's slice
	// fields see it: TestEncodeToCoversEveryCollectionTheDocumentDeclares and
	// its siblings enumerate slices, and a map field would walk past them.
	//
	// Decode leaves it SORTED BY KEY. Key order is not significant (SPEC §4.4),
	// so sorted is the one order that is the same on every run. [EncodeTo] writes the list in the order it is given.
	AppKeys []AppKey
}

// AppKey is one application key (SPEC §3.9).
//
// Value is whatever encoding/json produced for it: nil, bool, float64, string,
// []any or map[string]any.
//
// ⚠ A nil Value MEANS JSON null. The key is present: a nil Value is a key whose
// value is null, not an absent key, and it is written back as `null`. A TYPED
// nil — a nil []any or a nil map[string]any — is refused by [EncodeTo] and
// [Document.SetAppKey] rather than written: encoding/json spells it `null`,
// and a caller who built an empty slice by accident would lose an empty array
// with no error.
type AppKey struct {
	Key   string
	Value any
}

// validateAppKey is the one check every application key must pass before it is
// written or stored: its name contains a full stop, and its value is not a
// typed nil.
func validateAppKey(k AppKey) error {
	if !IsAppKey(k.Key) {
		return fmt.Errorf("vnx: application key %q contains no full stop (SPEC §3.9)", k.Key)
	}
	if k.Value != nil {
		v := reflect.ValueOf(k.Value)
		switch v.Kind() {
		case reflect.Slice, reflect.Map, reflect.Pointer:
			if v.IsNil() {
				return fmt.Errorf("vnx: application key %q holds a nil %T; use a nil Value for JSON null", k.Key, k.Value)
			}
		}
	}
	return nil
}

// validateAppKeys checks every key, and that no key appears twice (SPEC §3.9).
func validateAppKeys(keys []AppKey) error {
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if err := validateAppKey(k); err != nil {
			return err
		}
		if seen[k.Key] {
			return fmt.Errorf("vnx: application key %q appears twice", k.Key)
		}
		seen[k.Key] = true
	}
	return nil
}

// GetAppKey returns the value of one application key, and whether it is
// present. A present key holding JSON null returns (nil, true).
func (d *Document) GetAppKey(key string) (any, bool) {
	for _, k := range d.AppKeys {
		if k.Key == key {
			return k.Value, true
		}
	}
	return nil, false
}

// SetAppKey stores one application key. A key already present has its value
// replaced in place; a new key is inserted before the first key that sorts
// after it, so a list sorted by key, as [Decode] leaves it, stays sorted.
//
// It refuses what [EncodeTo] would refuse: a key with no full stop, or a typed
// nil value.
func (d *Document) SetAppKey(key string, value any) error {
	k := AppKey{Key: key, Value: value}
	if err := validateAppKey(k); err != nil {
		return err
	}
	for i := range d.AppKeys {
		if d.AppKeys[i].Key == key {
			d.AppKeys[i].Value = value
			return nil
		}
	}
	at := len(d.AppKeys)
	for i := range d.AppKeys {
		if d.AppKeys[i].Key > key {
			at = i
			break
		}
	}
	d.AppKeys = append(d.AppKeys, AppKey{})
	copy(d.AppKeys[at+1:], d.AppKeys[at:])
	d.AppKeys[at] = k
	return nil
}

// DeleteAppKey removes one application key, and reports whether it was
// present. Every copy goes, so a hand-built list holding the key twice is left
// holding it not at all.
func (d *Document) DeleteAppKey(key string) bool {
	var kept []AppKey
	found := false
	for _, k := range d.AppKeys {
		if k.Key == key {
			found = true
			continue
		}
		kept = append(kept, k)
	}
	d.AppKeys = kept
	return found
}

// IsAppKey reports whether a top-level key is an application key: whether it
// contains a full stop (SPEC §3.9). No key the format defines contains one.
func IsAppKey(key string) bool {
	return strings.Contains(key, ".")
}

// DocumentIsEmpty reports whether a document holds no library content at all.
//
// The header fields — Version, Generator, Created — are present in
// every document including this one, so they say nothing about whether it
// carries anything. Only the collections and the application keys do.
//
// ⚠ AN APPLICATION KEY COUNTS WHEN IT IS PRESENT, whatever its value. A key
// holding `null` or `[]` is still content the caller asked to write: SPEC §3.9
// exempts it from the absent-equals-empty rule, so it is not "empty".
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
// ⚠ EVERY COLLECTION [Document] DECLARES MUST BE COUNTED HERE. Another one
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
		len(d.Plans) == 0 &&
		len(d.Networks) == 0 &&
		len(d.AppKeys) == 0
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
// Decode reads the document from memory; [DecodeReader] is the same reader
// over a stream, and the one to use for a file.
func Decode(data []byte) (*Document, []Diagnostic, error) {
	return DecodeReader(bytes.NewReader(data))
}

// decodeAppKeys collects every application key, sorted by key. Every value is
// kept, `null` included, and none is inspected (SPEC §3.9), so this cannot
// raise a diagnostic.
func decodeAppKeys(obj map[string]any) []AppKey {
	var out []AppKey
	for k, v := range obj {
		if IsAppKey(k) {
			out = append(out, AppKey{Key: k, Value: v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// documentFormatKeys is every key the format defines at the top level.
var documentFormatKeys = map[string]bool{
	"vnx": true, "generator": true, "created": true,
	"folders": true, "networks": true, "plans": true,
	"tracks": true, "waypoints": true, "areas": true,
}

// isDocumentKey reports whether a top-level key is recognised: one the format
// defines, or an application key.
//
// ⚠ IT IS A PREDICATE AND NOT A TABLE because the application-key half is an
// open set. It applies to the TOP LEVEL ONLY: every other object keeps its
// fixed table, so a dotted key on an item is reported as unknown (SPEC §3.9).
func isDocumentKey(k string) bool {
	return documentFormatKeys[k] || IsAppKey(k)
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
	return reportUnknownKeysBy(obj, path, func(k string) bool { return known[k] }, diags)
}

// reportUnknownKeysBy is [reportUnknownKeys] over a predicate, for the one
// object whose key set is open: the document itself.
func reportUnknownKeysBy(obj map[string]any, path string, known func(string) bool, diags []Diagnostic) []Diagnostic {
	// Sorted so that a document with several unknown keys produces diagnostics
	// in a stable order; Go map iteration would otherwise vary run to run and
	// make a caller's output nondeterministic.
	unknown := make([]string, 0)
	for k := range obj {
		if !known(k) {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		// The key is a JSON Pointer reference token, so `~` and `/` in it are
		// escaped (RFC 6901); unescaped, "a/b" would name a key "b" inside "a".
		diags = append(diags, Diagnostic{Severity: Warning, Path: path + "/" + escapePointerToken(k),
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
			ErrInvalidVersion, declared, Version, err)
	}
	oMaj, oMin, _, err := parseVersion(Version)
	if err != nil {
		return fmt.Errorf("vnx: BUG: package version %q does not parse", Version)
	}
	// PATCH is deliberately absent from this comparison: see the doc comment.
	if dMaj > oMaj || (dMaj == oMaj && dMin > oMin) {
		return fmt.Errorf("%w: document is %s, this reader implements %s — a newer reader is needed",
			ErrNewerVersion, declared, Version)
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
			ErrOlderVersion, declared, Version, dMaj, dMin)
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
