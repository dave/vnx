// AN ARC MAY NOT RUN THROUGH A NODE.
//
// A committed arc's interior pips are geometry; its two ends are topology. A
// coordinate that is BITWISE EQUAL to an existing node's coordinate and sits in
// the middle of an arc is therefore a place the network says is a junction and
// the arc says is not — the node is unreachable from that arc, and the whole
// stretch either side of it was re-drawn rather than referenced.
//
// ⚠ THE MEASURED CASE. A leg piece was found that is bitwise identical to an
// existing canon arc for its whole length except that it runs a few metres
// PAST that arc's end node to another node. The end node's coordinate sat
// inside the new arc as an interior vertex instead of being a split point, so
// hundreds of metres of canon were re-minted as a copy to carry a 4 m tail.
//
// ⚠ WHY THE SYMMETRIC SNAP DOES NOT ALREADY CATCH IT. [LegSeamsFromNearbyNodes]
// asks the same question with a 15 m radius, but DECLINES
// (SnapDeclineOnCanon) every candidate projecting onto a segment inside an ON
// stretch, on the ground that the ride will reference canon there and a seam
// would cut the ride in half. When the ride then fails to resolve for an
// unrelated reason (for example the leg's end boundary was absorbed onto a
// node that is not on the followed arc's lineage), the piece is copied and
// nothing put the node back. This rule is the backstop for that hole, and it
// needs no radius: the vertex IS the node's coordinate.
//
// ⚠ NO EPSILON, NO CONSTANT, AND NONE MAY BE ADDED. The question is identity,
// not proximity: the same question the follow's candidate switch cost asks of
// two candidates' shared endpoint, and the same argument
// pieceHasExtent and [ZeroArcGap] make for their own exact zeroes. There is
// no number in this file. If a case needs "close enough", it belongs to the
// symmetric snap, which owns proximity and has a fixed radius for it.
package merge

import (
	"context"
	"fmt"
	"sort"
)

// InteriorNodeSeamDecline is why a bitwise-coincident node did NOT become a
// seam. Recorded rather than dropped: a rule that silently declines leaves the
// arc running through the node either way, and the population that keeps doing
// it has to be nameable.
type InteriorNodeSeamDecline string

const (
	InteriorNodeAccepted InteriorNodeSeamDecline = ""
	// The coincidence is at the leg's own first or last vertex. A leg's
	// ends are lat-lon anchors and are never noded by an interior rule.
	InteriorNodeDeclineTerminal InteriorNodeSeamDecline = "terminal-vertex"
	// This leg's points touch the node's coordinate in TWO OR MORE separate
	// stretches — the leg leaves and comes back. One seam would leave the other
	// visits still running through the node, and one seam per visit makes a
	// piece that begins and ends on the same node for every round trip. Both
	// are worse than declining, and a genuine out-and-back through a junction
	// is the ring split's question, not this rule's.
	InteriorNodeDeclineRevisited InteriorNodeSeamDecline = "revisited"
	// A seam of this leg, or one of its own two terminals, already claims this
	// node. Splitting again would put two seams on one node — the same
	// exclusion [LegSeamsFromNearbyNodes] applies, and for the same reason.
	//
	// ⚠ REPORTED, NOT FILTERED OUT OF THE SEARCH. This is not the hazard of a
	// NEAREST reduction taken after a filter: nothing here reduces, and every
	// hit is judged. Excluding these inside the query instead was tried first
	// and measured WORSE as an instrument: some offending arcs disappeared from
	// the candidate list entirely, so the rule looked like it had never seen
	// them.
	InteriorNodeDeclineAlreadySeamed InteriorNodeSeamDecline = "already-seamed"
	// Cutting here would leave a piece with no distance along the leg at all —
	// [ZeroArcGap]'s exact zero, measured to the neighbouring boundary.
	interiorNodeDeclineNoExtent InteriorNodeSeamDecline = "no-extent"
)

// interiorNodeSeamCandidate is one bitwise coincidence and what became of it.
type interiorNodeSeamCandidate struct {
	Idx      int
	Node     NodeID
	Lat, Lon float64
	// Visits is how many of this leg's vertices carry the node's coordinate.
	// A stationary GPS recording standing on a junction produces a long run of
	// them (runs of 20 have been measured) and they are ONE crossing, cut once.
	Visits  int
	Decline InteriorNodeSeamDecline
}

// InteriorNodeSeamReport is every candidate this leg considered.
type InteriorNodeSeamReport struct {
	Candidates []interiorNodeSeamCandidate
}

