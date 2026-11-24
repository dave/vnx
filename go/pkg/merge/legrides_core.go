package merge

import (
	"context"
	"fmt"
	"math"
)

// LegRide is the matcher's verdict about one piece of a leg, carried to the
// writer. The writer acts on this verdict rather than on any after-the-fact
// geometric detector.
//
// Arcs is the ordered list of canon arcs the piece rides, resolved from the align step's
// OWN RECORD of what it cut ([AlignLineage]) — never from geometry, never from
// a spatial search, and never from `run.CanonID` alone, which names a row that
// this leg's own later cuts may have deleted.
//
// ⚠ A LIST, NOT ONE ARC. A piece does not always resolve to a single post-cut
// arc: a GPS backtrack makes a LATER run of the SAME leg cut INSIDE an earlier
// run's interval, so the earlier run's piece spans two arcs and there is no
// single arc between its nodes at all. This occurs regularly on real data.
type LegRide struct {
	Run int
	// Canon is the arc the RUN NAMED, before any of this leg's own cuts — the
	// lineage's root, carried for error messages that have to say which piece
	// of canon a bad ride came from.
	Canon ArcID
	Arcs  []ArcID
	// Start/End are the nodes the align step's boundaries resolved this run's two ends to.
	// A piece claims this ride only when its OWN threaded nodes are these two,
	// exactly — see [LegRides.ForPiece]. They are a SECOND, independent check on
	// top of the point range: the range says "this is that ON stretch", the
	// nodes say "and nothing repointed its ends since".
	Start, End NodeID
	// AbsorbM is how far the align step moved this run's two ends to reach Start and End —
	// the sum of both boundaries' [AlignBoundary.AbsorbedM]. A referenced piece
	// legitimately covers its own extent PLUS those two reaches, because the
	// reference draws the join with canon's own vertices instead of a chord;
	// the writer's length guard needs to know how much of the excess was bought
	// and paid for.
	AbsorbM float64
	// RunM is the length of canon the RUN ITSELF claimed — |MeasureEnd −
	// MeasureStart| on the arc it bound. It is the resolver's own yardstick:
	// the leaves this ride names must cover that stretch and the two absorb
	// reaches, and nothing else.
	RunM float64
	// Reversed is derived from where Start and End sit in the lineage, not from
	// [FollowRun.Reversed]: the run's flag is the matcher's direction against
	// the row it bound, and an absorbed boundary can move an end onto a
	// different lineage node afterwards. The walk order of Arcs is what the
	// writer needs, and this is that order's own account of itself.
	Reversed bool
}

// LegRideKey is the piece a ride belongs to, as the INCLUSIVE point range the
// assembler gave its ON stretch ([OnPieceRange].Lo/Hi).
//
// ⚠ THE RANGE, NOT THE NODE PAIR, AND THE DIFFERENCE IS A MERGED ALTERNATE.
// Keying by the two nodes looks equivalent and is not: ANY piece threaded
// between those nodes would claim the ride, including an OFF piece that runs
// between the same two junctions by a DIFFERENT LINE. Referencing there would
// merge a genuine alternate, a failure that must never happen and that every
// other metric would still call success. Node pairs also tie on real data,
// where one leg rides one node pair twice in the same direction.
//
// The range is only comparable while the leg's index space is intact; see
// [LegRides.PointCount].
type LegRideKey struct{ Lo, Hi int }

// UnresolvedRide is one ON piece whose ride could NOT be resolved, and why.
// Recorded rather than discarded: the whole point of the verdict design is that
// a piece the matcher called ON and the writer copies anyway is a parallel
// duplicate, so the population that cannot be resolved is the population still
// producing them, and it has to be nameable.
type UnresolvedRide struct {
	Run   int
	Canon ArcID
	// Lo/Hi are the ON stretch's own point range, so a created piece can be
	// ATTRIBUTED to the reason its ride failed rather than lumped in with the
	// OFF pieces the writer is supposed to create.
	Lo, Hi    int
	Reason    string
	StartKind string
	EndKind   string
	StartNode NodeID
	EndNode   NodeID
}

