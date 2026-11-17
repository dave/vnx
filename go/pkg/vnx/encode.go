package vnx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
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
	Parent string `json:"parent,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

type wirePoint struct {
	Name   string       `json:"name,omitempty"`
	Folder string       `json:"folder,omitempty"`
	Hidden bool         `json:"hidden,omitempty"`
	Pinned string       `json:"pinned,omitempty"`
	At     wirePosition `json:"at"`
	Data   []any        `json:"data,omitempty"`
}

type wireArea struct {
	Name    string         `json:"name,omitempty"`
	Folder  string         `json:"folder,omitempty"`
	Hidden  bool           `json:"hidden,omitempty"`
	Pinned  string         `json:"pinned,omitempty"`
	Polygon []wirePosition `json:"polygon"`
	Data    []any          `json:"data,omitempty"`
}

type wireSegment struct {
	Pips []wirePosition `json:"pips"`
}

type wireTrack struct {
	Name     string        `json:"name,omitempty"`
	Folder   string        `json:"folder,omitempty"`
	Hidden   bool          `json:"hidden,omitempty"`
	Pinned   string        `json:"pinned,omitempty"`
	Started  string        `json:"started,omitempty"`
	Ended    string        `json:"ended,omitempty"`
	Segments []wireSegment `json:"segments"`
	Data     []any         `json:"data,omitempty"`
}

// wireArc is an arc of a network or a plan. `pips` is always written, empty
// for a straight arc: §3.10 makes the key required.
type wireArc struct {
	From string         `json:"from"`
	To   string         `json:"to"`
	Pips []wirePosition `json:"pips"`
	Data []any          `json:"data,omitempty"`
}

type wirePlan struct {
	Name   string                  `json:"name,omitempty"`
	Folder string                  `json:"folder,omitempty"`
	Hidden bool                    `json:"hidden,omitempty"`
	Pinned string                  `json:"pinned,omitempty"`
	Nodes  map[string]wirePosition `json:"nodes,omitempty"`
	Arcs   map[string]wireArc      `json:"arcs,omitempty"`
	Walk   []wirePass              `json:"walk"`
	Data   []any                   `json:"data,omitempty"`
}

// wirePass's `from` and `to` are `any` because §3.3.3 gives them two
// spellings: a node key is a string, a position is an array. A nil one is
// ABSENT, which is where §3.3.3's rule puts it; `turn` and `pin` are written
// only when the pass has one.
type wirePass struct {
	Arc  string       `json:"arc"`
	From any          `json:"from,omitempty"`
	To   any          `json:"to,omitempty"`
	Turn wirePosition `json:"turn,omitempty"`
	Pin  wirePosition `json:"pin,omitempty"`
}

type wireHighlight struct {
	Name   string     `json:"name,omitempty"`
	Hidden bool       `json:"hidden,omitempty"`
	Pinned string     `json:"pinned,omitempty"`
	Walk   []wirePass `json:"walk"`
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

// pointWire builds a top-level point or a network's point. They are one
// shape (spec §3.6) apart from `folder`, which §3.10 does not allow on an
// in-network point: a network already contains its points, so a folder there
// would place an item that is not placed. `inNetwork` is what keeps that from
// depending on the caller's discipline — the two uses share this function, so
// nothing else could.
func pointWire(w Point, inNetwork bool) (wirePoint, error) {
	at, err := positionWire(w.At)
	if err != nil {
		return wirePoint{}, err
	}
	data, err := dataBodies(w.Data)
	if err != nil {
		return wirePoint{}, err
	}
	pinned, err := pinnedWire("point", w.Name, w.Pinned)
	if err != nil {
		return wirePoint{}, err
	}
	out := wirePoint{Name: w.Name, Hidden: w.Hidden, Pinned: pinned, At: at, Data: data}
	if !inNetwork {
		out.Folder = w.Folder
	}
	return out, nil
}

// areaWire builds a top-level area or a network's area. `folder` is not allowed
// on an in-network area for the same reason as an in-network point; see
// pointWire.
func areaWire(a Area, inNetwork bool) (wireArea, error) {
	if err := checkRing(a.Polygon); err != nil {
		return wireArea{}, fmt.Errorf("area %q: %w", a.Name, err)
	}
	ring, err := positionsWire(a.Polygon)
	if err != nil {
		return wireArea{}, err
	}
	data, err := dataBodies(a.Data)
	if err != nil {
		return wireArea{}, err
	}
	pinned, err := pinnedWire("area", a.Name, a.Pinned)
	if err != nil {
		return wireArea{}, err
	}
	out := wireArea{Name: a.Name, Hidden: a.Hidden, Pinned: pinned, Polygon: ring, Data: data}
	if !inNetwork {
		out.Folder = a.Folder
	}
	return out, nil
}

// checkRing refuses a ring §3.7 would have a reader repair or drop: a writer
// emits a ring open, with at least three distinct positions. It does not open
// a closed ring itself — that would be the writer guessing its caller meant
// the repeat, which is the caller's to fix.
func checkRing(ring []Position) error {
	if n := len(ring); n >= 2 && ring[0].Lon == ring[n-1].Lon && ring[0].Lat == ring[n-1].Lat {
		return fmt.Errorf("vnx: a ring is written open, and this one's last position repeats its first")
	}
	if d := distinctLonLat(ring); d < 3 {
		return fmt.Errorf("vnx: a ring needs at least 3 distinct positions, this one has %d", d)
	}
	return nil
}

// pinnedWire is an item's `pinned` as it is written: verbatim, and absent when
// the item is not pinned (§3.8). A caller-built value outside §4.3's grammar
// is an error rather than a document the format forbids, as for a track's
// `started`.
func pinnedWire(kind, name string, t Timestamp) (string, error) {
	return pinnedWireOf(fmt.Sprintf("%s %q", kind, name), t)
}

// pinnedWireOf is pinnedWire for an item named by owner.
func pinnedWireOf(owner string, t Timestamp) (string, error) {
	if t == "" {
		return "", nil
	}
	if err := t.Valid(); err != nil {
		return "", fmt.Errorf("%s pinned: %w", owner, err)
	}
	return string(t), nil
}

func trackWire(t Track) (wireTrack, error) {
	pinned, err := pinnedWire("track", t.Name, t.Pinned)
	if err != nil {
		return wireTrack{}, err
	}
	out := wireTrack{Name: t.Name, Folder: t.Folder, Hidden: t.Hidden, Pinned: pinned}
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

func planWire(r Plan) (wirePlan, error) {
	pinned, err := pinnedWire("plan", r.Name, r.Pinned)
	if err != nil {
		return wirePlan{}, err
	}
	out := wirePlan{Name: r.Name, Folder: r.Folder, Hidden: r.Hidden, Pinned: pinned}
	if err := checkArcs(r.Nodes, r.Arcs); err != nil {
		return wirePlan{}, fmt.Errorf("plan %s: %w", QuoteName(r.Name), err)
	}
	if err := checkWalk(r.Walk, r.Arcs, true); err != nil {
		return wirePlan{}, fmt.Errorf("plan %s: %w", QuoteName(r.Name), err)
	}
	if len(r.Nodes) > 0 {
		out.Nodes = make(map[string]wirePosition, len(r.Nodes))
		for id, v := range r.Nodes {
			p, err := vertexWire(v)
			if err != nil {
				return wirePlan{}, err
			}
			out.Nodes[id] = p
		}
	}
	if len(r.Arcs) > 0 {
		out.Arcs = make(map[string]wireArc, len(r.Arcs))
		for id, a := range r.Arcs {
			aw, err := arcWire(a)
			if err != nil {
				return wirePlan{}, err
			}
			out.Arcs[id] = aw
		}
	}
	walk, err := walkWire(r.Walk)
	if err != nil {
		return wirePlan{}, err
	}
	out.Walk = walk
	data, err := dataBodies(r.Data)
	if err != nil {
		return wirePlan{}, err
	}
	out.Data = data
	return out, nil
}

// highlightWire builds highlight i of n. The walk is checked against n's arcs
// first: a writer never emits a walk its own reader would drop. A highlight's
// name is optional, so an error names it by its network and its index.
func highlightWire(n Network, i int, c Highlight) (wireHighlight, error) {
	owner := fmt.Sprintf("network %s highlight %d", QuoteName(n.Name), i)
	if err := checkWalk(c.Walk, n.Arcs, false); err != nil {
		return wireHighlight{}, fmt.Errorf("%s: %w", owner, err)
	}
	pinned, err := pinnedWireOf(owner, c.Pinned)
	if err != nil {
		return wireHighlight{}, err
	}
	out := wireHighlight{Name: c.Name, Hidden: c.Hidden, Pinned: pinned}
	walk, err := walkWire(c.Walk)
	if err != nil {
		return wireHighlight{}, err
	}
	out.Walk = walk
	data, err := dataBodies(c.Data)
	if err != nil {
		return wireHighlight{}, err
	}
	out.Data = data
	return out, nil
}

func walkWire(walk []Pass) ([]wirePass, error) {
	out := make([]wirePass, 0, len(walk))
	for _, p := range walk {
		wp := wirePass{Arc: p.Arc}
		var err error
		if p.From != nil {
			if wp.From, err = endpointWire(*p.From); err != nil {
				return nil, err
			}
		}
		if p.To != nil {
			if wp.To, err = endpointWire(*p.To); err != nil {
				return nil, err
			}
		}
		if p.Turn != nil {
			if wp.Turn, err = positionWire(p.Turn.Position()); err != nil {
				return nil, err
			}
		}
		if p.Pin != nil {
			if wp.Pin, err = positionWire(p.Pin.Position()); err != nil {
				return nil, err
			}
		}
		out = append(out, wp)
	}
	return out, nil
}

// arcWire builds an arc of a network or a plan. Its nodes were checked by
// checkArcs before any arc is written.
func arcWire(a Arc) (wireArc, error) {
	out := wireArc{From: a.From, To: a.To, Pips: make([]wirePosition, 0, len(a.Pips))}
	for _, v := range a.Pips {
		p, err := vertexWire(v)
		if err != nil {
			return wireArc{}, err
		}
		out.Pips = append(out.Pips, p)
	}
	data, err := dataBodies(a.Data)
	if err != nil {
		return wireArc{}, err
	}
	out.Data = data
	return out, nil
}

func vertexWire(v Vertex) (wirePosition, error) {
	return positionWire(v.Position())
}

func endpointWire(ep Endpoint) (any, error) {
	if ep.At != nil {
		return positionWire(ep.At.Position())
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

// QuoteName is s as a JSON string literal, without HTML escaping: the
// spelling every vnx library gives a name in an error, so the six read the
// same (Go's %q differs for control and non-ASCII characters). U+2028 and
// U+2029 are written raw, as the other five libraries write them:
// encoding/json escapes them even with SetEscapeHTML(false).
func QuoteName(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		// A string always encodes.
		panic(err)
	}
	return rawLineSeparators(strings.TrimSuffix(b.String(), "\n"))
}

// rawLineSeparators turns the escapes \u2028 and \u2029 in the JSON string
// literal q back into the raw characters. It reads q escape by escape, so the
// text \\u2028 (an escaped backslash, then "u2028") stays as it is.
func rawLineSeparators(q string) string {
	if !strings.Contains(q, `\u202`) {
		return q
	}
	var b strings.Builder
	for i := 0; i < len(q); i++ {
		if q[i] != '\\' || i+1 >= len(q) {
			b.WriteByte(q[i])
			continue
		}
		if esc := q[i:min(i+6, len(q))]; esc == `\u2028` || esc == `\u2029` {
			if esc == `\u2028` {
				b.WriteString("\u2028")
			} else {
				b.WriteString("\u2029")
			}
			i += 5
			continue
		}
		b.WriteString(q[i : i+2])
		i++
	}
	return b.String()
}
