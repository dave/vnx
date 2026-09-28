// Package gpx writes a [vnx.Document] as GPX 1.1 (spec 5b §3.1).
//
// ⚠ IT IS AN INTERCHANGE FORMAT, NOT A BACKUP. GPX carries points and lines
// and nothing else, so tags, styles, hidden state, arc boundaries, routing
// config, networks, areas and application keys do not survive. That is the ruling
// (§2.3), not a gap to be closed with an <extensions> block: every byte of a
// Viatrail-flavoured extension would be ignored by the Garmins and Gaias this
// format exists to talk to, and VNX already answers "I want all of it back".
//
// ⚠ THIS PACKAGE IMPORTS THE STANDARD LIBRARY AND pkg/vnx, NOTHING ELSE. It is
// reached from cmd/vnx, the public database-free CLI, whose purity_test.go
// enumerates its allow-set. TestImportsAreStdlibOnly in this directory is the
// local half of that guard.
package gpx

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"

	"github.com/dave/vnx/go/pkg/vnx"
)

// Write streams d as GPX 1.1 and reports what it emitted and what it dropped.
//
// It never refuses an unsupported item: §2.2 has a writer emit what fits and
// diagnose the rest. The empty-export refusal belongs to vnxbridge, which
// reads [vnx.WriteReport.Emitted] — see pkg/core/vnxbridge/formatwriter.go.
//
// An error means the bytes written so far are not a document. Every caller
// discards them: writeAtomically removes its temporary and never renames.
func Write(w io.Writer, d *vnx.Document) (vnx.WriteReport, error) {
	g := &writer{doc: d, b: vnx.Stick(w)}
	g.run()
	if g.err != nil {
		return vnx.WriteReport{}, g.err
	}
	// ⚠ THE STICKY WRITER'S ERROR IS RETURNED, ALWAYS. An ignored short write
	// means Write returns nil, the sink Syncs and renames, and a truncated
	// file lands as a successful export (stickywriter.go).
	if err := g.b.Err(); err != nil {
		return vnx.WriteReport{}, err
	}
	return vnx.WriteReport{Emitted: g.emitted, Diagnostics: g.diagnostics()}, nil
}

const (
	header = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<gpx version="1.1" creator="Viatrail" xmlns="http://www.topografix.com/GPX/1/1">` + "\n"
	footer = "</gpx>\n"
)

type writer struct {
	doc     *vnx.Document
	b       *vnx.StickyWriter
	err     error
	emitted int

	// Diagnostics are collected per collection and concatenated in DOCUMENT
	// ORDER by [writer.diagnostics]. They cannot simply be appended as the
	// walk goes, because the walk order and the document order differ — see
	// [writer.run].
	folderDiags   []vnx.Diagnostic
	waypointDiags []vnx.Diagnostic
	areaDiags     []vnx.Diagnostic
	trackDiags    []vnx.Diagnostic
	routeDiags    []vnx.Diagnostic
	networkDiags  []vnx.Diagnostic
	appKeyDiags   []vnx.Diagnostic
}

// run walks the document once and streams as it goes.
//
// ⚠ THE EMISSION ORDER IS THE SCHEMA'S, NOT THE DOCUMENT'S. gpxType is a
// SEQUENCE — metadata?, wpt*, rte*, trk*, extensions? — so every <wpt> must
// precede every <trk> or the file does not validate, however the document
// happened to be laid out. Interleaving them produces a file that opens in
// some readers and is rejected by any that validate, which is the worst of
// both.
//
// The DIAGNOSTICS still come out in document order (writer.go convention 6),
// which is why they are collected per collection rather than appended here.
func (g *writer) run() {
	g.collectFolderDiagnostics()
	g.b.WriteString(header)
	g.writeWaypoints()
	g.writeTracks()
	g.writeRoutes()
	g.b.WriteString(footer)
	g.collectAreaDiagnostics()
	g.collectNetworkDiagnostics()
	g.appKeyDiags = vnx.AppKeyDiagnostics(g.doc, "GPX")
}

// diagnostics concatenates in [vnx.Document] field order: folders, waypoints,
// areas, tracks, routes, networks, application keys.
func (g *writer) diagnostics() []vnx.Diagnostic {
	n := len(g.folderDiags) + len(g.waypointDiags) + len(g.areaDiags) +
		len(g.trackDiags) + len(g.routeDiags) + len(g.networkDiags) + len(g.appKeyDiags)
	if n == 0 {
		return nil
	}
	out := make([]vnx.Diagnostic, 0, n)
	out = append(out, g.folderDiags...)
	out = append(out, g.waypointDiags...)
	out = append(out, g.areaDiags...)
	out = append(out, g.trackDiags...)
	out = append(out, g.routeDiags...)
	out = append(out, g.networkDiags...)
	out = append(out, g.appKeyDiags...)
	return out
}

// -- the things GPX keeps ----------------------------------------------------

func (g *writer) writeWaypoints() {
	for i := range g.doc.Waypoints {
		if g.err != nil {
			return
		}
		w := &g.doc.Waypoints[i]
		path := fmt.Sprintf("/waypoints/%d", i)
		// A waypoint IS its position; vnx.Waypoint.At is a value and cannot be
		// absent, so CodeItemNoGeometry is unreachable here.
		name := g.flatName(path, w.Folder, w.Name, &g.waypointDiags)
		// Rounded at the write, never in the model (vnx/round.go).
		g.b.WriteString(`  <wpt lat="` + g.number(vnx.RoundCoordinate(w.At.Lat), "latitude") +
			`" lon="` + g.number(vnx.RoundCoordinate(w.At.Lon), "longitude") + `"`)
		if w.At.Ele == nil && w.At.Time == nil && name == "" && w.Notes == "" {
			g.b.WriteString("/>\n")
			g.emitted++
			continue
		}
		g.b.WriteString(">\n")
		// wptType is a sequence too: ele, time, … name, cmt, desc, ….
		g.writeEle("    ", w.At)
		g.writeTime("    ", w.At)
		g.writeText("    ", "name", name)
		g.writeText("    ", "desc", w.Notes)
		g.b.WriteString("  </wpt>\n")
		g.emitted++
	}
}

