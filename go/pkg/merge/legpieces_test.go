package merge

// legpieces_test.go — the leg cut's own acceptance. See legpieces_core.go.
//
// The align step put nodes into canon at each followed run's boundaries and
// stopped; the leg cut cuts the LEG at the same boundaries and terminates each
// piece ON the node the align step left there. The tests here are the record that it did, and each one names the
// cripple that reddens it.
//
// ⚠ EVERY ASSERTION HERE IS ON A NODE ID, NEVER ON A COORDINATE. A follower that
// minted a FRESH node at exactly the right place is geometrically
// indistinguishable from one that adopted canon's — same polyline, same
// network hash, same km — and whole-network accounting provably cannot see
// the difference. Only identity can.

import (
	"context"
	"testing"
)

// TestA2_TwoSeamsOnOneNodeAtAdjacentPointsAreOneSeam is the same-node-split degenerate that
// the same-node split's own remedy cannot reach, and it aborted real imports before this.
//
// The align step resolves each ON run's boundary to a canon node independently. Two of them
// can land on the SAME node: the leg touches junction J at the end of one run and
// again at the start of the next, because the comb dropped out of the band for a
// single point at the junction. When those two points are ADJACENT, the piece
// between them is two points long and both of its ends are J.
//
// The same-node split cuts a same-node piece at an interior vertex, and a
// two-point piece has none — so [furthestVertex] errors, correctly, and the whole
// import fails. Measured on a synthetic-trail sweep before this fix: 6 of 150 seeds,
// e.g. seed 113 "leg piece 74 of 85 (points 440..441, start canon node 11 at
// point 440, end canon node 11 at point 441)".
//
// ⚠ THE FIX IS NOT A SPLIT, IT IS A COLLAPSE, and the distinction is the whole
// point. A same-node piece WITH interior vertices is a real ring — the leg left
// J, went somewhere, and came back — and the same-node split commits it as two arcs because
// that geometry exists and must be stored. A same-node piece with NO interior
// vertex holds no geometry at all: both its ends adopt J's coordinate (the node-adoption rule), so
// it is a zero-length row. It is not a ring, it is ONE seam claimed twice, and
// the two claims are merged. Nothing is invented and nothing is dropped: the
// point that loses its claim stays in the leg as an ordinary interior vertex.
//
// The surviving claim is the EARLIER point. The two are interchangeable — under
// the node-adoption rule whichever keeps the claim is overwritten with J's coordinate and the other
// keeps its own, so either choice moves exactly one vertex, and both are inside
// the adoption the design already ratifies. Ascending order breaks the tie so the
// walk reaches J where it first arrives.
//
// CRIPPLE: remove the collapse and this test goes red with "no interior vertex to
// split at" — the production abort, at unit scale.
func TestA2_TwoSeamsOnOneNodeAtAdjacentPointsAreOneSeam(t *testing.T) {
	const j = NodeID(11)

	// Six points on a straight 500 m line. Runs 0 and 1 are ON; the single point
	// between them (index 3) is the one-point OFF excursion at the junction.
	asm := AssembledLeg{
		Points: []Point{mPt(0, 0), mPt(100, 0), mPt(200, 0), mPt(300, 0), mPt(400, 0), mPt(500, 0)},
		OnPieces: []OnPieceRange{
			{Run: 0, Lo: 0, Hi: 2, TerminalStart: true},
			{Run: 1, Lo: 3, Hi: 5, TerminalEnd: true},
		},
	}
	rep := AlignReport{Boundaries: []AlignBoundary{
		{Run: 0, AtRunEnd: true, Node: j, Decline: AlignCutMade},
		{Run: 1, AtRunEnd: false, Node: j, Decline: AlignDeclineAtNode},
	}}

	seams, err := legSeamsFromAlign(context.Background(), asm, rep, nil, nil)
	if err != nil {
		t.Fatalf("legSeamsFromAlign: %v", err)
	}
	if len(seams) != 1 {
		t.Fatalf("two boundaries on node %d at adjacent points 2 and 3 produced %d seam(s) (%+v), want 1 — "+
			"the piece between them would be a two-point row whose ends are both node %d, which storage forbids "+
			"and the same-node split cannot split", j, len(seams), seams, j)
	}
	if seams[0].Idx != 2 || !seams[0].HasNode || seams[0].Node != j {
		t.Errorf("the merged seam is %+v, want point 2 on canon node %d — the earlier claim survives", seams[0], j)
	}

	// LIVENESS. The collapse must be about ADJACENCY, not about the node id: two
	// claims on one node with a vertex between them are a real ring, and the
	// same-node split commits that as two arcs rather than merging it away.
	asmRing := asm
	asmRing.OnPieces = []OnPieceRange{
		{Run: 0, Lo: 0, Hi: 2, TerminalStart: true},
		{Run: 1, Lo: 4, Hi: 5, TerminalEnd: true},
	}
	ringSeams, err := legSeamsFromAlign(context.Background(), asmRing, rep, nil, nil)
	if err != nil {
		t.Fatalf("legSeamsFromAlign (ring): %v", err)
	}
	if len(ringSeams) != 2 {
		t.Fatalf("two boundaries on node %d at points 2 and 4 produced %d seam(s), want 2 — "+
			"the piece between them has an interior vertex, so it is a ring with real geometry and the same-node split splits it",
			j, len(ringSeams))
	}
}

