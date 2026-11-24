package merge

import (
	"context"
	"fmt"
	"math"
)

// legSeamsFromAlign derives the leg's interior cut points from where its ON
// pieces sit in the assembled points, and resolves each to the canon node the
// align step left there.
//
// The boundaries are looked up by (run, which end), the identity the align step
// records, and NOT by matching measures, which would be a float equality on a
// quantity that has been through a split's measure->fraction->measure round trip.
// zeroArcGap reports whether leg points i and j are the SAME PHYSICAL POINT:
// no distance along the leg between them, however many indices apart they are.
//
// It exists because a leg can carry a zero-length edge (a stationary GPS
// sample, a duplicated vertex) and a boundary can land on each end of it. Those
// are two indices and ONE place, and every rule about "one point cannot join
// the network at two nodes" is about the place, not the index.
//
// ⚠ EXACT ZERO, NOT A TOLERANCE, AND THE SCOPE IS DELIBERATE. This admits only
// the case that would otherwise abort the import: the boundary warp's taper
// walks compute s as an arc distance from the boundary, and their s == 0
// branches assign by that distance rather than by index, so it takes a genuine
// zero for one boundary to overwrite another's index. Two boundaries a
// millimetre apart taper normally and merely fold, which is an accepted
// outcome, not a defect. Widening this to a tolerance would start merging
// seams that are not the same point.
//
// The sum, rather than a straight-line chord between the two ends, because the
// leg's own measure is what the taper walks: a hairpin returning to its start
// has a zero chord and a real arc, and those must not merge.
func zeroArcGap(pts []Point, i, j int) bool {
	if i > j {
		i, j = j, i
	}
	if i < 0 || j >= len(pts) {
		return false
	}
	for k := i + 1; k <= j; k++ {
		if HaversineMetres(pts[k-1].Lat, pts[k-1].Lon, pts[k].Lat, pts[k].Lon) != 0 {
			return false
		}
	}
	return true
}

// boundaryKey identifies the ONE boundary the align step records per end of
// per ON run. It is that identity, NOT a measure, which has been through a
// split's measure->fraction->measure round trip and cannot be compared with ==.
type boundaryKey struct {
	Run   int
	AtEnd bool
}

