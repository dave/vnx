package merge

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/dave/vnx/go/pkg/detmath"
)

// followBands are the matcher tolerances. Each value is justified by a
// discrimination sweep; do not change one without repeating it. The
// matcher takes them by value so tests can exercise band edges; there is
// no production override path.
type followBands struct {
	EnterM float64 // read only by densifyStep
	StayM  float64 // read only by tests
}

// runKind distinguishes verbatim source spans (OFF) from canon-following
// spans (ON).
type runKind uint8

const (
	runOff runKind = iota
	runOn
)

// runProj is the projection of one source point onto its run's bound
// candidate: the on-line lat/lon, the measure position (cumulative metres
// along the candidate), and the horizontal distance from the source
// point to that on-line point. Evidence for diagnosis and the input to
// the rewrite's boundary interpolation.
type runProj struct {
	Measure float64
	Dist    float64
	Lat     float64
	Lon     float64
}

// followRun is one span of the partitioned leg. [Start, End] are inclusive
// RAW (non-normalised) positions on the leg's source polyline, in
// [sourcePos] terms. OFF runs carry only the range. ON runs additionally
// carry the bound candidate (ID + index into the candidates slice), the
// traversal direction (Reversed = anti-parallel = canon walked
// high-measure→low-measure), the measure interval on that candidate, and one runProj per
// source point in [Start, End].
//
// ⚠ RAW, NOT CANONICAL. [sourceMeasure.normalise] would collapse a position on
// a duplicate-vertex run forward onto the run's last vertex, and an OFF run's
// verbatim walk would then skip every duplicate before it. Start/End are never
// passed through it. Safe to hold this way because nothing compares them with
// `==`; readers convert them to a measure (sourceMeasure.measureAt) or read
// the measure fields instead.
type followRun struct {
	Kind  runKind
	Start sourcePos
	End   sourcePos

	// ON-only.
	CanonID      ArcID
	CanonIdx     int
	Reversed     bool
	MeasureStart float64
	MeasureEnd   float64
	Proj         []runProj

	// ChainHop marks a run that draws an intermediate candidate of a
	// same-layer switch chain — see [corridorRun.ChainHop]. Its Start and End
	// are one source position, so it lies within one source edge by
	// construction, and its direction is the one the chain's switches paid
	// for, not a projection read off the source.
	ChainHop bool
}

// measureEps guards measure comparisons against floating-point noise; it is not a
// tolerance in the band sense.
const measureEps = 1e-9

// PreppedCandidate is a candidate with its cumulative measure lengths
// precomputed. Degenerate candidates (fewer than two vertices, or zero
// total length) are represented by a nil entry in the slice.
type PreppedCandidate struct {
	arcID      ArcID
	Verts      []Point
	cumMeasure []float64
	Total      float64
	// Axis-aligned bounding box, for cheap O(1) distance pruning before the
	// O(vertices) projection scan.
	minLat, maxLat, minLon, maxLon float64
	// Suffix bounding boxes: suff{Min,Max}{Lat,Lon}[i] is the AABB of vertices
	// [i..end]. Lets projectRangeCeil stop a forward scan the moment the point is
	// farther from every remaining vertex than the best projection found — the
	// win in the high-overlap regime, where a followed candidate's later
	// geometry runs ahead of the source so its suffix box is quickly far.
	suffMinLat, suffMaxLat, suffMinLon, suffMaxLon []float64
	// cosMin = cos of the largest-magnitude latitude in the candidate. Used to
	// LOWER-bound the longitude metres of a point-to-suffix-box distance: every
	// edge's midpoint latitude has cos ≥ cosMin, so scaling Δlon by cosMin never
	// overstates the true distance — the box distance stays a valid lower bound
	// and the early-out is result-identical to the full scan.
	cosMin float64
	// Chunk bounding boxes: ch*[c] is the AABB of edges [c*chunkEdges ..
	// (c+1)*chunkEdges). Where the SUFFIX boxes prune a tail (they are nested, so
	// they can only reject a point outside the candidate's whole remaining
	// extent), these prune the MIDDLE: a long candidate — a committed transit leg
	// spanning hundreds of km — has a bounding box that contains almost
	// everything, so a point sitting inside that box but far from the actual line
	// is rejected by no suffix box and forces a full vertex scan. Chunk boxes are
	// local, so such a point skips whole blocks of edges at a time. Pure pruning:
	// a chunk is skipped only when its box lower-bounds farther than the best
	// distance still of interest, so it can never contain the winner.
	chMinLat, chMaxLat, chMinLon, chMaxLon []float64
	// poly is the candidate's [hintPoly], built on first use by [PreppedCandidate.hintPoly]
	// and reused for the rest of this candidate's life. Nil until then.
	poly *hintPoly
}

// chunkEdges is the number of edges per chunk box. Small enough that a chunk's
// AABB stays local on a coarsely-sampled candidate, large enough that the
// per-chunk test is amortised over many edges. Not a tolerance — any value is
// correct, it only trades pruning tightness against bookkeeping.
const chunkEdges = 32

