package vnx

import (
	"fmt"
	"io"
	"sort"
)

// EncodeTo streams a Document to w in canonical form (spec §4.5).
//
// Peak memory is O(the largest single item), not O(the document): the
// document- and network-level scaffolding is written directly, and each item's
// tree is built, rendered and dropped before the next one starts.
//
// It emits exactly what [Encode] emits — Encode is a bytes.Buffer over this
// function — and the equality is asserted over the whole oracle corpus rather
// than argued, because a streaming encoder that gets the canonical form subtly
// wrong still produces valid JSON.
//
// ⚠ THE ORDERING RULE MOVES FROM A POST-PASS TO A PRODUCER OBLIGATION here,
// and that is the whole difficulty. writeObject can sort an object's keys
// because it holds them all; a streamer cannot look ahead, so the two
// hand-rolled orderings below — the document's keys and a network's — must be
// SORTED rather than iterated, and are guarded by
// TestEncodeToCoversEveryCollectionTheDocumentDeclares and by the determinism
// test's two arms.
func EncodeTo(w io.Writer, doc *Document) error {
	return encodeTo(emitter{}, w, doc)
}

// encodeTo is EncodeTo and ReformatTo with the one decision made by the caller.
func encodeTo(e emitter, w io.Writer, doc *Document) error {
	if doc == nil {
		return fmt.Errorf("vnx: nothing to encode")
	}
	b := Stick(w)
	if err := writeDocument(e, b, doc); err != nil {
		return err
	}
	b.WriteString("\n")
	return b.err
}

func writeDocument(e emitter, b *StickyWriter, doc *Document) error {
	version := doc.Version
	if version == "" {
		version = Version
	}
	scalars := map[string]any{"vnx": version}
	// An empty header field is OMITTED, exactly as putString omits it. Writing
	// "" would turn "not known" into a value.
	putString2(scalars, "profile", doc.Profile)
	putString2(scalars, "generator", doc.Generator)
	putString2(scalars, "created", doc.Created)

	containers := make([]containerEmitter, 0, len(documentCollections))
	for _, c := range documentCollections {
		containers = append(containers, containerEmitter{
			key:     c.key,
			present: c.present(doc),
			emit:    func(b *StickyWriter, depth int) error { return c.emit(e, b, doc, depth) },
		})
	}
	return streamObject(b, 0, scalars, containers)
}

// containerEmitter is one container key of an object being streamed: its name,
// whether the document CARRIED it, and how to write it.
//
// ⚠ `present` IS NOT `len(...) > 0`, AND THE DIFFERENCE IS NORMATIVE. Spec §3.1
// makes an absent collection and an empty one the same CONTENT and different
// DOCUMENTS: a reader treats them alike, and a formatter reproduces which it was
// given, so whether a collection was there is model state exactly as a
// position's arity is. A nil slice or map is absent and its key is omitted; a
// non-nil empty one is present and emits `[]` or `{}`.
//
// The decoder already draws that line — an absent key leaves the field nil,
// while `[]` and `{}` produce non-nil empty values — so this is the encoder
// catching up with what the model already knew.
//
// ⚠ FOUR LENGTH GUARDS ON THE EMIT PATH ARE CORRECT AND MUST NOT BE "FIXED".
// streamObject's `len(scalars) == 0 && live == 0`, streamNetworks' `len(ns)`,
// streamNodes' `len(nodes)` and streamArcs' `len(arcs)` all render the EMPTY
// FORM — `[]` or `{}` — rather than omitting anything, and each is reached only
// after presence has already been decided above it. That is the distinction
// that makes them safe, and it is not visible from the call site.
//
// The unsafe shape is a length guard that decides whether a key APPEARS. A
// census on 2026-09-09 found eleven guards on this path: those four, `putData`,
// and six style sites. Six of the seven defects were one bug.
type containerEmitter struct {
	key     string
	present bool
	emit    func(b *StickyWriter, depth int) error
}

