package merge

import (
	"context"
	"fmt"
)

// referenceRideArcs turns a resolved ride into the walk the leg makes over
// canon's OWN arcs. It only reads the canon and writes nothing.
//
// ⚠ THE CHAIN IS CHECKED AGAINST THE STORED ARCS' OWN NODE IDS, NOT AGAINST
// THE LINEAGE THAT PRODUCED THEM. The lineage is the align step's record of
// what it cut; these are the arcs as the canon holds them now. If the two
// disagree, the piece is about to be threaded onto a walk that does not
// exist, and there is NO fallback to "write the bytes instead": that
// fall-through is precisely the silent parallel copy ride resolution exists to
// stop, and it would be written by the one code path that has just proved it
// does not understand the geometry.
func referenceRideArcs(ctx context.Context, canon Canon, ride *LegRide, startNode, endNode NodeID) ([]LegArcRef, error) {
	refs := make([]LegArcRef, 0, len(ride.Arcs))
	chainM := 0.0
	cur := startNode
	for i, id := range ride.Arcs {
		arc, err := canon.Arc(ctx, id)
		if err != nil {
			return nil, fmt.Errorf(
				"ResolveLegRides: canon arc %d (%d of %d on run %d's ride from node %d to node %d) could not be read — the align step recorded it as a leaf of the arc the run rode, so it was there when this leg's own pass ended: %w",
				id, i+1, len(ride.Arcs), ride.Run, startNode, endNode, err)
		}
		var dir Direction
		switch cur {
		case arc.StartNodeID:
			dir, cur = DirForward, arc.EndNodeID
		case arc.EndNodeID:
			dir, cur = DirReverse, arc.StartNodeID
		default:
			return nil, fmt.Errorf(
				"ResolveLegRides: run %d's ride does not chain — arc %d (%d of %d) runs %d..%d and the walk had reached node %d, going from node %d to node %d over %v",
				ride.Run, id, i+1, len(ride.Arcs), arc.StartNodeID, arc.EndNodeID, cur, startNode, endNode, ride.Arcs)
		}
		refs = append(refs, LegArcRef{Arc: LegArc{
			ID: arc.ID, StartNodeID: arc.StartNodeID, EndNodeID: arc.EndNodeID,
		}, Direction: dir, Created: false})
		chainM += arc.DistanceM
	}
	// ⚠ THE RIDE MUST NOT NAME MORE CANON THAN THE RUN CLAIMED. A referenced
	// piece legitimately reaches PAST the run's own measure interval (the
	// absorb gap, drawn with canon's own vertices instead of a chord), and the
	// align step records how far it absorbed at each end. Anything beyond that
	// plus the radius is a ride resolved to leaves the run never rode, and it
	// PERSISTS: the arc count is right, so no count notices it, while the
	// caller's record of the leg walks ground the GPS never touched. Measured
	// before the candidate rule was fixed: a 105 m piece resolved to a 2,081 m
	// walk, and one leg's walk went from 5,097 m to 9,042 m.
	//
	// ⚠ THE YARDSTICK IS THE RUN'S OWN INTERVAL, NOT THE PIECE'S POINT LENGTH,
	// AND THE DIFFERENCE IS WHOSE DEFECT IT IS. Against the piece's length this
	// guard also fires when the MATCHER over-matched (a run claiming 3,957 m of
	// canon for 406 m of mutated source), which aborted some randomised test
	// legs on geometry that must import cleanly. That is the matcher's verdict
	// being wrong, and the fix for a wrong verdict belongs in the matcher, not
	// here. Measured against the RUN's own interval the guard says exactly one
	// thing, which is the one thing this function is responsible for: the
	// leaves named are the leaves the run rode.
	//
	// NO FALLBACK, AND DELIBERATELY NOT "create it instead". Falling back would
	// hide exactly the case this exists to surface, and leave the resolver's
	// defect in place. No new constant: both terms are already in the record.
	if allow := ride.RunM + ride.AbsorbM + 2*EndpointWarpRadiusM; chainM > allow {
		return nil, fmt.Errorf(
			"ResolveLegRides: run %d's ride over %v on canon arc %d is %.1f m of chain for a run that measured %.1f m of that arc (nodes %d..%d) — a referenced piece may cover the run's own interval plus the %.1f m the align step absorbed at its ends plus the %.1f m radius, and this is %.1f m beyond that, so the ride names leaves the run never rode",
			ride.Run, ride.Arcs, ride.Canon, chainM, ride.RunM, startNode, endNode, ride.AbsorbM, 2*EndpointWarpRadiusM, chainM-allow)
	}
	if cur != endNode {
		return nil, fmt.Errorf(
			"ResolveLegRides: run %d's ride over %v starts at node %d and ends at node %d, but the piece ends on node %d — the align record and the nodes threaded to the writer describe different stretches",
			ride.Run, ride.Arcs, startNode, cur, endNode)
	}
	return refs, nil
}