func prepCandidates(canon []CanonArc) []*PreppedCandidate {
	out := make([]*PreppedCandidate, len(canon))
	for i, c := range canon {
		if len(c.Vertices) < 2 {
			continue
		}
		cum := make([]float64, len(c.Vertices))
		for j := 1; j < len(c.Vertices); j++ {
			cum[j] = cum[j-1] + HaversineMetres(
				c.Vertices[j-1].Lat, c.Vertices[j-1].Lon,
				c.Vertices[j].Lat, c.Vertices[j].Lon)
		}
		total := cum[len(cum)-1]
		if total <= 0 {
			continue // zero-length canon — not a candidate
		}
		minLat, maxLat := c.Vertices[0].Lat, c.Vertices[0].Lat
		minLon, maxLon := c.Vertices[0].Lon, c.Vertices[0].Lon
		for _, v := range c.Vertices {
			minLat, maxLat = math.Min(minLat, v.Lat), math.Max(maxLat, v.Lat)
			minLon, maxLon = math.Min(minLon, v.Lon), math.Max(maxLon, v.Lon)
		}
		// Suffix boxes, filled back-to-front: suff[i] covers vertices [i..end].
		n := len(c.Vertices)
		sMinLat := make([]float64, n)
		sMaxLat := make([]float64, n)
		sMinLon := make([]float64, n)
		sMaxLon := make([]float64, n)
		sMinLat[n-1], sMaxLat[n-1] = c.Vertices[n-1].Lat, c.Vertices[n-1].Lat
		sMinLon[n-1], sMaxLon[n-1] = c.Vertices[n-1].Lon, c.Vertices[n-1].Lon
		for j := n - 2; j >= 0; j-- {
			v := c.Vertices[j]
			sMinLat[j], sMaxLat[j] = math.Min(v.Lat, sMinLat[j+1]), math.Max(v.Lat, sMaxLat[j+1])
			sMinLon[j], sMaxLon[j] = math.Min(v.Lon, sMinLon[j+1]), math.Max(v.Lon, sMaxLon[j+1])
		}
		cosMin := math.Cos(math.Max(math.Abs(minLat), math.Abs(maxLat)) * math.Pi / 180.0)
		// Chunk boxes over edges [c*chunkEdges, (c+1)*chunkEdges); a chunk's box
		// spans the vertices of those edges, so it covers vertex indices
		// [c*chunkEdges, min((c+1)*chunkEdges, n-1)] inclusive.
		nedges := n - 1
		nchunks := (nedges + chunkEdges - 1) / chunkEdges
		chMinLat := make([]float64, nchunks)
		chMaxLat := make([]float64, nchunks)
		chMinLon := make([]float64, nchunks)
		chMaxLon := make([]float64, nchunks)
		for ch := range nchunks {
			loV := ch * chunkEdges
			hiV := min((ch+1)*chunkEdges, nedges)
			mnLat, mxLat := c.Vertices[loV].Lat, c.Vertices[loV].Lat
			mnLon, mxLon := c.Vertices[loV].Lon, c.Vertices[loV].Lon
			for j := loV; j <= hiV; j++ {
				v := c.Vertices[j]
				mnLat, mxLat = math.Min(v.Lat, mnLat), math.Max(v.Lat, mxLat)
				mnLon, mxLon = math.Min(v.Lon, mnLon), math.Max(v.Lon, mxLon)
			}
			chMinLat[ch], chMaxLat[ch] = mnLat, mxLat
			chMinLon[ch], chMaxLon[ch] = mnLon, mxLon
		}
		out[i] = &PreppedCandidate{
			arcID:      c.ID,
			Verts:      c.Vertices,
			cumMeasure: cum,
			Total:      total,
			minLat:     minLat, maxLat: maxLat, minLon: minLon, maxLon: maxLon,
			suffMinLat: sMinLat, suffMaxLat: sMaxLat, suffMinLon: sMinLon, suffMaxLon: sMaxLon,
			cosMin:   cosMin,
			chMinLat: chMinLat, chMaxLat: chMaxLat, chMinLon: chMinLon, chMaxLon: chMaxLon,
		}
	}
	return out
}

// Exported for the reference application's tests; not part of the stable API.
const MetresPerDeg = math.Pi * 6371000.0 / 180.0

// ClosestPointOnSeg returns the planar distance in metres from
// (probeLat, probeLon) to the segment a→b, plus the parameter t ∈ [0,1] of
// the closest point along it. Longitude is scaled by cos(mid latitude) so
// the result is metric for short segments at any latitude. Degenerate
// segments (a == b) fall back to point-to-point at t=0.
//
// Exported for the reference application's tests; not part of the stable API.
func ClosestPointOnSeg(aLat, aLon, bLat, bLon, probeLat, probeLon float64) (distM, t float64) {
	midLat := (aLat + bLat) * 0.5
	cosLat := detmath.Cos(midLat * math.Pi / 180.0)

	// Every product that meets an add or a subtract is converted to float64,
	// and the trig is detmath's: the result is the same on arm64, which fuses
	// x*y+z into one FMA, as on amd64, which does not (pkg/detmath).
	bx := float64((bLon - aLon) * MetresPerDeg * cosLat)
	by := float64((bLat - aLat) * MetresPerDeg)
	px := float64((probeLon - aLon) * MetresPerDeg * cosLat)
	py := float64((probeLat - aLat) * MetresPerDeg)

	lenSq := float64(bx*bx) + float64(by*by)
	if lenSq == 0 {
		return detmath.Hypot(px, py), 0
	}
	t = (float64(px*bx) + float64(py*by)) / lenSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	cx := float64(t * bx)
	cy := float64(t * by)
	return detmath.Hypot(px-cx, py-cy), t
}

// densifyStep is the internal densification resolution (metres). The
// source is a LINE, so matching must behave as the infinitely-densified limit —
// a long edge that crosses the bands mid-span must follow even when no vertex
// lands in-band. We reach that limit by resampling every source edge to this
// step before classification (band crossings are then located EXACTLY by
// bisection, so this is a DETECTION resolution, not a matching tolerance — the
// bands are untouched). DERIVED from the Enter band, not a free constant: eight
// samples across Enter resolve any crossing, and the step (Enter/8 = 1 m) sits
// far below 12.5 m, half the 25 m shortest coincidence worth believing, so
// every in-band feature that long spans many samples. Convergence is pinned by the dense-limit fixture
// (sparse == densified) and its halving no-op check.
func densifyStep(b followBands) float64 { return b.EnterM / 8 }

// densifySource resamples the leg to the given step: every original vertex is
// kept (orig true) and interior points are inserted along each edge at the
// step (orig false). Deterministic and pure. [assembleFollowedLegRunsCorridor]
// uses it to build the source path its node-snap corridor gate measures
// against. Elevation is linearly interpolated for inserted points; they are never
// committed verbatim (OFF emits only original vertices, ON emits canon), so
// their ele is immaterial and kept only for well-formedness.
func densifySource(pts []Point, step float64) (dpts []Point, orig []bool) {
	if len(pts) == 0 {
		return nil, nil
	}
	dpts = append(dpts, pts[0])
	orig = append(orig, true)
	for i := 0; i+1 < len(pts); i++ {
		a, c := pts[i], pts[i+1]
		segLen := HaversineMetres(a.Lat, a.Lon, c.Lat, c.Lon)
		if segLen > step {
			nins := int(math.Floor(segLen / step))
			// A crossing exactly at the far vertex needs no extra interior point.
			if float64(nins)*step >= segLen {
				nins--
			}
			for j := 1; j <= nins; j++ {
				f := float64(j) * step / segLen
				dpts = append(dpts, Point{
					Lat: a.Lat + float64(f*(c.Lat-a.Lat)), // float64: no FMA, the same on every arch (pkg/detmath)
					Lon: a.Lon + float64(f*(c.Lon-a.Lon)),
					Ele: lerpEle(a.Ele, c.Ele, f),
				})
				orig = append(orig, false)
			}
		}
		dpts = append(dpts, c)
		orig = append(orig, true)
	}
	return dpts, orig
}

