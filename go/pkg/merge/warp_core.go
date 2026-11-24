package merge

import (
	"fmt"
	"math"
)

// EndpointWarpRadiusM is how far from a leg terminal we look for an existing
// node to land on.
//
// Chosen on measured evidence: at 10 m the warp fired on none of the nine
// main-line stage joins of a multi-day test corpus — they sit 12.8 to 57.8 m
// apart. The pair distribution has no natural break (34 pairs at 10 m, 45 at
// 15, 58 at 25, 86 at 50), so the number is a judgement, not something the
// data settles.
//
// 15 m is deliberately short of the 25 m that would catch most such joins.
// The hazard a wider radius buys is real and geometric: two plans that begin on
// opposite banks of a river at nominally the same place are metres apart in a
// straight line, and warping one onto the other teleports it across the water.
// Until the warp can refuse a translation that crosses a waterway, the radius
// IS the guard.
//
// Do not infer a heading test can stand in for that guard. One was tried and it
// mislabels an overlap join as lateral: in one measured case a stage overshoots
// ~12.8 m PAST the next stage's start along the same line, so the approach
// angle reads 170° while the plans are in fact 7.3 m apart and plainly
// continuous.
//
// A band, like the followBands values: revisit it with evidence, not by
// silent retuning.
const EndpointWarpRadiusM = 15.0

// widenedZoneFactor sets zoneFor's displacement-driven zone term: the taper
// zone is 5 times the terminal's displacement (capped at the leg length).
// It applies to every warp; there is no separate flat-zone entry point.
//
// WHY 1.5 WOULD BE A BOUNDARY, NOT A WORKING VALUE. The taper's
// forward-progress factor at the tightest point is
// 1 - smoothstepMaxSlope*d/zone. At zone = 1.5d (smoothstepMaxSlope's own
// value) that is EXACTLY zero — the precise threshold where inversion just
// barely does not occur, and right at it the line stops advancing and the
// points visibly bunch into a kink. At zone = 5d it is 1 - 1.5/5 = 0.7: the
// tightest part of the taper still compresses the trail to 70% of its
// spacing, not 0%. 5x buys smoothness; it does not raise the floor: a leg
// shorter than 1.5x its own displacement still has nowhere to put it,
// however wide zoneFor is allowed to ask for.
const widenedZoneFactor = 5.0

// warpTarget is a resolved destination for one leg terminal — the coordinates
// of the node it should land on, exactly.
//
// Coordinates, not just a NodeID: the warp moves the terminal onto the
// node's position; whether the commit reuses that node is decided by the
// caller from NodeID below.
type warpTarget struct {
	Lat float64
	Lon float64

	// NodeID is the node these coordinates came FROM.
	//
	// Without it, the terminal is moved onto the node bitwise but the caller
	// has nothing to adopt, so it mints a fresh node 0.000 m from the one this
	// step chose. Measured on a three-track fixture: every follower start
	// doubled, and a follower that never diverges came out as a SEPARATE
	// CONNECTED COMPONENT — geometry lying exactly on canon, joined to it
	// nowhere.
	//
	// The identity exists only here. Nothing downstream can recover it: two nodes
	// at one coordinate are indistinguishable by then, which is why this is
	// carried rather than re-derived (the same shape as SplitCause.Parent).
	NodeID NodeID
}

