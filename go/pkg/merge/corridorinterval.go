package merge

// corridorinterval.go — import-measure corridor intervals with an exact, oriented
// canon-measure correspondence.
//
// The corridor matcher's state space needs two things a hull of band stretches
// cannot give:
//
//   - an EXACT image, not a hull: the nearest canon edge can switch part-way
//     along a band and the foot jumps at the switch, so a hull cannot answer
//     "what canon measure does THIS import position correspond to";
//   - ORIENTATION: whether canon measure INCREASES or DECREASES with import
//     measure over the interval, which a sorted (min,max) pair cannot say.
//
// This file is built from certified primitives — [bandPieces]
// (bandinterval.go) for the per-(source edge, canon edge) regime solve, and
// [RectsBeyond] for the box/chunk pruning that decides which canon edges are
// worth solving against a given source edge — with a join that keeps each
// regime's own piece (lo, hi, kind, dist² coefficients, oriented canon measure).
//
// CANDIDATE-SET SAFETY. "Which canon edges compete for a given source edge" is
// decided by [RectsBeyond]: a box CONTAINS its geometry, so the distance
// between two boxes lower-bounds the distance between the geometries inside
// them, and a box pair farther apart than d cannot hold a qualifying pair —
// skipping it cannot drop an interval. That is a source-EDGE-vs-canon-box RANGE
// query, not the point-vs-box queries hintPoly/pc.chunkBeyond/
// projectRangeCeil answer elsewhere in this package. A probe-and-pad
// candidate finder was tried and rejected for this: it is an
// empirically-tuned resolution constant in disguise.
//
// SELF-APPROACH VS HAIRPIN, the distinction the whole join exists for, is
// decided by canon-edge ADJACENCY: a
// hairpin's apex hands off between CONSECUTIVE canon edges as the source
// advances, so their regime pieces are canon-edge-adjacent and t-touching (or
// t-overlapping right at the shared vertex, where both sides' clamped foot is
// that vertex and so agree on its measure) — one interval. Self-approach — the
// source within d of two UNRELATED canon measures at once — has candidate pieces
// with non-adjacent canon-edge indices, which the same test refuses to join —
// two intervals. [canonEdgeRangesMeet] is that test.

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// CorridorPiece is one (source edge × canon edge × regime) contribution to a
// CorridorInterval: an exact, ORIENTED import-measure ↔ canon-measure correspondence,
// plus the dist²(t) this regime's band solve computed it from.
//
// Regime carries lo/hi in the SOURCE EDGE's own local [0,1] parameter — the
// same t [BandPiece.distSq] is defined over — because the cost integral
// needs to integrate over exactly that parameter's regime formula, not a
// re-derived one.
type CorridorPiece struct {
	SrcEdge, CanonEdge int
	Regime             BandPiece // .lo,.hi = source-edge-local T; .kind + coefficients = exact dist²(t)

	// Import measure at Regime.lo and Regime.hi respectively — monotonically
	// increasing with T, since measure within an edge is affine in T
	// (SourceMeasure's own header).
	ImportMeasureAtLo, ImportMeasureAtHi float64

	// Canon measure at Regime.lo and Regime.hi, ORIENTED (not sorted): may
	// increase OR decrease with import measure, which is what direction in the
	// corridor matcher's state space means.
	CanonMeasureAtLo, CanonMeasureAtHi float64
}

// CorridorInterval is one maximal import-measure stretch, for one candidate,
// where the candidate lies within d metres of the source polyline,
// parameterised on IMPORT measure, and carrying an exact oriented
// correspondence rather than a hull.
//
// Two returned intervals for the SAME candidate MAY overlap in import measure.
// That is self-approach — the source lying within d of two different, distant
// parts of the candidate at once — and it is exactly the case Pieces exists
// to represent rather than collapse.
type CorridorInterval struct {
	CandIdx                          int
	ImportMeasureLo, ImportMeasureHi float64
	Pieces                           []CorridorPiece // ascending import measure, contiguous or self-approach-overlapping
}