func lerpEle(a, c *float64, f float64) *float64 {
	if a == nil || c == nil {
		return nil
	}
	v := *a + float64(f*(*c-*a))
	return &v
}

// onLineEps bounds how far an assembled ON point may sit off its bound
// canon polyline before the post-condition rejects it. It is a
// floating-point guard, not a matching tolerance: interior points are
// byte-copied canon vertices (distance 0) and boundary points are
// interpolated ON a canon arc, so legitimate geometry sits at ~1e-9 m.
const onLineEps = 1e-6

// snapMeasureToCandidateEnd fixes a handoff defect: two point-contiguous ON
// runs of one leg, riding adjacent canon arcs with no OFF run between them,
// never left canon — but the second run's own measure boundary is the
// MATCHER's provisional choice, not a re-derivation against the shared node,
// and it can land a few metres short. Measured on a real multi-day track:
// run 0 ends EXACTLY at node n3 (measure 0.000 of its own candidate); run 1,
// on the adjacent candidate, starts at measure 325.298 of a 327.305 m arc —
// 2.007 m past n3 instead of at it. The assembled leg then draws its own
// CHORD from n3 to that interior point, skipping canon's own vertices in
// between: two lines where there should be one, and no node where they
// rejoin.
//
// ⚠ THIS IS EndpointWarpRadiusM (15 m), NOT A RUN LENGTH: this is a
// DISPLACEMENT — the join moves by up to `near` metres — and 15 m is the
// budget chosen for exactly that question (see [EndpointWarpRadiusM]).
// A run length (the shortest coincidence worth believing) has no bearing on
// how far a join is allowed to move.
//
// ⚠ NOT [EndpointWarpRadiusM] SHRUNK OR REPURPOSED — it is the SAME budget
// applied to a DIFFERENT boundary. [warpLegEndpoints]' callers use it to
// resolve a LEG'S OWN TERMINAL against a nearby node; this resolves an
// INTERIOR ON-run boundary, between two runs of the same leg, against the
// node they share. Neither subsumes the other: a leg terminal has no
// adjacent run to hand off to, and an interior handoff is not the leg's end.
//
// Symmetric by construction (near end OR far end, whichever is closer) so it
// does not care whether the run is forward or reversed.
func snapMeasureToCandidateEnd(measure, total float64) float64 {
	if near := math.Min(measure, total-measure); near < EndpointWarpRadiusM {
		if measure <= total-measure {
			return 0
		}
		return total
	}
	return measure
}

// nodeLookup answers "is there an existing committed node within radiusM of
// (lat, lon)" and, if so, WHERE it actually is — the ONLY way the otherwise
// pure assembler (no storage access, no clock, no globals and no package
// state) can ask about network state beyond the candidate geometry it was
// handed. A committed network node is not reachable from
// `[]CanonArc{ID, Vertices}` at all, and the storage lookup that
// answers it needs a transaction the assembler never receives — so the
// capability is INJECTED as a closure rather than threading a transaction
// through a function whose whole design point is having none.
//
// ⚠ REQUIRED, NOT OPTIONAL — see [noNodeLookup]. A parameter that quietly
// disables a feature when nil invites a fix that production never reaches:
// every caller must say, in code that compiles, whether it has node access
// or not.
type nodeLookup func(lat, lon, radiusM float64) (foundLat, foundLon float64, id NodeID, found bool, err error)

// snapMeasureAnyNode "rides canon" onto ANY existing node within
// EndpointWarpRadiusM of a boundary's on-line position, not only a
// candidate's own ends. This reaches a case [snapMeasureToCandidateEnd]
// cannot: in one measured track, node n3 sits 8.75 m from the boundary but
// INTERIOR to the ~12.28 km arc the leg self-follows there — nowhere near
// either of that candidate's own ends (245 m away), so no amount of
// candidate-end snapping finds it. Only a real node lookup can.
//
// ⚠ TAPER WAS REJECTED, RIDING WAS CHOSEN — and the difference matters here
// specifically. At an absorbed boundary the leg is FOLLOWING canon: its
// committed vertices over that run already ARE canon's vertices. A taper
// would blend the boundary's displacement back over the leg's own last few
// source-derived points, which moves them OFF canon and makes the piece stop
// being byte-identical to the trail it duplicates — exactly what a later
// merge of duplicate pieces requires, and exactly the wrong direction for
// the near-miss population this measurably grows (a sweep of mergeable
// pairs found near-misses rose with absorb radius in every test area, in
// one from 265 to 610). Riding canon — extending the run's own measure
// bounds out to the node, the same mechanism [assembleOnRun]'s
// interior-vertex filter already uses for a refined crossing — commits
// canon's OWN vertices from the node onward instead, so the piece stays
// byte-identical to whatever else already rides that stretch.
//
// ⚠ THE FOUND NODE MUST PROJECT ON-LINE, WITHIN onLineEps, OR IT IS DECLINED.
// "Ride canon to a node" presupposes the node has a real measure position on
// THIS candidate; a node merely nearby but off this candidate's own geometry
// is a different junction expressed as a coincidence, not this one, and
// snapping to it would invent a chord to reach it — reproducing the defect
// this exists to remove. onLineEps (1e-6 m) is the same tolerance post-
// condition 1 already requires of every on-line assembled point, not a new
// band invented for this case.
//
// ⚠ RADIUS IS EndpointWarpRadiusM, DELIBERATELY: this is the same operation
// as the terminal warp — moving a point onto a nearby node — so it carries
// the same budget. If evidence says the interior case wants its own number,
// give it one explicitly rather than inheriting whichever constant happened
// to be nearby.
//
// ⚠ NOT CORRIDOR-CHECKED HERE. It used to be — this function and
// [snapMeasureToCandidateEnd] each carried their own per-end proxy, and both
// proxies were separately measured insufficient (see [checkCorridor]'s
// header). The caller now applies the real corridor predicate ONCE, to both
// ends together, after [reconcileSnappedMeasures] — see that call site. This
// returns the raw candidate: snapped or not, on-line or declined for being
// off it, with nothing yet said about Stay.
// for being off it, with nothing yet said about Stay.
func snapMeasureAnyNode(pc *PreppedCandidate, measure float64, lookup nodeLookup) (float64, error) {
	if lookup == nil {
		return 0, errors.New("node snap: node lookup is nil")
	}
	p := boundaryPoint(pc, measure)
	nlat, nlon, _, found, err := lookup(p.Lat, p.Lon, EndpointWarpRadiusM)
	if err != nil {
		return 0, fmt.Errorf("node snap: node lookup at (%.9f,%.9f): %w", p.Lat, p.Lon, err)
	}
	if !found {
		return measure, nil
	}
	nodeMeasure, dist, _, _, ok := pc.projectRangeCeil(nlat, nlon, 0, pc.Total, onLineEps)
	if !ok || dist > onLineEps {
		// The nearby node is not on THIS candidate's own line — a different
		// junction, coincidentally close. Nothing to ride to here.
		return measure, nil
	}
	return nodeMeasure, nil
}

