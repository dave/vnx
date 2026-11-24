package merge

import (
	"math"
)

// DupSeqVerdict is what DupVertexSequenceEqual answers. Reversal and elevation
// are reported rather than folded into Equal: both are decisions ride
// resolution has to make explicitly, and neither may be settled by this
// function's return type.
//
// Exported for the reference application's tests; not part of the stable API.
type DupSeqVerdict struct {
	// Equal is lat/lon sequence equality, exact, in one direction or the
	// other. This is the whole of ride resolution's rule.
	Equal bool
	// Reversed is true when the match holds only after reversing. A
	// reference to a reversed arc must be DIRECTED: the caller must record
	// that it rides the arc backwards, not assume the forward direction.
	Reversed bool
	// EleEqual is true when the elevations also match, vertex for vertex, in
	// the SAME direction the lat/lon match held. False here with Equal true
	// means referencing canon would substitute canon's elevation for this
	// plan's own. That is reported, not decided, here.
	EleEqual bool
}

// DupVertexSequenceEqual answers ride resolution's question: are these two
// vertex sequences the same, exactly?
//
// ⚠ NO TOLERANCE, DELIBERATELY, AND IT MUST NEVER ACQUIRE ONE. Near-identical
// strands that are NOT bitwise equal (measured on test areas, some only a few
// metres apart) are exactly what a tolerance would wrongly swallow, and "no
// geometric judgement" is the promise this step rests on. If a pair really is
// the same trail and does not compare equal here, the fix belongs in the
// matcher, after which the follow rewrites one onto the other and it arrives
// here equal with no judgement of our own.
//
// Comparison is on lat/lon only. That is what the rule says; elevation is
// reported alongside so the substitution question stays visible instead of
// being answered by omission.
//
// Exported for the reference application's tests; not part of the stable API.
func DupVertexSequenceEqual(a, b []Point) DupSeqVerdict {
	if len(a) != len(b) {
		return DupSeqVerdict{}
	}
	if len(a) == 0 {
		// Two empty sequences are equal but say nothing. No arc has zero
		// vertices — its two endpoint nodes are always present — so this
		// is unreachable in practice and exists only so the loops
		// below need no length guard.
		return DupSeqVerdict{Equal: true, EleEqual: true}
	}
	fwd, fwdEle := true, true
	for i := range a {
		if a[i].Lat != b[i].Lat || a[i].Lon != b[i].Lon {
			fwd, fwdEle = false, false
			break
		}
		if !dupEleEqual(a[i].Ele, b[i].Ele) {
			fwdEle = false
		}
	}
	if fwd {
		return DupSeqVerdict{Equal: true, Reversed: false, EleEqual: fwdEle}
	}
	rev, revEle := true, true
	for i := range a {
		j := len(b) - 1 - i
		if a[i].Lat != b[j].Lat || a[i].Lon != b[j].Lon {
			rev, revEle = false, false
			break
		}
		if !dupEleEqual(a[i].Ele, b[j].Ele) {
			revEle = false
		}
	}
	if rev {
		return DupSeqVerdict{Equal: true, Reversed: true, EleEqual: revEle}
	}
	return DupSeqVerdict{}
}

// dupEleEqual compares two optional elevations. Both absent is equal; one
// absent is not. Node elevation is nullable and pip elevation is not, so a
// terminal vertex can legitimately carry no elevation at all.
//
// ⚠ NaN IS ABSENT, NOT A VALUE. A pip with no elevation carries a canonical
// NaN, and `*a == *b` is false for two NaNs, so without this every vertex of
// a track with no elevation would compare unequal.
func dupEleEqual(a, b *float64) bool {
	aAbsent := a == nil || math.IsNaN(*a)
	bAbsent := b == nil || math.IsNaN(*b)
	if aAbsent || bAbsent {
		return aAbsent && bAbsent
	}
	return *a == *b
}

// arcVertices is the vertex sequence a stored arc ACTUALLY holds: its two
// endpoint nodes' coordinates with its interior pips between them.
//
// ⚠ ENDPOINTS COME FROM THE NODES, NOT THE POLYLINE. An arc's polyline stores
// only interior pips, so reading the polyline alone compares two shapes both
// missing their ends. Anything that checks for duplicate arcs must
// reconstruct the committed shape this same way.
//
// Absent elevation arrives as a canonical NaN in a pip and as a null on a
// node; both are lifted to nil so "no elevation" compares equal to itself.
func arcVertices(arc Arc, start, end Node) []Point {
	v := make([]Point, 0, len(arc.Polyline)+2)
	v = append(v, Point{Lat: start.Lat, Lon: start.Lon, Ele: start.Ele})
	for _, p := range arc.Polyline {
		v = append(v, Point{Lat: p.Lat, Lon: p.Lon, Ele: pipEleOrNil(p.Ele)})
	}
	return append(v, Point{Lat: end.Lat, Lon: end.Lon, Ele: end.Ele})
}

// pipEleOrNil lifts a pip's float64 elevation to a pointer, mapping the
// absent-NaN to nil.
func pipEleOrNil(e float64) *float64 {
	if math.IsNaN(e) {
		return nil
	}
	return &e
}

// pieceCommittedVertices is the vertex sequence a piece WOULD commit, given the
// nodes its two ends resolved to.
//
// ⚠ THE NODES' COORDINATES WIN OVER THE PIECE'S OWN END VERTICES, because that
// is what the host's writer does: it discards a piece's terminal vertex in
// favour of the reused node's coordinates. Comparing the piece's raw first and
// last points instead would ask "is the source identical" where the question is
// "would the committed arc be identical", and after the endpoint warp those are
// not the same question.
func pieceCommittedVertices(pts []Point, start, end Node) []Point {
	// ⚠ THE INTERIOR IS TRIMMED THE WAY THE WRITER TRIMS IT. The write path
	// drops interior points sitting EXACTLY on the terminal node, because the
	// endpoint snap assigns the node's own coordinate and so manufactures a
	// second vertex there. This detector must ask its question about the
	// sequence that WOULD BE STORED, not the one handed in — otherwise a piece
	// with such a point never matches the arc it is a copy of, and ride
	// resolution commits a parallel duplicate instead of referencing canon
	// (two identical arcs, one leading pip apart).
	//
	// Float64 equality, no tolerance, for the same reason the writer uses it:
	// the values are equal bit for bit because one was assigned from the other.
	interior := pts[1 : len(pts)-1]
	lo := 0
	for lo < len(interior) && interior[lo].Lat == start.Lat && interior[lo].Lon == start.Lon {
		lo++
	}
	hi := len(interior)
	for hi > lo && interior[hi-1].Lat == end.Lat && interior[hi-1].Lon == end.Lon {
		hi--
	}
	v := make([]Point, 0, hi-lo+2)
	v = append(v, Point{Lat: start.Lat, Lon: start.Lon, Ele: start.Ele})
	v = append(v, interior[lo:hi]...)
	return append(v, Point{Lat: end.Lat, Lon: end.Lon, Ele: end.Ele})
}
