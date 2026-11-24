package merge

// Assembly properties of the live follow path: [CorridorFollowRuns] partitions
// the leg and [AssembleFollowedLegRunsCorridor] commits it. The fixtures are
// the assembly rules (terminals verbatim, canon interiors byte-copied,
// duplicates preserved, seams deduped, no taper at a sparse canon terminal).

import (
	"math"
	"math/rand"
	"testing"
)

// assembleCorridorPoints runs the live assembler with no node access, as
// production does, and returns only the committed points.
func assembleCorridorPoints(leg []Point, canon []CanonArc, runs []FollowRun, b FollowBands) ([]Point, error) {
	asm, err := AssembleFollowedLegRunsCorridor(leg, canon, runs, b, NoNodeLookup)
	return asm.Points, err
}

// assembleCorridorRuns is [assembleCorridorPoints] plus the effective runs.
func assembleCorridorRuns(leg []Point, canon []CanonArc, runs []FollowRun, b FollowBands) ([]Point, []FollowRun, error) {
	asm, err := AssembleFollowedLegRunsCorridor(leg, canon, runs, b, NoNodeLookup)
	return asm.Points, asm.Runs, err
}

// legEndPos is the leg's own last position in [SourcePos] terms.
func legEndPos(leg []Point) SourcePos {
	return SourcePos{Edge: len(leg) - 2, Frac: 1}
}

func TestFollowCorridorAssemble_FullDuplicateInteriorByteIdenticalTerminalsVerbatim(t *testing.T) {
	b := DefaultFollowBands()
	// Canon with distinctive interior pips carrying elevation.
	canonPts := []Point{
		mPt(0, 0, 100), mPt(25, 0, 110), mPt(50, 0, 120), mPt(75, 0, 130), mPt(100, 0, 140),
	}
	canon := []CanonArc{mCanon(1, canonPts...)}
	// Re-import: same trail, 1 m offset, denser sampling, different ele.
	leg := make([]Point, 21)
	for i := range leg {
		e := 999.0
		leg[i] = mPt(100*float64(i)/20, 1, e)
	}
	out := assembleOrFail(t, leg, canon, b)

	// Terminals stay verbatim (byte-identical to the source terminals).
	if out[0] != leg[0] {
		t.Fatalf("first point must be verbatim source terminal: got %+v want %+v", out[0], leg[0])
	}
	if out[len(out)-1] != leg[len(leg)-1] {
		t.Fatalf("last point must be verbatim source terminal: got %+v want %+v", out[len(out)-1], leg[len(leg)-1])
	}
	// After the verbatim terminal comes the ON-LINE boundary — the committed
	// line joins canon at the measure the matcher chose, it does not drift to
	// whatever canon vertex happens to come next. Dropping these was the
	// sparse-terminal taper (see assembleOnRun).
	if out[1].Lat != canonPts[0].Lat || out[1].Lon != canonPts[0].Lon {
		t.Fatalf("out[1] must be the on-line entry %v,%v, got %v,%v", canonPts[0].Lat, canonPts[0].Lon, out[1].Lat, out[1].Lon)
	}
	last := len(out) - 2
	if out[last].Lat != canonPts[len(canonPts)-1].Lat || out[last].Lon != canonPts[len(canonPts)-1].Lon {
		t.Fatalf("out[%d] must be the on-line exit %v,%v, got %v,%v", last, canonPts[len(canonPts)-1].Lat, canonPts[len(canonPts)-1].Lon, out[last].Lat, out[last].Lon)
	}
	// Interior is byte-identical to canon's interior pips (lat/lon/ele).
	interior := out[2 : len(out)-2]
	wantInterior := canonPts[1 : len(canonPts)-1]
	if len(interior) != len(wantInterior) {
		t.Fatalf("interior length %d, want %d (canon interior pips): out=%+v", len(interior), len(wantInterior), out)
	}
	for i := range interior {
		if interior[i].Lat != wantInterior[i].Lat || interior[i].Lon != wantInterior[i].Lon {
			t.Fatalf("interior[%d] lat/lon %v,%v != canon %v,%v", i, interior[i].Lat, interior[i].Lon, wantInterior[i].Lat, wantInterior[i].Lon)
		}
		ge, gok := eleOf(interior[i])
		we, wok := eleOf(wantInterior[i])
		if gok != wok || (gok && ge != we) {
			t.Fatalf("interior[%d] ele mismatch: got (%v,%v) want (%v,%v)", i, ge, gok, we, wok)
		}
	}
}