// snapMeasureToNearbyNodeCensus snaps a run-boundary measure onto a nearby
// node — first the candidate's own end ([snapMeasureToCandidateEnd]), then any
// existing node on the candidate's own line ([snapMeasureAnyNode]) — and
// reports which mechanism produced the change: "own-end", "any-node", or
// "none". The mechanism string is for measurement only; the live assembler
// discards it.
func snapMeasureToNearbyNodeCensus(pc *PreppedCandidate, measure float64, lookup nodeLookup) (float64, string, error) {
	if snapped := snapMeasureToCandidateEnd(measure, pc.Total); snapped != measure {
		return snapped, "own-end", nil
	}
	any, err := snapMeasureAnyNode(pc, measure, lookup)
	if err != nil {
		return 0, "", err
	}
	if any != measure {
		return any, "any-node", nil
	}
	return measure, "none", nil
}

// reconcileSnappedMeasures is the pairwise safety check neither snap call can make
// on its own: each end is snapped independently against ITS OWN nearer end of
// the candidate, so on a SHORT candidate (total under roughly
// 2×EndpointWarpRadiusM) a run sandwiched between two ON neighbours can have
// BOTH ends qualify and snap toward DIFFERENT ends, inverting the pair.
//
// ⚠ MEASURED, NOT ARGUED. On a real track: a length-1 run (Start==End, one
// comb sample) on a short candidate had MeasureStart snap toward the FAR end
// while MeasureEnd snapped toward the NEAR end, producing a forward measure
// pair [1.4093721404413397, 1.3570624077919646] — DESCENDING — which
// assembleOnRun's post-condition 2 correctly refused ("forward measures not
// ascending"), aborting the whole leg. That is the post-condition doing its
// job; the fix is not to weaken it but to stop handing it an inverted pair in
// the first place.
//
// If applying BOTH tentative snaps keeps the interval correctly ordered for
// `reversed` (forward: start <= end; reversed: start >= end, both with
// measureEps slack), they are kept. Otherwise BOTH are declined — reverting to
// origStart/origEnd — rather than guessing which one "should" win: nothing in
// either snap's own computation says which end more deserves the boundary,
// and reverting both is the pre-fix, already-valid behaviour.
func reconcileSnappedMeasures(origStart, origEnd, snapStart, snapEnd float64, reversed bool) (float64, float64) {
	ordered := snapStart <= snapEnd+measureEps
	if reversed {
		ordered = snapStart >= snapEnd-measureEps
	}
	if ordered {
		return snapStart, snapEnd
	}
	return origStart, origEnd
}

// checkCorridor asks the symmetric corridor question: is the corridor held —
// source sub-path and canon sub-polyline each within Stay of the other — over
// the WHOLE stretch, in BOTH directions? The node-snap path asks it of a
// tentative measure pair before committing to it, instead of a hand-rolled
// proxy.
//
// Two proxies were tried before it, in order:
//
//  1. A single-anchor endpoint check — the snapped position alone against
//     Stay. Passed every existing test; still let two randomized tests fail
//     the corridor by about a metre once the any-node fallback widened which
//     boundaries reach a snap at all (16.80 m and 15.11 m:
//     a crossing extension and a randomised self-consistency test). Canon
//     between the old boundary and the node is not a straight line, so a single endpoint check cannot
//     bound the worst point in between.
//  2. A check of every point canon newly covers, closing (1), but only
//     canon-against-source. It never asked the REVERSE direction: every
//     source sample within Stay of the newly-shifted canon. Real tracks
//     failed it as "source point N is X m from committed canon", the
//     corridor's OTHER loop, which this narrower check never ran at all.
//
// Both proxies passed their unit tests and both failed on real tracks,
// because a proxy can only be wrong in a way its author did not think of.
// This function is not a proxy: it checks both directions, so the snap path
// can never disagree about what "in corridor" means — there is exactly one
// corridor predicate in this file.
//
// srcOffset is added to each source-loop index to reproduce assembleOnRun's
// leg-relative error text; pass 0 for a caller with no such offset (src is
// already leg-relative there). reversed controls interior-vertex order,
// matching assembleOnRun's own canon-direction handling — geometrically inert
// for a distance check (a polyline's edge set does not depend on which end is
// listed first) but kept for exact parity with the extracted code.
func checkCorridor(src []Point, srcOffset int, pc *PreppedCandidate, measureStart, measureEnd float64, entryCross, exitCross *Point, reversed bool, stayM float64) error {
	lo, hi := measureStart, measureEnd
	if lo > hi {
		lo, hi = hi, lo
	}

	var interior []Point
	for i := range pc.Verts {
		a := pc.cumMeasure[i]
		if a > lo+measureEps && a < hi-measureEps {
			interior = append(interior, cloneImportPoint(pc.Verts[i]))
		}
	}
	if reversed {
		reverseImportPoints(interior)
	}

	entryOnline := boundaryPoint(pc, measureStart)
	exitOnline := boundaryPoint(pc, measureEnd)
	canonSub := make([]Point, 0, len(interior)+2)
	canonSub = append(canonSub, entryOnline)
	canonSub = append(canonSub, interior...)
	canonSub = append(canonSub, exitOnline)

	// The source stretch this run covers. When the caller refined a boundary to
	// an exact band crossing it ALSO extended the measure bounds to that crossing, so
	// the canon sub-polyline above reaches geometry whose matching source lies
	// between the crossing and the first/last ON sample. Measuring that canon
	// against `src` alone would compare it to a source path
	// that stops short of its own endpoint, and report a distance up to the
	// sample spacing too large — a false corridor violation right at the band
	// edge, where the exit crossing sits at exactly Stay by construction.
	// entryCross precedes src and exitCross follows it in SOURCE order
	// (independent of canon direction), so this stays a well-ordered path.
	// This tightens nothing: the crossings are themselves committed points, and
	// each is within its own band of canon by construction.
	//
	// srcPath is src plus the refined band crossings, and is used ONLY for the
	// canon→source direction below. The two directions need different paths:
	//
	//   - source→canon asserts that every source SAMPLE the matcher classified ON
	//     really is within Stay. A crossing is not such a sample; it is a derived
	//     boundary sitting at EXACTLY Stay by construction, so asserting it is
	//     within Stay + onLineEps would assert the crossing's solve converged
	//     to 1e-6, which is not its contract. It stays out of that loop.
	//   - canon→source must include them. The caller extended the measure bounds out
	//     to the crossings, so the canon sub-polyline reaches geometry whose
	//     matching source lies between a crossing and the first/last ON sample.
	//     Measuring against src alone compares that canon to a source path that
	//     stops short of its own endpoint and reports a distance up to the sample
	//     spacing too large — a FALSE corridor violation, always at the band edge.
	//     (Measured on a real track: a committed vertex measured 15.0096 m
	//     from a source path missing the very crossing that bounded it.)
	srcPath := src
	if entryCross != nil || exitCross != nil {
		ext := make([]Point, 0, len(src)+2)
		if entryCross != nil {
			ext = append(ext, *entryCross)
		}
		ext = append(ext, src...)
		if exitCross != nil {
			ext = append(ext, *exitCross)
		}
		srcPath = ext
	}

	stay := stayM + onLineEps
	canonPoly := newHintPoly(canonSub)
	srcPoly := newHintPoly(srcPath)
	// Source and canon follow one another, so successive nearest points advance
	// monotonically along the other polyline — a running hint keeps each O(local).
	hSrc := 0
	for i := range src {
		var d float64
		d, hSrc = canonPoly.NearestDist(src[i].Lat, src[i].Lon, hSrc)
		if d > stay {
			return fmt.Errorf("source point %d is %.4f m from committed canon (>Stay %.1f)",
				srcOffset+i, d, stayM)
		}
	}
	hCanon := 0
	for i := range canonSub {
		var d float64
		d, hCanon = srcPoly.NearestDist(canonSub[i].Lat, canonSub[i].Lon, hCanon)
		if d > stay {
			return fmt.Errorf("committed point %d is %.4f m from source (>Stay %.1f)",
				i, d, stayM)
		}
	}
	return nil
}

