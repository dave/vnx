package merge

// Within ONE candidate, a source edge mints a CorridorPiece against every
// canon edge within the corridor radius. At ~1 m sampling on both sides with
// R = 15 m that is roughly 35 pieces, of which one carries the true
// perpendicular foot. This drops the canon edges that are never the nearest.
//
// ⚠ IT NEVER COMPETES TWO CANDIDATES. A different candidate is a different
// PreppedCandidate and a different call, so a braid, a parallel path, or a
// trail on the other side of the valley keeps every piece it had. The prune
// is per contiguous canon-edge RUN, so a candidate that comes back on itself
// (a hairpin, a lollipop stem) keeps each approach's own nearest — the
// self-approach case CorridorInterval's header says Pieces exists to
// represent rather than collapse.
//
// WHY IT IS NEEDED, measured on one real 8-file import of a canyon trail:
//
//	no prune           9m55s
//	halo 8 (this)         45s
//	halo 2                13s
//
// nearestRunHaloEdges was FITTED, not derived: 8 was chosen by trying values
// against our own test data. Nothing proves it is enough, and no test fails
// when it is too small. It was accepted because a ten-minute import of a real
// area is a user's cost, to be revisited later.
//
// ⚠ WHY A SMALLER HALO IS NOT SAFE, AND WHY THE OBVIOUS ARGUMENT FOR ONE IS
// WRONG. The tempting justification is "a piece too far away to be worth
// travelling along cannot be in the answer". It is false. A piece is also
// usable as a STEPPING STONE — the path switches onto it and straight off
// again within one layer, paying a flat P each way and NOTHING for the
// piece's own distance (SolveCorridorPath's switch-edge block). So no bound
// on travel cost can license dropping it. That is not theory: an earlier
// bug was exactly a path hopping to a third candidate and back for 2P rather
// than making a same-candidate arm-jump of hundreds of metres.
//
// Measured, on the winning path only: stepping stones are 0.02%-0.04% of the
// alphabet (14 and 20 pieces on two test corpora), while 85%-97% of pieces
// are never touched at all. So the waste is real and enormous, and the
// reason it cannot be pruned soundly is a rare case rather than a common
// one — which is precisely why a tuned halo appears to work and cannot be
// trusted to keep working.
//
// THE EXIT. The right fix is not a better halo, it is not creating the
// redundancy: one state per (candidate, direction) instead of one per
// (source edge, canon edge, regime). That deletes this file. Until then,
// changing nearestRunHaloEdges REQUIRES a byte-identity comparison, not the
// standard suite: only a byte comparison sees a halo regression. A halo change is
// judged by comparing the committed output bytes before and after on real
// corpora. A large-corpus re-import check catches only a hard failure, not a
// byte change.

import (
	"fmt"
	"math"
)

// nearestRunHaloEdges is how many canon edges either side of a winner are
// kept. The value was fitted to our test data, not derived; see this file's
// header. Only a byte comparison of the output on real corpora sees a
// regression when it changes; no test does.
const nearestRunHaloEdges = 8

// NearestRunSamples is how many points in the source edge's own [0,1] the
// envelope is sampled at.
//
// The question "which canon edges are ever nearest" has an exact answer —
// the lower envelope of the pieces' dist²(t) — and this approximates it.
// MEASURED NOT TO BE THE BINDING CONSTRAINT: raising this to 4096 while
// holding the halo at 4 left a failing test red, and lowering it to 64 while
// raising the halo to 8 turned it green. The exact envelope would therefore
// not make a smaller halo safe, which is why it was not built.
const NearestRunSamples = 64

