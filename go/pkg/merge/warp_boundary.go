package merge

// The interior-boundary warp: repointing a boundary requires warping the leg
// to it.
//
// The boundary-alignment pass can resolve a followed run's INTERIOR boundary
// onto a node up to EndpointWarpRadiusM away, not just its own computed
// point. Resolving the boundary record is only half of that: the leg's OWN
// geometry still peels away from canon at the ORIGINAL, unmoved point, so
// unless the leg is warped to match, its committed polyline never actually
// reaches the node its own seam now names — a follow-partition failure seen
// in practice (the distances agreed with AbsorbedM to 6 decimal places).
//
// This is the SAME taper the endpoint warp applies at a leg's own terminals
// ([WarpLegEndpoints]) — same zone formula, same accumulate-then-exact-assign
// mechanism, same "one method, always tapers" contract — with one structural
// difference: a terminal has geometry on only ONE side, so its taper only
// ever needs to decay in one direction. An interior boundary has a leg on
// BOTH sides and must decay into both.
//
// WarpLegEndpoints itself is separate and unchanged; this primitive shares
// its zone/smoothstep/accumulate math rather than the function.

import (
	"fmt"
	"math"
)

// WarpBoundaryTarget is one point INTERIOR to a leg (never index 0 or
// len-1 — those are terminals, the endpoint warp's job) that must
// land bit-exact on Target.
type WarpBoundaryTarget struct {
	Idx    int
	Target WarpTarget
}