// assembleOnRun builds one ON run's committed points and verifies the
// per-run post-conditions. Boundary points substitute the verbatim source
// terminal when the run reaches a leg end; the measure interval is always
// validated against the on-line geometry the matcher chose.
// entryCross / exitCross are the refined band crossings, when the caller
// minted them: the caller extends the run's measure bounds out to them, so
// the canon sub-polyline this function emits spans source geometry that runs
// from entryCross, through pts[run.Start..run.End], to exitCross.
//
// It takes only `legFirst` and `legLast` as source geometry, which is all it
// semantically needs; it has no source-slice parameter, so a caller that
// reads the comb rather than a whole densified array can call it too.
//
// atLegStart/atLegEnd say whether this run's Start/End sits at the LEG's own
// first/last position; the caller ([assembleFollowedLegRunsCorridor]) computes
// them.
func assembleOnRun(legFirst, legLast Point, pc *PreppedCandidate, run followRun, b followBands, entryCross, exitCross *Point, atLegStart, atLegEnd bool) ([]Point, error) {
	lo, hi := run.MeasureStart, run.MeasureEnd
	if lo > hi {
		lo, hi = hi, lo
	}

	// Interior canon vertices strictly between the measure bounds, byte-copied,
	// reversed when anti-parallel. Collect their measures for the monotone check.
	var interior []Point
	var interiorMeasures []float64
	for i := range pc.Verts {
		a := pc.cumMeasure[i]
		if a > lo+measureEps && a < hi-measureEps {
			interior = append(interior, cloneImportPoint(pc.Verts[i]))
			interiorMeasures = append(interiorMeasures, a)
		}
	}
	if run.Reversed {
		reverseImportPoints(interior)
		reverseFloats(interiorMeasures)
	}

	// On-line boundaries (always on the canon line) — used for the
	// post-conditions regardless of terminal substitution.
	entryOnline := boundaryPoint(pc, run.MeasureStart)
	exitOnline := boundaryPoint(pc, run.MeasureEnd)

	// Post-condition 1: every on-line point lies on the bound canon polyline.
	canonSub := make([]Point, 0, len(interior)+2)
	canonSub = append(canonSub, entryOnline)
	canonSub = append(canonSub, interior...)
	canonSub = append(canonSub, exitOnline)
	pcPoly := pc.hintPoly()
	// The points advance monotonically along pc (interior measures are ordered), so
	// a running hint keeps each lookup local; seed it at the entry measure's edge.
	hint := sort.Search(len(pc.Verts)-1, func(k int) bool { return pc.cumMeasure[k+1] >= run.MeasureStart })
	for i, p := range canonSub {
		var d float64
		d, hint = pcPoly.NearestDist(p.Lat, p.Lon, hint)
		if d > onLineEps {
			return nil, fmt.Errorf("ON run: run [%+v,%+v] arc %d point %d lies %.6g m off canon (>%.0e)",
				run.Start, run.End, run.CanonID, i, d, onLineEps)
		}
	}

	// Post-condition 2: contiguous, measure-monotone interval.
	measureSeq := make([]float64, 0, len(interiorMeasures)+2)
	measureSeq = append(measureSeq, run.MeasureStart)
	measureSeq = append(measureSeq, interiorMeasures...)
	measureSeq = append(measureSeq, run.MeasureEnd)
	for i := 1; i < len(measureSeq); i++ {
		if run.Reversed && measureSeq[i] > measureSeq[i-1]+measureEps {
			return nil, fmt.Errorf("ON run: run [%+v,%+v] arc %d reverse measures not descending: %v",
				run.Start, run.End, run.CanonID, measureSeq)
		}
		if !run.Reversed && measureSeq[i] < measureSeq[i-1]-measureEps {
			return nil, fmt.Errorf("ON run: run [%+v,%+v] arc %d forward measures not ascending: %v",
				run.Start, run.End, run.CanonID, measureSeq)
		}
	}

	// No whole-run corridor check here. The corridor solve that proposes ON
	// runs deliberately proposes runs a source-within-Stay abort would refuse
	// (a hairpin); fabrication is guarded by the solve's own canon-side gap
	// term, not by a post-hoc reject here. [checkCorridor] still gates the
	// node-snap sites in follow_corridor_assemble.go — a snap is a separate
	// decision (whether to ride a measure onto a nearby node) the solve never
	// makes, so it keeps its own accept/decline gate.

	// Committed points. At a leg terminal the verbatim source point is KEPT —
	// following never moves a plan's endpoints (that is the terminal warp's
	// job) — but it is emitted IN ADDITION to the on-line boundary, not
	// INSTEAD of it.
	//
	// Substituting it would drop entryOnline/exitOnline entirely, so the
	// committed line ran from the terminal straight to the first INTERIOR canon
	// vertex. Canon vertex spacing is the source's, not ours: where canon has a
	// long straight edge the line reaches it only at that edge's far end, and
	// everything before it is a taper drawn beside canon rather than on it.
	// Measured on a real track that starts 2.42 m off canon with canon's next
	// vertex 136 m away: 136 m of road committed as a second strand tapering
	// 2.4 m -> 0 — two parallel lines where there should be one. A neighbouring
	// track escaped it only by luck: its terminal is 0.78 m off and canon
	// happens to have a vertex 3.9 m along, so its taper is 4 m.
	//
	// Emitting both keeps the anchor exactly where the source put it AND joins
	// canon at the measure the matcher actually chose, so the taper collapses to a
	// single sub-Stay hop at the terminal. entryOnline is within Stay of legFirst
	// by construction (the point is ON, so it passed the band), so this commits
	// no geometry the corridor has not already accepted.
	outPts := make([]Point, 0, len(interior)+4)
	if atLegStart && !samePoint(legFirst, entryOnline) {
		outPts = append(outPts, legFirst)
	}
	outPts = append(outPts, entryOnline)
	outPts = append(outPts, interior...)
	outPts = append(outPts, exitOnline)
	if atLegEnd && !samePoint(legLast, exitOnline) {
		outPts = append(outPts, legLast)
	}
	return outPts, nil
}