// WarpReport records what the warp did to a leg. It exists so TESTS can observe
// the outcome directly instead of inferring it from geometry — production
// discards it deliberately.
//
// Do not add a skip count here without a consumer that actually needs it.
// The thing that must stay true is the weaker and sufficient one — nothing
// downstream may infer coincidence from "the warp ran"; node adoption tests
// coordinates.
type WarpReport struct {
	// StartWarped / EndWarped report whether that end had a target at all —
	// true whenever StartNode/EndNode below is non-nil. There is only one way
	// to move a terminal, so this does not distinguish an outcome; it is kept
	// because callers and tests read it as "was this end touched at all"
	// without reaching into StartNode's pointer.
	StartWarped bool
	EndWarped   bool

	// StartNode / EndNode are the nodes the terminals were warped ONTO, and they
	// are what the commit joins. nil means "no node in range" — the genuine
	// case, where the terminal stays a lat-lon anchor and the caller mints a
	// new node.
	//
	// ⚠ A NON-NIL VALUE HERE IS A JOIN INSTRUCTION, NOT A HINT. A caller must
	// not fall back to minting when one of these is set: if the id cannot be
	// resolved, it should fail rather than quietly mint a duplicate, because a
	// duplicate is precisely the defect this carries.
	//
	// The other direction holds too: whenever `start`/`end` passed into
	// warpLegEndpoints is non-nil, this is set, unconditionally. See
	// checkTerminalsExact for the assertion that makes this true by
	// construction, not merely by this comment's say-so.
	StartNode *NodeID
	EndNode   *NodeID
}

