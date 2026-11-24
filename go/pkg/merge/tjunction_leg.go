package merge

// The T-junction split.
//
// A leg terminal that lands in the MIDDLE of an existing trail gets nothing
// from the endpoint warp: the warp searches for nodes, every node is an arc
// endpoint, and a terminal half way along a stored trail finds none. The
// T-junction split closes that: where a terminal lands within
// EndpointWarpRadiusM of a canon arc's INTERIOR, it splits that arc at the
// projection so the node the endpoint warp needs exists. The endpoint warp
// then runs completely unchanged and warps onto it.
//
// THIS IS THE ONLY STEP THAT REWRITES EXISTING CANON GEOMETRY. The follow and
// the endpoint warp never mutate canon; the endpoint warp's safety argument
// is that it only ever COPIES coordinates. Keeping every such write here is
// the design: if a change makes the endpoint warp write, the design has gone
// wrong.
//
// Ordering. This runs immediately BEFORE warpLegEndpointsToNodes, which is
// also before the elevation fill, so a minted node's elevation is
// interpolated from canon rather than sampled from a terrain model at a
// position the point has since left.
//
// Three of the six steps below are DECLINES: they leave the terminal where
// the source put it. That is not a fallback: nothing is guessed, substituted
// or degraded, and each has a concrete hazard behind it that is documented
// at the check. The one genuinely impossible state, a minted node further
// from the terminal than the warp radius, is an ERROR that aborts the merge.

import (
	"context"
	"fmt"
	"math"
)

// SplitCanonAtLegTerminals is the T-junction split's entry point: for each
// of the leg's two terminals it decides whether a canon arc should be split
// so that the endpoint warp has a node to warp onto, and performs the split.
//
// The leg itself is NEVER modified: this step returns no geometry. It moves
// nothing and writes nothing of this leg; the only thing it changes is canon.
//
// Call this immediately BEFORE warpLegEndpointsToNodes, on the raw source.
//
// Every error aborts the merge and the caller discards its writes. There is
// no path that commits the leg after a failed split: a split we could not
// complete or could not verify means we do not know what shape canon is now
// in, and continuing would be exactly the guess this step avoids.
func SplitCanonAtLegTerminals(
	ctx context.Context,
	canon Canon,
	pts []Point,
) (TjunctionReport, error) {
	var rep TjunctionReport
	if len(pts) < 2 {
		return rep, fmt.Errorf(
			"SplitCanonAtLegTerminals: leg has %d points, need at least 2", len(pts))
	}

	// FIRST terminal, then LAST, sequentially, each against a FRESH query.
	//
	// The first split DELETES the parent arc and mints two halves with new
	// ids, so a candidate found before it is stale: splitting it would target a
	// deleted arc. Nothing may be cached across the two terminals.
	//
	// This composes with the endpoint warp, which resolves both terminals
	// against the canon as it stands before EITHER of them moves: by the time
	// it runs, both splits have landed, so both nodes are there to be found.
	//
	// Neither call needs the leg's own measure length (see
	// tjunctionDeclineFor's doc). A non-finite leg is still caught, just one
	// function later: warpLegEndpointsToNodes errors on the same condition
	// over the same array.
	start, err := SplitCanonAtTerminal(ctx, canon, pts[0])
	if err != nil {
		return rep, err
	}
	rep.Start = start

	end, err := SplitCanonAtTerminal(ctx, canon, pts[len(pts)-1])
	if err != nil {
		return rep, err
	}
	rep.End = end

	return rep, nil
}