func legSeamsFromAlign(ctx context.Context, asm assembledLeg, rep AlignReport, startTerminal, endTerminal *NodeID) ([]legSeam, error) {
	st := stateOf(ctx)
	if len(asm.OnPieces) == 0 {
		return nil, nil
	}

	byRunEnd := make(map[boundaryKey]AlignBoundary, len(rep.Boundaries))
	for _, b := range rep.Boundaries {
		k := boundaryKey{Run: b.Run, AtEnd: b.AtRunEnd}
		if prev, dup := byRunEnd[k]; dup {
			return nil, fmt.Errorf(
				"seams from the align step: run %d's %s boundary was resolved twice (against arcs %d and %d) — a run has exactly one measure per end",
				b.Run, runEndName(b.AtRunEnd), prev.Parent, b.Parent)
		}
		byRunEnd[k] = b
	}

	// mintedThisLeg is every node THIS LEG's OWN align pass minted, across ALL
	// of its boundaries, not just the one a given seam is about to read. See
	// [legSeam.Adopted]: the align step cuts a leg's boundaries in its OWN
	// order (descending measure, tied by run/end), which is not leg-walk order,
	// so a LATER boundary of the same pass can decline against a node an
	// EARLIER boundary of the same pass just minted ("the second of the pair
	// declines"), and the align step's coordinate search is not scoped to
	// exclude this leg's own writes. Read alone, that later boundary's Decline
	// says "adopted" for a node this same leg minted a moment before. This set
	// closes the gap: a node is adopted only if NOTHING in this leg's own align
	// pass minted it.
	mintedThisLeg := make(map[NodeID]bool, rep.Cuts)
	for _, b := range rep.Boundaries {
		if b.Decline == AlignCutMade {
			mintedThisLeg[b.Node] = true
		}
	}

	last := len(asm.Points) - 1
	// Seams accumulate in ascending point order because OnPieces are in leg order
	// and each contributes Lo before Hi; the merge below relies on that.
	var seams []legSeam
	add := func(idx int, runIdx int, atEnd bool) error {
		b, ok := byRunEnd[boundaryKey{Run: runIdx, AtEnd: atEnd}]
		if !ok {
			return fmt.Errorf(
				"seams from the align step: no align boundary for run %d's %s, which the ON piece at point %d needs a node for",
				runIdx, runEndName(atEnd), idx)
		}
		node, hasNode := b.SeamNode()
		// ⛔ A NODELESS BOUNDARY IS NOT A SEAM, and cutting the leg there
		// produces a visible extra node. No align outcome produces one today
		// (each names a node); this keeps the leg cut correct if one returns.
		//
		// The align step cuts canon at a run boundary so the stretch the leg
		// rides spans a canon arc end to end, and the leg cut splits the leg at
		// the same place so both sides share the node. When the align step
		// declines without naming a node, canon is not cut, so there is no canon arc to span and no node to share.
		// Cutting the leg anyway buys nothing and costs a node: the leg mints its
		// own, typically 15 m or so from canon's, which reads as one place on the
		// map but is two nodes in the store.
		//
		// So the seam is DROPPED. The leg's piece simply continues through the
		// boundary, which is the honest answer, because at a declined boundary
		// the leg cut's premise ("this piece spans a canon arc") is false.
		//
		// ⚠ IT MOVES NO GEOMETRY. The points are unchanged and in the same order;
		// only the grouping into arcs changes, and one fewer node is minted.
		// That is why this is the answer and "resolve the boundary to canon's
		// nearby node" is not: the leg ADOPTS a seam node's coordinate, so naming
		// a node tens of metres away would jerk the leg sideways.
		//
		// An ON<->ON boundary where the OTHER run names a real node still gets
		// it: the merge below runs first and settles exactly that case.
		if !hasNode {
			return nil
		}
		// A node THIS LEG'S OWN align pass minted is never adopted, whichever
		// boundary record says so; see [legSeam.Adopted] and mintedThisLeg above.
		adopted := b.Decline != AlignCutMade && !mintedThisLeg[node]
		absorbed := b.Decline == AlignDeclineAbsorbedToNode
		absorbedM := 0.0
		if absorbed {
			absorbedM = b.AbsorbedM
		}
		if n := len(seams); n > 0 && (seams[n-1].Idx == idx || zeroArcGap(asm.Points, seams[n-1].Idx, idx)) {
			// An ON<->ON seam: the assembler dedups the shared point, so the
			// previous piece's Hi and this piece's Lo are the same index. Two
			// claims, and they must not disagree: one naming a node and the other
			// naming none is a stub decline meeting a real junction, which the
			// junction settles.
			//
			// ⚠ OR THE TWO INDICES ARE ONE PHYSICAL POINT. [zeroArcGap] widens
			// this from "the same index" to "no distance between them", which is
			// the same claim about the same place written two ways. Nothing below
			// changes: every arm here already reasons about ONE point claimed
			// twice, and two indices with zero metres between them ARE one point.
			//
			// Without it a zero-length leg edge carrying a boundary at each end
			// would reach the boundary warp as two live targets, whose taper zones
			// both have s == 0 at the other's index; the later one's exact
			// assignment would overwrite the earlier one and the join-invariant
			// post-condition would abort the import, losing unrelated legs in the
			// same batch.
			prev := &seams[n-1]
			switch {
			case prev.HasNode && hasNode && prev.Node != node:
				// Two boundaries at ONE leg point (the assembler already dedups
				// them to one idx) each independently settled on a DIFFERENT
				// nearby node. The align step re-asks an absorbed boundary's
				// search once the whole pass has finished cutting, but only
				// promotes to an EXACT (0 m) match, deliberately; this is the
				// residual case where neither side's later-found match was an
				// exact improvement.
				//
				// They cannot both be right: this is one physical point, not
				// two. RESOLUTION: whichever candidate sits closer, by TRUE
				// straight-line distance, to the leg's own raw point at this
				// index (asm.Points[idx], the leg's followed-but-not-yet-warped
				// coordinate, exactly what [legSeam.Idx] already means) is the
				// better answer for that point. This generalises the align
				// step's "an exact match beats an absorb" to "closer beats
				// farther" rather than adding a new rule, and it is the SAME
				// metric the one-node rule's node search picks nearest by
				// (haversine chord). It is not AbsorbedM, which is the
				// ALONG-CANON measure of a taper reach and only a proxy for this.
				pt := asm.Points[idx]
				prevD := HaversineMetres(pt.Lat, pt.Lon, prev.NodeLat, prev.NodeLon)
				thisD := HaversineMetres(pt.Lat, pt.Lon, b.NodeLat, b.NodeLon)
				if st.hooks.LegSeamConflict != nil {
					st.hooks.LegSeamConflict(st.leg, idx, prev.Node, node, prevD, thisD)
				}
				// Logged in production (through LegOptions.Logf), not only
				// through the test hook above, which is nil on every real
				// import: the tie-break is expected
				// to fire rarely, and that can only be checked if a real import
				// reports it. A debug line rather than a counter, because this is
				// a rare diagnostic event, not a term in the node-count identity.
				st.debugf(
					"seams from the align step: leg %q point %d resolved between canon node %d (%.3fm) and node %d (%.3fm) by distance, not conflict — %.3fm chosen",
					st.leg, idx, prev.Node, prevD, node, thisD, math.Min(prevD, thisD))
				switch {
				case thisD < prevD:
					prev.Node, prev.Adopted = node, adopted
					prev.Absorbed, prev.AbsorbedM = absorbed, absorbedM
					prev.NodeLat, prev.NodeLon = b.NodeLat, b.NodeLon
				case thisD == prevD:
					// A GENUINE TIE. It looks like a float coincidence, but a
					// zero-length leg edge puts two boundaries at ONE physical
					// point, where equal distances become the ordinary case.
					//
					// LOWEST NODE ID. It is not a better answer than the other
					// candidate (by construction there is none: both are the same
					// distance from the same point); it is a deterministic one.
					// Node IDs are assigned by the store and are total and
					// immutable once minted, so this does not depend on map
					// iteration order, on the order the searches returned, or on
					// which boundary the resolver happened to reach first.
					// ⚠ BOTH IDS CAPTURED BEFORE THE SWAP. The message must name
					// both candidates, and the branch below may assign
					// `prev.Node = node`; reading `prev.Node` after it would
					// report the same node twice.
					prevNode := prev.Node
					lost, kept := node, prev.Node
					if node < prev.Node {
						lost, kept = prev.Node, node
						prev.Node, prev.Adopted = node, adopted
						prev.Absorbed, prev.AbsorbedM = absorbed, absorbedM
						prev.NodeLat, prev.NodeLon = b.NodeLat, b.NodeLon
					}
					st.debugf(
						"seams from the align step: leg %q point %d is equidistant (%.3fm) from canon nodes %d and %d — kept the lower id %d, dropped %d",
						st.leg, idx, thisD, prevNode, node, kept, lost)
				}
				// else prev is already the closer candidate: prev wins, nothing to do.
			case hasNode && !prev.HasNode:
				// The flag travels with the node that wins, not the previous seam's
				// value — the previous claim contributed nothing, and this one is the
				// only one that says anything about adoption or absorption.
				prev.Node, prev.HasNode, prev.Adopted = node, true, adopted
				prev.Absorbed, prev.AbsorbedM = absorbed, absorbedM
				prev.NodeLat, prev.NodeLon = b.NodeLat, b.NodeLon
			}
			return nil
		}
		if n := len(seams); n > 0 && seams[n-1].Idx >= idx {
			return fmt.Errorf(
				"seams from the align step: seam at point %d follows a seam at point %d — seams run in leg order",
				idx, seams[n-1].Idx)
		}
		seams = append(seams, legSeam{
			Idx: idx, Node: node, HasNode: hasNode, Adopted: adopted,
			Absorbed: absorbed, AbsorbedM: absorbedM,
			NodeLat: b.NodeLat, NodeLon: b.NodeLon,
		})
		return nil
	}

	for i, p := range asm.OnPieces {
		// Asserted rather than consulted: the piece assembly prepends the leg's
		// verbatim anchor when the piece starts the leg off the line, so a
		// terminal piece's outer end is the leg's own end, and the seam test
		// below (an INTERIOR index) must already have excluded it. If these ever
		// disagree the leg is about to mint a node on its terminal anchor.
		if p.TerminalStart && p.Lo != 0 {
			return nil, fmt.Errorf(
				"seams from the align step: ON piece %d is flagged terminal at its start but begins at point %d, not 0", i, p.Lo)
		}
		if p.TerminalEnd && p.Hi != last {
			return nil, fmt.Errorf(
				"seams from the align step: ON piece %d is flagged terminal at its end but ends at point %d, not %d", i, p.Hi, last)
		}
		if p.Lo > 0 {
			if err := add(p.Lo, p.Run, false); err != nil {
				return nil, err
			}
		}
		if p.Hi < last {
			if err := add(p.Hi, p.Run, true); err != nil {
				return nil, err
			}
		}
	}
	// No terminal-stub floor here, deliberately. Dropping a seam whose terminal
	// piece is under the host's minimum ride length (0.5 m) was once needed to protect
	// a later terminal trim that no longer exists. Measured over several test
	// sweeps, removing the floor changed no failure class in kind or in count;
	// its only effect was on those short terminal pieces, while sub-0.5 m
	// INTERIOR pieces commit either way. Keeping a tolerance whose cause is gone
	// would leave a constant nobody could later justify.
	return collapseSameNodeSeams(seams, asm.Points, startTerminal, endTerminal), nil
}

