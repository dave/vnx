package merge

// corridorsolve.go — the shortest path over (interval, direction) states,
// layered on import measure.
//
// States are OFF, plus one state per (CorridorInterval, direction) pair —
// candidate granularity would make self-approach arm-jumps free and give
// direction no per-metre meaning. Interval granularity ([CorridorInterval], one per candidate per
// contiguous-or-self-approaching stretch) fixes both.
//
// The import measure [0, total] is partitioned by every interval's own
// ImportMeasureLo/Hi AND every PIECE boundary into elementary arcs. Within one
// arc the set of intervals covering it is constant (an interval's own
// boundary IS a breakpoint), so a state is either fully available across the
// whole arc or not available at all.
//
// ⛔ PIECE-LEVEL BREAKPOINTS ARE LOAD-BEARING FOR CORRECTNESS. Do not remove
// them as an optimisation. This comment used to say the opposite — "no
// piece-level breakpoints are needed for correctness; an arc's cost sums
// whatever pieces of the interval overlap it" — and both halves were wrong.
// They are corrected here rather than deleted, because the stale
// version licensed exactly the change that reintroduces the bug below.
//
// TWO reasons they are required, found independently:
//
//  1. A piece's canon measure is monotonic, so a direction change (an out-and-back's
//     turnaround, a hairpin's apex) can ONLY happen where one piece ends and the
//     next begins. Without a layer there, case-1 switches have nowhere to fire
//     mid-interval — measured: an out-and-back priced at P + full backtrack
//     (149.9) instead of min(P, backtrack) (~50).
//
//  2. [ArcCost] charges ONE piece, not the sum. That is only
//     correct because a piece boundary is always a breakpoint, so every piece
//     clipping an elementary arc covers the WHOLE of it — picking one is
//     picking among equals-in-extent. Drop piece breakpoints and contiguous
//     pieces would partially span an arc, so charging only the first would
//     silently UNDER-charge — trading the over-charge that caused non-convergent
//     re-import for an under-charge of the opposite sign.
//
// Why one and not the sum: pieces of ONE interval legitimately overlap on a
// self-approach, and the solver rides exactly one canon position at a time.
// Summing charged for every place canon came near — measured ~0.5/m on geometry
// sitting exactly ON canon, against OFF's 1/m — which made all-OFF globally
// cheapest and each re-import commit another full copy of the plan.
// [canonMeasureAtImportMeasure] and AssembleFollowedLegRunsCorridor both resolve overlap
// by first-piece-in-list-order-wins; ArcCost must agree with them, or the
// cost function prices geometry the assembler will not draw.
//
// The graph is layered: forward edges carry a state across one arc at
// the emission+backtrack cost (corridorcost.go), priced from the SINGLE piece of the interval
// that covers the arc (see [ArcCost] — a self-approach can leave
// more than one piece covering the same arc, and they are alternative
// descriptions of the same geometry, not independent quantities to add);
// switch edges, at zero import-measure cost but a real solver cost, let the path
// change state at a breakpoint. Three switch cases, in order of specificity:
//
//  1. Same interval, direction flip — P ("pays P once to flip", the
//     out-and-back recovery rule as arithmetic).
//  2. Different interval, SAME candidate (self-approach) — the raw canon-measure
//     gap between the two arms at the switch point, backtrack-weighted:
//     |measure(to) - measure(from)| * backtrackWeightPerM, UNCONDITIONALLY. This is
//     NOT [backtrackCost] itself: that function's direction-conditional
//     logic (free when measure moves WITH the declared direction) is correct
//     for a piece representing real, continuous, walked canon, but wrong
//     for an instantaneous jump between two disjoint stretches — a jump
//     that happens to land at a LARGER measure than where it started is not
//     "aligned progress" just because the sign matches; it is still a
//     teleport across un-walked canon and must cost its full magnitude
//     either way. (Found the hard way: routing this through backtrackCost
//     gave a 3km arm-jump a cost of 0 whenever the jump direction happened
//     to match the declared FWD/REV, which is exactly the failure the
//     acceptance case "no stretch jumps arms" exists to catch.) No separate
//     P: the backtrack term subsumes a wrong-arm measure-span guard —
//     this is that subsumption, not a new guard. When the
//     two arms are canon-measure-adjacent at the switch point the gap is ~0,
//     which is already the "P not charged" case, with no
//     separate exemption needed.
//  3. Different candidate — the SAME continuous teleport price as case 2,
//     measured along the canon GRAPH rather than along one arc, and capped at
//     P. Two candidates that share a network node V are one continuous line
//     through V, so a switch A→B at x moves the walker's canon position from
//     A's foot to B's foot VIA V, a canon-graph distance of dA + dB where
//     dA = |measure(A at x) − eA| and eA is the measure of A's own endpoint
//     that IS V (0 or A.total), and likewise dB. The price is
//     min(P, backtrackWeightPerM * (dA + dB)).
//
//     ⚠ THE PAIRING IS NAMED BY THE TWO DECLARED DIRECTIONS, NOT MINIMISED
//     OVER. eA is the end of A the walker is heading TOWARD (A.total for FWD,
//     0 for REV) and eB the end of B it is walking AWAY from (0 for FWD,
//     B.total for REV), so the two Dirs name exactly one pairing and there is
//     nothing to take a min over. If those two ends are not the same
//     coordinate the switch pays the flat P, like any other.
//
//     ⚠ AND THAT IS A PRICE RULE, NOT TIDINESS. Minimising over all four
//     pairings priced a switch between two PARALLEL arcs leaving one node as
//     "back along A to V, out along B" — 2m*BW for a walker m along A — which
//     is a TURNAROUND, and a turnaround inside one arc costs P (case 1). So
//     turning round by hopping to a parallel copy was cheaper than turning
//     round in place: an arbitrage in favour of duplicate rows. The carry then
//     compounded it, refusing A's side (moving away from V) and accepting B's,
//     so the leg drew V->m twice.
//
//     Candidates sharing no endpoint coordinate pay the flat P, unchanged.
//     (to/from OFF is still flat P, above this case.)
//
//     ⚠ THIS REPLACED A BINARY CLIFF, AND THE CLIFF WAS THE DEFECT. The rule
//     used to be "flat P UNLESS both sides sit exactly at their own arc
//     endpoint at the same layer" — a yes/no verdict over a continuous
//     quantity, so re-derivation noise of 2.9e-7 m flipped a 50-unit price and
//     cost one test track ~37% of its arcs. Three separate mechanisms existed only to decide which side
//     of that cliff a layer landed on — a pinned-vertex derivation, span-end
//     markers, and a first-writer-wins boundary unification that moved piece
//     bounds by up to 13.36 m on a false premise. All three are
//     deleted. The old free hand-off is now the special case dA = dB = 0, and
//     the noise that used to flip the verdict costs 2.9e-7 instead.
//
//     It is a pure function of (piece a, piece b, x): no map, no first writer,
//     no tolerance, and no constant that is not already P or
//     backtrackWeightPerM. Candidate order cannot change it, which is the
//     property the three failed attempts could not get
//     (TestCorridorSolve_CandidateOrderIndependent).
//
// Dijkstra over the whole layered graph (all costs non-negative) finds the
// cheapest way from (layer 0, OFF) — a leg is OFF by default until something
// pays P to enter a candidate — to any state at the final layer.
//
// ---- Bridge edges, the fourth cost term ---------------------------
//
// A FOURTH edge kind, alongside forward/switch above: a BRIDGE edge connects
// (ivA,dir) at ivA's far import-measure boundary directly to (ivB,dir) at ivB's
// near boundary, for the NEXT CorridorInterval of the SAME candidate along
// import measure, SKIPPING every layer in between. It wires
// [canonGapCost]/[envelopeMinCost] (corridorcost.go) into the solve. Its cost is the canon-side gap integral over the candidate's
// own canon measure between ivA and ivB: the ONLY term that prices claiming
// canon the import never actually walked, i.e. fabrication.
//
// Without this edge, the ONLY way from ivA to a later interval of the same
// candidate is via OFF: pay P to leave, cross the import-measure gap at its own
// raw metres, pay P to re-enter — [noBridgeCost], already exactly what the
// existing forward+switch edges produce with no special-casing. The bridge
// edge is the OTHER side of the inequality, [bridgeCost] — Dijkstra picks
// whichever is cheaper, same as every other choice in this graph.
//
// CANDIDATE-SET SOUNDNESS — the reason this is safe without a certified
// box-pruning range query over source arcs: see [BuildCorridorBridges]'s own doc comment for
// the argument in full. Short version: the competing source arcs are
// not searched for, they are read directly off the source polyline between
// ivA's end and ivB's start — exact, no resolution constant, and a
// SUBSET-of-true-competitors argument makes any restriction here CONSERVATIVE
// (can only raise the priced cost, never lower it), which is the safe
// direction for a fabrication guard.
//
// Dir is preserved across a bridge (FWD only bridges to FWD, REV to REV):
// the bridge is a continuation of the SAME declared direction, not a second
// direction-flip mechanism — case 1 already owns flips. A direction change
// across a gap still falls back to the OFF-mediated path, unaffected by
// this change.

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// CorridorDir is the declared direction of an ON state: true (CorridorFWD)
// means canon measure is expected to INCREASE as import measure increases.
type CorridorDir bool

const (
	CorridorFWD CorridorDir = true
	CorridorREV CorridorDir = false
)

// CorridorStateID names one state in the solver's state space. Off is OFF; else
// SpIdx indexes the solver's own flat [SolvePiece] list and Dir is the
// declared direction.
//
// ⚠ THE STATE IS A PIECE, NOT AN INTERVAL — and that is the
// braid fix rather than a refactor. A CorridorInterval holds a BAG of pieces
// that can each answer "what canon measure does this import position correspond
// to", and at a hairpin they answer differently: both arms of the switchback
// are within R. While the state was the interval that choice was made OUTSIDE
// the solve by a fixed rule, and switching correspondence mid-interval was
// free and unpriced — every silent switch teleports the canon measure, and canon
// no run rides is then bridged by the leg's own chord, which is the second
// line on the map.
//
// Three local rules were measured first and each fixed one failure mode while
// creating another: first-in-list (canon-edge ascending) jumps FORWARD over a
// hairpin apex; nearest follows the foot BACKWARD as the source departs and
// splits runs; continuity-then-nearest under-adopts at a leg start under a
// warp taper. They fail because a correspondence is a property of the PATH and
// each was deciding it per position. With the piece in the state Dijkstra
// decides it, priced by terms that already exist.
type CorridorStateID struct {
	Off   bool
	SpIdx int
	Dir   CorridorDir
}

// CorridorIvBounds is the only part of a [CorridorInterval] the solve still
// needs once [BuildCorridorBridges] has run: the interval's import-measure
// range. It exists so the pieces can be released at that point — see the
// release comment in [SolveCorridorPath]. Field names match
// CorridorInterval's on purpose, so a read moved onto this type reads the
// same.
type CorridorIvBounds struct {
	ImportMeasureLo, ImportMeasureHi float64
}

// SolvePiece is one (interval, piece) pair — the unit the state space is
// built over. CandIdx is copied from the owning interval so no consumer needs
// the interval list to answer "which candidate is this run on".
type SolvePiece struct {
	IvIdx   int
	CandIdx int
	P       CorridorPiece
}

// buildSolvePieces flattens ivs into the solver's state alphabet, preserving
// interval order and, within an interval, Pieces order — so the deterministic
// tiebreak stays the ascending-candidate-id order it always was.
// The alphabet's final length is the sum of the intervals' piece counts, known
// before a single element is written, so it is allocated exactly once.
// This is the one copy of the kept alphabet that outlives the mint —
// `ivs[].Pieces` is unreachable right after the bridges — so an
// append-grown doubling here holds an extra half-alphabet through the solve.
func buildSolvePieces(ivs []CorridorInterval) []SolvePiece {
	n := 0
	for _, iv := range ivs {
		n += len(iv.Pieces)
	}
	out := make([]SolvePiece, 0, n)
	for i, iv := range ivs {
		for _, pc := range iv.Pieces {
			out = append(out, SolvePiece{IvIdx: i, CandIdx: iv.CandIdx, P: pc})
		}
	}
	return out
}

// canonMeasureAtPiece is ONE piece's own oriented canon measure at import position x.
// Unlike the interval-level lookup it replaces there is nothing to resolve: a
// piece is a function by construction, so this cannot be asked a question with
// two answers.
func canonMeasureAtPiece(pc CorridorPiece, x float64) float64 {
	if pc.ImportMeasureAtHi <= pc.ImportMeasureAtLo {
		return pc.CanonMeasureAtLo
	}
	f := (x - pc.ImportMeasureAtLo) / (pc.ImportMeasureAtHi - pc.ImportMeasureAtLo)
	return pc.CanonMeasureAtLo + f*(pc.CanonMeasureAtHi-pc.CanonMeasureAtLo)
}

func (s CorridorStateID) String() string {
	if s.Off {
		return "OFF"
	}
	dir := "FWD"
	if s.Dir == CorridorREV {
		dir = "REV"
	}
	return fmt.Sprintf("sp%d/%s", s.SpIdx, dir)
}

// CorridorRun is one committed stretch of the winning path.
//
// Bridged marks a run produced by a bridge edge — one with NO covering
// CorridorPiece at all (that is the whole point of bridging: claiming canon
// measure the import never walked close to). CanonMeasureLo/CanonMeasureHi are only
// meaningful when Bridged is true: they are the ORIENTED canon measure at the
// run's ImportMeasureLo/ImportMeasureHi ends (may decrease, exactly like
// CorridorPiece's own convention), taken from the bridge's own precomputed
// endpoints rather than derived from any piece, because no piece covers
// this run's import-measure range to derive them from. A non-bridged run's
// canon-measure bounds are NOT summarised here on purpose: a run can span
// several pieces whose canon measure is not monotonic across the whole run (a
// piece is monotonic in ITSELF, not in relation to its neighbours — that
// non-monotonicity is exactly what the backtrack term prices), so a single
// (lo,hi) pair would silently under-measure an oscillating run. Callers
// wanting a non-bridged run's canon-measure coverage must still clip and sum
// per piece.
//
// Exported for the reference application's tests; not part of the stable API.
type CorridorRun struct {
	State CorridorStateID
	// CandIdx is the candidate this run rides, -1 for OFF. Carried on the run
	// itself so no consumer has to hold the interval list to find it.
	CandIdx                          int
	ImportMeasureLo, ImportMeasureHi float64
	Bridged                          bool
	// CanonMeasureLo/CanonMeasureHi are the ORIENTED canon measure at the run's two ends.
	// Meaningful for EVERY run now, not only bridged ones: with the piece in
	// the state a non-bridged run lies wholly inside one piece, so its
	// correspondence is exact at both ends rather than something a downstream
	// rule has to re-resolve.
	CanonMeasureLo, CanonMeasureHi float64
	// ChainHop marks a run that exists only to DRAW an intermediate state of a
	// same-layer switch chain: a candidate the walker was switched onto and
	// straight off again at one import position. Its import extent is zero
	// (ImportMeasureLo == ImportMeasureHi) and its canon extent is the stretch
	// the chain's switches paid for across it. It is recorded here, where the
	// solve knows it, because downstream a zero-width run is otherwise a
	// sub-eps sliver and is dropped ([CorridorFollowRuns]).
	ChainHop bool
}