// warpLegEndpoints returns a new point slice with each supplied terminal moved
// onto its target and the translation blended inward. A nil target leaves
// that end untouched, bitwise. The input slice is never mutated.
//
// ONE RULE, EVERYWHERE: this is the only terminal warp. The taper zone is
// sized by the displacement itself rather than a flat share of the leg — see
// zoneFor's doc for the formula and smoothstepMaxSlope's / widenedZoneFactor's
// docs for the mechanics.
// (The Ele-aliasing guarantee covers the point FIELDS. Ele is a *float64 and
// is copied by pointer, so output and input alias the same elevation values.
// Nothing writes through an Ele pointer today, but the claim is not
// transitive and should not be read as if it were.)
func warpLegEndpoints(pts []Point, start, end *warpTarget) ([]Point, WarpReport, error) {
	var rep WarpReport
	if len(pts) < 2 {
		return nil, rep, fmt.Errorf("WarpLegEndpoints: leg has %d points, need at least 2", len(pts))
	}

	out := make([]Point, len(pts))
	copy(out, pts)
	if start == nil && end == nil {
		return out, rep, nil
	}

	// Cumulative measure along the ORIGINAL leg, computed ONCE and shared by both
	// ends.
	//
	// THIS is what makes the two ends independent of each other and of the
	// order they are applied in — not the fact that the blend reads src rather
	// than out. Both ends measure measure distance against the same unwarped
	// array, so neither can see the other's motion.
	cum := make([]float64, len(pts))
	for k := 1; k < len(pts); k++ {
		cum[k] = cum[k-1] + HaversineMetres(
			pts[k-1].Lat, pts[k-1].Lon, pts[k].Lat, pts[k].Lon)
	}
	legLen := cum[len(cum)-1]
	if math.IsNaN(legLen) || math.IsInf(legLen, 0) {
		return nil, rep, fmt.Errorf(
			"WarpLegEndpoints: leg measure length is %v — the source geometry is not finite", legLen)
	}

	// zoneFor computes ONE end's own taper zone: zone =
	// min(legLen, widenedZoneFactor*displacement). There is no floor term (a
	// fraction of legLen): measured, one inflated the zone on every long leg
	// regardless of how small the actual displacement was (a 1 m correction
	// tapering over 1,350 m of trail on a 13.5 km leg), and
	// widenedZoneFactor*displacement alone already satisfies
	// zone >= 1.5*displacement with margin. Each end's zone depends on ITS OWN
	// target's displacement, so the two ends can (and generally will) get
	// DIFFERENT zones — they are not one shared scalar.
	//
	// CAPPED AT legLen — min(legLen, ...): a zone wider than the leg itself
	// would leave the taper still non-zero at the FAR end's own anchor slot,
	// a discontinuity that reasoning about the near end alone cannot see.
	// Capping guarantees the far end's weight is exactly zero (smoothstep(0) =
	// 0) whether or not the cap actually binds for a given call — see
	// TestWarp_FarEndNeverMovesWhenZoneIsCapped.
	zoneFor := func(target *warpTarget, ti int) float64 {
		if target == nil {
			// The value is provably never read: applyWarpAtEnd's own first
			// line returns immediately for a nil target, before its `zone`
			// parameter is used anywhere. Zero, not some derived width, says
			// that plainly rather than computing a number nothing consumes.
			return 0
		}
		// A non-finite target is reported by applyWarpAtEnd itself (it checks
		// again before using it); HaversineMetres of a non-finite pair is NaN,
		// and NaN propagates harmlessly through min() into a zone no caller
		// ever reads, because applyWarpAtEnd errors before reaching the loop
		// that would.
		d := HaversineMetres(pts[ti].Lat, pts[ti].Lon, target.Lat, target.Lon)
		return math.Min(legLen, widenedZoneFactor*d)
	}
	zoneStart := zoneFor(start, 0)
	zoneEnd := zoneFor(end, len(pts)-1)

	// OVERLAP. A fixed-fraction zone (< 0.5 of legLen) could never overlap
	// the other end's; a displacement-sized one can, on a leg too short to
	// hold both, so nothing structural stops zoneStart and zoneEnd from
	// covering the same interior points.
	//
	// Two alternatives were measured and rejected: erroring aborted a whole
	// file's import on a genuine 5.8 cm leg; proportionally SCALING the two
	// zones down avoided the abort but solved the wrong problem — it treated
	// the leg's length as a resource the two ends must ration, when the
	// actual defect was that writing final positions lets a second call
	// CLOBBER the first's contribution wherever they overlap.
	//
	// THE FIX: accumulate. Each end computes a DISPLACEMENT FIELD from the
	// unwarped source (dispLat/dispLon below) instead of writing `out`
	// directly, and the two fields are SUMMED once, after both ends have
	// run. Both fields read only pts/cum — never each other's motion, never
	// `out` — so the two ends stay independent; overlapping contributions ADD
	// instead of one overwriting the other. zoneStart/zoneEnd need no
	// rationing: each keeps whatever zoneFor gave it, capped at legLen.
	//
	// WHY THIS IS SAFE AT THE ENDPOINTS BY CONSTRUCTION, NOT BY ASSERTION.
	// At i=0 (the start terminal): wStart(0) = 1 (s=0, full weight — see
	// applyWarpAtEnd). wEnd(0) = 0, PROVABLY: end's own s at index 0 is
	// legLen-cum[0] = legLen, and end's zone is capped at legLen, so
	// legLen <= zoneEnd only at exact equality, where
	// smoothstep(legLen/zoneEnd) = smoothstep(1) = 1, giving weight exactly
	// 0 — and for zoneEnd < legLen (the common case) index 0 sits outside
	// end's zone entirely (s > zone), never touched at all. So
	// out[0] = pts[0] + vStart*1 + vEnd*0 = target, exactly, REGARDLESS of
	// how much the two zones overlap elsewhere. The far end mirrors this at
	// i=len-1. See TestWarp_TerminalExactRegardlessOfOverlap.
	dispLat := make([]float64, len(pts))
	dispLon := make([]float64, len(pts))
	var exact []exactAssignment

	warped, err := applyWarpAtEnd(dispLat, dispLon, &exact, pts, cum, legLen, zoneStart, start, false)
	if err != nil {
		return nil, rep, err
	}
	rep.StartWarped = warped

	warped, err = applyWarpAtEnd(dispLat, dispLon, &exact, pts, cum, legLen, zoneEnd, end, true)
	if err != nil {
		return nil, rep, err
	}
	rep.EndWarped = warped

	// The additive sum, applied once. Points neither end's zone reached get
	// dispLat[i]==dispLon[i]==0, and x+0.0 is bit-exact — the "outside both
	// zones, bitwise untouched" property (TestWarpLeg_OutsideZoneUntouched)
	// survives unchanged.
	for i := range out {
		out[i].Lat = pts[i].Lat + dispLat[i]
		out[i].Lon = pts[i].Lon + dispLon[i]
	}
	// exactAssignments — a snap's terminal, or any point coincident with a
	// terminal at measure distance 0 — are applied LAST, as direct overwrites
	// rather than arithmetic, and deliberately AFTER the additive sum above.
	// This is the same "assign, don't compute" requirement
	// TestWarpLeg_TerminalAssignedBitwise exists to hold: `pts[i] +
	// (target-pts[i])` is NOT guaranteed to round back to `target` bit-exactly
	// (Sterbenz's lemma only covers same-sign, factor-of-two-apart operands;
	// see that test's doc for the prime-meridian/equator counterexamples), so
	// these points bypass the accumulator entirely rather than relying on
	// its arithmetic to land exactly.
	for _, e := range exact {
		out[e.idx].Lat = e.lat
		out[e.idx].Lon = e.lon
	}

	if err := checkTerminalsExact(out, start, end); err != nil {
		return nil, rep, err
	}
	return out, rep, nil
}