// collapseSameNodeSeams applies every same-node emptiness collapse in the
// order the same-node split rule requires: empty, fold, then terminal. See
// collapseEmptySameNodeSeams / collapseFoldedSameNodeSeams /
// collapseTerminalSameNodeSeams for what each removes and why.
//
// ⚠ CALLED TWICE, DELIBERATELY. legSeamsFromAlign calls this once against the
// leg's FOLLOWED points, before any interior boundary is warped, which is
// correct for every seam whose geometry the boundary warp leaves untouched.
// The boundary warp can then shrink a piece that WAS a genuine ring (real
// extent, correctly kept as two seams here) down to an EMPTY one: every
// interior vertex between two same-node seams collapsed away, leaving a
// same-node piece with no extent. Nothing downstream re-asks the question, so
// the caller calls this AGAIN against the WARPED points once the warp has
// returned. Without the second call that piece reaches the same-node split
// rule with no interior vertex and aborts the whole import (seen on a real
// test area).
//
// Idempotent on a leg the warp did not touch: pieceHasExtent's verdict cannot
// become MORE true by shrinking a gap, only less, so a second pass over
// geometry the warp left alone finds nothing new to collapse.
func collapseSameNodeSeams(seams []legSeam, pts []Point, startTerminal, endTerminal *NodeID) []legSeam {
	last := len(pts) - 1
	return collapseTerminalSameNodeSeams(
		collapseFoldedSameNodeSeams(collapseEmptySameNodeSeams(seams, pts), pts, last, startTerminal, endTerminal),
		pts, last, startTerminal, endTerminal,
	)
}

