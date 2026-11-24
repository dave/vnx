package merge

// The follow: matching a leg against existing canon arcs. This file holds the
// follow's default bands, the per-leg canon candidate lookup, and the entry
// point that runs the corridor matcher and its assembler. It has no clock, no
// globals and no package state, and reads the canon only through the Canon
// interface. Every run carries its own evidence (bound arc, measure interval,
// direction), so a wrong match is diagnosable from the value alone.
//
// Matching is HORIZONTAL ONLY. Elevation never enters any distance, any
// bearing or any gate: elevation deltas between two recordings of the same
// trail are GPS error. Canon's elevation is carried onto followed stretches
// at assembly time, but it plays no part in deciding what follows what.

import (
	"context"
	"fmt"
	"sort"
)

// ⚠ There is no bearing agreement band. Whether the leg and a candidate run
// the same way is answered by measure progress, and nothing in this package
// computes a bearing. Re-introducing an angle would be a new band, not a
// retune.

// defaultFollowBands returns the default follow bands: Enter 8 m, Stay 15 m.
func defaultFollowBands() followBands {
	return followBands{
		EnterM: 8,
		StayM:  15,
	}
}

// The matcher's canon candidates are CanonArc values, exactly what the
// canon's rect reader returns: an arc ID plus the full ordered vertex list
// (start node, interior vertices, end node), with nil Ele for unknown
// elevation. Candidate vertices deliberately share the point type of the
// incoming leg, so the reader's output passes straight in with no glue.

// ---- candidate preparation --------------------------------------------

// ⚠ There is no gap-bridging pass. Merging two ON runs bound to the same
// candidate and direction across a short OFF gap was only ever needed to
// repair breaks made by a bearing gate; with no bearing gate it measured zero
// merges on several test areas. Re-introducing one would need a population of
// real cases to justify it.

// ---- geometry ---------------------------------------------------------
//
// Self-contained equirectangular projection. Kept local so the
// matcher stays a pure function with no reach beyond the exported
// great-circle helper.

// ⚠ NOTHING IN THIS PACKAGE COMPUTES AN ANGLE. Direction agreement is MEASURE
// PROGRESS: whether the source's next positions project FURTHER ALONG the
// candidate or FURTHER BACK, which is a comparison of two measure-length
// numbers. There is no heading, no window over which a heading is
// estimated, and no tolerance in degrees.

// ---- assembly primitives ---------------------------------------------

// noNodeLookup is the explicit "this caller has no node access" value.
//
// ⚠ NOT nil. A nil func value here would be indistinguishable from a
// caller that forgot to pass one, so [assembleFollowedLegRunsCorridor]
// and the any-node snap both ERROR on a literal nil rather than
// silently skip the any-node branch. A compiler won't catch a nil argument,
// but the very first call does.
func noNodeLookup(lat, lon, radiusM float64) (float64, float64, NodeID, bool, error) {
	return 0, 0, 0, false, nil
}

// canonCache memoises decoded canon arcs by id while one leg's candidate
// windows are scanned. Stored canon arcs are immutable, so a cached decode can
// never go stale within the leg. Decoding each candidate once per leg (rather
// than once per window it appears in) is what keeps GPS-outlier legs, whose
// area-sized arcs fall inside every window, off an O(windows) polyline-decode
// blowup. It is deliberately per-leg, not per-batch: canonCandidatesForLeg owns
// and discards it each call, keeping its signature a pure (leg → candidates)
// function, and the residual cross-leg re-decode is bounded and cheap.
type canonCache struct {
	byID map[ArcID]CanonArc
}

func newCanonCache() *canonCache {
	return &canonCache{byID: make(map[ArcID]CanonArc)}
}

// canonWindowPts is the number of consecutive leg points per candidate-lookup
// window. It is a COUNT, not a geometric tolerance (the windowing introduces
// none): a small window keeps each window's bbox tight, so a GPS-outlier point
// that jumps kilometres pulls in only nearby canon, never the whole network; a
// single whole-leg bbox degenerates to a network scan on those legs. 32 trades
// per-window query count against bbox tightness.
const canonWindowPts = 32