// CorridorIntervals returns, per candidate, the import-measure intervals where
// that candidate lies within d metres of the whole source polyline sa.
//
// An empty or all-nil candidate list is not a special case: the loop below
// simply contributes nothing, so an all-OFF/no-candidate leg yields no
// intervals.
func CorridorIntervals(ctx context.Context, sa *SourceMeasure, cands []*PreppedCandidate, d float64) ([]CorridorInterval, error) {
	var out []CorridorInterval
	for i, pc := range cands {
		if pc == nil {
			continue // PrepCandidates leaves degenerate candidates nil
		}
		ivs, err := candidateCorridorIntervals(ctx, sa, pc, i, d)
		if err != nil {
			return nil, err
		}
		out = append(out, ivs...)
	}
	return out, nil
}

// candidateCorridorIntervals is [CorridorIntervals] against ONE candidate.
func candidateCorridorIntervals(ctx context.Context, sa *SourceMeasure, pc *PreppedCandidate, candIdx int, d float64) ([]CorridorInterval, error) {
	if sa == nil {
		return nil, fmt.Errorf("CorridorIntervals: nil source measure")
	}
	if pc == nil {
		return nil, fmt.Errorf("CorridorIntervals: nil candidate — prepCandidates leaves degenerate candidates nil and the caller must skip them")
	}
	if !allFinite(d) || d < 0 {
		return nil, fmt.Errorf("CorridorIntervals: band must be finite and non-negative, got %v m", d)
	}

	bySrcEdge := make([][]CorridorPiece, sa.Edges())
	// Cost census. Read ONCE, outside the
	// loop: nil in production, so the mint pays one compare per source edge and
	// nothing else. See [CorridorCostObserver] — reporting must not change what
	// is minted, pruned or ordered.
	hooks := &stateOf(ctx).hooks
	obs := hooks.CorridorCost
	for e := 0; e < sa.Edges(); e++ {
		// The mint's cancellation point. One source edge is the
		// unit of mint work: [sourceEdgeCorridorPieces] allocates per canon edge
		// within d of THIS edge, so a leg cancelled here stops after at most one
		// more edge's pieces. Checked before the edge, not after, so a cancel
		// that arrives before the first edge mints nothing at all.
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("CorridorIntervals: cancelled at source edge %d of %d: %w", e, sa.Edges(), err)
		}
		edgeLen := sa.EdgeLen(e)
		// A source edge with no measure length carries no import measure, so it can
		// carry no correspondence: every piece it would mint has
		// ImportMeasureAtLo == ImportMeasureAtHi. Such a piece can never move the line
		// forward — the solver's forward edges span [bps[l], bps[l+1]] between
		// two DISTINCT breakpoints, and a zero-width piece is active over no
		// such span — and it adds no breakpoint of its own, so it is state
		// space that exists only to be switched through.
		//
		// ⚠ WHY DISCARDING IT LOSES NOTHING, STATED PROPERLY. The tempting
		// argument is the triangle inequality: within a candidate the arm-jump
		// term makes A->Z->B cost |z-a| + |z-b|, never less than the |b-a| of
		// going straight there. Since the cross-candidate price became
		// continuous ([crossCandidateSwitchCost]) that argument extends across
		// candidates too — both terms are canon distances at one weight — so
		// the inequality now covers transit generally rather than
		// same-candidate transit only. It is still not what closes this, for a
		// smaller reason: a triangle inequality says a detour is never CHEAPER,
		// which leaves a zero-width stepping stone tying rather than losing,
		// and a tie is decided by the tiebreak rather than by the price.
		//
		// What actually closes it is SHADOWING: a zero-length source edge is a
		// repeated coordinate, so the flanking real-length edges carry that same
		// coordinate as an endpoint, are within d of the same canon edges, and
		// [bandPieces] writes t exactly 0 or 1 at an edge end. Every
		// correspondence a zero-width piece offers at its single measure position is
		// therefore reproduced bitwise by a neighbouring piece that DOES have
		// extent. Nothing is removed from the state space; only duplicates are.
		//
		// The distinction is load-bearing, not pedantry. A similar-looking
		// domination argument for dropping CLAMPED-foot pieces was tried twice
		// here and is WRONG. Those carry a correspondence over real measure
		// extent that nothing else duplicates, so there is no shadow, and
		// dropping them moved a 19-file test corpus from 60 committed arcs
		// to 70. An incomplete argument
		// written here is what would license that mistake again.
		//
		// This is not a hypothetical. A GPS track that stands still repeats
		// its coordinate exactly, and repeats are ordinary: one real
		// recorded track has 1,248 consecutive duplicate points, 460 of them
		// on ONE coordinate. Each duplicate is a zero-length edge, and each
		// zero-length edge mints a piece against every canon edge within d —
		// which is what put 42,851 zero-width pieces of that leg's 58,853 at a
		// single import-measure position, 32,473 states on one layer, and 546 M of
		// the import's 610 M relaxations on that one leg.
		//
		// Deliberately an EXACT zero test, not a tolerance. The claim being
		// made is "this edge has no measure length at all", which is what makes
		// the piece unusable; a short-but-real edge carries a real, usable
		// correspondence and is none of this function's business.
		if edgeLen <= 0 {
			// Census: an explicit zero, not silence. This edge mints nothing by
			// the argument above, and that is a real count — a MISSING call
			// would be a broken instrument, and the two must stay
			// distinguishable.
			if obs != nil {
				obs.SourceEdgeMinted(e, 0, 0)
			}
			continue
		}
		p, q := sa.Pts[e], sa.Pts[e+1]
		pieces, err := sourceEdgeCorridorPieces(p, q, pc, d, true)
		if err != nil {
			return nil, err
		}
		minted := len(pieces)
		pieces, err = pruneToNearestCanonEdges(pieces, hooks)
		if err != nil {
			return nil, err
		}
		if obs != nil {
			obs.SourceEdgeMinted(e, minted, len(pieces))
		}
		for i := range pieces {
			pieces[i].SrcEdge = e
			pieces[i].ImportMeasureAtLo = sa.CumMeasure[e] + pieces[i].Regime.Lo*edgeLen
			pieces[i].ImportMeasureAtHi = sa.CumMeasure[e] + pieces[i].Regime.Hi*edgeLen
		}
		bySrcEdge[e] = pieces
	}
	return joinCorridorPieces(bySrcEdge, candIdx), nil
}

