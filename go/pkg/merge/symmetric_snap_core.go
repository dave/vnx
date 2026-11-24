package merge

import (
	"fmt"
	"math"
	"sort"
)

// SymmetricSnapDecline names why a candidate node was left alone. Every value
// except [SnapNoDecline] means the leg committed without adopting it, and the
// node remaining near the leg is expected residue rather than a defect.
type SymmetricSnapDecline int

const (
	SnapNoDecline SymmetricSnapDecline = iota

	// SnapDeclineTerminalVertex — the closest approach is the leg's own first
	// or last vertex (Fraction exactly 0 or 1). There is no interior there to
	// cut, and a leg TERMINAL mints and shares no node: a highlight's end is
	// a lat-lon anchor, and is never noded. Mirrors TjDeclineTerminalVertex.
	SnapDeclineTerminalVertex

	// SnapDeclineVertexAlreadySeamed — the candidate's projection lands
	// BITWISE on a leg vertex that a seam already claims for a DIFFERENT node.
	// One leg point cannot join the network in two places, and both seams are
	// Absorbed, so the boundary warp would be asked to move the one vertex
	// onto two nodes tens of metres apart.
	//
	// ⚠ THIS IS THE CALLER'S NODE EXCLUSION, SAID ABOUT POINTS INSTEAD OF
	// NODES. The caller's search already excludes "a node an align seam
	// already claims" — node identity. This is "never adopt onto a leg point
	// an align seam already claims" — point identity. A node-identity
	// exclusion cannot see this case at all, because the two nodes are
	// genuinely different (seen in randomised tests about 20 m apart).
	//
	// Without it, a small share of randomised imports aborted outright, each
	// one an align seam and an adopted candidate on a single vertex.
	SnapDeclineVertexAlreadySeamed

	// SnapDeclineOnCanon — the projection falls INSIDE a stretch the matcher
	// called ON. There, the leg IS canon: a node 15 m off canon at that point
	// belongs to some other trail, and pulling the leg onto it is a canon-side
	// concern, not this leg's.
	//
	// ⚠ WHAT IT COST TO ADOPT THERE, MEASURED. The seam splits the ride into
	// two pieces, NEITHER of which is a canon arc, so ride resolution cannot
	// reference either; the warp then drags the ride off canon to reach the
	// node; and the writer commits two parallel copies of a stretch the leg
	// was riding exactly. On several real-world test areas most candidates
	// (often well over half) fell inside an ON piece, so this is the ordinary
	// case, not an edge.
	//
	// It is ranked LAST so that a candidate declined for any older reason
	// still reports that reason: this rule only ever converts an ADOPTION.
	SnapDeclineOnCanon
)

func (d SymmetricSnapDecline) String() string {
	switch d {
	case SnapNoDecline:
		return "adopted"
	case SnapDeclineTerminalVertex:
		return "declined: closest approach is a leg terminal vertex"
	case SnapDeclineVertexAlreadySeamed:
		return "declined: another node already seams this leg vertex"
	case SnapDeclineOnCanon:
		return "declined: the projection is inside a stretch the leg follows canon along"
	}
	return fmt.Sprintf("symmetricSnapDecline(%d)", int(d))
}

// SymmetricSnapCandidate is one canon node the leg's path passed, and what was
// decided about it. Reported for every candidate the search returned, adopted
// or not — a decline is a fact about the run, and any node left near the leg
// can be checked against these.
type SymmetricSnapCandidate struct {
	Node     NodeID
	Lat, Lon float64 // the NODE's own coordinates, as recorded

	DistM     float64 // node → its projection on the leg
	PriorLenM float64 // along-leg measure of the projection
	Fraction  float64 // of the leg's whole walk
	SegIdx    int     // the leg segment holding the foot of the perpendicular

	// ProjLat/ProjLon are the foot of the perpendicular on the leg — where the
	// vertex is INSERTED, before the warp moves it onto the node. Taken from
	// the search result, never recomputed.
	ProjLat, ProjLon float64

	// StubM is the shorter side of the piece adopting would create, measured
	// against every boundary the leg already carries: how much room the seam
	// actually has. Reported for diagnostics; no rule reads it.
	StubM float64

	Decline SymmetricSnapDecline

	// Idx is the vertex index the adopted seam sits at, in the RETURNED point
	// slice. Meaningless unless Decline == SnapNoDecline.
	Idx int

	// Inserted says whether a new vertex was added for this seam, as against
	// the projection landing bitwise on a vertex the leg already had.
	Inserted bool
}

