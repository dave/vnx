// Package kml writes a [vnx.Document] as KML 2.2 (spec 5b §3.2, task T5).
//
// KML is an interchange format. Its job is to be read by Google Earth and by
// the other applications that speak it — not to be Viatrail's backup format,
// which is what VNX is for. So this writer is deliberately lossy and says what
// it lost: no <ExtendedData>, no invented namespaces (spec §2.3), and a
// diagnostic for every casualty (§2.2).
//
// ⚠ THIS PACKAGE IS STDLIB-ONLY APART FROM pkg/vnx, like pkg/vnx itself. No
// locerr, no db, no vtlog, and — the one that catches people —
// no pkg/core/importer either, which is why [Color] re-derives a conversion
// that already exists there.
//
// ⚠ .kmz IS WRITTEN, AND IT IS THIS WRITER'S BYTES IN A ZIP — NOT A SECOND
// WRITER. An earlier version of this comment said there was no .kmz "for the
// same reason there is no .vnz: a zip container earns its place when there is
// something to put beside the document". Dave overruled the premise on
// 2026-09-03 — the container is worth having for the SIZE alone, attachments
// or not — and .kmz and .vnz did fall due together, as predicted. What arrived
// is [vnx.WriteContainer] wrapping [Write] unchanged, so nothing about the
// KML this file produces depends on whether it is about to be deflated.
package kml

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/dave/vnx/go/pkg/vnx"
)

// RootDocumentName is the name a KMZ archive gives the KML inside it.
//
// ⚠ IT LIVES HERE, NOT IN pkg/vnx, BECAUSE IT IS A FACT ABOUT KML. pkg/vnx
// owns doc.vnx (vnx.RootDocumentName) and the container plumbing; which name a
// KMZ uses is this format's convention, and putting it in the generic layer
// would make the generic layer know about a format it does not otherwise write.
// It is the name importer/kmz.go prefers when it reads one back.
const RootDocumentName = "doc.kml"

// Write streams d to w as KML 2.2.
//
// It never refuses a document for what it cannot carry: it writes what fits and
// reports the rest (spec §2.2). The refusal that matters — a document this
// format reduced to nothing — is the CALLER's, made from
// [vnx.WriteReport.Emitted], because only the caller knows whether a file was
// about to be renamed into place.
//
// ⚠ THE ERROR RETURN IS THE SHORT-WRITE CHECK AND IT IS NOT OPTIONAL. An
// ignored short write means this function returns nil, the sink Syncs, sets
// `committed`, and renames a TRUNCATED file into place as a successful export
// (spec §4.1b, measured in 4b). [vnx.StickyWriter] latches the first failure
// and no-ops afterwards, so the single check below cannot be forgotten
// mid-walk. On error the report is ZERO, so a caller cannot act on counts taken
// from a walk that did not finish.
func Write(w io.Writer, d *vnx.Document) (vnx.WriteReport, error) {
	return WriteWith(w, d, vnx.WriteOptions{})
}

// WriteWith is Write with the labels the document does not carry: a network
// is a KML Folder named for it, so every item inside it is a Placemark named
// by its bare label (vnx.WriteOptions NetworkLabels: an item's name or its
// highlight label, or an integer unique within the network when it has none).
// Options that do not fit d are an error, and nothing is written.
func WriteWith(w io.Writer, d *vnx.Document, o vnx.WriteOptions) (vnx.WriteReport, error) {
	if err := o.Check(d); err != nil {
		return vnx.WriteReport{}, fmt.Errorf("kml: %w", err)
	}
	x := &writer{w: vnx.Stick(w), d: d, opts: o}
	x.document()
	if err := x.w.Err(); err != nil {
		return vnx.WriteReport{}, err
	}
	if x.err != nil {
		return vnx.WriteReport{}, x.err
	}
	return x.report, nil
}

