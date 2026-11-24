package merge

// corridorwire.go — the corridor solve's output, converted into
// [FollowRun]s the assembler can commit. The caller runs it once per leg.

import (
	"context"
	"fmt"
)

// solverEps is the width below which an import-measure run is sub-eps noise
// rather than real geometry — corridorsolve.go's own layering eps (1e-6),
// looser than ClipCorridorPiece's exact overlap test, so a run can land, at
// this scale, fractionally outside the bounds it was solved as active on
// (measured on a randomised test corpus: a 7.8e-9 m ON run). Shared by both the OFF
// and ON skips below: the same solver tolerance produces the same class of
// noise on either side of the OFF/ON boundary, so both need the same
// silent-drop treatment, not two independently chosen thresholds.
const solverEps = 1e-6

// corridorCostObserver receives the per-leg cost census. It is a TEST INSTRUMENT: [CorridorCostObserverForTests] is nil in
// production, so the whole census costs one nil compare per leg, one per
// (source edge × candidate), and one per solve.
//
// ⚠ IT MUST NOT CHANGE WHAT IS COMPUTED. Every call below reports a number the
// matcher already had; nothing here reorders a loop, moves an allocation that
// feeds a result, or decides anything. Byte-identical output is the bar for
// any memory work, so a census that perturbs the answer is a bug, not a
// measurement.
//
// The call order for one leg is fixed, and an implementation should assert it
// rather than tolerate a gap — a census that silently missed half a leg would
// leave every corpus lock green and report a memory saving that was never made:
//
//	LegStart
//	  SourceEdgeMinted × (source edges × non-nil candidates)
//	MintFinished                        ← the phase event
//	  SolveFinished                     ← ONLY when the solve actually ran
//	LegFinished
type corridorCostObserver interface {
	// LegStart opens one leg's record. candidates counts the NON-NIL prepped
	// candidates, which is exactly the set [CorridorIntervals] mints against.
	LegStart(sourceEdges, candidates int)

	// SourceEdgeMinted reports one (source edge × candidate) mint: how many
	// corridorPieces [sourceEdgeCorridorPieces] produced, and how many survived
	// [PruneToNearestCanonEdges].
	//
	// It fires for EVERY source edge, including a zero-length one that mints
	// nothing — reported as an explicit (0,0) rather than by silence, so a
	// missing call can never be read as a legitimate zero.
	//
	// ⚠ P_e, the quantity the solve's memory grows as, is KEPT pieces at source
	// edge e summed over ALL candidates — not minted, and not per candidate. The
	// solve's layers are shared across candidates, so a per-candidate sum answers
	// a different question.
	SourceEdgeMinted(srcEdge, minted, kept int)

	// MintFinished is the phase event: mint has finished, the solve has not
	// started. A live-memory probe splits its reading here, and a second
	// cost report would go at the same point.
	//
	// It fires once for every leg that reaches the mint→solve boundary, which is
	// a SUPERSET of the legs that solve: a leg whose points all coincide has
	// total == 0, and [SolveCorridorPath] returns nil there before doing any
	// work. Such a leg reports a phase event and no SolveFinished.
	MintFinished()

	// SolveFinished reports the Dijkstra table and the search's own work.
	// relaxations counts calls to the solver's relax closure (edges examined);
	// pqPushes counts every improvement that reaches the search heap, an insert
	// or a decrease, so it excludes the seeded start item; pqInserts counts the
	// inserts alone; and pqPops counts pops. The heap holds each state at most
	// once and every pop is live, so a completed solve has
	// pqPops == pqInserts + 1 (the seed), and pqPops is the number of states
	// the search reached.
	//
	// ⚠ pqPops USED TO COUNT STALE POPS TOO, and the identity was
	// pqPops == pqPushes + 1. That was the lazy queue, which pushed a duplicate
	// item on every improvement; it was replaced by an indexed heap, and pqPushes kept its meaning
	// (improvements) so its values carried over unchanged.
	//
	// A CANCELLED solve reports too, then returns the error. It is the only
	// way relaxations can be read at the moment of the stop, and the
	// cancellation tests assert the stop bound from this counter rather than from wall-clock time.
	// A cancelled solve is a leg that STOPPED, and its cost up to the stop is a
	// real number; pqPops == pqInserts + 1 does not hold on such a report.
	SolveFinished(layers, states, relaxations, pqPushes, pqInserts, pqPops int)

	// LegFinished closes the record. A leg that FAILS delivers nothing: an
	// error aborts the whole import, so there is no partial leg to report. A
	// cancelled leg is the same — it never reaches this call, so a record left
	// open is exactly how a census reader sees a stop.
	LegFinished()
}

