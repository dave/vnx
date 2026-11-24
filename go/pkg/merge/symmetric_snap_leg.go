package merge

// The symmetric snap: the leg-side adoption of nearby canon nodes.
//
// ── THE QUESTION NOBODY ASKED ───────────────────────────────────────────────
//
// The merge asks two proximity questions and both are asked FROM the incoming
// geometry: "is my TERMINAL near an existing node or arc?" (the endpoint warp
// and the T-junction split) and "does my BODY run along a canon arc?" (the
// follow). Neither can see an existing NODE that falls on the geometry being
// committed: it is not my terminal, and we touch at a point rather than
// running together. Nothing looks, at any distance.
//
// Measured on real data, that omission leaves nodes sitting on or beside arcs
// that do not end at them. Most are the mechanism this file closes: the node
// predates the arc, so had the leg that committed the arc adopted the node,
// the arc would have been cut there and both halves would terminate on it.
//
// ── WHAT THIS DOES, AND THE ONE THING IT DOES NOT ───────────────────────────
//
// This is the SECOND producer of [LegSeam]s, beside [LegSeamsFromAlign]. A leg
// about to commit asks which canon nodes its own path passes within
// [EndpointWarpRadiusM], and adopts each one as an interior seam: a vertex is
// inserted at the projection, the seam claims the node, and the boundary warp
// then moves that vertex onto the node so the committed line actually touches
// the junction it claims.
//
// ⚠ **CANON IS NEVER TOUCHED.** Canon is only ever modified to split arcs to
// add nodes, and this step does not even do that: no arc is split, no arc's
// polyline changes, no node is minted or moved. The whole step happens on the
// incoming leg, which is why it needs no new primitive and no accounting term;
// see [LegSeamsFromNearbyNodes]'s own notes on the node identity.
//
// ── THE RULE MIRRORS THE T-JUNCTION SPLIT'S DECLINES
//
// "Adopt every node in range" performs, from the LEG side, the splits the
// T-junction split makes from the CANON side. So [SymmetricSnapDeclineFor]
// mirrors [TjunctionDeclineFor], applied to the LEG's walk instead of the
// arc's:
//
//	TjDeclineTerminalVertex  → SnapDeclineTerminalVertex  (Fraction 0 or 1)
//
// Neither side declines a short stub: a short arc is followed byte-exactly on
// re-import, so the premise of a minimum-length rule does not hold.
// [SymmetricSnapCandidate.StubM] is still reported, measured against every
// boundary the leg already has, for diagnostics.
//
// ⚠ THE ONE-NODE RULE IS SATISFIED STRUCTURALLY, NOT CHECKED. The rule is "no
// two nodes within the radius", and it constrains node CREATION. This file
// creates no node (it adopts one that already exists), so there is nothing for
// the rule to judge. The leg-side hazard it would otherwise guard (one leg
// claiming two different nodes at nearly the same point) is what
// [SnapDeclineVertexAlreadySeamed] declines.

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// LegSeamsFromNearbyNodes is the producer. It asks which canon nodes the
// leg's own path passes within `EndpointWarpRadiusM`, adopts the ones the
// decline rules admit, and returns the leg's points with a vertex inserted at each adopted
// projection plus the merged seam list in ascending point order.
//
// `pts` is the leg AFTER the follow and AFTER [LegSeamsFromAlign] — the same
// array those seams' Idx values index. `seams` is that align seam list. Both
// are returned REPLACED rather than mutated, matching the convention
// [ApplyLegBoundaryWarp] and [WarpUnresolvedLegTerminals] already use at this
// call site: inserting a vertex renumbers every index after it, and a caller
// holding the old slice would be holding indices into a different array.
//
// ── WHAT EACH ADOPTED SEAM CARRIES, AND WHY Absorbed IS NOT OPTIONAL ────────
//
// `Absorbed`, `AbsorbedM`, `NodeLat`, `NodeLon` are all set, not just
// `Adopted`. [ApplyLegBoundaryWarp] targets a seam's NODE
// coordinates ONLY when `Absorbed`; every other seam gets a zero-displacement
// anchor at its own current point. A seam marked only `Adopted` would therefore
// never move the leg to reach its node, and the leg would commit geometry that
// does not touch the junction it claims — an arc whose endpoint node is up to
// 15 m off the end of its own polyline.
//
// `Adopted` is unconditionally TRUE, and unlike [LegSeamsFromAlign] this needs
// no mintedThisLeg correction. That function has to make one because the align step cuts a
// leg's own boundaries in its own order and a later boundary can resolve onto a
// node an earlier one of the SAME pass just minted. Nothing here mints
// anything, and the search explicitly excludes every node this leg already
// claims, so every node adopted here existed in canon before this leg's commit
// began — which is exactly what `Adopted` means.
//
// `AbsorbedM` is the candidate's own DistM: how far the leg has to reach to
// touch the node, which is the same quantity the align step records when a boundary is
// moved onto a node it did not compute. Taken from the search result, never
// recomputed.
//
// ── THE NODE IDENTITY IS UNCHANGED, AND THAT IS THE WHOLE REASON THIS IS
// CHEAP ─────────────────────────────────────────────────────────────────────
//
//	Nodes == 2×SourceLegs − AdoptedTerminals + AlignCuts + TJunctionCuts
//	         + LegMintedSeamNodes + RingSplits − RingMergedTerminals
//
// Every term counts a node CREATED or a creation SUPPRESSED. This producer
// creates nothing: it adopts a node that already exists in the canon. Concretely, the one
// term that could have moved is `LegMintedSeamNodes`, and [LegMintedSeamNodes]
// counts only seams with `!HasNode` — every seam here has `HasNode: true`, so
// it contributes zero by construction rather than by arithmetic luck.
// `AdoptedTerminals` is untouched because this produces INTERIOR seams and
// never a terminal (a leg's ends are anchors, enforced by
// SnapDeclineTerminalVertex).
//
// The identity is pinned by a test that adopts a seam.
//
// ── THE SENTINEL ────────────────────────────────────────────────────────────
//
// [NodesNearPath] ERRORS on a walk of identical points, wrapping
// [ErrWalkHasNoLength]. That is an EXPECTED absence here rather than a
// fault: a GPX route of two or more identical points is legal input that
// imports today, and such a leg has no path for a node to fall on. It is
// SKIPPED — the leg commits exactly as it does now — and the skip is recorded
// on the report so a caller can tell "asked and found nothing" from "never
// asked". Because the absence is expected, a fallback is correct here; every
// other error aborts the import. A test fails the import outright if the
// sentinel is propagated.
func LegSeamsFromNearbyNodes(
	ctx context.Context,
	canon Canon,
	pts []Point,
	seams []LegSeam,
	onRanges []OnPieceRange,
	startTerminal, endTerminal *NodeID,
) ([]Point, []LegSeam, SymmetricSnapReport, error) {
	st := stateOf(ctx)
	var rep SymmetricSnapReport

	if len(pts) < 2 {
		// Not a walk. NodesNearPath would call this invalid input and abort
		// the import; a leg this short has no interior to seam either way, and
		// the piece writer is the layer that judges whether it is legal
		// geometry at all. Left exactly as it arrived.
		return pts, seams, rep, nil
	}

	// EXCLUSION IS A REQUIRED PARAMETER AND THIS IS WHAT GOES IN IT. Two
	// populations, both of which would be wrong to adopt:
	//
	//   - every node an align seam already claims. Adopting it again would put
	//     two seams on one node, which is the "added twice" the acceptance
	//     forbids. The exclusion is applied inside the search, BEFORE the
	//     radius test, rather than as a post-hoc discard: a nearest-style
	//     filter applied after the fact is how an exclusion gets defeated.
	//   - this leg's own two terminals, where the endpoint warp has already
	//     resolved one. A terminal is a lat-lon anchor; re-adopting it as an
	//     INTERIOR seam would node the leg's own end.
	exclude := make([]NodeID, 0, len(seams)+2)
	for _, s := range seams {
		if s.HasNode {
			exclude = append(exclude, s.Node)
		}
	}
	if startTerminal != nil {
		exclude = append(exclude, *startTerminal)
	}
	if endTerminal != nil {
		exclude = append(exclude, *endTerminal)
	}

	found, err := NodesNearPath(ctx, canon, pts, EndpointWarpRadiusM, exclude)
	if err != nil {
		if errors.Is(err, ErrWalkHasNoLength) {
			rep.Skipped = true
			return pts, seams, rep, nil
		}
		return nil, nil, rep, fmt.Errorf("LegSeamsFromNearbyNodes: %w", err)
	}
	if len(found) == 0 {
		return pts, seams, rep, nil
	}

	// ── THE BOUNDARY SET THE STUB RULE MEASURES AGAINST ─────────────────────
	//
	// The leg's two ends, plus every align seam. All measured on the SAME
	// ruler the search's own PriorLenM came off — see [LegMeasureToVertex].
	total := LegMeasureLengthM(pts)
	boundaries := make([]float64, 0, len(seams)+2+len(found))
	boundaries = append(boundaries, 0, total)
	for _, s := range seams {
		if s.Idx < 0 || s.Idx >= len(pts) {
			return nil, nil, rep, fmt.Errorf(
				"LegSeamsFromNearbyNodes: align seam at point %d is outside the leg's %d points",
				s.Idx, len(pts))
		}
		boundaries = append(boundaries, LegMeasureToVertex(pts, s.Idx))
	}

	// Along-leg order, so StubM sees the leg as the commit will walk it and
	// an accepted candidate bounds the next one. The search's own order
	// is ascending PointIdx (the nearest VERTEX), which is not the same
	// question as where the foot of the perpendicular fell; node id makes this
	// order TOTAL, so two nodes projecting to one point still come out in one
	// fixed sequence.
	cands := make([]NodeNearPath, len(found))
	copy(cands, found)
	if st.hooks.SnapReverseSearchOrder {
		for i, j := 0, len(cands)-1; i < j; i, j = i+1, j-1 {
			cands[i], cands[j] = cands[j], cands[i]
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].PriorLenM != cands[j].PriorLenM {
			return cands[i].PriorLenM < cands[j].PriorLenM
		}
		return cands[i].ID < cands[j].ID
	})

	// ── THE POINT SET [SnapDeclineVertexAlreadySeamed] MEASURES AGAINST ─────
	//
	// Every place on this leg that a seam already claims: each align seam's own
	// vertex, plus each accepted candidate's projection as it is accepted. A
	// candidate landing at ZERO DISTANCE from one of them is declined.
	//
	// Node identity is NOT the question here and `exclude` above already
	// answers that one. This is POINT identity: the colliding nodes are
	// different nodes, which is exactly why a node-identity exclusion cannot
	// see them.
	//
	// ⚠ ZERO DISTANCE, NOT AN EQUAL INDEX, AND NOT A TOLERANCE. Two candidates
	// can project onto the SAME point without either landing on an existing
	// vertex — [InsertSymmetricSnapSeams] then inserts two coincident vertices,
	// and WarpLegAtBoundaries is asked to move that one place onto two nodes.
	// Comparing indices misses it (the vertices have different indices);
	// comparing coordinates bitwise nearly catches it, but the property that
	// actually breaks the taper is zero ARC distance, so that is what is
	// measured. A tolerance would start merging seams that are genuinely
	// different points and is a different decision.
	//
	// ⚠ WHICH ONE SURVIVES IS DECIDED BY THE SORT ABOVE, NOT HERE, and that is
	// deliberate. `cands` is ordered by PriorLenM then by NODE ID, which is a
	// TOTAL order — so coincident candidates are considered lowest id first and
	// the lowest id is the one that gets accepted. A genuine tie is broken by
	// the lower node ID: deterministic, not better. Node ids are assigned by
	// the store and immutable once minted, so the survivor does not depend on
	// map iteration order, on the order the search returned its results, or on
	// which candidate the loop happened to reach first.
	claimed := make([]Point, 0, len(seams)+len(cands))
	for _, s := range seams {
		claimed = append(claimed, pts[s.Idx])
	}
	pointAlreadySeamed := func(lat, lon float64) bool {
		for _, c := range claimed {
			if HaversineMetres(c.Lat, c.Lon, lat, lon) == 0 {
				return true
			}
		}
		return false
	}

	var takes []NodeNearPath
	var ords []int // takes[i]'s slot in rep.Candidates, so the report can be completed

	for _, c := range cands {
		out := SymmetricSnapCandidate{
			Node: c.ID, Lat: c.Lat, Lon: c.Lon,
			DistM: c.DistM, PriorLenM: c.PriorLenM, Fraction: c.Fraction, SegIdx: c.SegIdx,
			ProjLat: c.ProjLat, ProjLon: c.ProjLon,
			StubM: NearestBoundaryGapM(boundaries, c.PriorLenM),
		}
		out.Decline = SymmetricSnapDeclineFor(c.Fraction, pointAlreadySeamed(c.ProjLat, c.ProjLon), SegmentIsOnCanon(onRanges, c.SegIdx))
		rep.Candidates = append(rep.Candidates, out)
		if out.Decline != SnapNoDecline {
			continue
		}
		// An accepted candidate is a boundary for every later one, so a later
		// candidate's StubM is measured against it too.
		boundaries = append(boundaries, c.PriorLenM)
		claimed = append(claimed, Point{Lat: c.ProjLat, Lon: c.ProjLon})
		takes = append(takes, c)
		ords = append(ords, len(rep.Candidates)-1)
	}
	if len(takes) == 0 {
		if st.hooks.SymmetricSnap != nil {
			st.hooks.SymmetricSnap(st.leg, rep)
		}
		if st.hooks.SymmetricSnapOnCanon != nil {
			st.hooks.SymmetricSnapOnCanon(st.leg, rep, onRanges)
		}
		return pts, seams, rep, nil
	}

	outPts, outSeams, err := InsertSymmetricSnapSeams(pts, seams, takes, &rep, ords)
	if err != nil {
		return nil, nil, rep, err
	}
	if st.hooks.SymmetricSnap != nil {
		st.hooks.SymmetricSnap(st.leg, rep)
	}
	if st.hooks.SymmetricSnapOnCanon != nil {
		st.hooks.SymmetricSnapOnCanon(st.leg, rep, onRanges)
	}
	return outPts, outSeams, rep, nil
}