type writer struct {
	w      *vnx.StickyWriter
	d      *vnx.Document
	opts   vnx.WriteOptions
	depth  int
	report vnx.WriteReport
	// err is the first geometry that could not be resolved: a walk or an arc
	// naming what its owner does not hold. Only a hand-built Document reaches
	// it, and the write fails rather than drawing a guess.
	err error
}

// fail latches the first geometry failure.
func (x *writer) fail(err error) {
	if x.err == nil {
		x.err = err
	}
}

func (x *writer) document() {
	l := arrange(x.d)
	x.w.WriteString(xml.Header)
	x.open("kml", ` xmlns="http://www.opengis.net/kml/2.2"`)
	x.open("Document", "")
	x.bucket(l, &l.root, false)
	// The folders whose ancestry does not resolve, written at the root so that
	// what they hold survives, and diagnosed so that the move is not silent.
	for _, i := range l.looseFolders {
		x.diag(vnx.CodeFolderPathUnavailable, fmt.Sprintf("/folders/%d", i), x.d.Folders[i].Name,
			"folder's ancestry does not resolve — written at the root instead of in the tree")
		x.folder(l, i)
	}
	// Items naming a folder this document does not have. Same rule: kept, moved,
	// diagnosed. Dropping content over a lost position in a tree would be a far
	// larger loss than the one being reported.
	x.bucket(l, &l.loose, true)
	// Application keys are last because they have no folder and no geometry:
	// they are not written in any format, only reported.
	x.report.Diagnostics = append(x.report.Diagnostics, vnx.AppKeyDiagnostics(x.d, "KML")...)
	x.close("Document")
	x.close("kml")
}

// bucket writes one container's contents: its folders first, then its items in
// a fixed kind order. The order is pinned rather than incidental — §6.1's
// golden is compared byte for byte, so anything that varies between runs shows
// up as a failure that looks like a writer bug and is not.
func (x *writer) bucket(l *layout, b *bucket, homeless bool) {
	for _, i := range b.folders {
		x.folder(l, i)
	}
	for _, i := range b.waypoints {
		path := fmt.Sprintf("/waypoints/%d", i)
		x.homeless(homeless, path, x.d.Waypoints[i].Name)
		x.waypoint(x.d.Waypoints[i])
	}
	for _, i := range b.areas {
		path := fmt.Sprintf("/areas/%d", i)
		x.homeless(homeless, path, x.d.Areas[i].Name)
		x.area(x.d.Areas[i], path)
	}
	for _, i := range b.tracks {
		path := fmt.Sprintf("/tracks/%d", i)
		x.homeless(homeless, path, x.d.Tracks[i].Name)
		x.track(x.d.Tracks[i], path)
	}
	for _, i := range b.plans {
		path := fmt.Sprintf("/plans/%d", i)
		x.homeless(homeless, path, x.d.Plans[i].Name)
		x.plan(x.d.Plans[i], path)
	}
	for _, i := range b.networks {
		path := fmt.Sprintf("/networks/%d", i)
		x.homeless(homeless, path, x.d.Networks[i].Name)
		x.network(x.d.Networks[i], i, path)
	}
}

// homeless records that an item names a folder this document does not have.
//
// ⚠ THE ITEM IS KEPT, at the root. Losing a position in a tree is not worth
// dropping content over — and it is not worth hiding either, which is the other
// half of the same rule and the reason this is not simply a quiet re-parenting.
func (x *writer) homeless(on bool, path, name string) {
	if !on {
		return
	}
	x.diag(vnx.CodeFolderPathUnavailable, path, name,
		"item names a folder that is not in this document — written at the root")
}