// sourceEdgeCorridorPieces returns, for the source edge p→q, one CorridorPiece
// per (canon edge, regime) that lies within d, in ascending canon-edge order,
// after box/chunk pruning (the certified-safe candidate-set construction — see
// this file's header).
//
// Regime.lo/hi, .kind and its dist² coefficients come straight from
// [bandPieces] with nothing recomputed; only the canon-measure correspondence is
// computed here, by [orientedFootMeasure].
func sourceEdgeCorridorPieces(p, q Point, pc *PreppedCandidate, d float64, prune bool) ([]CorridorPiece, error) {
	if pc == nil {
		return nil, fmt.Errorf("CorridorIntervals: nil candidate — prepCandidates leaves degenerate candidates nil and the caller must skip them")
	}
	if !allFinite(p.Lat, p.Lon, q.Lat, q.Lon, d) {
		return nil, fmt.Errorf("CorridorIntervals: non-finite source edge or band [P=(%v,%v) Q=(%v,%v) d=%v]", p.Lat, p.Lon, q.Lat, q.Lon, d)
	}
	if d < 0 {
		return nil, fmt.Errorf("CorridorIntervals: band must be non-negative, got %g m", d)
	}

	sMinLat, sMaxLat := math.Min(p.Lat, q.Lat), math.Max(p.Lat, q.Lat)
	sMinLon, sMaxLon := math.Min(p.Lon, q.Lon), math.Max(p.Lon, q.Lon)

	nedges := len(pc.Verts) - 1
	if prune && RectsBeyond(sMinLat, sMaxLat, sMinLon, sMaxLon, pc.minLat, pc.maxLat, pc.minLon, pc.maxLon, d) {
		return nil, nil
	}

	var out []CorridorPiece
	for e := 0; e < nedges; e++ {
		if prune && e%chunkEdges == 0 {
			c := e / chunkEdges
			if RectsBeyond(sMinLat, sMaxLat, sMinLon, sMaxLon,
				pc.chMinLat[c], pc.chMaxLat[c], pc.chMinLon[c], pc.chMaxLon[c], d) {
				e = (c+1)*chunkEdges - 1 // the loop's e++ lands on the next chunk
				continue
			}
		}
		a, b := pc.Verts[e], pc.Verts[e+1]
		pieces, n, joins, nj, err := bandPieces(p, q, a, b, d)
		if err != nil {
			return nil, fmt.Errorf("CorridorIntervals: canon arc %d edge %d: %w", pc.arcID, e, err)
		}
		if n == 0 {
			continue
		}
		// This is validation, not a value this function needs: the per-regime
		// pieces are read from pieces[:n] directly below rather than from the
		// single (lo,hi) unionOneInterval returns, but the convexity assertion
		// it makes — that the regime contributions truly are one contiguous
		// interval — is exactly what this function's own guarantee (Pieces
		// arrive gap-free within one canon edge) rests on.
		if _, _, _, err := unionOneInterval(pieces[:n]); err != nil {
			return nil, fmt.Errorf("CorridorIntervals: %w %s%s", err, bandGeomStr(p, q, a, b, d), bandJoinsStr(joins[:nj], d))
		}
		for i := 0; i < n; i++ {
			bp := pieces[i]
			a0, a1, err := orientedFootMeasure(p, q, a, b, pc, e, bp.Lo, bp.Hi)
			if err != nil {
				return nil, err
			}
			out = append(out, CorridorPiece{
				CanonEdge:        e,
				Regime:           bp,
				CanonMeasureAtLo: a0,
				CanonMeasureAtHi: a1,
			})
		}
	}
	return out, nil
}