// SplitCanonAtTerminal runs the whole rule for ONE terminal: probe, search,
// decide, split, verify.
func SplitCanonAtTerminal(
	ctx context.Context,
	canon Canon,
	p Point,
) (TjunctionTerminal, error) {
	// 1. Does the endpoint warp already have this? A node inside the radius
	//    means the terminal warps onto it unchanged, and this step must not
	//    mint a second node beside it. This is also what makes the split
	//    IDEMPOTENT: the node a previous import minted at this T is found
	//    here, so re-importing the same file splits nothing.
	if _, ok, err := NearestNode(
		ctx, canon, p.Lat, p.Lon, EndpointWarpRadiusM, nil,
	); err != nil {
		return TjunctionTerminal{}, err
	} else if ok {
		return TjunctionTerminal{Decline: TjDeclineNodeInRadius}, nil
	}

	// 2. Otherwise: which canon arc's INTERIOR passes nearest? Candidates
	//    include geometry written earlier in this same merge, as the follow
	//    and the endpoint warp also allow. There is no exclusion set.
	cand, ok, err := NearestArc(
		ctx, canon, p.Lat, p.Lon, EndpointWarpRadiusM)
	if err != nil {
		return TjunctionTerminal{}, err
	}
	if !ok {
		return TjunctionTerminal{Decline: TjDeclineNoCanonInRadius}, nil
	}

	out := TjunctionTerminal{
		ArcID:     cand.ID,
		ProjDistM: cand.DistM,
		StubLenM:  math.Min(cand.PriorLenM, cand.NextLenM),
	}

	// 3-4. The two declines.
	if d := tjunctionDeclineFor(NearbyCanonArc(cand)); d != TjNoDecline {
		out.Decline = d
		return out, nil
	}

	// 5. THE ONE-NODE RULE AT THE PLACE THE NODE ACTUALLY LANDS.
	//
	//    Step 1 asks "is there a node within the radius of the TERMINAL?": the
	//    right question for "has the endpoint warp already got this", and the
	//    wrong one for "am I about to mint a node beside an existing one",
	//    because the mint happens at the PROJECTION of the terminal onto
	//    canon, up to EndpointWarpRadiusM away. A node 16 m from the terminal
	//    is correctly not found by step 1 and can still be 13 m from where
	//    this split lands.
	//
	//    ⚠ THE PARENT'S OWN ENDPOINT NODES CAN TRIGGER THIS, AND DO. There is
	//    no short-stub decline upstream, so a split landing within the radius
	//    of a parent endpoint is declined HERE, by this check
	//    (TjDeclineNodeNearProjection).
	//
	//    It also catches a node on OTHER geometry (a braided arc, or an
	//    earlier leg's terminal).
	//
	//    A DECLINE, not an error, and not an adoption. Adoption is not available
	//    here: this step's job is to put a node ON this canon arc at this
	//    place, and a node 13 m away on different geometry is not that node.
	//    Declining leaves the terminal to the endpoint warp, which is where it
	//    already goes whenever this step declines for any other reason.
	if _, ok, err := NearestNode(
		ctx, canon, cand.Lat, cand.Lon, EndpointWarpRadiusM, nil,
	); err != nil {
		return TjunctionTerminal{}, err
	} else if ok {
		out.Decline = TjDeclineNodeNearProjection
		return out, nil
	}

	// 6. Split, through the canon: [SplitArcAt] checks the fraction, computes
	//    the split and stores it with Canon.SplitArc, which mints the node,
	//    writes both halves, repairs anything the host keeps riding the
	//    parent, and deletes the parent.
	res, err := SplitArcAt(ctx, canon, cand.ID, cand.Fraction)
	if err != nil {
		return TjunctionTerminal{}, fmt.Errorf(
			"split canon arc %d at fraction %v for a terminal %.3f m away: %w",
			cand.ID, cand.Fraction, cand.DistM, err)
	}

	// 7. Verify the node we actually got.
	//
	//    Its coordinates come from the split's own walk of the parent, NOT
	//    from the projection computed above: the fraction is a round trip
	//    through two cumulative-distance sums, and the node is what the
	//    endpoint warp will warp onto. Comparing against cand.Lat/Lon would
	//    check the round trip against itself and prove nothing.
	//
	//    An ERROR, not a decline: the split has already happened, so there is
	//    nothing left to decline to, and a node outside the radius would hand
	//    the endpoint warp a displacement larger than its budget.
	d := HaversineMetres(p.Lat, p.Lon, res.Node.Lat, res.Node.Lon)
	if !(d <= EndpointWarpRadiusM) {
		return TjunctionTerminal{}, fmt.Errorf(
			"split canon arc %d at fraction %v: the minted node %d is %.6f m from the terminal, "+
				"beyond the %v m warp radius (the projection was %.6f m away) — the fraction round trip moved it",
			cand.ID, cand.Fraction, res.Node.ID, d, EndpointWarpRadiusM, cand.DistM)
	}

	out.Split = true
	out.NodeID = res.Node.ID
	out.MintedDistM = d
	out.PriorHalfID = res.Prior
	out.NextHalfID = res.Next
	out.ConsumedParentVertex = res.ConsumedParentVertex
	return out, nil
}
