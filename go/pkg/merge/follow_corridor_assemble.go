package merge

// follow_corridor_assemble.go — the assembler for corridor-solve runs
// ([CorridorFollowRuns], corridorwire.go).
//
// Corridor-solve runs carry exact analytic boundaries (interval/piece edges
// from corridorinterval.go, bridge endpoints) with no relationship to any resampling grid, so
// this assembler walks [pts]/[SourceMeasure] directly. [assembleOnRun] derives
// its output entirely from `pc`, `run.MeasureStart/End`,
// `entryCross/exitCross` and the leg's own terminal points, never from source
// geometry.
//
// ── THE CROSSING VERTEX ─────────────────────────────────────────────────────
//
// At every kind-A boundary (an OFF neighbour on that side — see
// corridor_boundary.go's A/B/C/D taxonomy) this mints the crossing VERTEX
// itself: the source point at the run's own Start/End, with lat/lon and
// elevation interpolated (nil-propagating) — the same point
// the corridorBoundaryPoint test helper builds from the boundary
// measure. The interval solve already certifies the position, so there is no
// search and no resolution constant. Measured, not assumed, why the vertex
// matters: dropping it and committing a straight chord from the OFF run's last
// verbatim point to the ON run's first on-canon point moves the seam ≈12.6 m
// on a fixture departing 40 m over 100 m at R=15
// (TestCorridorBoundaryPoint_DroppingItMovesTheSeam) — comparable to R itself.
// Kinds B/C/D have no crossing vertex.
//
// ── THE NODE-SNAP GATE ──────────────────────────────────────────────────────
//
// [checkCorridor] plus [snapMeasureToNearbyNodeCensus], kept deliberately.
// This is NOT inert in production — its first
// mechanism, snapping onto a candidate's OWN endpoint, is a pure function of
// the measure alone and fires with no node lookup at all; only its second
// mechanism (any existing network node) is gated on `lookup` and inert while
// [followLegAgainstCanon] passes noNodeLookup. [checkCorridor] needs a source
// path, so this file builds a densified source slice per run
// (densifyStep(b)), never for the whole leg.

import (
	"errors"
	"fmt"
	"math"
)

// sourceRunVertexRange returns the inclusive range of ORIGINAL vertex
// indices whose own position — vertexPos(i): (i,0), or (edges-1,1) for the
// final vertex, [SourceMeasure.vertexPos]'s own convention — falls within
// [lo,hi]. first is the smallest such index, last the largest; first > last
// means no original vertex falls in the range at all, ordinary for a
// corridor-solve boundary that is not vertex-aligned (a sub-edge stretch).
func sourceRunVertexRange(lo, hi SourcePos) (first, last int) {
	first = lo.Edge
	if lo.Frac > 0 {
		first++
	}
	last = hi.Edge
	if hi.Frac >= 1 {
		last++
	}
	return first, last
}

