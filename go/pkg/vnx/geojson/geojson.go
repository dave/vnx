// Package geojson writes a [vnx.Document] as GeoJSON (RFC 7946).
//
// It is one of the three lossy writers of spec 5b. Its job is to be read by
// OTHER software — QGIS, a notebook, a script — not to be Viatrail's backup
// format. VNX is that, which is what frees this writer to be honest and lossy
// rather than inventive.
//
// ⚠ THIS PACKAGE IS STDLIB-ONLY, like [vnx] itself. No locerr, no db, no
// vtlog. A writer that needs one of those has taken a wrong turn.
//
// ⚠ WHAT IT DOES NOT DO, and must not start doing (spec §2.3):
//
//   - No `properties` beyond `name` and `description`. Not tags, not style,
//     not hidden state. `properties` being schemaless is exactly what makes
//     that temptation easy to rationalise; it is an <extensions> block wearing
//     a different hat, and every byte of it would be ignored by the
//     applications this format exists to talk to.
//   - No folder member of any kind. RFC 7946 has no folder concept, so the
//     tree is folded into the NAME (§2.7) and nowhere else.
package geojson

import (
	"fmt"
	"io"
	"sort"

	"github.com/dave/vnx/go/pkg/vnx"
)

// Write emits d as one flat FeatureCollection and reports what it produced.
//
// ⚠ IT STREAMS, and it returns [vnx.StickyWriter.Err] rather than nil (spec
// §4.1b). Spec 4b measured what materialising the output costs — 1,242 MB of
// peak heap at 2M pips against 199 MB streaming — and its audit named the
// other half: an IGNORED SHORT WRITE means this function returns nil, the sink
// Syncs, sets `committed`, and renames a truncated file into place as a
// successful export. Data loss presenting as a clean write.
//
// On error it returns a ZERO report. A caller must not be able to read
// Emitted off a run that did not finish and conclude something was written.
func Write(w io.Writer, d *vnx.Document) (vnx.WriteReport, error) {
	f := &features{sw: vnx.Stick(w)}
	f.sw.WriteString(`{"type":"FeatureCollection","features":[`)

	// ⚠ THE WALK IS IN [vnx.Document] FIELD ORDER, and that is what makes the
	// diagnostics comparable between runs (writer.go convention 6). Decode
	// reads the collections in a different order; the STRUCT's order is the
	// document's declared order and the one a reader of the type sees.
	f.folders(d)
	f.waypoints(d)
	f.areas(d)
	f.tracks(d)
	f.routes(d)
	f.networks(d)
	f.diags = append(f.diags, vnx.AppKeyDiagnostics(d, "GeoJSON")...)

	f.sw.WriteString("\n]}\n")
	if f.err != nil {
		return vnx.WriteReport{}, f.err
	}
	if err := f.sw.Err(); err != nil {
		return vnx.WriteReport{}, err
	}
	return vnx.WriteReport{Emitted: f.emitted, Diagnostics: f.diags}, nil
}

// features is the walk's state: the sink, the count the bridge refuses on, and
// the casualties.
type features struct {
	sw      *vnx.StickyWriter
	emitted int
	diags   []vnx.Diagnostic
	// err is a failure the SINK did not cause — today only a coordinate that
	// is not finite. It is latched like a write error and checked in the same
	// place, so there is exactly one way for this writer to fail and exactly
	// one place that decides whether the file is written.
	err error
}

// fail latches the first non-sink failure.
func (f *features) fail(err error) {
	if f.err == nil {
		f.err = err
	}
}

func (f *features) warn(code, path string, args []string, message string) {
	f.diags = append(f.diags, vnx.Diagnostic{
		Severity: vnx.Warning,
		Code:     code,
		Path:     path,
		Args:     args,
		Message:  message,
	})
}

// flatName is §2.7's flattened name, and it raises the diagnostic when the
// path cannot be built. The item is KEPT under its bare name: losing a
// position in a tree GeoJSON cannot express anyway is not worth dropping
// content over, and it is not worth hiding either.
func (f *features) flatName(d *vnx.Document, path, folderID, name string) string {
	flat, ok := vnx.FlatName(d, folderID, name)
	if !ok {
		f.warn(vnx.CodeFolderPathUnavailable, path, []string{name},
			fmt.Sprintf("folder %q is not in this document or its parents form a cycle; the item keeps its bare name", folderID))
	}
	return flat
}