// followLegAgainstCanon runs the follow for one leg. It looks up the canon
// candidates near the leg, partitions the leg into OFF (verbatim) / ON
// (canon-following) runs, and assembles the rewritten geometry that will be
// committed.
//
// Canon is every arc visible through the Canon at this moment: earlier merges
// AND geometry written earlier in the same merge (earlier legs of the same
// batch). The caller's Canon must therefore see its own uncommitted writes.
//
// Every error propagates: a failed candidate read, an impossible partition, or
// a violated assembly post-condition fails the leg, and the caller discards
// the merge's writes. There is NO fallback to committing the leg verbatim: a
// follow the code cannot justify is a bug to surface, not to paper over. On an
// empty network the partition is all-OFF and the assembly returns the leg
// verbatim, so this is called unconditionally, with no empty-canon special case.
//
// ── THE RUNS IN THE RESULT ──────────────────────────────────────────────────
//
// The EFFECTIVE runs: the assembler's, not the matcher's. Each ON run names one
// canon arc (CanonID), the direction it was walked (Reversed) and the measure
// interval on that arc the assembled geometry actually rides
// ([MeasureStart, MeasureEnd]). The align step splits canon at those measure
// boundaries, so they are the positions where a shared node must be minted.
//
// ⚠ THEY MUST COME FROM THE ASSEMBLER, NOT FROM [corridorFollowRuns]. The
// assembler can move a boundary measure (node snapping), and the moved
// interval is the one the committed points were built from. Splitting canon
// at the matcher's measure instead could put the shared node away from where
// the leg actually joins.
func followLegAgainstCanon(
	ctx context.Context,
	canon Canon,
	pts []Point,
) (assembledLeg, error) {
	bands := defaultFollowBands()
	// ⚠ CorridorRadiusM, NOT bands.StayM. The candidate set has to be a
	// superset of what the MATCHER can bind, and the matcher is the corridor
	// solve, whose radius is R. Reading the Stay band here would tie two
	// constants together that must be free to move independently.
	//
	// `bands` is still threaded onward: corridorFollowRuns takes it for its
	// degenerate-leg contract, and the solve itself is band-free.
	cands, err := canonCandidatesForLeg(ctx, canon, pts, CorridorRadiusM)
	if err != nil {
		return assembledLeg{}, err
	}
	// The partition is the corridor solve.
	runs, err := corridorFollowRuns(ctx, pts, cands, bands)
	if err != nil {
		return assembledLeg{}, err
	}
	// The partition and the assembler MUST see the SAME candidate slice:
	// FollowRun.CanonIdx indexes into it.
	//
	// ⚠ noNodeLookup, NOT a real node lookup, AND THAT IS DELIBERATE. The
	// any-node snap (snapping a run boundary onto any nearby canon node, not
	// only the candidate's own) is implemented and tested, but deliberately
	// left unwired. Wiring it would change which interior seams adopt an
	// existing node, and nothing records whether an interior seam adopted a
	// pre-existing node or minted a fresh one (only the leg's two outer
	// terminals do). Enabling it should come with such a recorded field, set
	// when the leg is committed, rather than a derivation: node creation times
	// tie within a batch and node-ID magnitude is not a reliable signal.
	// [corridorFollowRuns]'s runs carry exact analytic SourcePos, which
	// [assembleFollowedLegRunsCorridor] assembles directly.
	asm, err := assembleFollowedLegRunsCorridor(pts, cands, runs, bands, noNodeLookup)
	if err != nil {
		return assembledLeg{}, err
	}
	return asm, nil
}