// AssembleFollowedLegRunsCorridor assembles one leg from its partition into
// [AssembledLeg]: the committed points, the effective runs, and the OnPieces
// bookkeeping [AssembledLeg.checkOnPieceRanges] verifies. Runs' Start/End are
// exact analytic SourcePos. See this file's header.
//
// Exported for the reference application's tests; not part of the stable API.
func AssembleFollowedLegRunsCorridor(pts []Point, canon []CanonArc, runs []FollowRun, b FollowBands, lookup NodeLookup) (AssembledLeg, error) {
	if lookup == nil {
		return AssembledLeg{}, errors.New("AssembleFollowedLegRunsCorridor: nodeLookup is nil — pass noNodeLookup if this caller has no node access")
	}
	if len(pts) < 2 {
		return assembleDegenerateLeg(pts, runs)
	}
	sa, err := NewSourceMeasure(pts)
	if err != nil {
		return AssembledLeg{}, err
	}
	prepped := PrepCandidates(canon)
	legFirst, legLast := pts[0], pts[len(pts)-1]

	var out []Point
	prevON := false
	eff := make([]FollowRun, 0, len(runs))
	var onPieces []OnPieceRange

	for r := range runs {
		run := runs[r]
		startMeasure, err := sa.MeasureAt(run.Start)
		if err != nil {
			return AssembledLeg{}, fmt.Errorf("AssembleFollowedLegRunsCorridor: run %d start: %w", r, err)
		}
		endMeasure, err := sa.MeasureAt(run.End)
		if err != nil {
			return AssembledLeg{}, fmt.Errorf("AssembleFollowedLegRunsCorridor: run %d end: %w", r, err)
		}
		// EXACT: this is an error check on a run the solver built with
		// End at or after Start, not a measurement. Slack here would only
		// let a genuinely inverted run through.
		if endMeasure < startMeasure {
			return AssembledLeg{}, fmt.Errorf("AssembleFollowedLegRunsCorridor: run %d spans [%.6g,%.6g] on the leg — end precedes start", r, startMeasure, endMeasure)
		}
		// The run's own two boundary points. Hoisted above the OFF branch
		// because atStart/atEnd below are decided on them; the ON path
		// downstream reuses these same two values rather than re-deriving
		// them (see the densifySource block).
		startPt, err := sa.PointAt(run.Start)
		if err != nil {
			return AssembledLeg{}, fmt.Errorf("AssembleFollowedLegRunsCorridor: run %d start point: %w", r, err)
		}
		endPt, err := sa.PointAt(run.End)
		if err != nil {
			return AssembledLeg{}, fmt.Errorf("AssembleFollowedLegRunsCorridor: run %d end point: %w", r, err)
		}

		// atStart/atEnd feed [assembleOnRun]'s terminal-substitution rule —
		// see that function's own doc comment.
		//
		// EXACT, NO TOLERANCE, and asked as two FACTS rather than as a
		// measurement. The question is not "is this measure value near zero"; it
		// is "is this the leg's FIRST run, and does it begin ON the leg's own
		// first point". The run index answers the first half; [samePoint] —
		// plain float equality on lat/lon — answers the second.
		//
		// Why the measure form needed a tolerance, and why this one does not.
		// The solver's breakpoints inherit floating-point noise from the band
		// solve's root find: measured on one randomized test seed, a piece boundary
		// landed at source-edge-local T = 3.62e-15 instead of 0, so the
		// leg's first run began at import measure 1.51e-14 m rather than at 0 and
		// an exact `startMeasure == 0` read it as an interior run — minting a
		// fresh terminal node a hair off the leg's own first point instead of
		// adopting it, which surfaced as a split ring. At that scale the
		// RESOLVED point is bitwise identical to pts[0] (an increment of
		// ~4e-20 degrees against an ulp of ~7e-15 at this latitude), so the
		// structural question answers cleanly exactly where the metric one
		// could not. Noise in a root solve is real noise — unlike the
		// coincidence tests elsewhere in this path, which were asking a
		// structural question with a float and are now asked structurally.
		//
		// The run-index half is load-bearing on a CLOSED LOOP, where the
		// leg's last point equals its first: without it the final run would
		// read as starting at the leg's start and take the substitution.
		atStart := r == 0 && samePoint(startPt, legFirst)
		atEnd := r == len(runs)-1 && samePoint(endPt, legLast)

		if run.Kind == RunOff {
			// Verbatim — every ORIGINAL source point in the run's range; no
			// fractional boundary point is manufactured. OFF commits only
			// vertices the source actually had.
			first, last := sourceRunVertexRange(run.Start, run.End)
			for i := first; i <= last; i++ {
				out = append(out, pts[i])
			}
			prevON = false
			eff = append(eff, run)
			continue
		}

		if run.CanonIdx < 0 || run.CanonIdx >= len(prepped) || prepped[run.CanonIdx] == nil {
			return AssembledLeg{}, fmt.Errorf("AssembleFollowedLegRunsCorridor: ON run %d bound to absent/degenerate candidate idx %d (arc %d)",
				r, run.CanonIdx, run.CanonID)
		}
		pc := prepped[run.CanonIdx]
		rr := run

		// The run's own source stretch, boundary to boundary, at densifyStep(b)
		// resolution — built per run, never for the whole leg. A
		// duplicate point where a boundary lands exactly on a vertex is
		// harmless (densifySource treats it as a zero-length edge and
		// inserts nothing extra) and not worth guarding against.
		first, last := sourceRunVertexRange(run.Start, run.End)
		runVerts := make([]Point, 0, last-first+3)
		runVerts = append(runVerts, startPt)
		for i := first; i <= last; i++ {
			runVerts = append(runVerts, pts[i])
		}
		runVerts = append(runVerts, endPt)
		src, _ := densifySource(runVerts, densifyStep(b))

		// Kind-A crossing vertices — an OFF neighbour on that side, and not
		// the leg's own terminal (a leg terminal is kind D, never kind A:
		// corridor_boundary.go's taxonomy). This IS
		// corridorBoundaryPoint (test-only): that function is exactly
		// sa.pointAt(sa.posAtMeasure(importMeasure)), and startPt/endPt above are
		// already sa.pointAt(run.Start)/sa.pointAt(run.End) — the same
		// lat/lon-plus-lerped-elevation construction, one measure round-trip
		// cheaper since run.Start/End are already the position
		// corridorBoundaryPoint would have to re-derive from a measure value.
		// Reusing them here, rather than reconstructing via
		// corridorBoundaryPoint(sa, startMeasure)/(sa, endMeasure), is the same
		// point with less floating-point surface, not a different one.
		// Unconditional where it applies: this cannot decline — the interval
		// solve already certifies the position, so there is no accept/reject
		// step left to make.
		var entryCross, exitCross *Point
		if r > 0 && runs[r-1].Kind == RunOff {
			cp := startPt
			entryCross = &cp
		}
		if r+1 < len(runs) && runs[r+1].Kind == RunOff {
			cp := endPt
			exitCross = &cp
		}

		// The node-snap gate — see
		// [snapMeasureToNearbyNodeCensus] and [checkCorridor] (follow_core.go).
		// Gated on entryCross/exitCross still being nil: a kind-A boundary
		// already has its answer above, so the snap only gets a look at what
		// kind-A left unanswered (an ON<->ON seam, or a run abutting another
		// ON run).
		{
			origStart, origEnd := rr.MeasureStart, rr.MeasureEnd
			snapStart, snapEnd := origStart, origEnd
			var snapErr error
			if r > 0 && entryCross == nil {
				snapStart, _, snapErr = snapMeasureToNearbyNodeCensus(pc, rr.MeasureStart, lookup)
				if snapErr != nil {
					return AssembledLeg{}, snapErr
				}
			}
			if r+1 < len(runs) && exitCross == nil {
				snapEnd, _, snapErr = snapMeasureToNearbyNodeCensus(pc, rr.MeasureEnd, lookup)
				if snapErr != nil {
					return AssembledLeg{}, snapErr
				}
			}
			trialStart, trialEnd := reconcileSnappedMeasures(origStart, origEnd, snapStart, snapEnd, run.Reversed)
			attempted := trialStart != origStart || trialEnd != origEnd
			accepted := false
			if attempted {
				// ⚠ CorridorRadiusM, NOT b.StayM. This is the LIVE assembler and
				// its corridor check is the corridor matcher's own R, whose one
				// definition site is corridorcost.go. b.StayM is the COMB's
				// hysteresis band, and corridorcost.go says so directly: R is
				// deliberately not aliased to FollowBands.StayM.
				//
				// The two values differ: CorridorRadiusM is 10 and StayM is
				// 15, so this check reads nothing from FollowBands.StayM.
				accepted = checkCorridor(src, 0, pc, trialStart, trialEnd, entryCross, exitCross, run.Reversed, CorridorRadiusM) == nil
				if accepted {
					rr.MeasureStart, rr.MeasureEnd = trialStart, trialEnd
				}
			}
		}

		runPts, err := assembleOnRun(legFirst, legLast, pc, rr, b, entryCross, exitCross, atStart, atEnd)
		if err != nil {
			return AssembledLeg{}, err
		}
		runIdx := len(eff)
		eff = append(eff, rr)
		if entryCross != nil {
			out = append(out, *entryCross)
		}

		// The SOLE dedup: an ON<->ON seam — same rule as the other two
		// assemblers.
		start := 0
		if prevON && len(out) > 0 && len(runPts) > 0 {
			lastOut := out[len(out)-1]
			if lastOut.Lat == runPts[0].Lat && lastOut.Lon == runPts[0].Lon {
				start = 1
			}
		}
		lo := len(out) - start
		out = append(out, runPts[start:]...)
		hi := len(out) - 1
		onPieces = append(onPieces, OnPieceRange{
			Run: runIdx, Lo: lo, Hi: hi,
			TerminalStart: atStart && !samePoint(legFirst, boundaryPoint(pc, rr.MeasureStart)),
			TerminalEnd:   atEnd && !samePoint(legLast, boundaryPoint(pc, rr.MeasureEnd)),
		})
		if exitCross != nil {
			out = append(out, *exitCross)
		}
		prevON = true
	}

	if len(out) == 0 {
		return AssembledLeg{}, errors.New("AssembleFollowedLegRunsCorridor: empty output")
	}
	for i, p := range out {
		if math.IsNaN(p.Lat) || math.IsNaN(p.Lon) || math.IsInf(p.Lat, 0) || math.IsInf(p.Lon, 0) {
			return AssembledLeg{}, fmt.Errorf("AssembleFollowedLegRunsCorridor: non-finite output point %d: %+v", i, p)
		}
	}
	res := AssembledLeg{Points: out, Runs: eff, OnPieces: onPieces}
	if err := res.checkOnPieceRanges(); err != nil {
		return AssembledLeg{}, err
	}
	return res, nil
}