// folders raises the empty-folder casualties. A NON-EMPTY folder is not a
// casualty (§2.7): every item it held carries its name in their own, so
// nothing was lost and a diagnostic would be noise on the common case. A
// folder with no items anywhere beneath it has nowhere to put its name and
// genuinely vanishes.
//
// ⚠ ONLY TOP-LEVEL ITEMS POPULATE A FOLDER. A [vnx.Network]'s in-network
// points and areas carry a Folder field of their own, but they are placed by
// the network they belong to and never appear in the tree independently, so
// they cannot keep a folder alive. Counting them would mark a folder non-empty
// whose name reaches no feature's name — the diagnostic would be missing on a
// real casualty.
func (f *features) folders(d *vnx.Document) {
	if len(d.Folders) == 0 {
		return
	}
	index := make(map[string]int, len(d.Folders))
	for i := range d.Folders {
		index[d.Folders[i].ID] = i
	}
	populated := make(map[int]bool, len(d.Folders))
	// ⚠ THE UPWARD WALK IS BOUNDED BY THE FOLDER COUNT, for the same reason
	// [vnx.FolderPath] is: Folder.Parent is a free string, and a hand-built
	// Document can be cyclic even though resolveParents clears cycles at
	// decode. A naive parent-walk hangs, and a writer must not hang on any
	// input.
	mark := func(folderID string) {
		i, ok := index[folderID]
		for step := 0; ok && step < len(d.Folders); step++ {
			if populated[i] {
				return // this ancestor chain is already marked
			}
			populated[i] = true
			i, ok = index[d.Folders[i].Parent]
		}
	}
	for i := range d.Waypoints {
		mark(d.Waypoints[i].Folder)
	}
	for i := range d.Areas {
		mark(d.Areas[i].Folder)
	}
	for i := range d.Tracks {
		mark(d.Tracks[i].Folder)
	}
	for i := range d.Routes {
		mark(d.Routes[i].Folder)
	}
	for i := range d.Networks {
		mark(d.Networks[i].Folder)
	}
	for i := range d.Folders {
		if populated[i] {
			continue
		}
		f.warn(vnx.CodeEmptyFolderDropped, fmt.Sprintf("/folders/%d", i),
			[]string{d.Folders[i].Name},
			"folder holds no items anywhere beneath it, and GeoJSON has no folders to keep it in")
	}
}

func (f *features) waypoints(d *vnx.Document) {
	for i := range d.Waypoints {
		w := &d.Waypoints[i]
		at := fmt.Sprintf("/waypoints/%d", i)
		f.begin(f.flatName(d, at, w.Folder, w.Name), w.Notes)
		f.point(w.At)
		f.end()
	}
}

func (f *features) areas(d *vnx.Document) {
	for i := range d.Areas {
		a := &d.Areas[i]
		at := fmt.Sprintf("/areas/%d", i)
		ring, ok := ringFor(a.Polygon)
		if !ok {
			f.warn(vnx.CodeItemNoGeometry, at+"/polygon", []string{a.Name}, ringProblem(a.Polygon))
			continue
		}
		f.begin(f.flatName(d, at, a.Folder, a.Name), a.Notes)
		f.polygon(ring)
		f.end()
	}
}

func (f *features) tracks(d *vnx.Document) {
	for i := range d.Tracks {
		t := &d.Tracks[i]
		at := fmt.Sprintf("/tracks/%d", i)
		var lines [][]vnx.Position
		var dropped []int
		for j := range t.Segments {
			// ⚠ A SEGMENT IS A LineString AND RFC 7946 §3.1.4 REQUIRES TWO OR
			// MORE POSITIONS. A one-pip segment cannot be written as one, and
			// writing it anyway produces an invalid file that lands as a
			// success — the same class of failure as an ignored short write.
			if len(t.Segments[j].Pips) < 2 {
				dropped = append(dropped, j)
				continue
			}
			lines = append(lines, t.Segments[j].Pips)
		}
		if len(lines) == 0 {
			// Nothing survived: ONE diagnostic for the item, not one per
			// segment. Per-segment rows here would say the same thing several
			// times about a track that is simply not on the map.
			f.warn(vnx.CodeItemNoGeometry, at, []string{t.Name},
				fmt.Sprintf("track has no segment of two or more positions (%d segments)", len(t.Segments)))
			continue
		}
		// The track IS emitted, so the losses are per-segment and are reported
		// that way: the Path names the segment an agent has to go and look at.
		// This is not the attribute-level noise convention 5 forbids — a
		// segment is geometry, and a dropped one is a piece of the walk that
		// is not in the file.
		//
		// ⚠ CodePartialGeometry, NOT CodeItemNoGeometry. The whole-item code's
		// English is "%q has no usable geometry — dropped", which is a false
		// statement about a track the reader can see in the file. Ruled
		// 2026-09-02 after this writer raised the whole-item code here and said
		// so, and the KML writer was found dropping the same segment silently:
		// one document, two disclosures, neither right.
		for _, j := range dropped {
			f.warn(vnx.CodePartialGeometry, fmt.Sprintf("%s/segments/%d", at, j), []string{t.Name},
				fmt.Sprintf("segment has %d position(s), a GeoJSON LineString needs two", len(t.Segments[j].Pips)))
		}
		f.begin(f.flatName(d, at, t.Folder, t.Name), t.Notes)
		f.multiLineString(lines)
		f.end()
	}
}

