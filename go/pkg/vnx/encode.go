package vnx

import (
	"bytes"
	"fmt"
	"math"
	"time"
)

// Encode writes a Document as a VNX file, in canonical form (spec §4.5).
//
// It always emits canonical bytes rather than offering a choice. Two exports of
// the same library must be byte-identical or diffs are unreadable and an agent
// cannot tell its own edit from formatting churn — and a writer that CAN emit
// non-canonical output is a writer that eventually does.
//
// Non-finite numbers are refused here rather than written: JSON has no
// spelling for them, and the alternative to refusing is emitting a `NaN`
// literal that every conforming reader then rejects, turning a writer's bug
// into an unreadable file the user only discovers later.
// Encode is a bytes.Buffer over [EncodeTo], and nothing more. It keeps its
// exact signature so that the committed goldens go on calling the same
// function they always called.
//
// ⚠ DO NOT OVERSTATE WHAT THAT BUYS. The name is preserved; the implementation
// underneath it is entirely new, so a green golden is a statement about the
// output equality of new code, not about "the same entry point" in any sense
// that survives a total rewrite. The weight is carried by the 37 frozen oracle
// expectations, which is a property over many documents rather than two
// fixtures.
//
// Callers that have somewhere to write should use EncodeTo: this function
// necessarily holds the whole output, which is the memory spec 4b exists to
// stop paying.
// emitter carries the one decision that separates a WRITER from a FORMATTER.
//
// A writer turns a model into a document: spec §4.5.4 has it round coordinates
// at that boundary, and §4.1 has it emit the shortest spelling of a position.
// A formatter re-renders a document a reader was handed, so it does neither —
// it reproduces the arity and the precision the document carried, because
// canonical form constrains key order and number SPELLING, not precision, and
// shortening is a writer's rule rather than part of canonical form.
//
// ⚠ IT IS A STRUCT AND NOT A bool PARAMETER ON PURPOSE. arcTree already takes
// a `named bool`, and a second adjacent bool is a silent transposition waiting
// to happen in a package whose whole job is byte-exact output.
type emitter struct{ verbatim bool }