// canonCandidatesForLeg returns the deduped, id-sorted set of canon arcs the
// leg could follow, in two phases:
//
//   - PHASE 1 (cheap, few queries): scan the leg in windows of canonWindowPts
//     points. Per window, an ID-ONLY spatial prefilter (Canon.ArcIDsInRect,
//     no polyline decode) names the window's candidates, and the expensive decode
//     (Canon.ArcsInRect) runs only when the window holds a candidate
//     not already in the per-leg cache. An area-sized arc overlapping every
//     window is therefore decoded ONCE for the leg, not once per window. Both
//     queries share the identical spatial predicate, so the prefilter's id set never
//     exceeds the decoder's; a prefiltered id that fails to decode is a hard
//     error, never a silent drop. On a large test area this took the lookup
//     from ~9 s of repeat polyline decodes to tens of milliseconds.
//   - PHASE 2 (tighten): a window's bbox is coarse (32 winding points can span
//     much of the area), so keep only candidates whose true geometry bbox comes
//     within radiusM of some leg EDGE (arcBBoxNearAnyEdge). The matcher pays
//     O(points × candidates × vertices); handing it the coarse window union
//     instead of the tight set roughly doubled the candidate count and
//     blew up the matcher. Tightening restores the tight set without
//     re-querying.
//
// radiusM is the CORRIDOR MATCHER'S OWN R (CorridorRadiusM), NOT a new
// tolerance: an arc the matcher can bind has source points within R of it, and
// a point within R of an arc's geometry is within R of its bbox, so neither
// the windowed prefilter nor the bbox tighten can drop a bindable candidate.
//
// ⚠ The binding radius belongs to the live matcher (the corridor solve), not
// to followBands.StayM; the two happen to be equal, but must stay free to move
// independently. Sorting by arc id ascending (the rect reader's own order)
// makes the candidate slice, and every CanonIdx and id tie-break inside the
// matcher, deterministic regardless of window iteration order.
func canonCandidatesForLeg(
	ctx context.Context,
	canon Canon,
	pts []Point,
	radiusM float64,
) ([]CanonArc, error) {
	cache := newCanonCache()
	hit := make(map[ArcID]struct{})
	for start := 0; start < len(pts); start += canonWindowPts {
		end := start + canonWindowPts
		if end > len(pts) {
			end = len(pts)
		}
		// ⚠ end+1: CONSECUTIVE WINDOWS MUST OVERLAP BY ONE POINT, or the edge
		// that straddles the seam belongs to no window at all.
		//
		// The windows advance by canonWindowPts and each rect is the bbox of
		// the window's POINTS padded by radiusM, so with half-open slices
		// pts[0:32] and pts[32:64] the EDGE pts[31]-pts[32] is in neither. An
		// arc lying beside that edge's midpoint sits outside both padded rects,
		// Canon.ArcIDsInRect never returns its id, and the per-edge tighten
		// below, which does correctly walk every edge, never gets to see it,
		// because it can only filter what the prefilter already returned.
		// Overlapping by one point puts every edge wholly inside at least one
		// window.
		rect := Bounds(windowStayRect(pts[start:min(end+1, len(pts))], radiusM))

		ids, err := canon.ArcIDsInRect(ctx, rect)
		if err != nil {
			return nil, err
		}
		anyMiss := false
		for _, id := range ids {
			hit[id] = struct{}{}
			if _, ok := cache.byID[id]; !ok {
				anyMiss = true
			}
		}
		if anyMiss {
			arcs, err := canon.ArcsInRect(ctx, rect)
			if err != nil {
				return nil, err
			}
			for _, s := range arcs {
				if _, ok := cache.byID[s.ID]; !ok {
					cache.byID[s.ID] = s
				}
			}
		}
	}

	out := make([]CanonArc, 0, len(hit))
	for id := range hit {
		s, ok := cache.byID[id]
		if !ok {
			// The prefilter and the decoder share one spatial predicate, so every
			// prefiltered id must decode. A miss means the two diverged (store
			// logic drift, or an arc with a dangling endpoint node the decoder
			// dropped): surface it loudly, never drop the candidate.
			return nil, fmt.Errorf("canon candidate arc %d passed the id prefilter but was not decoded by the rect reader", id)
		}
		// Tighten to the per-EDGE set: a window's bbox is coarse (32 winding
		// points can span much of the area), so drop any candidate whose true
		// geometry bbox is provably beyond radiusM of every source EDGE. The
		// matcher pays O(points × candidates × vertices), so a tight candidate
		// set, not the coarse window union, is what keeps it fast.
		//
		// ⚠ THE TEST IS PER-EDGE, NOT PER-POINT. The corridor solver prices
		// distance as an integral along the source EDGE, so an arc can be
		// beyond radiusM of every vertex and within radiusM of a long edge's
		// interior. A per-point test would drop it. Measured on several test
		// areas, a per-point test dropped 171 arcs the per-edge test keeps,
		// of which the solver binds 10: real, and small.
		if arcBBoxNearAnyEdge(s.Vertices, pts, radiusM) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
