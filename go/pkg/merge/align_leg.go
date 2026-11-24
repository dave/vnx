package merge

// The align step: split canon at the followed runs' measure boundaries.
//
// The follow rewrites a coincident stretch of the leg to lie byte-exactly on
// canon, but that stretch may span however many canon arcs it crossed: the
// geometry matches, the topology does not. The align step does the canon half
// of fixing that: at each end of an ON run it puts a real node into canon, so
// the stretch the leg rides spans whole canon arcs end to end.
//
// ── WHAT THE ALIGN STEP DOES NOT DO ────────────────────────────────────────
//
// It does not change the leg. Cutting the leg at the same boundaries, and
// sharing those nodes, is the leg cut ([LegSeamsFromAlign]). Keeping the two
// apart keeps each step's effect on the network separately measurable.
//
// This step reuses the T-junction split's mechanism, its decline vocabulary and
// its band rather than growing new ones.
//
// ── THE FOUR THINGS THAT MAKE THIS HARDER THAN IT LOOKS ─────────────────────
//
//  1. **The measure pair is DIRECTIONAL, and this design is immune to it.**
//     A run's MeasureStart and MeasureEnd are the canon measures at its FIRST and
//     LAST source positions ([corridorFollowRunFromMeasure]), so an anti-parallel
//     run (Reversed) has MeasureStart > MeasureEnd; [assembleOnRun] opens by
//     swapping them for exactly that reason. A per-run predicate ("does this run
//     need a split", reading MeasureStart against 0 and MeasureEnd against the
//     total) miscounts reversed runs.
//
//     This step never asks that question. Both measures go into a per-canon-arc
//     POOL, the pool is sorted descending, and each boundary is then qualified
//     INDEPENDENTLY and SYMMETRICALLY against both ends. Pooling makes the pair
//     unordered by construction and the symmetric test makes direction
//     immaterial, so no swap is needed here.
//
//  2. ⚠ **A split DELETES the parent and mints two new ids**, so every boundary
//     after the first must be re-resolved against the arc that now holds it.
//     This is STRUCTURAL rather than a thing to remember: boundaries on one canon
//     arc are cut in DESCENDING measure order, so every remaining (lower)
//     boundary sits inside the PRIOR half, which keeps the parent's start node
//     and therefore its **measure ORIGIN**. The loop re-reads that half and
//     recomputes its cumulative measure from the stored vertices before the next
//     cut. Nothing is carried across a split except one arc id.
//
//     ⚠ **THE ORIGIN IS PRESERVED. THE LENGTH IS NOT.** The split places the node
//     by LINEAR lat/lon interpolation at a parameter derived from a measure ratio,
//     and lat/lon-linear is not measure-linear, so the half can measure slightly
//     LESS than the measure it was cut at (measured up to a few millimetres,
//     about 1e-6 relative, scaling with arc length). A boundary at the previous
//     cut's position can therefore lie PAST the end of the half that now holds
//     it. That is "already at a node" (see DECLINE 1), not an out-of-range
//     measure.
//
//  3. ⚠ **Two runs of ONE leg can bind the same canon arc** (the follow can
//     re-bind two disjoint measures onto one candidate), so boundaries are
//     collected per canon id across the whole leg before any cutting starts.
//     Cutting run by run would have a later run naming an arc an earlier run's
//     split had already deleted.
//
//  4. ⚠ **A boundary that is already at a node is a DECLINE, not an error and
//     not a split.** That is the common case (a run following a canon arc end to
//     end wants nothing done), and it is also what makes the step idempotent: on
//     re-import a run's boundaries project to exactly 0 and total, and nothing is
//     cut.
//
// ── NO NEW TOLERANCE, NO NEW BAND ───────────────────────────────────────────
//
// [MeasureEps] qualifies "is this boundary already at an end", in the same
// measure role [assembleOnRun] uses it. Without it, `t == 1` arithmetic writes a
// half-arc of order 1e-14 m that a self-loop check does NOT catch, because the
// two node ids are distinct.
//
// [FollowBands.MinRunM] was used to decline a cut that would leave a stub
// shorter than the follow's own minimum run (the T-junction split's rule applied
// to the same hazard). That decline is currently disabled; see DECLINE 3.
//
// Where both apply, MinRunM subsumes MeasureEps on the outcome: any boundary
// within MeasureEps (1e-9 m) of an end leaves a piece far under MinRunM. What
// MeasureEps changes is the DECLINE the boundary is reported under, and that is
// not cosmetic: "already at a node" is the state this step exists to reach,
// while "stub too short" is a compromise. The leg cut consumes this report, so
// conflating the two would make it read a satisfied boundary as a failed one.
//
// The stub rule also subsumed near-duplicate boundaries, which is why there is
// no dedupe step and no clustering threshold: two runs wanting a cut at nearly
// the same measure produce one cut.
//
// ── THE ONE-NODE RULE ───────────────────────────────────────────────────────
//
// Everything above answers "is this boundary at THIS arc's own end". None of it
// answers "does some OTHER node, from a different cut or a different arc,
// already sit here". That gap is real: a run following a stored copy of canon
// (the follow copies canon byte-exact) can land a boundary at the identical
// measure of an earlier cut, and the same interpolation on the same vertices
// reproduces the same float64 bits, so a naive cut would mint a SECOND node
// 0.000 m from the first. Nodes from separate saved copies of the same route
// can also sit a fraction of a metre apart on different arcs.
//
// Both cases are one question, "is there already a node here", answered by one
// rule: never mint a node within EndpointWarpRadiusM of an existing one; adopt
// it instead. cutCanonAtMeasure applies it as a single network-wide search
// before every cut. A metre-scale radius can merge places that merely happen to
// be near each other (terminal clusters can span more than the radius), but one
// radius applied everywhere leaves no gap between two differently-scoped
// checks.

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// alignMeasure is one ON-run boundary waiting in a canon arc's pool, carrying
// the run identity forward so the resulting [AlignBoundary] can be looked up by
// the run that asked for it rather than by its float measure.
type alignMeasure struct {
	Run      int
	AtRunEnd bool
	Measure  float64
}

