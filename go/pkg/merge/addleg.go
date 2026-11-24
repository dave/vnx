package merge

import (
	"context"
	"fmt"
)

// LegOptions are what AddLeg needs from its caller besides the canon and the
// leg.
type LegOptions struct {
	// FillElevation fills the leg's missing elevations. AddLeg calls it once,
	// after the endpoint warp and before the follow, with the points that
	// will be committed, so an elevation is sampled where the point ends up.
	// It is required: a caller with nothing to fill passes NoFill, and a nil
	// FillElevation is an error.
	FillElevation func(ctx context.Context, pts []Point) error

	// Label names the leg in error messages and in diagnostic lines, and is
	// the leg string handed to [TestHooks] observers.
	Label string

	// Logf receives the merge's rare diagnostic lines (a tie broken by
	// distance or by node ID), one call per line, in fmt.Printf form with no
	// trailing newline. nil means silent: the lines are not formatted at all.
	Logf func(format string, args ...any)
}

// NoFill is the FillElevation of a leg whose elevations are not filled.
func NoFill(context.Context, []Point) error { return nil }

// LegResult is what AddLeg did to the canon and the leg.
type LegResult struct {
	// TJunction and Align are the T-junction split's and the align step's
	// reports: every canon arc the leg cut, in the order it cut them.
	TJunction TjunctionReport
	Align     AlignReport

	// Warp is the leg's final endpoint warp: StartNode and EndNode are the
	// existing nodes its two ends joined, nil where the end minted its own.
	Warp WarpReport

	// Pieces are the arcs the leg walks, in walk order, each with its
	// direction and whether the leg created it; Joins has one entry per join
	// between consecutive pieces.
	Pieces []LegArcRef
	Joins  []LegJoin

	// WarpedBoundaries are the leg's point indices the boundary warps
	// pinned, and SymmetricSnapInserted the vertices the symmetric snap
	// inserted, both in the committed leg's index space.
	WarpedBoundaries      []int
	SymmetricSnapInserted []SymmetricSnapInsert

	// RingMergedEndNode is the node a ring leg's two ends were merged onto,
	// at the leg's first point; nil when no ring merge happened. Ele is not
	// set.
	RingMergedEndNode *Node

	// Counts for the caller's result.
	RingMergedTerminals        int
	RingSplits                 int
	RingSplitTightPairs        [][2]NodeID
	LegMintedSeamNodes         int
	AdoptedTerminals           int
	DuplicateArcMultiplicity   int
	CreatedDespiteIdenticalArc int
}

// AddLeg merges one leg into the canon. Its steps run in this order: the
// T-junction split, the endpoint warp, FillElevation, the follow, the align
// step, the interior-node seams, the rides, the symmetric snap, the boundary
// warp, the unresolved terminals, and then the pieces, each checked for a
// duplicate arc.
//
// opts.FillElevation is required (pass NoFill for none; nil is an error).
//
// An error poisons the canon: the caller must discard every write the leg
// made.
//
// AddLeg is safe for concurrent use on separate canons: it keeps no package
// state, and everything a call needs travels in its opts and ctx.
func AddLeg(ctx context.Context, canon Canon, pts []Point, opts LegOptions) (LegResult, error) {
	res, err := addLeg(ctx, canon, pts, opts)
	if err != nil {
		return res, apiError("add leg", err)
	}
	return res, nil
}