// folder writes a real nested <Folder>.
//
// ⚠ AN EMPTY FOLDER IS NOT A CASUALTY IN KML AND RAISES NO DIAGNOSTIC. The
// decision the brief asked for: <Folder> is a real element that carries a name,
// so an empty folder survives an export as an empty folder and NOTHING IS LOST.
// [vnx.CodeEmptyFolderDropped] is for the flattened formats, where a folder
// holding nothing has nowhere to put its name and genuinely vanishes; its own
// declaration in pkg/vnx says so ("KML does not raise this"). Raising it here
// would report a loss that did not happen.
//
// ⚠ A FOLDER IS NOT AN EMITTED OBJECT ([vnx.WriteReport.Emitted] counts things
// that put something on a map), so a KML of nothing but empty folders is still
// refused by the caller — which is the right answer for a file holding no
// geometry at all.
func (x *writer) folder(l *layout, i int) {
	f := x.d.Folders[i]
	x.open("Folder", "")
	x.feature(f.Name, f.Notes, f.Hidden)
	if b := l.byIndex[i]; b != nil {
		x.bucket(l, b, false)
	}
	x.close("Folder")
}

// feature writes the three <Feature> children every named thing shares, in the
// order the KML 2.2 schema requires: name, visibility, description. ⚠ THE ORDER
// IS THE SCHEMA'S, not a preference — kml22gx validators reject a Placemark
// whose children are out of sequence, and Google Earth accepts it, so getting
// it wrong is invisible in the one place people look.
//
// Absent values are omitted rather than written empty. <visibility> appears
// only to say 0: 1 is KML's default and writing it everywhere would treble the
// noise in a file people read.
func (x *writer) feature(name, notes string, hidden bool) {
	if name != "" {
		x.leaf("name", name)
	}
	if hidden {
		x.leaf("visibility", "0")
	}
	if notes != "" {
		x.leaf("description", notes)
	}
}

func (x *writer) waypoint(p vnx.Waypoint) {
	x.open("Placemark", "")
	x.feature(p.Name, p.Notes, p.Hidden)
	// A point carries no opacity in the model, so the alpha byte is opaque.
	pointColour, _ := p.Style.String("color")
	if c, ok := Color(pointColour, nil); ok {
		x.open("Style", "")
		x.open("IconStyle", "")
		x.leaf("color", c)
		x.close("IconStyle")
		x.close("Style")
	}
	x.open("Point", "")
	x.coordinates([]vnx.Position{p.At})
	x.close("Point")
	x.close("Placemark")
	x.report.Emitted++
}

func (x *writer) area(a vnx.Area, path string) {
	ring, ok := closedRing(a.Polygon)
	if !ok {
		// Writing a ring that cannot close produces a Polygon no reader can
		// draw — an invalid file landing as a success, which is the failure
		// this Code exists for.
		x.diag(vnx.CodeItemNoGeometry, path, a.Name, ringProblem(a.Polygon))
		return
	}
	x.areaPlacemark(a, ring)
}

// ringProblem says which of closedRing's two rules a ring broke.
func ringProblem(ring []vnx.Position) string {
	if len(ring) < 3 {
		return fmt.Sprintf("area ring has %d positions, needs at least 3", len(ring))
	}
	return "area ring is closed: a VNX ring is stored open, and its last position must not repeat the first"
}

// areaPlacemark writes an area whose ring has already been checked. It is
// shared by the top-level areas and the in-network ones, so the two cannot
// drift apart on style or visibility — the in-network ones only started
// carrying either when §2.6's redesign gave them a Placemark of their own.
func (x *writer) areaPlacemark(a vnx.Area, ring []vnx.Position) {
	x.open("Placemark", "")
	x.feature(a.Name, a.Notes, a.Hidden)
	x.areaStyle(a.Style)
	x.polygon(ring)
	x.close("Placemark")
	x.report.Emitted++
}

func (x *writer) polygon(ring []vnx.Position) {
	x.open("Polygon", "")
	x.open("outerBoundaryIs", "")
	x.open("LinearRing", "")
	x.coordinates(ring)
	x.close("LinearRing")
	x.close("outerBoundaryIs")
	x.close("Polygon")
}