func (g *writer) writeTracks() {
	for i := range g.doc.Tracks {
		if g.err != nil {
			return
		}
		t := &g.doc.Tracks[i]
		path := fmt.Sprintf("/tracks/%d", i)
		// ⚠ ONE <trkseg> PER SEGMENT, NEVER ONE CONCATENATED LIST. The gap
		// between segment k and k+1 is a PAUSE and is not geometry; joining
		// them draws a straight line across wherever the walker stopped
		// (track.go). Pauses surviving is GPX's one real win over the others.
		segs := make([][]vnx.Position, 0, len(t.Segments))
		for _, s := range t.Segments {
			// A <trkseg> holding no <trkpt> is valid GPX that says nothing, so
			// it is not written. Nothing is lost: an empty segment carries no
			// position, no time and no pause boundary a reader could use.
			if len(s.Pips) == 0 {
				continue
			}
			segs = append(segs, s.Pips)
		}
		if len(segs) == 0 {
			g.trackDiags = append(g.trackDiags, noGeometry(path, t.Name))
			continue
		}
		name := g.flatName(path, t.Folder, t.Name, &g.trackDiags)
		g.writeTrk(name, t.Notes, segs)
	}
}

func (g *writer) writeRoutes() {
	for i := range g.doc.Routes {
		if g.err != nil {
			return
		}
		r := &g.doc.Routes[i]
		path := fmt.Sprintf("/routes/%d", i)
		pips := routePips(r)
		if len(pips) == 0 {
			g.routeDiags = append(g.routeDiags, noGeometry(path, r.Name))
			continue
		}
		name := g.flatName(path, r.Folder, r.Name, &g.routeDiags)
		// ⚠ A ROUTE IS A <trk>, NOT A <rte>, AND THIS IS LOAD-BEARING. GPX's
		// <rte> is a list of TURN CUES a device navigates between — sparse
		// points it draws its own line through. A Viatrail route is a dense
		// polyline that already follows the ground, which is a *track* in
		// GPX's vocabulary whatever we call it internally. Hundreds of dense
		// <rtept>s produce a file that validates and is useless on a device.
		g.writeTrk(name, r.Notes, [][]vnx.Position{pips})
	}
}

// routePips flattens a route's arcs into the single point list its one
// <trkseg> carries. Arc BOUNDARIES do not survive — GPX cannot express them —
// and that is a matrix fact, not a per-item casualty (writer.go convention 5).
//
// ⚠ THE SHARED ENDPOINT IS EMITTED ONCE. Arc k's last pip and arc k+1's first
// pip are the same node, present in BOTH pip lists by design (route.go: "node
// identity is the shared endpoint between arc k and arc k+1, present in both
// pip lists"). Writing it twice puts a zero-length step in the track, which
// some readers count as a stop.
//
// It is compared by full value, not by longitude and latitude alone: a repeat
// that differs in elevation or time is a second reading of the same place, and
// dropping it would lose data to a tidy-up.
func routePips(r *vnx.Route) []vnx.Position {
	var out []vnx.Position
	for _, a := range r.Arcs {
		for j := range a.Pips {
			if j == 0 && len(out) > 0 && out[len(out)-1].Equal(a.Pips[j]) {
				continue
			}
			out = append(out, a.Pips[j])
		}
	}
	return out
}

