package merge

import (
	"testing"
)

// TestFollowCorridorAssemble_AnyNodeSnapsInteriorBoundary is a real chord
// found in play-testing, hand-built, run through the live assembler
// [AssembleFollowedLegRunsCorridor]. An ON run's boundary lands a few metres
// past an existing node that is INTERIOR to its own candidate, nowhere near
// either of that candidate's own ends, so [snapMeasureToCandidateEnd] cannot
// reach it (own-end near = 30 m here, far outside the 15 m budget) and only a
// real node lookup can. With [NoNodeLookup] the assembler draws a chord from
// the shared node to the interior point, skipping canon's own vertex 1 m
// further in; with a lookup that reports the node, the boundary rides canon
// through it instead.
//
// Production passes [NoNodeLookup] today, so the
// any-node arm is inert in a real import. This test is what keeps that arm
// exercised until it is wired. The cripple IS the test: the SAME unmodified
// code is called once with [NoNodeLookup] (asserting the chord) and once with
// a fake lookup that reports the node (asserting the fix). Anything that made
// the any-node branch a no-op would make BOTH calls produce the chord.
func TestFollowCorridorAssemble_AnyNodeSnapsInteriorBoundary(t *testing.T) {
	b := DefaultFollowBands()
	node := mPt(50, 0)
	arcA := mCanon(1, mPt(0, 0), mPt(25, 0), mPt(50, 0))
	// arcB's own ends are 30 m and 50 m from the node — well outside
	// EndpointWarpRadiusM (15 m), so own-end snapping declines and only the
	// any-node lookup can reach it. (51,0) is canon's own vertex 1 m past the
	// node, the one a chord must not skip.
	arcB := mCanon(2, mPt(20, 0), mPt(47, 0), mPt(50, 0), mPt(51, 0), mPt(53, 0), mPt(100, 0))
	canon := []CanonArc{arcA, arcB}
	leg := straightLegX(21, 100, 0) // a vertex every 5 m; vertex 10 is the node
	// Computed, not hand-summed: mPt's metre offsets are a linear lat/lon
	// approximation, so haversine over (20,47,50) is not bit-exactly 27+3.
	nodeMeasureOnArcB := CanonMeasureLengthM(arcB.Vertices[:3])
	const measureStart = 33.0 // (53,0): 3 m past the node, past the 1 m vertex too
	runs := []FollowRun{
		{Kind: RunOn, Start: SourcePos{Edge: 0, Frac: 0}, End: SourcePos{Edge: 10, Frac: 0}, CanonID: arcA.ID, CanonIdx: 0,
			MeasureStart: 0, MeasureEnd: CanonMeasureLengthM(arcA.Vertices)},
		// Start is where run1's own first source position sits, 3 m past the
		// node on source edge 10 (x=50..55), matching MeasureStart.
		{Kind: RunOn, Start: SourcePos{Edge: 10, Frac: 0.6}, End: SourcePos{Edge: 19, Frac: 1}, CanonID: arcB.ID, CanonIdx: 1,
			MeasureStart: measureStart, MeasureEnd: CanonMeasureLengthM(arcB.Vertices)},
	}

	fakeLookup := func(lat, lon, radiusM float64) (float64, float64, NodeID, bool, error) {
		if HaversineMetres(lat, lon, node.Lat, node.Lon) <= radiusM {
			return node.Lat, node.Lon, NodeID(999), true, nil
		}
		return 0, 0, 0, false, nil
	}
	findRunB := func(eff []FollowRun) FollowRun {
		t.Helper()
		for _, r := range eff {
			if r.Kind == RunOn && r.CanonID == arcB.ID {
				return r
			}
		}
		t.Fatalf("no effective ON run bound to arcB in %+v", eff)
		return FollowRun{}
	}

	before, err := AssembleFollowedLegRunsCorridor(leg, canon, runs, b, NoNodeLookup)
	if err != nil {
		t.Fatalf("assembleFollowedLegRunsCorridor (noNodeLookup): %v", err)
	}
	if r := findRunB(before.Runs); r.MeasureStart != measureStart {
		t.Errorf("noNodeLookup: effective MeasureStart = %v, want %v unchanged — no lookup means no snap", r.MeasureStart, measureStart)
	}
	assertChordSkipsVertex(t, "noNodeLookup", before.Points, node, mPt(51, 0))

	after, err := AssembleFollowedLegRunsCorridor(leg, canon, runs, b, fakeLookup)
	if err != nil {
		t.Fatalf("assembleFollowedLegRunsCorridor (fakeLookup): %v", err)
	}
	if r := findRunB(after.Runs); r.MeasureStart != nodeMeasureOnArcB {
		t.Errorf("fakeLookup: effective MeasureStart = %v, want %v (snapped to the node)", r.MeasureStart, nodeMeasureOnArcB)
	}
	assertRidesCanonThroughNode(t, "fakeLookup", after.Points, node, mPt(51, 0))
}

// assertChordSkipsVertex is [assertRidesCanonThroughNode]'s negative:
// confirms the chord shape actually occurs (the node's next committed point is
// NOT canon's own vertex) — evidence the fixture reproduces the defect, not
// just that the fix's assertion happens to pass either way.
func assertChordSkipsVertex(t *testing.T, name string, out []Point, node, skippedVertex Point) {
	t.Helper()
	for i := 0; i+1 < len(out); i++ {
		if out[i].Lat == node.Lat && out[i].Lon == node.Lon {
			if out[i+1].Lat == skippedVertex.Lat && out[i+1].Lon == skippedVertex.Lon {
				t.Errorf("%s: node's next committed point is already canon's vertex (%.7f,%.7f) — fixture does not reproduce the defect",
					name, skippedVertex.Lat, skippedVertex.Lon)
			}
			return
		}
	}
	t.Errorf("%s: the shared node is absent from the committed leg: %+v", name, out)
}

// assertRidesCanonThroughNode checks the committed points contain `node`
// immediately followed by `nextVertex` — no chord, no skipped canon vertex.
func assertRidesCanonThroughNode(t *testing.T, name string, out []Point, node, nextVertex Point) {
	t.Helper()
	for i := 0; i+1 < len(out); i++ {
		if out[i].Lat == node.Lat && out[i].Lon == node.Lon {
			if out[i+1].Lat != nextVertex.Lat || out[i+1].Lon != nextVertex.Lon {
				t.Errorf("%s: node's next committed point is (%.7f,%.7f), want canon's own vertex (%.7f,%.7f) — a chord skipped it: %+v",
					name, out[i+1].Lat, out[i+1].Lon, nextVertex.Lat, nextVertex.Lon, out)
			}
			return
		}
	}
	t.Errorf("%s: the shared node is absent from the committed leg: %+v", name, out)
}