// PruneToNearestCanonEdges drops the pieces of one (source edge × candidate)
// whose canon edge is never the nearest within its own contiguous run.
//
// pieces must arrive in ascending canon-edge order, which is
// [sourceEdgeCorridorPieces]' own guarantee. Retained pieces are returned
// UNCHANGED — not clipped, not recomputed — so this can only ever remove
// state, never perturb a number.
func PruneToNearestCanonEdges(pieces []CorridorPiece) ([]CorridorPiece, error) {
	if len(pieces) == 0 {
		return pieces, nil
	}
	keep := make(map[int]bool, 8)
	for i := 0; i < len(pieces); {
		j := i
		for j+1 < len(pieces) {
			d := pieces[j+1].CanonEdge - pieces[j].CanonEdge
			if d != 0 && d != 1 {
				break // a gap in canon-edge index: a separate approach
			}
			j++
		}
		run := pieces[i : j+1]
		loEdge, hiEdge := run[0].CanonEdge, run[len(run)-1].CanonEdge
		winners := 0
		// markNearestAt finds which canon edge is nearest at t and keeps it
		// with its halo. Reports whether any piece of the run was active at
		// all — t outside every regime window is an ordinary expected
		// absence per sample, and only its AGGREGATE over the run matters.
		markNearestAt := func(t float64) {
			best, bestEdge := math.Inf(1), -1
			for k := range run {
				p := &run[k]
				if t < p.Regime.Lo || t > p.Regime.Hi {
					continue
				}
				if v := p.Regime.DistSq(t); v < best {
					best, bestEdge = v, p.CanonEdge
				}
			}
			if bestEdge < 0 {
				return
			}
			winners++
			for e := bestEdge - nearestRunHaloEdges; e <= bestEdge+nearestRunHaloEdges; e++ {
				if e >= loEdge && e <= hiEdge {
					keep[e] = true
				}
			}
		}
		for s := 0; s <= NearestRunSamples; s++ {
			markNearestAt(float64(s) / float64(NearestRunSamples))
		}
		// ⚠ EVERY PIECE IS EVALUATED AT ITS OWN MIDPOINT, and this is not an
		// accuracy refinement — it is what stops the prune DELETING A WHOLE
		// RUN. The uniform grid above only ever marks an edge that wins at
		// one of its points, so a run whose regime windows all fall BETWEEN
		// samples marks nothing, and every piece of it is then dropped
		// below. That is categorically different from choosing among ~35
		// redundant competitors: it removes the only correspondence a
		// self-approach has.
		//
		// The trigger is a window narrower than 1/NearestRunSamples in t. On
		// a dense GPS track (~1 m edges) that is a sub-centimetre graze at
		// the corridor rim; on a sparse PLAN edge of several hundred metres
		// it is METRES of real correspondence. Found in review after a corpus
		// run passed — the corpora do not happen to contain one.
		//
		// A piece is active at its own midpoint by construction (lo <= mid
		// <= hi, including the degenerate lo == hi), so this makes
		// winners == 0 unreachable rather than merely unlikely.
		for k := range run {
			markNearestAt((run[k].Regime.Lo + run[k].Regime.Hi) / 2)
		}
		if winners == 0 {
			return nil, fmt.Errorf(
				"corridornearest: prune found no nearest canon edge anywhere in a run of %d piece(s) over canon edges %d..%d — "+
					"every piece is active at its own midpoint by construction, so this is unreachable",
				len(run), loEdge, hiEdge)
		}
		i = j + 1
	}
	// ⚠ A NEW SLICE, NOT pieces[:0]. Compacting in place returns the pruned
	// length with the UN-pruned capacity, and the caller stores the result in
	// bySrcEdge, so every rejected piece stayed reachable until
	// joinCorridorPieces returned — 281.3 MB held for 24.4 MB wanted on one
	// measured leg. The over-capacity is not
	// small: sourceEdgeCorridorPieces builds its own slice by append, so the
	// arriving capacity is up to ~2× minted.
	//
	// slices.Clip would NOT fix this: it lowers cap but keeps the same backing
	// array, so nothing is released.
	//
	// The count pass is what makes the allocation exact. The PIECES THEMSELVES
	// ARE COPIED VERBATIM — this function's guarantee (see the doc comment
	// above) is that retained pieces come back unchanged, which is what makes
	// the whole change byte-identical.
	kept := 0
	for _, p := range pieces {
		if keep[p.CanonEdge] {
			kept++
		}
	}
	out := make([]CorridorPiece, 0, kept)
	for _, p := range pieces {
		if keep[p.CanonEdge] {
			out = append(out, p)
		}
	}
	if NearestPruneObserverForTests != nil {
		NearestPruneObserverForTests(len(pieces), len(out))
	}
	return out, nil
}