// Reformat re-renders a document that was DECODED, reproducing it exactly:
// every position keeps the arity and the precision the file carried.
//
// ⚠ IT IS NOT Encode, AND THE DIFFERENCE IS THE POINT. Encode is a WRITER --
// spec §4.5.4 has it round coordinates at the emit boundary and §4.1 has it
// emit the shortest spelling of a position, so Encode(Decode(f)) is legitimately
// not f. Reformat is the FORMATTER: §4.5.4 says a reader and a formatter round
// nothing, and shortening is likewise a writer's rule rather than part of
// canonical form.
//
// This is what the conformance corpus's accept tier compares against. That
// check has to build a MODEL -- canonicalising is bytes in and bytes out and
// cannot see a reader that discarded what it read -- while still reproducing the
// file, which only a formatter does.
func Reformat(doc *Document) ([]byte, error) {
	var b bytes.Buffer
	if err := encodeTo(emitter{verbatim: true}, &b, doc); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func Encode(doc *Document) ([]byte, error) {
	var b bytes.Buffer
	if err := EncodeTo(&b, doc); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// waypointTree writes a top-level waypoint or a network's point. They are one
// shape (spec §3.6) apart from `folder`, which §3.10 does not allow on an
// in-network point: a network already contains its points, so a folder there
// would place an item that is not placed. `inNetwork` is what keeps that from
// depending on the caller's discipline — the two uses share this function, so
// nothing else could.
func (e emitter) waypointTree(w Waypoint, inNetwork bool) (map[string]any, error) {
	o := map[string]any{}
	putString(o, "name", w.Name)
	putString(o, "notes", w.Notes)
	if !inNetwork {
		putString(o, "folder", w.Folder)
	}
	putBool(o, "hidden", w.Hidden)
	at, err := e.positionTree(w.At)
	if err != nil {
		return nil, err
	}
	o["at"] = at
	if s := pointStyleTree(w.Style); len(s) > 0 {
		o["style"] = s
	}
	if err := putData(o, w.Data); err != nil {
		return nil, err
	}
	return o, nil
}

// areaTree writes a top-level area or a network's area. `folder` is not allowed
// on an in-network area for the same reason as an in-network point; see
// waypointTree.
func (e emitter) areaTree(a Area, inNetwork bool) (map[string]any, error) {
	o := map[string]any{}
	putString(o, "name", a.Name)
	putString(o, "notes", a.Notes)
	if !inNetwork {
		putString(o, "folder", a.Folder)
	}
	putBool(o, "hidden", a.Hidden)
	ring, err := e.positionsTree(a.Polygon)
	if err != nil {
		return nil, err
	}
	o["polygon"] = ring
	if s := areaStyleTree(a.Style); len(s) > 0 {
		o["style"] = s
	}
	if err := putData(o, a.Data); err != nil {
		return nil, err
	}
	return o, nil
}

func (e emitter) trackTree(t Track) (map[string]any, error) {
	o := map[string]any{}
	putString(o, "name", t.Name)
	putString(o, "notes", t.Notes)
	putString(o, "folder", t.Folder)
	putBool(o, "hidden", t.Hidden)
	if t.Started != nil {
		o["started"] = formatTimestamp(*t.Started)
	}
	if t.Ended != nil {
		o["ended"] = formatTimestamp(*t.Ended)
	}
	segs := make([]any, 0, len(t.Segments))
	for _, s := range t.Segments {
		pips, err := e.positionsTree(s.Pips)
		if err != nil {
			return nil, err
		}
		segs = append(segs, map[string]any{"pips": pips})
	}
	o["segments"] = segs
	if s := chainStyleTree(t.Style); len(s) > 0 {
		o["style"] = s
	}
	if err := putData(o, t.Data); err != nil {
		return nil, err
	}
	return o, nil
}

// formatTimestamp writes §4.3's grammar: UTC, `Z`, and OPTIONAL fractional
// seconds — emitted only when there are any.
//
// ⚠ SECONDS-ONLY TRUNCATED SILENTLY. The column behind these is UnixMilli, and
// formatting with a seconds-only layout threw the milliseconds away on the way
// out; the reader then parsed back a different instant, and because BOTH sides
// truncated, a byte-comparison round trip could not see it. Bounded at under a
// second, and wrong for no reason — the grammar already allowed the digits.
func formatTimestamp(t time.Time) string {
	t = t.UTC()
	if t.Nanosecond() == 0 {
		return t.Format("2006-01-02T15:04:05Z")
	}
	// Trailing zeros trimmed, so a whole number of milliseconds does not
	// render as .123000000 — one fact, one spelling, same rule as the numbers.
	return t.Format("2006-01-02T15:04:05.999999999Z")
}

func (e emitter) routeTree(r Route) (map[string]any, error) {
	o := map[string]any{}
	putString(o, "name", r.Name)
	putString(o, "notes", r.Notes)
	putString(o, "folder", r.Folder)
	putBool(o, "hidden", r.Hidden)
	arcs := make([]any, 0, len(r.Arcs))
	for _, a := range r.Arcs {
		ao, err := e.arcTree(a, false)
		if err != nil {
			return nil, err
		}
		arcs = append(arcs, ao)
	}
	o["arcs"] = arcs
	if s := chainStyleTree(r.Style); len(s) > 0 {
		o["style"] = s
	}
	if err := putData(o, r.Data); err != nil {
		return nil, err
	}
	return o, nil
}

// chainTree writes one chain — a per-item builder, like every other function
// in this file. There is no networkTree: a network's own object is scaffolding
// with the pips inside it, so streamNetwork writes it directly rather than
// materialising it (encode_stream.go). Keeping a second whole-network builder
// beside it would be a second implementation of the container order with
// nothing left to compare it against.
func (e emitter) chainTree(c Chain) (map[string]any, error) {
	co := map[string]any{}
	putString(co, "name", c.Name)
	putString(co, "notes", c.Notes)
	putBool(co, "hidden", c.Hidden)
	links := make([]any, 0, len(c.Links))
	for _, l := range c.Links {
		lo := map[string]any{"arc": l.Arc}
		from, err := e.endpointTree(l.From)
		if err != nil {
			return nil, err
		}
		to, err := e.endpointTree(l.To)
		if err != nil {
			return nil, err
		}
		lo["from"], lo["to"] = from, to
		links = append(links, lo)
	}
	co["links"] = links
	if s := chainStyleTree(c.Style); len(s) > 0 {
		co["style"] = s
	}
	if err := putData(co, c.Data); err != nil {
		return nil, err
	}
	return co, nil
}

// arcTree writes an arc. `from` and `to` appear only on a NETWORK arc; a
// route's arcs own their geometry and name nothing, so writing ids there would
// invent a graph the route does not have.
func (e emitter) arcTree(a Arc, named bool) (map[string]any, error) {
	o := map[string]any{}
	if named {
		if a.From == "" || a.To == "" {
			return nil, fmt.Errorf("vnx: a network arc must name both its nodes, got from=%q to=%q", a.From, a.To)
		}
		o["from"], o["to"] = a.From, a.To
	}
	pips, err := e.positionsTree(a.Pips)
	if err != nil {
		return nil, err
	}
	o["pips"] = pips
	if err := putData(o, a.Data); err != nil {
		return nil, err
	}
	return o, nil
}

func (e emitter) endpointTree(ep Endpoint) (any, error) {
	if ep.At != nil {
		return e.positionTree(*ep.At)
	}
	if ep.Node == "" {
		return nil, fmt.Errorf("vnx: an endpoint must be a node id or a position, and this is neither")
	}
	return ep.Node, nil
}

// positionTree writes the fixed positional channels, trimming trailing absence.
//
// Trailing `null`s are OMITTED rather than written: [lon, lat, null, null] and
// [lon, lat] say exactly the same thing, and the shorter spelling is the one a
// WRITER emits. An interior absence still needs its null — [lon, lat, null, t]
// cannot be shortened without moving the time into the elevation channel.
//
// ⚠ THIS IS THE WRITER. A FORMATTER MUST NOT SHORTEN, and does not exist yet.
// Spec §4.1 makes shortening a writer's rule rather than part of canonical
// form, so re-rendering a document a reader was handed has to reproduce the
// arity it carried — which is what Position.Channels records and what nothing
// currently reads. See the Reformat work: until it lands, decode-then-Encode
// is a WRITE and legitimately shortens and rounds, and the accept tier cannot
// use it as its check.
//
// ⚠ THIS IS WHERE VNX ROUNDS, and it is the only place: every position on a
// VNX wire — a waypoint's `at`, an area's ring, a track's pips, an arc's pips,
// a node, an inline endpoint — is built here or by positionsTree, which is
// this function in a loop. Rounding in the Document instead would round the
// values a route's arc-boundary dedup and a ring's winding are computed from.
// See round.go.
func (e emitter) positionTree(p Position) ([]any, error) {
	if !e.verbatim {
		p = RoundPosition(p)
	}
	if err := finite(p.Lon, "longitude"); err != nil {
		return nil, err
	}
	if err := finite(p.Lat, "latitude"); err != nil {
		return nil, err
	}
	// A formatter reproduces the arity the document carried; a writer emits the
	// shortest spelling. Position.Channels is zero for a position built in code,
	// which has no spelling of its own, so a formatter shortens those too.
	want := 0
	if e.verbatim {
		want = p.Channels
	}

	out := []any{p.Lon, p.Lat}
	if p.Ele != nil {
		if err := finite(*p.Ele, "elevation"); err != nil {
			return nil, err
		}
		out = append(out, *p.Ele)
	} else if p.Time != nil || want >= 3 {
		out = append(out, nil)
	}
	if p.Time != nil {
		out = append(out, *p.Time)
	} else if want >= 4 {
		out = append(out, nil)
	}
	return out, nil
}

func (e emitter) positionsTree(ps []Position) ([]any, error) {
	out := make([]any, 0, len(ps))
	for _, p := range ps {
		t, err := e.positionTree(p)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func pointStyleTree(s PointStyle) map[string]any {
	o := map[string]any{}
	putString(o, "color", s.Color)
	putString(o, "icon", s.Icon)
	putString(o, "pin-shape", s.PinShape)
	putString(o, "label-position", s.LabelPosition)
	if s.IconSize != nil {
		o["icon-size"] = *s.IconSize
	}
	if s.Importance != nil {
		o["importance"] = *s.Importance
	}
	// Tristate: nil means "the renderer decides", which is spelled by the key
	// being absent. Writing false for nil would turn auto into always-hidden.
	if s.LabelVisible != nil {
		o["label-visible"] = *s.LabelVisible
	}
	return o
}

func areaStyleTree(s AreaStyle) map[string]any {
	o := map[string]any{}
	putString(o, "fill-color", s.FillColor)
	putString(o, "stroke-color", s.StrokeColor)
	if s.FillOpacity != nil {
		o["fill-opacity"] = *s.FillOpacity
	}
	if s.StrokeWidth != nil {
		o["stroke-width"] = *s.StrokeWidth
	}
	return o
}

func chainStyleTree(s ChainStyle) map[string]any {
	o := map[string]any{}
	putString(o, "color", s.Color)
	putString(o, "line-style", s.LineStyle)
	if s.Opacity != nil {
		o["opacity"] = *s.Opacity
	}
	if s.Width != nil {
		o["width"] = *s.Width
	}
	return o
}

// putData writes the `data` bag, carrying each record's body VERBATIM. The
// body is opaque apart from @type, so it is written back exactly as it came in
// — that is what makes an unknown record survive read-modify-write.
func putData(o map[string]any, recs []Record) error {
	// ⚠ nil, NOT len == 0. Spec §3.10 generalises §3.1's rule beyond the
	// envelope: requiring a key does not require a non-empty value, and an
	// empty array is distinct from an absent one. `dataField` returns nil for an
	// absent key and a non-nil empty slice for `[]`, so the model already draws
	// the line and only this guard erased it.
	if recs == nil {
		return nil
	}
	arr, err := recordBodies(recs)
	if err != nil {
		return err
	}
	o["data"] = arr
	return nil
}

// recordBodies is putData's payload, shared with the streaming encoder. A
// `data` bag is bounded by the item that carries it, so it is built whole
// rather than streamed.
func recordBodies(recs []Record) ([]any, error) {
	arr := make([]any, 0, len(recs))
	for _, r := range recs {
		if r.Type == "" {
			return nil, fmt.Errorf("vnx: a data record must carry %q", TypeKey)
		}
		body := make(map[string]any, len(r.Body)+1)
		for k, v := range r.Body {
			body[k] = v
		}
		body[TypeKey] = r.Type
		arr = append(arr, body)
	}
	return arr, nil
}

func putString(o map[string]any, key, v string) {
	if v != "" {
		o[key] = v
	}
}

// putBool writes only `true`. `hidden: false` is the default state of every
// item in a library, and writing it on all of them would triple the size of a
// document to say nothing.
func putBool(o map[string]any, key string, v bool) {
	if v {
		o[key] = v
	}
}

func finite(f float64, what string) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("vnx: %s is not finite, and JSON cannot spell that", what)
	}
	return nil
}