// collapseFoldedSameNodeSeams is [collapseEmptySameNodeSeams] generalised
// from "no extent between them" to "the leg's own source never got farther
// than EndpointWarpRadiusM from the node between them": it removes folds at
// INTERIOR boundaries.
//
// The align step stops an absorb reaching BACKWARD onto the node the leg's OWN
// start/end terminal already claims. It is blind to the same absorb reaching
// onto a node an EARLIER INTERIOR seam of the SAME leg already claims: the
// align step resolves one leg's boundaries per canon arc in DESCENDING MEASURE
// order, not leg-walk order, so a walk-EARLIER boundary can absorb onto a node
// a walk-LATER boundary is about to MINT, moments before it in that same pass.
// Nothing in the align step can see that these two boundaries are adjacent in
// the LEG's own walk; only this function can, because `seams` is already built
// in leg-walk (ascending point) order by the time it runs.
//
// ⚠ THE COMPARISON IS AGAINST THE MOST RECENT SURVIVING CLAIM ON THAT NODE,
// NOT ONLY THE IMMEDIATELY PRECEDING SEAM, unlike collapseEmptySameNodeSeams
// above, whose EMPTY test only ever fires between two ADJACENT same-node
// seams (nothing between them to be non-empty). A fold's own OFF run can
// carry a genuine, unrelated junction between the two same-node claims
// (e.g. `…40 39 71 39 38…`, node 71 sitting between the two claims on node
// 39); comparing only to `out[n-1]` would see 71 there and wave the second 39
// through as a different-node transition. The departure check does not care
// how many other seams sit in the range; it reads the leg's own points across
// the WHOLE stretch since the node's last surviving claim, which is the same
// question regardless of what else happened in it.
//
// ⚠ EXTENT ALONE DOES NOT DISCRIMINATE HERE THE WAY IT DOES ABOVE. A fold's
// MEASURE LENGTH between the two claims is shaped by wherever the follow's
// hysteresis happened to lock on, not by how far the source actually moved
// (a 20 m spike has been measured from barely 1 m of real source deviation),
// so "is there any extent" waves every fold through as if it were a genuine
// ring. What discriminates a fold from a ring is spatial: did the leg's own
// source, ANYWHERE between the two claims, get farther than
// EndpointWarpRadiusM from the node it is revisiting? A fold never does: the
// whole excursion is contained inside the very radius that made the align
// step consider absorbing in the first place. A genuine ring, which is
// correct topology for the same-node split rule, gets away from the node
// before it comes back.
//
// Reusing EndpointWarpRadiusM is deliberate, not a new tolerance:
// [pieceHasExtent]'s own header warns against fuzzing "is there any extent at
// all" into a radius, because that would collapse short REAL rings, but that
// is a different question from this one. This function never touches a piece
// WITH extent as "empty"; it asks whether a nonzero excursion is large enough
// to be believable, and EndpointWarpRadiusM is the package's existing answer
// to "how far away still counts as the same place" (the endpoint warp radius,
// and the align step's absorb radius), applied symmetrically to "how far away
// counts as having genuinely left".
//
// Measured on a hand-verified case: the leg's own OFF-run source, across the
// whole spike, never got farther than 13.3 m from the node it revisited, while
// the SAME leg's genuine different-node transitions never got closer than
// 38.6 m: a wide, clean gap either side of EndpointWarpRadiusM, which is why
// one existing radius is enough rather than a fitted one. A broader sweep over
// several test areas confirms this clears every fold without touching genuine
// large loops (a leg revisiting a junction ~600 m of committed geometry
// later).
//
// The EARLIER claim survives, same convention as collapseEmptySameNodeSeams
// and for the same reason: seams ascend, so the walk arriving at the node for
// the FIRST time is the one that gets to keep it. Whichever of the two was the
// MINT and which was the ABSORB in the align step makes no difference here:
// the mint's own node still exists in canon either way (the align step never
// undoes a cut); only whether THIS leg's OWN commit joins it there is being
// decided.
// `last` and the two terminals extend the same test to a seam that folds back
// onto a TERMINAL rather than another interior seam. collapseTerminalSameNodeSeams
// (below) only ever compares the OUTERMOST seam to its neighbouring terminal,
// so an interior fold two or more seams away from the terminal would sail past
// it (e.g. an interior seam folding back onto the LEG'S OWN END TERMINAL with
// one genuine seam between them, 3.77 m of committed excursion). THE TERMINAL
// ALWAYS WINS, never the seam, regardless of walk order, unlike two interior
// seams (where the earlier claim wins), because the terminal's coordinate is
// the endpoint warp's decision, already taken and already counted; the seam is
// the one that can be given up without contradicting a record.
//
// ⚠ THE TWO TERMINAL CHECKS BELOW MUST STAY SEPARATE, NOT A MAP KEYED BY
// NODE. A map like `map[NodeID]int{startTerminal:0, endTerminal:last}`
// fails for a leg whose start and end BOTH warp onto the same node (a real
// out-and-back leg): one node id names both position 0 and position `last`,
// so the map can hold only one of them. endTerminal, written second, would
// always win, so a fold at the START would be compared against the FAR-AWAY
// end position instead, which reads as "departs" almost unconditionally,
// since the whole leg lies between them, and the fold would go uncollapsed.
// Two independent `if`s cost one extra comparison per seam and cannot lose
// either terminal's claim to the other.
func collapseFoldedSameNodeSeams(seams []legSeam, pts []Point, last int, startTerminal, endTerminal *NodeID) []legSeam {
	out := seams[:0:0]
	lastKept := map[NodeID]int{} // node -> its index into out, most recent surviving claim
	for _, s := range seams {
		if s.HasNode {
			// ⚠ TWO INDEPENDENT CHECKS, NOT A MAP KEYED BY NODE. A leg whose
			// start and end terminals both warp onto the SAME node (a real
			// out-and-back leg, or two independently-warped terminals that
			// happen to land on one junction) has ONE node id naming BOTH
			// positions 0 and `last` — a map entry for that id can hold only
			// one of them, and whichever is written second (endTerminal,
			// always, by construction order) silently wins. An interior seam
			// folding back onto the START would then be compared against the
			// FAR AWAY end position instead, which reads as "departs" almost
			// unconditionally and lets the fold through uncollapsed. Checking
			// both terminals explicitly, every time, costs nothing (each is
			// one comparison) and cannot lose either claim to the other.
			if startTerminal != nil && s.Node == *startTerminal && !legDepartsRadius(pts, 0, s.Idx, EndpointWarpRadiusM) {
				continue // the terminal's claim always wins over a folding seam
			}
			if endTerminal != nil && s.Node == *endTerminal && !legDepartsRadius(pts, last, s.Idx, EndpointWarpRadiusM) {
				continue
			}
			if pos, ok := lastKept[s.Node]; ok && !legDepartsRadius(pts, out[pos].Idx, s.Idx, EndpointWarpRadiusM) {
				continue
			}
		}
		out = append(out, s)
		if s.HasNode {
			lastKept[s.Node] = len(out) - 1
		}
	}
	return out
}