// streamObject writes one object under §4.5's ordering rule, without holding
// its payload.
//
// ⚠ THE RULE IS SCALARS THEN CONTAINERS, EACH GROUP LEXICOGRAPHIC. writeObject
// can enforce it because it holds every value at once and classifies by type;
// a streamer cannot look ahead, so the obligation moves to the producer. The
// scalars are sorted here; the containers must ALREADY be in lexicographic
// order, which is asserted for each table rather than trusted
// (TestStreamedContainerTablesAreInCanonicalOrder).
func streamObject(b *StickyWriter, depth int, scalars map[string]any, containers []containerEmitter) error {
	live := 0
	for _, c := range containers {
		if c.present {
			live++
		}
	}
	if len(scalars) == 0 && live == 0 {
		b.WriteString("{}")
		return b.err
	}
	keys := make([]string, 0, len(scalars))
	for k := range scalars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString("{\n")
	inner := depth + 1
	first := true
	sep := func() {
		if !first {
			b.WriteString(",\n")
		}
		first = false
		b.WriteString(indent(inner))
	}
	for _, k := range keys {
		sep()
		writeKey(b, k)
		if err := writeCanonical(b, scalars[k], inner); err != nil {
			return err
		}
	}
	for _, c := range containers {
		if !c.present {
			continue
		}
		sep()
		writeKey(b, c.key)
		if err := c.emit(b, inner); err != nil {
			return err
		}
	}
	b.WriteString("\n" + indent(depth) + "}")
	return b.err
}

// putString2 is putString over a map[string]any. Same rule, same omission.
func putString2(o map[string]any, key, v string) {
	if v != "" {
		o[key] = v
	}
}

// writeKey emits a quoted key and its separator. Keys go through json.Marshal
// like any other string so that escaping is one implementation, not two.
func writeKey(b *StickyWriter, k string) {
	b.WriteString(canonicalString(k))
	b.WriteString(": ")
}

// documentCollection is one of the document's container keys.
//
// ⚠ THE TABLE IS GUARDED, NOT TRUSTED. `field` names the [Document] field it
// covers, and TestEncodeToCoversEveryCollectionTheDocumentDeclares reflects
// over the struct and fails if a slice field has no entry — so a tenth
// collection added to the model and forgotten here cannot ship as a document
// that silently omits it. It is the same shape, and exists for the same
// reason, as vnxbridge's TestDocumentIsEmptyCountsEveryCollectionTheDocument-
// Declares and TestCountsCoverEveryCollectionTheDocumentDeclares, which guard
// the same struct against two other hand-written lists.
type documentCollection struct {
	field   string
	key     string
	present func(*Document) bool
	emit    func(e emitter, b *StickyWriter, doc *Document, depth int) error
}