// samePoint reports whether two committed points occupy the identical lat/lon.
// Exact comparison is the right test here: both sides are either byte-copied
// canon vertices or interpolations that land exactly on a vertex, so "equal"
// means "the same point", and anything else is a real, if short, hop.
func samePoint(a, b Point) bool {
	return a.Lat == b.Lat && a.Lon == b.Lon
}

// boundaryPoint is the on-line point at measure position `at`: lat/lon on the
// canon line plus the linearly interpolated elevation of the bracketing
// canon vertices (nil if either bound's ele is nil).
func boundaryPoint(pc *PreppedCandidate, at float64) Point {
	lat, lon := pc.interpOnCanon(at)
	return Point{Lat: lat, Lon: lon, Ele: pc.interpEle(at)}
}

// hintPoly wraps a polyline with prefix and suffix bounding boxes so nearestDist
// can find the true nearest-absolute distance from a point in O(local) work when
// successive queries advance monotonically along it — exactly the assembly
// post-conditions, where source and canon follow one another. The box lower
// bound uses cosMin (cos of the largest-magnitude latitude) for longitude
// metres, which never overstates a distance, so the prefix/suffix pruning is
// result-identical to a full linear point-to-polyline scan.
type hintPoly struct {
	verts                                      []Point
	preMinLat, preMaxLat, preMinLon, preMaxLon []float64
	sufMinLat, sufMaxLat, sufMinLon, sufMaxLon []float64
	cosMin                                     float64
}

func newHintPoly(verts []Point) *hintPoly {
	hp := &hintPoly{verts: verts}
	n := len(verts)
	if n == 0 {
		return hp
	}
	hp.preMinLat, hp.preMaxLat = make([]float64, n), make([]float64, n)
	hp.preMinLon, hp.preMaxLon = make([]float64, n), make([]float64, n)
	hp.sufMinLat, hp.sufMaxLat = make([]float64, n), make([]float64, n)
	hp.sufMinLon, hp.sufMaxLon = make([]float64, n), make([]float64, n)
	hp.preMinLat[0], hp.preMaxLat[0] = verts[0].Lat, verts[0].Lat
	hp.preMinLon[0], hp.preMaxLon[0] = verts[0].Lon, verts[0].Lon
	for i := 1; i < n; i++ {
		v := verts[i]
		hp.preMinLat[i], hp.preMaxLat[i] = math.Min(v.Lat, hp.preMinLat[i-1]), math.Max(v.Lat, hp.preMaxLat[i-1])
		hp.preMinLon[i], hp.preMaxLon[i] = math.Min(v.Lon, hp.preMinLon[i-1]), math.Max(v.Lon, hp.preMaxLon[i-1])
	}
	hp.sufMinLat[n-1], hp.sufMaxLat[n-1] = verts[n-1].Lat, verts[n-1].Lat
	hp.sufMinLon[n-1], hp.sufMaxLon[n-1] = verts[n-1].Lon, verts[n-1].Lon
	for i := n - 2; i >= 0; i-- {
		v := verts[i]
		hp.sufMinLat[i], hp.sufMaxLat[i] = math.Min(v.Lat, hp.sufMinLat[i+1]), math.Max(v.Lat, hp.sufMaxLat[i+1])
		hp.sufMinLon[i], hp.sufMaxLon[i] = math.Min(v.Lon, hp.sufMinLon[i+1]), math.Max(v.Lon, hp.sufMaxLon[i+1])
	}
	hp.cosMin = math.Cos(math.Max(math.Abs(hp.preMinLat[n-1]), math.Abs(hp.preMaxLat[n-1])) * math.Pi / 180.0)
	return hp
}