// legDepartsRadius reports whether any point of pts[lo..hi] (order-
// insensitive) lies at least radiusM from pts[lo] — whether the leg's own
// source, somewhere in that stretch, genuinely got away from where it started
// rather than merely wobbling near it.
//
// ⚠ DECIDED, NOT ACCIDENTAL: a genuine loop SMALLER than radiusM (the source
// really does leave and come back, but never gets radiusM away from where it
// started) reads as a fold here and gets collapsed. This is reachable: the
// align step only ever proposes an absorb candidate within EndpointWarpRadiusM
// of a boundary, so both ends of a real spur under that footprint can
// independently resolve onto one node the same way a fold does. The call is
// to collapse it anyway, for two reasons. First, EndpointWarpRadiusM is not a
// new number invented for this question: it is already the package's answer
// to "how far away still counts as the same place" (the endpoint warp radius
// and the align step's absorb radius), so treating a sub-radius round trip as
// "still at the junction" is the SAME rule applied twice, not a second one.
// Second, and more load-bearing: nothing is deleted. Dropping a seam's claim
// moves no geometry: the leg's own points still commit, verbatim, in walk
// order, so a genuine small loop still RENDERS correctly as an out-and-back;
// the only thing lost is a shareable NODE at the fold point, which only
// matters if some OTHER leg would want to join exactly there, a vanishingly
// unlikely coincidence for a spur under 15 m. No such case was found across
// several test areas: every collapse spot-checked against the align step's
// own decision was a real fold.
func legDepartsRadius(pts []Point, lo, hi int, radiusM float64) bool {
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo < 0 || hi >= len(pts) {
		// Cannot happen — both indices come from this leg's own seams, built
		// against these same points — but reporting "it left" rather than
		// guessing "it didn't" is the fail-open-to-caution answer: the caller
		// treats "departs" as "keep both seams", never a silent merge.
		return true
	}
	anchor := pts[lo]
	for k := lo; k <= hi; k++ {
		if HaversineMetres(anchor.Lat, anchor.Lon, pts[k].Lat, pts[k].Lon) >= radiusM {
			return true
		}
	}
	return false
}