// SymmetricSnapReport is one leg's whole outcome. Counts rather than a bare
// slice length because "how many did the rule DECLINE" is the number the
// symmetric snap is judged by, and a caller that only sees adoptions cannot
// tell a rule that declined correctly from one that never fired.
type SymmetricSnapReport struct {
	// Skipped is set when the leg was not asked at all — a walk of identical
	// points, which the caller's node search cannot project onto.
	Skipped bool

	Candidates []SymmetricSnapCandidate
}

// Adopted counts the candidates this leg took as seams.
func (r SymmetricSnapReport) Adopted() int {
	n := 0
	for _, c := range r.Candidates {
		if c.Decline == SnapNoDecline {
			n++
		}
	}
	return n
}

// Inserted is every vertex this producer ADDED that the source never had —
// its index in the returned point slice, and the coordinate it was inserted AT.
// Ascending by index.
//
// The caller records it with the leg. A seam whose projection
// landed bitwise on a vertex the leg already carried is NOT here: no vertex was
// added, so there is nothing for a downstream reader to reconstruct.
//
// ⚠ THE COORDINATE IS THE PROJECTION, i.e. the vertex BEFORE the warp moves
// it onto its node, and that is the whole value of recording it. The committed
// coordinate is recoverable from the committed walk; the pre-warp one is not,
// and it is the one a replay needs — handed the post-warp coordinate, the taper
// it replays computes a zero displacement and every neighbouring point in the
// zone stays un-tapered, so the replay diverges from the committed walk.
func (r SymmetricSnapReport) Inserted() []SymmetricSnapInsert {
	var out []SymmetricSnapInsert
	for _, c := range r.Candidates {
		if c.Decline == SnapNoDecline && c.Inserted {
			out = append(out, SymmetricSnapInsert{Idx: c.Idx, Lat: c.ProjLat, Lon: c.ProjLon})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Idx < out[j].Idx })
	return out
}

// DescribeSymmetricSnap renders the report for a failure message.
//
// Exported for the reference application's tests; not part of the stable API.
func DescribeSymmetricSnap(r SymmetricSnapReport) string {
	if r.Skipped {
		return "symmetric snap: SKIPPED (leg walk has no length)"
	}
	s := fmt.Sprintf("symmetric snap: %d candidate(s), %d adopted", len(r.Candidates), r.Adopted())
	for _, c := range r.Candidates {
		s += fmt.Sprintf("\n  node %d (%.7f,%.7f) dist %.3fm at measure %.1fm seg %d stub %.1fm → %s",
			c.Node, c.Lat, c.Lon, c.DistM, c.PriorLenM, c.SegIdx, c.StubM, c.Decline)
	}
	return s
}

// symmetricSnapDeclineFor applies the symmetric snap's decline rules. Pure, so
// each can be tested directly — the same shape [tjunctionDeclineFor] has, and
// for the same reason.
func symmetricSnapDeclineFor(fraction float64, vertexAlreadySeamed, onCanon bool) SymmetricSnapDecline {
	// Mirror of tjunctionDeclineFor rule 3. A fraction of exactly 0 or 1 means
	// the closest approach is the walk's own terminal vertex — the leg's
	// endpoint anchor, which is never noded.
	if fraction == 0 || fraction == 1 {
		return SnapDeclineTerminalVertex
	}
	// See [SnapDeclineVertexAlreadySeamed]. Ranked below the terminal rule
	// because a leg's own end is the stronger statement — it is never noded
	// at all, whoever else is standing there.
	if vertexAlreadySeamed {
		return SnapDeclineVertexAlreadySeamed
	}
	// See [SnapDeclineOnCanon]. Ranked last, deliberately: this rule
	// only converts a candidate that would otherwise have been ADOPTED, so
	// every existing decline still reports the reason it always did.
	if onCanon {
		return SnapDeclineOnCanon
	}
	return SnapNoDecline
}