// orientedFootMeasure returns the canon measure of the foot of the source
// points at t0 and t1, in the order given, not sorted into (min,max) — the
// order is the orientation. Each foot is [ClosestPointOnSeg] on canon edge e,
// turned into a measure through pc.cumMeasure.
func orientedFootMeasure(p, q, a, b Point, pc *PreppedCandidate, e int, t0, t1 float64) (a0, a1 float64, err error) {
	measureAt := func(t float64) (float64, error) {
		// t == 0 and t == 1 name the source edge's own END POINTS, and they
		// are used AS SUCH rather than interpolated to. `p + 1*(q-p)` is not
		// bitwise q in floating point, so interpolating left the two sides of
		// a shared source vertex — piece A's t=1 on edge e, piece B's t=0 on
		// edge e+1 — computing their foot from two points an ulp apart, and
		// so disagreeing on a canon measure that is by construction ONE value.
		// That disagreement is what a tolerance downstream would have to
		// forgive; taking the endpoint exactly removes it instead.
		var lat, lon float64
		switch t {
		case 0:
			lat, lon = p.Lat, p.Lon
		case 1:
			lat, lon = q.Lat, q.Lon
		default:
			lat = p.Lat + t*(q.Lat-p.Lat)
			lon = p.Lon + t*(q.Lon-p.Lon)
		}
		_, s := ClosestPointOnSeg(a.Lat, a.Lon, b.Lat, b.Lon, lat, lon)
		if !allFinite(s) {
			return 0, fmt.Errorf("CorridorIntervals: canon arc %d edge %d: foot parameter %v at t=%v is not finite", pc.arcID, e, s, t)
		}
		return pc.cumMeasure[e] + s*(pc.cumMeasure[e+1]-pc.cumMeasure[e]), nil
	}
	a0, err = measureAt(t0)
	if err != nil {
		return 0, 0, err
	}
	a1, err = measureAt(t1)
	if err != nil {
		return 0, 0, err
	}
	return a0, a1, nil
}

