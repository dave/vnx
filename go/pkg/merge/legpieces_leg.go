package merge

// The leg cut: the leg is written as N arcs, cut where the align step cut
// canon, and every interior seam node is SHARED.
//
// The align step put nodes into canon at each ON run's measure boundaries, so
// the stretch a followed leg rides spans a canon arc end to end. On its own
// that would still leave the leg written as ONE arc with two fresh nodes,
// lying on canon and topologically unrelated to it. This file is the other
// half: the leg is cut at the same boundaries and each piece TERMINATES ON the
// node the align step found or minted, so following canon and being joined to
// it become the same fact.
//
// ── WHY THE LEG CUT IS WHAT MAKES RE-IMPORT CONVERGE ────────────────────────
//
// Without the leg cut the align pass cannot settle. Each import leaves N canon
// arcs correctly cut AND one long leg arc spanning all of them, so the next
// import's runs bound that long arc and cut it again, and again: measured on a
// test area, the cut count decays from pass 1 to a steady plateau per pass,
// most of them foreign (cuts of a previous leg's geometry). The plateau is
// structural, not numerical: the align step alone never produces a network
// where every followed stretch is already a whole arc.
//
// ── THE FOUR THINGS THAT MAKE THIS HARDER THAN IT LOOKS ─────────────────────
//
//  1. ⚠ **THE CUT IS AGAINST THE ASSEMBLED POINT LIST, NOT THE COMB.** Around an
//     entry boundary the assembled leg reads [… entryCross, Points[Lo], interior…]
//     and `entryCross` is NOT on canon — it is where the source crossed the band,
//     off the line. Points[Lo] is both the preceding OFF piece's last point and
//     this ON piece's first: ONE point, shared, and that is the shared node.
//     [OnPieceRange] carries those indices for exactly this reason; a cut placed
//     from comb indices instead lands one point out and writes an ON piece that
//     starts off canon.
//
//  2. ⚠ **BOTH SIDES OF EVERY SEAM MUST NAME THE SAME NODE ID.** An OFF piece
//     that mints its own node at a seam breaks the leg's walk in two, and a
//     caller tracing the leg then sees disconnected pieces. This file makes that
//     structural rather than a thing to check: piece k's start node IS piece
//     k-1's returned end node whenever the seam resolved to no canon node, and
//     IS the seam's canon node when it did, so there is no code path that mints
//     twice at one seam.
//
//  3. ⚠ **A LEG TERMINAL MINTS NO NODE, and this is deliberate.** A leg's end
//     is a lat-lon anchor; minting a node there would node every annotated
//     endpoint. It falls out of the seam derivation with no branch: a seam
//     exists only at an INTERIOR point index, and a terminal piece's outer end
//     is index 0 or len-1. [OnPieceRange.TerminalStart] / TerminalEnd are
//     asserted against that rather than consulted, so the two records have to
//     agree.
//
//  4. ⚠ **A PIECE WHOSE TWO ENDS RESOLVE TO ONE NODE IS SPLIT.** A leg that
//     leaves canon at junction J and rejoins at J gives its OFF piece
//     start == end == J, which the store rejects (an arc's two end nodes must
//     differ), failing the whole import. The piece is split at an existing
//     INTERIOR VERTEX instead. No midpoint is invented: a two-point piece in
//     this state is an error, because interpolating one would be writing
//     geometry the source never had.
//
// ── WHAT THE LEG ADOPTS, AND WHY THERE IS NO CHECK ON IT ────────────────────
//
// A reused node's coordinates and elevation WIN: the writer takes the terminal
// vertex from the stored node and ignores points[0]/points[len-1]. The leg
// therefore moves by up to ~4 mm at a shared boundary, because the align step
// places the node by lat/lon-linear interpolation at a measure-derived
// parameter. That adoption is intended, and verifying it against a radius
// would be a new tolerance. Do not add one.

import (
	"context"
	"fmt"
)

// LegJoin is what [CommitLegPieces] knows about ONE join between two
// consecutive written pieces of a leg, once it has decided which arcs those
// pieces actually are: the seam's adopted / absorbed / absorbed-distance
// claims, in the exact walk order the caller's record of the leg needs them
// in. A struct rather than three parallel slices because a ring split appends
// one of these mid-loop with no [LegSeam] behind it at all (see the function
// doc below): one append site per join, not three that could drift apart.
type LegJoin struct {
	Adopted   bool
	Absorbed  bool
	AbsorbedM float64
}