// Reasons an ON piece resolves to no ride. Every one of these is a piece the
// writer will CREATE, and each is a different question.
const (
	// The boundary was absorbed (the one-node rule, DECLINE 2) onto a node that
	// is not on the lineage the run rode — a node belonging to a parallel copy
	// or a crossing arc, up to EndpointWarpRadiusM away.
	RideForeignAbsorb = "foreign-absorb"
	// Both ends resolved to ONE node: a stationary or zero-extent run. The ring
	// rule owns this piece; there is no stretch to reference.
	RideSameNode = "same-node"
	// The canon arc closes on itself, so one node id appears twice in the
	// lineage and "the leaves between these two nodes" has two answers.
	rideAmbiguousRingCanon = "ambiguous-ring-canon"
	// The symmetric snap adopted a node whose projection falls INSIDE the ON
	// stretch, so the piece the writer will see is half the ride; see
	// [LegRides.RemapAfterSnapInsertions].
	RideSnapSeamInsideRide = "snap-seam-inside-ride"
	// The ON stretch's own boundary is not a seam of the committed leg: the leg
	// cut dropped it (a stub decline names no node) or merged it into a
	// neighbouring claim that named a DIFFERENT node. Either way no committed
	// piece is this ON stretch, so there is nothing to reference.
	RideBoundaryNotASeam = "boundary-not-a-seam"
	// TWO ON stretches map onto ONE committed piece, because the seams between
	// them were merged or collapsed away. The piece then covers both rides and
	// the stretch between, and referencing either would silently drop the rest
	// of the piece's geometry. Neither is offered.
	RideTwoRidesOnePiece = "two-rides-one-piece"
	// The ON stretch is committed in more than one piece — the caller's
	// interior-node seaming cut it at a vertex that is bitwise an existing node — and one of those
	// divisions is not a single point on the ridden lineage, so "the leaves
	// between these two nodes" has no answer for the stretches it borders.
	//
	// ⚠ AN INTERIOR DIVISION THAT FAILS COSTS TWO PIECES, NOT ONE. It borders
	// the stretch on each side of it, and neither can be referenced, so both are
	// copied — this reason is counted per PIECE and an interior failure
	// contributes two rows. Only the stretches beyond them, whose own two
	// divisions both resolve, are still offered.
	RideInteriorNodeOffLineage = "interior-node-off-lineage"
	// The boundary's node IS on the ridden lineage, but at the wrong POSITION —
	// further along canon than the absorb radius from where the run actually
	// ended. Taking it would resolve the ride to leaves the run never rode.
	rideBoundaryOffPosition = "boundary-off-position"
)

// LegRides is every ride one leg resolved, indexed by the directed node pair
// the claiming piece must present.
type LegRides struct {
	ByPiece    map[LegRideKey]*LegRide
	Unresolved []UnresolvedRide
	// PointCount is len(asm.Points) at the moment the rides were resolved.
	//
	// ⚠ THE RIDES' KEYS ARE POINT INDICES, AND ONE STAGE RENUMBERS THEM. The
	// symmetric snap INSERTS a vertex at each adopted projection, which
	// renumbers every later index. Nothing else does — the endpoint warp
	// returns the same length with Idx unchanged, the terminal warp moves
	// points in place, and the ring split adds a PIECE, not a point. So the
	// length is a sufficient witness.
	//
	// ⚠ A MISMATCH IS AN ERROR, NOT A QUIET REFUSAL. Treating it as "no ride"
	// would turn every ON piece of the leg into a created copy of canon — the
	// parallel strands ride resolution exists to remove — while the import
	// still reported success. The caller compares this against the committing
	// point count immediately before the commit step and fails the item; see
	// [CheckRidesIndexSpace].
	PointCount int
}

// ForPiece returns the ride for a piece running from startNode to endNode, if
// this leg resolved one. Both ends must be threaded (a nil end is a free
// anchor, which references nothing) and both must be EXACTLY the nodes the
// align step's boundaries named — a seam merge, a symmetric-snap adoption
// inside the run, or a terminal that warped somewhere else all leave the piece
// spanning something other than the ride, and it is then created.
//
// [TestHooks].LegRideSuppress is applied by CommitLegPieces, which then does
// not ask.
func (r *LegRides) ForPiece(pointCount, lo, hi int, startNode, endNode *NodeID) *LegRide {
	// ⚠ NO POINT-COUNT TEST HERE. Returning nil on a mismatch would read as
	// a guard and behave as a fallback: a stale index space would turn EVERY
	// ON piece of the leg into a created copy of canon while the import
	// reported success. The mismatch is an ERROR, raised once for the whole
	// leg by the caller immediately before the commit step (see
	// [CheckRidesIndexSpace]). pointCount is still taken so the call site
	// keeps its subject.
	if r == nil || startNode == nil || endNode == nil {
		return nil
	}
	ride := r.ByPiece[LegRideKey{Lo: lo, Hi: hi}]
	if ride == nil || ride.Start != *startNode || ride.End != *endNode {
		return nil
	}
	return ride
}

