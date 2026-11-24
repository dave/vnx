package merge

import (
	"math"
	"testing"
)

// lollipopChainFixture is the smallest case found by a directed search
// (28 of 5,000 random cases aborted the import before the fix). A stub A
// runs from M to N; a loop at N is stored as two arcs C1 (N->P) and C2 (P->N),
// since an arc may not start and end on one node; B leaves M round a
// semicircle south of A and runs east past N. The source zigzags densely
// along A — so riding A is far cheaper than anything else there — then heads
// straight to N and east along B.
//
// At N the cheapest move onto B is not the flat P switch but a same-layer
// chain costing about 38 m: A FWD to N, round the loop (C1, C2) as a U-turn,
// back down A REV to M, and onto B through M.
//
// The numbers are the search's own float64 draws, kept exact because the
// chain's price clears P by little enough that a rounded fixture might not
// take it.
type lollipopChainFixture struct {
	A, C1, C2, B, src []Point
}

func newLollipopChainFixture() lollipopChainFixture {
	const (
		rad   = 7.280389823435656
		cs    = 0.7251831025050954
		c1    = 0.9898974959313127
		c2    = 0.9177624351413868
		c3    = 1.4804090118927031
		sq    = 0.9246319468430607
		b1    = 0.026276120367301047
		b2    = -0.881482989101934
		b3    = -1.0126052987354524
		zc    = 8.261707365199348
		zw    = 6.583028552779551
		pitch = 0.01958019205564271
		amp   = 0.23962368951722557
	)
	la := 2 * rad
	M, N := mPt(0, 0), mPt(la, 0)
	P := mPt(la-cs, cs*c1)
	var f lollipopChainFixture
	f.A = []Point{M, N}
	f.C1 = []Point{N, P}
	f.C2 = []Point{P, mPt(la-cs*c2, cs*c3), N}
	f.B = []Point{M}
	for i := 1; i < 24; i++ {
		th := math.Pi * float64(i) / 24
		f.B = append(f.B, mPt(rad-rad*math.Cos(th), -rad*sq*math.Sin(th)))
	}
	f.B = append(f.B, mPt(la+b1, b2), mPt(80, b3))
	sgn := 1.0
	for x := zc - zw/2; x <= zc+zw/2; x += pitch {
		f.src = append(f.src, mPt(x, sgn*amp))
		sgn = -sgn
	}
	f.src = append(f.src, N, mPt(80, 0))
	return f
}

// TestCorridorHighlight_LollipopSolveDrawsThePaidWalk: the solve's runs are the
// walk the chain paid for, hop by hop. Candidates are in the order the import
// sees them (arc id order: B, C1, C2, A).
func TestCorridorHighlight_LollipopSolveDrawsThePaidWalk(t *testing.T) {
	f := newLollipopChainFixture()
	cands := PrepCandidates([]CanonArc{mCanon(1, f.B...), mCanon(2, f.C1...), mCanon(3, f.C2...), mCanon(4, f.A...)})
	const iB, iC1, iC2, iA = 0, 1, 2, 3
	sa, err := NewSourceMeasure(f.src)
	if err != nil {
		t.Fatal(err)
	}
	res, err := SolveCorridorPath(t.Context(), sa.Total(), sa, buildIvs(t, sa, cands, CorridorRadiusM), cands, CorridorRadiusM, EmissionK)
	if err != nil {
		t.Fatalf("solveCorridorPath: %v", err)
	}
	// Find the chain: the drawn hop across C1.
	h := -1
	for i, r := range res.Runs {
		if r.ChainHop && r.CandIdx == iC1 {
			h = i
			break
		}
	}
	if h < 1 || h+3 >= len(res.Runs) {
		t.Fatalf("no drawn chain across the loop in the runs — the fixture no longer takes the U-turn, so it tests nothing: %+v", res.Runs)
	}
	x := res.Runs[h].ImportMeasureLo
	want := []struct {
		cand     int
		dir      CorridorDir
		hop      bool
		lo, hi   float64 // canon; NaN = not checked
		atImport bool    // ImportMeasureLo == Hi == x
	}{
		{iA, CorridorFWD, false, math.NaN(), cands[iA].Total, false},
		{iC1, CorridorFWD, true, 0, cands[iC1].Total, true},
		{iC2, CorridorFWD, true, 0, cands[iC2].Total, true},
		{iA, CorridorREV, true, cands[iA].Total, 0, true},
		{iB, CorridorFWD, false, 0, math.NaN(), false},
	}
	for k, w := range want {
		r := res.Runs[h-1+k]
		if r.CandIdx != w.cand || r.State.Dir != w.dir || r.ChainHop != w.hop {
			t.Errorf("run %d is candidate %d %v hop=%v, want candidate %d %v hop=%v", h-1+k, r.CandIdx, r.State.Dir, r.ChainHop, w.cand, w.dir, w.hop)
			continue
		}
		if !math.IsNaN(w.lo) && r.CanonMeasureLo != w.lo {
			t.Errorf("run %d (candidate %d) starts at canon %v, want %v", h-1+k, w.cand, r.CanonMeasureLo, w.lo)
		}
		if !math.IsNaN(w.hi) && r.CanonMeasureHi != w.hi {
			t.Errorf("run %d (candidate %d) ends at canon %v, want %v", h-1+k, w.cand, r.CanonMeasureHi, w.hi)
		}
		if w.atImport && (r.ImportMeasureLo != x || r.ImportMeasureHi != x) {
			t.Errorf("run %d (candidate %d) spans import [%v,%v], want the chain's one position %v", h-1+k, w.cand, r.ImportMeasureLo, r.ImportMeasureHi, x)
		}
	}
	if res.Runs[h-1].ImportMeasureHi != x || res.Runs[h+3].ImportMeasureLo != x {
		t.Errorf("the runs either side of the chain do not meet it at import %v: [%v, %v]", x, res.Runs[h-1].ImportMeasureHi, res.Runs[h+3].ImportMeasureLo)
	}
}