// TestA2_SeamConflictPicksTheCloserCandidate is the Prove-It for the seam
// conflict rule (a synthetic-trail residual, "seed-1"): two boundaries of the SAME leg, meeting at
// ONE leg-walk point, independently resolved to two DIFFERENT, both-genuinely-
// valid nearby nodes. The fix picks whichever is TRUE-CHORD-CLOSER to the
// leg's own raw point — not whichever boundary happened to be recorded
// first, and not the along-canon AbsorbedM (a proxy the fix deliberately
// does not use — see the branch's own comment in legpieces_core.go).
//
// CRIPPLE: revert to picking the FIRST-recorded boundary regardless of
// distance (or compare AbsorbedM instead of the true chord) and this test
// goes red on the reversed-order case below, silently — the wrong candidate
// wins and the only symptom in a real import is a drifted network hash,
// which is exactly why this needs a name, not just a passing suite.
func TestA2_SeamConflictPicksTheCloserCandidate(t *testing.T) {
	const (
		near = NodeID(20) // 2 m from the leg's own point
		far  = NodeID(21) // 14 m from the leg's own point
	)
	// Six points on a straight 500 m line, same shape as the adjacency test
	// above. Runs 0 and 1 meet at point 3 (300, 0) — an ON↔ON seam, so the
	// assembler dedups it to one idx, and legSeamsFromAlign sees two
	// boundary records both claiming it.
	pts := []Point{mPt(0, 0), mPt(100, 0), mPt(200, 0), mPt(300, 0), mPt(400, 0), mPt(500, 0)}
	asm := AssembledLeg{
		Points: pts,
		OnPieces: []OnPieceRange{
			{Run: 0, Lo: 0, Hi: 3, TerminalStart: true},
			{Run: 1, Lo: 3, Hi: 5, TerminalEnd: true},
		},
	}
	nearPt, farPt := mPt(300, 2), mPt(300, 14)

	runOnce := func(label string, run0Node, run1Node NodeID, run0Pt, run1Pt Point, want NodeID) {
		t.Helper()
		rep := AlignReport{Boundaries: []AlignBoundary{
			{Run: 0, AtRunEnd: true, Node: run0Node, Decline: AlignDeclineAbsorbedToNode,
				NodeLat: run0Pt.Lat, NodeLon: run0Pt.Lon, AbsorbedM: 999}, // deliberately WRONG AbsorbedM — proves the fix reads NodeLat/NodeLon, not this field
			{Run: 1, AtRunEnd: false, Node: run1Node, Decline: AlignDeclineAbsorbedToNode,
				NodeLat: run1Pt.Lat, NodeLon: run1Pt.Lon, AbsorbedM: 999},
		}}
		seams, err := legSeamsFromAlign(context.Background(), asm, rep, nil, nil)
		if err != nil {
			t.Fatalf("%s: legSeamsFromAlign: %v", label, err)
		}
		if len(seams) != 1 {
			t.Fatalf("%s: %d seam(s), want 1 (the shared ON↔ON point)", label, len(seams))
		}
		if seams[0].Idx != 3 || !seams[0].HasNode {
			t.Fatalf("%s: seam is %+v, want point 3 with a node", label, seams[0])
		}
		if seams[0].Node != want {
			t.Errorf("%s: seam resolved to node %d, want %d (the true-chord-closer candidate) — AbsorbedM=999 on both sides was a deliberate decoy", label, seams[0].Node, want)
		}
	}

	// Node `near` (2 m) must win over `far` (14 m) regardless of which
	// boundary record it arrives on — proving this is genuinely about
	// DISTANCE, not "whichever is recorded first" or "whichever is second".
	runOnce("near recorded first", near, far, nearPt, farPt, near)
	runOnce("near recorded second", far, near, farPt, nearPt, near)
}