// areaStyle writes the fill and the stroke, each only if its own colour is
// usable. ⚠ THE TWO ARE INDEPENDENT: an area with a fill colour and no stroke
// colour gets a PolyStyle and no LineStyle, rather than one default-coloured
// outline invented to keep the pair symmetrical.
//
// ⚠ IT READS A STYLE BY KEY RATHER THAN BY FIELD, because a style is opaque
// (spec §3.10). A key that is absent, or present with the wrong type, reads as
// absent here — this is a lossy conversion asking whether it has something it
// can use, not a reader diagnosing a document.
func (x *writer) areaStyle(s vnx.Style) {
	fillColor, _ := s.String("fill-color")
	fillOpacity, hasFillOpacity := s.Float("fill-opacity")
	var fo *float64
	if hasFillOpacity {
		fo = &fillOpacity
	}
	fill, hasFill := Color(fillColor, fo)
	// The format carries no stroke opacity, so a stroke is opaque.
	strokeColor, _ := s.String("stroke-color")
	stroke, hasStroke := Color(strokeColor, nil)
	if !hasFill && !hasStroke {
		return
	}
	x.open("Style", "")
	if hasStroke {
		x.open("LineStyle", "")
		x.leaf("color", stroke)
		if w, ok := s.Float("stroke-width"); ok {
			x.leaf("width", num(w))
		}
		x.close("LineStyle")
	}
	if hasFill {
		x.open("PolyStyle", "")
		x.leaf("color", fill)
		x.close("PolyStyle")
	}
	x.close("Style")
}

// lineStyle writes the <Style> for a track or a plan, drawn as a line. A
// highlight has no style of its own (§3.3.3), so it takes none. Width without a usable colour still gets a LineStyle: a width
// is not a colour and carrying it invents nothing.
func (x *writer) lineStyle(s vnx.Style) {
	colour, _ := s.String("color")
	opacity, hasOpacity := s.Float("opacity")
	var op *float64
	if hasOpacity {
		op = &opacity
	}
	c, ok := Color(colour, op)
	width, hasWidth := s.Float("width")
	if !ok && !hasWidth {
		return
	}
	x.open("Style", "")
	x.open("LineStyle", "")
	if ok {
		x.leaf("color", c)
	}
	if hasWidth {
		x.leaf("width", num(width))
	}
	x.close("LineStyle")
	x.close("Style")
}

// track writes a recorded hike: one <LineString> per segment inside one
// <MultiGeometry>.
//
// ⚠ THE SEGMENTS STAY SEPARATE AND THAT IS THE WHOLE POINT. The gap between
// segment k and k+1 is a PAUSE, never geometry; concatenating them draws a
// straight line across wherever the walker stopped for lunch. KML's
// MultiGeometry is what lets this survive, and it is one of only two cells this
// format carries without loss.
//
// The MultiGeometry is written even for a single segment, so every track has
// the same shape and a reader never has to branch on the count.
func (x *writer) track(t vnx.Track, path string) {
	var lines [][]vnx.Position
	var lostSegments []int
	for i, s := range t.Segments {
		// A one-pip segment is not a line and KML has nothing to make of it.
		if len(s.Pips) >= 2 {
			lines = append(lines, s.Pips)
			continue
		}
		lostSegments = append(lostSegments, i)
	}
	if len(lines) == 0 {
		x.diag(vnx.CodeItemNoGeometry, path, t.Name, "track has no segment with two or more pips")
		return
	}
	// ⚠ THE TRACK IS EXPORTED AND PART OF IT IS NOT, SO IT IS SAID (ruled
	// 2026-09-02). This writer used to drop the segment in silence, which is
	// the fallback the house rule forbids — and the whole-item Code was not the
	// answer either, because its English says "dropped" about a track the
	// reader can plainly see. The rows come first so they read in segment order
	// beside the item they belong to.
	for _, i := range lostSegments {
		x.diag(vnx.CodePartialGeometry, fmt.Sprintf("%s/segments/%d", path, i), t.Name,
			"segment has fewer than two pips and cannot be a LineString")
	}
	x.open("Placemark", "")
	x.feature(t.Name, t.Notes, t.Hidden)
	x.lineStyle(t.Style)
	x.open("MultiGeometry", "")
	for _, line := range lines {
		x.lineString(line)
	}
	x.close("MultiGeometry")
	x.close("Placemark")
	x.report.Emitted++
}

