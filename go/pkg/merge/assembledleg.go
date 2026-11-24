package merge

// assembledleg.go — the value the follow assembler returns, and the small
// helpers shared by every assembly path: the degenerate-leg case, the piece-range
// post-condition, and run text for error messages. The assembler itself is
// [assembleFollowedLegRunsCorridor].

import (
	"errors"
	"fmt"
)

// assembledLeg is everything the assembler knows about one leg it just built:
// the committed points, the effective runs, and where each ON run's on-canon
// stretch SITS in those points.
//
// It is one value rather than three returns because it is one answer. Points,
// runs and piece ranges are three views of a single assembly; parallel slices
// would mean a new signature per view and no place for the invariants that tie
// them together (see checkOnPieceRanges).
//
// The ranges are deliberately NOT on [followRun]. followRun is the matcher's
// vocabulary — [corridorFollowRuns] and the tests' hand-built runs produce it —
// and a field only the assembler fills would be left ZERO by every other
// producer. Zero is a valid point index, so such a run would silently claim its
// piece begins at Points[0]: a wrong value that looks plausible.
type assembledLeg struct {
	Points []Point
	Runs   []followRun

	// OnPieces has one entry per ON run, in run order. A leg with no ON run has
	// none, which is most legs.
	OnPieces []OnPieceRange
}

// OnPieceRange locates one ON run's on-canon stretch inside [assembledLeg.Points].
//
// Lo and Hi are INCLUSIVE indices, and they bound exactly the points
// [assembleOnRun] produced — NOT the refined band crossings around them.
// `entryCross` is not on canon (it is where the source crossed the band, off the
// line), so the cut the caller makes falls BETWEEN entryCross and Points[Lo]. Points[Lo]
// is therefore both the preceding OFF piece's last point and this ON piece's
// first: one point, shared, which is the shared node. Off by one either way and
// the ON piece starts off canon or the seam is unshared and the result renders in
// pieces.
//
// ⚠ Lo is NOT always on canon: at a LEG TERMINAL [assembleOnRun] emits the
// verbatim source anchor before the on-line boundary (see its comment).
// TerminalStart / TerminalEnd say so, because that
// end must NOT be given a canon node — a highlight's end is a lat-lon anchor.
type OnPieceRange struct {
	Run    int // index into assembledLeg.Runs
	Lo, Hi int // inclusive, into assembledLeg.Points

	// The piece's first/last point is the leg's own terminal anchor rather than a
	// point on canon, so it can never be a shared canon node.
	TerminalStart bool
	TerminalEnd   bool
}

// checkOnPieceRanges is an INTRINSIC POST-CONDITION, not an oracle, and the
// distinction is load-bearing enough to state at the top.
//
// It checks the assembler's own bookkeeping against the points the assembler
// itself just appended: the ranges must be ordered, disjoint except at the shared
// seam points, and within bounds. It cannot tell you the assembly is RIGHT — only
// that the indices describe the array that was built.
//
// Two things cover the ranges: this, for an off-by-one, and — the real one —
// the caller's tests, where every interior ON piece must be byte-identical to
// the canon stretch it rides. A range off by one takes in entryCross, which is NOT on
// canon, so byte-identity fails.
func (a assembledLeg) checkOnPieceRanges() error {
	prevHi := -1
	for i, p := range a.OnPieces {
		if p.Lo < 0 || p.Hi < p.Lo || p.Hi >= len(a.Points) {
			return fmt.Errorf("ON pieces: ON piece %d spans points [%d,%d] of %d — not a range of the assembled leg",
				i, p.Lo, p.Hi, len(a.Points))
		}
		if p.Lo < prevHi {
			return fmt.Errorf("ON pieces: ON piece %d starts at point %d, before piece %d ended at %d",
				i, p.Lo, i-1, prevHi)
		}
		if p.Run < 0 || p.Run >= len(a.Runs) || a.Runs[p.Run].Kind != runOn {
			return fmt.Errorf("ON pieces: ON piece %d names run %d, which is not an ON run of this leg", i, p.Run)
		}
		prevHi = p.Hi
	}
	return nil
}

// assembleDegenerateLeg handles sub-two-point legs.
//
// A 1-point leg's only legal partition is one OFF run [0,0], which commits that
// point verbatim. A 0-point leg
// has no legal run at all. Both are errors rather than silent empties when the
// runs disagree, because a caller that gets here with anything else has
// mis-derived its partition.
func assembleDegenerateLeg(pts []Point, runs []followRun) (assembledLeg, error) {
	if len(pts) == 0 {
		if len(runs) != 0 {
			return assembledLeg{}, fmt.Errorf("degenerate leg: %d run(s) on an empty leg", len(runs))
		}
		return assembledLeg{}, errors.New("degenerate leg: empty output")
	}
	for r := range runs {
		if runs[r].Kind != runOff || runs[r].Start != (sourcePos{}) || runs[r].End != (sourcePos{}) {
			return assembledLeg{}, fmt.Errorf(
				"degenerate leg: single-point leg carries run %d %+v — the only legal partition is one OFF run [0,0]",
				r, runBrief(runs[r]))
		}
	}
	if len(runs) == 0 {
		return assembledLeg{}, errors.New("degenerate leg: empty output")
	}
	return assembledLeg{Points: []Point{pts[0]}, Runs: append([]followRun(nil), runs...)}, nil
}

// runBrief is a followRun without its per-point projections, for error text.
//
// It must live in a non-test file: this file calls it from a production error
// path. Defined in a _test.go file, the package still compiles under `go test`
// (which compiles test files) but FAILS under `go build`, which no test-only
// gate can see.
func runBrief(r followRun) string {
	if r.Kind == runOff {
		return fmt.Sprintf("OFF[%+v,%+v]", r.Start, r.End)
	}
	return fmt.Sprintf("ON[%+v,%+v] arc=%d rev=%v measure=[%.6f,%.6f]", r.Start, r.End, r.CanonID, r.Reversed, r.MeasureStart, r.MeasureEnd)
}