// TestA2_TwoSeamsOnOneNodeWithCoincidentInteriorAreOneSeam is the case the
// adjacency rule could not see, and it ABORTS A USER'S WHOLE IMPORT.
//
// Reproduced on a real recording: leg
// piece 56 of 64, points 1354..1356 — THREE BYTE-IDENTICAL POINTS, both ends
// resolving to canon node 163. The piece HAS an interior vertex, so the adjacency
// collapse leaves it alone and hands it to the same-node split as "a real ring";
// that then
// measures its measure, gets exactly 0, and errors — `a piece that begins and ends on
// the same node has measure length 0 — it cannot be split`. The import dies.
//
// ⚠ THE PREMISE THAT FAILED IS "AN INTERIOR VERTEX MEANS REAL GEOMETRY". It does
// not. Committed track geometry is full of duplicate consecutive vertices — a
// paused GPS logs the same fix twice, measured at 1,894 pairs across 152 of
// one real area's 1,478 arcs — and nothing in the pipeline dedupes them. A vertex
// that coincides with its neighbours adds a point and no extent.
//
// ⚠ AND IT IS NOT REALLY ABOUT THE ABSORB RADIUS, which is how it was found. A
// smaller radius happens to CUT where a wider one absorbs, so the radius decides
// whether a cut lands on such a run — not whether the degenerate piece is
// survivable. Any radius can hit it, including no absorb feature at all; the
// tested ones simply have not cut in that exact spot yet.
//
// The condition is therefore NO GEOMETRY, not adjacency: adjacency is the
// two-point case of it (the node-adoption rule makes both ends adopt J, so the row is zero-length),
// and coincident interior vertices are the rest.
//
// CRIPPLE: restore the `s.Idx-prev.Idx == 1` condition and this goes red with the
// production abort message.
func TestA2_TwoSeamsOnOneNodeWithCoincidentInteriorAreOneSeam(t *testing.T) {
	const j = NodeID(11)

	// Points 2, 3 and 4 are BYTE-IDENTICAL — the paused-GPS shape. Runs 0 and 1
	// bound the middle of that run, so the piece between the seams is points 2..4:
	// three points, one interior vertex, and zero measure.
	dup := mPt(200, 0)
	asm := AssembledLeg{
		Points:   []Point{mPt(0, 0), mPt(100, 0), dup, dup, dup, mPt(500, 0)},
		OnPieces: []OnPieceRange{{Run: 0, Lo: 0, Hi: 2, TerminalStart: true}, {Run: 1, Lo: 4, Hi: 5, TerminalEnd: true}},
	}
	rep := AlignReport{Boundaries: []AlignBoundary{
		{Run: 0, AtRunEnd: true, Node: j, Decline: AlignCutMade},
		{Run: 1, AtRunEnd: false, Node: j, Decline: AlignDeclineAtNode},
	}}

	seams, err := legSeamsFromAlign(context.Background(), asm, rep, nil, nil)
	if err != nil {
		t.Fatalf("legSeamsFromAlign: %v", err)
	}
	if len(seams) != 1 {
		t.Fatalf("two boundaries on node %d at points 2 and 4, with every point between them COINCIDENT, "+
			"produced %d seam(s) (%+v), want 1 — the piece between them has an interior vertex but no extent, "+
			"so the same-node split cannot split it and the import aborts", j, len(seams), seams)
	}
	if seams[0].Idx != 2 || !seams[0].HasNode || seams[0].Node != j {
		t.Errorf("the merged seam is %+v, want point 2 on canon node %d — the earlier claim survives", seams[0], j)
	}
}