func (x *writer) plan(r vnx.Plan, path string) {
	line, err := vnx.WalkLine(r.Nodes, r.Arcs, r.Walk)
	if err != nil {
		x.fail(fmt.Errorf("kml: plan %q: %w", r.Name, err))
		return
	}
	if len(line) < 2 {
		x.diag(vnx.CodeItemNoGeometry, path, r.Name, "plan's walk covers fewer than two positions")
		return
	}
	x.open("Placemark", "")
	x.feature(r.Name, r.Notes, r.Hidden)
	x.lineStyle(r.Style)
	x.lineString(line)
	x.close("Placemark")
	x.report.Emitted++
}

// network writes a network as a <Folder> holding ONE Placemark for its arcs,
// then ONE NAMED PLACEMARK PER in-network point, area and highlight (§2.6, redesigned
// 2026-09-03).
//
// ⚠ THE ARCS PLACEMARK REPEATS THE FOLDER'S NAME ON PURPOSE. It is not
// redundancy to tidy away. It mirrors GeoJSON, where the arcs feature IS the
// network and carries its name, so the same library exported both ways reads the
// same; and an unnamed <Placemark> renders in Google Earth as a blank sidebar row
// a user can neither identify nor search for.
//
// ⚠ POINTS, AREAS AND HIGHLIGHTS ARE PLACEMARKS OF THEIR OWN BECAUSE A PLACEMARK HAS
// ONE NAME, ONE STYLE AND ONE VISIBILITY. vnx.Arc carries none of those — it is
// pure geometry — while Network.Points are full vnx.Waypoints and Network.Areas
// full vnx.Areas, each with Name, Notes, Hidden AND Style (item.go:34-55). Until
// this redesign all four fields were destroyed on every one of them: they rode as
// bare geometry inside the network's shared MultiGeometry. Folding any of them
// back in would destroy them again, silently.
//
// ⚠ THIS DUPLICATES GEOMETRY — a highlight's line covers arcs the arcs Placemark also
// emits — AND THAT IS ACCEPTED AND DELIBERATE. Do NOT "optimise" it by dropping
// the bare arcs, and do NOT quieten the display with <visibility>0</visibility> on
// the arcs Placemark: either would silently hide an arc no highlight traverses, the
// exact loss this shape exists to prevent.
//
// ⚠ WHAT KML CANNOT SAY IS THE SHARING. Two highlights crossing the same trail
// reference the same arc, and no target format can express that; the geometry
// survives, the graph does not. That is a class-level fact for the matrix and the
// export screen, NOT a per-item diagnostic — one per highlight would bury the real
// casualties.
func (x *writer) network(n vnx.Network, index int, path string) {
	labels, err := x.opts.NetworkLabels(x.d, index)
	if err != nil {
		x.fail(fmt.Errorf("kml: %w", err))
		return
	}
	arcIDs := make([]string, 0, len(n.Arcs))
	for id := range n.Arcs {
		arcIDs = append(arcIDs, id)
	}
	// ⚠ SORTED BECAUSE Arcs IS A MAP. Ranging a map emits in Go's randomised
	// order and §6.1's byte-compared golden goes flaky — a failure that looks
	// like a writer bug and is not.
	sort.Strings(arcIDs)

	var lines [][]vnx.Position
	for _, id := range arcIDs {
		// Node to node: an arc's line is at least its two ends.
		line, err := vnx.ArcLine(n.Nodes, n.Arcs[id])
		if err != nil {
			x.fail(fmt.Errorf("kml: network %q arc %q: %w", n.Name, id, err))
			return
		}
		lines = append(lines, line)
	}
	// The areas are checked BEFORE the network's own emptiness is decided,
	// because a network whose only content is an unusable ring holds nothing —
	// and because the ring's own casualty is reported in walk order either way.
	drawableAreas := map[int][]vnx.Position{}
	var areaOrder []int
	for i, a := range n.Areas {
		ring, ok := closedRing(a.Polygon)
		if !ok {
			x.diag(vnx.CodeItemNoGeometry, fmt.Sprintf("%s/areas/%d", path, i), labels.Areas[i],
				"in-network "+ringProblem(a.Polygon))
			continue
		}
		drawableAreas[i] = ring
		areaOrder = append(areaOrder, i)
	}

	if len(lines)+len(n.Points)+len(drawableAreas) == 0 {
		// The whole network object is gone, Folder and all. The arcs are not
		// "partly" lost — reporting them as well would double-report one loss.
		x.diag(vnx.CodeItemNoGeometry, path, n.Name, "network holds no drawable arc, point or area")
		// ⚠ THE HIGHLIGHTS STILL RUN. A network with no arc can only hold highlights
		// with empty walks, each of which is wholly lost and writes nothing;
		// without this loop those whole-item casualties would go unreported.
		for i, c := range n.Highlights {
			x.highlight(c, labels.Highlights[i], fmt.Sprintf("%s/highlights/%d", path, i), n)
		}
		return
	}

	x.open("Folder", "")
	// The Folder is where the network's own name, notes and visibility live.
	// KML visibility inherits down the tree, so hiding the network here hides
	// everything inside it and no child has to repeat the state.
	x.feature(n.Name, n.Notes, n.Hidden)
	if len(lines) > 0 {
		// ⚠ NO <description> AND NO <visibility> HERE. Both belong to the Folder
		// above; repeating the notes would show them twice in Google Earth's
		// sidebar, and repeating the visibility would say nothing the folder has
		// not already said.
		x.open("Placemark", "")
		x.feature(n.Name, "", false)
		x.open("MultiGeometry", "")
		for _, line := range lines {
			x.lineString(line)
		}
		x.close("MultiGeometry")
		x.close("Placemark")
		x.report.Emitted++
	}
	// Each item is named by its label (an integer when it has no name).
	for j, p := range n.Points {
		p.Name = labels.Points[j]
		x.waypoint(p)
	}
	for _, i := range areaOrder {
		a := n.Areas[i]
		a.Name = labels.Areas[i]
		x.areaPlacemark(a, drawableAreas[i])
	}
	for i, c := range n.Highlights {
		x.highlight(c, labels.Highlights[i], fmt.Sprintf("%s/highlights/%d", path, i), n)
	}
	x.close("Folder")
}

