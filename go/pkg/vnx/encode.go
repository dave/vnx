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
func Encode(doc *Document) ([]byte, error) {
	var b bytes.Buffer
	if err := EncodeTo(&b, doc); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func waypointTree(w Waypoint) (map[string]any, error) {
	o := map[string]any{}
	putString(o, "name", w.Name)
	putString(o, "notes", w.Notes)
	putString(o, "folder", w.Folder)
	putBool(o, "hidden", w.Hidden)
	at, err := positionTree(w.At)
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

func areaTree(a Area) (map[string]any, error) {
	o := map[string]any{}
	putString(o, "name", a.Name)
	putString(o, "notes", a.Notes)
	putString(o, "folder", a.Folder)
	putBool(o, "hidden", a.Hidden)
	ring, err := positionsTree(a.Polygon)
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

func trackTree(t Track) (map[string]any, error) {
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
		pips, err := positionsTree(s.Pips)
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

func routeTree(r Route) (map[string]any, error) {
	o := map[string]any{}
	putString(o, "name", r.Name)
	putString(o, "notes", r.Notes)
	putString(o, "folder", r.Folder)
	putBool(o, "hidden", r.Hidden)
	arcs := make([]any, 0, len(r.Arcs))
	for _, a := range r.Arcs {
		ao, err := arcTree(a, false)
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
func chainTree(c Chain) (map[string]any, error) {
	co := map[string]any{}
	putString(co, "name", c.Name)
	putString(co, "notes", c.Notes)
	putBool(co, "hidden", c.Hidden)
	links := make([]any, 0, len(c.Links))
	for _, l := range c.Links {
		lo := map[string]any{"arc": l.Arc}
		from, err := endpointTree(l.From)
		if err != nil {
			return nil, err
		}
		to, err := endpointTree(l.To)
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
func arcTree(a Arc, named bool) (map[string]any, error) {
	o := map[string]any{}
	if named {
		if a.From == "" || a.To == "" {
			return nil, fmt.Errorf("vnx: a network arc must name both its nodes, got from=%q to=%q", a.From, a.To)
		}
		o["from"], o["to"] = a.From, a.To
	}
	pips, err := positionsTree(a.Pips)
	if err != nil {
		return nil, err
	}
	o["pips"] = pips
	if err := putData(o, a.Data); err != nil {
		return nil, err
	}
	return o, nil
}

func endpointTree(e Endpoint) (any, error) {
	if e.At != nil {
		return positionTree(*e.At)
	}
	if e.Node == "" {
		return nil, fmt.Errorf("vnx: an endpoint must be a node id or a position, and this is neither")
	}
	return e.Node, nil
}

// positionTree writes the fixed positional channels, trimming trailing absence.
//
// Trailing `null`s are OMITTED rather than written: [lon, lat, null, null] and
// [lon, lat] say exactly the same thing, and the shorter spelling is the
// canonical one. An interior absence still needs its null — [lon, lat, null,
// t] cannot be shortened without moving the time into the elevation channel.
//
// ⚠ THIS IS WHERE VNX ROUNDS, and it is the only place: every position on a
// VNX wire — a waypoint's `at`, an area's ring, a track's pips, an arc's pips,
// a node, an inline endpoint — is built here or by positionsTree, which is
// this function in a loop. Rounding in the Document instead would round the
// values a route's arc-boundary dedup and a ring's winding are computed from.
// See round.go.
func positionTree(p Position) ([]any, error) {
	p = RoundPosition(p)
	if err := finite(p.Lon, "longitude"); err != nil {
		return nil, err
	}
	if err := finite(p.Lat, "latitude"); err != nil {
		return nil, err
	}
	out := []any{p.Lon, p.Lat}
	if p.Ele != nil {
		if err := finite(*p.Ele, "elevation"); err != nil {
			return nil, err
		}
		out = append(out, *p.Ele)
	} else if p.Time != nil {
		out = append(out, nil)
	}
	if p.Time != nil {
		out = append(out, *p.Time)
	}
	return out, nil
}

func positionsTree(ps []Position) ([]any, error) {
	out := make([]any, 0, len(ps))
	for _, p := range ps {
		t, err := positionTree(p)
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
	if len(recs) == 0 {
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
