package merge

import (
	"math"
	"testing"
)

// ---- source-measure position ----------------------------------------------
//
// Fixtures are built with the metric test frame (mPt), but nothing here
// asserts against the frame's nominal metres: edge lengths are haversine
// (HaversineMetres), and the tests recompute the expected values with
// the same call SourceMeasure uses. That keeps the assertions exact rather than
// tolerant of the frame's ~0.1% latitude approximation.

func mustSourceMeasure(t *testing.T, pts []Point) *SourceMeasure {
	t.Helper()
	sa, err := NewSourceMeasure(pts)
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	return sa
}

// xLine is a west→east polyline through the given east offsets in metres.
func xLine(xs ...float64) []Point {
	pts := make([]Point, len(xs))
	for i, x := range xs {
		pts[i] = mPt(x, 0)
	}
	return pts
}

func hav(a, b Point) float64 {
	return HaversineMetres(a.Lat, a.Lon, b.Lat, b.Lon)
}

func TestSrcMeasureNewRejectsShortPolyline(t *testing.T) {
	// No edges means no position can exist, so this is a caller bug, not a
	// degenerate leg to be tolerated.
	for _, pts := range [][]Point{nil, xLine(0)} {
		if _, err := NewSourceMeasure(pts); err == nil {
			t.Fatalf("newSourceMeasure(%d points): expected an error", len(pts))
		}
	}
	if _, err := NewSourceMeasure(xLine(0, 10)); err != nil {
		t.Fatalf("newSourceMeasure(2 points): %v", err)
	}
}

func TestSrcMeasureMeasureAtMatchesCumulativeHaversine(t *testing.T) {
	pts := xLine(0, 10, 40, 100)
	sa := mustSourceMeasure(t, pts)
	if sa.Edges() != 3 {
		t.Fatalf("edges = %d, want 3", sa.Edges())
	}
	cum := 0.0
	for e := range sa.Edges() {
		want := hav(pts[e], pts[e+1])
		// Not bit-identical, deliberately: edgeLen is a DIFFERENCE of
		// cumulative values (as projectRangeCeil takes it), so an edge's length
		// always agrees with the cumulative array even when that costs it the
		// last ulp against a fresh haversine.
		if math.Abs(sa.EdgeLen(e)-want) > 1e-9 {
			t.Fatalf("edgeLen(%d) = %v, want %v", e, sa.EdgeLen(e), want)
		}
		got, err := sa.MeasureAt(SourcePos{Edge: e, Frac: 0})
		if err != nil {
			t.Fatalf("measureAt: %v", err)
		}
		if got != cum {
			t.Fatalf("measureAt(edge %d, frac 0) = %v, want %v", e, got, cum)
		}
		// Half way along an edge is half its measure — measure is linear in frac, the
		// same convention projectRangeCeil uses on the canon side.
		mid, err := sa.MeasureAt(SourcePos{Edge: e, Frac: 0.5})
		if err != nil {
			t.Fatalf("measureAt: %v", err)
		}
		if math.Abs(mid-(cum+want/2)) > 1e-9 {
			t.Fatalf("measureAt(edge %d, frac 0.5) = %v, want %v", e, mid, cum+want/2)
		}
		cum += want
	}
	if math.Abs(sa.Total()-cum) > 1e-9 {
		t.Fatalf("total = %v, want %v", sa.Total(), cum)
	}
}