// CorridorSolveResult is [SolveCorridorPath]'s output.
//
// Exported for the reference application's tests; not part of the stable API.
type CorridorSolveResult struct {
	Runs []CorridorRun // ascending import measure, adjacent runs never share a state (merged)
	Cost float64
}

// CorridorTunables bundles P and the backtrack weight for an override — a
// single struct rather than two more variadic float64s, because
// [TransitionCostT] needs both together at every call and a mismatched pair
// (P from one sweep point, weight from another) would silently measure a
// configuration nobody asked for. Only tests pass one; every production
// SolveCorridorPath call site omits it and gets the
// production constants. It exists to make P and backtrackWeightPerM
// sweepable offline; both values were tuned with it.
type CorridorTunables struct {
	P               float64
	BacktrackWeight float64
}

// resolveTunables returns the single override in overrides, or the
// production defaults when none was given. A caller passing more than one
// is a caller bug (the variadic slot exists for optionality, not for a
// list), so it errors rather than silently taking the first.
func resolveTunables(overrides []CorridorTunables) (CorridorTunables, error) {
	switch len(overrides) {
	case 0:
		return CorridorTunables{P: StateChangeCostP, BacktrackWeight: backtrackWeightPerM}, nil
	case 1:
		return overrides[0], nil
	default:
		return CorridorTunables{}, fmt.Errorf("corridorsolve: resolveTunables: got %d overrides, want at most 1", len(overrides))
	}
}

// Both questions this file asks about endpoints — "is this canon-measure
// position the candidate's own end vertex" and "are these two candidates'
// end vertices the same network node" — are EXACT, no epsilon. The values
// compared are written by construction, not measured: a candidate's measure
// table starts at 0 and ends at pc.total by construction, and two candidates
// incident on one node carry that node's coordinate as the same float64,
// copied from the same row. A tolerance here would be a pricing decision —
// it decides whether a switch is free or costs P — dressed as rounding.
//
// Measured before removing them: this pair previously sat at 0.5 m, and 0.0
// and 0.5 give the identical result on a 19-file test corpus (60 arcs, 44 nodes,
// 259.256084 km, hash d1578bb2) with the full test suite green at 0.0,
// while 15.0 changes the hash. They were inert.

// clipCorridorPiece restricts cp to the import-measure range [x0,x1] ∩
// [cp.ImportMeasureAtLo, cp.ImportMeasureAtHi], interpolating Regime.lo/hi and the
// oriented canon measure linearly in import measure — exact for RegimeSpan (whose
// foot parameter, and so canon measure, is affine in t by construction) and
// trivially exact for RegimeNearA/NearB (whose canon measure is constant, so
// interpolation returns that same constant). ok is false when the ranges
// don't overlap.
func clipCorridorPiece(cp CorridorPiece, x0, x1 float64) (out CorridorPiece, ok bool) {
	lo := math.Max(cp.ImportMeasureAtLo, x0)
	hi := math.Min(cp.ImportMeasureAtHi, x1)
	if hi <= lo || cp.ImportMeasureAtHi <= cp.ImportMeasureAtLo {
		return CorridorPiece{}, false
	}
	span := cp.ImportMeasureAtHi - cp.ImportMeasureAtLo
	f0 := (lo - cp.ImportMeasureAtLo) / span
	f1 := (hi - cp.ImportMeasureAtLo) / span
	out = cp
	out.ImportMeasureAtLo, out.ImportMeasureAtHi = lo, hi
	out.Regime.Lo = cp.Regime.Lo + f0*(cp.Regime.Hi-cp.Regime.Lo)
	out.Regime.Hi = cp.Regime.Lo + f1*(cp.Regime.Hi-cp.Regime.Lo)
	out.CanonMeasureAtLo = cp.CanonMeasureAtLo + f0*(cp.CanonMeasureAtHi-cp.CanonMeasureAtLo)
	out.CanonMeasureAtHi = cp.CanonMeasureAtLo + f1*(cp.CanonMeasureAtHi-cp.CanonMeasureAtLo)
	return out, true
}

// ArcCost is the emission+backtrack cost of holding state (iv,dir)
// across one elementary arc [x0,x1], priced from the SINGLE piece of iv
// that covers it, chosen the SAME way [canonMeasureAtImportMeasure] already chooses
// one for transitions/bridges, and the same way
// [AssembleFollowedLegRunsCorridor]'s greedy left-to-right piece consumption
// (corridorwire.go) resolves it when committing geometry: the FIRST piece in
// iv.Pieces' own order whose range clips [x0,x1].
//
// iv's pieces are NOT guaranteed non-overlapping in import measure —
// CorridorPiece's own doc comment: "ascending import measure, contiguous OR
// SELF-APPROACH-OVERLAPPING". A self-approach (including the ordinary case
// of several short, roughly-collinear canon edges each independently within
// R of the same source position) can leave more than one piece covering the
// same [x0,x1], each with its OWN dist²(t) and so its own emission. Summing
// them charged a position sitting exactly on canon for every OTHER, farther
// piece also within R — measured on a straight re-import leg at ~0.5/m
// instead of 0, enough to make the solver prefer OFF over adopting canon it
// sits right on top of. The invariant this file's callers depend on: the
// cost function must price exactly the geometry
// [AssembleFollowedLegRunsCorridor] will draw for the winning path, and that
// assembler already resolves the identical overlap by first-piece-wins
// (corridorwire.go's own "GREEDY, LEFT-TO-RIGHT, NOT every piece that
// clips" — found on a real multi-day route) — so this picks the SAME piece
// rather than a new tie-break.
//
// No piece clipping [x0,x1] at all is NOT special-cased into an error here:
// the forward edge that calls this only exists when iv.ImportMeasureLo/Hi cover
// the whole arc ([ActiveOverArc]), which is looser (its own eps)
// than [clipCorridorPiece]'s exact overlap test, so an arc can — at
// sub-eps scale — sit fractionally outside every piece's true bounds
// (measured: corridorwire.go's comment on sub-eps runs, a
// 7.8e-9 m run). Returning 0 there matches that comment's own reliance on
// this function's silence, rather than erroring on floating-point noise the
// solver's own tolerance already produced.
// backtrackWeight is optional, for tuning sweeps only (see [CorridorTunables]):
// omitted, [backtrackCost] uses the production constant.
//
// sp is a pointer for the reason [TransitionCostT] gives: the forward edge
// calls this once per pop, and a SolvePiece is 176 B. Nothing here writes it.
//
// Exported for the reference application's tests; not part of the stable API.
func ArcCost(sp *SolvePiece, dir CorridorDir, x0, x1, r float64, k int, backtrackWeight ...float64) (float64, error) {
	cp, ok := clipCorridorPiece(sp.P, x0, x1)
	if !ok {
		return 0, nil
	}
	e, err := emissionCost(cp, r, k)
	if err != nil {
		return 0, err
	}
	c := e + backtrackCost(cp, bool(dir), backtrackWeight...)
	// ⚠ A NEGATIVE ARC COST IS AN ERROR. Every term is ≥ 0 by construction —
	// the emission integral included — and the
	// solve is a Dijkstra, which is wrong on a negative edge: it can improve a
	// state it has already popped. A negative here is a broken cost term.
	if !(c >= 0) {
		return 0, fmt.Errorf("corridorsolve: arc cost %v over import measure [%v,%v] is negative or NaN (emission %v) — a cost term is broken", c, x0, x1, e)
	}
	return c, nil
}

// canonMeasureAtImportMeasure evaluates iv's oriented canon measure at one import-measure
// position x, linearly interpolating within whichever piece covers it — the
// same interpolation [clipCorridorPiece] uses, exact for the same reason.
func canonMeasureAtImportMeasure(iv CorridorInterval, x float64) (float64, bool) {
	const eps = 1e-6
	for _, p := range iv.Pieces {
		if x < p.ImportMeasureAtLo-eps || x > p.ImportMeasureAtHi+eps {
			continue
		}
		if p.ImportMeasureAtHi <= p.ImportMeasureAtLo {
			return p.CanonMeasureAtLo, true
		}
		// CLAMPED to the piece's own domain. This function deliberately
		// ACCEPTS an x up to eps outside a piece's range (see the eps above),
		// and an unclamped interpolation then EXTRAPOLATES past the piece's
		// two ends — putting the answer fractionally off the end of the canon
		// arc. Found on a randomised test seed: a bridge whose departure measure
		// came back as -2.26e-07 m, which the caller's canon split correctly
		// refused as outside a 1598.6 m arc. A piece's correspondence is
		// defined ON its own range; asked at the boundary it answers with the
		// boundary, exactly as [clipCorridorPiece] already does for a range.
		f := (x - p.ImportMeasureAtLo) / (p.ImportMeasureAtHi - p.ImportMeasureAtLo)
		if f < 0 {
			f = 0
		} else if f > 1 {
			f = 1
		}
		return p.CanonMeasureAtLo + f*(p.CanonMeasureAtHi-p.CanonMeasureAtLo), true
	}
	return 0, false
}

// crossCandidateEndMeasure is the canon measure of pc's endpoint vertex vi —
// 0 for the first vertex, pc.total for the last. Both are written by
// construction (the measure table starts at 0 and ends at total), so this is
// a lookup, not a derivation.
func crossCandidateEndMeasure(pc *PreppedCandidate, vi int) float64 {
	if vi == 0 {
		return 0
	}
	return pc.Total
}

// crossCandidateSwitchCost prices a switch between two DIFFERENT candidates at
// import-measure position x — this file's header, case 3.
//
// Two candidates that share a network node V are ONE continuous line through V
// along the canon graph. A switch A→B at x teleports the walker's canon
// position from A's foot to B's foot via V, and the canon-graph distance of
// that teleport is dA + dB: how far A's foot at x still is from V along A,
// plus how far B's foot at x already is from V along B. That is the same
// quantity case 2 charges for a same-candidate teleport, measured along the
// graph instead of along one arc, so it is charged the same way — at
// BacktrackWeight, capped at P.
//
// ⚠ THE CAP IS NOT COSMETIC. [SolveCorridorPath]'s generic fan-out lays a flat
// P edge between every cross-candidate pair at a layer, so P is already the
// upper bound Dijkstra sees. Stating it here keeps this function the SINGLE
// definition of the price rather than a term the solver silently improves on —
// which is what [TestCorridorSolve_TheTableIsFullyRelaxed] checks by comparing
// the solver's own relaxation against this function.
//
// ⚠ SHARED ENDPOINTS ONLY, AND BY COORDINATE IDENTITY, NOT PROXIMITY. Two
// candidates incident on one node carry that node's coordinate as the same
// float64, copied from the same row, so the test is bitwise and there is no
// tolerance to fit. Interior vertices are not shared between candidates and
// name no node, so they are not considered.
//
// ⚠ ONE PAIRING, NAMED BY THE DIRECTIONS — see the header's case 3 for why
// minimising over all four was an arbitrage in favour of duplicate rows. Even
// where two candidates share BOTH ends (a 2-gon, a loop arc), the declared
// Dirs pick one end each and there is no choice left to make.
//
// A pair sharing no endpoint returns P, exactly as before.
func crossCandidateSwitchCost(a, b *SolvePiece, aDir, bDir CorridorDir, cands []*PreppedCandidate, x float64, t CorridorTunables) float64 {
	sh := crossCandidateSharedPair(a, b, aDir, bDir, cands, x)
	if !sh.OK {
		return stateChangeCost(t.P)
	}
	return math.Min(stateChangeCost(t.P), t.BacktrackWeight*sh.Dist)
}

// crossCandidateShare is the cheapest way through a shared node from piece a
// to piece b at one import-measure position: WHICH canon measure on each side
// is the node (EA, EB), and how far the walker's two feet are from it in
// total (Dist = dA + dB). OK is false when the two candidates share no
// endpoint coordinate at all.
//
// ⚠ EA AND EB ARE THE REASON THIS IS A STRUCT AND NOT A FLOAT. The price is
// only half of what the solver decides here. A switch that PAYS for the
// teleport foot_A -> V -> foot_B has bought that canon, and
// [SolveCorridorPath]'s run reconstruction must DRAW it — otherwise the cost
// function prices geometry the assembler never commits, which is the same
// class of defect [ArcCost]'s own header records. So the two measures are
// returned alongside the distance rather than recomputed at the second site
// from a formula that could drift.
//
// ⚠ IA AND IB ARE CARRIED FOR THE SAME REASON, ONE LEVEL DOWN. They are the
// vertex INDICES the two end measures came from — the only structural record
// of WHICH end of each arc was paid for. The carry-through sites used to
// re-derive the direction of the move from EA/EB with a float comparison
// (an earlier version did); an index and the state's declared Dir
// answer the same question with no tolerance at all, so the index travels with
// the measure it explains.
type crossCandidateShare struct {
	EA, EB float64
	IA, IB int
	Dist   float64
	OK     bool
}

// crossCandidateSharedPair is the ONE definition of which shared endpoint a
// cross-candidate switch goes through, and how far it is. Both the price
// ([crossCandidateSwitchCost]) and the carry-through in the run
// reconstruction read it, so they cannot disagree about which node was paid
// for.
//
// Two candidates sharing BOTH ends (a 2-gon, a loop arc) offer up to four
// pairings and this takes the cheapest. The tie-break is the fixed iteration
// order below — first vertex before last on each side, strict `<` on the
// distance — so an exact tie takes the lowest-index pairing deterministically
// rather than whichever the walk reached last.
func crossCandidateSharedPair(a, b *SolvePiece, aDir, bDir CorridorDir, cands []*PreppedCandidate, x float64) crossCandidateShare {
	pA, pB := cands[a.CandIdx], cands[b.CandIdx]
	if len(pA.Verts) == 0 || len(pB.Verts) == 0 {
		return crossCandidateShare{}
	}
	// The DECLARED directions name the ends, so there is exactly one pairing to
	// test — no min, no tie-break. A walker whose canon measure increases is
	// heading for its arc's high end; one whose measure decreases is heading
	// for 0. It leaves B the other way round: it has just arrived, so it starts
	// at the end it is walking AWAY from.
	ia, ib := 0, len(pB.Verts)-1
	if aDir == CorridorFWD {
		ia = len(pA.Verts) - 1
	}
	if bDir == CorridorFWD {
		ib = 0
	}
	if pA.Verts[ia].Lat != pB.Verts[ib].Lat || pA.Verts[ia].Lon != pB.Verts[ib].Lon {
		return crossCandidateShare{}
	}
	eA := crossCandidateEndMeasure(pA, ia)
	eB := crossCandidateEndMeasure(pB, ib)
	mA := canonMeasureAtPiece(a.P, x)
	mB := canonMeasureAtPiece(b.P, x)
	return crossCandidateShare{EA: eA, EB: eB, IA: ia, IB: ib, Dist: math.Abs(mA-eA) + math.Abs(mB-eB), OK: true}
}