// nearestBoundaryGapM is the distance from `at` to the closest boundary already
// on the leg — the shorter side of the piece a seam at `at` would create.
//
// The whole set is scanned rather than the two neighbours found by search:
// boundaries is a handful of entries per leg, and a scan cannot go wrong when
// the slice is appended to mid-loop the way the caller appends accepted
// candidates.
func nearestBoundaryGapM(boundaries []float64, at float64) float64 {
	gap := math.Inf(1)
	for _, b := range boundaries {
		if d := math.Abs(at - b); d < gap {
			gap = d
		}
	}
	return gap
}

// legMeasureToVertex is the cumulative along-leg measure at vertex `idx`.
//
// ⚠ IT IS THE SAME RULER, NOT A SECOND ONE, AND THAT IS LOAD-BEARING. StubM
// compares this against `NodeNearPath.PriorLenM`, which the walk
// projection produced by summing HaversineMetres over consecutive vertices of
// THIS SAME ARRAY in ascending order. This is that sum, stopped early: same
// primitive, same operands, same order, so the values are bit-identical rather
// than merely close. [LegMeasureLengthM] is the same sum run to the end, and
// the T-junction split already depends on it agreeing with the warp's own
// measure for exactly this reason.
//
// A test pins this against the projection's own result for the vertex itself
// and requires EXACT equality. If someone reorders the sum or swaps the
// distance primitive, StubM silently starts measuring against a different
// leg.
func legMeasureToVertex(pts []Point, idx int) float64 {
	total := 0.0
	for k := 1; k <= idx; k++ {
		total += HaversineMetres(
			pts[k-1].Lat, pts[k-1].Lon, pts[k].Lat, pts[k].Lon)
	}
	return total
}

// snapProjectionVertex reports which leg vertex a candidate's projection lands
// BITWISE on, if any. The walk projection signals "the foot of the
// perpendicular is a vertex the leg already had" by returning that vertex's
// stored coordinates verbatim rather than a lerp that merely rounds to them, so
// exact equality is the right comparison and a tolerance would be wrong.
//
// ⚠ ONE DEFINITION, TWO CALLERS, DELIBERATELY. The caller's candidate loop
// asks it to decide [SnapDeclineVertexAlreadySeamed]; [insertSymmetricSnapSeams]
// asks it to decide whether to insert a vertex or claim an existing one. If
// those two ever answered differently, the decline would guard a vertex the
// merge does not use and the merge would collide on a vertex the decline did
// not check — a guard that silently stops matching its subject. They cannot
// drift while they are the same function.
func snapProjectionVertex(pts []Point, c NodeNearPath) (int, bool, error) {
	if c.SegIdx < 0 || c.SegIdx >= len(pts)-1 {
		return 0, false, fmt.Errorf(
			"node %d reports segment %d, which is not a segment of a %d-point leg",
			c.ID, c.SegIdx, len(pts))
	}
	a, b := pts[c.SegIdx], pts[c.SegIdx+1]
	switch {
	case c.ProjLat == a.Lat && c.ProjLon == a.Lon:
		return c.SegIdx, true, nil
	case c.ProjLat == b.Lat && c.ProjLon == b.Lon:
		return c.SegIdx + 1, true, nil
	}
	return 0, false, nil
}