// addLeg is [AddLeg] without the error prefix the API adds; the merge calls it.
func addLeg(ctx context.Context, canon Canon, pts []Point, opts LegOptions) (LegResult, error) {
	if opts.FillElevation == nil {
		return LegResult{}, fmt.Errorf("FillElevation is nil; pass NoFill for a leg with nothing to fill")
	}
	ctx = withLeg(ctx, opts.Label, opts.Logf)
	var res LegResult
	// The T-junction split: where a terminal lands within
	// EndpointWarpRadiusM of the INTERIOR of a canon arc, where the endpoint
	// warp has no node to warp onto, split that arc so the node exists.
	// The endpoint warp below stays a pure copier. This must precede the
	// warp (which needs the node) and therefore also the elevation fill, so
	// the minted node's elevation is canon's own interpolation rather than a
	// sample at a position the point has since left.
	rep, err := splitCanonAtLegTerminals(ctx, canon, pts)
	if err != nil {
		return LegResult{}, err
	}
	res.TJunction = rep

	// The endpoint warp: move each terminal that lands within
	// EndpointWarpRadiusM of an existing node exactly onto it, blending the
	// translation inward over the leg's first/last 10% of measure length. Runs
	// on the RAW SOURCE, BEFORE the follow below; that ordering is part of
	// the warp's safety argument, not a detail.
	//
	// Ahead of the elevation fill, so elevation is sampled at the coordinates
	// actually committed. Filling first would sample the pre-warp position
	// and then move the point up to a full radius away from where its
	// elevation came from.
	warped, warpRep, err := warpLegEndpointsToNodes(ctx, canon, pts)
	if err != nil {
		return LegResult{}, err
	}

	// The elevation fill: fill the leg's missing elevations BEFORE it is
	// committed. A fill failure fails the leg; there is no fallback to
	// committing without elevations.
	if err := opts.FillElevation(ctx, warped); err != nil {
		return LegResult{}, err
	}

	// The follow: rewrite the stretches of this leg that coincide with
	// existing canon to lie exactly on it, leaving genuinely divergent
	// stretches verbatim (terminals never move, including a terminal the
	// endpoint warp just placed on a node). Runs AFTER the elevation fill
	// above and BEFORE the leg is committed, so a followed stretch carries
	// canon's elevation. Every error fails the leg; there is no fallback to
	// committing the leg verbatim.
	//
	asm, err := followLegAgainstCanon(ctx, canon, warped)
	if err != nil {
		return LegResult{}, err
	}
	followed, runs := asm.Points, asm.Runs

	// The align step: put a node into canon at each followed run's measure
	// boundaries, so the stretch this leg rides spans a canon arc END TO END.
	// AFTER the follow, whose runs are its input, and BEFORE the commit below.
	//
	// CANON ONLY: the leg half is the leg cut, below.
	alignRep, err := splitCanonAtRunBoundaries(ctx, canon, runs, warpRep.StartNode, warpRep.EndNode)
	if err != nil {
		return LegResult{}, err
	}
	res.Align = alignRep

	// The leg cut: the leg is cut at the same boundaries and each piece
	// terminates on the node the align step found or minted there, so
	// following canon and being JOINED to it are one fact. A leg with no
	// followed run has no seam, and commits one arc with two fresh nodes.
	seams, err := legSeamsFromAlign(ctx, asm, alignRep, warpRep.StartNode, warpRep.EndNode)
	if err != nil {
		return LegResult{}, err
	}
	// AN ARC MAY NOT RUN THROUGH A NODE (see [legSeamsAtInteriorNodes] for
	// the rule and why there is no radius). Every vertex
	// of this leg that is BITWISE an existing node's coordinate becomes a
	// seam, so the piece is cut there instead of drawing over the junction.
	//
	// ⚠ AFTER legSeamsFromAlign AND BEFORE resolveLegRides, and both halves
	// are load-bearing. After the leg cut, because the align seams are its exclusion
	// set. Before the rides, because a ride is keyed on the COMMITTED
	// PIECE's point range — resolved first, a ride would span a piece the
	// writer is about to cut in two, be claimed by neither half, and both
	// halves would copy the canon the ride had already named.
	//
	// It inserts no vertex (the split point is a vertex the leg already
	// had), so unlike the symmetric snap below it needs no index remap and
	// leaves LegRides.PointCount a sufficient witness.
	seams, _, err = legSeamsAtInteriorNodes(ctx, canon, followed, seams, warpRep.StartNode, warpRep.EndNode)
	if err != nil {
		return LegResult{}, err
	}
	// Ride resolution: THE MATCHER'S VERDICT, RESOLVED ONCE PER LEG, HERE. A
	// ride is what the writer references instead of committing a parallel
	// copy; see [LegRide]. Resolved from `asm` and `alignRep` together (the
	// two records that between them say which piece was ON and which canon
	// arcs now cover it) and BEFORE the symmetric snap and the warp, because
	// both of those work in a point-index space this leg is about to leave.
	// LegRides.PointCount is what a stale ride is DETECTED by: the mismatch
	// is an error raised just before commitLegPieces, not a quiet refusal of
	// the ride.
	var rides *LegRides
	rides, err = resolveLegRides(ctx, asm, alignRep, seams)
	if err != nil {
		return LegResult{}, err
	}

	// The symmetric snap: the THIRD proximity question. Does an existing
	// NODE fall on the geometry about to be committed? The endpoint warp and
	// the T-junction split ask about the leg's TERMINALS and the follow about
	// its BODY running along an arc; a node belonging to a trail this leg
	// merely CROSSES is invisible to both. Every canon node within
	// EndpointWarpRadiusM of this leg's path becomes an interior seam,
	// subject to the same declines the align seams apply.
	//
	// ⚠ AFTER legSeamsFromAlign AND BEFORE applyLegBoundaryWarp, and both
	// halves of that placement are load-bearing. After the leg cut, because
	// the align seams are its exclusion set and its stub-rule boundaries;
	// asked first, it would adopt a node the align step is about to claim and
	// put two seams on one node. Before the warp, because the seams it makes are
	// Absorbed and it is the warp that moves the leg onto their nodes.
	//
	// followed/seams are REPLACED, the same convention the two calls below
	// use: this INSERTS a vertex at each adopted projection, which renumbers
	// every later index, and the producer does that remapping itself.
	var snapRep SymmetricSnapReport
	followed, seams, snapRep, err = legSeamsFromNearbyNodes(ctx, canon, followed, seams, asm.OnPieces, warpRep.StartNode, warpRep.EndNode)
	if err != nil {
		return LegResult{}, err
	}
	// The indices this producer ADDED, captured here and returned as
	// LegResult.SymmetricSnapInserted: recorded rather than re-derived,
	// because they cannot be recovered later. pointsAfterSnap pins the array
	// length at this moment so the check below can prove the indices are
	// still meaningful when they are finally recorded.
	symmetricSnapInserted := snapRep.inserted()
	pointsAfterSnap := len(followed)
	// The rides were resolved against the PRE-snap index space; move them
	// into this one (see LegRides.remapAfterSnapInsertions). Abandoning
	// them instead was measured to leave kilometres of byte-identical
	// parallel strands.
	rides.remapAfterSnapInsertions(symmetricSnapInserted, pointsAfterSnap)
	// The boundary warp, the leg half of the one-node rule: an Absorbed
	// seam's node is up to EndpointWarpRadiusM from where the leg actually
	// is, so the leg must be warped to reach it before it commits (see
	// applyLegBoundaryWarp). followed/seams are REPLACED, not merely read,
	// because a too-short gap next to a boundary can also collapse points
	// out of the leg (Idx remapped accordingly).
	followed, seams, err = applyLegBoundaryWarp(followed, seams)
	if err != nil {
		return LegResult{}, err
	}
	// Record every index the warp above actually pinned (returned as
	// LegResult.WarpedBoundaries). Captured HERE, before
	// collapseSameNodeSeams can drop or renumber anything below: every
	// element of seams at this point had a warpBoundaryTarget built for
	// it by applyLegBoundaryWarp (legSeamsFromAlign never returns a
	// !HasNode seam, and that function filters nothing further), so the
	// full set is simply every Idx still in seams.
	warpedBoundaries := make([]int, len(seams))
	for i, s := range seams {
		warpedBoundaries[i] = s.Idx
	}
	// The unresolved terminals, the one-node rule at the leg-terminal mint
	// site: never mint a node within EndpointWarpRadiusM of an existing one;
	// adopt it instead. Only the side(s) warpRep left nil are looked up.
	// This is done here rather than in the writer (see
	// warpUnresolvedLegTerminals for why). followed/warpRep are REPLACED,
	// matching the applyLegBoundaryWarp convention above. multiPiece
	// (len(seams) > 0 at this point) gates the multi-piece ring merge of the
	// leg's own two ends; see that function's doc.
	var ringMergedByGap bool
	followed, warpRep, ringMergedByGap, err = warpUnresolvedLegTerminals(ctx, canon, followed, warpRep, len(seams) > 0)
	if err != nil {
		return LegResult{}, err
	}
	if ringMergedByGap {
		res.RingMergedTerminals++
	}
	// The warp/collapse above can shrink a same-node piece that had real
	// extent a moment ago down to an empty one; see collapseSameNodeSeams
	// for why this MUST run again, against the now-warped points, not just
	// once inside legSeamsFromAlign.
	seams = collapseSameNodeSeams(seams, followed, warpRep.StartNode, warpRep.EndNode)
	res.LegMintedSeamNodes = legMintedSeamNodes(followed, seams)
	res.AdoptedTerminals = adoptedTerminalCount(warpRep, ringMergedByGap)
	// ⚠ THE RIDES' INDEX SPACE MUST STILL BE THIS LEG'S. LegRides keys are
	// POINT INDICES, so a ride is only meaningful while the point count it
	// was resolved against still holds. Returning no ride on a mismatch
	// would be a FALLBACK wearing a guard's clothes: every ON piece of the
	// leg would silently become a created copy of canon, which is exactly
	// the parallel-strand defect ride resolution exists to remove, and the
	// merge would still report success.
	//
	// remapAfterSnapInsertions above is the ONE stage allowed to move the
	// space, and it updates PointCount as it goes. So by here the counts
	// agree or a stage nobody accounted for has changed the leg. Error,
	// not fallback.
	//
	// Unconditional, and deliberately separate from the
	// symmetric-snap-indices guard below: that one only fires when the
	// snap inserted something, and the population this catches is every
	// OTHER way the count could move.
	if err := checkRidesIndexSpace(rides, len(followed), opts.Label); err != nil {
		return LegResult{}, err
	}
	pieces, seamJoins, ringSplits, ringMergedTerminals, tightPairs, ringMergedNode, interiorWarpedBoundaries, dupMultiplicities, createdDespiteDup, err := commitLegPieces(ctx, canon, followed, seams, rides, warpRep.StartNode, warpRep.EndNode)
	if err != nil {
		return LegResult{}, err
	}
	// A broken invariant this leg SURVIVED rather than failed on. It is
	// reported so the caller can stop it growing quietly while every merge
	// reports success.
	res.DuplicateArcMultiplicity = dupMultiplicities
	res.CreatedDespiteIdenticalArc = createdDespiteDup
	res.RingSplits = ringSplits
	res.RingMergedTerminals += ringMergedTerminals
	res.RingSplitTightPairs = tightPairs
	// ringMergedNode is non-nil for a single-piece leg whose two ends were
	// ring-merged in the commit step, and ringMergedByGap is set for the
	// multi-piece ring merge above (see LegResult.RingMergedEndNode). Either
	// way the node sits at followed[0]'s own coordinate: the ring node
	// reuses the piece's own start, and the leg's first point is the minted
	// coordinate, untouched. Captured here, not re-derived downstream: the
	// coordinate is read from the same `followed` this leg actually
	// committed, not assumed.
	if ringMergedNode != nil {
		res.RingMergedEndNode = &Node{ID: *ringMergedNode, Lat: followed[0].Lat, Lon: followed[0].Lon}
	} else if ringMergedByGap {
		res.RingMergedEndNode = &Node{ID: *warpRep.StartNode, Lat: followed[0].Lat, Lon: followed[0].Lon}
	}
	// interiorWarpedBoundaries is commitLegPieces's own account of
	// every ring split's interior warp: a SECOND call to
	// warpLegAtBoundaries that function makes on its own, distinct
	// from applyLegBoundaryWarp above, which warpedBoundaries alone
	// would miss. Same index space, so a plain append is
	// correct: nothing to remap or de-duplicate against.
	allWarpedBoundaries := append(append([]int(nil), warpedBoundaries...), interiorWarpedBoundaries...)
	// ⚠ THE RECORDED INDICES ARE ONLY MEANINGFUL IF NOTHING BETWEEN THE
	// INSERTION AND HERE CHANGED THE POINT COUNT. Today nothing does:
	// applyLegBoundaryWarp returns the same length with Idx UNCHANGED,
	// warpUnresolvedLegTerminals warps in place, and commitLegPieces's ring
	// split adds a PIECE, not a point. That is a property of today's
	// pipeline, not a guarantee, and a reader stripping by a stale index
	// would silently compare the wrong vertices. So it is asserted rather
	// than assumed: if a future stage starts adding or dropping points, this
	// fails loud here instead of going quietly wrong in the caller.
	if len(symmetricSnapInserted) > 0 && len(followed) != pointsAfterSnap {
		return LegResult{}, fmt.Errorf(
			"symmetric snap recorded %d inserted vertex/vertices at a %d-point leg, but the leg is %d points by the time it commits — a later stage changed the point count",
			len(symmetricSnapInserted), pointsAfterSnap, len(followed))
	}
	res.Warp = warpRep
	res.Pieces = pieces
	res.Joins = seamJoins
	res.WarpedBoundaries = allWarpedBoundaries
	res.SymmetricSnapInserted = symmetricSnapInserted
	return res, nil
}

// checkRidesIndexSpace fails when the leg about to commit is no longer the
// length its rides were resolved against. leg names the leg in the error
// (LegOptions.Label).
//
// A ride is a range of POINT INDICES. remapAfterSnapInsertions is the one
// stage allowed to move that space, and it updates [LegRides.PointCount] as it
// goes, so by commit time the two counts agree, or a stage nobody accounted
// for has changed the leg and every ride now names different vertices.
//
// It is an error, not a fallback: returning nil per piece instead would turn
// every ON piece of the leg into a created copy of canon, the parallel strands
// ride resolution exists to remove, and the merge would still report success.
//
// nil rides is not a mismatch: a leg the matcher resolved nothing for offers
// no ranges and has no index space to invalidate.
func checkRidesIndexSpace(rides *LegRides, pointCount int, leg string) error {
	if rides == nil || pointCount == rides.PointCount {
		return nil
	}
	return fmt.Errorf(
		"leg %s commits %d point(s) but its canon rides were resolved against %d — a stage between ride resolution and the commit changed the point count",
		leg, pointCount, rides.PointCount)
}