// joinCorridorPieces groups the flat, per-source-edge corridor pieces into
// CorridorIntervals — one interval per CONNECTED COMPONENT of the pieces
// under [piecesAdjacent].
//
// WHY A COMPONENT AND NOT A CHAIN. A CorridorInterval is defined (see
// [CorridorInterval]) as one maximal import-measure stretch where the candidate
// lies within d of the source. "Maximal stretch of a set of pieces that touch
// each other" is a connected-component question, and a component is a
// property of the pieces alone: it does not depend on the order they are
// visited, on which of two equally valid neighbours is tested first, or on
// any piece being designated "the one the source is really riding".
//
// The previous form was a sequential chain-continuation with an open set: at
// each source edge, each group of pieces claimed at most ONE still-open chain
// and each chain could be claimed by at most one group, first match in list
// order winning. Redundant competitor pieces — ordinary once canon has been
// split into many short consecutive edges by earlier splitting, since
// every canon edge within R of a straight stretch qualifies — open chains of
// their own, and those shadow chains then STEAL the group that the chain the
// source is actually riding needed to continue. Each steal closes a live
// chain and opens a fresh one: one interval becomes two, the solver gets two
// states where the geometry has one, and switching between them cuts a run,
// a node and an arc. Measured on a 19-file test corpus: one 16 km leg
// following ONE canon arc produced 62 intervals for that candidate, 29 of
// them wholly contained inside a longer one on a canon edge that longer one
// already covered — the same fact recorded twice, then disagreed with.
//
// Components have no such competition. A redundant competitor piece is
// adjacent to the pieces around it, so it joins the component instead of
// spawning a rival, and the interval count reflects the geometry rather than
// the visit order.
//
// SELF-APPROACH still separates, by the same test as before and for the same
// reason: pieces on non-adjacent canon edges are not adjacent, so the source
// lying within d of two UNRELATED canon measures at once yields two components. A
// hairpin's apex hands off between CONSECUTIVE canon edges, so its pieces are
// adjacent and stay one component. [canonEdgeRangesMeet] is that test.
//
// Piece order within an interval, and interval order in the output, are both
// construction order (source edge ascending, canon edge ascending within an
// edge) — the same order the chain form produced, so every consumer that
// reads Pieces sequentially sees the sequence it always did.
func joinCorridorPieces(bySrcEdge [][]CorridorPiece, candIdx int) []CorridorInterval {
	// Flatten, keeping construction order: source edge ascending, and within
	// a source edge the canon-edge-ascending order sourceEdgeCorridorPieces
	// emits. flat[i]'s index IS its construction rank, which is what orders
	// both the pieces inside an interval and the intervals in the output.
	var flat []CorridorPiece
	edgeStart := make([]int, len(bySrcEdge)+1)
	for e, pieces := range bySrcEdge {
		edgeStart[e] = len(flat)
		flat = append(flat, pieces...)
	}
	edgeStart[len(bySrcEdge)] = len(flat)
	if len(flat) == 0 {
		return nil
	}

	// Union-find over pieces. Only two edge classes can ever be adjacent —
	// same source edge, or consecutive source edges ([piecesAdjacent]) — so
	// the pairs tested are bounded by the per-edge piece counts and never
	// approach |flat|².
	parent := make([]int, len(flat))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]] // path halving
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		// Keep the LOWER construction rank as the root so a component's root
		// is its first piece, which is what orders the output below.
		if rb < ra {
			ra, rb = rb, ra
		}
		parent[rb] = ra
	}

	for e := 0; e < len(bySrcEdge); e++ {
		lo, hi := edgeStart[e], edgeStart[e+1]
		for i := lo; i < hi; i++ {
			for j := i + 1; j < hi; j++ {
				if piecesAdjacent(flat[i], flat[j]) {
					union(i, j)
				}
			}
			if e+1 < len(bySrcEdge) {
				for j := edgeStart[e+1]; j < edgeStart[e+2]; j++ {
					if piecesAdjacent(flat[i], flat[j]) {
						union(i, j)
					}
				}
			}
		}
	}

	// Collect components in first-piece order.
	order := make([]int, 0, 8)
	byRoot := make(map[int][]CorridorPiece, 8)
	for i := range flat {
		r := find(i)
		if _, seen := byRoot[r]; !seen {
			order = append(order, r)
		}
		byRoot[r] = append(byRoot[r], flat[i])
	}
	sort.Ints(order)

	out := make([]CorridorInterval, 0, len(order))
	for _, r := range order {
		pieces := byRoot[r]
		// min/max over every piece, not just the list's own first/last
		// entries: pieces are in canon-edge order within a source edge, not
		// import-measure order, and a competitor piece can cover only PART of its
		// own source edge's range.
		lo, hi := pieces[0].ImportMeasureAtLo, pieces[0].ImportMeasureAtHi
		for _, p := range pieces[1:] {
			lo = min(lo, p.ImportMeasureAtLo)
			hi = max(hi, p.ImportMeasureAtHi)
		}
		out = append(out, CorridorInterval{
			CandIdx:         candIdx,
			ImportMeasureLo: lo,
			ImportMeasureHi: hi,
			Pieces:          pieces,
		})
	}
	return out
}

