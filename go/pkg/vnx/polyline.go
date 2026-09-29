package vnx

import (
	"fmt"
	"math"
)

// Cutting an arc where a walk enters, turns and leaves it. ONE implementation,
// [WalkLegs], and every writer draws a chain or a route through it.
//
// ⚠ A WALK IS NOT A LIST OF WHOLE ARCS. Each of its passes covers one arc from
// an entry to an exit, perhaps by way of a turn, and an entry or exit is either
// a node — the arc's own end — or a position part-way along it ([Endpoint]).
// Emitting the whole arc for a pass that uses part of it would draw the walk
// running past where the user stopped.
//
// ⚠ IT LIVES HERE BECAUSE TWO WRITERS MUST DRAW THE SAME LINE. It used to live
// in kml only, with geojson carrying a second answer that SNAPPED an inline
// endpoint to the nearest recorded pip instead of cutting the segment. One
// library then exported one chain as two different polylines, differing by up
// to half a segment at each end — the four-writers drift round.go's header
// warns about, made real. Following DocumentIsEmpty's discipline (a rule two
// callers must obey belongs in pkg/vnx), the answer is here and there is only
// one of it.

// cut is a point ON a polyline: the index of the segment it lies on, and how
// far along that segment it sits (0..1).
type cut struct {
	seg int
	t   float64
}

// before reports whether c comes earlier along the polyline than o.
func (c cut) before(o cut) bool {
	if c.seg != o.seg {
		return c.seg < o.seg
	}
	return c.t < o.t
}

// project finds the point on pips closest to p, at any distance (SPEC
// §3.3.3). Where two segments are equally near, the earlier one wins: `<`, not
// `<=`, is that rule.
func project(pips []Position, p Position) cut {
	best := cut{}
	bestDist := math.Inf(1)
	for i := 0; i+1 < len(pips); i++ {
		t, d := nearestOnSegment(p, pips[i], pips[i+1])
		if d < bestDist {
			bestDist, best = d, cut{seg: i, t: t}
		}
	}
	return best
}

// nearestOnSegment projects p onto the segment ab in a local planar frame and
// returns how far along it lands (0..1) and how far away it is, in metres.
// Over the length of one arc segment the flat-earth error is far below
// anything this decides, so a geodesic projection would buy nothing.
//
// ⚠ THE MODEL IS SPEC §3.3.3's, EXACTLY, AND IT IS NOT A FREE CHOICE.
// Equirectangular about the LATITUDE OF THE QUERY POINT — p, not a — held
// constant for every vertex, with the IUGG mean radius. The cosine reference is
// the part that matters: taking it from each segment's start instead reads an
// endpoint on a long north-south arc 8.5% further away, and five
// implementations have to agree to the metre. This function used to do exactly
// that, with a metresPerDegLat of 111132.0 against the 111194.9 this radius
// implies — a fifth undocumented model that nobody chose.
//
// ⚠ IT DECIDES WHERE EVERY WRITER CUTS A WALK. A reader stores a walk position
// as written; the nearest point it identifies is found here, and any other
// model would draw the same walk somewhere else.
func nearestOnSegment(p, a, b Position) (t, metres float64) {
	const earthRadiusMetres = 6371008.8
	rad := math.Pi / 180
	cosLat := math.Cos(p.Lat * rad)
	px := (p.Lon - a.Lon) * rad * earthRadiusMetres * cosLat
	py := (p.Lat - a.Lat) * rad * earthRadiusMetres
	bx := (b.Lon - a.Lon) * rad * earthRadiusMetres * cosLat
	by := (b.Lat - a.Lat) * rad * earthRadiusMetres
	lenSq := bx*bx + by*by
	if lenSq == 0 {
		return 0, math.Hypot(px, py)
	}
	t = (px*bx + py*by) / lenSq
	t = math.Max(0, math.Min(1, t))
	return t, math.Hypot(px-t*bx, py-t*by)
}

// at is the position a cut denotes.
//
// ⚠ AN INTERPOLATED POINT CARRIES NO ELEVATION AND NO TIME, and that is the
// same ruling as everywhere else in this format: absent is absent. We know
// where the walker turned round; we do not know how high it was there, and
// writing a number we derived would be indistinguishable, downstream, from one
// that was measured. A cut landing exactly ON a pip copies that pip whole, so
// the ordinary case — every node endpoint, and every inline endpoint that sits
// on a recorded point — keeps its elevation.
func at(pips []Position, c cut) Position {
	a, b := pips[c.seg], pips[c.seg+1]
	switch c.t {
	case 0:
		return a
	case 1:
		return b
	}
	return Position{
		Lon: a.Lon + (b.Lon-a.Lon)*c.t,
		Lat: a.Lat + (b.Lat-a.Lat)*c.t,
	}
}

