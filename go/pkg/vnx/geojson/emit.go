package geojson

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/dave/vnx/go/pkg/vnx"
)

// The JSON emission. It is hand-streamed rather than built as a tree and
// handed to json.Marshal, for the reason spec §4.1b gives: a whole-library
// track is large, and materialising it costs 6× the peak heap and 22% more
// time than streaming it. Everything here writes straight through the sticky
// writer and checks nothing; [Write] makes the one check at the end.
//
// ⚠ THE LAYOUT IS ONE FEATURE PER LINE, and that is for the golden. §6.1
// compares bytes, and a single-line FeatureCollection turns a one-feature
// change into a diff of the whole file that no reviewer can read. The features
// themselves stay compact: an indented position list would multiply a 2M-pip
// export by the width of the indent for nobody's benefit.

// begin opens a Feature and writes its properties. The caller writes the
// geometry next and then calls end.
func (f *features) begin(name, description string) {
	if f.emitted == 0 {
		f.sw.WriteString("\n")
	} else {
		f.sw.WriteString(",\n")
	}
	f.emitted++
	f.sw.WriteString(`{"type":"Feature","properties":{`)
	// ⚠ name AND description ONLY (§2.3). Tags, style and hidden state are
	// NOT here and must not be added: `properties` is schemaless, which is
	// exactly what makes an <extensions> block easy to rationalise into it.
	//
	// An EMPTY name is omitted rather than written as "". The item has no
	// name; `"name": ""` asserts it has one that is blank, and a consumer
	// showing an empty label is worse off than one showing none.
	sep := ""
	if name != "" {
		f.sw.WriteString(`"name":`)
		f.str(name)
		sep = ","
	}
	if description != "" {
		f.sw.WriteString(sep + `"description":`)
		f.str(description)
	}
	f.sw.WriteString(`},"geometry":`)
}

// end closes the Feature the last begin opened.
func (f *features) end() { f.sw.WriteString("}") }

func (f *features) str(s string) {
	// json.Marshal is the escaper. Hand-rolling one is how a lone U+2028 or a
	// control character becomes an unparseable file.
	b, err := json.Marshal(s)
	if err != nil {
		// A Go string is always marshalable; this cannot happen, and a silent
		// fallback to the raw bytes would emit invalid JSON if it ever did.
		f.sw.WriteString(`""`)
		return
	}
	f.sw.WriteBytes(b)
}

// number renders with ECMAScript semantics, including the negative-zero rule:
// JSON.stringify(-0) is "0", and a sign that carries no information is not worth
// two writers disagreeing about.
//
// ⚠ A NON-FINITE COORDINATE CANNOT REACH HERE FROM Decode — decodePosition
// refuses NaN and ±Inf — but a hand-built Document can carry one, and
// json.Marshal fails on it. Rendering it as null would put a null ordinate in
// a position array, which RFC 7946 forbids outright; 0 would put the point off
// Africa. Neither is a thing to do quietly, so the write is failed.
func (f *features) number(v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		f.fail(fmt.Errorf("geojson: coordinate %v is not finite", v))
		return
	}
	if v == 0 {
		v = 0 // -0 renders as 0
	}
	b, err := json.Marshal(v)
	if err != nil {
		f.fail(err)
		return
	}
	f.sw.WriteBytes(b)
}

// position writes one coordinate array.
//
// ⚠ THIS IS WHERE GeoJSON ROUNDS, and it is the only place — every Point,
// LineString, MultiLineString and Polygon ordinate goes through here. It sits
// AFTER the geometry: ringFor has already checked closure and fixed the
// winding, and appendJoined has already dropped the duplicate arc endpoint, all
// against full values. See vnx/round.go.
func (f *features) position(p vnx.Position) {
	p = vnx.RoundPosition(p)
	f.sw.WriteString("[")
	f.number(p.Lon)
	f.sw.WriteString(",")
	f.number(p.Lat)
	// ⚠ TWO ELEMENTS WHEN ELEVATION IS UNKNOWN (RFC 7946 §3.1.1). NEVER a
	// null third ordinate — the RFC does not allow one — and never 0, which
	// would silently put every unknown point at sea level. Time is not a
	// position channel in GeoJSON at all and is dropped; that is a matrix
	// fact, not a per-item casualty.
	if p.Ele != nil {
		f.sw.WriteString(",")
		f.number(*p.Ele)
	}
	f.sw.WriteString("]")
}

func (f *features) positions(ps []vnx.Position) {
	f.sw.WriteString("[")
	for i := range ps {
		if i > 0 {
			f.sw.WriteString(",")
		}
		f.position(ps[i])
	}
	f.sw.WriteString("]")
}

func (f *features) point(p vnx.Position) {
	f.sw.WriteString(`{"type":"Point","coordinates":`)
	f.position(p)
	f.sw.WriteString("}")
}

func (f *features) lineString(ps []vnx.Position) {
	f.sw.WriteString(`{"type":"LineString","coordinates":`)
	f.positions(ps)
	f.sw.WriteString("}")
}

func (f *features) multiLineString(lines [][]vnx.Position) {
	f.sw.WriteString(`{"type":"MultiLineString","coordinates":[`)
	for i, l := range lines {
		if i > 0 {
			f.sw.WriteString(",")
		}
		f.positions(l)
	}
	f.sw.WriteString("]}")
}

// polygon writes the ONE closed ring a [vnx.Area] is. The model has no holes
// and no multi-ring — item.go says so and validates it at every write site —
// so the coordinates array holds exactly one linear ring, and promising more
// here would be promising something the model cannot produce.
func (f *features) polygon(ring []vnx.Position) {
	f.sw.WriteString(`{"type":"Polygon","coordinates":[`)
	f.positions(ring)
	f.sw.WriteString("]}")
}