func (f *features) routes(d *vnx.Document) {
	for i := range d.Routes {
		r := &d.Routes[i]
		at := fmt.Sprintf("/routes/%d", i)
		line, err := vnx.WalkLine(r.Nodes, r.Arcs, r.Walk)
		if err != nil {
			f.fail(fmt.Errorf("geojson: route %q: %w", r.Name, err))
			return
		}
		if len(line) < 2 {
			f.warn(vnx.CodeItemNoGeometry, at, []string{r.Name},
				fmt.Sprintf("route has %d position(s) across %d pass(es), a GeoJSON LineString needs two", len(line), len(r.Walk)))
			continue
		}
		f.begin(f.flatName(d, at, r.Folder, r.Name), r.Notes)
		f.lineString(line)
		f.end()
	}
}

// networks emits §2.6's shape as redesigned 2026-09-03: ONE MultiLineString
// Feature holding the network's arcs and carrying the network's own name and
// notes, then ONE NAMED FEATURE PER in-network point, area and chain.
//
// ⚠ THE GeometryCollection IS GONE. It bent "one Feature per thing" so that the
// in-network points and areas had somewhere to go, and the price was their Name,
// Notes, Hidden and Style — vnx.Arc carries none of those, but Network.Points are
// full vnx.Waypoints and Network.Areas full vnx.Areas (item.go:34-55), and all
// four fields were destroyed on every one of them. Dave's rule replaces it:
// unnamed things merge into one geometry, named things get their own named
// object. A flat FeatureCollection of simple geometries is also what QGIS,
// Mapbox GL and Leaflet handle reliably; a nested GeometryCollection is not.
//
// ⚠ NO ARCS MEANS NO FEATURE OF ITS OWN. Dave, 2026-09-03: "a network is
// designed to have arcs, and if it doesn't have arcs, then it's fine to have the
// network name and description lost when you're exporting to GeoJSON. So let's
// just only emit the multi-line string if it has arcs." An earlier draft of this
// ruling wrote {"type":"MultiLineString","coordinates":[]} to keep the name;
// that is withdrawn, and so is the validator relaxation it needed.
//
// The name is not wholly gone even so: the network's in-network points and areas
// carry it in their own names through [inNetworkName]. What IS lost is the
// network's NOTES, which nothing else carries — so the loss is disclosed with
// [vnx.CodePartialGeometry], part of the network having reached the file and
// part of it not. Dave ruled the loss acceptable; he did not rule it silent, and
// a silent drop is the fallback the house rule forbids.
//
// ⚠ A CHAIN CANNOT EXIST WITHOUT ARCS — it is a traversal of them — so the
// no-arcs case holds points and areas and nothing else.
//
// ⚠ A NETWORK HOLDING NOTHING AT ALL IS DROPPED with [vnx.CodeItemNoGeometry]
// instead. The two are different claims and must not be merged: one says the
// network is in the file without its notes, the other says it is not in the file.
//
// ⚠ NETWORK AND FOLDER PATHS COLLIDE AND THAT IS ACCEPTED (Dave, 2026-09-03). An
// in-network point "Bothy" in network "Cairngorms" in folder "Highlands" flattens
// to the same name as a waypoint "Bothy" in folder "Highlands / Cairngorms". The
// earlier draft's "Contents" segment was what separated them and Dave removed it.
// GeoJSON names are not identifiers and folder flattening already collides when
// two items share a name in one folder. Do NOT "fix" it.
//
// ⚠ THE DUPLICATION IS DELIBERATE. A chain's line covers arcs the network Feature
// also emits. Do NOT "optimise" it by dropping the bare arcs: an arc no chain
// traverses would then vanish silently, which is the same class of loss this
// shape exists to prevent.
func (f *features) networks(d *vnx.Document) {
	for i := range d.Networks {
		n := &d.Networks[i]
		at := fmt.Sprintf("/networks/%d", i)

		// ⚠ SORTED. Network.Arcs is a map, and ranging over it emits in
		// random order — the byte-compared golden of §6.1 would go flaky in a
		// way that looks like a bug in the writer and is not.
		arcIDs := make([]string, 0, len(n.Arcs))
		for id := range n.Arcs {
			arcIDs = append(arcIDs, id)
		}
		sort.Strings(arcIDs)

		var lines [][]vnx.Position
		for _, id := range arcIDs {
			// Node to node: an arc's line is at least its two ends, so every
			// arc makes a LineString.
			line, err := vnx.ArcLine(n.Nodes, n.Arcs[id])
			if err != nil {
				f.fail(fmt.Errorf("geojson: network %q arc %q: %w", n.Name, id, err))
				return
			}
			lines = append(lines, line)
		}

		type ringed struct {
			index int
			ring  []vnx.Position
		}
		var rings []ringed
		for j := range n.Areas {
			ring, ok := ringFor(n.Areas[j].Polygon)
			if !ok {
				// ⚠ THE WHOLE-ITEM CODE, DELIBERATELY, EVEN THOUGH THE NETWORK
				// SURVIVES. Args[0] is the AREA's own name and the area is gone
				// entirely, so "%q has no usable geometry — dropped" is true of
				// the thing it names. CodePartialGeometry would say part of the
				// area could not be placed, which is the wrong claim: none of it
				// could. The test is what the Args name, not what the walk was
				// in the middle of.
				f.warn(vnx.CodeItemNoGeometry, fmt.Sprintf("%s/areas/%d/polygon", at, j),
					[]string{n.Areas[j].Name}, ringProblem(n.Areas[j].Polygon))
				continue
			}
			rings = append(rings, ringed{j, ring})
		}

		if len(lines) == 0 && len(n.Points) == 0 && len(rings) == 0 {
			f.warn(vnx.CodeItemNoGeometry, at, []string{n.Name},
				"network holds no arc, point or area that can be placed")
		} else {
			base := f.flatName(d, at, n.Folder, n.Name)
			if len(lines) > 0 {
				f.begin(base, n.Notes)
				f.multiLineString(lines)
				f.end()
			} else {
				f.warn(vnx.CodePartialGeometry, at, []string{n.Name},
					"network has no arc, so it has no Feature of its own and its notes are not written; "+
						"its name survives in the names of the points and areas it holds")
			}
			// ⚠ THE WALK IS POINTS, AREAS, CHAINS — the order the old shared
			// GeometryCollection used, kept so the diagnostics of one document
			// stay comparable with the runs that came before the redesign. The
			// spec illustrates a different order in its KML snippet than its
			// prose lists, so no order was ruled and this one is the least
			// disruptive.
			for j := range n.Points {
				p := &n.Points[j]
				f.begin(inNetworkName(base, p.Name), p.Notes)
				f.point(p.At)
				f.end()
			}
			for _, r := range rings {
				a := &n.Areas[r.index]
				f.begin(inNetworkName(base, a.Name), a.Notes)
				f.polygon(r.ring)
				f.end()
			}
		}

		f.chains(d, n, at)
	}
}