// exactAssignment is one point that must land BIT-EXACTLY on a coordinate —
// a snapped terminal, or a source point coincident with a terminal at measure
// distance 0 — rather than through warpLegEndpoints' additive accumulator,
// which computes a sum and is not guaranteed to round to the target exactly.
// See the "exactAssignments" comment at its one call site.
type exactAssignment struct {
	idx      int
	lat, lon float64
}

// checkTerminalsExact is a POST-CONDITION on top of the construction argument
// in warpLegEndpoints' own comment: an invariant being provable is not the
// same as it being preserved by whoever edits this next. Whichever end got a
// target must have its terminal sitting bit-exact on it, because node
// adoption downstream compares coordinates with `==`, not a tolerance; a
// near-miss here would silently mint a duplicate node instead of adopting
// the one this step chose.
//
// Unconditional: every non-nil target taper-joins, so a non-nil start/end
// that ISN'T bit-exact in `out` is a bug.
//
// Tested directly (not only indirectly through bit-exactness fixtures) by
// TestCheckTerminalsExact_CatchesAViolation, which hands this function a
// deliberately wrong `out` to prove the failure branch actually fires — a
// guard that can only be observed passing is not proven to be able to fail.
func checkTerminalsExact(out []Point, start, end *warpTarget) error {
	if start != nil {
		if math.Float64bits(out[0].Lat) != math.Float64bits(start.Lat) ||
			math.Float64bits(out[0].Lon) != math.Float64bits(start.Lon) {
			return fmt.Errorf(
				"CheckTerminalsExact: start terminal (%v,%v) is not bit-exact on its target (%v,%v) — the join invariant is violated",
				out[0].Lat, out[0].Lon, start.Lat, start.Lon)
		}
	}
	if end != nil {
		last := len(out) - 1
		if math.Float64bits(out[last].Lat) != math.Float64bits(end.Lat) ||
			math.Float64bits(out[last].Lon) != math.Float64bits(end.Lon) {
			return fmt.Errorf(
				"CheckTerminalsExact: end terminal (%v,%v) is not bit-exact on its target (%v,%v) — the join invariant is violated",
				out[last].Lat, out[last].Lon, end.Lat, end.Lon)
		}
	}
	return nil
}