// WarpLegAtBoundaries blends a leg's geometry toward every boundary in
// `boundaries` at once, using the SAME widened+capped zone rule and
// additive-accumulate mechanism WarpLegEndpoints uses for terminals (see
// that function's own comment for why accumulation — not scaling, not an
// error — is how overlapping zones combine). The two leg terminals
// (index 0 and len(pts)-1) are byte-for-byte PROTECTED: whatever an
// interior boundary's own zone computes for them is discarded, because by
// the time this function runs they have already been finalized by the
// endpoint warp and the follow (which leaves terminals verbatim) and must not
// move again here.
//
// A nil or empty `boundaries` returns pts unchanged, bitwise — this is the
// ordinary case, since most followed runs need no interior boundary at all.
//
// The input slice is never mutated.
func WarpLegAtBoundaries(pts []Point, boundaries []WarpBoundaryTarget) ([]Point, error) {
	out := make([]Point, len(pts))
	copy(out, pts)
	if len(boundaries) == 0 {
		return out, nil
	}
	if len(pts) < 2 {
		return nil, fmt.Errorf("warpLegAtBoundaries: leg has %d points, need at least 2", len(pts))
	}

	// Cumulative measure along the ORIGINAL leg, exactly as WarpLegEndpoints
	// computes it — shared by every boundary, so each one's taper reads only
	// the unwarped source, never another boundary's motion.
	cum := make([]float64, len(pts))
	for k := 1; k < len(pts); k++ {
		cum[k] = cum[k-1] + HaversineMetres(
			pts[k-1].Lat, pts[k-1].Lon, pts[k].Lat, pts[k].Lon)
	}
	legLen := cum[len(cum)-1]
	if math.IsNaN(legLen) || math.IsInf(legLen, 0) {
		return nil, fmt.Errorf(
			"warpLegAtBoundaries: leg measure length is %v — the source geometry is not finite", legLen)
	}
	last := len(pts) - 1

	dispLat := make([]float64, len(pts))
	dispLon := make([]float64, len(pts))
	var exact []exactAssignment

	for _, b := range boundaries {
		if b.Idx <= 0 || b.Idx >= last {
			return nil, fmt.Errorf(
				"warpLegAtBoundaries: boundary at index %d is not interior — a leg's own terminals (0 and %d) are warped by the terminal step, not by this function",
				b.Idx, last)
		}
		if !finiteLatLon(b.Target.Lat, b.Target.Lon) {
			return nil, fmt.Errorf(
				"warpLegAtBoundaries: target (%v,%v) at index %d is not finite", b.Target.Lat, b.Target.Lon, b.Idx)
		}
		if !finiteLatLon(pts[b.Idx].Lat, pts[b.Idx].Lon) {
			return nil, fmt.Errorf(
				"warpLegAtBoundaries: source point (%v,%v) at index %d is not finite", pts[b.Idx].Lat, pts[b.Idx].Lon, b.Idx)
		}

		vLat := b.Target.Lat - pts[b.Idx].Lat
		vLon := b.Target.Lon - pts[b.Idx].Lon
		if vLat == 0 && vLon == 0 {
			// Already exactly coincident — the boundary's own resolved node IS
			// where the leg already is (an exact match, or one of
			// ApplyLegBoundaryWarp's own self-anchor targets for a
			// non-Absorbed seam). There is nothing to BLEND, but this index
			// still needs to be PINNED: without registering it as an exact
			// assignment too, a DIFFERENT, genuinely-displaced boundary whose
			// zone happens to reach this same index can move it anyway,
			// failing the bit-exactness post-condition below for a boundary
			// that never asked to move at all (seen in practice: a self-anchor
			// seam a few points from a real absorb). Every OTHER boundary gets this same protection for
			// free via its own s==0 case in the walk below; this is that same
			// protection for the one case that skips the walk entirely.
			exact = append(exact, exactAssignment{b.Idx, b.Target.Lat, b.Target.Lon})
			continue
		}

		displacement := HaversineMetres(pts[b.Idx].Lat, pts[b.Idx].Lon, b.Target.Lat, b.Target.Lon)
		// No flat-fraction floor term: zoneFor's formula has none (it
		// measured as dead weight; endpointTaperFraction's doc has the
		// evidence), and this function shares the SAME formula.
		ideal := widenedZoneFactor * displacement
		center := cum[b.Idx]

		// TWO INDEPENDENT CAPS, NOT ONE — this is where an interior boundary
		// genuinely differs from a terminal, and getting it wrong folds the
		// leg right at its own protected end (found by
		// TestWarpLegAtBoundaries_NeverInvertsAArc before this fix: a
		// single legLen-wide cap left the zone reaching a PROTECTED terminal
		// with a large but non-zero weight, and the terminal's own hard
		// override then created exactly the kink the cap exists to prevent —
		// the same discontinuity zoneFor's cap closes at a leg's own far end).
		//
		// zoneLeft is capped at `center` — the measure distance back to index 0 —
		// so smoothstep(1 - center/zoneLeft) is EXACTLY 0 at index 0 whenever
		// the cap binds, mirroring zoneFor's `min(legLen, ...)` proof.
		// zoneRight mirrors it against the far end. Both read the SAME ideal
		// width; only the ceiling differs.
		zoneLeft := math.Min(center, ideal)
		zoneRight := math.Min(legLen-center, ideal)

		// LEFT: walk from the boundary down to index 0, s increasing as the
		// distance from the boundary grows. idx==b.Idx itself is s==0 here —
		// exact-assigned, not computed, for the same reason WarpLegEndpoints'
		// own s==0 branch is: `pts[i] + (target-pts[i])` is not guaranteed to
		// round back to target bit-exactly.
		for idx := b.Idx; idx >= 0; idx-- {
			s := center - cum[idx]
			if s > zoneLeft {
				break
			}
			if s == 0 {
				exact = append(exact, exactAssignment{idx, b.Target.Lat, b.Target.Lon})
				continue
			}
			w := smoothstep(1 - s/zoneLeft)
			dispLat[idx] += vLat * w
			dispLon[idx] += vLon * w
		}
		// RIGHT: walk from just past the boundary up to the far end. idx==b.Idx
		// was already handled by the LEFT walk above, so this starts at b.Idx+1.
		for idx := b.Idx + 1; idx <= last; idx++ {
			s := cum[idx] - center
			if s > zoneRight {
				break
			}
			// The MIRROR of the LEFT walk's own s==0 branch, and it was
			// missing. s==0 past the boundary means this point repeats the
			// boundary's coordinate exactly — an ordinary stationary GPS
			// sample — so it sits where the boundary sits and must move
			// where the boundary moves, assigned rather than computed.
			//
			// Without it this divides by zero. zoneRight is min(legLen-center,
			// ideal), so a leg whose every point is ONE repeated coordinate
			// has legLen == center == 0, zoneRight == 0, s == 0, and
			// smoothstep(1 - 0/0) is NaN — which then reaches the caller's
			// storage, where it fails far away from the cause. Seen on a real
			// track with 1,248 consecutive duplicate points, which produce
			// exactly that piece.
			if s == 0 {
				exact = append(exact, exactAssignment{idx, b.Target.Lat, b.Target.Lon})
				continue
			}
			w := smoothstep(1 - s/zoneRight)
			dispLat[idx] += vLat * w
			dispLon[idx] += vLon * w
		}
	}

	// The additive sum, applied once — see WarpLegEndpoints' own comment for
	// why overlapping zones must SUM rather than one boundary's write
	// clobbering another's.
	for i := range out {
		out[i].Lat = pts[i].Lat + dispLat[i]
		out[i].Lon = pts[i].Lon + dispLon[i]
	}
	// Exact assignments last, overriding the sum — same "assign, don't
	// compute" requirement WarpLegEndpoints' own exact list exists for.
	for _, e := range exact {
		out[e.idx].Lat = e.lat
		out[e.idx].Lon = e.lon
	}
	// PROTECT THE TERMINALS, UNCONDITIONALLY, LAST OF ALL — overriding
	// anything an interior boundary's own zone computed for them. By the time
	// this function runs, index 0 and `last` have already been finalized by
	// the endpoint warp (warped onto a node, or left verbatim) and carried
	// through the follow unchanged; a boundary whose zone happens to reach that far must
	// not perturb them. This is not a re-derivation of identity — it is
	// restoring exactly the value this function was handed, which needs no
	// coordinate comparison to do.
	out[0] = pts[0]
	out[last] = pts[last]

	if warpBoundaryZoneObserverForTests != nil {
		// DIAGNOSTIC: how much of a leg does the taper actually touch?
		// touched = indices with a genuine nonzero displacement, excluding
		// both terminals (never move — reset above regardless) and each
		// boundary's OWN index (exempted from a byte-identity check as a
		// seam). The residual (touched, non-seam) is exactly the population an
		// under-sized seam exemption is blind to.
		touched, touchedNonSeam := 0, 0
		seamIdx := make(map[int]bool, len(boundaries))
		for _, b := range boundaries {
			seamIdx[b.Idx] = true
		}
		for i := 1; i < last; i++ {
			if dispLat[i] == 0 && dispLon[i] == 0 {
				continue
			}
			touched++
			if !seamIdx[i] {
				touchedNonSeam++
			}
		}
		warpBoundaryZoneObserverForTests(CurrentLegForTests, len(pts), touched, touchedNonSeam, len(boundaries), legLen)
	}

	// POST-CONDITION, on top of the construction argument above: every
	// boundary must land bit-exact on its own target, because node adoption
	// downstream compares coordinates with `==`, not a tolerance — the same
	// invariant CheckTerminalsExact asserts for the endpoint warp's terminals.
	// Proven able to fail, not merely observed passing, by
	// TestWarpLegAtBoundaries_PostConditionCatchesAViolation.
	for _, b := range boundaries {
		if math.Float64bits(out[b.Idx].Lat) == math.Float64bits(b.Target.Lat) &&
			math.Float64bits(out[b.Idx].Lon) == math.Float64bits(b.Target.Lon) {
			continue
		}
		// ⚠ NAME THE CAUSE, DO NOT JUST REPORT THE SYMPTOM. "not bit-exact"
		// reads as a rounding problem and sends the reader to the taper
		// arithmetic, where it once misled an investigation. The overwhelmingly likelier cause is another boundary
		// standing at ZERO ARC DISTANCE from this one: the s==0 branches in
		// both walks above exact-assign by DISTANCE, not by index, so a
		// boundary whose neighbour is arc-coincident writes its own target over
		// that neighbour's index, and `exact` is applied in order so the later
		// boundary wins. Two coincident leg vertices adopting two DIFFERENT
		// nodes is the shape that produces it (a zero-width run).
		//
		// This is a REPORT, not a repair. Landing each boundary on its own
		// target would commit the leg as a spike between two nodes tens of
		// metres apart, which is silently wrong geometry in place of a loud
		// refusal — strictly worse. The refusal is correct; what to do about
		// two arc-coincident boundaries wanting different nodes is an open
		// question.
		culprit := ""
		for _, o := range boundaries {
			if o.Idx == b.Idx {
				continue
			}
			if math.Float64bits(out[b.Idx].Lat) == math.Float64bits(o.Target.Lat) &&
				math.Float64bits(out[b.Idx].Lon) == math.Float64bits(o.Target.Lon) {
				culprit = fmt.Sprintf(
					" — it is sitting on the target of the boundary at index %d (node %d) instead, "+
						"so the two boundaries are at ZERO arc distance and the s==0 exact assignment overwrote this one",
					o.Idx, o.Target.NodeID)
				break
			}
		}
		return nil, fmt.Errorf(
			"warpLegAtBoundaries: boundary at index %d (%v,%v) is not bit-exact on its target (%v,%v)%s — the join invariant is violated",
			b.Idx, out[b.Idx].Lat, out[b.Idx].Lon, b.Target.Lat, b.Target.Lon, culprit)
	}
	return out, nil
}