func (g *writer) writeTrk(name, notes string, segs [][]vnx.Position) {
	g.b.WriteString("  <trk>\n")
	// trkType sequence: name, cmt, desc, src, link, number, type, …, trkseg*.
	g.writeText("    ", "name", name)
	g.writeText("    ", "desc", notes)
	for _, pips := range segs {
		g.b.WriteString("    <trkseg>\n")
		for i := range pips {
			if g.err != nil {
				return
			}
			p := pips[i]
			g.b.WriteString(`      <trkpt lat="` + g.number(vnx.RoundCoordinate(p.Lat), "latitude") +
				`" lon="` + g.number(vnx.RoundCoordinate(p.Lon), "longitude") + `"`)
			if p.Ele == nil && p.Time == nil {
				g.b.WriteString("/>\n")
				continue
			}
			g.b.WriteString(">\n")
			g.writeEle("        ", p)
			g.writeTime("        ", p)
			g.b.WriteString("      </trkpt>\n")
		}
		g.b.WriteString("    </trkseg>\n")
	}
	g.b.WriteString("  </trk>\n")
	g.emitted++
}

// -- the things GPX drops ----------------------------------------------------

func (g *writer) collectAreaDiagnostics() {
	for i := range g.doc.Areas {
		g.areaDiags = append(g.areaDiags, vnx.Diagnostic{
			Severity: vnx.Warning,
			Code:     vnx.CodeGPXAreaUnsupported,
			Path:     fmt.Sprintf("/areas/%d", i),
			Args:     []string{g.doc.Areas[i].Name},
			Message:  "GPX has no polygon element — area not exported",
		})
	}
}

func (g *writer) collectNetworkDiagnostics() {
	for i := range g.doc.Networks {
		// ⚠ DO NOT INVENT A FLATTENING TO BE HELPFUL. Excluding networks is a
		// deliberate deferral with a named future (§2.6, Dave 2026-09-02), and
		// both obvious flattenings are bad: per-chain duplicates the geometry
		// two chains share, per-arc produces dozens of unnamed fragments.
		g.networkDiags = append(g.networkDiags, vnx.Diagnostic{
			Severity: vnx.Warning,
			Code:     vnx.CodeGPXNetworkUnsupported,
			Path:     fmt.Sprintf("/networks/%d", i),
			Args:     []string{g.doc.Networks[i].Name},
			Message:  "GPX cannot express a shared-arc network — network not exported",
		})
	}
}

// collectFolderDiagnostics reports the folders that genuinely vanish.
//
// ⚠ A NON-EMPTY FOLDER IS NOT A CASUALTY. Its name survives inside every
// contained item's flattened name (§2.7), so nothing was lost and a diagnostic
// would be noise on the common case — the case being every folder in a normal
// library.
//
// ⚠ "EMPTY" MEANS NO ITEM ANYWHERE BENEATH IT, and it is measured against the
// DOCUMENT, not against what GPX kept. A folder holding only areas is
// therefore not reported here, and that is deliberate: the areas already
// raised one diagnostic each, and a second row saying their folder went with
// them adds a casualty the user cannot act on separately.
func (g *writer) collectFolderDiagnostics() {
	if len(g.doc.Folders) == 0 {
		return
	}
	index := make(map[string]int, len(g.doc.Folders))
	for i := range g.doc.Folders {
		index[g.doc.Folders[i].ID] = i
	}
	occupied := make([]bool, len(g.doc.Folders))
	// mark makes id and every ancestor of id occupied.
	//
	// ⚠ WHAT TERMINATES THIS WALK IS THE MEMO, NOT THE COUNT BOUND, and the
	// difference was measured rather than reasoned: replacing `for range
	// g.doc.Folders` with `for {` leaves the cyclic-folder test passing. A
	// cycle revisits a folder that is already occupied and returns there.
	//
	// The bound stays anyway. Folder.Parent is a free string — resolveParents
	// clears cycles at DECODE, so a Document read from a file cannot be
	// cyclic, but a Document is an ordinary Go struct anyone may fill in — and
	// "a writer must not hang on any input" is worth a structural guarantee
	// that does not depend on the memo argument above still being true after
	// the next edit. It is a second lock, and it is documented as one rather
	// than as the lock.
	mark := func(id string) {
		i, ok := index[id]
		if !ok {
			return
		}
		for range g.doc.Folders {
			if occupied[i] {
				return // this folder and its ancestors are already marked
			}
			occupied[i] = true
			parent := g.doc.Folders[i].Parent
			if parent == "" {
				return
			}
			if i, ok = index[parent]; !ok {
				return
			}
		}
	}
	for i := range g.doc.Waypoints {
		mark(g.doc.Waypoints[i].Folder)
	}
	for i := range g.doc.Areas {
		mark(g.doc.Areas[i].Folder)
	}
	for i := range g.doc.Tracks {
		mark(g.doc.Tracks[i].Folder)
	}
	for i := range g.doc.Routes {
		mark(g.doc.Routes[i].Folder)
	}
	for i := range g.doc.Networks {
		mark(g.doc.Networks[i].Folder)
	}
	for i := range g.doc.Folders {
		if occupied[i] {
			continue
		}
		g.folderDiags = append(g.folderDiags, vnx.Diagnostic{
			Severity: vnx.Warning,
			Code:     vnx.CodeEmptyFolderDropped,
			Path:     fmt.Sprintf("/folders/%d", i),
			Args:     []string{g.doc.Folders[i].Name},
			Message:  "GPX is flat and the folder holds no items, so its name has nowhere to go — folder not exported",
		})
	}
}