// RemapAfterSnapInsertions moves every ride's point range into the index space
// the symmetric snap just created, and DROPS any ride the snap cut in half.
//
// ⚠ WITHOUT THIS, ONE ADOPTION ANYWHERE ON A LEG WOULD DISABLE EVERY RIDE ON
// IT. [LegRides.PointCount] refuses a ride whose index space has moved, which
// is correct and far too blunt on its own: measured on test areas, falling
// back to "no rides for this leg" put several kilometres of byte-identical
// parallel strands back into the network. The snap's own record says exactly
// where it inserted, so the ranges are moved rather than abandoned.
//
// A vertex inserted STRICTLY INSIDE a ride is a different matter: it carries a
// seam, so the piece the writer will offer is half the ON stretch and is not
// this ride. That ride is dropped.
func (r *LegRides) RemapAfterSnapInsertions(inserted []SymmetricSnapInsert, newPointCount int) {
	r.PointCount = newPointCount
	if len(inserted) == 0 || len(r.ByPiece) == 0 {
		return
	}
	// shift maps an OLD index to its NEW one. Each inserted vertex sits at its
	// own index in the NEW slice and pushes everything from there rightwards,
	// so the ascending scan can apply them one at a time.
	shift := func(i int) int {
		for _, ins := range inserted {
			if ins.Idx <= i {
				i++
			}
		}
		return i
	}
	moved := make(map[LegRideKey]*LegRide, len(r.ByPiece))
	for k, ride := range r.ByPiece {
		lo, hi := shift(k.Lo), shift(k.Hi)
		split := false
		for _, ins := range inserted {
			if ins.Idx > lo && ins.Idx < hi {
				split = true
			}
		}
		if split {
			r.Unresolved = append(r.Unresolved, UnresolvedRide{
				Run: ride.Run, Reason: RideSnapSeamInsideRide, Lo: lo, Hi: hi,
				StartNode: ride.Start, EndNode: ride.End,
			})
			continue
		}
		moved[LegRideKey{Lo: lo, Hi: hi}] = ride
	}
	r.ByPiece = moved
}