// inNetworkName is §2.7's join applied inside a network: the network's own
// flattened name, then the item's. It is the ONE spelling of that join, so the
// points, areas and chains of a network cannot drift apart on it.
//
// An empty base — a network with no name at the root — leaves the item's bare
// name rather than a leading separator.
func inNetworkName(base, name string) string {
	if base == "" {
		return name
	}
	return base + vnx.PathSeparator + name
}

func (f *features) chains(d *vnx.Document, n *vnx.Network, at string) {
	// The network's own flattened name prefixes every chain: "Folder /
	// Cairngorms / Lairig Ghru" (§2.7). The separator is the same one, because
	// a second spelling of the same join is the drift the shared constant
	// exists to prevent.
	base, ok := vnx.FlatName(d, n.Folder, n.Name)
	if !ok {
		base = n.Name // already diagnosed by the network's own flatName call
	}
	for j := range n.Chains {
		c := &n.Chains[j]
		cat := fmt.Sprintf("%s/chains/%d", at, j)
		// ⚠ A WALK THAT DOES NOT RESOLVE FAILS THE WRITE. It cannot come from
		// a file — the reader drops such a chain — so only a hand-built
		// Document reaches here with one, and drawing part of it, or the whole
		// arc forwards, would put a line on the map nobody walked.
		line, err := vnx.WalkLine(n.Nodes, n.Arcs, c.Walk)
		if err != nil {
			f.fail(fmt.Errorf("geojson: network %q chain %q: %w", n.Name, c.Name, err))
			return
		}
		if len(line) < 2 {
			f.warn(vnx.CodeItemNoGeometry, cat, []string{c.Name},
				fmt.Sprintf("chain has %d position(s) across %d pass(es), a GeoJSON LineString needs two", len(line), len(c.Walk)))
			continue
		}
		f.begin(inNetworkName(base, c.Name), c.Notes)
		f.lineString(line)
		f.end()
	}
}