// TestA2_TerminalSeamOnOneNodeWithCoincidentInteriorCollapses is the SAME defect
// as the interior case, in the TERMINAL collapse, and it aborts a real import.
//
// Reproduced on a real recording: leg piece 21 of 21, points 854..856,
// start canon node 15 at point 854, end THREADED node 15 — both ends one node,
// three points, zero extent. [collapseTerminalSameNodeSeams] requires the last
// seam to sit at `last-1`, so a THREE-point terminal piece (seam at last-2)
// slips past it, and the same-node split then errors on measure length 0. Its own header states the
// premise that fails, in the same words the interior one did: "ADJACENCY IS
// AGAIN THE CONDITION... Only the two-point piece, which holds no geometry at
// all, is collapsed."
//
// ⚠ IT WAS BEING MASKED. This abort does not fire while the any-node arm is
// enabled — that arm happens to move the boundary so the degenerate piece never
// forms. A feature that hides an abort is not a fix for it: turn the arm off, or
// change its radius, and the abort returns. So this is fixed on its own merits,
// exactly as the interior case was, and neither depends on what we decide about
// the snap.
//
// CRIPPLE: restore `end.Idx == last-1` and this goes red with the production
// abort message.
func TestA2_TerminalSeamOnOneNodeWithCoincidentInteriorCollapses(t *testing.T) {
	const j = NodeID(11)
	dup := mPt(400, 0)
	// Points 3, 4 and 5 are byte-identical; the leg's END terminal is node j and
	// the last seam also names j, at index 3 — last-2, not last-1.
	pts := []Point{mPt(0, 0), mPt(100, 0), mPt(200, 0), dup, dup, dup}
	last := len(pts) - 1
	end := j
	seams := []LegSeam{{Idx: 1, Node: NodeID(7), HasNode: true}, {Idx: 3, Node: j, HasNode: true}}

	got := collapseTerminalSameNodeSeams(seams, pts, last, nil, &end)
	if len(got) != 1 {
		t.Fatalf("a terminal piece of three COINCIDENT points, both ends node %d, produced %d seam(s) (%+v), want 1 — "+
			"the piece has an interior vertex but no extent, so the same-node split cannot split it and the import aborts", j, len(got), got)
	}

	// LIVENESS. A terminal piece with REAL extent is a genuine ring and must keep
	// both claims — the same-node split splits it because that geometry exists.
	realPts := []Point{mPt(0, 0), mPt(100, 0), mPt(200, 0), mPt(300, 0), mPt(350, 0), mPt(300, 0)}
	ring := collapseTerminalSameNodeSeams([]LegSeam{{Idx: 1, Node: NodeID(7), HasNode: true}, {Idx: 3, Node: j, HasNode: true}}, realPts, last, nil, &end)
	if len(ring) != 2 {
		t.Fatalf("a terminal piece with real extent produced %d seam(s), want 2 — it is a ring and the same-node split owns it", len(ring))
	}
}