// CommitLegPieces writes the leg as one arc per stretch between seams,
// each terminating on the shared node, and returns the arc ids in WALK
// ORDER, together with one [LegJoin] per JOIN those arcs actually form.
//
// No seams means one arc with two fresh nodes, nothing shared. Most legs are
// that leg.
//
// ⚠ THE THREADING IS THE POINT. `prevEnd` carries the node the previous piece
// actually ended on — minted or adopted — into the next piece's start, so a seam
// with no canon node still ends up with ONE node rather than two coincident ones.
// A seam WITH a canon node names it on both sides, and the assertion below pins
// that the two ways of knowing agree.
//
// ⚠ THE RETURNED `joins` LIST IS BUILT HERE, NOT FROM len(seams). A piece
// whose two ends resolve to one node is split in two by [CommitPiece],
// which makes ONE EXTRA join this piece's own [LegSeam] never described, so
// len(out)-1 can exceed len(seams), and the two lists cannot be zipped
// positionally by a caller after the fact. This loop is the one place that
// knows, join by join, whether it is looking at a seam's own claim or a ring
// split's own join (always the zero LegJoin: the record carries no
// adopted/absorbed provenance either way, whether the split's node was minted
// or adopted), so it is built here, in walk order, and handed to the caller
// complete.
// LegArcRef is one arc a leg's walk lands on, and whether this leg MINTED it.
//
// ⚠ A bare list of arcs is not enough. When every arc a leg walked was one it
// had just written, "the arcs this piece produced" and "the arcs this piece
// owns" were the same list. A piece that REFERENCES canon says neither: it
// produced no arc and owns nothing, and it may walk the arc it found
// BACKWARDS. All three facts have to travel together from the only place that
// knows them.
type LegArcRef struct {
	Arc LegArc
	// Direction is how this leg walks the arc. Always DirForward for an arc
	// the leg minted — a leg writes its own arcs along its own walk — and
	// DirReverse whenever a referenced canon arc is stored the other way
	// round, which is common in practice (about a third of real matches on
	// one test area).
	Direction Direction
	// Created is false for a referenced canon arc. Deletion and node
	// accounting key on this; tracing the leg's path does not.
	Created bool
}

// LegArc is what a leg keeps of an arc its walk lands on: the arc's id and
// end nodes.
type LegArc struct {
	ID          ArcID
	StartNodeID NodeID
	EndNodeID   NodeID
}

// Direction is how a leg walks an arc: DirForward from the arc's start node
// to its end node, DirReverse the other way.
type Direction uint8

const (
	DirForward Direction = 0
	DirReverse Direction = 1
)

// walkStart / walkEnd are the nodes this leg ENTERS and LEAVES the arc at,
// which is the stored arc's own start/end only when the leg walks it forward.
//
// ⚠ READ THESE, NOT Arc.StartNodeID / Arc.EndNodeID, wherever the question is
// about the LEG'S PATH — "do consecutive pieces join", "which node does the
// leg's second piece begin on". A referenced canon arc carries whatever
// orientation its creator gave it, so the arc's own ends answer a different
// question from the walk's.
func (r LegArcRef) WalkStart() NodeID {
	if r.Direction == DirReverse {
		return r.Arc.EndNodeID
	}
	return r.Arc.StartNodeID
}

func (r LegArcRef) WalkEnd() NodeID {
	if r.Direction == DirReverse {
		return r.Arc.StartNodeID
	}
	return r.Arc.EndNodeID
}