// insertSymmetricSnapSeams rebuilds the leg's points with a vertex at each
// adopted projection, and merges the new seams into the align list with every
// index remapped.
//
// ── THE REMAPPING ───────────────────────────────────────────────────────────
//
// `LegSeam.Idx` is a VERTEX INDEX, so inserting a vertex renumbers everything
// after it. THIS PRODUCER DOES THE REMAPPING — nothing downstream will. (The
// boundary warp, ApplyLegBoundaryWarp, remaps nothing and returns seams with
// Idx UNCHANGED.)
//
// The rebuild is a single ordered merge rather than a sequence of splices, and
// that is deliberate: repeated `slices.Insert` shifts indices under the very
// seams still waiting to be placed. Both inputs are already in ascending order — align
// seams by construction, adopted candidates because the caller walked them in
// along-leg order — so one pass emits the new array and both remapped index
// sets together.
//
// A projection can land BITWISE on a vertex the leg already had, which
// the walk projection reports by returning that vertex's stored
// coordinates verbatim rather than a lerp that merely rounds to them. No vertex
// is inserted in that case: the seam claims the existing index. Inserting a
// duplicate point would put a zero-length edge into the committed geometry.
func insertSymmetricSnapSeams(
	pts []Point,
	seams []LegSeam,
	takes []NodeNearPath,
	rep *SymmetricSnapReport,
	ords []int,
) ([]Point, []LegSeam, error) {
	// insertAfter[k] is the list of adopted candidates whose vertex belongs
	// immediately after original vertex k; atVertex[k] is the list that landed
	// bitwise ON original vertex k. Keyed by original index so nothing has to
	// be re-derived once the array starts growing.
	insertAfter := make(map[int][]int, len(takes)) // original seg start → take indices
	atVertex := make(map[int]int, len(takes))      // original vertex → take index

	for i, c := range takes {
		v, onVertex, err := snapProjectionVertex(pts, c)
		if err != nil {
			return nil, nil, fmt.Errorf("InsertSymmetricSnapSeams: %w", err)
		}
		if !onVertex {
			insertAfter[c.SegIdx] = append(insertAfter[c.SegIdx], i)
			continue
		}
		if prev, dup := atVertex[v]; dup {
			return nil, nil, fmt.Errorf(
				"InsertSymmetricSnapSeams: nodes %d and %d both land on leg vertex %d — one point cannot join the network at two nodes",
				takes[prev].ID, c.ID, v)
		}
		atVertex[v] = i
	}

	// newIdxOf[k] is where original vertex k lands in the rebuilt array.
	newIdxOf := make([]int, len(pts))
	outPts := make([]Point, 0, len(pts)+len(takes))
	takeIdx := make([]int, len(takes))       // take → its vertex index in outPts
	takeInserted := make([]bool, len(takes)) // take → did it add a vertex
	for i := range takeIdx {
		takeIdx[i] = -1
	}

	for k, p := range pts {
		newIdxOf[k] = len(outPts)
		outPts = append(outPts, p)
		if ti, ok := atVertex[k]; ok {
			takeIdx[ti] = newIdxOf[k]
		}
		for _, ti := range insertAfter[k] {
			c := takes[ti]
			takeIdx[ti] = len(outPts)
			takeInserted[ti] = true
			outPts = append(outPts, Point{
				Lat: c.ProjLat,
				Lon: c.ProjLon,
				Ele: interpolatedLegEle(pts[k], pts[k+1], c.ProjLat, c.ProjLon),
			})
		}
	}

	// The merged seam list, ascending Idx. Align seams keep every field and
	// take their remapped index; adopted seams are built here.
	out := make([]LegSeam, 0, len(seams)+len(takes))
	for _, s := range seams {
		s.Idx = newIdxOf[s.Idx]
		out = append(out, s)
	}
	for i, c := range takes {
		if takeIdx[i] < 0 {
			return nil, nil, fmt.Errorf(
				"InsertSymmetricSnapSeams: node %d was accepted but placed nowhere in the rebuilt leg — the merge lost it", c.ID)
		}
		out = append(out, LegSeam{
			Idx:     takeIdx[i],
			Node:    c.ID,
			HasNode: true,
			// Every node here existed in canon before this leg's commit — the
			// search excluded everything this leg already claims — so Adopted
			// is unconditional and needs no mintedThisLeg correction.
			Adopted: true,
			// ⚠ Absorbed, NOT merely Adopted. ApplyLegBoundaryWarp moves the
			// leg ONLY for Absorbed; see this producer's own doc.
			Absorbed:  true,
			AbsorbedM: c.DistM,
			NodeLat:   c.Lat,
			NodeLon:   c.Lon,
		})
		rep.Candidates[ords[i]].Idx = takeIdx[i]
		rep.Candidates[ords[i]].Inserted = takeInserted[i]
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Idx < out[j].Idx })

	// Ascending AND STRICTLY increasing: two seams at one index is one point
	// claiming two nodes.
	//
	// ⚠ ONLY THE EQUALITY ARM IS REACHABLE. The `sort.Slice` immediately
	// above orders by Idx, so out[i].Idx >= out[i-1].Idx holds by
	// construction and "out of order" can never be what fired; the message
	// says only what the check can actually see.
	//
	// ⚠ IT IS A BACKSTOP, NOT THE GUARD. [SnapDeclineVertexAlreadySeamed] is
	// the real guard, applied where the
	// candidate is judged; this stays as an assertion because reaching it means
	// that rule did not run, and because it still covers the case that rule
	// cannot see — two ALIGN seams arriving on one vertex, which is not this
	// producer's to decline.
	for i := 1; i < len(out); i++ {
		if out[i].Idx <= out[i-1].Idx {
			return nil, nil, fmt.Errorf(
				"InsertSymmetricSnapSeams: nodes %d and %d both seam leg point %d after the merge — one point cannot join the network at two nodes, and snapDeclineVertexAlreadySeamed should have excluded this",
				out[i-1].Node, out[i].Node, out[i].Idx)
		}
	}
	return outPts, out, nil
}