// between is the piece of pips running from a to b, in that order. When b comes
// first along the polyline the piece is reversed, which is how a chain walking
// an arc backwards gets drawn the way it is walked.
func between(pips []Position, a, b cut) []Position {
	if b.before(a) {
		out := between(pips, b, a)
		for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
			out[l], out[r] = out[r], out[l]
		}
		return out
	}
	out := []Position{at(pips, a)}
	for i := a.seg + 1; i <= b.seg; i++ {
		out = append(out, pips[i])
	}
	out = append(out, at(pips, b))
	return out
}

// ArcLine is the whole line of arc a: its From node's position, its interior
// pips, and its To node's position (SPEC §3.3.2). An error names a node the
// owner does not have.
func ArcLine(nodes map[string]Vertex, a Arc) ([]Position, error) {
	from, ok := nodes[a.From]
	if !ok {
		return nil, fmt.Errorf("vnx: arc names node %q, which is not in its owner", a.From)
	}
	to, ok := nodes[a.To]
	if !ok {
		return nil, fmt.Errorf("vnx: arc names node %q, which is not in its owner", a.To)
	}
	line := make([]Position, 0, len(a.Pips)+2)
	line = append(line, from.Position())
	for _, v := range a.Pips {
		line = append(line, v.Position())
	}
	return append(line, to.Position()), nil
}

// WalkLegs is the line each pass of a walk covers, one leg per pass, in walk
// order (SPEC §3.3.3): from the pass's entry, out to its turn where it has one,
// and on to its exit, along its arc's line whatever order those points lie in.
// A leg runs the way the walk goes, so a pass walking its arc from To to From
// is reversed.
//
// ⚠ A NODE END IS THE ARC'S OWN END, NOT A PROJECTION. The walk says which node
// it is at, and a node is one end of its arc's line by construction, so it
// cuts there: a projection could land on the other end of an arc that bends
// back past its own start. A position is cut at the nearest point, at any
// distance — §3.3.3's model, the same projection a reader would use.
//
// ⚠ A WALK THAT DOES NOT RESOLVE IS AN ERROR, never a guess. A pass naming an
// absent arc, a node that is not an end of its arc, a walk that does not join:
// each is refused with the pass and the rule, where drawing "the whole arc
// forwards" would put a line on the map that nobody walked.
//
// Like every other geometry step it runs BEFORE ROUNDING (round.go): the cut is
// taken at full precision and the writer rounds what comes out.
func WalkLegs(nodes map[string]Vertex, arcs map[string]Arc, walk []Pass) ([][]Position, error) {
	cur := walkCursor{last: len(walk) - 1}
	legs := make([][]Position, 0, len(walk))
	for i, p := range walk {
		arc, known := arcs[p.Arc]
		if !known {
			return nil, (&walkFault{pass: i, code: CodePassArcAbsent, msg: fmt.Sprintf("pass names arc %q, which is not here", p.Arc)}).error()
		}
		entry, exit, f := cur.step(i, p, arc)
		if f != nil {
			return nil, f.error()
		}
		line, err := ArcLine(nodes, arc)
		if err != nil {
			return nil, fmt.Errorf("pass %d: %w", i, err)
		}
		in, out := cutAt(line, arc, entry), cutAt(line, arc, exit)
		if p.Turn == nil {
			legs = append(legs, between(line, in, out))
			continue
		}
		turn := project(line, p.Turn.Position())
		leg := between(line, in, turn)
		legs = append(legs, append(leg, between(line, turn, out)[1:]...))
	}
	return legs, nil
}

// WalkLine is the walk's legs joined into one line: where a leg starts at the
// point the previous one ended, the shared point is written once.
func WalkLine(nodes map[string]Vertex, arcs map[string]Arc, walk []Pass) ([]Position, error) {
	legs, err := WalkLegs(nodes, arcs, walk)
	if err != nil {
		return nil, err
	}
	var out []Position
	for _, leg := range legs {
		if n := len(out); n > 0 && len(leg) > 0 && out[n-1].Lon == leg[0].Lon && out[n-1].Lat == leg[0].Lat {
			leg = leg[1:]
		}
		out = append(out, leg...)
	}
	return out, nil
}

// cutAt is where an endpoint lies on its arc's line: a node at that end, a
// position at its nearest point.
func cutAt(line []Position, arc Arc, e Endpoint) cut {
	switch {
	case e.At != nil:
		return project(line, e.At.Position())
	case e.Node == arc.From:
		return cut{seg: 0, t: 0}
	default:
		// step has checked the node is an end, so it is To.
		return cut{seg: len(line) - 2, t: 1}
	}
}