func TestFollowCorridorAssemble_OffOnJunctionLandsOnCanonNoBackwardSpike(t *testing.T) {
	b := DefaultFollowBands()
	// Canon is a single long edge; the leg enters coincidence MID-edge.
	canon := []CanonArc{mCanon(1, mPt(0, 0), mPt(100, 0))}
	leg := []Point{
		mPt(10, 40), mPt(25, 40), // OFF, far north
		mPt(40, 1), mPt(55, 1), mPt(70, 1), mPt(85, 1), // ON, mid→end
	}
	runs, err := CorridorFollowRuns(t.Context(), leg, canon, b)
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	on := onRuns(runs)
	if len(on) != 1 {
		t.Fatalf("expected 1 ON run, got %+v", runs)
	}
	out, eff, err := assembleCorridorRuns(leg, canon, runs, b)
	if err != nil {
		t.Fatalf("assembleFollowedLegRunsCorridor: %v", err)
	}
	effOn := onRuns(eff)
	if len(effOn) != 1 {
		t.Fatalf("expected 1 effective ON run, got %+v", eff)
	}
	// The last OFF point then the ON entry boundary must be consecutive.
	// The entry boundary must be exactly the MeasureStart projection on canon,
	// and its measure must be strictly less than the exit boundary's — no
	// backward spike. Verify the entry boundary sits on the canon line.
	//
	// Compared against the EFFECTIVE run's MeasureStart, not the matcher's:
	// the emitted geometry is built from the assembler's measure.
	pc := PrepCandidates(canon)[0]
	entryLat, entryLon := pc.InterpOnCanon(effOn[0].MeasureStart)
	// Find the first on-line point in the output (y≈0).
	var entry Point
	found := false
	for _, p := range out {
		yM := (p.Lat - testBaseLat) * mPerDegLat
		if math.Abs(yM) < 1e-3 {
			entry = p
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no on-line ON point in output: %+v", out)
	}
	if math.Abs(entry.Lat-entryLat) > 1e-9 || math.Abs(entry.Lon-entryLon) > 1e-9 {
		t.Fatalf("entry boundary %v,%v != MeasureStart projection %v,%v", entry.Lat, entry.Lon, entryLat, entryLon)
	}
	// Measure-monotone: the on-line points' x must be strictly increasing (no
	// backward spike) across the ON run.
	prevX := -1.0
	for _, p := range out {
		yM := (p.Lat - testBaseLat) * mPerDegLat
		if math.Abs(yM) < 1e-3 {
			x := (p.Lon - testBaseLon) * mPerDegLon()
			if x < prevX-1e-6 {
				t.Fatalf("backward spike in ON geometry: x=%v after %v", x, prevX)
			}
			prevX = x
		}
	}
}

func TestFollowCorridorAssemble_AntiParallelReversedWithEle(t *testing.T) {
	b := DefaultFollowBands()
	canonPts := []Point{
		mPt(0, 0, 100), mPt(25, 0, 125), mPt(50, 0, 150), mPt(75, 0, 175), mPt(100, 0, 200),
	}
	canon := []CanonArc{mCanon(1, canonPts...)}
	// Leg east→west (reversed), 1 m off, no ele.
	leg := make([]Point, 21)
	for i := range leg {
		x := 100 - 100*float64(i)/20
		leg[i] = mPt(x, 1)
	}
	runs, err := CorridorFollowRuns(t.Context(), leg, canon, b)
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	on := onRuns(runs)
	if len(on) != 1 || !on[0].Reversed {
		t.Fatalf("expected 1 reversed ON run, got %+v", runs)
	}
	out, err := assembleCorridorPoints(leg, canon, runs, b)
	if err != nil {
		t.Fatalf("assembleFollowedLegRunsCorridor: %v", err)
	}
	// Interior canon vertices appear in REVERSED order: the committed
	// interior x should be descending (canon interior pips walked high→low).
	// out is [terminal, on-line entry, interior…, on-line exit, terminal] —
	// the on-line boundaries carry INTERPOLATED elevation, so they are not
	// part of the byte-copied interior this asserts on.
	interior := out[2 : len(out)-2]
	prevX := math.Inf(1)
	for _, p := range interior {
		x := (p.Lon - testBaseLon) * mPerDegLon()
		if x > prevX+1e-6 {
			t.Fatalf("anti-parallel interior not reversed (x ascending): %+v", interior)
		}
		prevX = x
		// ele byte-copied from the matched canon vertex (one of 125/150/175).
		if e, ok := eleOf(p); ok {
			if e != 125 && e != 150 && e != 175 {
				t.Fatalf("interior ele %v not a canon interior value", e)
			}
		}
	}
}

func TestFollowCorridorAssemble_BoundaryEleNilWhenCanonBoundNil(t *testing.T) {
	b := DefaultFollowBands()
	// Canon whose middle vertices lack elevation — a boundary interpolated
	// between a nil-ele pair must be nil.
	canon := []CanonArc{mCanon(1,
		mPt(0, 0, 100), mPt(50, 0), mPt(100, 0), // vertices 2,3 have nil ele
	)}
	leg := straightLegX(21, 100, 1)
	runs, err := CorridorFollowRuns(t.Context(), leg, canon, b)
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	if len(onRuns(runs)) != 1 {
		t.Fatalf("expected 1 ON run, got %+v", runs)
	}
	out, err := assembleCorridorPoints(leg, canon, runs, b)
	if err != nil {
		t.Fatalf("assembleFollowedLegRunsCorridor: %v", err)
	}
	// The exit boundary is interpolated near x=100 between two nil-ele
	// vertices → must be nil. Terminals are verbatim source (also nil).
	for _, p := range out {
		x := (p.Lon - testBaseLon) * mPerDegLon()
		if x > 55 { // beyond the last ele-bearing vertex (x=0)
			if _, ok := eleOf(p); ok {
				// A vertex byte-copied from a nil-ele canon vertex, or an
				// interpolation between nil bounds, must be nil.
				t.Fatalf("point past x=55 must have nil ele (nil canon bounds), got value: %+v", p)
			}
		}
	}
}

func TestFollowCorridorAssemble_NoZeroLengthStepsAndSeamDedup(t *testing.T) {
	b := DefaultFollowBands()
	// Two abutting canon arcs sharing an endpoint at x=50. A leg
	// coincident across both yields two ON runs; the ON↔ON seam must dedup
	// the shared boundary point and emit no zero-length step.
	canonA := mCanon(1, mPt(0, 0), mPt(25, 0), mPt(50, 0))
	canonB := mCanon(2, mPt(50, 0), mPt(75, 0), mPt(100, 0))
	canon := []CanonArc{canonA, canonB}
	leg := straightLegX(21, 100, 1)
	runs, err := CorridorFollowRuns(t.Context(), leg, canon, b)
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	if len(onRuns(runs)) != 2 {
		t.Fatalf("expected a 2-run chain, got %+v", runs)
	}
	out, err := assembleCorridorPoints(leg, canon, runs, b)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	for i := 1; i < len(out); i++ {
		if out[i].Lat == out[i-1].Lat && out[i].Lon == out[i-1].Lon {
			t.Fatalf("zero-length step at index %d: %+v", i, out[i])
		}
	}
}

func TestFollowCorridorAssemble_RepealedCorridorPostConditionCommitsVerbatim(t *testing.T) {
	b := DefaultFollowBands()
	// A hand-built ON run claiming coincidence with a canon the source is
	// nowhere near (40 m off) used to trip the symmetric-corridor
	// post-condition (assembleOnRun's CheckCorridor abort).
	//
	// A ruling (2026-08-19) REPEALS that abort:
	// it belonged to the pre-cutover matcher, which never proposed a run
	// outside it. Fabrication is now priced by the corridor solve's own
	// canon-side gap term, not caught post-hoc here — any run that
	// reaches the assembler, however it was produced, is committed as given.
	// This test now locks in THAT behaviour instead of the repealed one, so a
	// future accidental reinstatement of the abort goes red here rather than
	// silently reintroducing the old bound.
	canon := []CanonArc{mCanon(1, mPt(0, 0), mPt(100, 0))}
	leg := straightLegX(11, 100, 40) // 40 m north of canon
	pc := PrepCandidates(canon)[0]
	corrupt := []FollowRun{{
		Kind:         RunOn,
		Start:        SourcePos{},
		End:          legEndPos(leg),
		CanonID:      1,
		CanonIdx:     0,
		Reversed:     false,
		MeasureStart: 0,
		MeasureEnd:   pc.Total,
		Proj:         make([]RunProj, len(leg)),
	}}
	out, err := assembleCorridorPoints(leg, canon, corrupt, b)
	if err != nil {
		t.Fatalf("post-condition 3 is repealed — expected no error on a 40 m-off forced ON run, got %v", err)
	}
	// Not just "no error" — the committed geometry must actually RIDE canon
	// (Y=0), not the source's own Y=40 line, proving the abort is genuinely
	// gone rather than silently declining for some unrelated reason. Not
	// asserted point-by-point: assembleOnRun may keep a leg-terminal anchor
	// verbatim at Y=40 either side (assembly never moves a plan's endpoint,
	// unrelated to post-condition 3), so this only requires that SOME
	// committed point actually reached canon.
	canonLat := mPt(0, 0).Lat
	onCanon := false
	for _, p := range out {
		if math.Abs(p.Lat-canonLat) < 1e-9 {
			onCanon = true
			break
		}
	}
	if !onCanon {
		t.Fatalf("no committed point rides canon (Y=0) — expected the 40 m-off run to commit onto canon; got %+v", out)
	}
}

func TestFollowCorridorAssemble_AllOffIsVerbatim(t *testing.T) {
	b := DefaultFollowBands()
	// No canon → the whole leg is one OFF run → output byte-identical to
	// source (the empty-network degenerate case).
	leg := straightLegX(7, 60, 0)
	out, err := assembleCorridorPoints(leg, nil, []FollowRun{{Kind: RunOff, Start: SourcePos{}, End: legEndPos(leg)}}, b)
	if err != nil {
		t.Fatalf("assembleFollowedLegRunsCorridor: %v", err)
	}
	if len(out) != len(leg) {
		t.Fatalf("verbatim leg length %d != source %d", len(out), len(leg))
	}
	for i := range leg {
		if out[i] != leg[i] {
			t.Fatalf("verbatim point %d changed: %+v != %+v", i, out[i], leg[i])
		}
	}
}

func TestFollowCorridorAssemble_SpurApexNeverCommitted(t *testing.T) {
	b := DefaultFollowBands()
	// The source runs straight past a 30 m out-and-back canon spur at x=40
	// whose base no source sample lands on (samples straddle it at x=37.5
	// and x=42.5). Whatever partition the solve chooses around the spur, the
	// committed leg must follow canon and must never draw the spur's apex.
	canon := []CanonArc{mCanon(1,
		mPt(0, 0), mPt(20, 0), mPt(40, 0),
		mPt(40, 30), mPt(40, 0), // out-and-back spur, base at x=40
		mPt(60, 0), mPt(80, 0), mPt(100, 0),
	)}
	// Source samples straddle the spur base (offset 2.5 m so none is at x=40).
	leg := make([]Point, 20)
	for i := range leg {
		leg[i] = mPt(2.5+5*float64(i), 1)
	}
	runs, err := CorridorFollowRuns(t.Context(), leg, canon, b)
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	if len(onRuns(runs)) == 0 {
		t.Fatalf("expected the leg to follow canon somewhere, got all OFF: %+v", runs)
	}
	out, err := assembleCorridorPoints(leg, canon, runs, b)
	if err != nil {
		t.Fatalf("assembleFollowedLegRunsCorridor: %v", err)
	}
	onCanon := false
	for _, p := range out {
		yM := (p.Lat - testBaseLat) * mPerDegLat
		if yM > 15 {
			t.Fatalf("committed output contains the canon spur apex (y=%.1f m): %+v", yM, p)
		}
		if math.Abs(yM) < 1e-3 {
			onCanon = true
		}
	}
	if !onCanon {
		t.Fatalf("no committed point rides canon (y=0): %+v", out)
	}
}

// TestFollowCorridorAssemble_ExactDuplicateByteIdentical guards the assembly half of
// the duplicate-re-import bar: a leg that is an EXACT copy of a committed canon
// arc, following only that arc, commits an interior polyline
// byte-identical to the canon's interior (IEEE-754 bits, ele included). This is
// the assembly-level invariant; the stored-row dup bar additionally depends on
// re-import seeing the same canon it saw the first time (re-derive stability),
// which is a separate concern outside the assembler.
func TestFollowCorridorAssemble_ExactDuplicateByteIdentical(t *testing.T) {
	b := DefaultFollowBands()
	check := func(name string, canonPts []Point) {
		canon := []CanonArc{{ID: 1, Vertices: canonPts}}
		leg := append([]Point(nil), canonPts...) // exact copy
		out := assembleOrFail(t, leg, canon, b)
		// Terminals verbatim; interior byte-identical to canon's interior.
		aInt := canonPts[1 : len(canonPts)-1]
		bInt := out[1 : len(out)-1]
		if len(bInt) != len(aInt) {
			t.Fatalf("%s: committed interior has %d pts, canon interior has %d (exact dup must match)", name, len(bInt), len(aInt))
		}
		for i := range aInt {
			if math.Float64bits(bInt[i].Lat) != math.Float64bits(aInt[i].Lat) ||
				math.Float64bits(bInt[i].Lon) != math.Float64bits(aInt[i].Lon) {
				t.Fatalf("%s: interior[%d] not byte-identical to canon vertex", name, i)
			}
			be, bok := eleOf(bInt[i])
			ae, aok := eleOf(aInt[i])
			if bok != aok || (bok && math.Float64bits(be) != math.Float64bits(ae)) {
				t.Fatalf("%s: interior[%d] ele not byte-identical", name, i)
			}
		}
	}
	// Wiggly line with elevation, and a self-approaching winding walk.
	check("wiggle-ele", []Point{
		mPt(0, 0, 100), mPt(30, 2, 110), mPt(60, -1, 105), mPt(90, 3, 120), mPt(120, 0, 130), mPt(150, 1, 140),
	})
	rng := rand.New(rand.NewSource(1860))
	check("winding-self-approach", randCandidates(rng, 1)[0].Vertices)
}

func TestFollowCorridorAssemble_OffVerbatimPreservesSourceDuplicates(t *testing.T) {
	b := DefaultFollowBands()
	// Empty network → all-OFF verbatim. A consecutive duplicate source point
	// (GPS resting jitter) must be PRESERVED byte-for-byte — no silent
	// shortening (faithful import).
	leg := []Point{mPt(0, 0), mPt(10, 1), mPt(20, 0), mPt(20, 0), mPt(30, 1), mPt(40, 0)}
	out, err := assembleCorridorPoints(leg, nil, []FollowRun{{Kind: RunOff, Start: SourcePos{}, End: legEndPos(leg)}}, b)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(out) != len(leg) {
		t.Fatalf("verbatim OFF dropped a point: got %d want %d (the source duplicate must survive)", len(out), len(leg))
	}
	for i := range leg {
		if out[i] != leg[i] {
			t.Fatalf("OFF point %d changed: %+v != %+v", i, out[i], leg[i])
		}
	}
	if !(out[2].Lat == out[3].Lat && out[2].Lon == out[3].Lon) {
		t.Fatalf("the consecutive-duplicate pair was not preserved")
	}
}

func TestFollowCorridorAssemble_OnInteriorPreservesCanonDuplicates(t *testing.T) {
	b := DefaultFollowBands()
	// Canon with a consecutive DUPLICATE vertex at (60,0). ON interior is a
	// byte-copy of canon — the duplicate must appear in the committed interior.
	canon := []CanonArc{mCanon(1, mPt(0, 0), mPt(30, 0), mPt(60, 0), mPt(60, 0), mPt(90, 0), mPt(120, 0))}
	leg := straightLegX(25, 120, 1)
	runs, err := CorridorFollowRuns(t.Context(), leg, canon, b)
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	if len(onRuns(runs)) != 1 {
		t.Fatalf("expected 1 ON run over the dup-vertex canon, got %+v", runs)
	}
	out, err := assembleCorridorPoints(leg, canon, runs, b)
	if err != nil {
		t.Fatalf("assemble (post-conditions with canon dup): %v", err)
	}
	want := mPt(60, 0)
	dupFound := false
	for i := 1; i < len(out); i++ {
		if out[i].Lat == want.Lat && out[i].Lon == want.Lon && out[i-1].Lat == want.Lat && out[i-1].Lon == want.Lon {
			dupFound = true
		}
	}
	if !dupFound {
		t.Fatalf("canon duplicate vertex (60,0) not preserved in committed interior: %+v", out)
	}
}

func TestFollowCorridorAssemble_OnOnSeamStillDedups(t *testing.T) {
	b := DefaultFollowBands()
	// Two abutting canon arcs share the exact vertex (50,0). A leg
	// following both chains across the seam; arc1's minted exit coincides with
	// arc2's minted entry — the SOLE ratified dedup (keep-first). No consecutive
	// duplicate may survive at the seam.
	arcA := mCanon(1, mPt(0, 0), mPt(25, 0), mPt(50, 0))
	arcB := mCanon(2, mPt(50, 0), mPt(75, 0), mPt(100, 0))
	canon := []CanonArc{arcA, arcB}
	leg := straightLegX(21, 100, 1)
	runs, err := CorridorFollowRuns(t.Context(), leg, canon, b)
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	if len(onRuns(runs)) != 2 {
		t.Fatalf("expected a 2-run chain, got %+v", runs)
	}
	out, err := assembleCorridorPoints(leg, canon, runs, b)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	for i := 1; i < len(out); i++ {
		if out[i].Lat == out[i-1].Lat && out[i].Lon == out[i-1].Lon {
			t.Fatalf("ON↔ON seam not deduped: consecutive duplicate at %d: %+v", i, out[i])
		}
	}
}

// TestFollowCorridorAssemble_SparseCanonTerminalDoesNotTaper is the regression fixture
// for a play-testing report (2026-07-25): a plan that correctly matched
// committed canon still rendered as a SECOND STRAND beside it for 136 m.
//
// The cause was not the matcher — every point classified ON — but assembly.
// A run reaching a leg terminal substituted the verbatim source endpoint for
// the on-line boundary INSTEAD OF emitting both, so the committed line ran
// from the endpoint straight to the first INTERIOR canon vertex. Canon vertex
// spacing belongs to whoever imported it, so where canon is one long straight
// edge the line only reaches it at that edge's far end and tapers beside it
// the whole way. Keeping the endpoint is correct — assembly never moves terminals —
// but it must not cost the join.
//
// The fixture is the real geometry's shape: canon with NO interior vertex over
// the followed stretch, and a source terminal offset within the band. Under
// the old assembly the committed line is the source's own chord, up to ~2.4 m
// off canon along its whole length; the assertion below fails at ~1.9 m.
func TestFollowCorridorAssemble_SparseCanonTerminalDoesNotTaper(t *testing.T) {
	b := DefaultFollowBands()
	// Vertex-less canon: one 300 m straight edge, exactly like the real
	// 145.7 m tail edge a later file had to reach across.
	canonPts := []Point{mPt(0, 0), mPt(300, 0)}
	canon := []CanonArc{mCanon(1, canonPts...)}

	// Source runs parallel inside the Enter band, starting 2.4 m off (the
	// measured real offset) and converging — so it is ON throughout.
	leg := make([]Point, 21)
	for i := range leg {
		f := float64(i) / 20
		leg[i] = mPt(300*f, 2.4*(1-f))
	}
	out := assembleOrFail(t, leg, canon, b)

	// The terminals stay exactly where the source put them.
	if out[0] != leg[0] {
		t.Fatalf("first point must be the verbatim source terminal: got %+v want %+v", out[0], leg[0])
	}
	if out[len(out)-1] != leg[len(leg)-1] {
		t.Fatalf("last point must be the verbatim source terminal: got %+v want %+v", out[len(out)-1], leg[len(leg)-1])
	}

	// THE BUG: walk the committed polyline densely and require that, away from
	// the sub-Stay hop at each terminal, it lies ON canon rather than tapering
	// beside it. Canon here is the line y=0 over x in [0,300], so the offence is
	// simply |y| — which is exactly what the map draws as a second strand.
	xOf := func(p Point) float64 { return (p.Lon - testBaseLon) * mPerDegLon() }
	yOf := func(p Point) float64 { return (p.Lat - testBaseLat) * mPerDegLat }
	const skirt = 6.0 // ignore the terminal hops themselves
	worstD, worstAt := 0.0, 0.0
	for i := 0; i+1 < len(out); i++ {
		x0, y0 := xOf(out[i]), yOf(out[i])
		x1, y1 := xOf(out[i+1]), yOf(out[i+1])
		for k := 0; k <= 400; k++ {
			f := float64(k) / 400
			x, y := x0+(x1-x0)*f, y0+(y1-y0)*f
			if x < skirt || x > 300-skirt {
				continue
			}
			if d := math.Abs(y); d > worstD {
				worstD, worstAt = d, x
			}
		}
	}
	if worstD > 0.05 {
		t.Fatalf("committed line tapers beside canon: %.3f m off at measure %.1f m (want on-canon)\nout=%+v", worstD, worstAt, out)
	}
}

// assembleOrFail partitions then assembles, failing the test on error.
func assembleOrFail(t *testing.T, leg []Point, canon []CanonArc, b FollowBands) []Point {
	t.Helper()
	runs, err := CorridorFollowRuns(t.Context(), leg, canon, b)
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	out, err := assembleCorridorPoints(leg, canon, runs, b)
	if err != nil {
		t.Fatalf("assembleFollowedLegRunsCorridor: %v", err)
	}
	return out
}

func onRuns(runs []FollowRun) []FollowRun {
	var out []FollowRun
	for _, r := range runs {
		if r.Kind == RunOn {
			out = append(out, r)
		}
	}
	return out
}

func eleOf(p Point) (float64, bool) {
	if p.Ele == nil {
		return 0, false
	}
	return *p.Ele, true
}

// randCandidates builds n random WINDING polyline candidates: long random
// walks with small steps in a tight box, so a candidate frequently
// self-approaches (two far-apart measure positions land spatially close). That
// self-approach is what lets a re-imported leg overlap ONE arc at two
// disjoint measures — the real-data shape behind the measure-descending / corridor-
// breach defects.
func randCandidates(rng *rand.Rand, n int) []CanonArc {
	cands := make([]CanonArc, n)
	for i := range cands {
		nv := 20 + rng.Intn(30)
		verts := make([]Point, nv)
		x := rng.Float64() * 120
		y := rng.Float64() * 120
		for j := range verts {
			verts[j] = mPt(x, y)
			x += -12 + rng.Float64()*24
			y += -12 + rng.Float64()*24
		}
		cands[i] = CanonArc{ID: ArcID(i + 1), Vertices: verts}
	}
	return cands
}

// handoffRunsCorridor hand-builds the two ON runs of straightLegX(21, 100, 0)
// (a vertex every 5 m), split into arcA [0,50] and arcB [50,100] at the shared
// node (50,0), with arcB's MeasureStart set to `measureStart` instead of the
// exact 0 the node sits at. run1's Start is the source position measureStart
// metres past the node, so Start and MeasureStart name the same place, as a
// matcher-produced run's would.
//
// ⚠ HAND-BUILT, NOT THE MATCHER'S OWN OUTPUT. Steering the matcher into
// producing a chosen measure tests the matcher, not the assembler; the runs
// are stated directly.
func handoffRunsCorridor(arcA, arcB CanonArc, measureStart float64) []FollowRun {
	past := 50 + measureStart
	edge := int(past / 5)
	return []FollowRun{
		{Kind: RunOn, Start: SourcePos{Edge: 0, Frac: 0}, End: SourcePos{Edge: 10, Frac: 0}, CanonID: arcA.ID, CanonIdx: 0,
			MeasureStart: 0, MeasureEnd: CanonMeasureLengthM(arcA.Vertices)},
		{Kind: RunOn, Start: SourcePos{Edge: edge, Frac: (past - 5*float64(edge)) / 5}, End: SourcePos{Edge: 19, Frac: 1}, CanonID: arcB.ID, CanonIdx: 1,
			MeasureStart: measureStart, MeasureEnd: CanonMeasureLengthM(arcB.Vertices)},
	}
}

// TestFollowCorridorAssemble_OnOnHandoffSnapsToSharedNode reproduces the
// a real probe: two point-contiguous ON runs hand off between
// adjacent canon arcs — no OFF run between them, so the leg never left canon
// — but the second run's own measure boundary is a few metres short of the
// shared node. Without the snap the assembler draws a CHORD from the node to
// that interior point, skipping canon's own vertex in between; with it, the
// boundary snaps onto the node (the candidate's own end, which needs no node
// lookup) and canon's own geometry survives.
func TestFollowCorridorAssemble_OnOnHandoffSnapsToSharedNode(t *testing.T) {
	b := DefaultFollowBands()
	arcA := mCanon(1, mPt(0, 0), mPt(25, 0), mPt(50, 0))
	// arcB carries an interior vertex 3 m from the shared node (53,0) — canon's
	// own geometry the chord must not skip.
	arcB := mCanon(2, mPt(50, 0), mPt(53, 0), mPt(75, 0), mPt(100, 0))
	canon := []CanonArc{arcA, arcB}
	leg := straightLegX(21, 100, 0)

	// 4 m past the node — well inside EndpointWarpRadiusM (15 m) and past the
	// interior vertex at 3 m, so an unsnapped assembly skips it.
	runs := handoffRunsCorridor(arcA, arcB, 4.0)

	out, eff, err := assembleCorridorRuns(leg, canon, runs, b)
	if err != nil {
		t.Fatalf("assembleFollowedLegRunsCorridor: %v", err)
	}
	effIdx := -1
	for i, r := range eff {
		if r.Kind == RunOn && r.CanonID == arcB.ID {
			effIdx = i
		}
	}
	if effIdx < 0 {
		t.Fatalf("no effective ON run bound to arcB in %+v", eff)
	}
	if eff[effIdx].MeasureStart != 0 {
		t.Errorf("effective MeasureStart = %v, want 0 (snapped to the shared node)", eff[effIdx].MeasureStart)
	}
	// No consecutive duplicate (the ON↔ON seam dedup must still fire — the
	// snap makes the two boundaries coincide again), and the node's very next
	// committed point is canon's own (53,0) vertex, not a chord past it.
	for i := 1; i < len(out); i++ {
		if out[i-1].Lat == out[i].Lat && out[i-1].Lon == out[i].Lon {
			t.Errorf("consecutive duplicate at %d — ON↔ON dedup did not fire after the snap: %+v", i, out[i])
		}
	}
	assertRidesCanonThroughNode(t, "handoff", out, mPt(50, 0), mPt(53, 0))
}

// TestFollowCorridorAssemble_OnOnHandoffBeyondBudgetNotSnapped is the other
// side of the band: a boundary 20 m from the shared node (> EndpointWarpRadiusM's
// 15 m) is a genuine gap, not a rounding artefact, and must NOT be snapped —
// the join stays exactly where the run named it.
//
// ⚠ TWO DEFENCES HOLD THIS OUTCOME, so it pins the outcome, not the budget.
// Widening the snap budget to 25 m leaves this green (measured): the trial
// snap is then refused by the node-snap corridor gate ([CheckCorridor] at
// CorridorRadiusM), because 20 m of canon before the run's own source start
// is outside the corridor. Widening the budget AND opening that gate turns it
// red (measured).
func TestFollowCorridorAssemble_OnOnHandoffBeyondBudgetNotSnapped(t *testing.T) {
	b := DefaultFollowBands()
	arcA := mCanon(1, mPt(0, 0), mPt(25, 0), mPt(50, 0))
	arcB := mCanon(2, mPt(50, 0), mPt(75, 0), mPt(100, 0))
	canon := []CanonArc{arcA, arcB}
	leg := straightLegX(21, 100, 0)

	const beyond = 20.0 // > EndpointWarpRadiusM (15)
	runs := handoffRunsCorridor(arcA, arcB, beyond)

	_, eff, err := assembleCorridorRuns(leg, canon, runs, b)
	if err != nil {
		t.Fatalf("assembleFollowedLegRunsCorridor: %v", err)
	}
	effIdx := -1
	for i, r := range eff {
		if r.Kind == RunOn && r.CanonID == arcB.ID {
			effIdx = i
		}
	}
	if effIdx < 0 {
		t.Fatalf("no effective ON run bound to arcB in %+v", eff)
	}
	if eff[effIdx].MeasureStart != beyond {
		t.Errorf("effective MeasureStart = %v, want %v unchanged — 20 m is beyond the 15 m budget", eff[effIdx].MeasureStart, beyond)
	}
}

// onRunsBoundTo reports whether every ON run in runs binds canonID, and at
// least one does. The tests below call the matcher directly on a hand-built
// fixture, so the shape assertion is a HUMAN-WRITTEN expectation on the
// matcher's own return value.
func onRunsBoundTo(runs []FollowRun, canonID ArcID) bool {
	found := false
	for _, r := range runs {
		if r.Kind != RunOn {
			continue
		}
		found = true
		if r.CanonID != canonID {
			return false
		}
	}
	return found
}

func TestFollowCorridorMatch_AntiParallel(t *testing.T) {
	bands := DefaultFollowBands()
	a := straightLegX(11, 100, 0)
	b := make([]Point, 21) // east→west, 1 m north
	for i := range b {
		b[i] = mPt(100-100*float64(i)/20, 1)
	}
	canon := []CanonArc{{ID: 1, Vertices: a}}
	runs, err := CorridorFollowRuns(t.Context(), b, canon, bands)
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	if !onRunsBoundTo(runs, 1) {
		t.Fatalf("anti-parallel B must follow A (reversed), got runs %+v", runs)
	}
	for _, r := range onRuns(runs) {
		if !r.Reversed {
			t.Fatalf("anti-parallel B must bind A reversed, got %+v", r)
		}
	}
}

func TestFollowCorridorMatch_CanonAcrossAbuttingArcs(t *testing.T) {
	bands := DefaultFollowBands()
	ca := straightLegX(6, 50, 0) // x 0..50
	cb := make([]Point, 6)
	for i := range cb {
		cb[i] = mPt(50+10*float64(i), 0) // x 50..100, abuts ca at x=50
	}
	leg := straightLegX(21, 100, 1) // spans both, 1 m north
	canon := []CanonArc{{ID: 1, Vertices: ca}, {ID: 2, Vertices: cb}}
	runs, err := CorridorFollowRuns(t.Context(), leg, canon, bands)
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	var onRuns []FollowRun
	for _, r := range runs {
		if r.Kind == RunOn {
			onRuns = append(onRuns, r)
		}
	}
	if len(onRuns) != 2 {
		t.Fatalf("a leg over two abutting canon arcs must recover as 2 consecutive ON runs, got %d: %+v", len(onRuns), runs)
	}
	got := map[ArcID]bool{onRuns[0].CanonID: true, onRuns[1].CanonID: true}
	if !got[1] || !got[2] {
		t.Fatalf("chained runs must bind both canon arcs 1 and 2, got %+v", onRuns)
	}
}

func TestFollowCorridorMatch_HairpinArm(t *testing.T) {
	bands := DefaultFollowBands()
	// Canon: out along north 0 (x 0→90), turn, back along north 3 (x 90→0) — a
	// hairpin whose two arms are 3 m apart (both within Stay of a mid source).
	hp := []Point{
		mPt(0, 0), mPt(30, 0), mPt(60, 0), mPt(90, 0),
		mPt(90, 3), mPt(60, 3), mPt(30, 3), mPt(0, 3),
	}
	// Source follows the OUTBOUND arm only (x 0→90 at north 0.5): monotone
	// continuity must bind the outbound arm, never jump to the return arm.
	src := make([]Point, 19)
	for i := range src {
		src[i] = mPt(5*float64(i), 0.5)
	}
	canon := []CanonArc{{ID: 1, Vertices: hp}}
	runs, err := CorridorFollowRuns(t.Context(), src, canon, bands)
	if err != nil {
		t.Fatalf("corridorFollowRuns: %v", err)
	}
	if !onRunsBoundTo(runs, 1) {
		t.Fatalf("hairpin follower must bind the canon arc on the outbound arm, got runs %+v", runs)
	}
	// Both arms are arc 1, so the arc ID cannot tell them apart: the measure
	// and direction can. The outbound arm is measure [0, outEnd], walked forward.
	outEnd := CanonMeasureLengthM(hp[:4])
	for _, r := range onRuns(runs) {
		if r.Reversed || r.MeasureStart < -1e-6 || r.MeasureEnd > outEnd+1e-6 {
			t.Fatalf("hairpin follower bound the return arm: want forward within [0, %.2f], got %+v", outEnd, r)
		}
	}
}
