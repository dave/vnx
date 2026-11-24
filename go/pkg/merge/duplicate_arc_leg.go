package merge

// The duplicate-arc detector.
//
// One question, asked at the moment a leg piece is about to be written: is
// there already a canon arc between the SAME TWO NODES whose vertex sequence
// is identical to what this piece would commit, forward or reversed?
//
// ⚠ The detector only answers; it changes nothing that is committed. The
// caller decides what to do with a match.
//
// ── WHAT "IDENTICAL" MEANS, AND WHAT IT DELIBERATELY DOES NOT ───────────────
//
// Exact equality of the lat/lon vertex sequence. No tolerance, ever. The
// detector makes NO geometric judgement: a strand 8 m wide is the matcher's
// verdict that two recordings differ, and if that verdict is wrong the fix
// belongs in the matcher, after which the follow rewrites one onto the other
// and the pair arrives here equal with no judgement of our own. On real data
// the population a tolerance would wrongly swallow is not hypothetical:
// strands exist at 0.00 m separation both ways with different vertex
// sequences, and others a couple of metres apart.
//
// ⚠ ELEVATION IS NOT COMPARED. The rule is stated on lat/lon only. Whether a
// piece that references canon may take canon's elevation in place of its own
// is an open question of value substitution, and data without elevation
// cannot inform it (agreement there just means neither side has any). The
// verdict REPORTS elevation agreement so the question stays visible; nothing
// consults it.

import (
	"context"
	"fmt"
)

// DuplicateArcMatch is one canon arc a piece would duplicate exactly.
type DuplicateArcMatch struct {
	// Arc is the whole stored arc, not just its id: the caller references it
	// directly and re-loading it would be a second read of something in hand.
	Arc Arc
	// Reversed says the piece walks this arc OPPOSITE to the direction it is
	// stored in, so an occupancy of it must be DirReverse. It is not a
	// curiosity: a good share of real matches are reversed.
	Reversed bool
	// EleEqual is REPORTED and never consulted — see this file's header.
	EleEqual bool
}

// FindDuplicateCanonArcs answers the detector's question for one piece, and
// returns EVERY canon arc that qualifies, ordered by id.
//
// An empty result is the ordinary case and not a failure.
//
// ⚠ IT RETURNS THE SET AND TAKES NO VERDICT. The multiplicity rule lives at the
// one call site instead (the commit step), for a reason that is not tidiness:
// a network built before this check existed can legitimately hold several
// identical arcs between one node pair, so a detector that errored on sight
// would fail on existing networks. Detection is safe to run everywhere; the
// verdict belongs where the action is.
//
// ⚠ EITHER END UNRESOLVED MEANS NO MATCH, AND THAT IS AN EXPECTED ABSENCE, NOT
// A SWALLOWED ONE. `startNode` / `endNode` are nil when no node was resolved
// to that end, and a node that does not exist yet cannot bound an arc that
// already exists — so "is there a canon arc between the same two nodes" has the
// answer "no", definitively, without a query.
func FindDuplicateCanonArcs(
	ctx context.Context,
	canon Canon,
	pts []Point,
	startNode, endNode *NodeID,
) ([]DuplicateArcMatch, error) {
	if startNode == nil || endNode == nil || len(pts) < 2 {
		return nil, nil
	}
	if *startNode == *endNode {
		// A self-loop piece is handled by the ring split before any arc is
		// written, and the store forbids such an arc outright. There is nothing
		// here to reference.
		return nil, nil
	}
	start, _, err := canon.Node(ctx, *startNode)
	if err != nil {
		return nil, fmt.Errorf("duplicate-arc detector: load start node %d: %w", *startNode, err)
	}
	end, _, err := canon.Node(ctx, *endNode)
	if err != nil {
		return nil, fmt.Errorf("duplicate-arc detector: load end node %d: %w", *endNode, err)
	}
	candidates, err := canon.ArcsBetween(ctx, *startNode, *endNode)
	if err != nil {
		return nil, fmt.Errorf("duplicate-arc detector: arcs between nodes %d and %d: %w", *startNode, *endNode, err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	mine := PieceCommittedVertices(pts, start, end)

	var matches []DuplicateArcMatch
	for _, arc := range candidates {
		// An arc between this pair may be stored either way round, so its own
		// start/end nodes decide which coordinates bound it — not the piece's.
		aStart, aEnd := start, end
		if arc.StartNodeID != *startNode {
			aStart, aEnd = end, start
		}
		v := DupVertexSequenceEqual(mine, ArcVertices(arc, aStart, aEnd))
		if !v.Equal {
			continue
		}
		matches = append(matches, DuplicateArcMatch{Arc: arc, Reversed: v.Reversed, EleEqual: v.EleEqual})
	}
	return matches, nil
}