// Exported for the reference application's tests; not part of the stable API.
func CommitLegPieces(
	ctx context.Context,
	canon Canon,
	points []Point,
	seams []LegSeam,
	rides *LegRides,
	startTerminal, endTerminal *NodeID,
) ([]LegArcRef, []LegJoin, int, int, [][2]NodeID, *NodeID, []int, int, int, error) {
	// TEST SWITCH, INERT IN PRODUCTION. The claim "referencing the ride is
	// what removes the parallel copies" is only shown by a run in which it
	// does not happen; without this a test could pass because the copies were
	// never there.
	hooks := &stateOf(ctx).hooks
	suppressRides := hooks.LegRideSuppress != nil && hooks.LegRideSuppress()
	if len(points) < 2 {
		return nil, nil, 0, 0, nil, nil, nil, 0, 0, fmt.Errorf("CommitLegPieces: a leg of %d point(s) cannot be committed", len(points))
	}
	last := len(points) - 1
	for i, s := range seams {
		if s.Idx <= 0 || s.Idx >= last {
			return nil, nil, 0, 0, nil, nil, nil, 0, 0, fmt.Errorf(
				"CommitLegPieces: seam %d is at point %d of %d — a seam is interior, and a leg's own ends are lat-lon anchors that mint no node",
				i, s.Idx, last)
		}
	}

	out := make([]LegArcRef, 0, len(seams)+1)
	// multiplicities counts pieces that met MORE THAN ONE identical canon arc
	// — a broken invariant this import survives and records rather than fails
	// on.
	multiplicities := 0
	// Counts pieces written even though an identical canon arc already existed.
	createdDespiteDuplicates := 0
	joins := make([]LegJoin, 0, len(seams)+1)
	ringSplits := 0
	ringMergedTerminals := 0
	var tightPairs [][2]NodeID
	// legRingMergedNode is the node a merged-terminal ring formed on, if any.
	// Can only be non-nil for a single-piece leg (merging the piece's own
	// ends requires BOTH ends unresolved on the SAME CommitPiece call, which
	// for a multi-piece leg never happens: only the outermost piece at each
	// end can have a nil terminal, and they are different calls unless there
	// are no interior seams at all).
	var legRingMergedNode *NodeID
	// legInteriorWarpedBoundaries records every ring split's own interior
	// warp (CommitPiece's own, separate call to WarpLegAtBoundaries, distinct
	// from the boundary warp the caller applies to the whole leg), in the
	// LEG's own index space. Can fire on ANY piece, not just a single-piece
	// leg: unlike legRingMergedNode, a genuine "leaves canon and comes back to
	// an already-existing node" ring split needs no self-merge at all, so a
	// leg with many interior seams can still hit this on one of its pieces.
	var legInteriorWarpedBoundaries []int
	// The leg's FIRST piece starts on the node the endpoint warp moved its
	// terminal onto, when there was one. nil is the genuine case — no node
	// within the warp radius, so the terminal stays a lat-lon anchor and the
	// writer mints.
	prevEnd := startTerminal
	lo := 0
	// Two DIFFERENT declined seams of THIS SAME LEG can land on the exact
	// same coordinate — the leg's own followed geometry touches one canon vertex
	// more than once (a trail crossing revisited), and the align step's
	// existing-node check (DECLINE 1b) cannot see it, because it runs before
	// this leg mints anything. Without this map every revisit mints its OWN
	// node at a coordinate that already has one (measured: one leg, 9 nodes at
	// one lat/lon). Keyed by the coordinate THIS LEG mints at, resolved
	// in-process rather than by a canon search: a same-leg fact, settled
	// without a query.
	mintedAt := map[[2]float64]NodeID{}
	for k := 0; k <= len(seams); k++ {
		hi := last
		// The LAST piece ends on the node the endpoint warp moved the far
		// terminal onto, same rule and same nil meaning.
		endNode := endTerminal
		var declinedCoord [2]float64
		mintingDeclined := false
		if k < len(seams) {
			endNode = nil
			hi = seams[k].Idx
			if seams[k].HasNode {
				n := seams[k].Node
				endNode = &n
			} else if reused, ok := mintedAt[[2]float64{points[hi].Lat, points[hi].Lon}]; ok {
				endNode = &reused
			} else {
				declinedCoord = [2]float64{points[hi].Lat, points[hi].Lon}
				mintingDeclined = true
			}
		}

		startNode := prevEnd
		if k > 0 && seams[k-1].HasNode {
			n := seams[k-1].Node
			// Both records of this seam's node, and they are independent: `prevEnd`
			// is what the writer returned for the previous piece, `n` is what the
			// align step recorded. Disagreement means the seam list and the
			// write walk have drifted out of step.
			if prevEnd != nil && *prevEnd != n {
				return nil, nil, 0, 0, nil, nil, nil, 0, 0, fmt.Errorf(
					"CommitLegPieces: seam %d names canon node %d but the previous piece ended on node %d — the two sides of a seam must be one node",
					k-1, n, *prevEnd)
			}
			startNode = &n
		}

		// THE MATCHER'S VERDICT FOR THIS PIECE, or nil. A ride is offered only
		// when this piece IS the ON stretch the align step resolved — the same
		// point range and the same two nodes — so a seam merge, a symmetric-snap
		// adoption inside the run or a terminal that warped elsewhere all leave
		// the piece without one and it is written as its own arc.
		var ride *LegRide
		if !suppressRides {
			ride = rides.forPiece(len(points), lo, hi, startNode, endNode)
		}
		arcs, endID, ringSplit, ringSplitMinted, ringNodeMinted, tightPair, ringMergedNode, midWarpedIdx, multiple, despiteDup, err := CommitPiece(ctx, canon, points[lo:hi+1], startNode, endNode, ride)
		if multiple {
			multiplicities++
		}
		if despiteDup {
			createdDespiteDuplicates++
		}
		if err != nil {
			// ⚠ `endNode`, NOT nil. Describe the end side as it actually
			// resolved: a description that contradicts the error it decorates
			// costs the next reader more than no description would.
			return nil, nil, 0, 0, nil, nil, nil, 0, 0, fmt.Errorf("leg piece %d of %d (points %d..%d, start %s, end %s): %w",
				k+1, len(seams)+1, lo, hi, seamSideName(seams, k-1, prevEnd), seamSideName(seams, k, endNode), err)
		}
		if ringMergedNode != nil {
			legRingMergedNode = ringMergedNode
		}
		if midWarpedIdx != nil {
			// lo + midWarpedIdx converts CommitPiece's own PIECE-relative
			// index into the LEG's index space — `points[lo:hi+1]` is
			// exactly the slice CommitPiece received as `pts`, so its own
			// index 0 IS points[lo]. No remapping is needed (a ring split
			// cuts at an EXISTING vertex, never inserts a new coordinate),
			// and this holds by construction: `lo` is this loop's own
			// slice offset, not re-derived.
			legInteriorWarpedBoundaries = append(legInteriorWarpedBoundaries, lo+*midWarpedIdx)
		}
		if ringSplit {
			// The join a ring split makes BETWEEN this piece's two halves, before
			// this piece's own seam, is real leg topology regardless of what
			// happened to the node there — record it unconditionally.
			joins = append(joins, LegJoin{})
			// ringSplits, unlike ringSplit, is NOT every branch-take: it counts
			// nodes this mechanism MINTED. A ring split can also ADOPT an
			// existing nearby node under the one-node rule, contributing zero
			// new nodes, so counting every branch-take would overcount the
			// import's node total by one per adoption.
			if ringSplitMinted {
				ringSplits++
			}
			// ringMergedTerminals is a THIRD, independent count: the ring's own
			// shared node (not the split one) being a genuine fresh mint
			// because the piece's two unresolved ends were merged.
			if ringNodeMinted {
				ringMergedTerminals++
			}
			if tightPair != nil {
				tightPairs = append(tightPairs, *tightPair)
			}
		}
		if mintingDeclined {
			mintedAt[declinedCoord] = endID
		}
		// A REFERENCED RIDE CAN BE SEVERAL ARCS, AND EACH JUNCTION BETWEEN THEM
		// IS A REAL JOIN. The leg walks canon through a node the align step cut
		// (a backtrack run of this same leg cutting inside this run's interval),
		// and the caller's record of the leg requires exactly one join fewer than
		// arcs. The ring split above is the same shape and the same zero LegJoin:
		// the record carries no adopted/absorbed provenance either way, because
		// the leg neither minted this node for itself nor moved to reach it — it
		// is on canon, and the node is canon's.
		//
		// ⚠ ONLY FOR A REFERENCED PIECE. A ring split also returns two arcs
		// for one piece and the branch above already appends ITS join; counting
		// here as well would report one join too many for every ring.
		if ride != nil {
			for range arcs[1:] {
				joins = append(joins, LegJoin{})
			}
		}
		out = append(out, arcs...)
		if k < len(seams) {
			// The join between this piece and the next — the [LegSeam] this piece
			// was cut FOR, in the same walk order the arcs above were written.
			joins = append(joins, LegJoin{Adopted: seams[k].Adopted, Absorbed: seams[k].Absorbed, AbsorbedM: seams[k].AbsorbedM})
		}
		prevEnd = &endID
		lo = hi
	}
	return out, joins, ringSplits, ringMergedTerminals, tightPairs, legRingMergedNode, legInteriorWarpedBoundaries, multiplicities, createdDespiteDuplicates, nil
}