// highlight writes one Placemark for a highlight, named by label: the label the
// caller gave it, or its integer within the network (vnx.WriteOptions
// NetworkLabels). A highlight has no name, notes or style of its own (§3.3.3),
// so the Placemark carries none of them.
//
// ⚠ THE LABEL IS BARE. Before the 2026-09-03 redesign it was
// "Network / Highlight", because the network was a Placemark and nesting could not
// say which network a highlight belonged to. The network is a <Folder> now and the
// nesting says it, so the prefix would be the folder's name written twice.
//
// ⚠ HIDDEN IS THE HIGHLIGHT'S ALONE, for the same reason. The Placemark is a CHILD
// of the network's Folder rather than a sibling of a network Placemark, and KML
// visibility inherits down a folder, so a hidden network already hides it.
// ORing the network's state in here would hide a visible highlight the moment its
// network was hidden AND make it impossible to unhide in Google Earth.
func (x *writer) highlight(c vnx.Highlight, label, path string, n vnx.Network) {
	// ⚠ A WALK THAT DOES NOT RESOLVE FAILS THE WRITE. The reader drops such a
	// highlight, so only a hand-built Document reaches here with one, and drawing
	// part of it would put a line on the map nobody walked.
	line, err := vnx.WalkLine(n.Nodes, n.Arcs, c.Walk)
	if err != nil {
		x.fail(fmt.Errorf("kml: network %q %s: %w", n.Name, path, err))
		return
	}
	if len(line) < 2 {
		x.diag(vnx.CodeItemNoGeometry, path, label, "highlight's walk covers fewer than two positions")
		return
	}
	x.open("Placemark", "")
	x.feature(label, "", c.Hidden)
	x.lineString(line)
	x.close("Placemark")
	x.report.Emitted++
}