// CorridorFollowRuns runs the corridor solve (intervals, Dijkstra and
// bridges) for one leg and converts its result into [FollowRun]s.
//
// R and k are [CorridorRadiusM]/[EmissionK] (corridorcost.go) — NOT
// b.StayM/b.EnterM, the older matcher's bands, which are deliberately not
// aliased to the new constants. b is threaded through for the
// degenerate-leg contract only; the solve itself is band-free.
//
// ctx stops the leg part-way. It is threaded
// down to the three places that can run unboundedly long — the mint's source-edge
// loop, [BuildCorridorBridges] and the Dijkstra loop — and a cancellation comes
// back as a `%w`-wrapped [context.Canceled], never as a partial or empty result.
// The caller cancels it, for example on memory pressure or a user's cancel.
func CorridorFollowRuns(ctx context.Context, pts []Point, canon []CanonArc, b FollowBands) ([]FollowRun, error) {
	// Degenerate-leg contract: an empty leg has no runs, and a one-point leg
	// is one OFF run that commits that point verbatim.
	if len(pts) == 0 {
		return nil, nil
	}
	if len(pts) == 1 {
		return []FollowRun{{Kind: RunOff}}, nil
	}

	sa, err := NewSourceMeasure(pts)
	if err != nil {
		return nil, err
	}
	prepped := PrepCandidates(canon)
	// Cost census. Nil in production; the counting loop below runs
	// only when a test has armed the hook.
	if obs := CorridorCostObserverForTests; obs != nil {
		n := 0
		for _, pc := range prepped {
			if pc != nil {
				n++
			}
		}
		obs.LegStart(sa.Edges(), n)
	}
	ivs, err := CorridorIntervals(ctx, sa, prepped, CorridorRadiusM)
	if err != nil {
		return nil, err
	}
	// The phase event, placed EXACTLY here: every mint allocation has been made
	// and released, and no solve allocation has been made yet. A leg with
	// total == 0 still passes this point and then gets no solve — see
	// [corridorCostObserver.MintFinished].
	if obs := CorridorCostObserverForTests; obs != nil {
		obs.MintFinished()
	}
	// ivs is HANDED OVER here: [SolveCorridorPath] releases every
	// ivs[i].Pieces once its bridges are priced, so nothing below this line may read them — and nothing does: the
	// runs loop works from res and prepped only. That is the invariant the
	// release rests on.
	res, err := SolveCorridorPath(ctx, sa.Total(), sa, ivs, prepped, CorridorRadiusM, EmissionK)
	if err != nil {
		return nil, err
	}
	if obs := CorridorCostObserverForTests; obs != nil {
		obs.LegFinished()
	}
	if res == nil {
		// total <= 0 (every point coincides) — [SolveCorridorPath]'s own
		// expected-absence contract (its total==0 branch). Nothing to
		// follow; the whole leg commits verbatim, as it always would have.
		return []FollowRun{{Kind: RunOff, Start: sa.Start(), End: sa.End()}}, nil
	}

	out := make([]FollowRun, 0, len(res.Runs))
	for _, run := range res.Runs {
		if run.State.Off {
			// A degenerate (sub-solverEps) OFF run is dropped, not committed
			// — the OFF-side twin of the ON-side skip below. Found via
			// TestChaosImport_Invariants and two tests since deleted (it
			// regressed with earlier fixes
			// in this file): a leg's very first
			// breakpoint arc can be, at the solver's own eps scale
			// (corridorsolve.go's own layering), a genuine but vanishingly
			// thin OFF sliver — measured [edge:0 frac:0] to
			// [edge:0 frac:1.2686268194537227e-16], i.e. 0 import-measure
			// metres. Left in, it commits ONE verbatim point
			// (sourceRunVertexRange still resolves a first==last vertex for
			// it) immediately before the leg's real ON content, and that
			// point survives into the assembled leg's own [Points] even
			// though the run that produced it carries no real geometry —
			// pushing the true first ON piece to point index 1 or 2 instead
			// of 0 while [AssembleFollowedLegRunsCorridor]'s own atStart
			// still (correctly) reads this as the leg's own measure-0 start,
			// since sub-eps error only moved WHICH FollowRun claims measure 0,
			// not whether anything does. LegSeamsFromAlign's own assertion
			// (legpieces.go: "ON piece 0 is flagged terminal at its start
			// but begins at point %d, not 0") is what catches the
			// resulting mismatch — a real invariant, not an overly strict
			// check; the fix is not committing the phantom point in the
			// first place.
			if run.ImportMeasureHi-run.ImportMeasureLo <= solverEps {
				continue
			}
			start, err := sa.PosAtMeasure(run.ImportMeasureLo)
			if err != nil {
				return nil, fmt.Errorf("CorridorFollowRuns: OFF run start: %w", err)
			}
			end, err := sa.PosAtMeasure(run.ImportMeasureHi)
			if err != nil {
				return nil, fmt.Errorf("CorridorFollowRuns: OFF run end: %w", err)
			}
			out = append(out, FollowRun{Kind: RunOff, Start: start, End: end})
			continue
		}

		// The solver's state NAMES the piece this run rides, so its oriented
		// canon measure at both ends is already exact — see [CorridorRun].
		//
		// This is where coalescePieces used to run, and it is gone. It existed
		// to resolve an interval's competing pieces into one correspondence by
		// a greedy left-to-right rule; that resolution is now the solve's own,
		// priced by [TransitionCostT]'s canon-measure discontinuity term rather
		// than decided by list position. A bridged run needs no special arm
		// any more either — it has no covering piece, which is the point of
		// bridging, and its canon measure arrives on the run exactly as a
		// non-bridged one's now does.
		if run.CandIdx < 0 || run.CandIdx >= len(prepped) || prepped[run.CandIdx] == nil {
			return nil, fmt.Errorf("CorridorFollowRuns: ON run [%.6g,%.6g] names candidate %d, which is absent or degenerate — internal inconsistency",
				run.ImportMeasureLo, run.ImportMeasureHi, run.CandIdx)
		}
		if run.ImportMeasureHi-run.ImportMeasureLo <= solverEps && !run.ChainHop {
			// Sub-eps run: the ON-side twin of the OFF skip above. Carries no
			// geometry to commit at the solver's own layering scale.
			//
			// A CHAIN-HOP run is not one of these, although it has no import
			// extent either: it carries the canon a same-layer switch chain
			// paid for across an intermediate candidate, and dropping it is
			// exactly the defect it exists to fix ([CorridorRun.ChainHop]).
			continue
		}
		fr, err := corridorFollowRunFromMeasure(sa, prepped[run.CandIdx], run.CandIdx, run.ImportMeasureLo, run.ImportMeasureHi, run.CanonMeasureLo, run.CanonMeasureHi)
		if err != nil {
			return nil, fmt.Errorf("CorridorFollowRuns: run: %w", err)
		}
		fr.ChainHop = run.ChainHop
		out = append(out, fr)
	}
	return MergeAdjacentSameCandidateRuns(out), nil
}