// CommitPiece writes ONE piece, splitting it in two when both of its ends
// resolve to the same node. It returns the arc ids it wrote and the
// node the piece ENDED on, which the caller threads into the next piece.
//
// The two bools are DIFFERENT questions: ringSplit says whether the ring-split
// branch executed at all — a real joint in leg topology either way, which
// callers must record (a join) regardless of what happened to the node there
// — and ringSplitMinted says whether that joint's node was a GENUINE FRESH
// MINT rather than an adoption of an existing nearby node. An adoption
// satisfies the joint with zero new nodes, so a caller that counts every
// ringSplit as a mint overcounts the node total by one per adoption.
//
// tightPair is non-nil only for a genuine mint whose excluded node WAS within
// EndpointWarpRadiusM: the shape a check of the one-node rule must exempt.
// Computed from coordinates already in hand (no extra lookup).
//
// Exported for the reference application's tests; not part of the stable API.
func CommitPiece(
	ctx context.Context,
	canon Canon,
	pts []Point,
	startNode, endNode *NodeID,
	ride *LegRide,
) ([]LegArcRef, NodeID, bool, bool, bool, *[2]NodeID, *NodeID, *int, bool, bool, error) {
	st := stateOf(ctx)
	// A route whose two ends are close together is a loop: merge its ends
	// into one node and split the loop as a ring. Without this, a leg's own
	// two ends could sit within EndpointWarpRadiusM of EACH OTHER and still
	// mint two separate nodes for what the source drew as one point.
	//
	// The canon rejects an arc whose start and end node are the same before
	// any write, so this can't simply resolve both ends to the SAME id and
	// fall into the ring-split branch below the way a genuine leave-and-return
	// junction already does — the merge has to happen BEFORE the writer.
	//
	// Scope: only when NEITHER end has been resolved by anything upstream
	// (both nil). One end already sitting on an existing node is a different
	// question this does not cover.
	//
	// ⚠ THE CHORD ALONE IS NOT ENOUGH. A short, nearly-straight spur has its
	// own two ends within radius of each other WITHOUT ever having left the
	// vicinity — there is no loop to split, just a line whose endpoints happen
	// to be close. Merging it anyway invents a there-and-back the source never
	// drew: the writer discards the leg's own final vertex in favour of the
	// reused node's coordinates, so the written geometry becomes A → mid → A
	// on a straight 12 m track. So the gate also requires that the leg
	// actually left the radius ([legDepartsRadius], the same judgement the
	// fold-collapse path makes). Real loops measured on test areas (2-26 km)
	// all pass this gate.
	mergeOwnEnds := startNode == nil && endNode == nil && len(pts) >= 3 &&
		HaversineMetres(pts[0].Lat, pts[0].Lon, pts[len(pts)-1].Lat, pts[len(pts)-1].Lon) <= EndpointWarpRadiusM &&
		legDepartsRadius(pts, 0, len(pts)-1, EndpointWarpRadiusM)

	if !mergeOwnEnds && (startNode == nil || endNode == nil || *startNode != *endNode) {
		// The duplicate-arc check: is there already a canon arc between these
		// two nodes carrying the identical vertex sequence? It is run
		// unconditionally rather than behind the observer, so its cost is
		// always part of the measured import.
		dupMatches, err := FindDuplicateCanonArcs(ctx, canon, pts, startNode, endNode)
		if err != nil {
			return nil, 0, false, false, false, nil, nil, nil, false, false, err
		}
		if st.hooks.DuplicateArc != nil {
			st.hooks.DuplicateArc(dupMatches, startNode, endNode)
		}
		// Whether this piece is written despite an identical arc. Decided HERE,
		// before the ride branch, because this is where the detector's answer
		// and the verdict are both in hand; it is only reported when the piece
		// goes on to be created. The interior-pip exemption is stated the way
		// the writer can decide it: len(pts) == 2 is a bare edge between two
		// nodes, and two bare edges between one node pair are equal by
		// definition rather than by having copied anything.
		createdDespiteDuplicate := len(dupMatches) > 0 && len(pts) > 2
		// The multiplicity anomaly, reported from the ONE place the detector's
		// answer exists. Like createdDespiteDuplicate it is decided here and
		// reported only when the piece goes on to be CREATED — a referenced
		// piece has nothing to choose between (see the ride branch below).
		createdMetSeveralDuplicates := len(dupMatches) > 1
		// REFERENCE BECAUSE THE MATCHER SAID SO. The decision to reuse canon
		// comes from the follow's verdict, not from comparing bytes.
		//
		// The piece IS the ON stretch of a run, and the align step's own record
		// says which canon arcs now cover it. The leg steps onto those arcs and
		// writes nothing — no geometry is assembled for it, no bytes are
		// compared. The gap between where the solver ended the run and the node
		// the boundary was absorbed onto does not need to be drawn: the
		// referenced arc IS the drawing, with canon's own vertices.
		//
		// ⚠ THE PIECE'S OWN POINTS ARE NOT CONSULTED, AND THAT INCLUDES THE
		// TAPER. The boundary warp built a taper (or the assembler a chord) to
		// carry this piece the last few metres onto the absorbed node; it is
		// discarded with the piece. Writing that taper would produce a long
		// parallel copy of canon differing from it only in the metres either
		// side of a join.
		//
		// ⚠ NO NODE IS SAVED HERE, ONLY AN ARC. Both ends had already resolved
		// to existing nodes before this branch — LegRides.forPiece requires
		// it — so there was never a mint to suppress. A re-import that still
		// gains NODES is gaining them from canon CUTS, which this does not
		// touch.
		//
		// ⚠ BOTH MODES, NOT ANNOTATED ONLY. Bare and Annotated imports must
		// write byte-identical geometry, so a de-duplication that ran in one
		// mode and not the other would break that invariant. Bare mode's own
		// rule — write nothing and RECORD nothing for a wholly-coincident leg
		// — is about the record, not the geometry.
		if ride != nil {
			refs, err := referenceRideArcs(ctx, canon, ride, *startNode, *endNode)
			if err != nil {
				return nil, 0, false, false, false, nil, nil, nil, false, false, err
			}
			// `multiple` is deliberately FALSE for a referenced piece. It asks
			// "this piece met more than one identical canon arc, so the writer
			// had to choose" — the ride NAMES the arcs, so there is nothing to
			// choose between and the question does not arise.
			return refs, *endNode, false, false, false, nil, nil, nil, false, false, nil
		}
		arc, err := addFrozenArcToNetwork(ctx, canon, pts, startNode, endNode)
		if err != nil {
			return nil, 0, false, false, false, nil, nil, nil, false, false, err
		}
		return []LegArcRef{{Arc: arc, Direction: DirForward, Created: true}}, arc.EndNodeID, false, false, false, nil, nil, nil, createdMetSeveralDuplicates, createdDespiteDuplicate, nil
	}

	// The ring split. The leg left canon at this node and came back to it (or,
	// under mergeOwnEnds above, its own two unresolved ends sit close enough
	// to BECOME one node), so the piece is a closed ring on one node. The
	// canon forbids an arc from a node to itself, so the ring is cut at an
	// existing interior vertex instead, which mints one node and changes no
	// geometry (when nothing nearby exists to share — see below when
	// something does).
	//
	// The split lands at the point furthest from the ring's own anchor (the
	// out-and-back's actual turn), not at measure-halfway, which only
	// coincides with the turn when the out and back legs are symmetric. See
	// furthestVertex's own doc.
	mid, err := furthestVertex(pts)
	if err != nil {
		return nil, 0, false, false, false, nil, nil, nil, false, false, err
	}

	// The one-node rule: before minting at the split point, the SAME nearby-node
	// lookup every other node-creation site takes. It is a REAL exclusion, not a
	// post-hoc discard: the ring's own node (*startNode, when it already exists
	// — when the ends are being merged there is nothing to exclude yet, since
	// the merged node isn't minted until the write below) is removed from the
	// candidate set BEFORE picking nearest, so a genuinely different, adoptable
	// node inside the radius is still found even when the piece's own end
	// happens to be the single closest thing. The exclusion exists because the
	// split's whole purpose is to give this self-looping piece a DISTINCT third
	// node; resolving onto its own end would recreate the exact self-loop the
	// canon rejects. Only when NOTHING ELSE is within radius does this fall
	// through to mint fresh.
	var midExclude []NodeID
	if startNode != nil {
		midExclude = []NodeID{*startNode}
	}
	var target *Node
	nearby, found, err := NearestNode(
		ctx, canon, pts[mid].Lat, pts[mid].Lon, EndpointWarpRadiusM, midExclude)
	if err != nil {
		return nil, 0, false, false, false, nil, nil, nil, false, false, fmt.Errorf("existing-node lookup for a ring-split's own halfway point: %w", err)
	}
	if found {
		n, _, err := canon.Node(ctx, nearby.ID)
		if err != nil {
			return nil, 0, false, false, false, nil, nil, nil, false, false, fmt.Errorf("load node %d found within %v m of a ring-split's halfway point: %w", nearby.ID, EndpointWarpRadiusM, err)
		}
		target = &n
	}

	warped := pts
	var midNode *NodeID
	// midWarpedIdx records the ring split's own interior warp — a separate
	// call to WarpLegAtBoundaries this function makes on its own, distinct
	// from the boundary warp the caller applies to the whole leg.
	// PIECE-RELATIVE here (an index into `pts`, this call's own argument);
	// the caller adds its own `lo` offset to convert to the LEG's index
	// space — see CommitLegPieces's own comment on why that conversion
	// is safe with no remapping needed. nil whenever this warp never ran
	// (target == nil — nothing within radius, mid stays an ordinary fresh
	// mint).
	var midWarpedIdx *int
	if target != nil {
		// The one-node rule's OTHER half: resolving mid to a node up to 15 m
		// away without also tapering the geometry toward it would leave a
		// discontinuity right at the split — the same defect the align step's
		// absorbed interior boundaries would have without WarpLegAtBoundaries.
		// Treat mid as an interior boundary of the WHOLE undivided piece — not
		// two independently-tapered halves, which could leave a kink exactly
		// where they meet.
		//
		// There is no gap-collapse, so mid stays put — nothing remaps it — and
		// can fold if the ring is tight enough. Accepted, not prevented.
		boundaries := []WarpBoundaryTarget{{Idx: mid, Target: WarpTarget{Lat: target.Lat, Lon: target.Lon, NodeID: target.ID}}}
		warpedPts, err := WarpLegAtBoundaries(pts, boundaries)
		if err != nil {
			return nil, 0, false, false, false, nil, nil, nil, false, false, fmt.Errorf("warp a ring-split piece toward node %d: %w", target.ID, err)
		}
		warped = warpedPts
		id := target.ID
		midNode = &id
		midIdx := mid
		midWarpedIdx = &midIdx
	}

	ringDesc := "a self-referencing piece"
	if startNode != nil {
		ringDesc = fmt.Sprintf("a piece that begins and ends on node %d", *startNode)
	}
	first, err := addFrozenArcToNetwork(ctx, canon, warped[:mid+1], startNode, midNode)
	if err != nil {
		return nil, 0, false, false, false, nil, nil, nil, false, false, fmt.Errorf("first half of %s: %w", ringDesc, err)
	}
	half := first.EndNodeID

	// The ring's own shared node. An already-resolved junction (startNode
	// non-nil) keeps its identity unchanged. When the ends are being merged
	// (startNode nil), `first` just minted it fresh at warped[0] via the SAME
	// nil-pointer path the ordinary branch above already uses for a plain
	// terminal — no new mint primitive, no second lookup — and it is reused
	// for BOTH ends below instead of letting the second half mint its own,
	// separate node at the piece's true end.
	ringNode := startNode
	ringNodeMinted := ringNode == nil
	if ringNode == nil {
		id := first.StartNodeID
		ringNode = &id
	}

	// Only a GENUINE mint (target == nil) is this call's own doing — an
	// adoption reuses a node some OTHER site already minted and already
	// reported, so recording it again here under a "ring-split" label would
	// overwrite that node's true provenance with a false one.
	// The GENUINE tight-ring shape, computed for free from coordinates already
	// in hand: a fresh mint (target == nil) whose excluded ring node was
	// itself within radius of the split point. pts[0], not warped[0] — for a
	// genuine mint nothing warped (warped == pts, unconditionally, in that
	// branch), so they are the same point, but pts makes the "no warp
	// happened here" fact visible without re-deriving it. ONE computation
	// below, TWO reporters: the same `d <= EndpointWarpRadiusM` result is
	// returned as tightPair for the caller's result AND fired through
	// [TestHooks].RingSplitExclusion for test diagnostics, so there are
	// not two sources that could drift apart.
	var tightPair *[2]NodeID
	if target == nil {
		if d := HaversineMetres(warped[mid].Lat, warped[mid].Lon, pts[0].Lat, pts[0].Lon); d <= EndpointWarpRadiusM {
			tightPair = &[2]NodeID{half, *ringNode}
			if st.hooks.RingSplitExclusion != nil {
				st.hooks.RingSplitExclusion(st.leg, half, *ringNode, d)
			}
		}
	}
	second, err := addFrozenArcToNetwork(ctx, canon, warped[mid:], &half, ringNode)
	if err != nil {
		return nil, 0, false, false, false, nil, nil, nil, false, false, fmt.Errorf("second half of %s: %w", ringDesc, err)
	}
	// The node `first` minted or adopted at the cut is the one no other counter sees.
	// ringSplit is always true here (the branch executed); ringSplitMinted is
	// only true for a genuine mint (target == nil) — see the doc comment above
	// for why a caller must not conflate the two. ringNodeMinted is a THIRD,
	// independent fact — whether the RING'S OWN node (not the split one) was
	// freshly minted this call, true only when the ends were merged
	// (startNode was nil on entry).
	//
	// ringMergedNode surfaces that same fact to the caller by id, not just by
	// count. nil whenever ringNodeMinted is false (an already-resolved
	// junction, which the caller already records as an ordinary adoption):
	// recording it there too would claim a self-merge for a leg that made an
	// ordinary adoption.
	var ringMergedNode *NodeID
	if ringNodeMinted {
		ringMergedNode = ringNode
	}
	// ⚠ THE RING-SPLIT HALVES ARE NOT DE-DUPLICATED. Both are arcs this leg
	// just minted, at nodes it just resolved, so they are Created and forward
	// like any other write. Referencing is not attempted on this branch: the
	// halves' shared middle node does not exist until `first` writes it, so
	// there is nothing for the detector's "an arc between the same two nodes"
	// question to have found beforehand. If a ring's halves ever do need
	// de-duplicating, it is a separate question, not an omission here.
	return []LegArcRef{
		{Arc: first, Direction: DirForward, Created: true},
		{Arc: second, Direction: DirForward, Created: true},
	}, second.EndNodeID, true, target == nil, ringNodeMinted, tightPair, ringMergedNode, midWarpedIdx, false, false, nil
}