// carryEndIsExtreme reports whether vertex index vi, on a candidate of nVerts
// vertices, is the arc end a walker in direction dir is heading TOWARD: the
// last vertex when the canon measure increases, the first when it decreases.
//
// ⚠ IT REPLACED A MEASURE COMPARISON, which
// asked `to >= at` / `to <= at` on two floats to decide the same thing. The
// comparison was never answering a question about the data: `to` is always an
// arc's own extreme end measure — exactly 0 or exactly total, written by
// construction ([crossCandidateEndMeasure]) — and a canon measure inside the
// arc's domain always reaches the extreme it is walking toward. So the only
// input that could make it false was a measure that had escaped the domain by
// interpolation noise: the recorded case is -2.26e-7 m on a 1598.6 m arc
// (see [canonMeasureAtImportMeasure]), and both carry sites abort the
// whole caller's write on a false answer. The rule: where our own code
// created the coincidence, the later "is this
// the same end?" is answered from the recorded structural fact — here the
// vertex index the pairing chose — never from a float tolerance.
//
// ⚠ IT REPLACED A DERIVED-DIRECTION TEST BEFORE THAT. That one read the run's own CanonMeasureLo/Hi to decide which
// way it was going, which could contradict the state's declared Dir, and had
// to invent an answer for a zero-width run that has no derived direction at
// all. Once the PAIRING is direction-consistent the declared Dir is the only
// authority either side needs, and both sites use it.
//
// The receiving side of a carry asks this with dir INVERTED: a run that STARTS
// where a teleport landed begins at the end it is walking AWAY from, which is
// the extreme of the opposite direction.
func carryEndIsExtreme(dir CorridorDir, vi, nVerts int) bool {
	if dir == CorridorFWD {
		return vi == nVerts-1
	}
	return vi == 0
}

// TransitionCostT is the cost of switching from `from` to `to` at
// import-measure position x — this file's header, cases 1-3. from==to costs 0
// (staying is not a switch) but is never actually invoked that way by the
// solver, which only builds edges between distinct states.
//
// The tunables come in already resolved ([resolveTunables]): a variadic slot
// costs a slice per call and resolveTunables was 1.7% of a dense solve on its
// own, which is absurd for a two-field struct the caller already holds.
//
// It also takes its two pieces BY POINTER. SolvePiece embeds a CorridorPiece,
// so `a, b := sps[i], sps[j]` copied two of them per relaxation and showed up
// as 12% of the solve in runtime.duffcopy. Nothing here mutates them.
//
// The arithmetic below is unchanged and deliberately kept in ONE place: the
// obvious alternative — inlining the same-candidate price into the solver's
// loop — makes a second copy of a formula whose divergence would silently
// reprice every solve rather than fail anything, and it does not even
// reproduce bit-for-bit (measured: 1 ULP apart, the compiler fusing the
// multiply-add at one site and not the other).
//
// Exported for the reference application's tests; not part of the stable API.
func TransitionCostT(from, to CorridorStateID, x float64, sps []SolvePiece, cands []*PreppedCandidate, t CorridorTunables) float64 {
	if from == to {
		return 0
	}
	if from.Off || to.Off {
		return stateChangeCost(t.P)
	}
	a, b := &sps[from.SpIdx], &sps[to.SpIdx]
	if a.CandIdx == b.CandIdx {
		c := 0.0
		// A turnaround is a turnaround whether or not the correspondence also
		// changes: P, exactly as case 1 always charged it.
		if from.Dir != to.Dir {
			c += stateChangeCost(t.P)
		}
		// Changing WHICH correspondence the line is on costs the canon measure it
		// discontinuously skips or repeats. This is case 2's arm-jump term,
		// now applied to EVERY correspondence change rather than only to the
		// ones that happened to cross an interval boundary — which is the
		// whole point of putting the piece in the state. It is ZERO for the
		// ordinary hand-off from one piece to the next along one trail:
		// consecutive pieces agree bitwise at a shared source or canon vertex,
		// because [orientedFootMeasure] takes those endpoints exactly rather than
		// interpolating to them. A piece boundary is free; only a real
		// teleport is charged.
		if from.SpIdx != to.SpIdx {
			gap := math.Abs(canonMeasureAtPiece(b.P, x) - canonMeasureAtPiece(a.P, x))
			// The gap is the canon measure the correspondence skips or repeats at
			// this instant, and it is charged at the backtrack weight — case
			// 2's arm-jump term, now applied to every correspondence change.
			//
			// NOT ALSO CHARGED P, and that was measured rather than assumed.
			// Charging P per discontinuity looks right, since a jump is a
			// seam — but it double-prices the ordinary case. When the source
			// runs OFFSET from canon its nearest foot genuinely jumps at every
			// bend bisector, because the source cuts the corner; those are
			// CONTINUATIONS, not seams, and
			// [mergeAdjacentSameCandidateRuns] carries the skipped canon
			// through. Adding P there made the solve avoid necessary
			// hand-offs: on a test corpus, 87 committed arcs without it, 153
			// with.
			c += t.BacktrackWeight * gap
		}
		return c
	}
	return crossCandidateSwitchCost(a, b, from.Dir, to.Dir, cands, x, t) // case 3: cross-candidate
}

// ---- Bridge edges -----------------------------------------------------

// CorridorBridge is one precomputed FromIv -> ToIv bridge edge: the NEXT
// CorridorInterval of the SAME candidate along import measure, separated from
// FromIv by a genuine import-measure gap (not a self-approach overlap, which
// case 2 in [TransitionCostT] already prices). CanonLo/CanonHi are the
// ORIENTED canon measure at FromIv's far end and ToIv's near end respectively
// (not sorted — may decrease, exactly CorridorPiece's own convention),
// carried through so a resulting [CorridorRun] can report them without
// recomputing.
type CorridorBridge struct {
	FromIv, ToIv     int
	Cost             float64
	CanonLo, CanonHi float64
}

// BuildCorridorBridges finds every such adjacent-pair gap across every
// candidate and prices it with the canon-side gap integral
// (corridorcost.go) via [candidateBridgeCost].
//
// CANDIDATE-SET SOUNDNESS. Discovering WHICH source arcs are ever competitive near an ARBITRARY canon edge
// is not closed-form: a probe-and-pad heuristic missed the true
// nearest arc by 138% on a real 1,956.8 m canon edge — an empirically
// tuned resolution constant in disguise, explicitly not fit to reuse.
//
// This function does not answer that general question, and does not need
// to. The question it actually asks is narrower: "for THIS candidate's gap
// between two of its OWN already-adopted stretches, is bridging cheap?" —
// and for that question there is an EXACT, SEARCH-FREE answer:
// [candidateBridgeCost] takes the competing source arcs to be exactly
// the source polyline trimmed to [ivA.ImportMeasureHi, ivB.ImportMeasureLo]
// ([sourceArcsBetween]) — the stretch of track the import itself ran
// between leaving ivA and rejoining ivB, read directly off `sa`, not
// discovered by probing.
//
// WHY THIS IS SOUND, not merely convenient: [envelopeMinCost] takes a MIN
// over its srcArcs, so restricting the set to fewer-than-all competitors
// can only RAISE the computed integral, never lower it (removing a
// candidate from a min-search cannot make the min smaller). A conservative
// SUBSET therefore biases the bridge cost UP, never down — the safe
// direction for a fabrication guard, and the OPPOSITE of the probe
// heuristic's failure (which UNDER-estimated distance on the missed arc,
// making a real gap look cheaper than it was — over-permitting exactly the
// fabrication this term exists to price). The failure mode left over here
// is the safe one: a gap that some OTHER, non-adjacent stretch of source
// happens to run close to but the direct "between" stretch does not will
// be priced as more expensive than the truth, and may be refused where a
// fuller search would have allowed it. That is lost coverage, not
// fabricated adoption — recorded as a known limitation, not hidden.
//
// It is also COMPLETE for the question it answers, unlike that heuristic:
// "the source between these two measure positions" has no probe density or
// search radius to tune. It is not a general replacement for a
// certified box-pruning range query (unbuilt, and the right fix for a
// general canon-side query); it only ever needs to answer
// for gaps this solver already knows the two bounding intervals of.
//
// Only ADJACENT pairs (consecutive in import-measure order for one candidate)
// get a bridge — bridging past an intervening interval of the SAME
// candidate is out of scope: Dijkstra can already reach a further interval
// via two adjacent bridges plus whatever the intervening interval costs,
// so nothing is unreachable, only a direct skip-two edge is not built.
// ⚠ `pOverride` IS THE SOLVE'S OWN P, AND IT IS NOT COSMETIC. The only place
// P is read below is [importGapCostBetween]'s ceiling, which is the no-bridge
// alternative the short circuit compares against — and the solve prices its OFF
// edges with `t.P` ([SolveCorridorPath]), not the production constant. Under a
// tuning sweep with a raised P the two disagreed: the ceiling stayed at the
// production 2P while the real OFF path cost 2*t.P, so a short-circuited return
// could be a lower bound BELOW the honest alternative and the "no shortest path
// prefers it" guarantee in importGapCostBetween's header did not hold. Omitted
// by the direct-call tests, which measure the production P deliberately.
//
// ⚠ ctx IS CHECKED HERE AND NOT ONLY IN THE CALLER. This function runs BETWEEN the mint and the solve, and its own header
// says above that the adjacent-pair short circuit "is not a bound on the work":
// [candidateBridgeCost]'s canon-side gap integral walks the source trimmed to
// the gap, per pair, per candidate. So it can sit for a long time with the leg
// already cancelled unless it asks. Checked once per candidate and once per
// bridge, which are its two units of work.
//
// Exported for the reference application's tests; not part of the stable API.
func BuildCorridorBridges(ctx context.Context, sa *SourceMeasure, ivs []CorridorInterval, cands []*PreppedCandidate, r float64, k int, pOverride ...float64) ([]CorridorBridge, error) {
	if len(ivs) == 0 {
		return nil, nil
	}
	byCand := map[int][]int{}
	for i, iv := range ivs {
		byCand[iv.CandIdx] = append(byCand[iv.CandIdx], i)
	}
	candIdxs := make([]int, 0, len(byCand))
	for c := range byCand {
		candIdxs = append(candIdxs, c)
	}
	sort.Ints(candIdxs) // deterministic iteration order, matching this file's tiebreak discipline elsewhere

	var out []CorridorBridge
	for _, candIdx := range candIdxs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("corridorsolve: bridges cancelled at candidate %d: %w", candIdx, err)
		}
		idxs := byCand[candIdx]
		// Tie-break on the interval's own index, not left to sort.Slice's
		// unstable default: two intervals of one candidate CAN share the
		// same ImportMeasureLo (self-approach overlap), and this file already
		// carries one recorded incident of an unstable ordering silently
		// producing a wrong number (this file's own header, the removed
		// sort.Float64s(bps) finding) — cheap enough to close explicitly
		// rather than lean on pdqsort's incidental determinism.
		sort.Slice(idxs, func(a, b int) bool {
			if ivs[idxs[a]].ImportMeasureLo != ivs[idxs[b]].ImportMeasureLo {
				return ivs[idxs[a]].ImportMeasureLo < ivs[idxs[b]].ImportMeasureLo
			}
			return idxs[a] < idxs[b]
		})
		for n := 0; n+1 < len(idxs); n++ {
			fromIdx, toIdx := idxs[n], idxs[n+1]
			ivA, ivB := ivs[fromIdx], ivs[toIdx]
			if ivA.ImportMeasureHi >= ivB.ImportMeasureLo {
				continue // touching or self-approach overlap — not a gap, case 2 already covers it
			}
			// AN INTERVAL WITH NO IMPORT-MEASURE EXTENT IS NOT SOMEWHERE A BRIDGE
			// CAN LEAVE FROM OR LAND ON.
			//
			// The breakpoint construction in [SolveCorridorPath] adds a layer
			// only for `lo < hi`, at BOTH the interval and the piece level. A
			// component made entirely of tangency pieces — a source edge that
			// grazes the d-band at exactly one t, which
			// [TestCorridorIntervals_AStationarySourceMintsNoPieces] observes
			// over a hundred of on its own straight fixture — therefore
			// contributes NO layer at all.
			//
			// Nothing here refused it, so an isolated graze component sitting
			// beyond a genuine gap got a bridge built to it; the solve then
			// looked its ImportMeasureLo up in layerOf, missed, and returned the
			// "not a breakpoint — internal inconsistency" error, failing the
			// WHOLE import. Loud rather than silent, but a hard failure on
			// ordinary degenerate geometry all the same.
			//
			// Skipping is not a fallback hiding a missing value: a zero-extent
			// interval has no layer, no state and no forward edge, so there is
			// literally nothing for a bridge to land on and nothing that could
			// travel along it after landing. The bridge is not being suppressed
			// — it never had a destination.
			if ivA.ImportMeasureHi <= ivA.ImportMeasureLo || ivB.ImportMeasureHi <= ivB.ImportMeasureLo {
				continue
			}
			// One priced bridge is the unit of work here — the pairs skipped
			// above cost nothing, and [candidateBridgeCost] below is where the
			// gap integral is actually walked.
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("corridorsolve: bridges cancelled at candidate %d ivs %d->%d: %w", candIdx, fromIdx, toIdx, err)
			}
			cost, canonLo, canonHi, err := candidateBridgeCost(ctx, sa, cands[candIdx], ivA, ivB, r, k, pOverride...)
			if err != nil {
				return nil, fmt.Errorf("corridorsolve: bridge candidate %d ivs %d->%d: %w", candIdx, fromIdx, toIdx, err)
			}
			out = append(out, CorridorBridge{FromIv: fromIdx, ToIv: toIdx, Cost: cost, CanonLo: canonLo, CanonHi: canonHi})
		}
	}
	return out, nil
}

// candidateBridgeCost is [bridgeCost] for one candidate gap between ivA and
// ivB: BOTH gap integrals summed — pc's canon-side one
// ([canonGapCostBetween], the canon it claims against the source actually run
// through the gap) and the import-side one ([importGapCostBetween], that same
// source against that same claimed canon). Returns the oriented
// (canonLo,canonHi) alongside the cost so callers don't recompute it —
// CorridorRun.CanonMeasureLo/Hi and [BuildCorridorBridges] both need it.
//
// ⚠ THE IMPORT-SIDE TERM IS NOT SYMMETRY FOR ITS OWN SAKE. The canon-side
// integral prices "canon claimed ground the import never walked". Nothing
// priced "the import walked ground the committed line never draws", so a
// bridge was the one edge in the solve consuming import measure for free
// ([importGapCost]'s own header carries the measurement). The two gaps are
// only the same gap when the source leaves and returns at DIFFERENT canon
// positions; when it leaves and comes back to nearly the same one — a
// GPS excursion, a detour, a dropout — the canon gap is ~0 and the import gap
// is kilometres.
//
// A ZERO-WIDTH CANON SPAN IS NO LONGER AN EARLY RETURN. It used to return 0
// with "ends coincide in canon measure: nothing to bridge", which is the
// worst case of exactly the defect above: the free teleport. The claimed
// canon is then a single point ([candidateArcsBetween] returns it as one
// degenerate arc, which canonEdgeDistSqRegimes handles in its own
// degenerate-frame branch), and the import side is charged against it.
func candidateBridgeCost(ctx context.Context, sa *SourceMeasure, pc *PreppedCandidate, ivA, ivB CorridorInterval, r float64, k int, pOverride ...float64) (cost, canonLo, canonHi float64, err error) {
	measureA, ok := canonMeasureAtImportMeasure(ivA, ivA.ImportMeasureHi)
	if !ok {
		return 0, 0, 0, fmt.Errorf("corridorsolve: interval end %.6g not on its own interval", ivA.ImportMeasureHi)
	}
	measureB, ok := canonMeasureAtImportMeasure(ivB, ivB.ImportMeasureLo)
	if !ok {
		return 0, 0, 0, fmt.Errorf("corridorsolve: interval start %.6g not on its own interval", ivB.ImportMeasureLo)
	}
	lo, hi := math.Min(measureA, measureB), math.Max(measureA, measureB)

	// The caller ([BuildCorridorBridges]) has already refused any pair whose
	// import measures touch or overlap, so this window is non-empty.
	srcArcs, err := sourceArcsBetween(sa, ivA.ImportMeasureHi, ivB.ImportMeasureLo)
	if err != nil {
		return 0, 0, 0, err
	}
	canonArcs, err := candidateArcsBetween(pc, lo, hi)
	if err != nil {
		return 0, 0, 0, err
	}

	canonSide := 0.0
	if hi > lo {
		canonSide, err = canonGapCostBetween(ctx, pc, lo, hi, srcArcs, r, k)
		if err != nil {
			return 0, 0, 0, err
		}
	}
	importSide, err := importGapCostBetween(ctx, srcArcs, canonArcs, r, k,
		noBridgeCost(ivB.ImportMeasureLo-ivA.ImportMeasureHi, pOverride...)-canonSide)
	if err != nil {
		return 0, 0, 0, err
	}
	return bridgeCost(canonSide + importSide), measureA, measureB, nil
}