// boxDistM is the planar metres from (lat,lon) to an axis-aligned box, using
// cosMin for the longitude axis so the result is a true lower bound on the
// distance to any edge inside the box. Zero when the point is inside.
func boxDistM(lat, lon, minLat, maxLat, minLon, maxLon, cosMin float64) float64 {
	dLatM := 0.0
	if lat < minLat {
		dLatM = (minLat - lat) * MetresPerDeg
	} else if lat > maxLat {
		dLatM = (lat - maxLat) * MetresPerDeg
	}
	dLonM := 0.0
	if lon < minLon {
		dLonM = (minLon - lon) * MetresPerDeg * cosMin
	} else if lon > maxLon {
		dLonM = (lon - maxLon) * MetresPerDeg * cosMin
	}
	return math.Hypot(dLatM, dLonM)
}

// cloneImportPoint deep-copies a point so the assembled output never
// aliases the candidate's storage.
func cloneImportPoint(p Point) Point {
	return Point{Lat: p.Lat, Lon: p.Lon, Ele: cloneEle(p.Ele)}
}

func cloneEle(e *float64) *float64 {
	if e == nil {
		return nil
	}
	v := *e
	return &v
}

func reverseImportPoints(s []Point) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func reverseFloats(s []float64) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// hintPoly returns the candidate's whole-polyline [hintPoly], building it once.
//
// ── WHY IT IS CACHED, MEASURED ──────────────────────────────────────────────
//
// [assembleOnRun] needs this for post-condition 1 and used to call
// [newHintPoly] itself, so the eight length-n float64 arrays were rebuilt from
// scratch FOR EVERY ON RUN over the FULL candidate — O(ON runs × candidate
// vertices) allocation for a value that depends only on `verts`, which never
// changes.
//
// On a worst-case real leg (5,000 source points, 2,714,561 densified)
// against its real canon (19 candidates, 88,540 vertices, 3,712 ON runs),
// that rebuild allocated **2,629 MB — 75% of the whole follow path's
// allocation**, against 387 MB for the densified source substrate and 417 MB
// for the partition. Measured with GC disabled, so the figure is total bytes
// allocated, exact rather than sampled.
//
// Caching is byte-identical, not merely close: a [hintPoly] is a pure function
// of `verts` (prefix/suffix boxes and cosMin, all derived), nothing mutates
// `verts` after [prepCandidates] builds it, and [hintPoly.nearestDist] only
// reads — it takes its hint as an argument and stores nothing. Each
// [prepCandidates] call builds fresh candidates that never outlive their one
// leg, so there is no sharing across legs and no lock is needed.
func (pc *PreppedCandidate) hintPoly() *hintPoly {
	if pc.poly == nil {
		pc.poly = newHintPoly(pc.Verts)
	}
	return pc.poly
}

// projectRangeCeil projects (lat, lon) onto the candidate and returns the
// nearest projection whose measure lies in [minMeasure, maxMeasure], with a
// caller-supplied distance CEILING: the caller states the largest distance it
// could still act on, and the scan may stop as soon as no remaining edge can
// beat it.
//
// The ceiling is pure pruning. It tightens only the suffix-box early-out
// BOUND; the winner selection is untouched (still the nearest projection in
// the measure window). Two properties make the result the same as an unpruned
// scan for the caller:
//
//   - If the true nearest projection is within ceilM, it is still returned. At
//     every edge preceding it the suffix box CONTAINS that projection, so the
//     box's lower bound is ≤ its distance ≤ ceilM ≤ bound — the break cannot
//     fire before it is found.
//   - If the scan does stop early, the only outcomes lost are projections
//     FARTHER than ceilM, which the caller discards on the very next line
//     ([snapMeasureAnyNode]: `dist > onLineEps`). ok=false and a too-far
//     ok=true are handled identically there.
//
// This is what keeps a far point O(1) per candidate instead of O(vertices).
// Without it, `best` stays +Inf until the first projection lands, so a point
// hundreds of km from a long, coarsely-sampled candidate (a committed transit
// leg) never triggers the early-out and scans every edge — while the
// whole-candidate AABB is far too coarse to reject it.
func (pc *PreppedCandidate) projectRangeCeil(lat, lon, minMeasure, maxMeasure, ceilM float64) (measure, dist, plat, plon float64, ok bool) {
	best := math.Inf(1)
	nedges := len(pc.Verts) - 1
	// Skip edges that end before the window starts — their whole measure range is
	// below minMeasure, so their (measure-clamped) projection is filtered out anyway.
	// cumMeasure is monotone, so a binary search finds the first in-window edge; a
	// narrow window (the common continuation case) then scans only a few edges
	// instead of the whole candidate. Result-identical to a full scan.
	lo := sort.Search(nedges, func(i int) bool { return pc.cumMeasure[i+1] >= minMeasure-measureEps })
	// hasCeil is hoisted: testing it per edge showed up as ~5% of total CPU in
	// its own right on long-candidate inputs.
	hasCeil := !math.IsInf(ceilM, 1)
	for i := lo; i < nedges; i++ {
		if pc.cumMeasure[i] > maxMeasure+measureEps {
			break // this edge and all after it start past the window
		}
		// Suffix-box lower bound: vertices [i..end] all lie in the precomputed
		// suffix box, so the point's distance to that box floors its distance to
		// every remaining edge (cosMin never overstates the longitude metres —
		// every edge midpoint's cos ≥ cosMin — so this is a true lower bound).
		// Once that floor exceeds the best projection found, no later edge can
		// win: stop. Result-identical to scanning to maxMeasure, and the win in the
		// followed-duplicate regime, where a candidate's later geometry runs
		// ahead of the source and its suffix box goes far within a few edges.
		//
		// The bound is min(best, ceiling): before the first hit `best` is +Inf,
		// so an unbounded caller behaves exactly as before, while a caller that
		// supplied a ceiling can stop on the FIRST edge — the far-point case.
		bound := best
		if ceilM < bound {
			bound = ceilM
		}
		if hasCeil || ok {
			// Chunk-box skip, at chunk starts only: the chunk's edges are all
			// provably beyond the bound, so none can win — jump the whole block.
			// This is the one that prunes a point sitting INSIDE a long
			// candidate's overall extent, which no nested suffix box can reject.
			if i%chunkEdges == 0 && pc.chunkBeyond(i/chunkEdges, lat, lon, bound) {
				i = (i/chunkEdges+1)*chunkEdges - 1 // -1: the loop's i++ lands on the next chunk
				continue
			}
			dLatM := 0.0
			if lat < pc.suffMinLat[i] {
				dLatM = (pc.suffMinLat[i] - lat) * MetresPerDeg
			} else if lat > pc.suffMaxLat[i] {
				dLatM = (lat - pc.suffMaxLat[i]) * MetresPerDeg
			}
			dLonM := 0.0
			if lon < pc.suffMinLon[i] {
				dLonM = (pc.suffMinLon[i] - lon) * MetresPerDeg * pc.cosMin
			} else if lon > pc.suffMaxLon[i] {
				dLonM = (lon - pc.suffMaxLon[i]) * MetresPerDeg * pc.cosMin
			}
			if dLatM*dLatM+dLonM*dLonM > bound*bound {
				break
			}
		}
		a0, a1 := pc.Verts[i], pc.Verts[i+1]
		d, t := ClosestPointOnSeg(a0.Lat, a0.Lon, a1.Lat, a1.Lon, lat, lon)
		a := pc.cumMeasure[i] + t*(pc.cumMeasure[i+1]-pc.cumMeasure[i])
		if a < minMeasure-measureEps || a > maxMeasure+measureEps {
			continue
		}
		if d < best {
			best = d
			measure = a
			dist = d
			plat = a0.Lat + t*(a1.Lat-a0.Lat)
			plon = a0.Lon + t*(a1.Lon-a0.Lon)
			ok = true
		}
	}
	return
}