// ResolveLegRides turns the align step's per-boundary record into one ride per
// ON piece.
//
// ⚠ NODE IDENTITY, NOT MEASURE. Every position in this function is located by
// which NODE sits there, never by comparing two float measures: a half's
// recomputed length is not the measure it was cut at (about 1e-6 relative), so
// a measure comparison would need a tolerance and ride resolution introduces
// none. The lineage records the node at every division, so the
// question "where does this boundary sit on the arc the run rode" is answered
// by looking its node up in that list.
func ResolveLegRides(ctx context.Context, asm AssembledLeg, rep AlignReport, seams []LegSeam) (*LegRides, error) {
	st := stateOf(ctx)
	ignoreMeasure := st.hooks.A1AbsorbIgnoreMeasure != nil && st.hooks.A1AbsorbIgnoreMeasure()
	out := &LegRides{ByPiece: map[LegRideKey]*LegRide{}, PointCount: len(asm.Points)}
	byRunEnd := make(map[boundaryKey]AlignBoundary, len(rep.Boundaries))
	for _, b := range rep.Boundaries {
		byRunEnd[boundaryKey{Run: b.Run, AtEnd: b.AtRunEnd}] = b
	}
	for _, p := range asm.OnPieces {
		if p.Run < 0 || p.Run >= len(asm.Runs) {
			return nil, fmt.Errorf(
				"ResolveLegRides: ON piece at points %d..%d names run %d, and the leg has %d run(s)",
				p.Lo, p.Hi, p.Run, len(asm.Runs))
		}
		r := asm.Runs[p.Run]
		lineage, ok := rep.lineageFor(r.CanonID)
		if !ok {
			return nil, fmt.Errorf(
				"ResolveLegRides: run %d rode canon arc %d, and the align step recorded no cut lineage for it — every ON run's arc is cut or declined against, so a missing lineage means the align report and the runs describe different legs",
				p.Run, r.CanonID)
		}
		start, sOK := byRunEnd[boundaryKey{Run: p.Run, AtEnd: false}]
		end, eOK := byRunEnd[boundaryKey{Run: p.Run, AtEnd: true}]
		if !sOK || !eOK {
			return nil, fmt.Errorf(
				"ResolveLegRides: the align step reports no boundary for run %d's %s — one boundary per end of every ON run",
				p.Run, runEndName(!sOK))
		}
		sNode, sHas := start.SeamNode()
		eNode, eHas := end.SeamNode()
		failRange := func(reason string, lo, hi int, startNode, endNode NodeID) {
			out.Unresolved = append(out.Unresolved, UnresolvedRide{
				Run: p.Run, Canon: r.CanonID, Reason: reason, Lo: lo, Hi: hi,
				StartKind: alignDeclineName(start.Decline), EndKind: alignDeclineName(end.Decline),
				StartNode: startNode, EndNode: endNode,
			})
		}
		fail := func(reason string) {
			failRange(reason, p.Lo, p.Hi, sNode, eNode)
		}
		if !sHas || !eHas {
			// Every align outcome names a node, so this is not a data
			// condition — it is the align step and the leg cut disagreeing about
			// what a boundary is. No fallback: a piece created here is a parallel copy nobody
			// counted.
			return nil, fmt.Errorf(
				"ResolveLegRides: run %d on canon arc %d has a boundary with no node (start %s, end %s) — every align outcome names a node",
				p.Run, r.CanonID, alignDeclineName(start.Decline), alignDeclineName(end.Decline))
		}
		if sNode == eNode {
			fail(RideSameNode)
			continue
		}
		si, sCount := lineageNodeIndex(lineage, sNode)
		ei, eCount := lineageNodeIndex(lineage, eNode)
		// ── EACH END'S OWN VERDICT, IN THE ORDER THE PIECE HAS ALWAYS USED ──
		//
		// Held per END rather than collapsed into one answer for the whole ON
		// stretch. A piece with no interior division fails for exactly the
		// reason it always did — the precedence below is the old cascade,
		// unchanged — but an interior division can make the GOOD end's half a
		// ride even when the other end's half is not one, and one shared
		// verdict cannot say that. See [RideInteriorNodeOffLineage] and the
		// division loop below for the case this exists for.
		sReason, eReason := "", ""
		if sCount > 1 || eCount > 1 {
			sReason, eReason = rideAmbiguousRingCanon, rideAmbiguousRingCanon
		} else if sCount == 0 || eCount == 0 {
			// A CUT's own node and a DECLINE-1 "already at this row's end" node
			// are lineage nodes by construction — the lineage is built from the
			// same cuts, and DECLINE 1 reads the parent row's own ends. So the
			// only way a node can be missing is the one-node rule's absorb
			// reaching a node that is not on the ridden lineage at all: a foreign
			// absorb. Anything else is an internal inconsistency.
			//
			// ⚠ THE ERROR IS RAISED FOR THE FIRST MISSING END ONLY. Judging the
			// second one as well would turn a piece that reports foreign-absorb
			// into a failed import.
			missing, kind := start, "start"
			if sCount != 0 {
				missing, kind = end, "end"
			}
			switch missing.Decline {
			case AlignDeclineAbsorbedToNode, AlignDeclineAtExistingNode:
			default:
				return nil, fmt.Errorf(
					"ResolveLegRides: run %d's %s boundary on canon arc %d resolved to node %d by %s, and that node is not on the arc's own cut lineage (%d leaf/leaves, nodes %v) — a cut mints its node on the lineage and a decline reads the row's own end, so neither can land off it",
					p.Run, kind, r.CanonID, missing.Node, alignDeclineName(missing.Decline), len(lineage.Leaves), lineageNodes(lineage))
			}
			if sCount == 0 {
				sReason = RideForeignAbsorb
			}
			if eCount == 0 {
				eReason = RideForeignAbsorb
			}
		}
		// ⚠ ON THE LINEAGE IS NOT ENOUGH — IT MUST BE WHERE THE RUN ENDED.
		// DECLINE 2's candidate set is bounded along canon, but one route past
		// it survives and must: a node at 0.000 m is DECLINE 1b's case and is
		// adopted whatever its measure, because cutting there would mint a
		// second node on top of an existing one. Canon that loops back on
		// itself then hands a run a boundary node thousands of metres along
		// its own lineage — e.g. a 405 m run whose start boundary sat at
		// measure 4,362 adopted a coincident node at measure 0, and the ride
		// spanned 3,957 m.
		//
		// The align step is right to keep that node; the RIDE must not be built on it. The
		// piece is created instead, and counted. Same radius, measured along
		// canon — no new tolerance.
		if sReason == "" && !nodeIsAtMeasure(lineage, si, start.MeasureM, ignoreMeasure) {
			sReason = rideBoundaryOffPosition
		}
		if eReason == "" && !nodeIsAtMeasure(lineage, ei, end.MeasureM, ignoreMeasure) {
			eReason = rideBoundaryOffPosition
		}
		// THE PIECE'S RANGE IS THE SEAM'S INDEX, NOT THE ON RANGE'S. Two ON runs
		// can meet at two ADJACENT indices with no distance between them, and
		// LegSeamsFromAlign merges those two claims into ONE seam at the EARLIER
		// index (ZeroArcGap). The committed piece then runs from that earlier
		// index, and a ride keyed on the ON range's own Lo names a piece that
		// does not exist; on test data this committed byte-identical parallel
		// arcs several kilometres long.
		lo, loOK := rideSeamIndex(seams, p.Lo, sNode, 0)
		hi, hiOK := rideSeamIndex(seams, p.Hi, eNode, len(asm.Points)-1)
		// An end that already has a verdict keeps it: the lineage checks take
		// precedence over this cascade, so the reported reason is the first
		// failing check's.
		firstReason := func(fallback string) string {
			if sReason != "" {
				return sReason
			}
			if eReason != "" {
				return eReason
			}
			return fallback
		}
		if !loOK || !hiOK {
			fail(firstReason(RideBoundaryNotASeam))
			continue
		}
		if lo >= hi {
			// The merge folded the whole ON stretch onto one point.
			fail(firstReason(RideSameNode))
			continue
		}
		// ── THE DIVISIONS THIS ON STRETCH IS ACTUALLY COMMITTED IN ──────────
		//
		// The caller's interior-node seaming can have cut this stretch at a
		// vertex that is bitwise an existing node, so the writer will offer TWO
		// pieces where the align step described one run. A ride is per COMMITTED PIECE ([LegRideKey]), so
		// one ride spanning both would be claimed by neither and the leg would
		// copy canon on both halves — which is the whole defect that rule exists
		// to remove, reappearing one layer down. Each stretch between two
		// consecutive divisions is resolved on its own.
		//
		// With no interior seam this is one stretch between the two boundaries.
		divs := []rideDivision{{Idx: lo, Node: sNode, Li: si, OK: sReason == "", Reason: sReason}}
		for _, s := range seams {
			if s.Idx <= lo || s.Idx >= hi || !s.HasNode {
				continue
			}
			li, cnt := lineageNodeIndex(lineage, s.Node)
			divs = append(divs, rideDivision{
				Idx: s.Idx, Node: s.Node, Li: li,
				OK: cnt == 1, Reason: RideInteriorNodeOffLineage,
			})
		}
		divs = append(divs, rideDivision{Idx: hi, Node: eNode, Li: ei, OK: eReason == "", Reason: eReason})
		// Direction is read off the OUTERMOST divisions that resolved, not off
		// the two boundaries: when one boundary is off the lineage its index is
		// -1 and says nothing about which way the run walked canon.
		firstOK, lastOK := -1, -1
		for i, d := range divs {
			if d.OK {
				if firstOK < 0 {
					firstOK = i
				}
				lastOK = i
			}
		}
		reversed := firstOK >= 0 && lastOK > firstOK && divs[lastOK].Li < divs[firstOK].Li
		for j := 0; j+1 < len(divs); j++ {
			a, b := divs[j], divs[j+1]
			// ⚠ ONE ROW PER COMMITTED PIECE, AND UNDER THAT PIECE'S OWN RANGE.
			// The unit here is the PIECE, not the division: an unresolvable
			// interior division leaves BOTH stretches beside it unreferenceable,
			// and the writer copies BOTH, so two rows is the honest cost and not
			// a double count. Each row names its own piece's range and nodes, not
			// the whole ON stretch's `p.Lo, p.Hi`, so it can be attributed to the
			// piece the writer actually copied. The key a resolved half is filed
			// under is `LegRideKey{a.Idx, b.Idx}` (below); the unresolved halves
			// name the same thing.
			failPiece := func(reason string) { failRange(reason, a.Idx, b.Idx, a.Node, b.Node) }
			switch {
			case !a.OK:
				failPiece(a.Reason)
				continue
			case !b.OK:
				failPiece(b.Reason)
				continue
			case a.Li == b.Li:
				failPiece(RideSameNode)
				continue
			case reversed != (b.Li < a.Li):
				// The divisions do not walk canon in one direction, so "the
				// leaves between these two nodes" is not a stretch of this run.
				failPiece(RideInteriorNodeOffLineage)
				continue
			}
			loLi, hiLi := a.Li, b.Li
			if hiLi < loLi {
				loLi, hiLi = hiLi, loLi
			}
			arcs := make([]ArcID, 0, hiLi-loLi)
			for i := loLi; i < hiLi; i++ {
				arcs = append(arcs, lineage.Leaves[i].Arc)
			}
			if reversed {
				for i, k := 0, len(arcs)-1; i < k; i, k = i+1, k-1 {
					arcs[i], arcs[k] = arcs[k], arcs[i]
				}
			}
			key := LegRideKey{Lo: a.Idx, Hi: b.Idx}
			if prev, dup := out.ByPiece[key]; dup {
				// The ON RANGES are disjoint (the assembler checks this), so this is
				// always the seams between them having merged or collapsed: one
				// committed piece now covers two rides and whatever lies between.
				delete(out.ByPiece, key)
				out.Unresolved = append(out.Unresolved,
					UnresolvedRide{Run: prev.Run, Reason: RideTwoRidesOnePiece, Lo: key.Lo, Hi: key.Hi, StartNode: prev.Start, EndNode: prev.End})
				failPiece(RideTwoRidesOnePiece)
				continue
			}
			// ⚠ THE ABSORB REACHES BELONG TO THE OUTER ENDS ONLY. An interior
			// division is a vertex the leg is ALREADY standing on, bitwise —
			// nothing was moved to reach it, so it buys the writer's length guard
			// no slack at either sub-ride.
			absorb := 0.0
			if j == 0 {
				absorb += start.AbsorbedM
			}
			if j+2 == len(divs) {
				absorb += end.AbsorbedM
			}
			// ⚠ AND THE YARDSTICK IS THE RUN'S OWN INTERVAL ONLY WHILE THE
			// STRETCH IS THE WHOLE RUN. Once the piece is divided, the run's
			// measure covers both halves and would let either one name twice
			// the canon it rides; the lineage's own measures at the two
			// division nodes are what this stretch actually claims. The
			// undivided case keeps the run's interval exactly.
			runM := math.Abs(r.MeasureEnd - r.MeasureStart)
			if len(divs) > 2 {
				runM = math.Abs(lineageMeasureAt(lineage, hiLi) - lineageMeasureAt(lineage, loLi))
			}
			ride := &LegRide{Run: p.Run, Canon: r.CanonID, Arcs: arcs, Start: a.Node, End: b.Node, Reversed: reversed,
				AbsorbM: absorb, RunM: runM}
			if st.hooks.LegRideFault != nil {
				// FAULT INJECTION, INERT IN PRODUCTION. The writer's "the chain
				// does not close" abort is meant to be unreachable under correct
				// code, and a guard nobody can fire is a guard
				// nobody knows works.
				st.hooks.LegRideFault(ride)
			}
			out.ByPiece[key] = ride
		}
	}
	if st.hooks.LegRide != nil {
		st.hooks.LegRide(st.leg, out)
	}
	return out, nil
}