// TestCollapseTerminalSameNodeSeams is the Prove-It for the seven synthetic-trail
// seeds that ABORTED THE WHOLE IMPORT (35, 47, 63, 71, 94, 99, 130) once the endpoint warp
// could land a terminal on the node the leg's own last seam already named.
//
// Each case states the geometry in the units the defect is in — a seam index and
// a terminal node — rather than through a full import, so a failure names the
// rule that broke rather than the seed that noticed.
func TestCollapseTerminalSameNodeSeams(t *testing.T) {
	node := func(n NodeID) *NodeID { return &n }
	seam := func(idx int, n NodeID) LegSeam { return LegSeam{Idx: idx, Node: n, HasNode: true} }
	declined := func(idx int) LegSeam { return LegSeam{Idx: idx} }

	cases := []struct {
		name       string
		seams      []LegSeam
		last       int
		start, end *NodeID
		want       []LegSeam
	}{{
		// The measured defect: leg rejoins canon at J at the second-to-last point,
		// then ends within the warp radius of J. Final piece would be two points
		// both ending on J — zero-length, and the same-node split has no interior vertex to cut.
		name:  "end terminal adopts the node the last seam named",
		seams: []LegSeam{seam(3, 7), seam(9, 4)},
		last:  10, end: node(4),
		want: []LegSeam{seam(3, 7)},
	}, {
		name:  "start terminal adopts the node the first seam named (seed 63)",
		seams: []LegSeam{seam(1, 4), seam(6, 9)},
		last:  10, start: node(4),
		want: []LegSeam{seam(6, 9)},
	}, {
		name:  "both ends collapse",
		seams: []LegSeam{seam(1, 4), seam(5, 8), seam(9, 6)},
		last:  10, start: node(4), end: node(6),
		want: []LegSeam{seam(5, 8)},
	}, {
		// NOT adjacent: the piece from the seam to the terminal has interior
		// vertices, so it is a real ring and the same-node split commits it as two arcs.
		name:  "same node but a gap — a genuine ring, left alone",
		seams: []LegSeam{seam(3, 7), seam(8, 4)},
		last:  10, end: node(4),
		want: []LegSeam{seam(3, 7), seam(8, 4)},
	}, {
		name:  "adjacent but a DIFFERENT node — nothing to collapse",
		seams: []LegSeam{seam(3, 7), seam(9, 5)},
		last:  10, end: node(4),
		want: []LegSeam{seam(3, 7), seam(9, 5)},
	}, {
		// A declined seam mints the leg's own node and names none, so it cannot be
		// the terminal's node and must never be dropped by this rule.
		name:  "adjacent declined seam is not a same-node collapse",
		seams: []LegSeam{seam(3, 7), declined(9)},
		last:  10, end: node(4),
		want: []LegSeam{seam(3, 7), declined(9)},
	}, {
		name:  "no warp at either terminal leaves every seam standing",
		seams: []LegSeam{seam(1, 4), seam(9, 6)},
		last:  10,
		want:  []LegSeam{seam(1, 4), seam(9, 6)},
	}, {
		name:  "a single seam that both terminals would claim collapses once and stays empty",
		seams: []LegSeam{seam(1, 4)},
		last:  2, start: node(4), end: node(4),
		want: nil,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// DISTINCT points, 100 m apart, so every piece in this table HAS extent.
			// That keeps each case testing what it was written to test: an adjacent
			// (two-point) piece collapses because two points never have extent
			// whatever their coordinates, and the "gap" case stays a genuine ring
			// because its interior vertices are real. The coincident-points case is
			// TestA2_TerminalSeamOnOneNodeWithCoincidentInteriorCollapses.
			pts := make([]Point, tc.last+1)
			for i := range pts {
				pts[i] = mPt(float64(i)*100, 0)
			}
			got := collapseTerminalSameNodeSeams(append([]LegSeam(nil), tc.seams...), pts, tc.last, tc.start, tc.end)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d seam(s) %+v, want %d %+v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("seam %d: got %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}