// importGapCostBetween sums [importGapCost] over every source arc a bridge
// skips, against the canon arcs it claims, and STOPS as soon as the running
// sum passes `ceiling`.
//
// THE SHORT CIRCUIT IS CORRECTNESS-NEUTRAL AND IT IS WHAT MAKES THE TERM
// AFFORDABLE. The term is O(srcArcs x canonEdges²) per bridge — envelopeMinCost
// is quadratic in its piece count and each canon arc contributes up to three
// pieces — and the gaps that matter are the long ones, thousands of source
// arcs each. Callers pass the no-bridge alternative (2P + the gap's own OFF
// metres, [noBridgeCost]) less whatever the canon side already cost.
//
// ⚠ "A HANDFUL OF ARCS" WAS TOO OPTIMISTIC AND IS CORRECTED HERE. It holds for
// a gap whose source is far off the claimed canon — each arc contributes a
// large increment and the ceiling is passed almost at once. NEAR THE BAND EDGE
// it does not: the per-arc increments are small compared with the gap's own OFF
// metres, so the sum creeps toward a ceiling of roughly the same size and the
// loop can walk most or all of the gap's arcs before deciding. The short
// circuit is a worst-case-preserving optimisation, not a bound on the work.
//
// What a short-circuited return means: the value is a LOWER BOUND on the true
// integral, and it is strictly GREATER than `ceiling`. Both halves matter. It
// never overcharges a bridge, and it is already dearer than the honest OFF
// path the solve can always take instead, so no shortest path prefers it. The
// bridge edge is still emitted rather than dropped — dropping it would remove
// a graph edge on a cost argument, and the graph's reachability is not this
// function's to decide.
func importGapCostBetween(ctx context.Context, srcArcs, canonArcs [][2]Point, r float64, k int, ceiling float64) (float64, error) {
	total := 0.0
	for _, arc := range srcArcs {
		lenM := HaversineMetres(arc[0].Lat, arc[0].Lon, arc[1].Lat, arc[1].Lon)
		if lenM == 0 {
			continue // a stationary sample consumes no import measure and so owes nothing
		}
		c, err := importGapCost(ctx, arc[0], arc[1], canonArcs, lenM, r, k)
		if err != nil {
			return 0, err
		}
		total += c
		if total > ceiling {
			return total, nil
		}
	}
	return total, nil
}

// candidateArcsBetween is [sourceArcsBetween] on the OTHER side: pc's own
// vertices spanning canon measure [loMeasure, hiMeasure], trimmed EXACTLY to
// those two positions rather than padded to an edge boundary.
//
// The trim is the same necessity [canonGapCostBetween]'s uLo/uHi is: canon
// edges are not densified (a real 1,956.8 m Snowdon edge), so handing an
// untrimmed edge to the import-side integral would let source hundreds of
// metres away claim to be "on" a canon edge it merely shares a vertex with.
//
// A zero-width span returns ONE DEGENERATE ARC at that position, not an empty
// set. That is the claimed canon — a single point — and pricing source
// against it is the whole point; an empty set would be silently free.
func candidateArcsBetween(pc *PreppedCandidate, loMeasure, hiMeasure float64) ([][2]Point, error) {
	if pc == nil {
		return nil, fmt.Errorf("corridorsolve: nil candidate")
	}
	if !allFinite(loMeasure, hiMeasure) {
		return nil, fmt.Errorf("corridorsolve: canon window [%v,%v] is not finite", loMeasure, hiMeasure)
	}
	lo := math.Max(0, math.Min(loMeasure, hiMeasure))
	hi := math.Min(pc.Total, math.Max(loMeasure, hiMeasure))
	loLat, loLon := pc.interpOnCanon(lo)
	loPt := Point{Lat: loLat, Lon: loLon}
	if !(hi > lo) {
		return [][2]Point{{loPt, loPt}}, nil
	}
	hiLat, hiLon := pc.interpOnCanon(hi)
	hiPt := Point{Lat: hiLat, Lon: hiLon}
	eLo, eHi := candidateCanonEdgeAt(pc, lo), candidateCanonEdgeAt(pc, hi)
	if eHi < eLo {
		eHi = eLo
	}
	if eLo == eHi {
		return [][2]Point{{loPt, hiPt}}, nil
	}
	// One arc per canon edge crossed, ends trimmed: eHi-eLo+1, known here.
	arcs := make([][2]Point, 0, eHi-eLo+1)
	arcs = append(arcs, [2]Point{loPt, pc.Verts[eLo+1]})
	for e := eLo + 1; e < eHi; e++ {
		arcs = append(arcs, [2]Point{pc.Verts[e], pc.Verts[e+1]})
	}
	arcs = append(arcs, [2]Point{pc.Verts[eHi], hiPt})
	return arcs, nil
}

// sourceArcsBetween returns the source polyline's own arcs spanning
// import measure [loMeasure, hiMeasure], trimmed EXACTLY to those two positions — never
// padded to an edge boundary. See [BuildCorridorBridges]'s soundness
// argument: padding outward would ADD competing arcs, which is the
// UNSAFE direction here (more competitors can only lower the computed gap
// cost, making a bridge look cheaper than the truth).
func sourceArcsBetween(sa *SourceMeasure, loMeasure, hiMeasure float64) ([][2]Point, error) {
	if sa == nil {
		return nil, fmt.Errorf("corridorsolve: nil source measure")
	}
	if !(hiMeasure > loMeasure) {
		return nil, fmt.Errorf("corridorsolve: source window [%.6g,%.6g] is empty or inverted", loMeasure, hiMeasure)
	}
	loPos, err := sa.PosAtMeasure(loMeasure)
	if err != nil {
		return nil, fmt.Errorf("corridorsolve: source window lo: %w", err)
	}
	hiPos, err := sa.PosAtMeasure(hiMeasure)
	if err != nil {
		return nil, fmt.Errorf("corridorsolve: source window hi: %w", err)
	}
	loLat, loLon, err := sa.LatLonAt(loPos)
	if err != nil {
		return nil, err
	}
	hiLat, hiLon, err := sa.LatLonAt(hiPos)
	if err != nil {
		return nil, err
	}
	loPt := Point{Lat: loLat, Lon: loLon}
	hiPt := Point{Lat: hiLat, Lon: hiLon}
	if loPos.Edge == hiPos.Edge {
		return [][2]Point{{loPt, hiPt}}, nil
	}
	// One arc per source edge crossed, ends trimmed: known here.
	arcs := make([][2]Point, 0, hiPos.Edge-loPos.Edge+1)
	arcs = append(arcs, [2]Point{loPt, sa.Pts[loPos.Edge+1]})
	for e := loPos.Edge + 1; e < hiPos.Edge; e++ {
		arcs = append(arcs, [2]Point{sa.Pts[e], sa.Pts[e+1]})
	}
	arcs = append(arcs, [2]Point{sa.Pts[hiPos.Edge], hiPt})
	return arcs, nil
}

// candidateCanonEdgeAt returns the index of pc's canon edge containing measure
// position `at` — [SourceMeasure.posAtMeasure]'s own binary search, ported here
// because [PreppedCandidate] predates [SourcePos] and has no type of its
// own to carry it.
func candidateCanonEdgeAt(pc *PreppedCandidate, at float64) int {
	e := sort.Search(len(pc.cumMeasure), func(k int) bool { return pc.cumMeasure[k] >= at }) - 1
	e = max(e, 0)
	e = min(e, len(pc.Verts)-2)
	return e
}

// canonGapCostBetween sums [canonGapCost] over every canon edge of pc that
// overlaps [canonLo, canonHi], trimming the FIRST and LAST edge to their
// exact overlapping sub-range ([canonGapCost]'s uLo/uHi) so a near-zero
// canon gap costs near zero rather than a whole edge's worth — canon edges
// are not densified (one measured edge is 1,956.8 m), so this trim is the
// common case, not a rare one.
func canonGapCostBetween(ctx context.Context, pc *PreppedCandidate, canonLo, canonHi float64, srcArcs [][2]Point, r float64, k int) (float64, error) {
	if pc == nil {
		return 0, fmt.Errorf("corridorsolve: nil candidate")
	}
	lo, hi := math.Max(0, canonLo), math.Min(pc.Total, canonHi)
	if hi <= lo {
		return 0, nil
	}
	eLo, eHi := candidateCanonEdgeAt(pc, lo), candidateCanonEdgeAt(pc, hi)
	if eHi < eLo {
		eHi = eLo
	}
	total := 0.0
	for e := eLo; e <= eHi; e++ {
		elen := pc.cumMeasure[e+1] - pc.cumMeasure[e]
		if elen == 0 {
			continue // zero-length canon edge (duplicated vertex): zero-measure, no contribution
		}
		uLo, uHi := 0.0, 1.0
		if e == eLo {
			uLo = (lo - pc.cumMeasure[e]) / elen
		}
		if e == eHi {
			uHi = (hi - pc.cumMeasure[e]) / elen
		}
		c, err := canonGapCost(ctx, pc.Verts[e], pc.Verts[e+1], srcArcs, elen, uLo, uHi, r, k)
		if err != nil {
			return 0, fmt.Errorf("corridorsolve: canon edge %d: %w", e, err)
		}
		total += c
	}
	return total, nil
}

// ---- the graph and Dijkstra ------------------------------------------

type corridorNode struct {
	layer int
	state CorridorStateID
}

// packLayerSlot collapses a (layer, slot-within-layer) pair into one int64 —
// the address the Dijkstra search uses throughout.
//
// ⚠ WHY A SLOT AND NOT A STATE, MEASURED TWICE. dist and prev began as
// map[corridorNode]…, a 4-field struct key hashed on every one of millions of
// accesses: runtime.mapaccess2 was 13% of a single 5,000-point import.
// Packing the state into an int64 key removed the hashing but not the map, and
// on a denser leg the map itself became the largest single cost in the whole
// run — 34% in mapaccess2 plus 14% in madvise, purely from growing it. A
// slot indexes a dense per-layer array instead, so a relaxation is a bounds
// check and a store.
//
// The layout is fixed and needs no arithmetic to invert: a layer's slots are
// its statesBy entries in order, so the state at (layer, slot) is simply
// statesBy[layer][slot]. Slots fit in 32 bits with room to spare — the worst
// corpus leg has ~6e4 states at a layer — and layers likewise.
func packLayerSlot(layer int, slot int32) int64 {
	return int64(layer)<<32 | int64(uint32(slot))
}

// unpackLayerSlot is packLayerSlot's exact inverse.
func unpackLayerSlot(key int64) (layer int, slot int32) {
	return int(key >> 32), int32(uint32(key))
}

// CorridorPackedState is one [CorridorStateID] in 4 bytes rather than 24: the
// per-layer state lists hold one entry per (layer, state) and there are a great
// many of them — 2,242,965 on the bundle fixture's worst leg, where the 24 B
// struct costs 53.8 MB of the ~54 MB tables and 4 B says the same thing in
// 9.0 MB. dist (8 B) and prev (8 B) per slot cannot shrink; this one can.
//
// ⚠ THE LISTS ARE NOT THE SOLVE'S DOMINANT LIVE TERM, as this comment used to
// say. Measured same-leg, the lazy priority queue was, until the indexed heap
// replaced it (see TestCorridorSolve_Footprint). Now no one term dominates: on the full
// bundle's leg 32 dist and prev are 17.17 MiB each, the heap's entries 17.11 and
// the switchIdx slot lists 15.35, of 96.63 accounted. The saving here is real
// and was measured exactly; what was never measured was the share of the solve
// it came out of.
//
// LAYOUT, chosen so the three invariants the solve rests on hold by
// construction rather than by a test:
//
//	bit 31      ON flag — 0 for OFF, 1 for every ON state
//	bits 30..1  SpIdx
//	bit 0       direction — 0 for CorridorFWD, 1 for CorridorREV
//
// The three invariants:
//   - OFF is the value 0, which is LESS than every ON state (all of which have
//     bit 31 set), so ascending packed value puts OFF first — [stateLess]'s
//     first clause, for free.
//   - Ascending packed value then orders by SpIdx (bits 30..1), then FWD before
//     REV (bit 0) — [stateLess]'s remaining two clauses, also for free. So the
//     final-layer tiebreak can read either the packed value or the decoded
//     state and get the same answer; it still reads the decoded state through
//     [stateLess], and TestCorridorPackedState_OrderMatchesStateLess pins the
//     agreement so a future reader may rely on it.
//   - The ON flag is a flag and not "SpIdx == -1", so OFF and SpIdx 0 are
//     different values, exactly as CorridorStateID{Off:true} and
//     CorridorStateID{SpIdx:0} are different structs. Equality on packed values
//     is therefore the same relation as equality on the structs, which is what
//     the "is this the state I popped" tests in the search compare.
//
// ⚠ SpIdx HEADROOM, MEASURED NOT ASSUMED. SpIdx indexes the piece ALPHABET, so
// the bound is the piece count, not the state count, and 30 bits hold
// 1,073,741,824 of them.
//   - bundle-small, all nine legs, read off the real lists
//     (TestCorridorPackedState_FixtureStatesRoundTrip): largest alphabet 6,763
//     pieces, largest SpIdx 6,762 — 158,767x of headroom.
//   - The worst leg in any fixture here is the full bundle fixture's, at
//     2,242,965 STATES summed over its layers. A layer's list is
//     1 + 2 x (pieces covering it), so that leg's alphabet is at most
//     ~1,121,000 pieces even if no piece covered more than one layer — still
//     958x of headroom.
//
// A [SolvePiece] — the element the alphabet is actually made of — measures
// 176 B (a 160 B [CorridorPiece] plus two ints), MEASURED with
// unsafe.Sizeof, not counted by eye. So an alphabet big enough to overflow this
// field would want 189 GB for the pieces alone, long after the solve tables ran
// the box out of memory.
//
// It is still CHECKED, in [SolveCorridorPath], because a silent wrap would
// alias two states onto one slot and quietly corrupt the winning path rather
// than fail.
type CorridorPackedState uint32