func TestSrcMeasureCompareEdgeBoundaryIsOnePoint(t *testing.T) {
	sa := mustSourceMeasure(t, xLine(0, 10, 40, 100))
	// The pinned contract: frac 1 on edge k and frac 0 on edge k+1 are the
	// SAME point and must compare equal — exactly, with no epsilon.
	for e := range sa.Edges() - 1 {
		hi := SourcePos{Edge: e, Frac: 1}
		lo := SourcePos{Edge: e + 1, Frac: 0}
		eq, err := sa.Equal(hi, lo)
		if err != nil {
			t.Fatalf("equal: %v", err)
		}
		if !eq {
			t.Fatalf("(edge %d, frac 1) != (edge %d, frac 0): the same point compared unequal", e, e+1)
		}
	}
	// And they are a single point in the ordering too: strictly after anything
	// earlier on edge e, strictly before anything later on edge e+1.
	before := SourcePos{Edge: 0, Frac: 0.999}
	at := SourcePos{Edge: 0, Frac: 1}
	after := SourcePos{Edge: 1, Frac: 0.001}
	for _, c := range []struct {
		a, b SourcePos
		want int
	}{
		{before, at, -1},
		{at, after, -1},
		{after, at, 1},
		{at, at, 0},
	} {
		got, err := sa.Compare(c.a, c.b)
		if err != nil {
			t.Fatalf("compare: %v", err)
		}
		if got != c.want {
			t.Fatalf("compare(%+v, %+v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSrcMeasureCompareAgreesWithMeasureOrder(t *testing.T) {
	// A polyline with zero-length edges at the head, in the middle and at the
	// tail: the three places a duplicate vertex can break an ordering.
	sa := mustSourceMeasure(t, xLine(0, 0, 10, 10, 40, 100, 100))
	var ps []SourcePos
	for e := range sa.Edges() {
		for _, f := range []float64{0, 0.25, 0.5, 1} {
			ps = append(ps, SourcePos{Edge: e, Frac: f})
		}
	}
	for _, a := range ps {
		for _, b := range ps {
			got, err := sa.Compare(a, b)
			if err != nil {
				t.Fatalf("compare: %v", err)
			}
			aa, err := sa.MeasureAt(a)
			if err != nil {
				t.Fatalf("measureAt: %v", err)
			}
			ab, err := sa.MeasureAt(b)
			if err != nil {
				t.Fatalf("measureAt: %v", err)
			}
			want := 0
			switch {
			case aa < ab:
				want = -1
			case aa > ab:
				want = 1
			}
			if got != want {
				t.Fatalf("compare(%+v, %+v) = %d but measures are %v vs %v (want %d)", a, b, got, aa, ab, want)
			}
		}
	}
}

func TestSrcMeasureZeroLengthEdgePositions(t *testing.T) {
	// Edge 1 (10 → 10) is zero length: a GPS resting-jitter duplicate, which
	// this codebase preserves verbatim. Every position on it — and both its
	// neighbours' shared boundaries — is one point.
	sa := mustSourceMeasure(t, xLine(0, 10, 10, 40))
	if sa.EdgeLen(1) != 0 {
		t.Fatalf("edgeLen(1) = %v, want 0", sa.EdgeLen(1))
	}
	same := []SourcePos{
		{Edge: 0, Frac: 1},
		{Edge: 1, Frac: 0},
		{Edge: 1, Frac: 0.5},
		{Edge: 1, Frac: 1},
		{Edge: 2, Frac: 0},
	}
	for _, p := range same {
		for _, q := range same {
			eq, err := sa.Equal(p, q)
			if err != nil {
				t.Fatalf("equal: %v", err)
			}
			if !eq {
				t.Fatalf("%+v and %+v are the same point but compared unequal", p, q)
			}
		}
		// All of them normalise to the one representative past the dead edge.
		n, err := sa.Normalise(p)
		if err != nil {
			t.Fatalf("normalise: %v", err)
		}
		if n != (SourcePos{Edge: 2, Frac: 0}) {
			t.Fatalf("normalise(%+v) = %+v, want {2 0}", p, n)
		}
		// And they are all the same place on the ground.
		lat, lon, err := sa.LatLonAt(p)
		if err != nil {
			t.Fatalf("latLonAt: %v", err)
		}
		if lat != sa.Pts[1].Lat || lon != sa.Pts[1].Lon {
			t.Fatalf("latLonAt(%+v) = (%v,%v), want the duplicated vertex (%v,%v)",
				p, lat, lon, sa.Pts[1].Lat, sa.Pts[1].Lon)
		}
	}
	// The dead edge does not break the ordering around it.
	got, err := sa.Compare(SourcePos{Edge: 0, Frac: 0.5}, SourcePos{Edge: 1, Frac: 0.5})
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if got != -1 {
		t.Fatalf("mid-edge-0 vs the dead edge: compare = %d, want -1", got)
	}
	got, err = sa.Compare(SourcePos{Edge: 1, Frac: 0.5}, SourcePos{Edge: 2, Frac: 0.5})
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if got != -1 {
		t.Fatalf("the dead edge vs mid-edge-2: compare = %d, want -1", got)
	}
}

func TestSrcMeasureCanonicalForm(t *testing.T) {
	// The canonical form is stated on SourcePos and is FORWARD-MOST: (i,1) →
	// (i+1,0), a zero-length edge collapses onto the next edge, and the
	// polyline's own end is where the pushing stops. Pinned directly here
	// rather than only through compare, so a later change to the rule shows up
	// as this test failing and not as a downstream ordering surprise.
	//
	// Edge lengths: 0 dead, 1 live, 2 dead, 3 live, 4 dead (trailing).
	sa := mustSourceMeasure(t, xLine(0, 0, 10, 10, 40, 40))
	last := sa.Edges() - 1
	for _, c := range []struct {
		in, want SourcePos
		why      string
	}{
		{SourcePos{Edge: 0, Frac: 0}, SourcePos{Edge: 1, Frac: 0}, "a leading dead edge collapses forward"},
		{SourcePos{Edge: 0, Frac: 0.5}, SourcePos{Edge: 1, Frac: 0}, "any frac on a dead edge is its one point"},
		{SourcePos{Edge: 0, Frac: 1}, SourcePos{Edge: 1, Frac: 0}, "frac 1 pushes to the next edge"},
		{SourcePos{Edge: 1, Frac: 0}, SourcePos{Edge: 1, Frac: 0}, "the start of a live edge is already canonical"},
		{SourcePos{Edge: 1, Frac: 0.5}, SourcePos{Edge: 1, Frac: 0.5}, "an interior position is already canonical"},
		{SourcePos{Edge: 1, Frac: 1}, SourcePos{Edge: 3, Frac: 0}, "frac 1 pushes THROUGH the dead edge that follows"},
		{SourcePos{Edge: 2, Frac: 0.25}, SourcePos{Edge: 3, Frac: 0}, "the dead edge itself lands past it"},
		{SourcePos{Edge: 3, Frac: 0.75}, SourcePos{Edge: 3, Frac: 0.75}, "interior again"},
		{SourcePos{Edge: 3, Frac: 1}, SourcePos{Edge: last, Frac: 0}, "a TRAILING dead edge takes frac 0, not 1"},
		{SourcePos{Edge: last, Frac: 0.5}, SourcePos{Edge: last, Frac: 0}, "and swallows every frac on it"},
		{SourcePos{Edge: last, Frac: 1}, SourcePos{Edge: last, Frac: 0}, "including its own end"},
	} {
		got, err := sa.Normalise(c.in)
		if err != nil {
			t.Fatalf("normalise(%+v): %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("normalise(%+v) = %+v, want %+v (%s)", c.in, got, c.want, c.why)
		}
		// Idempotent, which is what lets a caller normalise once and then use
		// == and map keys on the result.
		again, err := sa.Normalise(got)
		if err != nil {
			t.Fatalf("normalise(normalise(%+v)): %v", c.in, err)
		}
		if again != got {
			t.Fatalf("normalise is not idempotent: %+v → %+v → %+v", c.in, got, again)
		}
	}

	// On a polyline with a live final edge, the terminal keeps frac 1 — it has
	// nowhere forward to go.
	live := mustSourceMeasure(t, xLine(0, 10, 40))
	got, err := live.Normalise(SourcePos{Edge: 1, Frac: 1})
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	if got != (SourcePos{Edge: 1, Frac: 1}) {
		t.Fatalf("normalise(terminal) = %+v, want {1 1}", got)
	}

	// The map-key promise: every representation of one point keys the same
	// bucket once normalised, and distinct points do not collide.
	seen := map[SourcePos]int{}
	for _, p := range []SourcePos{{Edge: 1, Frac: 1}, {Edge: 2, Frac: 0}, {Edge: 2, Frac: 0.9}, {Edge: 3, Frac: 0}} {
		n, err := sa.Normalise(p)
		if err != nil {
			t.Fatalf("normalise: %v", err)
		}
		seen[n]++
	}
	if len(seen) != 1 {
		t.Fatalf("one point keyed %d buckets: %v", len(seen), seen)
	}
	n0, _ := sa.Normalise(SourcePos{Edge: 1, Frac: 0.5})
	if _, clash := seen[n0]; clash {
		t.Fatalf("a distinct point collided with the duplicate cluster's key")
	}
}

func TestSrcMeasureNormaliseKeepsInteriorAndTerminal(t *testing.T) {
	sa := mustSourceMeasure(t, xLine(0, 10, 40))
	// An interior position of a positive-length edge is already canonical.
	p := SourcePos{Edge: 0, Frac: 0.5}
	n, err := sa.Normalise(p)
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	if n != p {
		t.Fatalf("normalise(%+v) = %+v, want it unchanged", p, n)
	}
	// The polyline's own end has nowhere to go.
	n, err = sa.Normalise(sa.End())
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	if n != sa.End() {
		t.Fatalf("normalise(end) = %+v, want %+v", n, sa.End())
	}
}

func TestSrcMeasureVertexPos(t *testing.T) {
	pts := xLine(0, 10, 40, 100)
	sa := mustSourceMeasure(t, pts)
	for i := range pts {
		p, err := sa.VertexPos(i)
		if err != nil {
			t.Fatalf("vertexPos(%d): %v", i, err)
		}
		lat, lon, err := sa.LatLonAt(p)
		if err != nil {
			t.Fatalf("latLonAt: %v", err)
		}
		// Byte-identical, not close: the assembly rules keep terminals exactly where
		// the source put them, and every original vertex is addressable here
		// without moving.
		if lat != pts[i].Lat || lon != pts[i].Lon {
			t.Fatalf("vertexPos(%d) is at (%v,%v), want (%v,%v)", i, lat, lon, pts[i].Lat, pts[i].Lon)
		}
	}
	if _, err := sa.VertexPos(-1); err == nil {
		t.Fatalf("vertexPos(-1): expected an error")
	}
	if _, err := sa.VertexPos(len(pts)); err == nil {
		t.Fatalf("vertexPos(%d): expected an error", len(pts))
	}
	// The first and last vertices are the polyline's terminals.
	first, _ := sa.VertexPos(0)
	last, _ := sa.VertexPos(len(pts) - 1)
	if eq, _ := sa.Equal(first, sa.Start()); !eq {
		t.Fatalf("vertexPos(0) = %+v is not start() = %+v", first, sa.Start())
	}
	if eq, _ := sa.Equal(last, sa.End()); !eq {
		t.Fatalf("vertexPos(last) = %+v is not end() = %+v", last, sa.End())
	}
}

func TestSrcMeasureAdvanceAcrossEdges(t *testing.T) {
	pts := xLine(0, 10, 40, 100)
	sa := mustSourceMeasure(t, pts)
	l0, l1 := sa.EdgeLen(0), sa.EdgeLen(1)

	// Landing exactly on a vertex: the position must compare equal to that
	// vertex however it is represented.
	got, err := sa.Advance(sa.Start(), l0)
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	v1, _ := sa.VertexPos(1)
	if eq, _ := sa.Equal(got, v1); !eq {
		t.Fatalf("advance(start, edgeLen(0)) = %+v, want vertex 1 (%+v)", got, v1)
	}

	// Crossing two edges to land mid-third.
	want := l0 + l1 + 20
	got, err = sa.Advance(sa.Start(), want)
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if got.Edge != 2 {
		t.Fatalf("advance over two edges landed on edge %d, want 2", got.Edge)
	}
	at, err := sa.MeasureAt(got)
	if err != nil {
		t.Fatalf("measureAt: %v", err)
	}
	if math.Abs(at-want) > 1e-9 {
		t.Fatalf("advance(start, %v) landed at measure %v", want, at)
	}

	// Backwards, from the end.
	back, err := sa.Advance(sa.End(), -(20 + l1))
	if err != nil {
		t.Fatalf("advance backward: %v", err)
	}
	at, err = sa.MeasureAt(back)
	if err != nil {
		t.Fatalf("measureAt: %v", err)
	}
	if math.Abs(at-(sa.Total()-20-l1)) > 1e-9 {
		t.Fatalf("backward advance landed at measure %v, want %v", at, sa.Total()-20-l1)
	}
}

func TestSrcMeasureAdvanceAcrossZeroLengthEdges(t *testing.T) {
	// Two duplicated vertices in the middle of the run: advancing through them
	// must neither stall nor consume measure.
	sa := mustSourceMeasure(t, xLine(0, 10, 10, 10, 40))
	l0 := sa.EdgeLen(0)
	for _, e := range []int{1, 2} {
		if sa.EdgeLen(e) != 0 {
			t.Fatalf("edgeLen(%d) = %v, want 0", e, sa.EdgeLen(e))
		}
	}
	// Stepping exactly onto the duplicate cluster.
	got, err := sa.Advance(sa.Start(), l0)
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if eq, _ := sa.Equal(got, SourcePos{Edge: 2, Frac: 0.5}); !eq {
		t.Fatalf("advance onto the duplicate cluster = %+v, want the cluster point", got)
	}
	// And straight past it in one step.
	got, err = sa.Advance(sa.Start(), l0+5)
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if got.Edge != 3 {
		t.Fatalf("advance past the duplicates landed on edge %d, want 3", got.Edge)
	}
	at, err := sa.MeasureAt(got)
	if err != nil {
		t.Fatalf("measureAt: %v", err)
	}
	if math.Abs(at-(l0+5)) > 1e-9 {
		t.Fatalf("advance past the duplicates landed at measure %v, want %v", at, l0+5)
	}
	// Starting ON a dead edge and advancing works the same way.
	got, err = sa.Advance(SourcePos{Edge: 1, Frac: 0.5}, 5)
	if err != nil {
		t.Fatalf("advance from a zero-length edge: %v", err)
	}
	at, err = sa.MeasureAt(got)
	if err != nil {
		t.Fatalf("measureAt: %v", err)
	}
	if math.Abs(at-(l0+5)) > 1e-9 {
		t.Fatalf("advance from the dead edge landed at measure %v, want %v", at, l0+5)
	}
}

func TestSrcMeasureAdvanceOffEitherEndIsError(t *testing.T) {
	sa := mustSourceMeasure(t, xLine(0, 10, 40))
	// Past the end and before the start are errors, NOT clamps: a sweep asking
	// for a window the source does not have has mis-derived the window, and
	// returning the terminal would let it measure a shorter span than it thinks.
	if _, err := sa.Advance(sa.End(), 1); err == nil {
		t.Fatalf("advance past the end: expected an error, got a position")
	}
	if _, err := sa.Advance(sa.Start(), -1); err == nil {
		t.Fatalf("advance before the start: expected an error, got a position")
	}
	if _, err := sa.Advance(sa.Start(), sa.Total()*2); err == nil {
		t.Fatalf("advance well past the end: expected an error, got a position")
	}
	// Landing exactly ON a terminal is legal.
	got, err := sa.Advance(sa.Start(), sa.Total())
	if err != nil {
		t.Fatalf("advance to exactly the end: %v", err)
	}
	if eq, _ := sa.Equal(got, sa.End()); !eq {
		t.Fatalf("advance(start, total) = %+v, want end() = %+v", got, sa.End())
	}
	// Non-finite distances are caller bugs.
	for _, d := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := sa.Advance(sa.Start(), d); err == nil {
			t.Fatalf("advance(start, %v): expected an error", d)
		}
	}
}

func TestSrcMeasureAdvanceZeroIsIdentity(t *testing.T) {
	sa := mustSourceMeasure(t, xLine(0, 10, 10, 40))
	for _, p := range []SourcePos{
		sa.Start(), sa.End(),
		{Edge: 0, Frac: 0.3}, {Edge: 0, Frac: 1},
		{Edge: 1, Frac: 0.5}, {Edge: 2, Frac: 0.7},
	} {
		got, err := sa.Advance(p, 0)
		if err != nil {
			t.Fatalf("advance(%+v, 0): %v", p, err)
		}
		if got != p {
			t.Fatalf("advance(%+v, 0) = %+v, want the identity", p, got)
		}
	}
}

func TestSrcMeasureMeasureRoundTrip(t *testing.T) {
	sa := mustSourceMeasure(t, xLine(0, 10, 10, 40, 100))
	// measure → position → measure at each branch of posAtMeasure. Edges:
	// 0 = 0→10, 1 = 10→10 (zero length), 2 = 10→40, 3 = 40→100.
	cum := sa.CumMeasure
	roundTrip := func(sa *SourceMeasure, want float64) {
		t.Helper()
		p, err := sa.PosAtMeasure(want)
		if err != nil {
			t.Fatalf("posAtMeasure(%v): %v", want, err)
		}
		got, err := sa.MeasureAt(p)
		if err != nil {
			t.Fatalf("measureAt: %v", err)
		}
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("posAtMeasure(%v) round-tripped to %v (position %+v)", want, got, p)
		}
	}
	for _, want := range []float64{
		0,                     // search returns -1: clamped up to edge 0
		cum[1] / 2,            // interior of edge 0
		cum[1],                // tie on the vertex before the zero-length edge
		(cum[2] + cum[3]) / 2, // interior of edge 2, past the zero-length edge
		cum[3],                // tie on the vertex that starts edge 3
		(cum[3] + cum[4]) / 2, // interior of the last edge
		sa.Total(),            // the far terminal
	} {
		roundTrip(sa, want)
	}
	// A zero-length FIRST edge is the only way to reach the l == 0 branch:
	// measure 0 clamps onto edge 0, which has no length.
	sz := mustSourceMeasure(t, xLine(0, 0, 10))
	for _, want := range []float64{0, sz.Total() / 2, sz.Total()} {
		roundTrip(sz, want)
	}
	// position → distance → position, forwards and back.
	ps := []SourcePos{
		{Edge: 0, Frac: 0}, {Edge: 0, Frac: 0.4}, {Edge: 1, Frac: 0.5},
		{Edge: 2, Frac: 0.25}, {Edge: 3, Frac: 0.9}, {Edge: 3, Frac: 1},
	}
	for _, a := range ps {
		for _, b := range ps {
			d, err := sa.MeasureBetween(a, b)
			if err != nil {
				t.Fatalf("measureBetween: %v", err)
			}
			// Signed: positive exactly when b is after a.
			c, err := sa.Compare(a, b)
			if err != nil {
				t.Fatalf("compare: %v", err)
			}
			if (c < 0 && d < 0) || (c > 0 && d > 0) || (c == 0 && d != 0) {
				t.Fatalf("measureBetween(%+v,%+v) = %v disagrees with compare = %d", a, b, d, c)
			}
			got, err := sa.Advance(a, d)
			if err != nil {
				t.Fatalf("advance(%+v, %v): %v", a, d, err)
			}
			ab, err := sa.MeasureAt(b)
			if err != nil {
				t.Fatalf("measureAt: %v", err)
			}
			ag, err := sa.MeasureAt(got)
			if err != nil {
				t.Fatalf("measureAt: %v", err)
			}
			if math.Abs(ag-ab) > 1e-9 {
				t.Fatalf("advance(%+v, measureBetween→%v) landed at measure %v, want %v (%+v)", a, d, ag, ab, b)
			}
		}
	}
}

func TestSrcMeasurePosAtMeasureRejectsOutOfRange(t *testing.T) {
	sa := mustSourceMeasure(t, xLine(0, 10, 40))
	for _, at := range []float64{-1, -1e-12, sa.Total() + 1e-12, sa.Total() * 2, math.NaN()} {
		if _, err := sa.PosAtMeasure(at); err == nil {
			t.Fatalf("posAtMeasure(%v): expected an error, got a position", at)
		}
	}
	for _, at := range []float64{0, sa.Total() / 3, sa.Total()} {
		if _, err := sa.PosAtMeasure(at); err != nil {
			t.Fatalf("posAtMeasure(%v): %v", at, err)
		}
	}
}

func TestSrcMeasureValidateRejectsOutOfRange(t *testing.T) {
	sa := mustSourceMeasure(t, xLine(0, 10, 40))
	// A frac a hair outside [0,1] is a bug in the caller's solve. It is an
	// error here, never a clamp — the house rule the whole merge runs on.
	bad := []SourcePos{
		{Edge: -1, Frac: 0},
		{Edge: 2, Frac: 0},
		{Edge: 0, Frac: -1e-15},
		{Edge: 0, Frac: 1 + 1e-15},
		{Edge: 0, Frac: math.NaN()},
		{Edge: 0, Frac: math.Inf(1)},
	}
	for _, p := range bad {
		if err := sa.Validate(p); err == nil {
			t.Fatalf("validate(%+v): expected an error", p)
		}
		if _, err := sa.MeasureAt(p); err == nil {
			t.Fatalf("measureAt(%+v): expected an error", p)
		}
		if _, _, err := sa.LatLonAt(p); err == nil {
			t.Fatalf("latLonAt(%+v): expected an error", p)
		}
		if _, err := sa.Compare(p, sa.Start()); err == nil {
			t.Fatalf("compare(%+v, start): expected an error", p)
		}
		if _, err := sa.Advance(p, 1); err == nil {
			t.Fatalf("advance(%+v, 1): expected an error", p)
		}
	}
}

func TestSrcMeasureLatLonAtInterpolates(t *testing.T) {
	pts := xLine(0, 100)
	sa := mustSourceMeasure(t, pts)
	lat, lon, err := sa.LatLonAt(SourcePos{Edge: 0, Frac: 0.25})
	if err != nil {
		t.Fatalf("latLonAt: %v", err)
	}
	wantLat := pts[0].Lat + 0.25*(pts[1].Lat-pts[0].Lat)
	wantLon := pts[0].Lon + 0.25*(pts[1].Lon-pts[0].Lon)
	if lat != wantLat || lon != wantLon {
		t.Fatalf("latLonAt(frac 0.25) = (%v,%v), want (%v,%v)", lat, lon, wantLat, wantLon)
	}
	// Interpolation is linear in the SAME parameter the measure is linear in, so a
	// quarter of the measure is a quarter of the way along the ground.
	d := HaversineMetres(pts[0].Lat, pts[0].Lon, lat, lon)
	if math.Abs(d-sa.Total()/4) > 1e-6 {
		t.Fatalf("frac 0.25 is %v m along a %v m edge", d, sa.Total())
	}
}

func TestSrcMeasureAllDuplicateVertices(t *testing.T) {
	// A leg that never moves: total measure zero. Representable, not an error —
	// every position on it is the one point, and only a zero-distance advance
	// is possible.
	sa := mustSourceMeasure(t, xLine(0, 0, 0))
	if sa.Total() != 0 {
		t.Fatalf("total = %v, want 0", sa.Total())
	}
	eq, err := sa.Equal(sa.Start(), sa.End())
	if err != nil {
		t.Fatalf("equal: %v", err)
	}
	if !eq {
		t.Fatalf("start and end of a zero-length polyline compared unequal")
	}
	d, err := sa.MeasureBetween(sa.Start(), sa.End())
	if err != nil {
		t.Fatalf("measureBetween: %v", err)
	}
	if d != 0 {
		t.Fatalf("measureBetween(start,end) = %v, want 0", d)
	}
	if _, err := sa.Advance(sa.Start(), 0); err != nil {
		t.Fatalf("advance(start, 0): %v", err)
	}
	if _, err := sa.Advance(sa.Start(), 1e-12); err == nil {
		t.Fatalf("advance along a zero-length polyline: expected an error")
	}
}