func noGeometry(path, name string) vnx.Diagnostic {
	return vnx.Diagnostic{
		Severity: vnx.Warning,
		Code:     vnx.CodeItemNoGeometry,
		Path:     path,
		Args:     []string{name},
		Message:  "no positions to write — item not exported",
	}
}

// flatName folds the folder path into the item's name (§2.7). An unbuildable
// path keeps the item under its bare name and says so: losing a position in a
// tree GPX cannot express anyway is not worth dropping content over, and it is
// not worth hiding either.
func (g *writer) flatName(path, folder, name string, into *[]vnx.Diagnostic) string {
	flat, ok := vnx.FlatName(g.doc, folder, name)
	if !ok {
		*into = append(*into, vnx.Diagnostic{
			Severity: vnx.Warning,
			Code:     vnx.CodeFolderPathUnavailable,
			Path:     path,
			Args:     []string{name},
			Message:  "the folder path could not be built — item kept under its bare name",
		})
	}
	return flat
}

// -- rendering ---------------------------------------------------------------

// writeEle writes <ele> when the elevation is known.
//
// ⚠ AN UNKNOWN ELEVATION OMITS THE ELEMENT AND NEVER WRITES 0. A zero puts
// every unknown point at sea level, which reads as data rather than as
// absence — the exact failure the format's nil *float64 exists to prevent.
// Rounding does not touch that: the nil test comes first, so an absent
// elevation stays absent and never becomes 0.0.
func (g *writer) writeEle(indent string, p vnx.Position) {
	if p.Ele == nil {
		return
	}
	g.b.WriteString(indent + "<ele>" + g.number(vnx.RoundElevation(*p.Ele), "elevation") + "</ele>\n")
}

// writeTime writes <time> when the position carries one, ISO 8601 in UTC.
// Absent time omits the element; there is no zero to fall back to and none is
// invented.
func (g *writer) writeTime(indent string, p vnx.Position) {
	if p.Time == nil {
		return
	}
	g.b.WriteString(indent + "<time>" + formatTime(*p.Time) + "</time>\n")
}

// formatTime renders Unix milliseconds as ISO 8601 UTC. Whole seconds lose the
// fraction: "…:05Z" and "…:05.000Z" are the same instant, and the shorter
// spelling is what every other GPX in the wild carries.
func formatTime(ms int64) string {
	t := time.UnixMilli(ms).UTC()
	if ms%1000 == 0 {
		return t.Format("2006-01-02T15:04:05") + "Z"
	}
	return t.Format("2006-01-02T15:04:05.000") + "Z"
}

// writeText writes an escaped text element, or nothing when the value is
// empty. An empty <name/> says "this is named the empty string"; omission says
// "unnamed", which is what an absent value means.
func (g *writer) writeText(indent, tag, s string) {
	if s == "" {
		return
	}
	g.b.WriteString(indent + "<" + tag + ">")
	if err := xml.EscapeText(g.b, []byte(s)); err != nil {
		g.fail(err)
	}
	g.b.WriteString("</" + tag + ">\n")
}

// number renders a coordinate or an elevation. Its callers have ALREADY
// rounded (vnx/round.go); this only spells the float64 it is handed.
//
// 'f' with precision -1 is the shortest decimal that round-trips, and never an
// exponent: 1e-7 is a legal float and an illegal xsd:decimal, so a coordinate
// written in exponent form makes the file invalid.
//
// ⚠ A NON-FINITE VALUE IS AN ERROR, NOT A SUBSTITUTION. Decode refuses NaN and
// ±Inf, so only a hand-built Document reaches here with one; writing the Go
// spelling "NaN" into an attribute produces an invalid file reported as a
// successful export. The empty string it returns after failing is never
// committed — the latched error aborts before the sink renames.
func (g *writer) number(v float64, what string) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		g.fail(fmt.Errorf("gpx: %s is %v, which GPX cannot spell", what, v))
		return ""
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// fail latches the first error. Like the sticky writer it keeps the first one:
// a later failure is usually a consequence of the first, and reporting the
// consequence sends a reader to the wrong place.
func (g *writer) fail(err error) {
	if g.err == nil {
		g.err = err
	}
}