func (r InteriorNodeSeamReport) Accepted() int {
	n := 0
	for _, c := range r.Candidates {
		if c.Decline == InteriorNodeAccepted {
			n++
		}
	}
	return n
}

// InteriorNodeSeamObserverForTests reports every leg's whole candidate set,
// accepted and declined alike, under the [CurrentLegForTests] convention every
// other observer in this package uses. nil on a real import.
//
// It exists because a DECLINE leaves no trace whatsoever — the leg commits
// exactly as it would have — so "the rule looked here and refused" is otherwise
// unfalsifiable at scale, and a committed arc still running through a
// node is equally consistent with a correct decline and with the rule never
// having fired at all.
var InteriorNodeSeamObserverForTests func(leg string, rep InteriorNodeSeamReport)

// LegSeamsAtInteriorNodes adds one seam per existing node whose coordinate is
// bitwise equal to an INTERIOR vertex of the leg, and returns the merged seam
// list in ascending point order.
//
// ⚠ IT INSERTS NO VERTEX, AND THAT IS WHY THE INDEX SPACE IS SAFE. The split
// point is a vertex the leg already had — that is what "bitwise equal" buys —
// so nothing is renumbered, `len(pts)` does not move, and
// [LegRides.PointCount] stays a sufficient witness. This is the whole reason
// the rule can run BEFORE [ResolveLegRides] instead of needing the
// remap [LegRides.RemapAfterSnapInsertions] does for the symmetric snap.
//
// ⚠ IT MINTS NOTHING, so the node-count identity is untouched by construction
// rather than by arithmetic: every seam it adds has HasNode true, and
// [LegMintedSeamNodes] counts only seams with HasNode false.
//
// Adopted is unconditionally true and Absorbed unconditionally false. The node
// existed before this leg's commit (it came out of the canon), and the leg is
// ALREADY standing on it — there is no displacement for [ApplyLegBoundaryWarp]
// to taper, which is exactly what a non-Absorbed seam asks it for.
func LegSeamsAtInteriorNodes(
	ctx context.Context,
	canon Canon,
	pts []Point,
	seams []LegSeam,
	startTerminal, endTerminal *NodeID,
) ([]LegSeam, InteriorNodeSeamReport, error) {
	var rep InteriorNodeSeamReport
	if len(pts) < 3 {
		// No interior vertex to split at. Nothing to ask.
		return seams, rep, nil
	}

	// Every node a seam of this leg already claims, plus the two terminals the
	// endpoint warp resolved. Both would put a second seam on one node — but they are
	// DECLINED rather than hidden from the search; see
	// [InteriorNodeDeclineAlreadySeamed] for the measurement that settled it.
	claimed := map[NodeID]bool{}
	for _, s := range seams {
		if s.HasNode {
			claimed[s.Node] = true
		}
	}
	if startTerminal != nil {
		claimed[*startTerminal] = true
	}
	if endTerminal != nil {
		claimed[*endTerminal] = true
	}

	// EXCLUDE NOTHING, SAID EXPLICITLY. [NodesAtWalkVertices] makes
	// `exclude` a required parameter precisely so a caller cannot forget the
	// question, and this caller's answer is "no node is excluded": the search is
	// for vertices the leg is ALREADY standing on, and a node's own membership
	// of this leg is what `claimed` filters afterwards, not what the identity
	// search should be blind to.
	found, err := NodesAtWalkVertices(ctx, canon, pts, []NodeID{})
	if err != nil {
		return nil, rep, fmt.Errorf("LegSeamsAtInteriorNodes: %w", err)
	}
	if len(found) == 0 {
		if InteriorNodeSeamObserverForTests != nil {
			InteriorNodeSeamObserverForTests(CurrentLegForTests, rep)
		}
		return seams, rep, nil
	}

	// ── ONE CROSSING, ONE SEAM ──────────────────────────────────────────────
	//
	// The hits are grouped by node id, in ascending vertex order. A CONSECUTIVE
	// run of hits is one crossing held still (a recording can stand on one
	// node for 20 consecutive samples) and is cut at
	// its FIRST vertex, so the piece before it ends exactly on the node and the
	// stationary tail belongs to the piece after. Cutting per hit would shatter
	// such an arc into many pieces, nearly all of them zero-length, which the
	// store forbids outright.
	byNode := map[NodeID][]int{}
	order := []NodeID{}
	coord := map[NodeID][2]float64{}
	for _, f := range found {
		if _, seen := byNode[f.ID]; !seen {
			order = append(order, f.ID)
			coord[f.ID] = [2]float64{f.Lat, f.Lon}
		}
		byNode[f.ID] = append(byNode[f.ID], f.VertexIdx)
	}

	last := len(pts) - 1
	seamAt := map[int]bool{}
	for _, s := range seams {
		seamAt[s.Idx] = true
	}

	type accepted struct {
		idx  int
		node NodeID
		lat  float64
		lon  float64
	}
	var takes []accepted

	for _, id := range order {
		idxs := byNode[id]
		c := interiorNodeSeamCandidate{
			Idx: idxs[0], Node: id,
			Lat: coord[id][0], Lon: coord[id][1],
			Visits: len(idxs),
		}
		// Interior only: a leg's ends are anchors. A terminal hit is dropped from the run before
		// the consecutiveness test, so a leg that STARTS standing on the node
		// and then walks away is still one crossing of its own start anchor and
		// is declined, not cut.
		interior := make([]int, 0, len(idxs))
		for _, i := range idxs {
			if i > 0 && i < last {
				interior = append(interior, i)
			}
		}
		if len(interior) == 0 {
			c.Decline = InteriorNodeDeclineTerminal
			rep.Candidates = append(rep.Candidates, c)
			continue
		}
		c.Idx = interior[0]
		// ONE consecutive run over the WHOLE hit list, terminals included — a
		// leg that stands on the node across its own first vertex is still one
		// crossing, and testing the interior subset alone would call the gap
		// the filter just made a second visit.
		consecutive := true
		for k := 1; k < len(idxs); k++ {
			if idxs[k] != idxs[k-1]+1 {
				consecutive = false
				break
			}
		}
		if !consecutive {
			c.Decline = InteriorNodeDeclineRevisited
			rep.Candidates = append(rep.Candidates, c)
			continue
		}
		if claimed[id] || seamAt[c.Idx] {
			c.Decline = InteriorNodeDeclineAlreadySeamed
			rep.Candidates = append(rep.Candidates, c)
			continue
		}
		// ── EXTENT, AGAINST THE BOUNDARIES AS THEY WILL ACTUALLY STAND ──────
		//
		// The neighbours are the leg's own ends, the align seams, and every
		// candidate accepted so far — the same "an accepted candidate bounds
		// the next one" the symmetric snap's StubM uses, for the same
		// reason: two accepted cuts must not make an empty piece between them.
		//
		// ⚠ [ZeroArcGap], NOT pieceHasExtent, AND THE DIFFERENCE COST THE
		// HEADLINE CASE. pieceHasExtent calls a TWO-POINT piece extentless
		// unconditionally, because the piece writer overwrites both of its ends with the
		// seam node's coordinate — true where it is used, which is only ever
		// between two seams naming the SAME node. Here the two ends are
		// different nodes at different coordinates, and a two-point piece
		// between them is a real arc (the measured case above is 4.31 m of
		// geometry over one edge), and pieceHasExtent declined every such
		// candidate for having it.
		prev, next := 0, last
		for _, s := range seams {
			if s.Idx < c.Idx && s.Idx > prev {
				prev = s.Idx
			}
			if s.Idx > c.Idx && s.Idx < next {
				next = s.Idx
			}
		}
		for _, t := range takes {
			if t.idx < c.Idx && t.idx > prev {
				prev = t.idx
			}
			if t.idx > c.Idx && t.idx < next {
				next = t.idx
			}
		}
		if ZeroArcGap(pts, prev, c.Idx) || ZeroArcGap(pts, c.Idx, next) {
			c.Decline = interiorNodeDeclineNoExtent
			rep.Candidates = append(rep.Candidates, c)
			continue
		}
		takes = append(takes, accepted{idx: c.Idx, node: id, lat: c.Lat, lon: c.Lon})
		seamAt[c.Idx] = true
		rep.Candidates = append(rep.Candidates, c)
	}

	if InteriorNodeSeamObserverForTests != nil {
		InteriorNodeSeamObserverForTests(CurrentLegForTests, rep)
	}
	if len(takes) == 0 {
		return seams, rep, nil
	}

	out := make([]LegSeam, 0, len(seams)+len(takes))
	out = append(out, seams...)
	for _, t := range takes {
		out = append(out, LegSeam{
			Idx: t.idx, Node: t.node, HasNode: true, Adopted: true,
			NodeLat: t.lat, NodeLon: t.lon,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Idx < out[j].Idx })
	// The same collapse [LegSeamsFromAlign] ends with, for the same reason and
	// idempotent on a list it has nothing to do to: a new seam can sit beside an
	// existing same-node one and make an empty piece the writer would abort on.
	return CollapseSameNodeSeams(out, pts, startTerminal, endTerminal), rep, nil
}
