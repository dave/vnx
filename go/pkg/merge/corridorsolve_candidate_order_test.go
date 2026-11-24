package merge

// Candidate-order independence — a discriminating instrument that was
// never built while the hand-off repairs below were tried.
//
// The three failed repairs to the free node hand-off all reconciled several
// candidates' independently-solved records of one node, and the winning
// record was decided by WALK ORDER: `unifySharedVertexBoundaries` took the
// first writer, and the walk was interval order, which is candidate order. A
// displacement of up to 13.36 m was therefore applied to whichever candidate
// happened to come second, and a
// shift statistic could not adjudicate the rules because every rule shifts.
//
// The continuous price ([crossCandidateSwitchCost]) has no such record. It is
// a pure function of (piece a, piece b, x), so the same candidates in a
// different order must produce the same solve, exactly. That is a property,
// not a number, and this is the test of it.
//
// ⚠ THIS FILE USES ONLY PRE-EXISTING API ON PURPOSE. It names none of the
// new symbols, so it compiles and runs unchanged against the arm before this
// change — which is how it was red-proofed rather than merely asserted.

import (
	"fmt"
	"testing"
)

// candOrderKey identifies a candidate by its own geometry rather than by its
// index, which is exactly what the permutation moves. prepOne gives every
// fixture candidate the same arc id, so the endpoints are the identity.
func candOrderKey(pc *PreppedCandidate) string {
	f := pc.Verts[0]
	l := pc.Verts[len(pc.Verts)-1]
	return fmt.Sprintf("%.12f,%.12f->%.12f,%.12f", f.Lat, f.Lon, l.Lat, l.Lon)
}

// solveSignature renders a solve result in terms that survive a permutation:
// each run's candidate named by geometry, plus its import and canon measures.
func solveSignature(res *corridorSolveResult, cands []*PreppedCandidate) []string {
	out := make([]string, 0, len(res.Runs))
	for _, r := range res.Runs {
		who := "OFF"
		if !r.State.Off {
			who = candOrderKey(cands[r.CandIdx])
		}
		out = append(out, fmt.Sprintf("%s import[%.9f,%.9f] canon[%.9f,%.9f] bridged=%v",
			who, r.ImportMeasureLo, r.ImportMeasureHi, r.CanonMeasureLo, r.CanonMeasureHi, r.Bridged))
	}
	return out
}

// TestCorridorSolve_CandidateOrderIndependent runs one leg against every
// permutation of its candidate list and requires the winning path to be
// identical every time.
//
// The fixture is a NEAR-PERPENDICULAR junction, which is the shape that broke
// the deleted mechanisms: two arcs meeting at V at genuinely different angles
// solve WHERE in import measure the walk reaches V to answers metres apart
// (19.98 m was measured on this construction), so a first-writer-wins reconciliation moves whichever arc
// came second. A third candidate that shares no node with either competes for
// the same stretch, so there is a real choice for the order to disturb.
func TestCorridorSolve_CandidateOrderIndependent(t *testing.T) {
	const R = CorridorRadiusM
	v := mPt(100, 5)
	build := func() []*PreppedCandidate {
		return []*PreppedCandidate{
			prepOne(t, mPt(0, 8), v),
			prepOne(t, v, mPt(200, -8)),
			prepOne(t, mPt(0, -6), mPt(200, -6)),
		}
	}
	base := build()
	sa, err := newSourceMeasure(straightLegX(41, 200, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}

	perms := [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	var want []string
	for _, perm := range perms {
		cands := make([]*PreppedCandidate, len(perm))
		for i, p := range perm {
			cands[i] = base[p]
		}
		ivs := buildIvs(t, sa, cands, R)
		res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, emissionK)
		if err != nil {
			t.Fatalf("perm %v: solveCorridorPath: %v", perm, err)
		}
		got := solveSignature(res, cands)
		if want == nil {
			want = got
			// ⚠ LIVENESS. An all-OFF or single-run solve is invariant under
			// permutation for reasons that have nothing to do with the price,
			// so it would pass this test while measuring nothing.
			on := 0
			for _, r := range res.Runs {
				if !r.State.Off {
					on++
				}
			}
			if on < 2 {
				t.Fatalf("the reference solve has %d ON runs: with fewer than two the permutation "+
					"cannot disturb anything and this test measures nothing. Runs: %+v", on, res.Runs)
			}
			t.Logf("reference solve, %d runs:", len(got))
			for _, l := range got {
				t.Logf("  %s", l)
			}
			continue
		}
		if len(got) != len(want) {
			t.Fatalf("perm %v produced %d runs, the reference produced %d — the winning path depends "+
				"on candidate order.\n got: %v\nwant: %v", perm, len(got), len(want), got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("perm %v run %d = %s, reference = %s — the winning path depends on candidate "+
					"order, so some part of the price is decided by walk order rather than by geometry",
					perm, i, got[i], want[i])
			}
		}
	}
}

// TestCorridorSolve_CandidateOrderIndependent_Junction is the same property on
// a junction of THREE arcs meeting at one node, the shape of a real
// junction where: three candidates each solved WHERE the walk reaches the node in
// their own frame and got three answers 2.9e-10 m apart, and only the pairing
// that happened to share a solver layer could hand off.
//
// ⚠ THIS ARM IS NOT RED-PROOFED AND IS RECORDED AS SUCH. Run against
// the code before the fix it PASSES, so on this fixture the deleted
// first-writer-wins reconciliation happened not to change the winning path.
// Only the near-perpendicular arm above actually goes red there (a 0.4995 m
// run-boundary shift under permutation [1 0 2]). This arm is kept as a guard
// on the property for the junction shape, not as evidence the property was
// ever broken there — a red proof is a moment unless it is frozen,
// a distinction stated rather than left for the next reader to discover.
func TestCorridorSolve_CandidateOrderIndependent_Junction(t *testing.T) {
	const R = CorridorRadiusM
	v := mPt(100, 2)
	base := []*PreppedCandidate{
		prepOne(t, mPt(0, 3), v),
		prepOne(t, v, mPt(200, 4)),
		prepOne(t, v, mPt(150, 60)), // the third arm at the same node, off the source
	}
	sa, err := newSourceMeasure(straightLegX(41, 200, 0))
	if err != nil {
		t.Fatalf("newSourceMeasure: %v", err)
	}
	perms := [][]int{{0, 1, 2}, {2, 1, 0}, {1, 2, 0}, {2, 0, 1}, {1, 0, 2}, {0, 2, 1}}
	var want []string
	for _, perm := range perms {
		cands := make([]*PreppedCandidate, len(perm))
		for i, p := range perm {
			cands[i] = base[p]
		}
		ivs := buildIvs(t, sa, cands, R)
		res, err := solveCorridorPath(t.Context(), sa.Total(), sa, ivs, cands, R, emissionK)
		if err != nil {
			t.Fatalf("perm %v: solveCorridorPath: %v", perm, err)
		}
		got := solveSignature(res, cands)
		if want == nil {
			want = got
			on := 0
			for _, r := range res.Runs {
				if !r.State.Off {
					on++
				}
			}
			if on < 2 {
				t.Fatalf("the reference solve has %d ON runs, so the permutation cannot disturb "+
					"anything: %+v", on, res.Runs)
			}
			continue
		}
		for i := range got {
			if i >= len(want) || got[i] != want[i] {
				t.Fatalf("perm %v differs at run %d.\n got: %v\nwant: %v", perm, i, got, want)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("perm %v produced %d runs, reference %d", perm, len(got), len(want))
		}
	}
}