const (
	// CorridorPackedOFF is OFF. Zero on purpose — see the ordering note above.
	CorridorPackedOFF CorridorPackedState = 0
	// corridorPackedOn is the ON flag, bit 31.
	corridorPackedOn CorridorPackedState = 1 << 31
	// corridorPackedREV is the direction bit, bit 0, set for CorridorREV.
	corridorPackedREV CorridorPackedState = 1
	// CorridorMaxSolvePieces is the exclusive bound on SpIdx: bits 30..1.
	//
	// Exported for the reference application's tests; not part of the stable API.
	CorridorMaxSolvePieces = 1 << 30
)

// PackCorridorOn packs an ON state. The caller has already checked spIdx
// against [CorridorMaxSolvePieces] (once per solve, on the whole alphabet).
//
// Exported for the reference application's tests; not part of the stable API.
func PackCorridorOn(spIdx int, dir CorridorDir) CorridorPackedState {
	ps := corridorPackedOn | CorridorPackedState(uint32(spIdx))<<1
	if dir == CorridorREV {
		ps |= corridorPackedREV
	}
	return ps
}

// off reports OFF without decoding the rest.
func (ps CorridorPackedState) off() bool { return ps&corridorPackedOn == 0 }

// spIdx is only meaningful when !off.
func (ps CorridorPackedState) spIdx() int { return int((ps &^ corridorPackedOn) >> 1) }

// unpack is the exact inverse of packing: CorridorPackedOFF for OFF,
// PackCorridorOn for an ON state (the tests' packCorridorState packs either).
func (ps CorridorPackedState) Unpack() CorridorStateID {
	if ps.off() {
		return CorridorStateID{Off: true}
	}
	dir := CorridorFWD
	if ps&corridorPackedREV != 0 {
		dir = CorridorREV
	}
	return CorridorStateID{SpIdx: ps.spIdx(), Dir: dir}
}

// candidateOfPacked is [candidateOfState] without the decode.
func candidateOfPacked(ps CorridorPackedState, sps []SolvePiece) int {
	if ps.off() {
		return -1
	}
	return sps[ps.spIdx()].CandIdx
}

// ActiveOverArc reports whether interval i fully covers [x0,x1] — the
// condition for a forward (staying-in-state) edge across that arc to
// exist at all.
//
// EXACT comparison. Before, this function tested at ±1e-6 while
// [clipCorridorPiece] (ArcCost's own piece lookup) tested at exact
// float64 equality: two predicates over the same "is x0..x1 inside iv"
// question, disagreeing by up to 1e-6 m. That let this function offer a
// forward edge for an arc clipCorridorPiece could not find a single piece
// covering, producing a run with no geometry to commit — corridorwire.go's
// own solverEps skips exist ONLY because this mismatch could manufacture
// such a run. x0/x1 are always SolveCorridorPath's own breakpoints, built by
// exact float64 union of every interval/piece boundary (including iv's own),
// so tightening this to exact costs nothing when x0/x1 truly are iv's own
// registered bounds — it only stops the function from claiming coverage past
// them.
//
// sp is a pointer, as in [ArcCost]: the bridge loop calls this for every piece
// of the target interval on every qualifying pop.
//
// Exported for the reference application's tests; not part of the stable API.
func ActiveOverArc(sp *SolvePiece, x0, x1 float64) bool {
	return sp.P.ImportMeasureAtLo <= x0 && sp.P.ImportMeasureAtHi >= x1
}

// CorridorSolveCancelRelaxations spaces [SolveCorridorPath]'s cancellation check.
//
// ⚠ IT SETS STOP SPEED AND CANNOT CHANGE THE ANSWER. The check reads ctx and
// nothing else; on an un-cancelled solve every relaxation takes the branch it
// always took, and no cost, order or tiebreak depends on this number.
//
// IT COUNTS RELAXATIONS, NOT POPS, and that is the whole point. One pop fans out
// over a WHOLE layer (the generic fan-out block below is O(states at the layer) —
// ~6e4 states on one layer of the dwell fixture's worst leg) and the bridge block
// scans every sp per bridge, so "once per pop" is not a bound on the work between
// two checks. Relaxations are: the leg stops after at most this many edges
// examined.
//
// Exported for the reference application's tests; not part of the stable API.
const CorridorSolveCancelRelaxations = 4096

