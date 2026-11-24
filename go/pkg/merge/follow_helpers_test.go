package merge

import (
	"strings"
	"testing"
)

// Direct tests for the shared follow helpers that the live corridor path
// uses (prepCandidates, densifySource, checkCorridor). The import-level tests
// do not reach these edge branches, so these pin them directly.

func TestPrepCandidates_SkipsDegenerateCanon(t *testing.T) {
	canon := []CanonArc{
		mCanon(1, mPt(0, 0)),                          // one vertex: not a polyline
		mCanon(2, mPt(10, 0), mPt(10, 0)),             // zero length
		mCanon(3, mPt(0, 0), mPt(50, 0), mPt(100, 0)), // a real candidate
	}
	pcs := prepCandidates(canon)
	if len(pcs) != len(canon) {
		t.Fatalf("prepCandidates returned %d slots for %d arcs — the slice must stay index-aligned with canon", len(pcs), len(canon))
	}
	if pcs[0] != nil {
		t.Errorf("a one-vertex canon arc was prepared as a candidate; want nil")
	}
	if pcs[1] != nil {
		t.Errorf("a zero-length canon arc was prepared as a candidate; want nil")
	}
	if pcs[2] == nil {
		t.Fatalf("a 100 m canon arc was not prepared as a candidate")
	}
}

func TestDensifySource_ExactMultipleAddsNoPointAtTheFarVertex(t *testing.T) {
	a, c := mPt(0, 0), mPt(40, 0)
	segLen := HaversineMetres(a.Lat, a.Lon, c.Lat, c.Lon)
	dpts, orig := densifySource([]Point{a, c}, segLen/4)
	// Four equal steps: three interior samples, then the far vertex itself —
	// never a fourth sample sitting on (or a rounding error short of) it.
	want := []bool{true, false, false, false, true}
	if len(orig) != len(want) || len(dpts) != len(want) {
		t.Fatalf("densifySource gave %d points (orig %v), want %d (orig %v)", len(dpts), orig, len(want), want)
	}
	for i := range want {
		if orig[i] != want[i] {
			t.Fatalf("orig = %v, want %v", orig, want)
		}
	}
	if dpts[4] != c {
		t.Fatalf("last point = %+v, want the far vertex %+v verbatim", dpts[4], c)
	}
}

func TestCheckCorridor_RejectsCanonThatLeavesTheSource(t *testing.T) {
	// The source runs straight along y=0 and every source point lies ON canon,
	// so only the canon→source direction can see the 40 m spike.
	src := straightLegX(21, 100, 0)
	pc := prepCandidates([]CanonArc{
		mCanon(1, mPt(0, 0), mPt(50, 0), mPt(50, 40), mPt(50.1, 0), mPt(100, 0)),
	})[0]
	err := checkCorridor(src, 0, pc, 0, pc.Total, nil, nil, false, defaultFollowBands().StayM)
	if err == nil {
		t.Fatal("checkCorridor accepted canon with a 40 m spike away from the source")
	}
	if !strings.Contains(err.Error(), "committed point") {
		t.Fatalf("checkCorridor failed for the wrong reason: %v — want the committed-point (canon→source) check", err)
	}
}

func TestCheckCorridor_ExitCrossingExtendsTheSourcePath(t *testing.T) {
	// The source stops at x=80; the run's exit crossing is at x=100 where canon
	// ends. Without the crossing appended to the source path, canon's last
	// 20 m would be measured against a source that stops short (20 m > Stay).
	src := straightLegX(17, 80, 0)
	pc := prepCandidates([]CanonArc{mCanon(1, mPt(0, 0), mPt(100, 0))})[0]
	exit := mPt(100, 0)
	if err := checkCorridor(src, 0, pc, 0, pc.Total, nil, &exit, false, defaultFollowBands().StayM); err != nil {
		t.Fatalf("checkCorridor with the exit crossing: %v", err)
	}
	if err := checkCorridor(src, 0, pc, 0, pc.Total, nil, nil, false, defaultFollowBands().StayM); err == nil {
		t.Fatal("checkCorridor without the exit crossing accepted 20 m of canon past the source's end — the fixture does not separate the two cases")
	}
}