// addFrozenArcToNetwork adds one frozen arc to the canon: [PrepareArc]
// resolves the ends and computes the interior, stats and bounds, and
// [Canon.AddArc] stores it (the minted ends, the arc and its derived index
// data; any bounds catch-up is the host's).
//
// `startNodeID` / `endNodeID` are PrepareArc's contract, passed straight
// through: nil MINTS a fresh node at that end, an id makes the arc terminate
// on that node and adopt its coordinates and elevation. Every caller is
// [CommitLegPieces], which passes nil at a leg terminal (a leg's end is a
// lat-lon anchor) and a seam node everywhere else. A leg with no seams passes
// two nils.
//
// Caller has already guaranteed len(pts) >= 2.
func addFrozenArcToNetwork(
	ctx context.Context,
	canon Canon,
	pts []Point,
	startNodeID, endNodeID *NodeID,
) (LegArc, error) {
	a, err := PrepareArc(ctx, canon, pts, startNodeID, endNodeID)
	if err != nil {
		return LegArc{}, fmt.Errorf("add frozen arc: %w", err)
	}
	arc, err := canon.AddArc(ctx, a)
	if err != nil {
		return LegArc{}, fmt.Errorf("add frozen arc: %w", err)
	}
	return LegArc{ID: arc.ID, StartNodeID: arc.StartNodeID, EndNodeID: arc.EndNodeID}, nil
}