// There is deliberately no collapsing of short gaps between adjacent
// boundaries (too short to absorb a displacement). Folds are accepted rather
// than prevented: a boundary very close to another boundary or a terminal
// can fold, and that is an expected outcome, not a defect.

// ApplyLegBoundaryWarp is the leg-warp half of repointing a boundary,
// composed for the caller that turns aligned seams into committed leg pieces
// (it runs between legSeamsFromAlign and the commit). Repointing a seam
// RECORD onto a node up to EndpointWarpRadiusM away is only half of the job —
// the leg's own committed geometry has to actually reach that node, or the
// caller writes a piece whose own declared boundary sits nowhere the followed
// points pass. Observed, not theorised: without this a real case aborts with
// "committed leg is not a valid follow partition ... NEAREST: any committed
// vertex 4.118796 m ... any ABSORBED seam 8.064847 m" — the distances this
// function exists to close.
//
// Every [LegSeam] becomes a [WarpBoundaryTarget]: an Absorbed one targets its
// own Node's real, recorded coordinates (NodeLat/NodeLon — never
// recomputed); every other HasNode seam targets its OWN current point, a
// zero-displacement target that still ANCHORS its index (WarpLegAtBoundaries'
// own exact-assignment protection for a zero-displacement target, see that
// function's doc) while asking it to move nowhere — that function's own
// "already exactly coincident" branch is exactly this no-op. Seams with
// !HasNode (dropped stub declines) are never in the slice
// legSeamsFromAlign returns, so there is nothing to filter here.
//
// No gap-collapse runs (see the note above): the points are handed to
// WarpLegAtBoundaries as they are, and a boundary very close to another
// boundary or a terminal can fold. That is accepted, not prevented.
//
// Returns the warped points and the same seams, Idx UNCHANGED (nothing
// remaps indices any more) — every other field (Node, Adopted, Absorbed,
// AbsorbedM, NodeLat/NodeLon) was already untouched.
func ApplyLegBoundaryWarp(pts []Point, seams []LegSeam) ([]Point, []LegSeam, error) {
	if len(seams) == 0 {
		out := make([]Point, len(pts))
		copy(out, pts)
		return out, seams, nil
	}
	targets := make([]WarpBoundaryTarget, len(seams))
	for i, s := range seams {
		lat, lon := s.NodeLat, s.NodeLon
		if !s.Absorbed {
			lat, lon = pts[s.Idx].Lat, pts[s.Idx].Lon
		}
		targets[i] = WarpBoundaryTarget{Idx: s.Idx, Target: WarpTarget{Lat: lat, Lon: lon, NodeID: s.Node}}
	}

	warpedPts, err := WarpLegAtBoundaries(pts, targets)
	if err != nil {
		return nil, nil, fmt.Errorf("applyLegBoundaryWarp: %w", err)
	}
	return warpedPts, seams, nil
}