func (x *writer) lineString(ps []vnx.Position) {
	x.open("LineString", "")
	x.coordinates(ps)
	x.close("LineString")
}

// coordinates writes KML's coordinate list.
//
// ⚠ THE ORDER IS lon,lat[,alt] — THE OPPOSITE OF EVERYWHERE ELSE IN THIS
// CODEBASE, which says lat then lon. A writer that swaps them produces a file
// that opens without complaint and puts Scotland in the Indian Ocean.
//
// ⚠ AN UNKNOWN ELEVATION OMITS THE THIRD ORDINATE AND NEVER WRITES 0. Zero is
// sea level, and writing it would silently relocate every unknown point there —
// the exact failure vnx's pipEle exists to prevent on the VNX side. KML has no
// way to say "unknown", which is why the matrix marks this cell as a loss.
// ⚠ THIS IS WHERE KML ROUNDS, and it is the only place: every Point, LineString
// and LinearRing goes through here. `num` itself must NOT round, because it also
// spells a style's stroke width, which is not a coordinate (vnx/round.go).
func (x *writer) coordinates(ps []vnx.Position) {
	x.indent()
	x.w.WriteString("<coordinates>")
	for i, p := range ps {
		if i > 0 {
			x.w.WriteString(" ")
		}
		p = vnx.RoundPosition(p)
		x.w.WriteString(num(p.Lon))
		x.w.WriteString(",")
		x.w.WriteString(num(p.Lat))
		if p.Ele != nil {
			x.w.WriteString(",")
			x.w.WriteString(num(*p.Ele))
		}
	}
	x.w.WriteString("</coordinates>\n")
}

// diag records one casualty, following the six conventions pinned in
// pkg/vnx/writer.go: Code always set, Path always the document pointer,
// Args[0] the item's own bare name, Severity always Warning, one per dropped
// ITEM, in walk order.
//
// An unnamed item passes no arg rather than a substitute: inventing
// "(untitled)" here would be untranslatable English on a French screen.
func (x *writer) diag(code, path, name, message string) {
	d := vnx.Diagnostic{Severity: vnx.Warning, Code: code, Path: path, Message: message}
	if name != "" {
		d.Args = []string{name}
	}
	x.report.Diagnostics = append(x.report.Diagnostics, d)
}

// num formats a coordinate or a width. 'f' with -1 precision is the shortest
// string that reads back as the same float64, so the golden is stable and no
// digit is invented. Coordinates arrive already rounded from [writer.coordinates];
// widths arrive full precision and stay that way — a stroke width is not a
// measurement of the ground.
func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func (x *writer) indent() {
	for i := 0; i < x.depth; i++ {
		x.w.WriteString("  ")
	}
}

func (x *writer) open(tag, attrs string) {
	x.indent()
	x.w.WriteString("<" + tag + attrs + ">\n")
	x.depth++
}

func (x *writer) close(tag string) {
	x.depth--
	x.indent()
	x.w.WriteString("</" + tag + ">\n")
}

// leaf writes an element whose text is escaped. ⚠ EVERY USER STRING GOES
// THROUGH xml.EscapeText: a folder called "Ben & Nevis" writes a file no parser
// will open, and the export would land as a success.
func (x *writer) leaf(tag, text string) {
	x.indent()
	x.w.WriteString("<" + tag + ">")
	// EscapeText's error can only come from the writer, which latches it.
	_ = xml.EscapeText(x.w, []byte(text))
	x.w.WriteString("</" + tag + ">\n")
}