// MergeAdjacentSameCandidateRuns merges consecutive ON followRuns that share
// a candidate and direction and whose canon measure touches across the boundary
// — the SAME "one seam per genuine change, not per internal boundary"
// principle the solver applies within one solver run (its state names the
// piece — the deleted coalescePieces did it after the fact), extended across
// solver-RUN boundaries too. Within one run cannot see this: a
// BRIDGE run is emitted directly via [corridorFollowRunFromMeasure], one
// FollowRun per bridge, with no look-ahead at whatever solver run continues
// the SAME (interval, direction) state right after it — and it always does
// continue when the bridge fires at all, because a bridge only ever lands AT
// the target interval's own near boundary, which is exactly where that
// interval's own walk resumes.
//
// Measured on one real leg: of 48 solver runs, 18
// were bridged, and ALL 18 were immediately followed by a non-bridged
// continuation of the identical (interval, direction) state — each pair
// committing as two followRuns and so two spurious seams, on top of what
// coalescePieces already fixed. FollowRun itself carries no Bridged field,
// and AssembleFollowedLegRunsCorridor draws MeasureStart->MeasureEnd on the
// candidate identically either way, so a bridge's own canon measure touching its
// continuation's is exactly as mergeable as two pieces coalescePieces
// already merges within one run — this is that same merge, applied one
// level up, over CorridorFollowRuns' own output rather than one run's own
// pieces.
func MergeAdjacentSameCandidateRuns(runs []FollowRun) []FollowRun {
	if len(runs) == 0 {
		return runs
	}
	out := make([]FollowRun, 0, len(runs))
	out = append(out, runs[0])
	for _, r := range runs[1:] {
		last := &out[len(out)-1]
		// THE REFUSAL BELOW TESTS THE MERGED SPAN, NOT THE INPUTS' DEGENERACY.
		// It refuses exactly one thing: a merge whose RESULT would have zero
		// canon extent (merged MeasureStart == merged MeasureEnd). That is the
		// property to protect, and the shape below is still refused.
		//
		// THE SHAPE: a leg that approaches ONE canon vertex repeatedly from
		// several unrelated points along its own length commits as several
		// genuinely separate solver runs, every one clamped to the SAME
		// degenerate canon measure (the nearest vertex, never a real edge
		// interior). Each such run trivially "touches" the next (a constant
		// equals itself, so MeasureEnd == MeasureStart holds repeatedly), so
		// a naive merge collapsed five of them into ONE FollowRun spanning the
		// leg's entire ~300-edge import-measure length at a ZERO-width canon
		// span — assembleOnRun draws canon vertices strictly BETWEEN
		// MeasureStart and MeasureEnd, and there are none between a position
		// and itself, so the committed piece held only its own two
		// (identical-node) terminal anchors: "2 point(s), begins and ends on
		// the same node, no interior vertex to split at", which the assembler
		// rejects. The bridge+continuation case this function exists for
		// always has REAL canon movement on the continuation side (it walks an
		// actual interval); merging two mutually-degenerate runs is the
		// failure mode, not the target one.
		//
		// An earlier form refused "when BOTH INPUTS are zero-width". When
		// [noDirection] had only its zero-width clause, the two readings
		// coincided. A SECOND clause was then added (a reversal contained in
		// one source edge is a projection artefact, not a traversal), and
		// "both degenerate" silently grew to cover pairs with real canon
		// extent on both sides. Measured on a 19-file test corpus: exactly
		// ONE pair was refused there, and it was one of those — a 135.6 m
		// reversed run inside source edge 0 followed by its own 14.0 m
		// bridge, both flagged only by the second clause. The refusal minted
		// a spurious degree-2 node. Neither run is zero-width and their merge
		// has 149.6 m of canon extent, so the zero-extent rule was never in
		// question for it.
		//
		// The two tests are NOT equivalent, in both directions:
		//   - two zero-width runs at the SAME clamped vertex (the
		//     five-run chain above) merge to zero extent and are still refused;
		//   - two zero-width runs at DIFFERENT measures now merge when the pair
		//     ASCENDS, and correctly: the result spans real canon between two
		//     distinct anchors, which is precisely what assembleOnRun asks for.
		//     Only ascending, because a zero-width run reads Reversed=false
		//     (canonHi<canonLo is false for a constant), so strideReversed is
		//     forward and `continues` demands r.MeasureStart >= last.MeasureEnd:
		//     a stall at 100 then one at 140 merges, 140 then 100 stays two
		//     runs and its seam survives. (No such pair occurs on any corpus we
		//     measure — the count is 0 on every test corpus, in
		//     either order.)
		//   - a pair with only ONE degenerate side whose merge would land back
		//     on its own start is now refused, where before it was allowed
		//     through and would have committed the zero-extent piece
		//     described above.
		// EXACT, no tolerance. Both questions are structural, not
		// measurements: "did this run move along canon at all" and "is A's
		// canon end B's canon start". Both sides come from
		// [corridorFollowRunFromMeasure] over the SAME interval's pieces, so
		// where the answer is yes it is yes to the last bit — measured on
		// a 19-file test corpus, replacing the 1e-9 comparisons here
		// with these changed no committed geometry at all.
		// A RUN HAS A DIRECTION ONLY IF THE WALK GAVE IT ONE. Two shapes fail
		// that test, and both mint nodes on stretches where nothing divides:
		//
		//   ZERO WIDTH   no canon measure travelled at all, so there is no
		//                direction to read — but Reversed is canonHi<canonLo,
		//                which is false, so every stall reads FORWARD.
		//
		//   REVERSAL INSIDE ONE SOURCE EDGE   a source edge is a straight line
		//                between two recorded points. A walker cannot double
		//                back along it. The projected foot can and does — it
		//                slides backwards as the source approaches canon at an
		//                angle, then advances once past the closest point — and
		//                the committed line then draws that stretch of canon
		//                twice. Measured on a 19-file test corpus: 3 such
		//                runs, 21.48 m drawn twice, and between them the last
		//                unjustified node in the network.
		//
		// A FORWARD single-edge run is NOT suspect: a sparse GPX can cover a lot
		// of canon forward on one edge, and that is ordinary. The asymmetry is
		// real — going forward needs no turn, going back needs two.
		//
		// ⚠ AND A GENUINE OUT-AND-BACK MUST SURVIVE THIS. A spur walked up and
		// back down is a real reversal, and it spans MANY source edges in both
		// directions because the walker had to travel out and return. That is
		// what separates it from the artefact, and it is why the fix is here
		// rather than in the cost model: BacktrackCost charges measure travelled
		// AGAINST a run's declared direction, so a run that declares itself
		// reversed and goes backwards is charged nothing — correctly, because a
		// real spur should be cheap. The defect is not that reversals are
		// underpriced; it is that this one is not a reversal.
		//
		// ⚠ A HIGHLIGHT-HOP RUN IS NEITHER, and is exempt from the second clause.
		// It has no import extent at all, so it lies inside one source edge by
		// construction, and a REVERSED one would read as a projection artefact
		// — after a same-candidate forward run, the merge below would then
		// clamp its canon away without a word. Its direction is not read off
		// the source: it is the direction the chain's switches paid for
		// ([CorridorRun.ChainHop]).
		noDirection := func(fr FollowRun) bool {
			if fr.MeasureEnd == fr.MeasureStart {
				return true
			}
			return fr.Reversed && fr.Start.Edge == fr.End.Edge && !fr.ChainHop
		}
		lastDegenerate := noDirection(*last)
		rDegenerate := noDirection(r)
		// A DEGENERATE RUN HAS NO DIRECTION, AND Reversed LIES ABOUT IT.
		// Reversed is canonHi<canonLo, which is false for a zero-width span, so
		// every stall reads FORWARD. Drop one into the middle of a REVERSE ride
		// and the direction test below refuses on both sides, splitting one
		// continuous follow into three — and each seam cuts canon and mints a
		// node. On a 19-file test corpus, 21 of its 61 ON runs were degenerate and 42 of its 67 run
		// boundaries sat next to one.
		//
		// A stall is legitimate geometry — the source rounds a corner canon
		// takes as a sharp vertex, so the foot point stops while the source
		// keeps moving. It is the DIRECTION READ off it that is wrong, so the
		// fix is to take the direction from whichever side actually has one
		// rather than to suppress the run. A pair of stalls at ONE vertex
		// still merges to nothing and is still refused — by the merged-span
		// test below, not by counting degenerate inputs.
		sameDirection := last.Reversed == r.Reversed || lastDegenerate || rDegenerate
		strideReversed := last.Reversed
		if lastDegenerate {
			strideReversed = r.Reversed
		}
		// CONTINUES, not "ends exactly where the next begins". When the source
		// runs OFFSET from canon, its nearest foot genuinely jumps at a bend
		// bisector — from part-way along one canon edge to part-way along the
		// next — because the source cuts the corner. The line still continues
		// along canon there, so the canon between the two feet (including the
		// bend vertex itself) belongs to it: requiring equality committed the
		// two sides as SEPARATE arcs and left that vertex drawn by
		// neither, which is the vertex-skipping seam this whole change is
		// about. Measured on a 19-file test corpus, one such seam skipped
		// 14.64 m.
		//
		// The test is measure ORDER, exact and with no tolerance: forward runs
		// must not step back, reverse runs must not step forward. A run that
		// would have to reverse is a genuine turnaround and stays separate.
		continues := false
		if strideReversed {
			continues = r.MeasureStart <= last.MeasureEnd
		} else {
			continues = r.MeasureStart >= last.MeasureEnd
		}
		if last.Kind == RunOn && r.Kind == RunOn &&
			last.CanonIdx == r.CanonIdx && sameDirection && continues {
			// THE MERGED RUN MUST BE WELL-FORMED IN ITS OWN DECLARED DIRECTION.
			// Absorbing a directionless run keeps last.MeasureStart, and when that
			// start sits AHEAD of where the continuation ends, the result is a
			// run labelled forward whose measures descend — which assembleOnRun
			// refuses outright ("forward measures not ascending: [52.13 42.52]";
			// four randomized test seeds, all HARD import failures, found the
			// first time this rule ran).
			//
			// The shape it rejects is real, not a rounding edge: a leg rejoins
			// canon, the foot slides back, and the continuation then does NOT
			// travel past the rejoin. The directionless run is covering canon
			// the continuation never reaches, so folding it away would lose
			// ground the leg actually rode. Leave those two runs alone — the
			// seam stays, and so does its node, which is the honest outcome.
			newEnd := r.MeasureEnd
			if strideReversed && newEnd > last.MeasureEnd {
				newEnd = last.MeasureEnd
			}
			if !strideReversed && newEnd < last.MeasureEnd {
				newEnd = last.MeasureEnd
			}
			// THE MERGED SPAN MUST HAVE CANON EXTENT (the property above,
			// tested on the result). assembleOnRun draws canon vertices
			// strictly BETWEEN MeasureStart and MeasureEnd, and there are none
			// between a position and itself: a zero-extent merged run commits a
			// piece of "2 point(s), begins and ends on the same node, no
			// interior vertex to split at", which fails the whole import.
			// EXACT, no tolerance — but not bit-exact by construction, and the
			// limit is worth stating. newEnd is one run's MeasureEnd and
			// last.MeasureStart is a DIFFERENT run's far end; the two are
			// bit-equal only when both are vertex-clamped copies of the same
			// cumMeasure (the RegimeNearA/RegimeNearB paths). Where either is
			// INTERPOLATED along an edge, `==` can miss by an ulp and the
			// refusal will not fire. We keep `==` anyway: it is the same
			// semantics [noDirection]'s zero-width clause has always had, so
			// this is no weaker than what it replaces, and the populations it
			// catches (vertex stalls, and merges landing back on a clamped
			// start) are exactly the clamped ones. A tolerance here would start
			// refusing merges with real, small canon extent, which is the
			// defect this rule was loosened to stop.
			if newEnd == last.MeasureStart {
				out = append(out, r)
				continue
			}
			if strideReversed != (newEnd < last.MeasureStart) {
				out = append(out, r)
				continue
			}
			last.End = r.End
			// THE MEASURE ONLY EVER EXTENDS THE WAY THE STRIDE IS GOING. Absorbing
			// a directionless run must not drag the end BACKWARDS: a trailing
			// single-edge reversal would otherwise pull a forward stretch's end
			// back by its own artefact distance, committing the same double-draw
			// this rule exists to remove. For a zero-width run the two ends are
			// equal and this is a no-op, which is why it was not needed before.
			last.MeasureEnd = newEnd
			// The merged stride takes the direction of whichever side had one.
			last.Reversed = strideReversed
			last.ChainHop = last.ChainHop || r.ChainHop
			continue
		}
		out = append(out, r)
	}
	return out
}