// rideSeamIndex maps ONE end of an ON range to the index the committed piece
// will actually break at: the seam carrying that end's node.
//
// ⚠ THAT IS NOT ALWAYS THE ON RANGE'S OWN INDEX. Two ON runs can meet at two
// ADJACENT indices, and the leg cut then holds ONE seam for them —
// LegSeamsFromAlign merges two claims on one physical point, and
// CollapseSameNodeSeams removes a same-node seam whose piece has no extent.
// Both keep the EARLIER index. The committed piece runs from there, so a ride
// keyed on the ON range's own Lo names a piece that does not exist, which on
// test data committed byte-identical parallel arcs.
//
// The seam is found BY ITS NODE, and the nearest one wins if a leg touches the
// same node twice. `terminal` is the answer when the ON range reaches the leg's
// own end, where there is no seam at all.
//
// It returns false when no seam carries the node — a boundary the leg cut
// dropped (a stub decline names none), or a merge that resolved to the OTHER
// side's node. Both mean no committed piece is this ON stretch.
func rideSeamIndex(seams []LegSeam, want int, node NodeID, terminal int) (int, bool) {
	if want == terminal {
		return terminal, true
	}
	best, found := 0, false
	for _, sm := range seams {
		if !sm.HasNode || sm.Node != node {
			continue
		}
		if !found || AbsInt(sm.Idx-want) < AbsInt(best-want) {
			best, found = sm.Idx, true
		}
	}
	return best, found
}