// SplitCanonAtRunBoundaries is the align step's entry point: given the
// EFFECTIVE runs of one leg's follow, put a node into canon at each ON run's
// measure boundaries so the stretch the leg rides spans a canon arc end to end.
//
// ⚠ THE RUNS MUST BE THE ASSEMBLER'S, NOT THE MATCHER'S. [followLegAgainstCanon]
// returns them and its doc says why. The matcher's measures are the provisional
// choice and sit up to one densify step (about 1 m) from where the leg actually
// joins.
//
// The leg itself is NEVER modified: this step returns no geometry and writes
// nothing of this leg. The only thing it changes is canon.
//
// Call this AFTER the follow and BEFORE the leg is frozen. Every error must make
// the caller discard the merge's writes: a cut that could not complete leaves
// canon in an undescribed shape, and continuing would be a guess.
//
// ⚠ NO PATH REPAIR HERE. The Canon's split already repairs the stored walks that
// rode the parent. Do not add a second repair.
//
// startTerminal / endTerminal are the leg's OWN warped terminal nodes (from the
// endpoint warp, [WarpReport] StartNode/EndNode), nil when that terminal warped
// onto nothing. They let the boundary resolver know when a LATER boundary of
// this same leg would resolve back onto a node the leg's own terminal already
// claims. Nothing else in this function reads them.
func SplitCanonAtRunBoundaries(
	ctx context.Context,
	canon Canon,
	runs []FollowRun,
	startTerminal, endTerminal *NodeID,
) (AlignReport, error) {
	var rep AlignReport

	// firstRun / lastRun: the run indices that OWN startTerminal / endTerminal
	// respectively. A boundary belonging to either of those runs is the ordinary,
	// harmless case (it IS the terminal's own resolution, not a revisit of it), so
	// the terminal claim below only ever applies to some OTHER run.
	firstRun, lastRun := 0, len(runs)-1

	// Collect every ON run's two boundaries, grouped by the canon arc the run
	// named. Grouping FIRST is point 3 of the header: two runs of one leg can bind
	// the same arc, and cutting run by run would have the second name an arc the
	// first already deleted.
	byCanon := map[ArcID][]alignMeasure{}
	for i := range runs {
		r := runs[i]
		if r.Kind != RunOn {
			continue
		}
		// BOTH MEASURES, UNORDERED. The pair is directional (an anti-parallel run has
		// MeasureStart > MeasureEnd), and it does not matter: they join a pool that is
		// sorted below, and each is then qualified symmetrically against both ends
		// of whatever arc holds it. See point 1 of the header.
		if math.IsNaN(r.MeasureStart) || math.IsNaN(r.MeasureEnd) || math.IsInf(r.MeasureStart, 0) || math.IsInf(r.MeasureEnd, 0) {
			return AlignReport{}, fmt.Errorf(
				"SplitCanonAtRunBoundaries: run %d on canon arc %d has non-finite measures [%v,%v]",
				i, r.CanonID, r.MeasureStart, r.MeasureEnd)
		}
		byCanon[r.CanonID] = append(byCanon[r.CanonID],
			alignMeasure{Run: i, AtRunEnd: false, Measure: r.MeasureStart},
			alignMeasure{Run: i, AtRunEnd: true, Measure: r.MeasureEnd})
	}
	if len(byCanon) == 0 {
		return rep, nil
	}

	// Deterministic arc order. Map iteration is randomised, and the cuts of
	// two different canon arcs are independent, but the SEQUENCE in which the
	// results are reported is not, and neither is a failure message. Order by id,
	// the same order every canon reader returns.
	ids := make([]ArcID, 0, len(byCanon))
	for id := range byCanon {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, canonID := range ids {
		measures := byCanon[canonID]

		// ⚠ THE RANGE CHECK BELONGS HERE, AGAINST THE ARC THE RUN NAMED, AND
		// NOWHERE ELSE. A measure is a position on the candidate the matcher bound, so
		// "is this measure inside the geometry it was measured against" is a question
		// about THAT arc, asked once, before any of this leg's cutting has moved
		// anything. Asking it again per boundary against a HALF is a different
		// question in the same words: a half's recomputed length is not the measure it
		// was cut at (see point 2 of the header), so a boundary at the previous cut's
		// position can measure a few millimetres PAST the half that now holds it, and
		// treating that as out of range would abort a valid import.
		orig, ok, err := canon.CanonArc(ctx, canonID)
		if err != nil {
			return AlignReport{}, err
		}
		if !ok {
			return AlignReport{}, fmt.Errorf(
				"SplitCanonAtRunBoundaries: canon arc %d is not in the canon — a run cannot have followed geometry that is not there",
				canonID)
		}
		origTotal := CanonMeasureLengthM(orig.Vertices)
		if !(origTotal > 0) || math.IsNaN(origTotal) || math.IsInf(origTotal, 0) {
			return AlignReport{}, fmt.Errorf(
				"SplitCanonAtRunBoundaries: canon arc %d has measure length %v — it cannot carry a boundary", canonID, origTotal)
		}
		for _, a := range measures {
			if a.Measure < -MeasureEps || a.Measure > origTotal+MeasureEps {
				return AlignReport{}, fmt.Errorf(
					"SplitCanonAtRunBoundaries: boundary at measure %.9f m is outside canon arc %d, which is %.9f m long",
					a.Measure, canonID, origTotal)
			}
		}

		// DESCENDING: point 2 of the header. Cutting the highest measure first leaves
		// every remaining boundary inside the prior half at an unchanged measure
		// ORIGIN, because the prior half inherits the parent's start node.
		//
		// Ties break on (run, end) so the sequence is a function of the runs and not
		// of map iteration: two runs of one leg CAN bind the same canon arc at
		// the same measure (point 3), and the second of the pair declines, so which one
		// declines has to be decided by something stable.
		sort.Slice(measures, func(i, j int) bool {
			if measures[i].Measure != measures[j].Measure {
				return measures[i].Measure > measures[j].Measure
			}
			if measures[i].Run != measures[j].Run {
				return measures[i].Run < measures[j].Run
			}
			return !measures[i].AtRunEnd && measures[j].AtRunEnd
		})

		// The cut tree of THIS original arc, recorded as it is built; see
		// [AlignLineage]. It starts as the one whole arc the run named and gains
		// a division per cut; the descending order means the cut always lands in
		// leaves[0] (the current `cur`), so the list stays in ascending measure
		// order without a sort.
		origStartNode, origEndNode, err := canon.ArcEnds(ctx, canonID)
		if err != nil {
			return AlignReport{}, fmt.Errorf(
				"SplitCanonAtRunBoundaries: read endpoint nodes of canon arc %d to record its cut lineage: %w", canonID, err)
		}
		origStart, _, err := canon.Node(ctx, origStartNode)
		if err != nil {
			return AlignReport{}, fmt.Errorf("SplitCanonAtRunBoundaries: load node %d for canon arc %d's cut lineage: %w", origStartNode, canonID, err)
		}
		origEnd, _, err := canon.Node(ctx, origEndNode)
		if err != nil {
			return AlignReport{}, fmt.Errorf("SplitCanonAtRunBoundaries: load node %d for canon arc %d's cut lineage: %w", origEndNode, canonID, err)
		}
		lineage := AlignLineage{Canon: canonID, TotalM: origTotal, Leaves: []AlignLeaf{{
			Arc: canonID, LoM: 0, HiM: origTotal, LoNode: origStartNode, HiNode: origEndNode,
			LoLat: origStart.Lat, LoLon: origStart.Lon, HiLat: origEnd.Lat, HiLon: origEnd.Lon,
		}}}

		cur := canonID
		for _, a := range measures {
			// Which node (if any) is claimed by the leg's own terminal, for a boundary
			// that belongs to some OTHER run of this leg. See cutCanonAtMeasure's
			// DECLINE 1b / DECLINE 2.
			var claim legTerminalClaim
			switch {
			case !a.AtRunEnd && a.Run != firstRun && startTerminal != nil:
				claim = legTerminalClaim{Node: *startTerminal, Active: true}
			case a.AtRunEnd && a.Run != lastRun && endTerminal != nil:
				claim = legTerminalClaim{Node: *endTerminal, Active: true}
			}
			b, err := cutCanonAtMeasure(ctx, canon, canonID, cur, a, claim, lineage)
			if err != nil {
				return AlignReport{}, err
			}
			rep.Boundaries = append(rep.Boundaries, b)
			rep.LineageCutConnectorPairs = append(rep.LineageCutConnectorPairs, b.LineageCutConnectorPairs...)
			if b.Decline == AlignCutMade {
				rep.Cuts++
				// The lower boundaries live in the PRIOR half from here on.
				cur = b.Prior
				// leaves[0] IS `cur` by construction, and the cut replaces it
				// with its two halves: Prior keeps the low end and the original
				// start node, Next takes the stretch up to whatever leaves[0]
				// used to reach. Asserted rather than assumed: if the arc this
				// cut split is not the one the lineage thinks is lowest, the
				// record and the cutting have drifted and every ride resolved
				// from it afterwards would name the wrong arcs.
				if lineage.Leaves[0].Arc != b.Parent {
					return AlignReport{}, fmt.Errorf(
						"SplitCanonAtRunBoundaries: cut on canon arc %d was made against row %d, but the lineage's lowest leaf is row %d — the descending cut order has been broken",
						canonID, b.Parent, lineage.Leaves[0].Arc)
				}
				low := lineage.Leaves[0]
				lineage.Leaves[0] = AlignLeaf{Arc: b.Prior, LoM: low.LoM, HiM: b.MeasureM, LoNode: low.LoNode, HiNode: b.Node,
					LoLat: low.LoLat, LoLon: low.LoLon, HiLat: b.NodeLat, HiLon: b.NodeLon}
				lineage.Leaves = append(lineage.Leaves, AlignLeaf{})
				copy(lineage.Leaves[2:], lineage.Leaves[1:])
				lineage.Leaves[1] = AlignLeaf{Arc: b.Next, LoM: b.MeasureM, HiM: low.HiM, LoNode: b.Node, HiNode: low.HiNode,
					LoLat: b.NodeLat, LoLon: b.NodeLon, HiLat: low.HiLat, HiLon: low.HiLon}
			}
		}
		rep.Lineages = append(rep.Lineages, lineage)
	}
	if err := reresolveAbsorbsAgainstThisPass(ctx, canon, &rep); err != nil {
		return AlignReport{}, err
	}
	return rep, nil
}

// reresolveAbsorbsAgainstThisPass asks the one-node rule's exact-match
// question (see cutCanonAtMeasure) a second time, once the pass has finished
// writing.
//
// ⚠ STILL LOAD-BEARING. The align step resolves and cuts interleaved, per canon
// arc, in one pass. A collect-resolve-apply design (resolving every boundary
// against a frozen pre-cut snapshot, with same-batch boundaries deduped against
// EACH OTHER before any cut) would make this unnecessary, but that is not what
// is built. Measured: with this call disabled, some leg orderings still abort,
// even with the network-wide search; the wider search closes some of what this
// function catches, not all of it.
//
// ⚠ THE DEFECT IT CLOSES ABORTS A WHOLE IMPORT, and the abort is an ORDERING
// ARTIFACT, not a geometric conflict. Example shape: two runs of ONE leg meet
// at one assembled point. The first run's end lands on canon arc X and is CUT
// there, minting a node exactly at the leg's point. The second run's start
// lands on canon arc Y at exactly the SAME coordinate (the two arcs lie on top
// of each other there, because the follow copies canon byte-exact), but arc Y's
// boundary was resolved BEFORE arc X was cut, so the search absorbed it onto
// some other node about 14 m away. The leg cut then has one leg point claimed
// as two different nodes and can only abort.
//
// Nothing about the geometry made those two answers disagree. Resolve the same
// two boundaries in the other order and the search answers the same node for
// both. The pass's own cut order (per canon id, descending measure) decided
// which of two coincident boundaries got the exact answer and which got a
// 14 m guess.
//
// So this re-asks, exactly once, after every cut this pass will make has
// landed. It is the SAME search cutCanonAtMeasure already ran: no new rule,
// no new tolerance, and no new band. What changes is only WHEN the question is
// answered.
//
// ⚠ ONLY ABSORBS ARE RECONSIDERED. An absorb is the one outcome that names a
// node the boundary never measured itself against, up to EndpointWarpRadiusM
// away, and the leg then ADOPTS that coordinate, so the answer carries a
// displacement. A node sitting AT the boundary's own coordinate is strictly
// better evidence about the same point and costs 0 m. The other declines are
// left alone deliberately: AlignDeclineAtNode is already exact, and
// AlignDeclineStayBandStub names no node at all, so promoting it would MINT a
// join where the leg currently passes through uncut: a change to what the
// network contains, not a correction of a wrong answer.
//
// ⚠ NO TERMINAL-CLAIM CHECK HERE, BECAUSE THIS ONLY EVER PROMOTES TO AN EXACT
// (0 m) MATCH. A node at the boundary's own coordinate is somewhere the leg
// demonstrably IS, so joining it states a fact rather than manufacturing a
// revisit, and the same-node ring split handles the resulting shape. That is
// why the search below, though it runs at EndpointWarpRadiusM, only ACTS when
// the result is exactly 0 m away. A match at, say, 8 m would be a genuinely
// different answer, and reconsidering it would need per-boundary context this
// function does not carry; promoting non-exact improvements is the
// collect-resolve-apply restructure described above, not a small addition here.
//
// ⚠ IT IS RARE. Measured on randomised test runs and several real-world test
// areas, promotions are a handful at most and absent on the real-world areas.
// This is not a quality improvement, it is an abort that could not be left in.
func reresolveAbsorbsAgainstThisPass(ctx context.Context, canon Canon, rep *AlignReport) error {
	for i := range rep.Boundaries {
		b := &rep.Boundaries[i]
		if b.Decline != AlignDeclineAbsorbedToNode {
			continue
		}
		existing, found, err := NearestNode(ctx, canon, b.Lat, b.Lon, EndpointWarpRadiusM, nil)
		if err != nil {
			return fmt.Errorf(
				"re-resolve an absorbed boundary against its own coordinate (%.9f,%.9f): %w", b.Lat, b.Lon, err)
		}
		if !found || existing.DistM != 0 || existing.ID == b.Node {
			continue
		}
		b.Decline = AlignDeclineAtExistingNode
		b.Node = existing.ID
		b.NodeLat, b.NodeLon = existing.Lat, existing.Lon
		b.AbsorbedM = 0
	}
	return nil
}

// legTerminalClaim names a node this leg's OWN terminal already sits on, for
// a boundary that belongs to some OTHER run of the SAME leg; see
// SplitCanonAtRunBoundaries, where it is built. The zero value (Active false)
// means no claim applies to this boundary, which is the ordinary case for
// every leg that has no OFF run adjacent to its own ON run boundaries.
type legTerminalClaim struct {
	Node   NodeID
	Active bool
}

// cutCanonAtMeasure resolves ONE boundary against the arc that currently holds
// it, decides, and cuts.
//
// `parent` is that arc: the id the run named for the first boundary on an
// arc, and the previous cut's prior half after that. `measure` is measured from
// the ORIGINAL arc's start node, which is still the parent's start node
// because descending order only ever hands us prior halves.
//
// `claim` is the leg's own terminal claim; see DECLINE 1b / DECLINE 2 below.
func cutCanonAtMeasure(
	ctx context.Context,
	canon Canon,
	runCanonID ArcID,
	parent ArcID,
	a alignMeasure,
	claim legTerminalClaim,
	lineage AlignLineage,
) (AlignBoundary, error) {
	measure := a.Measure

	// RE-READ, every time. The previous cut deleted an arc and minted two, so the
	// vertex list and the cumulative measure this boundary was measured against are
	// stale the moment that landed.
	arc, ok, err := canon.CanonArc(ctx, parent)
	if err != nil {
		return AlignBoundary{}, err
	}
	if !ok {
		// NOT a decline. A run named an arc, or a cut minted a half, and the arc
		// is gone: this pass has lost track of what it split, and there is no
		// answer to fall back to.
		return AlignBoundary{}, fmt.Errorf(
			"SplitCanonAtRunBoundaries: canon arc %d (from run on arc %d) is not in the canon — "+
				"a boundary cannot be resolved against a row that no longer exists",
			parent, runCanonID)
	}

	total := CanonMeasureLengthM(arc.Vertices)
	if !(total > 0) || math.IsNaN(total) || math.IsInf(total, 0) {
		return AlignBoundary{}, fmt.Errorf(
			"SplitCanonAtRunBoundaries: canon arc %d has measure length %v — it cannot carry a boundary", parent, total)
	}

	out := AlignBoundary{
		RunCanonID: runCanonID, Parent: parent,
		Run: a.Run, AtRunEnd: a.AtRunEnd,
		MeasureM: measure, TotalM: total,
	}

	// DECLINE 1: the boundary is AT AN END OF THIS ARC, so the node it wants is
	// already there.
	//
	// ⚠ THE COMPARISON IS `>= total - MeasureEps`, WITH NO UPPER RANGE CHECK ABOVE
	// IT. A measure at or PAST this arc's end is at its end node; that is the
	// correct classification of the geometry, not a tolerance for an unknown. It has
	// to be, because a half's recomputed length is NOT the measure it was cut at:
	// the split places the node by lat/lon-linear interpolation at a
	// measure-derived parameter, lat/lon-linear is not measure-linear, and the half
	// therefore measures up to a few millimetres SHORT of the measure that produced
	// it (about 1e-6 relative, scaling with length). Treating that overshoot as
	// out-of-range would abort a valid import.
	//
	// The genuinely-lost-boundary case is still caught, and caught BETTER: the
	// range check runs once per canon arc in the caller, against the arc the run
	// actually named, before any cutting has moved anything. A boundary that
	// escapes THERE fails loudly; a boundary that merely overshoots a half it
	// helped create is at that half's end.
	//
	// MeasureEps stays in its measure role at the low end too: without it the
	// fraction below can reach (total-1ulp)/total, which passes the split's strict
	// (0,1) fraction check and writes a half of order 1e-14 m that a self-loop
	// check cannot see (the two node ids are distinct).
	//
	// ⚠ A DECLINE STILL HAS TO NAME THE NODE. This step could stop at "nothing to
	// do"; the leg cut cannot, because the leg has to commit its piece ONTO that
	// node, and the boundaries that decline here are exactly the ones where canon
	// already has the topology the leg needs. The id is not derivable afterwards
	// ([CanonArc] carries vertices and no node ids), so it is read here, at the one
	// moment the parent arc is known.
	if measure <= MeasureEps || measure >= total-MeasureEps {
		startNode, endNode, err := canon.ArcEnds(ctx, parent)
		if err != nil {
			return AlignBoundary{}, fmt.Errorf(
				"read endpoint nodes of canon arc %d for a boundary already at its end: %w", parent, err)
		}
		out.Decline = AlignDeclineAtNode
		if measure <= MeasureEps {
			out.Node = startNode
		} else {
			out.Node = endNode
		}
		n, _, err := canon.Node(ctx, out.Node)
		if err != nil {
			return AlignBoundary{}, fmt.Errorf("load node %d for a boundary already at its end: %w", out.Node, err)
		}
		out.NodeLat, out.NodeLon = n.Lat, n.Lon
		return out, nil
	}

	// DECLINE 1b / DECLINE 2: THE ONE-NODE RULE. Before minting any node, if a
	// node already exists within EndpointWarpRadiusM, use that node instead of
	// creating one. One radius, one condition, every node-creation site.
	//
	// ONE search, at the boundary's own computed coordinate, against the WHOLE
	// network: not scoped to this arc's own ends, because the nodes this rule
	// exists to catch are not scoped that way either (two near-coincident nodes can
	// sit on two different arcs entirely).
	//
	// ⚠ REPOINTING A BOUNDARY REQUIRES WARPING THE LEG TO IT. This step only
	// decides WHICH node; it does not move canon (canon is not cut at all when this
	// fires). The leg cut ([WarpLegAtBoundaries]) is what makes the LEG's own
	// geometry actually reach Node, so this boundary's NodeLat/NodeLon and
	// AbsorbedM are recorded here: identity is recorded, never re-derived.
	//
	// ⚠ A NODE CLAIMED BY THE LEG'S OWN TERMINAL IS ADOPTED LIKE ANY OTHER. A
	// terminal that warps exactly onto a node but whose very next source point is
	// still off-line can read as a short OFF run to the follow's hysteresis; the
	// leg's real ON run then starts past run 0, and THAT boundary's search can land
	// back within the radius of the SAME node the terminal already sits on.
	// Adopting it makes the piece between them a same-node ring, and the piece
	// builder splits every such ring at its halfway point, as it does for every
	// other closed loop. Refusing the adopt instead would be a special case that
	// mints extra nodes inside the radius, breaking the one-node rule. The leg may
	// double back over itself there; that is accepted.
	//
	// `claim` is therefore not subtracted from the candidate set. exclude stays
	// empty, not nil, so this call keeps [NearestNode]'s excluding shape: if a
	// future case needs a real exclusion, the site already has it.
	lat, lon := InterpolateMeasurePointM(arc.Vertices, measure)
	out.Lat, out.Lon = lat, lon
	exclude := []NodeID{}
	nearby, foundNearby, err := NearestNode(ctx, canon, lat, lon, EndpointWarpRadiusM, exclude)
	if err != nil {
		return AlignBoundary{}, fmt.Errorf(
			"SplitCanonAtRunBoundaries: search for a node within %v m of the boundary's own coordinate (%.9f,%.9f): %w",
			EndpointWarpRadiusM, lat, lon, err)
	}
	// ⚠ DECLINE 2 PREFERS A NODE ON THE STRETCH THE RUN ACTUALLY RODE.
	//
	// The search above is network-wide, so a meaningful share of the nodes it
	// would absorb a run boundary onto (measured at several to over thirty percent
	// on real-world test areas) are NOT on the arc the run rode: they belong to a
	// parallel copy or to a trail that merely crosses here. The run was ON canon;
	// if the piece's end node is not canon's node, ride resolution cannot match the
	// piece to canon, so it is CREATED, and what gets created is a copy of the
	// whole ridden stretch plus a short taper.
	//
	// So the candidate set for THIS site is the ridden lineage's own nodes, and
	// nothing else. If none is within the radius the boundary is CUT, exactly
	// as if no node were near, and the leg reaches the foreign node (if it ever
	// does) as an ordinary OFF stretch instead of by dragging canon's own line
	// sideways. The one-node rule YIELDS to "no arc parallel to canon" at exactly
	// this point and nowhere else; see the invariant assertion below, which is
	// exempted for precisely the mints this produces and for no others.
	//
	// NO NEW TOLERANCE: the same EndpointWarpRadiusM, measured with the same
	// haversine chord, against a smaller candidate set. The lineage's node
	// coordinates are this step's own record ([AlignLeaf]), not a re-derivation.
	//
	// ⚠ AN EXACT COINCIDENCE IS NOT AN ABSORB, AND IT KEEPS THE OLD ANSWER.
	// The lineage preference applies to the search that MOVES a boundary up to the
	// radius to reach a node. A node at 0.000 m is DECLINE 1b's case: canon already
	// has a node exactly where this boundary is, put there by some other arc's cut,
	// and cutting again would mint a SECOND node 0.000 m from the first. That is
	// what the one-node rule exists to prevent, and no amount of parallel-copy
	// removal is worth reintroducing it. The piece whose ride then cannot be
	// resolved is created instead (rare in practice).
	lineageNearby, lineageDistM, lineageFound, err := NearestLineageNode(lineage, measure, lat, lon)
	if err != nil {
		return AlignBoundary{}, err
	}
	exactForeign := foundNearby && !lineageFound && nearby.DistM == 0
	preferredOverForeign := foundNearby && !lineageFound && !exactForeign
	adopted := lineageFound || exactForeign
	if exactForeign {
		lineageNearby, lineageDistM = Node{ID: nearby.ID, Lat: nearby.Lat, Lon: nearby.Lon}, nearby.DistM
	}
	if A1Decline2SuppressAdoptForTests != nil && A1Decline2SuppressAdoptForTests() {
		// FAULT INJECTION, INERT IN PRODUCTION. The post-mint invariant check
		// below should be unreachable under correct code: DECLINE 2's own search,
		// immediately above, would already have found and ADOPTED anything within
		// radius, so the ONLY way the post-mint check can see something is if
		// DECLINE 2 itself fails to adopt a genuinely-nearby node. This simulates
		// exactly that failure (never the geometry, only this one decision) so the
		// invariant's own abort path can be exercised without engineering a
		// millimetre-scale placement. Nil by default: no behaviour change when unset.
		adopted = false
	}
	if adopted {
		out.Node = lineageNearby.ID
		out.NodeLat, out.NodeLon = lineageNearby.Lat, lineageNearby.Lon
		if lineageDistM == 0 {
			out.Decline = AlignDeclineAtExistingNode
		} else {
			out.Decline = AlignDeclineAbsorbedToNode
			out.AbsorbedM = lineageDistM
		}
		return out, nil
	}

	// Nothing within the radius: fall through to DECLINE 3, which declines
	// (no node, leg continues through uncut) rather than manufacture a
	// same-node revisit.
	//
	// A claimed node found within the radius is ADOPTED above, not refused, so
	// reaching this line can only mean the search found nothing adoptable.

	// DECLINE 3: THE StayM BAND. EndpointWarpRadiusM..MinRunM from the parent's
	// nearest end is too far to absorb (DECLINE 2 above) and was declined here
	// rather than cut, because that band is not evidence of a real branch.
	//
	// EndpointWarpRadiusM and the follow's StayM are the same 15 m, which means
	// this band BEGINS exactly where an entered run is, by construction, still
	// allowed to be riding a candidate it has already left: a leg that truly
	// branches at node N keeps being classified ON until it has drifted StayM past
	// N, so the run's recorded end sits somewhere past the real branch, not at it.
	// A cut mints a node where the matcher NOTICED the run leave, not where it
	// left. On re-import of the same file the matcher's tie-break ("nearest, then
	// lowest arc ID") can lock onto that stub and produce another one near it;
	// measured over successive re-imports this converges (each stub lands closer to
	// the true branch until it falls inside EndpointWarpRadiusM and is absorbed),
	// but it is convergence, not idempotence.
	//
	// When enabled, the decline names no node (out.Node stays its zero value),
	// which the leg cut reads as "no node" and drops cleanly: the leg's own piece
	// continues through the boundary uncut.
	//
	// ⚠ DO NOT WIDEN OR NARROW THIS BAND TO "FIX" A FUTURE CASE. Its edges are
	// EndpointWarpRadiusM and MinRunM, both fixed parameters; this branch only
	// decides what happens BETWEEN them.
	//
	// ⚠ THE DECLINE IS CURRENTLY DISABLED (EXPERIMENT). Its justification was that
	// a sub-MinRun stub is permanently unfollowable, because an older matcher
	// reverted any run under the band to OFF. That premise no longer holds: the
	// current follow does not do that, and a sub-MinRun arc has been observed
	// followed byte-exactly on re-import.
	//
	// ⚠ DISABLED, NOT DELETED, AND RE-ENABLING IS NOT ONE CHANGE. The rule may
	// still be right for a reason not yet written down. But [ResolveLegRides]
	// errors on any align boundary with Node == 0, and this decline is the ONLY
	// producer of a nodeless boundary, so re-enabling it also needs ResolveLegRides
	// to accept a nodeless boundary again; otherwise the first stub it declines
	// becomes a failed import.
	if false {
		out.Decline = AlignDeclineStayBandStub
		return out, nil
	}

	// [SplitArcAt] on the canon: it checks the fraction is in (0, 1), computes
	// the split and stores it with Canon.SplitArc, which rewrites only the walks
	// that ride the parent.
	fraction := measure / total
	res, err := SplitArcAt(ctx, canon, parent, fraction)
	if err != nil {
		return AlignBoundary{}, fmt.Errorf(
			"split canon arc %d at fraction %v (measure %.6f m of %.6f m) for a followed run boundary: %w",
			parent, fraction, measure, total, err)
	}

	out.Decline = AlignCutMade
	out.Node = res.Node.ID
	if preferredOverForeign && A1ForeignAbsorbObserverForTests != nil {
		// Every boundary DECLINE 2's lineage preference redirected from an absorb
		// to a cut: the node it refused, how far away that was, and the node the cut
		// minted instead, reported AFTER the mint so an observer can classify the new
		// node by its degree rather than just count it. nil in production.
		A1ForeignAbsorbObserverForTests(CurrentLegForTests, runCanonID, a.Run, a.AtRunEnd, nearby.ID, nearby.DistM, res.Node.ID)
	}
	out.NodeLat, out.NodeLon = res.Node.Lat, res.Node.Lon
	out.Prior = res.Prior
	out.Next = res.Next
	out.ConsumedParentVertex = res.ConsumedParentVertex

	// ⚠ THE INVARIANT, ASSERTED WHERE IT CAN ACTUALLY BREAK. The one-node rule,
	// "no two nodes within EndpointWarpRadiusM of each other", is the whole point
	// of the search above, and this is the ONE place left that could violate it:
	// everything upstream that could have avoided this mint already ran and found
	// nothing, so if something else is still within the radius of the point this
	// cut just chose, the resolve step itself has a bug, not a downstream
	// consequence to chase through later stages. Checked here, once, right after
	// the mint.
	//
	// ⚠ EXCLUDES THE JUST-MINTED NODE. Searching around res.Node's own coordinate
	// without excluding it finds ITSELF at 0 m every time, which would make the
	// assertion unable to fire regardless of the real geometry. A guard that
	// cannot fire is worse than no guard. The leg's terminal claim plays no part:
	// a claimed node within radius is adopted above, so a claim cannot coexist
	// with reaching this mint. Measured across several real-world test areas in
	// both directions: no fires.
	//
	// ⚠ THE PLURAL SEARCH, AND THE REASON IS THE EXEMPTION BELOW, NOT THE
	// ABORT. The abort only needs "is anything there", so the nearest would
	// do. The lineage-preference exemption has to be RECORDED, and what it is
	// exempted from is a PAIRWISE check over the finished network: a mint with two
	// foreign nodes inside the radius makes two violating pairs, so recording only
	// the nearest would exempt one and leave its twin looking like a genuine
	// violation. Same search, same box, same exclude ([NearbyNodes] and
	// [NearestNode] share one body), reduced to nearest here for the message
	// and kept whole for the record.
	nearbyAll, err := NearbyNodes(ctx, canon, res.Node.Lat, res.Node.Lon, EndpointWarpRadiusM, []NodeID{res.Node.ID})
	if err != nil {
		return AlignBoundary{}, fmt.Errorf(
			"SplitCanonAtRunBoundaries: invariant check after minting node %d: %w", res.Node.ID, err)
	}
	var found bool
	nearby = NearbyNode{}
	for _, n := range nearbyAll {
		if !found || n.DistM < nearby.DistM {
			nearby, found = n, true
		}
	}
	// ⚠ THE EXEMPTION IS NARROW. This assertion says "nothing else is within
	// EndpointWarpRadiusM of what I just minted", justified by DECLINE 2 having
	// already adopted anything that was. But DECLINE 2 adopts only a node on the
	// ridden lineage, so a FOREIGN node within the radius is a reachable, intended
	// outcome rather than a resolver defect, and `preferredOverForeign` is true
	// for exactly those mints and no others. Every other way to reach this mint
	// still aborts.
	wouldAbort := found && !preferredOverForeign
	if preferredOverForeign {
		// THE RECORD OF THE LINEAGE-PREFERENCE DECISION, WRITTEN WHERE IT IS MADE.
		// Every node still inside EndpointWarpRadiusM of the node this cut
		// just minted, because DECLINE 2 chose to cut rather than drag canon
		// sideways onto a foreign node. Recorded, not derived: nothing in the
		// finished network distinguishes these pairs from a resolver defect
		// (both are "two nodes 4 m apart"), and the fact that this pair is an
		// intended outcome is known here and nowhere else.
		for _, n := range nearbyAll {
			out.LineageCutConnectorPairs = append(out.LineageCutConnectorPairs, [2]NodeID{res.Node.ID, n.ID})
		}
	}
	if wouldAbort {
		return AlignBoundary{}, fmt.Errorf(
			"SplitCanonAtRunBoundaries: INVARIANT VIOLATED — just minted node %d at (%.9f,%.9f), and node %d already "+
				"sits %.3f m away, inside endpointWarpRadiusM (%v m). The unified search above should have found it and used "+
				"it instead of cutting; the resolve step itself has a defect, not the geometry",
			res.Node.ID, res.Node.Lat, res.Node.Lon, nearby.ID, nearby.DistM, EndpointWarpRadiusM)
	}
	return out, nil
}

var A1ForeignAbsorbObserverForTests func(leg string, runCanonID ArcID, run int, atRunEnd bool, refused NodeID, distM float64, minted NodeID)
