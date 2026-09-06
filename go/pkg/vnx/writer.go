package vnx

import "strings"

// The substrate the lossy writers share (spec 5b §2.2, §2.7, §4.1b).
//
// GPX, KML and GeoJSON live in SUBPACKAGES of this one — pkg/vnx/gpx, /kml,
// /geojson — and are written by three agents in parallel. Everything in this
// file exists because all three need it and none may answer it alone: three
// answers to one question in one format family is drift with a build behind
// it.
//
// ⚠ THE CONVENTIONS EVERY WRITER DIAGNOSTIC FOLLOWS, pinned here so the three
// cannot disagree:
//
//  1. **Code is always set.** A writer diagnostic reaches a USER, and the
//     bridge can only translate it through the Code. An empty Code is refused
//     at the boundary (vnxbridge.writerDiagnostic), not defaulted.
//  2. **Path is always set**, to the document pointer of the thing that was
//     dropped — "/areas/3", "/networks/1", "/folders/2", "/rulesets/0" — the
//     same JSON-pointer convention the decode sites use. It is what lets an
//     agent reading `vnx convert`'s stderr go straight to the offending
//     element instead of hunting by name.
//  3. **Args[0] is the item's own Name, verbatim and unprefixed**, wherever
//     the item has one. NOT the flattened folder path: KML keeps real folders
//     and would never build one, so a path here would make the same
//     diagnostic read differently per format for no gain. An unnamed item
//     passes its empty name rather than a substitute — inventing "(untitled)"
//     in this package would be untranslatable English on a French screen.
//     A ruleset carries no name at all ([Document.Rulesets] is []any, opaque
//     by ruling), so its diagnostics pass NO args.
//  4. **Severity is Warning at every writer site.** Every one of them reports
//     something DROPPED, which is exactly what Warning is documented to mean.
//  5. **One diagnostic per dropped ITEM, never per dropped attribute.** A
//     GeoJSON export carries name and description only (§2.3), so tags,
//     styles and hidden state are lost on every item in the library — one
//     diagnostic each would bury the real casualties under hundreds of rows
//     nobody can act on. Attribute-level losses are matrix facts, proven by
//     the per-cell tests of §6.3, not per-item diagnostics.
//  6. **Diagnostics come out in document order**, because the walk is in
//     document order and a stable order is what makes them comparable
//     between runs.

// WriteReport is what one writer produced. Every writer returns it:
//
//	func Write(w io.Writer, d *vnx.Document) (vnx.WriteReport, error)
//
// ⚠ Emitted IS WHY THIS IS A STRUCT AND NOT JUST []Diagnostic, and it closes a
// real hole (spec 5b §2.2, T3). vnxbridge refuses an EMPTY export — but its
// check runs inside Export, before any format is known, and asks whether the
// DOCUMENT is empty. Select two networks and choose GPX: the document holds
// two networks and is not empty, Export succeeds, the writer then drops both
// (§2.6) and streams a header-only file, and the sink syncs and renames it
// into place as a success. That is the file §2.2 calls "worse than an error,
// because it looks like a success".
//
// The count is reported by the WRITER because the writer is the only thing
// that knows what its format kept. Re-deriving "what would GPX keep" in
// vnxbridge would be a second answer to that question, drifting from the first
// the day either changes — the duplicated truth §2.4 exists to prevent.
type WriteReport struct {
	// Emitted is how many top-level OBJECTS the writer wrote: <wpt> and <trk>
	// in GPX, <Placemark> in KML, Feature in GeoJSON.
	//
	// ⚠ A CONTAINER IS NOT AN OBJECT. KML's <Folder> nests real folders and
	// carries no geometry, so it does not count; a KML export of nothing but
	// empty folders is a file holding nothing and is refused, exactly as the
	// GPX-of-networks case is. Count the things that put something on a map.
	Emitted int
	// Diagnostics is one entry per casualty, in document order, each
	// following the six conventions above.
	Diagnostics []Diagnostic
}

// PathSeparator joins the segments of a flattened name (spec §2.7, Dave
// 2026-09-02). Space, slash, space — the spaces are part of the ruling.
const PathSeparator = " / "

// FolderPath is the flattened path of the folder with this id: "Foo / Bar",
// root first, joined by [PathSeparator].
//
// It returns ("", true) for the root — folderID "" — because an item at the
// root keeps its BARE NAME WITH NO PREFIX (§2.7). That is a found path, not a
// failure, and the two must not be confused: a caller treating "" as an error
// would diagnose every root item in the library.
//
// ok is false when no path could be built: the id names no folder in this
// document, or its parent chain forms a cycle. The caller keeps the item and
// uses its bare name, and raises [CodeFolderPathUnavailable] — losing a
// position in a tree the format cannot express anyway is not worth dropping an
// item over, and it is not worth hiding either.
//
// ⚠ THE WALK IS BOUNDED BY THE FOLDER COUNT, so a cyclic document cannot hang
// a writer. The trigger is narrow, and overstating it in a comment would be
// its own kind of wrong: [resolveParents] clears both dangling and circular
// parents at DECODE, so a Document read from a file never reaches here with a
// cycle. A hand-built Document can — pkg/vnx is a public package and a
// Document is an ordinary Go struct anyone may fill in — and "a writer must
// not hang on any input" is not a claim that wants an exception.
//
// ⚠ AN ANCESTOR WITH AN EMPTY NAME CONTRIBUTES AN EMPTY SEGMENT rather than
// being skipped. Skipping it would quietly flatten two levels into one, which
// is a fallback hiding a malformed document; " / Bar" is odd-looking and
// truthful.
func FolderPath(d *Document, folderID string) (string, bool) {
	if folderID == "" {
		return "", true
	}
	index := make(map[string]int, len(d.Folders))
	for i := range d.Folders {
		index[d.Folders[i].ID] = i
	}
	i, ok := index[folderID]
	if !ok {
		return "", false
	}
	var names []string
	seen := make(map[int]bool, len(d.Folders))
	for range d.Folders {
		if seen[i] {
			return "", false // a cycle: no path exists
		}
		seen[i] = true
		names = append(names, d.Folders[i].Name)
		parent := d.Folders[i].Parent
		if parent == "" {
			// Reverse in place: the walk collected leaf-first.
			for l, r := 0, len(names)-1; l < r; l, r = l+1, r-1 {
				names[l], names[r] = names[r], names[l]
			}
			return strings.Join(names, PathSeparator), true
		}
		i, ok = index[parent]
		if !ok {
			// A parent naming nothing leaves the rest of the path unknowable.
			// Returning the part we walked would be a plausible half-path
			// nothing marks as partial.
			return "", false
		}
	}
	// More steps than there are folders: the chain cannot terminate.
	return "", false
}

// FlatName is the name an item carries in a format with no folders (§2.7):
// its folder path, then its own name, joined by [PathSeparator].
//
// ⚠ IT EXISTS SO GPX AND GEOJSON CANNOT DISAGREE ABOUT THE JOIN. The
// separator's spaces, the root having no prefix, and what happens when the
// path is unavailable are three details two independent writers would answer
// two ways, and the difference would show up as one format's names silently
// differing from the other's.
//
// KML does not call this: it has real nested <Folder> elements and keeps the
// tree.
func FlatName(d *Document, folderID, name string) (string, bool) {
	path, ok := FolderPath(d, folderID)
	if !ok || path == "" {
		return name, ok
	}
	return path + PathSeparator + name, true
}