// piecesAdjacent reports whether two corridor pieces belong to the same
// continuous stretch of corridor — the edge relation [joinCorridorPieces]
// takes connected components of.
//
// EXACT, NO TOLERANCE, and deliberately so. The two tests it makes are the
// two FACTS that decide the question, asked in the frame each was decided in:
//
//   - Canon locality is an INTEGER test on edge indices
//     ([canonEdgeRangesMeet]).
//   - Source locality is asked in the SOURCE EDGE's own local T frame, never
//     in metres. Within one source edge, two pieces share that frame exactly,
//     so `lo <= hi` overlap is exact. Across consecutive source edges the
//     question is not "are these two measure values equal to within something" —
//     it is "does the earlier piece reach the shared vertex, and does the
//     later piece start from it", which [bandPieces] answers structurally by
//     writing exactly 1 and exactly 0 into the regime bounds at an edge's own
//     ends. Comparing ImportMeasureAtHi against ImportMeasureAtLo in metres instead
//     would be asking a structural question with a float, which is the shape
//     MeasureEps exists to paper over and the reason this function does not use
//     it.
//
// Non-consecutive source edges are never adjacent: a corridor that leaves the
// band for a whole source edge and returns has genuinely left it.
func piecesAdjacent(a, b CorridorPiece) bool {
	if !canonEdgeRangesMeet(a.CanonEdge, a.CanonEdge, b.CanonEdge, b.CanonEdge) {
		return false
	}
	switch {
	case a.SrcEdge == b.SrcEdge:
		return a.Regime.Lo <= b.Regime.Hi && b.Regime.Lo <= a.Regime.Hi
	case a.SrcEdge+1 == b.SrcEdge:
		return a.Regime.Hi == 1 && b.Regime.Lo == 0
	case b.SrcEdge+1 == a.SrcEdge:
		return b.Regime.Hi == 1 && a.Regime.Lo == 0
	}
	return false
}

// RectsBeyond reports whether two lat/lon boxes are provably farther apart than
// marginM.
//
// It is a box-to-box test because the query is an EDGE: an edge can pass close
// to a box with both its endpoints far away, so testing the endpoints would not
// be a lower bound and could prune a real interval.
//
// Longitude metres are scaled by the cosine of the LARGEST latitude either box
// reaches. Cosine falls with latitude, so that is the smallest scale any point
// between them can have, and using it can only UNDERSTATE the true separation —
// which is the direction that keeps the test safe. (pc.cosMin is the same idea
// over the candidate alone; it is not reused because the source edge may sit
// outside the candidate's latitude range entirely.)
func RectsBeyond(aMinLat, aMaxLat, aMinLon, aMaxLon, bMinLat, bMaxLat, bMinLon, bMaxLon, marginM float64) bool {
	dLatM := 0.0
	if aMaxLat < bMinLat {
		dLatM = (bMinLat - aMaxLat) * MetresPerDeg
	} else if bMaxLat < aMinLat {
		dLatM = (aMinLat - bMaxLat) * MetresPerDeg
	}
	dLonDeg := 0.0
	if aMaxLon < bMinLon {
		dLonDeg = bMinLon - aMaxLon
	} else if bMaxLon < aMinLon {
		dLonDeg = aMinLon - bMaxLon
	}
	dLonM := 0.0
	if dLonDeg > 0 {
		maxAbsLat := math.Max(
			math.Max(math.Abs(aMinLat), math.Abs(aMaxLat)),
			math.Max(math.Abs(bMinLat), math.Abs(bMaxLat)))
		dLonM = dLonDeg * MetresPerDeg * math.Cos(maxAbsLat*math.Pi/180)
	}
	return dLatM*dLatM+dLonM*dLonM > marginM*marginM
}

// canonEdgeRangesMeet reports whether two canon edge ranges overlap or are
// adjacent. Adjacency is tested in BOTH directions deliberately: an anti-parallel
// follow walks canon edges downward as the source advances, so the continuation
// of a stretch can sit below it as easily as above.
func canonEdgeRangesMeet(aLo, aHi, bLo, bHi int) bool {
	if aLo <= bHi && bLo <= aHi {
		return true // overlapping
	}
	return bLo == aHi+1 || aLo == bHi+1
}