// interpolatedLegEle is the elevation for a vertex inserted at (lat, lon) on
// the segment a→b.
//
// ⚠ THE RULE IS MIRRORED, NOT INVENTED. It is what the T-junction split
// already does when it puts a node into the middle of a canon segment: linear
// interpolation, and UNKNOWN (nil) when either end is unknown. A missing
// elevation is a legal, expected state at this point in the pipeline — the DEM
// fill runs earlier and can legitimately leave a point unfilled where no tile
// covers it — so nil here is the same absence, not a swallowed failure.
//
// The interpolation weight is the ratio of two haversine distances on the
// segment, and it is used for ELEVATION ONLY. It is not an along-walk position
// and nothing else reads it; every position in this file comes from the search
// result. A zero-length segment (stationary GPS, which real tracks do contain)
// has no ratio to take, and its two ends are the same point, so the start's
// elevation is the answer — a branch, not a fallback.
func interpolatedLegEle(a, b Point, lat, lon float64) *float64 {
	if a.Ele == nil || b.Ele == nil {
		return nil
	}
	segLen := HaversineMetres(a.Lat, a.Lon, b.Lat, b.Lon)
	if segLen == 0 {
		v := *a.Ele
		return &v
	}
	t := HaversineMetres(a.Lat, a.Lon, lat, lon) / segLen
	v := *a.Ele + (*b.Ele-*a.Ele)*t
	return &v
}

// segmentIsOnCanon reports whether leg segment `seg` (the edge from point seg to
// point seg+1) lies inside a stretch the matcher called ON.
//
// The ranges are the assembler's own [OnPieceRange]s, INCLUSIVE at both ends,
// so segment seg is inside when Lo <= seg < Hi: the segment leaving Hi has left
// the ON stretch, and the segment arriving at Lo has not entered it.
func segmentIsOnCanon(onRanges []OnPieceRange, seg int) bool {
	for _, r := range onRanges {
		if seg >= r.Lo && seg < r.Hi {
			return true
		}
	}
	return false
}

// SymmetricSnapInsert is one vertex the symmetric snap added to a leg: the
// index it sits at in the leg's committed point array, and the coordinate it
// was inserted AT.
//
// ⚠ Lat/Lon IS THE PRE-WARP COORDINATE — the projection of the adopted node
// onto the leg — NOT where the vertex ends up. The warp then moves it onto
// the node, and tapers the neighbouring points toward it. The committed
// coordinate is readable from the committed walk at Idx; the pre-warp one is
// recoverable from nothing, and it is the one a replay needs in order to
// reproduce that taper. Given the post-warp coordinate a replay computes a zero
// displacement and leaves the whole zone un-tapered.
type SymmetricSnapInsert struct {
	Idx      int
	Lat, Lon float64
}