// SolveCorridorPath is the shortest path over (interval,direction)∪{OFF}
// states, layered on import measure, using the emission/backtrack/state-change
// terms of corridorcost.go.
//
// total <= 0 is an EXPECTED absence (a degenerate/zero-length leg, mirroring
// CorridorFollowRuns' own <2-point degenerate-leg contract) — nil, nil, not an
// error.
// An empty (or all-nil-producing) ivs is NOT special-cased: it is the most
// common production leg (no nearby canon at all) and must produce a
// well-formed single-run all-OFF result, cost == total (OFF's own raw-metre
// charge, see the forward-edge comment below), same as any other path
// through this function.
// tunables is optional, for tuning sweeps only (see [CorridorTunables]):
// omitted, it resolves to the production constants (StateChangeCostP,
// backtrackWeightPerM) via [resolveTunables].
//
// ⚠ IT TAKES OWNERSHIP OF ivs: each interval's Pieces are released (set to nil)
// as soon as [BuildCorridorBridges], their last reader, has returned, so that
// one copy of the kept alphabet is not held alongside the solve's own tables.
// A caller that still needs
// ivs[].Pieces afterwards must pass a clone — see the release comment below.
//
// sa is the source polyline, needed ONLY to find the
// competing arcs for a bridge's canon-side gap integral
// ([sourceArcsBetween]) — see [BuildCorridorBridges]. It is never
// dereferenced when ivs is empty (the two degenerate call sites below pass
// nil safely).
//
// Exported for the reference application's tests; not part of the stable API.
func SolveCorridorPath(ctx context.Context, total float64, sa *SourceMeasure, ivs []CorridorInterval, cands []*PreppedCandidate, r float64, k int, tunables ...CorridorTunables) (*CorridorSolveResult, error) {
	hooks := &stateOf(ctx).hooks
	if total < 0 || !allFinite(total) {
		return nil, fmt.Errorf("corridorsolve: total import measure must be finite and non-negative, got %v", total)
	}
	if total == 0 {
		return nil, nil
	}
	if err := validateEvenK(k); err != nil {
		return nil, err
	}
	if !(r > 0) || !allFinite(r) {
		return nil, fmt.Errorf("corridorsolve: radius must be finite and positive, got %v", r)
	}
	t, err := resolveTunables(tunables)
	if err != nil {
		return nil, err
	}
	for i, iv := range ivs {
		if iv.CandIdx < 0 || iv.CandIdx >= len(cands) || cands[iv.CandIdx] == nil {
			return nil, fmt.Errorf("corridorsolve: interval %d has invalid CandIdx %d", i, iv.CandIdx)
		}
	}

	// Shared-endpoint neighbours, built ONCE per solve: which other candidates
	// does candidate c share at least one endpoint coordinate with. Those are
	// exactly the pairs [crossCandidateSwitchCost] can price below P, so they
	// are exactly the pairs the switch-edge block below must relax explicitly;
	// every other cross-candidate pair is served by the flat-P generic fan-out.
	// The price is NOT restated here — this list only says whom to ask about.
	type endpointCoord struct{ lat, lon float64 }
	candsAtCoord := map[endpointCoord][]int{}
	for i, pc := range cands {
		if pc == nil || len(pc.Verts) == 0 {
			continue
		}
		for _, vi := range [2]int{0, len(pc.Verts) - 1} {
			k := endpointCoord{pc.Verts[vi].Lat, pc.Verts[vi].Lon}
			// A loop candidate (verts[0] == verts[last]) would otherwise list
			// itself twice under one coordinate.
			if got := candsAtCoord[k]; len(got) > 0 && got[len(got)-1] == i {
				continue
			}
			candsAtCoord[k] = append(candsAtCoord[k], i)
		}
	}
	candNeighbours := make([][]int, len(cands))
	for _, group := range candsAtCoord {
		for _, i := range group {
			for _, j := range group {
				if i == j {
					continue
				}
				candNeighbours[i] = append(candNeighbours[i], j)
			}
		}
	}
	for i := range candNeighbours {
		// Deduplicate (two candidates can share BOTH ends) and order by
		// candidate index — a map walk is randomised, and relaxation order
		// decides ties, so leaving it to the map would make the solve
		// non-deterministic.
		if len(candNeighbours[i]) == 0 {
			continue
		}
		sort.Ints(candNeighbours[i])
		out := candNeighbours[i][:1]
		for _, j := range candNeighbours[i][1:] {
			if j != out[len(out)-1] {
				out = append(out, j)
			}
		}
		candNeighbours[i] = out
	}

	// Breakpoints: 0, total, every interval boundary, AND every PIECE
	// boundary within every interval, clamped into range.
	//
	// Piece boundaries matter, not just interval boundaries: a piece's own
	// canon measure is monotonic (BandPiece's own guarantee — see CorridorPiece's
	// doc comment), so a direction change (an out-and-back's turnaround, a
	// hairpin's apex) can ONLY happen where one piece ends and the next
	// begins. Without a layer there, the switch mechanism (case 1: direction
	// flip within one interval) has nowhere to fire mid-interval, and the
	// solver is forced to declare one direction for the WHOLE interval —
	// which measurably produced the wrong answer (an out-and-back priced at
	// P + full-backtrack instead of just P) before this was found.
	brk := map[float64]bool{0: true, total: true}
	for _, iv := range ivs {
		lo, hi := math.Max(0, iv.ImportMeasureLo), math.Min(total, iv.ImportMeasureHi)
		if lo < hi {
			brk[lo], brk[hi] = true, true
		}
		for _, p := range iv.Pieces {
			plo, phi := math.Max(0, p.ImportMeasureAtLo), math.Min(total, p.ImportMeasureAtHi)
			if plo < phi {
				brk[plo], brk[phi] = true, true
			}
		}
	}
	// The state alphabet. Built once, here, so every consumer below indexes
	// the same list.
	sps := buildSolvePieces(ivs)
	// ⚠ The alphabet must fit the packed state's SpIdx field, or two states
	// would alias onto one slot and the winning path would be silently wrong.
	// See [CorridorPackedState]'s headroom note: 958x on the full bundle's worst
	// leg, 158,767x on the small bundle's.
	if len(sps) >= CorridorMaxSolvePieces {
		return nil, fmt.Errorf("corridorsolve: %d solve pieces exceeds the packed state's SpIdx range (%d)", len(sps), CorridorMaxSolvePieces)
	}

	bps := make([]float64, 0, len(brk))
	for b := range brk {
		bps = append(bps, b)
	}
	sort.Float64s(bps)
	layerOf := make(map[float64]int, len(bps))
	for i, x := range bps {
		layerOf[x] = i
	}

	// Per-layer state lists, built ONCE.
	//
	// A per-layer scan of the whole alphabet (the deleted statesAtLayer) is
	// a scan of every PIECE rather than every interval — hundreds instead of
	// dozens on a real leg. Running it per Dijkstra pop made the solve O(pops x |sps|) and
	// roughly doubled the test suite's wall time. The states available at
	// a layer do not depend on the search, so they are computed here by
	// bucketing each piece into the layers its own import-measure range covers
	// (binary search on the sorted breakpoints), and the loop below reads
	// them.
	// ⚠ COUNT, THEN FILL — the lists below set the size of every slot-indexed
	// table in the solve and they are append-grown no longer. (They are
	// not the dominant live term: measured same-leg, statesBy is 5% of a
	// leg's solve bytes — TestCorridorSolve_Footprint. What
	// they DO decide is the slot count, which dist and prev then pay 8 B each
	// for, so the count is worth getting exactly right whatever its own width.)
	// Measured at 1.43x their exact fit when grown by append: a
	// layer's list is 1 + 2 x (pieces
	// covering it), and the doubling overshoot is held for the whole solve, not
	// just while it is being built. The counting pass is the SAME scan as the
	// filling pass, in the same order, so it cannot see a different population;
	// the contiguity guard lives in it, and therefore still fires on the first
	// offending (piece, layer) exactly as before.
	pieceLoLayer := make([]int32, len(sps))
	pieceSpan := make([]int32, len(sps))
	statesAt := make([]int32, len(bps))
	for l := range statesAt {
		statesAt[l] = 1 // OFF, appended first at every layer
	}
	for i, sp := range sps {
		lo := sort.SearchFloat64s(bps, sp.P.ImportMeasureAtLo)
		l := lo
		for ; l < len(bps) && bps[l] <= sp.P.ImportMeasureAtHi; l++ {
			if bps[l] < sp.P.ImportMeasureAtLo {
				return nil, fmt.Errorf("corridorsolve: piece %d covers layer %d (measure %v) below its own ImportMeasureAtLo %v — the layers a piece covers must be contiguous", i, l, bps[l], sp.P.ImportMeasureAtLo)
			}
			statesAt[l] += 2
		}
		if l > lo {
			pieceLoLayer[i] = int32(lo)
			pieceSpan[i] = int32(l - lo)
		}
	}

	// ⛔ REFUSE BEFORE ALLOCATING. The count pass above knows Σ_l statesAt[l]
	// BEFORE statesBy, dist, prev, pieceSlots and the search heap exist, so a
	// leg whose tables would not fit is failed here with an error rather than
	// allocating them and hoping. The heap is in the projection: it holds each state at most once and is allocated at projectedSlots.
	//
	// ⚠ IT READS THE PROJECTED SIZE AND NOTHING ELSE, AND CANNOT CHANGE THE
	// ANSWER — the same property as CorridorSolveCancelRelaxations' stop check.
	// On every leg that fits, this adds one sum over statesAt and one
	// comparison: no allocation, no cost, no ordering, no tiebreak. The leg
	// that does not fit gets no answer at all, so there is nothing for it to
	// change.
	//
	// ⚠ AN ERROR, NEVER A FALLBACK. It does not degrade, sample, solve a
	// subset or hand the leg to another matcher: the solve fails with a
	// message instead of the process dying, and the caller is expected to
	// abort its whole write on it, with no fallback.
	projectedSlots := 0
	for _, n := range statesAt {
		projectedSlots += int(n)
	}
	if budget := hooks.SolveSlotTableBudgetBytes; budget > 0 {
		if projected := CorridorProjectedSlotBytes(len(bps), projectedSlots, len(sps)); projected > budget {
			return nil, fmt.Errorf("corridorsolve: this leg's state tables and search heap need %d bytes (%.1f MB) for %d slots over %d layers from a %d-piece alphabet, above the %d byte (%.1f MB) budget — refused before allocating. "+
				"⚠ THIS BOUNDS THE PROJECTABLE TABLES ONLY (statesBy, dist, prev, pieceSlots, and the search heap with its position table): it does NOT cover the lazily built switchIdx maps, the piece alphabet or the bookkeeping. It is not the solve's total footprint and must not be scaled into one",
				projected, float64(projected)/(1<<20), projectedSlots, len(bps), len(sps),
				budget, float64(budget)/(1<<20))
		}
	}

	// ⚠ PACKED, NOT CorridorStateID. 4 B per slot instead of 24;
	// the order of these lists, and OFF's slot 0, are unchanged — see
	// [CorridorPackedState] for why the packing preserves both by construction.
	statesBy := make([][]CorridorPackedState, len(bps))
	for i := range statesBy {
		statesBy[i] = make([]CorridorPackedState, 0, statesAt[i])
		statesBy[i] = append(statesBy[i], CorridorPackedOFF)
	}
	// A state's SLOT within its layer, recorded as the layer lists are built.
	//
	// The search below addresses (layer, slot), not (layer, state): the
	// Dijkstra tables are one dense array per layer rather than a map keyed on
	// the packed state, because on a dense leg the map was the single largest
	// cost in the whole import — 34% of it in mapaccess2 alone, plus the
	// allocation churn of growing it (madvise, another 14%). Every lookup the
	// search needs is therefore resolved here, once, into an array index.
	//
	// OFF is appended first at every layer, so OFF's slot is always 0.
	// A piece's two states are appended adjacently, FWD then REV, so its REV
	// slot is always its FWD slot + 1. The layers one piece covers are
	// CONTIGUOUS — sort.SearchFloat64s returns the first index whose
	// breakpoint is >= the piece's own ImportMeasureAtLo, so the guard below can
	// never fire — which is what lets pieceSlots be indexed by
	// (layer - pieceLoLayer). It is an error rather than a `continue` because
	// if that ever stopped holding, silently skipping would corrupt every
	// subsequent slot in the piece.
	// Sized from the piece's own layer span, counted above.
	pieceSlots := make([][]int32, len(sps))
	for i := range pieceSlots {
		pieceSlots[i] = make([]int32, 0, pieceSpan[i])
	}
	for i := range sps {
		lo := int(pieceLoLayer[i])
		for l := lo; l < lo+int(pieceSpan[i]); l++ {
			pieceSlots[i] = append(pieceSlots[i], int32(len(statesBy[l])))
			statesBy[l] = append(statesBy[l],
				PackCorridorOn(i, CorridorFWD),
				PackCorridorOn(i, CorridorREV))
		}
	}
	// slotAt is the inverse: which slot does this state occupy at this layer?
	// -1 when the state is not available there at all.
	slotAt := func(layer int, s CorridorStateID) int32 {
		if s.Off {
			return 0
		}
		off := layer - int(pieceLoLayer[s.SpIdx])
		slots := pieceSlots[s.SpIdx]
		if off < 0 || off >= len(slots) {
			return -1
		}
		slot := slots[off]
		if s.Dir == CorridorREV {
			slot++
		}
		return slot
	}

	// Per-layer switch-edge indexes. See the switch-edge block in the Dijkstra
	// loop below for why these exist: only two kinds of state pair are priced
	// at anything other than a flat P, and these two lists are exactly those
	// pairs' participants, so the expensive per-pop scan only has to look at
	// them rather than at the layer's whole state list.
	//
	// Built LAZILY, per layer, because a solve typically reaches only a
	// fraction of its layers' states and building all of them up front costs
	// more than the scan it saves.
	type layerSwitchIndex struct {
		byCand map[int][]int32
	}
	switchIdx := make([]*layerSwitchIndex, len(bps))
	switchIndexAt := func(layer int) *layerSwitchIndex {
		if ix := switchIdx[layer]; ix != nil {
			return ix
		}
		ix := &layerSwitchIndex{byCand: map[int][]int32{}}
		for slot, ps := range statesBy[layer] {
			if ps.off() {
				continue
			}
			ix.byCand[sps[ps.spIdx()].CandIdx] = append(ix.byCand[sps[ps.spIdx()].CandIdx], int32(slot))
		}
		switchIdx[layer] = ix
		return ix
	}
	// genericDone[l] records that layer l's flat-P fan-out has already been
	// done from the cheapest state there — see the switch-edge block.
	genericDone := make([]bool, len(bps))
	runnerUpDone := make([]bool, len(bps))
	minCandAt := make([]int, len(bps))

	// Bridge edges (this file's header, "bridge edges, the fourth cost
	// term"). Priced once, up front — same discipline as breakpoints above —
	// then indexed two ways: bridgesFrom for Dijkstra relaxation (by the
	// departure interval), bridgeByPair for path reconstruction (by the
	// (from,to) pair, to recover CanonLo/CanonHi without recomputing them).
	bridges, err := BuildCorridorBridges(ctx, sa, ivs, cands, r, k, t.P)
	if err != nil {
		return nil, err
	}
	bridgesFrom := map[int][]CorridorBridge{}
	bridgeByPair := map[[2]int]CorridorBridge{}
	for _, br := range bridges {
		bridgesFrom[br.FromIv] = append(bridgesFrom[br.FromIv], br)
		bridgeByPair[[2]int{br.FromIv, br.ToIv}] = br
	}

	// THE PIECE ALPHABET IS RELEASED HERE.
	//
	// [BuildCorridorBridges] above is the last reader of iv.Pieces
	// (via [canonMeasureAtImportMeasure]). Everything below needs only each
	// interval's two import-measure bounds — the breakpoint set is already
	// built, [buildSolvePieces] has already taken its own copy of every piece
	// into sps, and the three later interval reads (the bridge-departure test,
	// the bridge target's layer, [ActiveOverArc]'s bounds) are Lo/Hi only. So
	// the bounds are copied out and the Pieces slices dropped BEFORE dist,
	// prev and the per-layer tables below are allocated, which is where this
	// solve's own peak live memory is: without this, one whole copy of the kept
	// alphabet stays reachable beside the solve's tables for no reason.
	//
	// ⚠ THIS FUNCTION TAKES OWNERSHIP OF ivs. It writes nil into every
	// ivs[i].Pieces, so a CALLER MUST NOT READ ivs[].Pieces AFTER CALLING IT,
	// and must not call it twice with the same slice. Production's only caller
	// ([CorridorFollowRuns], corridorwire.go) drops ivs at this call; the two
	// test callers that do keep reading pass a clone
	// (cloneCorridorIntervals, a test helper).
	ivBounds := make([]CorridorIvBounds, len(ivs))
	for i := range ivs {
		ivBounds[i] = CorridorIvBounds{
			ImportMeasureLo: ivs[i].ImportMeasureLo,
			ImportMeasureHi: ivs[i].ImportMeasureHi,
		}
		ivs[i].Pieces = nil
	}

	// Dijkstra over (layer,state) nodes: forward edges cross one arc at
	// ArcCost; switch edges move within a layer at TransitionCostT; bridge
	// edges skip straight to a later interval of the same candidate at
	// bridgeCost. All three are non-negative, so plain Dijkstra applies.
	dist := make([][]float64, len(bps))
	prev := make([][]int64, len(bps))
	for l := range bps {
		d := make([]float64, len(statesBy[l]))
		p := make([]int64, len(statesBy[l]))
		for i := range d {
			d[i] = math.Inf(1)
			p[i] = -1
		}
		dist[l], prev[l] = d, p
	}
	dist[0][0] = 0 // (layer 0, OFF)
	// The search queue: an INDEXED heap ([corridorHeap]) holding each state at
	// most once, ordered by (dist, packed key). It replaced a lazy queue that
	// pushed a duplicate item on every improvement and discarded the stale one
	// when it popped — 4,359,193 items (80.84 MiB held) for 2,242,965 states on
	// the largest fixture's worst leg. Both pop the same sequence of live
	// states under the same total order, which a per-leg digest comparison
	// proved on every leg it solved.
	//
	// ⚠ ALLOCATED ONCE, AT FULL SIZE, FROM THE COUNT PASS. A state is in the heap
	// at most once, so projectedSlots bounds it exactly and there is no append
	// growth. It is charged to the up-front refusal ([CorridorProjectedSlotBytes])
	// for that reason: every byte it will hold is known before the first relax.
	// An insert past that capacity is the heap's own error, and it would mean
	// the count pass under-counted — it is returned, never absorbed.
	pq, err := newCorridorHeap(dist, projectedSlots)
	if err != nil {
		return nil, err
	}
	if err := pq.Insert(packLayerSlot(0, 0)); err != nil {
		return nil, err
	}
	if hooks.corridorSolveHeap != nil {
		hooks.corridorSolveHeap(pq)
	}
	// The heap's high-water mark, for [CorridorSolveFootprint]. It observes and
	// cannot change the answer: nothing downstream reads it. The bytes are fixed
	// at construction now, so this is a count, not the memory — kept because the
	// footprint guard checks it against States.
	pqPeakLen := pq.Len()

	// uKey is the key of the state currently being expanded; relax closes over
	// it so the three edge kinds below share one relaxation, and so the
	// closure is allocated once rather than once per pop.
	var uKey int64
	// Cost census counters. Incremented
	// UNCONDITIONALLY rather than behind a nil check: an int increment in a
	// closure the loop already allocates is cheaper than loading and comparing
	// the observer interface on every one of the hundreds of millions of
	// relaxations a dense leg makes, and neither can change an answer.
	// relaxations counts edges EXAMINED, the quantity the stop check is spaced
	// by ([CorridorSolveCancelRelaxations]); pqPushes counts every improvement that reaches the
	// heap — an insert or a decrease — so the seeded start item is not one of
	// them; pqInserts counts the inserts alone, so a completed solve has
	// pqPops == pqInserts + 1 (the seed).
	var relaxations, pqPushes, pqInserts, pqPops int
	// The solve's stop. stopErr, once set, makes relax a no-op that does not
	// even count, so the census below reports exactly the relaxations done up to
	// the check that stopped — the quantity the stop bound is stated in.
	// The search then leaves its loop at the next pop.
	//
	// TWO different checks set it, and it is named for what it does rather than
	// for either of them: ctx cancellation, and the heap's own refusals (an
	// insert past its capacity, or an improvement to a state already popped).
	// Both are stops, both report the same census, and both are returned as an
	// error with no partial result — so one field, not two, and no second
	// branch on the hot path.
	var stopErr error
	relax := func(layer int, slot int32, nd float64) {
		if stopErr != nil {
			return
		}
		relaxations++
		if relaxations%CorridorSolveCancelRelaxations == 0 {
			if err := ctx.Err(); err != nil {
				stopErr = fmt.Errorf("corridorsolve: cancelled after %d relaxations: %w", relaxations, err)
				return
			}
		}
		if nd < dist[layer][slot]-1e-12 {
			dist[layer][slot] = nd
			prev[layer][slot] = uKey
			pqPushes++
			// dist is written FIRST: the heap orders by reading it.
			inserted, err := pq.Improve(packLayerSlot(layer, slot))
			if err != nil {
				stopErr = fmt.Errorf("corridorsolve: %w (after %d relaxations over %d layers)", err, relaxations, len(bps))
				return
			}
			if inserted {
				pqInserts++
				if n := pq.Len(); n > pqPeakLen {
					pqPeakLen = n
				}
			}
		}
	}

	for pq.Len() > 0 {
		if stopErr != nil {
			break // set inside relax; reported by the census below, returned after it
		}
		topKey := pq.Pop()
		pqPops++
		uLayer, uSlot := unpackLayerSlot(topKey)
		// uPacked is the popped state as it is stored; the loops below compare
		// against it packed rather than decoding twice. Packed equality is the
		// same relation as CorridorStateID equality — see [CorridorPackedState].
		uPacked := statesBy[uLayer][uSlot]
		u := corridorNode{layer: uLayer, state: uPacked.Unpack()}
		uKey = topKey
		// No stale check: the heap holds each state once, at its current dist,
		// so every pop is live. The lazy queue's `top.dist > dist + 1e-9` test
		// was deleted with it — as dead code, not under the zero-tolerance rule,
		// which leaves cost comparisons such as relax's 1e-12 alone. Its 1e-9
		// window used to let a stale item within a nanometre re-expand a state
		// to no effect (714,376 such relaxations on the randomised test corpus, none of which
		// improved anything); the heap simply does not make them.
		du := dist[uLayer][uSlot]

		// Switch edges: to every other state valid at this same layer.
		//
		// Read [TransitionCostT] as a price list and only two kinds of pair
		// cost anything other than a FLAT P: same-candidate pairs (a
		// turnaround plus the arm-jump term — which can come out cheaper OR
		// dearer than P) and cross-candidate pairs that share a network node,
		// which pay min(P, BacktrackWeight * the canon-graph distance through
		// it) — cheaper than P, and 0 only when both feet are AT the node.
		// Everything else — every OFF<->ON pair, every cross-candidate pair
		// with no shared endpoint — is exactly P, independent of which state
		// you came from.
		//
		// That independence is what makes the fan-out a per-LAYER job rather
		// than a per-POP one. Dijkstra pops a layer's states in increasing
		// final distance, so the FIRST state popped at a layer is the cheapest
		// generic source there, and every later pop's generic edge is
		// dominated: dist[u] >= dist[u0], so dist[u]+P >= dist[u0]+P. Doing it
		// once per layer is O(states); doing it once per pop was O(states^2),
		// and putting the PIECE in the state multiplied the state count and
		// turned that square into a regression (one test file took minutes
		// instead of seconds). The prices are
		// unchanged — this is the same graph, enumerated without the
		// duplicates.
		//
		// The two non-generic kinds stay per-pop, because for those the source
		// state genuinely changes the price. Same-candidate pairs are
		// EXCLUDED from the generic fan-out rather than merely also priced
		// explicitly: their true cost can exceed P, so serving one with a
		// generic edge would UNDERCHARGE it. Shared-endpoint cross-candidate
		// pairs need no such exclusion — [crossCandidateSwitchCost] is capped
		// at P, so the explicit edge can only ever beat the generic one the
		// fan-out may have laid down first, never undercut a truer price.
		genericP := stateChangeCost(t.P)
		uCand := candidateOfState(u.state, sps)
		if !genericDone[u.layer] {
			genericDone[u.layer] = true
			minCandAt[u.layer] = uCand
			for slot, ps := range statesBy[u.layer] {
				if ps == uPacked || candidateOfPacked(ps, sps) == uCand {
					continue
				}
				relax(u.layer, int32(slot), du+genericP)
			}
		} else if !runnerUpDone[u.layer] && uCand != minCandAt[u.layer] {
			runnerUpDone[u.layer] = true
			for slot, ps := range statesBy[u.layer] {
				if candidateOfPacked(ps, sps) != minCandAt[u.layer] {
					continue
				}
				relax(u.layer, int32(slot), du+genericP)
			}
		}
		if !u.state.Off {
			usp := sps[u.state.SpIdx]
			ix := switchIndexAt(u.layer)
			for _, slot := range ix.byCand[usp.CandIdx] {
				ps := statesBy[u.layer][slot]
				if ps == uPacked {
					continue
				}
				c := TransitionCostT(u.state, ps.Unpack(), bps[u.layer], sps, cands, t)
				relax(u.layer, slot, du+c)
			}
			// Cross-candidate switches to a SHARED-ENDPOINT neighbour. Only
			// these can be priced below the flat P the generic fan-out
			// already laid down ([crossCandidateSwitchCost]), and the price
			// depends on u's own foot position, so unlike the generic edge it
			// cannot be done once per layer from the cheapest state. Every
			// other cross-candidate pair is left to the fan-out.
			for _, nb := range candNeighbours[usp.CandIdx] {
				for _, slot := range ix.byCand[nb] {
					c := TransitionCostT(u.state, statesBy[u.layer][slot].Unpack(), bps[u.layer], sps, cands, t)
					relax(u.layer, slot, du+c)
				}
			}
		}

		// Bridge edges: only fire when u sits EXACTLY at the departure
		// interval's far import-measure boundary — the one layer BuildCorridorBridges
		// priced this edge from. u.state.Off is excluded implicitly: OFF never
		// appears as a bridgesFrom key (only real interval indices do).
		if !u.state.Off && bps[u.layer] == ivBounds[sps[u.state.SpIdx].IvIdx].ImportMeasureHi {
			for _, br := range bridgesFrom[sps[u.state.SpIdx].IvIdx] {
				toLayer, ok := layerOf[ivBounds[br.ToIv].ImportMeasureLo]
				if !ok {
					return nil, fmt.Errorf("corridorsolve: bridge target iv %d's ImportMeasureLo is not a breakpoint — internal inconsistency", br.ToIv)
				}
				// The bridge lands on the target INTERVAL; which of its pieces
				// carries the line from there is a choice, so every piece
				// available at that layer gets an edge and the solve picks.
				for j := range sps {
					sp := &sps[j]
					if sp.IvIdx != br.ToIv {
						continue
					}
					if !ActiveOverArc(sp, ivBounds[br.ToIv].ImportMeasureLo, ivBounds[br.ToIv].ImportMeasureLo) {
						continue
					}
					toSlot := slotAt(toLayer, CorridorStateID{SpIdx: j, Dir: u.state.Dir})
					if toSlot < 0 {
						// ActiveOverArc said this piece carries the bridge
						// target, and the layer lists say it is not there. The
						// two tests were reconciled deliberately (7fa634c69);
						// a disagreement is a real inconsistency, not a case
						// to skip past.
						return nil, fmt.Errorf("corridorsolve: bridge target piece %d is active at measure %v but has no state at layer %d", j, ivBounds[br.ToIv].ImportMeasureLo, toLayer)
					}
					relax(toLayer, toSlot, du+br.Cost)
				}
			}
		}

		// Forward edge: cross the arc [bps[u.layer], bps[u.layer+1]] in
		// the same state, if that state is available for the WHOLE arc.
		//
		// OFF is NOT free per metre: the bridge/no-bridge inequality prices it explicitly
		// (noBridgeCost = 2P + offImportMetres, corridorcost.go), and
		// without that charge the shortest path always prefers never
		// adopting anything — OFF's flat 0 would beat paying P for ANY
		// candidate, however perfectly it overlays the source, since
		// emission can only add cost, never remove the entry fee. Charging
		// OFF its own raw import metres (unweighted, matching
		// noBridgeCost's own literal term) is what makes "adopt a candidate
		// sitting right on top of the source" and "flip direction on an
		// out-and-back rather than abandon the return leg" actually win.
		if u.layer+1 < len(bps) {
			x0, x1 := bps[u.layer], bps[u.layer+1]
			avail := u.state.Off
			arcC := x1 - x0 // OFF's own cost, overwritten below when ON
			if !u.state.Off {
				sp := &sps[u.state.SpIdx]
				if ActiveOverArc(sp, x0, x1) {
					avail = true
					c, err := ArcCost(sp, u.state.Dir, x0, x1, r, k, t.BacktrackWeight)
					if err != nil {
						return nil, err
					}
					arcC = c
				} else {
					arcC = 0 // unreachable: not avail, forward edge skipped below
				}
			}
			if avail {
				vSlot := slotAt(u.layer+1, u.state)
				if vSlot < 0 {
					// Same reconciliation as the bridge case above:
					// ActiveOverArc says the state spans this whole
					// arc, so it must exist at the far end of it.
					return nil, fmt.Errorf("corridorsolve: state %v is active over [%v,%v] but has no state at layer %d", u.state, x0, x1, u.layer+1)
				}
				relax(u.layer+1, vSlot, du+arcC)
			}
		}
	}

	// Cost census. Reported here, where the search has finished and
	// nothing after this point relaxes an edge. A leg that errors below
	// delivers no census at all — see [CorridorCostObserver.LegFinished]. A
	// CANCELLED search reaches this line too, deliberately: its relaxation count
	// is what the stop bound is asserted from, and it is a real cost.
	if obs := hooks.CorridorCost; obs != nil {
		states := 0
		for _, ss := range statesBy {
			states += len(ss)
		}
		obs.SolveFinished(len(bps), states, relaxations, pqPushes, pqInserts, pqPops)
	}

	// The solve's own allocation footprint, term by term. Emitted from
	// the same place as the census and for the same reason: the search has
	// finished, so every capacity below is final, and a CANCELLED search
	// reaches this line too.
	//
	// The switch indexes are summed HERE rather than inside
	// [corridorSolveFootprintOf] because layerSwitchIndex is local to this
	// function; only its measured totals cross the boundary.
	if obs := hooks.CorridorSolveFootprint; obs != nil {
		var sw corridorSwitchIndexUse
		for _, ix := range switchIdx {
			if ix == nil {
				continue
			}
			sw.Layers++
			for _, slots := range ix.byCand {
				sw.Entries++
				sw.SlotCap += cap(slots)
			}
		}
		f := corridorSolveFootprintOf(bps, statesBy, sps, dist, prev, pieceSlots, ivBounds, sw, pq, pqPeakLen)
		f.ProjectedSlotBytes = CorridorProjectedSlotBytes(len(bps), projectedSlots, len(sps))
		obs(f)
	}
	if stopErr != nil {
		// A stop is an error, never a partial result: dist/prev are half-relaxed
		// here, so nothing below may read them.
		return nil, stopErr
	}

	// Pick the cheapest state at the final layer. Deterministic tiebreak on
	// an exact cost tie ([corridorFinalPick]): prefer OFF, then lowest IvIdx, then FWD before REV — id-sorted
	// candidates (canonCandidatesForLeg) make IvIdx ascending the same
	// tiebreak as arc id ascending.
	last := len(bps) - 1
	bestState, bestCost, found := corridorFinalPick(statesBy[last], dist[last])
	if !found {
		return nil, fmt.Errorf("corridorsolve: no path reached the final layer — this should be unreachable, OFF is always available")
	}

	// Reconstruct the path by walking prev[] back from (last,bestState), then
	// reverse and merge adjacent runs sharing a state.
	type step struct {
		layer int
		state CorridorStateID
	}
	var path []step
	bestSlot := slotAt(last, bestState)
	if bestSlot < 0 {
		return nil, fmt.Errorf("corridorsolve: winning state %v has no slot at the final layer %d", bestState, last)
	}
	curKey := packLayerSlot(last, bestSlot)
	for {
		cl, cs := unpackLayerSlot(curKey)
		path = append(path, step{cl, statesBy[cl][cs].Unpack()})
		p := prev[cl][cs]
		if p < 0 {
			break
		}
		curKey = p
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	// ── THE PAID TELEPORT MUST BE DRAWN ────────────────────────────────────
	//
	// A cross-candidate switch whose price came from a shared node
	// ([crossCandidateSharedPair]) bought the canon foot_A -> V -> foot_B, at
	// BacktrackWeight per metre. The runs either side must CONTAIN it, or the
	// cost function priced geometry the assembler never draws — the same class
	// of defect [ArcCost]'s header records for the emission term.
	//
	// This is NOT a snap and NOT a placement heuristic. It is the rule case 2
	// already states for a same-candidate switch ("a continuation carrying the
	// skipped canon, not a seam", see the merge loop below): the walker moved
	// continuously through V, so the drawn stretch runs up to V and resumes
	// from V. Recording it HERE is the point — the solver is the only thing
	// that knows WHICH node was paid for. The caller's 15 m node absorb downstream is a
	// proximity GUESS at the same question, and when it guesses a different
	// node it warps the leg to it and the ride stops resolving.
	//
	// ⚠ ONLY WHERE IT IS A CONTINUATION, on the merge loop's terms with one
	// stated difference. A run whose canon measure was moving AWAY from V is at
	// a genuine turnaround, and turnarounds mint nodes by design;
	// extending it to V would draw canon backwards over ground the walker did
	// not cover.
	//
	// ⚠ A CHAIN OF SWITCHES IS DRAWN HOP BY HOP. Several switches at ONE layer buy a walk through several nodes:
	// A's foot -> V1 -> across B -> V2 -> C's foot. Every intermediate
	// candidate is drawn as a zero-width run ([chainHopRun]), so the leg
	// references B instead of committing a copy of it. That includes a chain
	// that comes back to a candidate it already visited — a loop at the end
	// of a stub used as a U-turn — which is drawn as it was paid for: out to
	// the loop, round it, back down the stub. It is drawn rather
	// than priced out of the solve, which would have cost a second copy of
	// every state (+68 MiB on the worst leg).
	//
	// THE DIFFERENCE: a degenerate (zero-width) run has no direction of its
	// own, and the carry then takes the extension UNCONDITIONALLY, where
	// the merge loop resolves the same ambiguity by borrowing the NEXT run's
	// direction (its `strideRev = rRev` when `lastDegen`). The merge loop has a
	// neighbour to borrow from because it is comparing two runs; the carry is
	// deciding one end of one run against a node, and there is no second run in
	// hand. Taking the extension is the direction that draws the canon the
	// switch paid for, which is the whole point of the carry.
	//
	// pendingCarryLo is the eB owed to the NEXT run appended, and it is keyed
	// on the candidate so it can only be taken by a run of the candidate the
	// switch landed on. In a chained A->B->C switch B is now drawn
	// ([chainHopRun]), so B's run takes B's measure and C's switch sets C's.
	// pendingCarryDir and pendingCarryIB travel with it: the direction the
	// switch was priced for and the vertex index EB came from, which is what the
	// receiving end's structural test reads (see [carryEndIsExtreme]).
	pendingCarryLo, pendingCarryCand := 0.0, -1
	pendingCarryDir, pendingCarryIB := CorridorFWD, -1
	var runs []CorridorRun
	for i := 0; i+1 < len(path); i++ {
		if path[i].layer == path[i+1].layer {
			// A switch step: no import-measure extent, but it may have paid to
			// teleport through a shared node, in which case it decides where
			// the runs either side of it stop and start.
			from, to := path[i].state, path[i+1].state
			x := bps[path[i].layer]
			// ⚠ AN INTERMEDIATE STATE OF A SAME-LAYER CHAIN IS DRAWN. A state reached by a switch and left
			// by another switch at this same layer holds no import extent, but
			// the chain's switches paid for canon across it: into it from the
			// end the first switch arrived at, out of it to the end the next
			// one leaves by. Until this was drawn the leg committed less canon
			// than the solve charged for — on the corpus, 132 two-switch
			// chains committed a straight duplicate of the middle arc instead
			// of referencing it — and a chain that came back to its own
			// candidate the other way round (a loop at the end of a stub used
			// as a U-turn) reached the Dir check below with a run that was not
			// the one the switch was priced for, and aborted the import.
			//
			// It is appended AFTER this switch's carry is handled, so the
			// NEXT switch's sending side finds it as `last` — the run of the
			// very state that switch leaves from — and extends it to the end
			// that switch paid for. See [CorridorRun.ChainHop].
			intermediate := !to.Off && i+2 < len(path) && path[i+2].layer == path[i+1].layer
			if from.Off || to.Off || sps[from.SpIdx].CandIdx == sps[to.SpIdx].CandIdx {
				if intermediate {
					r, err := chainHopRun(to, x, sps, cands, &pendingCarryLo, &pendingCarryCand, pendingCarryDir, pendingCarryIB)
					if err != nil {
						return nil, err
					}
					runs = append(runs, r)
				}
				continue
			}
			sh := crossCandidateSharedPair(&sps[from.SpIdx], &sps[to.SpIdx], from.Dir, to.Dir, cands, x)
			// STRICTLY below P. At or above it the switch was priced as the
			// flat cross-candidate P — it bought no teleport, so it owes no
			// geometry.
			if !sh.OK || !(t.BacktrackWeight*sh.Dist < stateChangeCost(t.P)) {
				if intermediate {
					r, err := chainHopRun(to, x, sps, cands, &pendingCarryLo, &pendingCarryCand, pendingCarryDir, pendingCarryIB)
					if err != nil {
						return nil, err
					}
					runs = append(runs, r)
				}
				continue
			}
			if n := len(runs); n > 0 {
				last := &runs[n-1]
				if !last.State.Off && !last.Bridged &&
					last.CandIdx == sps[from.SpIdx].CandIdx &&
					last.ImportMeasureHi == x {
					if err := checkCarrySend(last, n-1, from.Dir, sh.IA, x, cands); err != nil {
						return nil, err
					}
					last.CanonMeasureHi = sh.EA
				}
			}
			pendingCarryLo, pendingCarryCand = sh.EB, sps[to.SpIdx].CandIdx
			pendingCarryDir, pendingCarryIB = to.Dir, sh.IB
			if intermediate {
				r, err := chainHopRun(to, x, sps, cands, &pendingCarryLo, &pendingCarryCand, pendingCarryDir, pendingCarryIB)
				if err != nil {
					return nil, err
				}
				runs = append(runs, r)
			}
			continue
		}
		lo, hi := bps[path[i].layer], bps[path[i+1].layer]
		from, st := path[i].state, path[i+1].state

		// A bridge is the ONLY edge kind that both spans more than one
		// layer AND changes state.IvIdx — a plain forward edge always keeps
		// u.state fixed (that is its definition), and a switch edge is
		// always same-layer (caught by the "continue" above). So this
		// condition alone identifies a bridge step; bridgeByPair is then
		// consulted only to recover its precomputed CanonLo/CanonHi, not to
		// re-decide whether it IS one.
		bridged := false
		candIdx := -1
		var canonLo, canonHi float64
		if !st.Off {
			sp := sps[st.SpIdx]
			candIdx = sp.CandIdx
			// A non-bridged run lies wholly inside ONE piece, so its oriented
			// canon measure at each end is exact and needs no resolution. This is
			// what lets corridorwire.go drop coalescePieces entirely.
			//
			// Via [clipCorridorPiece], NOT by interpolating directly: clipping
			// CLAMPS to the piece's own domain, and interpolation extrapolates
			// past it. A run can sit sub-eps outside the piece it was solved
			// on — [ActiveOverArc] and the solver's layering disagree at
			// that scale, which corridorwire.go's own solverEps skips already
			// document — and extrapolating there put a canon measure just off the
			// end of the arc: measured on a randomised test seed, a measure of
			// -2.26e-7 m on a 1598.6 m arc, which the caller's canon split
			// correctly refused. Clipping is also what [ArcCost] prices, so
			// this commits exactly the geometry the solve paid for.
			if cp, ok := clipCorridorPiece(sp.P, lo, hi); ok {
				canonLo, canonHi = cp.CanonMeasureAtLo, cp.CanonMeasureAtHi
			} else {
				canonLo, canonHi = canonMeasureAtPiece(sp.P, lo), canonMeasureAtPiece(sp.P, hi)
			}
		}
		if !from.Off && !st.Off && sps[from.SpIdx].IvIdx != sps[st.SpIdx].IvIdx {
			br, ok := bridgeByPair[[2]int{sps[from.SpIdx].IvIdx, sps[st.SpIdx].IvIdx}]
			if !ok {
				return nil, fmt.Errorf("corridorsolve: interval jump %d->%d at layer %d has no matching bridge — internal inconsistency", sps[from.SpIdx].IvIdx, sps[st.SpIdx].IvIdx, path[i].layer)
			}
			bridged = true
			canonLo, canonHi = br.CanonLo, br.CanonHi
		}

		// A bridged run must never merge into an adjacent same-state run
		// that is NOT itself a bridge (or vice versa): the merged run's
		// single CanonMeasureLo/Hi pair would then silently misrepresent
		// whichever side got absorbed — see CorridorRun's own doc comment
		// on why non-bridged runs deliberately carry no such summary at all.
		if n := len(runs); n > 0 && runs[n-1].State == st && runs[n-1].ImportMeasureHi == lo && runs[n-1].Bridged == bridged {
			runs[n-1].ImportMeasureHi = hi
			if !bridged {
				runs[n-1].CanonMeasureHi = canonHi
			}
			// The same state continuing across a layer is not a run START, so
			// there is nothing here for a pending carry to attach to. Dropping
			// it rather than holding it is deliberate: held, it would attach to
			// some LATER run whose boundary this switch never paid for.
			pendingCarryCand = -1
			continue
		}
		// The other half of the carry-through: a run that STARTS where a paid
		// teleport landed begins at the node. A bridged run is excluded — its
		// canon bounds are the bridge's own precomputed pair and are not this
		// run's to move.
		//
		// The test is the mirror of the far-end one: this run walks AWAY from
		// the node in its own declared direction, so the node is the extreme of
		// the OPPOSITE direction — which is [carryEndIsExtreme] asked with the
		// Dir inverted, exactly as the deleted float test was asked in the
		// opposite sense. Structural for the same reason, and an error for the
		// same reason: a Dir that does not match the one the switch was priced
		// for, or an index that is not that direction's end, means this is not
		// the run the teleport bought.
		if pendingCarryCand >= 0 && candIdx == pendingCarryCand && !bridged {
			if err := checkCarryReceive(st.Dir, canonLo, candIdx, cands, pendingCarryDir, pendingCarryIB); err != nil {
				return nil, err
			}
			canonLo = pendingCarryLo
		}
		pendingCarryCand = -1
		runs = append(runs, CorridorRun{State: st, CandIdx: candIdx, ImportMeasureLo: lo, ImportMeasureHi: hi, Bridged: bridged, CanonMeasureLo: canonLo, CanonMeasureHi: canonHi})
	}
	// Merge adjacent runs that are ONE stretch of the same trail.
	//
	// A CorridorRun is a maximal stretch, not a raw state. Merging on state
	// identity alone was enough while the state was the INTERVAL — a leg
	// riding one interval produced one run. With the piece in the state, an
	// ordinary uninterrupted follow hands off from piece to piece and emits
	// one run each, so the stretch has to be reassembled here on nearly the
	// same terms [mergeAdjacentSameCandidateRuns] uses downstream: same
	// candidate, same direction, and canon measure that CONTINUES rather than
	// doubling back.
	//
	// THE TERMS ARE NOT IDENTICAL, since 8bbf1e091. Downstream now refuses on
	// the MERGED SPAN (merged start == merged end); here we still refuse when
	// BOTH inputs are degenerate, with degeneracy meaning zero-width only
	// (CanonMeasureLo == CanonMeasureHi) — there is no second, single-source-edge
	// clause on this side, because a CorridorRun carries no source-edge
	// endpoints to read one from. That is safe: with the pair-degeneracy
	// refusal in place at least one side has real extent, and `continues` plus
	// the strideRev/lastDegen direction rule mean the merged span runs from
	// last.CanonMeasureLo to r.CanonMeasureHi across it, so this loop cannot
	// produce a zero-extent run the way the downstream one could. The merged-span
	// test is the stronger rule; it is needed downstream and would be a no-op here.
	//
	// Continuation is measure ORDER, exact, no tolerance. A forward stretch must
	// not step back and a reverse one must not step forward; where the source
	// runs offset from canon and cuts a corner the foot jumps forward at the
	// bend bisector, and that is a continuation carrying the skipped canon,
	// not a seam.
	runs = dropEmptyChainHops(runs)
	runs = mergeCorridorRuns(runs)

	if len(runs) == 0 {
		// A single-layer path (total==0 already excluded above, so this is
		// unreachable in practice) — still return a well-formed OFF run
		// rather than an empty result.
		runs = []CorridorRun{{State: CorridorStateID{Off: true}, ImportMeasureLo: 0, ImportMeasureHi: total}}
	}

	// A hook for [TestCorridorSolve_TheTableIsFullyRelaxed], which re-relaxes
	// every edge against the finished table and fails if any of them still
	// improves a distance. nil in production; see the test for why the
	// question is worth asking at all.
	//
	// It fires after the backtrack. Nothing between the search and this line
	// writes dist, statesBy or sps, so the table observers see the finished
	// table.
	if hooks.CorridorSolveTable != nil {
		hooks.CorridorSolveTable(bps, statesBy, sps, cands, dist, t, r, k, ivBounds, layerOf, bridgesFrom)
	}

	return &CorridorSolveResult{Runs: runs, Cost: bestCost}, nil
}

// chainHopRun is the run that draws an INTERMEDIATE state of a same-layer
// switch chain — see the switch step in [SolveCorridorPath]'s run
// reconstruction and [CorridorRun.ChainHop]. It sits at import position x
// with no import extent. Its canon starts where the switch into it arrived:
// the pending carry's end when that switch was a paid teleport onto this
// candidate, and the state's own foot otherwise. It ends at the foot; the
// next switch, if it too is a paid teleport, extends it to the end it leaves
// by. The pending carry is consumed here, exactly as a layer step's run
// consumes it.
func chainHopRun(st CorridorStateID, x float64, sps []SolvePiece, cands []*PreppedCandidate, pendingLo *float64, pendingCand *int, pendingDir CorridorDir, pendingIB int) (CorridorRun, error) {
	sp := &sps[st.SpIdx]
	foot := canonMeasureAtPiece(sp.P, x)
	lo := foot
	if *pendingCand >= 0 && sp.CandIdx == *pendingCand {
		if err := checkCarryReceive(st.Dir, lo, sp.CandIdx, cands, pendingDir, pendingIB); err != nil {
			return CorridorRun{}, err
		}
		lo = *pendingLo
	}
	*pendingCand = -1
	return CorridorRun{State: st, CandIdx: sp.CandIdx, ImportMeasureLo: x, ImportMeasureHi: x, CanonMeasureLo: lo, CanonMeasureHi: foot, ChainHop: true}, nil
}

// dropEmptyChainHops drops a chain-hop run the chain paid nothing across.
// Such a run was reached and left by switches that bought no teleport, so its
// canon starts and ends at its own foot: no import extent and no canon extent.
// There is nothing to draw, and it is dropped here rather than handed
// downstream as a point-sized run. An ordinary run is never touched here,
// whatever its extent.
//
// ⚠ NEVER SEEN TO FIRE. Measured: 0 of 41 chain hops dropped across the
// randomised and real-track test corpora, plus an earlier full census: all
// 135 hops node to node. A hop can have no canon extent only if it is reached
// AND left by switches that paid for no teleport — two zero-cost
// same-candidate hand-offs at one layer, for instance — and the full-identity
// chain census found no such chain. It is kept because that shape is not
// ruled out by construction, and a point-sized run reaching the commit would
// mint a zero-length row. [TestDropEmptyChainHops] pins what it does.
func dropEmptyChainHops(runs []CorridorRun) []CorridorRun {
	kept := runs[:0]
	for _, r := range runs {
		if r.ChainHop && r.CanonMeasureLo == r.CanonMeasureHi {
			continue
		}
		kept = append(kept, r)
	}
	return kept
}

// mergeCorridorRuns merges adjacent runs that are ONE stretch of the same
// trail — see the rule's full statement at its call site in
// [SolveCorridorPath].
func mergeCorridorRuns(runs []CorridorRun) []CorridorRun {
	merged := runs[:0:0]
	for _, r := range runs {
		if n := len(merged); n > 0 {
			last := &merged[n-1]
			lastDegen := last.CanonMeasureLo == last.CanonMeasureHi
			rDegen := r.CanonMeasureLo == r.CanonMeasureHi
			lastRev := last.CanonMeasureHi < last.CanonMeasureLo
			rRev := r.CanonMeasureHi < r.CanonMeasureLo
			// A DEGENERATE RUN HAS NO DIRECTION — see the same rule, and the
			// measurement behind it, at [mergeAdjacentSameCandidateRuns].
			// Direction here is DERIVED from the measure pair rather than stored,
			// so a merged stride keeps its own CanonMeasureLo and needs no flag
			// repair; only the comparison has to stop reading a zero-width
			// span as "forward".
			sameDir := lastRev == rRev || lastDegen || rDegen
			strideRev := lastRev
			if lastDegen {
				strideRev = rRev
			}
			continues := false
			if strideRev {
				continues = r.CanonMeasureLo <= last.CanonMeasureHi
			} else {
				continues = r.CanonMeasureLo >= last.CanonMeasureHi
			}
			if !last.State.Off && !r.State.Off &&
				!last.Bridged && !r.Bridged &&
				last.CandIdx == r.CandIdx &&
				sameDir &&
				last.ImportMeasureHi == r.ImportMeasureLo &&
				continues && !(lastDegen && rDegen) {
				last.ImportMeasureHi = r.ImportMeasureHi
				last.CanonMeasureHi = r.CanonMeasureHi
				// A merged run that absorbed a highlight hop KEEPS the flag. The
				// flag is what stops the wire layer dropping a zero-width run
				// as a sub-eps sliver ([CorridorFollowRuns]); merged with a
				// sub-eps ordinary run it is still sub-eps, and clearing the
				// flag there would drop the canon the highlight paid for, silently.
				last.ChainHop = last.ChainHop || r.ChainHop
				continue
			}
		}
		merged = append(merged, r)
	}
	return merged
}

// checkCarrySend is the sending half of the carry-through: the run being
// extended to the node a paid teleport went through must be the run of the
// state the switch left from, in the direction the switch was priced for, and
// the node must be the end that direction walks toward.
//
// ⚠ AN ERROR, NOT A SKIP — AND A STRUCTURAL ONE. The pairing is
// direction-consistent by construction: ia is the vertex the declared Dir
// walks TOWARD, and its end measure is 0 for REV or total for FWD, so asking
// the question of the INDEX needs no tolerance. The float form of this test
// (an earlier version) could only refuse on a measure interpolation had
// pushed sub-nanometre outside the arc, and it aborted the whole write for it.
//
// ⚠ AND ONE THAT CONSTRUCTION MAKES UNREACHABLE, since chain hops are
// drawn. The carryEndIsExtreme clause cannot be false:
// [crossCandidateSharedPair] derives ia from the same Dir passed here. The Dir
// clause COULD be false before: `last` was the run whose state reached this
// layer, and a chain of same-layer switches could leave the leg on this
// candidate in the other direction — the lollipop (a stub ridden FWD, a loop
// at its far end used as a U-turn, the stub REV, then the next trail), which
// aborted the solve here and under the float test before it. Every
// intermediate state of a chain is now drawn as its own run ([chainHopRun]),
// so `last` IS the state the switch leaves from. It stays an error because a
// failure would mean the run being extended is not the one the teleport
// bought, and silently skipping would leave the leg drawing less canon than it
// was charged for.
func checkCarrySend(last *CorridorRun, runIdx int, fromDir CorridorDir, ia int, x float64, cands []*PreppedCandidate) error {
	if last.State.Dir != fromDir || !carryEndIsExtreme(fromDir, ia, len(cands[last.CandIdx].Verts)) {
		return fmt.Errorf("corridorsolve: carry-through at import measure %v pairs run %d (declared direction %v, canon end %v) with vertex %d of %d on candidate %d, priced for direction %v — internal inconsistency",
			x, runIdx, last.State.Dir, last.CanonMeasureHi, ia, len(cands[last.CandIdx].Verts), last.CandIdx, fromDir)
	}
	return nil
}

// checkCarryReceive is the receiving half of the carry-through: a run that
// STARTS where a paid teleport landed begins at the node, so its declared Dir
// must be the one the switch was priced for, and the node must be the extreme
// of the OPPOSITE direction ([carryEndIsExtreme] asked with the Dir inverted).
//
// ⚠ AN ERROR THAT CONSTRUCTION MAKES UNREACHABLE. The pending carry is set by
// the switch INTO the state that receives it, from that state's own Dir and
// [crossCandidateSharedPair]'s own IB for it, and it is consumed by the very
// next run appended, which is that state's (a chain's intermediate states are
// drawn — [chainHopRun] — so no other run can come between). It stays an error
// because a failure would mean the run is not the one the teleport bought, and
// drawing it anyway would put canon where the solve did not pay for it.
func checkCarryReceive(stDir CorridorDir, canonLo float64, candIdx int, cands []*PreppedCandidate, pendingDir CorridorDir, pendingIB int) error {
	if stDir != pendingDir || !carryEndIsExtreme(!pendingDir, pendingIB, len(cands[candIdx].Verts)) {
		return fmt.Errorf("corridorsolve: carry-through pairs a run (declared direction %v, canon start %v) with vertex %d of %d on candidate %d, priced for direction %v — internal inconsistency",
			stDir, canonLo, pendingIB, len(cands[candIdx].Verts), candIdx, pendingDir)
	}
	return nil
}

// corridorFinalPick is the cheapest reached state at the final layer: states
// and dist are that layer's list and distances. Unreached states (dist +Inf)
// are skipped, and found is false when none was reached.
//
// ⚠ AN EXACT TOTAL ORDER: cost, then [stateLess] only on an exact tie. It used to treat costs within 1e-9 as tied, which let the
// tiebreak hand the leg to a dearer state; two costs that differ by any amount
// are two different answers, and the cheaper one wins.
func corridorFinalPick(states []CorridorPackedState, dist []float64) (best CorridorStateID, cost float64, found bool) {
	cost = math.Inf(1)
	for slot, ps := range states {
		d := dist[slot]
		if math.IsInf(d, 1) {
			continue
		}
		s := ps.Unpack()
		if !found || d < cost || (d == cost && stateLess(s, best)) {
			best, cost, found = s, d, true
		}
	}
	return best, cost, found
}

// stateLess is the deterministic tiebreak order: OFF first, then ascending
// IvIdx (== ascending candidate id, since ivs is built from an id-sorted
// candidate list, so byte-identical duplicate candidates are broken by
// id), then FWD before REV.
func stateLess(a, b CorridorStateID) bool {
	if a.Off != b.Off {
		return a.Off
	}
	if a.Off {
		return false
	}
	if a.SpIdx != b.SpIdx {
		return a.SpIdx < b.SpIdx
	}
	return a.Dir == CorridorFWD && b.Dir == CorridorREV
}

func candidateOfState(s CorridorStateID, sps []SolvePiece) int {
	if s.Off {
		return -1
	}
	return sps[s.SpIdx].CandIdx
}

// zzOptCheck — SCRATCH.