// collapseTerminalSameNodeSeams is [collapseEmptySameNodeSeams] one step
// further out: the same two-point same-node piece, but formed between a seam and
// the LEG'S OWN TERMINAL rather than between two seams.
//
// ⚠ WITHOUT THIS THE IMPORT ABORTS, exactly as the interior case does. The
// endpoint warp moves a terminal onto a nearby existing node, and that node
// can be the very one the leg's last seam already named: the leg rejoins
// canon at J and then ends within the warp radius of J. The final piece is
// then two points whose ends are both J, which the store rejects (an arc may
// not start and end on the same node), and the same-node split rule needs an
// interior vertex it does not have. [furthestVertex] errors and takes the
// user's whole import with it.
//
// Measured over a randomised test sweep before this existed: about 5% of seeds
// aborted, and in every one the two points were BYTE-IDENTICAL (gap
// 0.000000 m), so the arc would have been zero-length. Both the start and the
// end terminal occur.
//
// ⚠ THE SEAM LOSES THE CLAIM, NOT THE TERMINAL, and that is the opposite of the
// interior rule's "earlier survives" for a reason. The terminal's node is the
// endpoint warp's decision, already taken, already counted and already checked
// against the committed IDs. The seam is the claim that can be given up
// without contradicting a record. The point that loses its claim stays in the
// leg as an ordinary interior vertex at its own coordinates: nothing is
// invented and nothing is discarded, the same trade seam adoption already
// makes.
//
// EMPTINESS IS THE CONDITION, NOT ADJACENCY, for the same reason as in
// [collapseEmptySameNodeSeams]. A same-node terminal piece with REAL EXTENT is
// a ring (the leg left J, went somewhere, and came back to end on it), and the
// same-node split rule commits it as two arcs because that geometry exists. A
// piece with NO extent holds nothing to commit or to split, however many
// points it has: a real track produced a THREE-point terminal piece, both
// ends on one node, zero measure, which an adjacency test let through to the
// split and aborted the import.
//
// ⚠ This must not rely on the symmetric snap, which happens to move the
// boundary so this piece never forms in some cases. A feature that hides an
// abort is not a fix for it: disable the snap, or change its radius, and the
// abort returns. This is correct on its own merits.
//
// The accounting is unchanged by construction: a collapsed seam is one that NAMED
// a canon node, so it was never counted by [legMintedSeamNodes], and the leg
// loses one seam and one piece together, which keeps `Arcs == legs + seams`.
func collapseTerminalSameNodeSeams(seams []legSeam, pts []Point, last int, startTerminal, endTerminal *NodeID) []legSeam {
	if len(seams) == 0 {
		return seams
	}
	if first := seams[0]; startTerminal != nil && first.HasNode && first.Node == *startTerminal && !pieceHasExtent(pts, 0, first.Idx) {
		seams = seams[1:]
		if len(seams) == 0 {
			return seams
		}
	}
	if end := seams[len(seams)-1]; endTerminal != nil && end.HasNode && end.Node == *endTerminal && !pieceHasExtent(pts, end.Idx, last) {
		seams = seams[:len(seams)-1]
	}
	return seams
}