// Lexicographic by key, which is the canonical container order. Asserted, not
// assumed — TestDocumentCollectionsAreInCanonicalOrder.
var documentCollections = []documentCollection{
	{"Areas", "areas", func(d *Document) bool { return d.Areas != nil },
		func(e emitter, b *StickyWriter, d *Document, depth int) error {
			return streamItems(b, depth, len(d.Areas), func(i int) (map[string]any, error) { return e.areaTree(d.Areas[i], false) })
		}},
	{"Folders", "folders", func(d *Document) bool { return d.Folders != nil },
		func(e emitter, b *StickyWriter, d *Document, depth int) error {
			return streamItems(b, depth, len(d.Folders), func(i int) (map[string]any, error) { return folderTree(d.Folders[i]) })
		}},
	{"Networks", "networks", func(d *Document) bool { return d.Networks != nil },
		func(e emitter, b *StickyWriter, d *Document, depth int) error {
			return streamNetworks(e, b, d.Networks, depth)
		}},
	{"Routes", "routes", func(d *Document) bool { return d.Routes != nil },
		func(e emitter, b *StickyWriter, d *Document, depth int) error {
			return streamItems(b, depth, len(d.Routes), func(i int) (map[string]any, error) { return e.routeTree(d.Routes[i]) })
		}},
	{"Rulesets", "rulesets", func(d *Document) bool { return d.Rulesets != nil },
		func(e emitter, b *StickyWriter, d *Document, depth int) error {
			// Verbatim and uninspected (vnx.go:57-63), and NOT streamed: the
			// block is already whole in memory, its elements are arbitrary
			// values rather than item trees, and handing it to writeCanonical
			// is what keeps every writer rule — including the one-line rule for
			// an array that happens to hold only numbers — applying inside a
			// block this package refuses to understand.
			return writeCanonical(b, []any(d.Rulesets), depth)
		}},
	{"Tracks", "tracks", func(d *Document) bool { return d.Tracks != nil },
		func(e emitter, b *StickyWriter, d *Document, depth int) error {
			return streamItems(b, depth, len(d.Tracks), func(i int) (map[string]any, error) { return e.trackTree(d.Tracks[i]) })
		}},
	{"Waypoints", "waypoints", func(d *Document) bool { return d.Waypoints != nil },
		func(e emitter, b *StickyWriter, d *Document, depth int) error {
			return streamItems(b, depth, len(d.Waypoints), func(i int) (map[string]any, error) { return e.waypointTree(d.Waypoints[i], false) })
		}},
}

// streamItems writes an array of item objects, building ONE item's tree at a
// time and dropping it before the next.
//
// The item type is map[string]any rather than any, and that is load-bearing: an
// array of numbers renders on ONE line (writeArray's allScalarNumbers branch)
// and an array of objects does not, so a helper that accepted `any` could be
// handed a number array and would emit it multi-line. An item tree is always an
// object, and the signature says so.
func streamItems(b *StickyWriter, depth, n int, tree func(int) (map[string]any, error)) error {
	if n == 0 {
		// Unreachable through writeDocument, which omits an empty collection's
		// key entirely. Kept honest rather than assumed.
		b.WriteString("[]")
		return b.err
	}
	b.WriteString("[\n")
	inner := depth + 1
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(indent(inner))
		t, err := tree(i)
		if err != nil {
			return err
		}
		if err := writeCanonical(b, t, inner); err != nil {
			return err
		}
	}
	b.WriteString("\n" + indent(depth) + "]")
	return b.err
}

// folderTree is the per-item builder documentTree inlined. Every other
// collection already had one; this one did not, and a streaming encoder needs
// them all to be the same shape.
func folderTree(f Folder) (map[string]any, error) {
	o := map[string]any{"id": f.ID}
	putString(o, "name", f.Name)
	putString(o, "notes", f.Notes)
	putString(o, "parent", f.Parent)
	putBool(o, "hidden", f.Hidden)
	if err := putData(o, f.Data); err != nil {
		return nil, err
	}
	return o, nil
}

// streamNetworks writes the networks array, one network at a time.
func streamNetworks(e emitter, b *StickyWriter, ns []Network, depth int) error {
	if len(ns) == 0 {
		b.WriteString("[]")
		return b.err
	}
	b.WriteString("[\n")
	inner := depth + 1
	for i := range ns {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(indent(inner))
		if err := streamNetwork(e, b, ns[i], inner); err != nil {
			return err
		}
	}
	b.WriteString("\n" + indent(depth) + "]")
	return b.err
}

// streamNetwork writes one network — the second of the two hand-rolled
// orderings, and the one with the pips in it.
//
// ⚠ NODES COME AFTER ARCS, AND THAT IS MANDATORY. Container order is
// lexicographic — arcs, areas, chains, data, nodes, points — so a streaming
// encoder that emitted the nodes first to establish the keys before the arcs
// referenced them would produce a different file. Confirmed in
// testdata/rich-fixture.golden.vnx:96 (`arcs`) and :324 (`nodes`).
//
// ⚠ Nodes and Arcs are map[string]…, so their keys are COLLECTED AND SORTED,
// never iterated. Go map iteration is randomised per process and per range, so
// an encoder that iterated them would produce different bytes on every run with
// no error and no panic — non-determinism rather than breakage, which only a
// byte comparison catches. That is what the determinism test's two arms are
// for, and this is the code they are aimed at.
func streamNetwork(e emitter, b *StickyWriter, n Network, depth int) error {
	scalars := map[string]any{}
	putString2(scalars, "folder", n.Folder)
	putString2(scalars, "name", n.Name)
	putString2(scalars, "notes", n.Notes)
	if n.Hidden {
		scalars["hidden"] = true
	}
	return streamObject(b, depth, scalars, networkContainers(e, n))
}