// nodeIsAtMeasure reports whether the lineage node at index `idx` sits within
// EndpointWarpRadiusM ALONG CANON of `measure` — the place the boundary was
// actually computed. Index 0 is the original arc's start, index i the high end
// of leaf i-1. ignoreMeasure is [TestHooks].A1AbsorbIgnoreMeasure's answer.
func nodeIsAtMeasure(l AlignLineage, idx int, measure float64, ignoreMeasure bool) bool {
	if ignoreMeasure {
		// TEST SWITCH, INERT IN PRODUCTION. One switch drops the
		// along-canon bound at BOTH places it is applied — DECLINE 2's
		// candidate set and this check — because that is the single property
		// being tested, and leaving either in place hides the guard the test
		// is about.
		return true
	}
	m := 0.0
	if idx > 0 {
		m = l.Leaves[idx-1].HiM
	} else {
		m = l.Leaves[0].LoM
	}
	d := m - measure
	if d < 0 {
		d = -d
	}
	return d <= EndpointWarpRadiusM
}

// lineageNodeIndex reports the position of a node in a lineage's node sequence
// (leaf 0's low node, then every leaf's high node) and HOW MANY times it
// appears. The count is returned rather than swallowed because a canon arc that
// closes on itself has one node at both ends, and "the leaves between these two
// nodes" then has two answers — see rideAmbiguousRingCanon.
func lineageNodeIndex(l AlignLineage, n NodeID) (idx, count int) {
	idx = -1
	for i, leaf := range l.Leaves {
		if i == 0 && leaf.LoNode == n {
			idx, count = 0, count+1
		}
		if leaf.HiNode == n {
			idx, count = i+1, count+1
		}
	}
	return idx, count
}