// collapseEmptySameNodeSeams merges two seams that name the same canon node with
// NO GEOMETRY between them into one. They are one seam claimed twice, not two.
//
// ⚠ WITHOUT THIS THE IMPORT ABORTS. The align step resolves each ON run's
// boundary to a canon node independently, and two of them can land on the
// same node J: the leg touches the junction at one run's end and again at the
// next run's start, because the follow left the band for a single point at J.
// The piece between two such seams is TWO POINTS whose ends are both J, which
// the store rejects (an arc may not start and end on the same node) and which
// the same-node split rule cannot rescue: its remedy is a cut at an interior
// vertex and there is no interior vertex. [furthestVertex] therefore errors and
// takes the user's whole import with it. Measured over a randomised test
// sweep: about 4% of seeds.
//
// ⚠ EMPTINESS IS THE CONDITION, NOT THE SHARED NODE, AND NOT ADJACENCY. A
// same-node piece with REAL EXTENT is a genuine ring (the leg left J, went
// somewhere and came back), and the same-node split rule commits it as two
// arcs because that geometry is real. A piece with no extent holds nothing to
// commit or to split. Merging it invents nothing and discards nothing; the
// point that loses its claim stays in the leg as an ordinary interior vertex,
// at its own coordinates.
//
// There are TWO ways to hold no extent:
//
//   - TWO POINTS. Both ends adopt J's coordinate, so the arc is zero-length
//     whatever the source points were.
//   - THREE OR MORE COINCIDENT POINTS. The piece HAS an interior vertex, so an
//     adjacency test would wave it through as "a real ring", but every point
//     is at the same coordinate, so the measure is exactly 0 and the split
//     fails on it. Seen on a real track: three byte-identical points, both
//     ends on one node.
//
// ⚠ "AN INTERIOR VERTEX MEANS REAL GEOMETRY" IS FALSE. Committed track
// geometry carries duplicate consecutive vertices wherever a GPS logged the
// same fix twice (measured at roughly one arc in ten on a real test area), and
// nothing dedupes them. A committed polyline CAN hold duplicate consecutive
// vertices.
//
// ⚠ IT IS NOT A PROPERTY OF THE ABSORB RADIUS. The radius decides whether the
// align step CUTS at such a run or absorbs past it, so it changes how often a
// cut lands there, not whether the resulting piece is survivable. Any radius
// can hit this, including no absorb at all.
//
// The EARLIER claim survives. The two are interchangeable: whichever keeps the
// claim is the one overwritten with J's coordinate and the other keeps its own,
// so either choice moves exactly one vertex and both sit inside the normal
// seam adoption. Seams ascend, so taking the first is the walk arriving at J
// where it first reaches it.
//
// ⚠ THE COMPARISON IS AGAINST THE LAST KEPT SEAM, AND THAT IS DELIBERATELY NOT A
// GREEDY COLLAPSE OF EVERY CONSECUTIVE CLAIM. Three claims on J at points i, i+1,
// i+2 that are spatially DISTINCT drop only the middle one, leaving a piece
// i..i+2 with a real interior vertex: a ring, which the same-node split rule
// commits as two arcs. This function removes only the pieces that rule cannot
// handle and hands everything else to it unchanged; collapsing a claim with
// geometry behind it would discard a junction touch on no evidence.
func collapseEmptySameNodeSeams(seams []legSeam, pts []Point) []legSeam {
	out := seams[:0:0]
	for _, s := range seams {
		if n := len(out); n > 0 {
			prev := out[n-1]
			if prev.HasNode && s.HasNode && prev.Node == s.Node && !pieceHasExtent(pts, prev.Idx, s.Idx) {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// pieceHasExtent reports whether the leg's points from lo to hi inclusive span any
// distance at all.
//
// Two points never do, because seam adoption overwrites both ends with the
// seam node's coordinate, so the source measure between them is irrelevant
// and is not consulted. Three or more do only if some point differs from the
// others; a run of byte-identical fixes sums to exactly 0.
//
// ⚠ EXACT ZERO, NOT A TOLERANCE. The question is "is there anything here", and
// its answer is either 0 or obviously-not-0; a metre-scale radius does not
// belong in a question about identity. A tolerance here would silently
// collapse short REAL rings, which are geometry the source actually has.
func pieceHasExtent(pts []Point, lo, hi int) bool {
	if hi-lo < 2 {
		return false
	}
	if lo < 0 || hi >= len(pts) {
		// The caller's indices come from seams built against these very points, so
		// this cannot happen, and if it ever does, silently reporting "no extent"
		// would DELETE a seam. Report extent so the piece reaches the same-node
		// split, which fails loudly, rather than vanishing here.
		return true
	}
	return canonMeasureLengthM(pts[lo:hi+1]) > 0
}

func runEndName(atEnd bool) string {
	if atEnd {
		return "end"
	}
	return "start"
}

// legMintedSeamNodes counts the seams at which the LEG mints a node of its own
// rather than terminating on one canon already has: the seams whose HasNode is
// false, which the commit step writes with a nil end node so the store mints
// one and threads its ID into the next piece.
//
// It is the third and last way a node enters the network, after a leg's two
// terminals and a cut, and it is counted for exactly the reason the other two
// are: the committed node count is checked against the causes that produced
// it, and a cause nothing counts makes that identity unclosable.
//
// ⚠ COUNT IT HERE, NOT AT THE DECLINE. A stub decline is what CREATES the
// possibility, but it does not settle it: legSeamsFromAlign merges an ON<->ON
// seam where one run declined and the other found a junction, and the junction
// wins (`case hasNode && !prev.HasNode`). Counting declines would therefore
// over-report exactly where the two topologies do meet. The seam list is the
// record of what the leg is about to commit, which is the event the node count
// is about.
//
// ⚠ DEDUPED BY COORDINATE, to stay the count the commit step ACTUALLY mints
// rather than the count of declined seams. Two declined seams of one leg can
// share an exact coordinate (the leg's own geometry touches one canon vertex
// more than once), and the commit step mints the first and reuses it for the
// rest, so this must count coordinates, not occurrences, or the two disagree
// and the node-count identity stops reconciling.
func legMintedSeamNodes(points []Point, seams []legSeam) int {
	seen := map[[2]float64]bool{}
	n := 0
	for _, s := range seams {
		if s.HasNode {
			continue
		}
		c := [2]float64{points[s.Idx].Lat, points[s.Idx].Lon}
		if seen[c] {
			continue
		}
		seen[c] = true
		n++
	}
	return n
}

// seamSideName says where one end of a piece got its node, for error messages:
// canon's own node, a node this leg minted at an earlier seam, or the leg's own
// terminal anchor (no node at all). A message naming only the node id cannot
// tell the first two apart, and they have different causes.
func seamSideName(seams []legSeam, k int, threaded *NodeID) string {
	switch {
	case k < 0 || k >= len(seams):
		if threaded != nil {
			return fmt.Sprintf("threaded node %d", *threaded)
		}
		return "leg terminal (no node)"
	case seams[k].HasNode:
		return fmt.Sprintf("canon node %d at point %d", seams[k].Node, seams[k].Idx)
	case threaded != nil:
		return fmt.Sprintf("threaded node %d at point %d", *threaded, seams[k].Idx)
	default:
		return fmt.Sprintf("minted at point %d", seams[k].Idx)
	}
}

// furthestVertex picks the INTERIOR vertex FURTHEST (great-circle distance)
// from the piece's own shared start/end coordinate — pts[0], which equals
// pts[len(pts)-1] since this is only ever called on a piece that begins and
// ends on one node.
//
// Why the furthest vertex and not the measure-halfway one: measure-halfway
// only lands at an out-and-back's actual turn when the out and back legs are
// the SAME length. An asymmetric out-and-back (out 3 km, back by a 4 km
// variant) puts the measure midpoint half a kilometre down the return leg,
// nowhere meaningful. Furthest-from-the-anchor puts the node at the turn
// itself, a real feature of the walk, regardless of asymmetry.
//
// ⚠ Stability caveat: argmax of a distance function is flat at its own
// maximum. Many vertices can sit at nearly identical distances, so the winner
// can flip to a neighbour under a perturbation far smaller than one that would
// move a measure-halfway choice. If re-importing the same file ever shows this
// vertex flipping between passes, measure-halfway is the fallback, rather than
// a tolerance or a tie-break heuristic.
//
// The vertex is always an EXISTING one, never interpolated (the split
// satisfies a storage constraint, not license to invent geometry the source
// never had), and a piece with no interior vertex is an error, not a guessed
// midpoint.
//
// TIE-BREAK: lowest index wins on an EXACT tie. It is deterministic by
// construction (strict `>`, first-seen wins); near-ties, not exact ones, are
// the real stability risk named above.
func furthestVertex(pts []Point) (int, error) {
	if len(pts) < 3 {
		return 0, fmt.Errorf(
			"a piece of %d point(s) begins and ends on the same node and has no interior vertex to split at", len(pts))
	}
	anchor := pts[0]
	best, bestD := 1, -1.0
	for i := 1; i < len(pts)-1; i++ {
		if d := HaversineMetres(anchor.Lat, anchor.Lon, pts[i].Lat, pts[i].Lon); d > bestD {
			best, bestD = i, d
		}
	}
	return best, nil
}