// networkContainers is the network's container table, in canonical order:
// arcs, areas, chains, data, nodes, points.
//
// ⚠ `arcs` SORTS BEFORE `areas`, and both spec 4b's plan and its brief say the
// opposite. Lexicographic means lexicographic: "arcs" and "areas" agree on
// "ar" and then `c` < `e`. Written the plan's way this produced a valid file
// that differed from the golden at byte 885 of the conformance corpus's
// example-network. The rule is the sort, never a remembered list.
//
// It is a function rather than a literal inside streamNetwork so that
// TestStreamedContainerTablesAreInCanonicalOrder can read the SAME order the
// encoder writes, instead of a second copy of it standing beside the first.
func networkContainers(e emitter, n Network) []containerEmitter {
	return []containerEmitter{
		{"arcs", n.Arcs != nil, func(b *StickyWriter, d int) error { return streamArcs(e, b, n.Arcs, d) }},
		{"areas", n.Areas != nil, func(b *StickyWriter, d int) error {
			return streamItems(b, d, len(n.Areas), func(i int) (map[string]any, error) { return e.areaTree(n.Areas[i], true) })
		}},
		{"chains", n.Chains != nil, func(b *StickyWriter, d int) error {
			return streamItems(b, d, len(n.Chains), func(i int) (map[string]any, error) { return e.chainTree(n.Chains[i]) })
		}},
		{"data", n.Data != nil, func(b *StickyWriter, d int) error {
			recs, err := recordBodies(n.Data)
			if err != nil {
				return err
			}
			return writeCanonical(b, recs, d)
		}},
		{"nodes", n.Nodes != nil, func(b *StickyWriter, d int) error { return streamNodes(e, b, n.Nodes, d) }},
		{"points", n.Points != nil, func(b *StickyWriter, d int) error {
			return streamItems(b, d, len(n.Points), func(i int) (map[string]any, error) { return e.waypointTree(n.Points[i], true) })
		}},
	}
}

// streamNodes writes the nodes object: small, scalar, and buffered whole
// because it is O(nodes) and nothing else.
func streamNodes(e emitter, b *StickyWriter, nodes map[string]Position, depth int) error {
	if len(nodes) == 0 {
		b.WriteString("{}")
		return b.err
	}
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("{\n")
	inner := depth + 1
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(indent(inner))
		writeKey(b, k)
		t, err := e.positionTree(nodes[k])
		if err != nil {
			return err
		}
		if err := writeCanonical(b, t, inner); err != nil {
			return err
		}
	}
	b.WriteString("\n" + indent(depth) + "}")
	return b.err
}

// streamArcs writes the arcs object one arc at a time. The pips live here, so
// this is where the streaming actually earns its keep.
func streamArcs(e emitter, b *StickyWriter, arcs map[string]Arc, depth int) error {
	if len(arcs) == 0 {
		b.WriteString("{}")
		return b.err
	}
	keys := make([]string, 0, len(arcs))
	for k := range arcs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("{\n")
	inner := depth + 1
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(indent(inner))
		writeKey(b, k)
		t, err := e.arcTree(arcs[k], true)
		if err != nil {
			return err
		}
		if err := writeCanonical(b, t, inner); err != nil {
			return err
		}
	}
	b.WriteString("\n" + indent(depth) + "}")
	return b.err
}