// rideDivision is one place a committed piece of an ON stretch begins or ends:
// its point index, the node standing there, and where that node sits in the
// ridden lineage. The two boundaries the align step recorded are always divisions; an
// interior seam inside the stretch is one too.
//
// OK is whether this division can bound a ride at all. A division that is not
// OK does not merely lose its own ride — it breaks the stretch on BOTH sides,
// which is why the reason travels with the division rather than with the piece.
type rideDivision struct {
	Idx    int
	Node   NodeID
	Li     int
	OK     bool
	Reason string
}

// lineageMeasureAt is the canon measure of the lineage's node number `idx` —
// leaf 0's low measure for idx 0, and leaf idx-1's high measure otherwise.
//
// ⚠ THE SAME EXPRESSION [nodeIsAtMeasure] READS, and deliberately not a second
// one: both are asking where a lineage node sits along the arc the run rode,
// and two spellings of that could disagree at a cut boundary.
func lineageMeasureAt(l AlignLineage, idx int) float64 {
	if idx > 0 {
		return l.Leaves[idx-1].HiM
	}
	return l.Leaves[0].LoM
}

func lineageNodes(l AlignLineage) []NodeID {
	out := make([]NodeID, 0, len(l.Leaves)+1)
	for i, leaf := range l.Leaves {
		if i == 0 {
			out = append(out, leaf.LoNode)
		}
		out = append(out, leaf.HiNode)
	}
	return out
}

func alignDeclineName(d AlignDecline) string {
	switch d {
	case AlignCutMade:
		return "cut"
	case AlignDeclineAtNode:
		return "at-row-end"
	case AlignDeclineAtExistingNode:
		return "at-existing-node"
	case AlignDeclineAbsorbedToNode:
		return "absorbed"
	}
	return fmt.Sprintf("decline-%d", int(d))
}

func AbsInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
