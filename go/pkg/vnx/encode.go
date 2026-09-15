package vnx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

// The wire shapes, and the builders that fill them from the model.
//
// ⚠ FIELD ORDER IS KEY ORDER, AND THAT IS THE WHOLE MECHANISM. encoding/json
// emits a struct's fields in declaration order, so the readable ordering —
// identity first, geometry next, opaque payload last — costs one decision per
// type and no code at all. Spec §4.4 says key order is not significant, so this
// is a property of THIS implementation's output and is not promised to any
// other.
//
// ⚠ `omitempty` IS CONFORMING, AND §3.1 IS WHY. An absent collection and an
// empty one carry the same content and a reader treats them alike, so a writer
// may spell either. Nothing downstream may reintroduce a distinction between
// them.

// wirePosition is spec §4.1's positional channel array: [lon, lat, ele, time].
type wirePosition []any

type wireFolder struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Notes  string `json:"notes,omitempty"`
	Parent string `json:"parent,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
	Data   []any  `json:"data,omitempty"`
}

type wireWaypoint struct {
	Name   string       `json:"name,omitempty"`
	Notes  string       `json:"notes,omitempty"`
	Folder string       `json:"folder,omitempty"`
	Hidden bool         `json:"hidden,omitempty"`
	At     wirePosition `json:"at"`
	Style  Style        `json:"style,omitempty"`
	Data   []any        `json:"data,omitempty"`
}

type wireArea struct {
	Name    string         `json:"name,omitempty"`
	Notes   string         `json:"notes,omitempty"`
	Folder  string         `json:"folder,omitempty"`
	Hidden  bool           `json:"hidden,omitempty"`
	Polygon []wirePosition `json:"polygon"`
	Style   Style          `json:"style,omitempty"`
	Data    []any          `json:"data,omitempty"`
}

type wireSegment struct {
	Pips []wirePosition `json:"pips"`
}

type wireTrack struct {
	Name     string        `json:"name,omitempty"`
	Notes    string        `json:"notes,omitempty"`
	Folder   string        `json:"folder,omitempty"`
	Hidden   bool          `json:"hidden,omitempty"`
	Started  string        `json:"started,omitempty"`
	Ended    string        `json:"ended,omitempty"`
	Segments []wireSegment `json:"segments"`
	Style    Style         `json:"style,omitempty"`
	Data     []any         `json:"data,omitempty"`
}

// wireArc carries `from` and `to` only on a NETWORK arc. A route's arcs own
// their geometry and name nothing, so writing ids there would invent a graph
// the route does not have.
type wireArc struct {
	From string         `json:"from,omitempty"`
	To   string         `json:"to,omitempty"`
	Pips []wirePosition `json:"pips"`
	Data []any          `json:"data,omitempty"`
}

type wireRoute struct {
	Name   string    `json:"name,omitempty"`
	Notes  string    `json:"notes,omitempty"`
	Folder string    `json:"folder,omitempty"`
	Hidden bool      `json:"hidden,omitempty"`
	Arcs   []wireArc `json:"arcs"`
	Style  Style     `json:"style,omitempty"`
	Data   []any     `json:"data,omitempty"`
}

// wireLink's endpoints are `any` because §3.3.3 gives them two spellings: a
// node key is a string, an inline handle is a position array.
type wireLink struct {
	Arc  string `json:"arc"`
	From any    `json:"from"`
	To   any    `json:"to"`
}

type wireChain struct {
	Name   string     `json:"name,omitempty"`
	Notes  string     `json:"notes,omitempty"`
	Hidden bool       `json:"hidden,omitempty"`
	Links  []wireLink `json:"links"`
	Style  Style      `json:"style,omitempty"`
	Data   []any      `json:"data,omitempty"`
}

// Encode writes a Document as a VNX file.
//
// Non-finite numbers are refused rather than written: JSON has no spelling for
// them, and the alternative to refusing is emitting a `NaN` literal that every
// conforming reader then rejects, turning a writer's bug into an unreadable
// file the user only discovers later.
//
// Callers that have somewhere to write should use [EncodeTo]: this function
// necessarily holds the whole output.
func Encode(doc *Document) ([]byte, error) {
	var b bytes.Buffer
	if err := EncodeTo(&b, doc); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// waypointWire builds a top-level waypoint or a network's point. They are one
// shape (spec §3.6) apart from `folder`, which §3.10 does not allow on an
// in-network point: a network already contains its points, so a folder there
// would place an item that is not placed. `inNetwork` is what keeps that from
// depending on the caller's discipline — the two uses share this function, so
// nothing else could.
func waypointWire(w Waypoint, inNetwork bool) (wireWaypoint, error) {
	at, err := positionWire(w.At)
	if err != nil {
		return wireWaypoint{}, err
	}
	data, err := dataBodies(w.Data)
	if err != nil {
		return wireWaypoint{}, err
	}
	out := wireWaypoint{Name: w.Name, Notes: w.Notes, Hidden: w.Hidden, At: at, Style: w.Style, Data: data}
	if !inNetwork {
		out.Folder = w.Folder
	}
	return out, nil
}

// areaWire builds a top-level area or a network's area. `folder` is not allowed
// on an in-network area for the same reason as an in-network point; see
// waypointWire.
func areaWire(a Area, inNetwork bool) (wireArea, error) {
	ring, err := positionsWire(a.Polygon)
	if err != nil {
		return wireArea{}, err
	}
	data, err := dataBodies(a.Data)
	if err != nil {
		return wireArea{}, err
	}
	out := wireArea{Name: a.Name, Notes: a.Notes, Hidden: a.Hidden, Polygon: ring, Style: a.Style, Data: data}
	if !inNetwork {
		out.Folder = a.Folder
	}
	return out, nil
}

func trackWire(t Track) (wireTrack, error) {
	out := wireTrack{Name: t.Name, Notes: t.Notes, Folder: t.Folder, Hidden: t.Hidden, Style: t.Style}
	// Written verbatim, so what was read is what is written. A caller-built
	// value outside §4.3's grammar is an error rather than a document the
	// format forbids.
	for _, ts := range []struct {
		key string
		v   Timestamp
	}{{"started", t.Started}, {"ended", t.Ended}} {
		if ts.v == "" {
			continue
		}
		if err := ts.v.Valid(); err != nil {
			return wireTrack{}, fmt.Errorf("track %q %s: %w", t.Name, ts.key, err)
		}
	}
	out.Started = string(t.Started)
	out.Ended = string(t.Ended)
	out.Segments = make([]wireSegment, 0, len(t.Segments))
	for _, s := range t.Segments {
		pips, err := positionsWire(s.Pips)
		if err != nil {
			return wireTrack{}, err
		}
		out.Segments = append(out.Segments, wireSegment{Pips: pips})
	}
	data, err := dataBodies(t.Data)
	if err != nil {
		return wireTrack{}, err
	}
	out.Data = data
	return out, nil
}

func routeWire(r Route) (wireRoute, error) {
	out := wireRoute{Name: r.Name, Notes: r.Notes, Folder: r.Folder, Hidden: r.Hidden, Style: r.Style}
	out.Arcs = make([]wireArc, 0, len(r.Arcs))
	for _, a := range r.Arcs {
		aw, err := arcWire(a, false)
		if err != nil {
			return wireRoute{}, err
		}
		out.Arcs = append(out.Arcs, aw)
	}
	data, err := dataBodies(r.Data)
	if err != nil {
		return wireRoute{}, err
	}
	out.Data = data
	return out, nil
}

func chainWire(c Chain) (wireChain, error) {
	out := wireChain{Name: c.Name, Notes: c.Notes, Hidden: c.Hidden, Style: c.Style}
	out.Links = make([]wireLink, 0, len(c.Links))
	for _, l := range c.Links {
		from, err := endpointWire(l.From)
		if err != nil {
			return wireChain{}, err
		}
		to, err := endpointWire(l.To)
		if err != nil {
			return wireChain{}, err
		}
		out.Links = append(out.Links, wireLink{Arc: l.Arc, From: from, To: to})
	}
	data, err := dataBodies(c.Data)
	if err != nil {
		return wireChain{}, err
	}
	out.Data = data
	return out, nil
}

// arcWire builds an arc. `named` is true for a network arc, which must name
// both its nodes.
func arcWire(a Arc, named bool) (wireArc, error) {
	var out wireArc
	if named {
		if a.From == "" || a.To == "" {
			return wireArc{}, fmt.Errorf("vnx: a network arc must name both its nodes, got from=%q to=%q", a.From, a.To)
		}
		out.From, out.To = a.From, a.To
	}
	pips, err := positionsWire(a.Pips)
	if err != nil {
		return wireArc{}, err
	}
	out.Pips = pips
	data, err := dataBodies(a.Data)
	if err != nil {
		return wireArc{}, err
	}
	out.Data = data
	return out, nil
}

func endpointWire(ep Endpoint) (any, error) {
	if ep.At != nil {
		return positionWire(*ep.At)
	}
	if ep.Node == "" {
		return nil, fmt.Errorf("vnx: an endpoint must be a node id or a position, and this is neither")
	}
	return ep.Node, nil
}

// positionWire writes the fixed positional channels, trimming trailing absence.
//
// Trailing `null`s are OMITTED rather than written: [lon, lat, null, null] and
// [lon, lat] say exactly the same thing, and §4.1 has a writer emit the shorter
// one. An interior absence still needs its null — [lon, lat, null, t] cannot be
// shortened without moving the time into the elevation channel.
//
// ⚠ THIS WRITER DOES NOT ROUND, and that changed: it used to, because §4.1
// required it. The format no longer constrains a writer's precision, so a value
// handed to this function reaches the wire as it was given. An application that
// wants smaller files rounds its own document before encoding — [RoundPosition]
// is still here for that, as a utility rather than an obligation.
func positionWire(p Position) (wirePosition, error) {
	if err := finite(p.Lon, "longitude"); err != nil {
		return nil, err
	}
	if err := finite(p.Lat, "latitude"); err != nil {
		return nil, err
	}
	out := wirePosition{p.Lon, p.Lat}
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

func positionsWire(ps []Position) ([]wirePosition, error) {
	out := make([]wirePosition, 0, len(ps))
	for _, p := range ps {
		t, err := positionWire(p)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// dataBodies builds the `data` bag, carrying each record's body VERBATIM. The
// body is opaque apart from @type, so it is written back exactly as it came in
// — that is what makes an unknown record survive read-modify-write.
//
// A `data` bag is bounded by the item that carries it, so it is built whole
// rather than streamed.
func dataBodies(recs []Record) ([]any, error) {
	if len(recs) == 0 {
		return nil, nil
	}
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

func finite(f float64, what string) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("vnx: %s is not finite, and JSON cannot spell that", what)
	}
	return nil
}

// marshalCompact renders one value with encoding/json, with no whitespace at
// all.
//
// ⚠ THE DOCUMENT IS NOT INDENTED, AND THAT IS THE DECISION RATHER THAN AN
// OMISSION. Nothing about a VNX document's whitespace is constrained, and
// indenting costs about three times the file: a 200,000-pip track is 25.5 MB
// indented and 8.3 MB compact. The alternative — indenting the structure while
// keeping a position on one line — needs a re-indent pass over already-valid
// JSON, which is a custom emitter wearing a different hat and is the thing this
// library stopped having. Pretty-printing belongs to a TOOL, not to the format.
//
// ⚠ HTML ESCAPING IS OFF. Go escapes `<`, `>` and `&` by default so that its
// output can be pasted into a script tag. That is a sound default for a general
// encoder and needless noise in a document nobody embeds in HTML; both
// spellings decode to the same string, so this is a choice about size rather
// than about conformance.
//
// The trailing newline json.Encoder appends is removed here, because the caller
// is placing this value inside a larger document.
func marshalCompact(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	out := b.Bytes()
	return out[:len(out)-1], nil
}