// interpOnCanon returns the lat/lon on the candidate at cumulative measure
// position `at` (clamped to [0, total]).
func (pc *PreppedCandidate) interpOnCanon(at float64) (lat, lon float64) {
	if at <= 0 {
		return pc.Verts[0].Lat, pc.Verts[0].Lon
	}
	if at >= pc.Total {
		last := pc.Verts[len(pc.Verts)-1]
		return last.Lat, last.Lon
	}
	// Binary search for the segment containing `at`.
	i := sort.Search(len(pc.cumMeasure), func(k int) bool { return pc.cumMeasure[k] >= at }) - 1
	i = max(i, 0)
	i = min(i, len(pc.Verts)-2)
	segLen := pc.cumMeasure[i+1] - pc.cumMeasure[i]
	if segLen == 0 {
		return pc.Verts[i].Lat, pc.Verts[i].Lon
	}
	t := (at - pc.cumMeasure[i]) / segLen
	a0, a1 := pc.Verts[i], pc.Verts[i+1]
	return a0.Lat + t*(a1.Lat-a0.Lat), a0.Lon + t*(a1.Lon-a0.Lon)
}

// interpEle linearly interpolates the candidate's elevation at measure `at`,
// returning nil when either bracketing vertex has unknown (nil) elevation
// — byte fidelity over prettiness.
func (pc *PreppedCandidate) interpEle(at float64) *float64 {
	if at <= 0 {
		return cloneEle(pc.Verts[0].Ele)
	}
	if at >= pc.Total {
		return cloneEle(pc.Verts[len(pc.Verts)-1].Ele)
	}
	i := sort.Search(len(pc.cumMeasure), func(k int) bool { return pc.cumMeasure[k] >= at }) - 1
	i = max(i, 0)
	i = min(i, len(pc.Verts)-2)
	a0, a1 := pc.Verts[i], pc.Verts[i+1]
	if a0.Ele == nil || a1.Ele == nil {
		return nil
	}
	segLen := pc.cumMeasure[i+1] - pc.cumMeasure[i]
	if segLen == 0 {
		return cloneEle(a0.Ele)
	}
	t := (at - pc.cumMeasure[i]) / segLen
	e := *a0.Ele + t*(*a1.Ele-*a0.Ele)
	return &e
}

// nearestDist returns the nearest-absolute distance from (lat,lon) to the
// polyline — byte-identical to a full linear scan — plus the edge index that
// achieves it, to seed the next query's hint. It seeds from the hint edge and
// expands both ways, stopping in each direction once the whole remaining
// prefix/suffix box floors past the best found (a box distance never exceeds a
// contained edge's distance, so nothing skipped could win). Correct for any
// hint; O(local) when the hint is near, as monotone queries keep it.
func (hp *hintPoly) NearestDist(lat, lon float64, hint int) (float64, int) {
	n := len(hp.verts)
	if n == 0 {
		return math.Inf(1), 0
	}
	if n == 1 {
		return HaversineMetres(lat, lon, hp.verts[0].Lat, hp.verts[0].Lon), 0
	}
	nedges := n - 1
	if hint < 0 {
		hint = 0
	} else if hint >= nedges {
		hint = nedges - 1
	}
	edgeDist := func(j int) float64 {
		d, _ := ClosestPointOnSeg(hp.verts[j].Lat, hp.verts[j].Lon, hp.verts[j+1].Lat, hp.verts[j+1].Lon, lat, lon)
		return d
	}
	best, bestEdge := edgeDist(hint), hint
	for j := hint + 1; j < nedges; j++ {
		if boxDistM(lat, lon, hp.sufMinLat[j], hp.sufMaxLat[j], hp.sufMinLon[j], hp.sufMaxLon[j], hp.cosMin) > best {
			break
		}
		if d := edgeDist(j); d < best {
			best, bestEdge = d, j
		}
	}
	for j := hint - 1; j >= 0; j-- {
		if boxDistM(lat, lon, hp.preMinLat[j+1], hp.preMaxLat[j+1], hp.preMinLon[j+1], hp.preMaxLon[j+1], hp.cosMin) > best {
			break
		}
		if d := edgeDist(j); d < best {
			best, bestEdge = d, j
		}
	}
	return best, bestEdge
}

// chunkBeyond reports whether every edge in chunk c is provably farther than
// marginM from (lat, lon). Same lower-bound contract as [rectsBeyond]: cosMin
// never overstates the longitude metres, so a true result is always safe.
func (pc *PreppedCandidate) chunkBeyond(c int, lat, lon, marginM float64) bool {
	dLatM := 0.0
	if lat < pc.chMinLat[c] {
		dLatM = (pc.chMinLat[c] - lat) * MetresPerDeg
	} else if lat > pc.chMaxLat[c] {
		dLatM = (lat - pc.chMaxLat[c]) * MetresPerDeg
	}
	dLonM := 0.0
	if lon < pc.chMinLon[c] {
		dLonM = (pc.chMinLon[c] - lon) * MetresPerDeg * pc.cosMin
	} else if lon > pc.chMaxLon[c] {
		dLonM = (lon - pc.chMaxLon[c]) * MetresPerDeg * pc.cosMin
	}
	return dLatM*dLatM+dLonM*dLonM > marginM*marginM
}
