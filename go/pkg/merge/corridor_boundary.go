package merge

// The committed crossing vertex `cp` at a run boundary.
//
// A crossing vertex has two halves: the source-line POSITION where distance to
// canon crosses the corridor radius, and the committed POINT built there
// (lat/lon by linear interpolation, elevation by [lerpEle] at the same
// fraction). corridorinterval.go answers the position analytically: a
// corridorInterval's own ImportMeasureLo/ImportMeasureHi ARE that crossing,
// exact, with no search. [sourceMeasure.pointAt] (srcmeasure.go) builds the
// point.
//
// ── THE DECISION, PER BOUNDARY KIND (A/B/C/D) ─────────────────────────────
//
//	A. CLEAN OFF↔ON CROSSING. cp = corridorBoundaryPoint(sa, x) at the
//	   corridor interval's own boundary measure x (ImportMeasureLo entering ON,
//	   ImportMeasureHi leaving it). Position: exact, by construction.
//	   Elevation: lerped from the bracketing ORIGINAL source vertices
//	   (sa.pts[edge], sa.pts[edge+1]) at the position's own frac — see
//	   sourceMeasure.eleAt.
//	   Never dropped, never zeroed: nil propagates exactly as lerpEle already
//	   defines it, so an elevation-less source produces an elevation-less cp,
//	   not a fabricated sea-level one.
//
//	B. DECLINED REFINEMENT. No cp is minted.
//
//	C. ON↔ON SEAM. No cp is minted at a seam. The seam point is one of the two
//	   runs' own on-canon endpoints, assembleOnRun's output, and is not
//	   touched here.
//
//	D. LEG TERMINAL. Refinement never applies to a leg terminal. The leg's own
//	   verbatim terminal point is committed unchanged and is not touched here
//	   either.
//
// So only kind A mints a crossing vertex, and its elevation is answered
// explicitly, never dropped, never defaulted to 0 — see
// TestCorridorBoundaryPoint_PreservesElevation and
// TestSourceMeasureEleAt_NilPropagatesNeverFabricatesZero.
//
// ── IF cp WERE DROPPED (measured, not assumed) ──────────────────────────────
//
// TestCorridorBoundaryPoint_DroppingItMovesTheSeam builds a source edge that
// departs canon at 40 m and closes to 0 m over 100 m of travel (R = 15 m) and
// measures how far the analytic crossing sits off the straight chord that
// would replace it if cp were simply omitted — the chord a caller gets by
// jumping directly from the OFF run's last verbatim point to the ON run's
// first on-canon point. Measured: **≈12.6 m**, comparable to R itself. The
// leg would bridge the gap with its own geometry, which is exactly the defect
// the corridor matcher exists to fix, so dropping cp is not an available
// simplification.
//
// corridorBoundaryPoint, the test helper, builds it from a measure: only
// tests call it. Production mints the same kind-A vertex in
// follow_corridor_assemble.go from sa.pointAt(run.Start/End).