// corridorFollowRunFromMeasure builds one ON [FollowRun] for the source stretch
// [importLo,importHi], bound to candidate candIdx over the ORIENTED canon measure
// [canonLo,canonHi] (may run high-to-low; not pre-sorted, matching
// [CorridorPiece]'s and [CorridorBridge]'s own convention).
//
// Reversed is read off the measure pair itself, never off a state's declared
// direction: a declared-FWD run CAN move backward within one piece (that is
// exactly what the backtrack term prices), so only the measure pair's own order
// tells [assembleOnRun]'s measure-monotone post-condition the truth it needs.
func corridorFollowRunFromMeasure(sa *SourceMeasure, pc *PreppedCandidate, candIdx int, importLo, importHi, canonLo, canonHi float64) (FollowRun, error) {
	start, err := sa.PosAtMeasure(importLo)
	if err != nil {
		return FollowRun{}, fmt.Errorf("start: %w", err)
	}
	end, err := sa.PosAtMeasure(importHi)
	if err != nil {
		return FollowRun{}, fmt.Errorf("end: %w", err)
	}
	return FollowRun{
		Kind:         RunOn,
		Start:        start,
		End:          end,
		CanonID:      pc.arcID,
		CanonIdx:     candIdx,
		Reversed:     canonHi < canonLo,
		MeasureStart: canonLo,
		MeasureEnd:   canonHi,
	}, nil
}