// applyWarpAtEnd computes one end's contribution into the ACCUMULATORS
// dispLat/dispLon (or, for a point that must land bit-exact, appends to
// *exact) — it never writes `out` directly, so a second call for the other
// end can never clobber this one's work; see warpLegEndpoints' "THE FIX:
// accumulate" comment for why that matters. All geometry is read from the
// unwarped src/cum: each end only ever knows about its own target and the
// untouched source.
//
// ONE OUTCOME: given a target, it tapers; there is no snap or decline
// outcome. A leg whose own measure length can't absorb its own displacement
// is not pre-processed, so this function can be handed geometry that folds —
// and does nothing to prevent that, deliberately: a fold there is accepted.
func applyWarpAtEnd(
	dispLat, dispLon []float64,
	exact *[]exactAssignment,
	src []Point,
	cum []float64,
	legLen, zone float64,
	target *warpTarget,
	atEnd bool,
) (warped bool, err error) {
	if target == nil {
		return false, nil
	}
	if !finiteLatLon(target.Lat, target.Lon) {
		return false, fmt.Errorf(
			"WarpLegEndpoints: target (%v, %v) is not finite", target.Lat, target.Lon)
	}

	ti := 0
	if atEnd {
		ti = len(src) - 1
	}
	// The SOURCE terminal needs checking too, not just the target. A NaN here
	// makes vLat NaN, which is not == 0, and then every subsequent comparison
	// against NaN is false — so a non-total guard would wave it through and the
	// whole taper zone would come out NaN, surfacing much later with an error
	// naming the wrong stage.
	//
	// Stated plainly because it would otherwise look better-tested than it is:
	// via warpLegEndpoints this check is REDUNDANT. A non-finite terminal also
	// poisons the cumulative measure sum, so the caller's measure-length check catches
	// the same inputs first, and removing either one alone leaves the tests
	// green. It is kept as a total guard on this helper in its own right — the
	// caller's check is about the leg, this one is about the end being warped —
	// but no test pins it individually, and none can while the caller is total.
	if !finiteLatLon(src[ti].Lat, src[ti].Lon) {
		return false, fmt.Errorf(
			"WarpLegEndpoints: source terminal (%v, %v) is not finite", src[ti].Lat, src[ti].Lon)
	}

	vLat := target.Lat - src[ti].Lat
	vLon := target.Lon - src[ti].Lon
	if vLat == 0 && vLon == 0 {
		// Already exactly coincident. Nothing to blend, and nothing may move —
		// a zero-magnitude translation must not perturb the interior. This is
		// also the branch a leg whose terminal is already on its node takes on
		// re-import. Still joins: the caller sets the node off `target != nil`.
		return false, nil
	}

	// ONE OUTCOME — no absorb gate here. Whatever zone this end was given, it
	// tapers into it, folding if the leg is too short (see the doc comment).
	for k := range src {
		idx := k
		if atEnd {
			idx = len(src) - 1 - k
		}
		s := cum[idx]
		if atEnd {
			s = legLen - cum[idx]
		}
		if s > zone {
			// Points are visited in increasing measure distance from this terminal,
			// so the first one past the zone ends the run.
			break
		}
		if s == 0 {
			// ASSIGNED, never computed — and keyed on the MEASURE DISTANCE being
			// zero, not on the index being the terminal's.
			//
			// A trace that logged while stationary has several points at the
			// terminal's exact measure position; those get w == 1 too, and computing
			// src + v*1.0 for them is not guaranteed to round to target (up to
			// 29 ulp out near the equator or the prime meridian — see
			// TestWarpLeg_TerminalAssignedBitwise). In the source they were bitwise identical to the
			// terminal; a computed path would silently pull them apart. Assigning
			// is also the exact answer: a point coincident with the terminal,
			// translated by the full vector, lands exactly on the target.
			//
			// This goes on the exact list, NOT
			// into the accumulator — a later exact-list application always wins
			// over the additive sum (see warpLegEndpoints' apply order), so a
			// point that is s==0 for THIS end lands exactly on THIS end's target
			// even if the other end's accumulator also touched it.
			*exact = append(*exact, exactAssignment{idx, target.Lat, target.Lon})
			continue
		}
		w := smoothstep(1 - s/zone)
		dispLat[idx] += vLat * w
		dispLon[idx] += vLon * w
	}
	return true, nil
}

func finiteLatLon(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsInf(lat, 0) &&
		!math.IsNaN(lon) && !math.IsInf(lon, 0)
}

// smoothstep is the classic 3t²-2t³ ease on [0,1]. Its derivative is zero at
// both ends, which is the point: a linear ramp would leave a residual kink
// where the taper stops (gradient d/zone, about 4.4 degrees at the measured
// medians), whereas this meets the untouched geometry with matching slope.
//
// A shape, not a threshold — there is nothing here to tune. But note it is not
// free: its max slope of 1.5 is what forces the zone-adequacy factor in
// smoothstepMaxSlope, where a linear ramp would have needed none.
func smoothstep(t float64) float64 {
	return t * t * (3 - 2*t)
}
