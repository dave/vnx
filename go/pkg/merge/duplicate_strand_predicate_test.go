package merge

// THE BYTE-IDENTICAL PREDICATE, AND ITS OWN SELF-TEST.
//
// The duplicate-strand rule is "same vertex sequence, exactly, forward or
// reversed". Every number the rule is calibrated on comes out of this function,
// and the census
// that used to stand in for it was measuring something looser — a ONE-WAY vertex
// Hausdorff zero, which counted 15 strands where sequence equality counts 10.
//
// ⚠ THE PREDICATE HAS ITS OWN TESTS BECAUSE THE CENSUS CANNOT RED-PROOF IT.
// The census's own counts are report-only by design (they are the rule's
// progress metric and are supposed to fall), so a predicate that silently answered "yes"
// to everything would change a log line and fail nothing. The cases below are
// the crippling: each one is a way the predicate could be wrong that a
// whole-import run would not notice.

import (
	"math"
	"testing"
)

// ⚠ THE PREDICATE ITSELF LIVES IN duplicate_arc_core.go, NOT HERE
// (2026-09-01). It started as test-only, measuring the census; it is now the
// PRODUCTION rule that decides whether a piece references canon or commits a
// copy. One definition site, deliberately: an instrument and a rule that are
// separately written drift, and the first measurement found that they had.

func dupPt(lat, lon float64) Point {
	return Point{Lat: lat, Lon: lon}
}

func dupPtEle(lat, lon, ele float64) Point {
	return Point{Lat: lat, Lon: lon, Ele: &ele}
}

// TestDupVertexSequenceEqual is the predicate's red-proof: every row is a way
// the answer could be wrong, and each is asserted rather than reported.
func TestDupVertexSequenceEqual(t *testing.T) {
	base := []Point{dupPt(54.0, -3.0), dupPt(54.1, -3.1), dupPt(54.2, -3.2)}
	rev := []Point{dupPt(54.2, -3.2), dupPt(54.1, -3.1), dupPt(54.0, -3.0)}

	cases := []struct {
		name     string
		a, b     []Point
		want     bool
		reversed bool
	}{
		{"identical forward", base, base, true, false},
		{"identical reversed", rev, base, true, true},
		{
			// ⚠ THE CASE THE OLD ONE-WAY MEASURE GOT WRONG. Every vertex
			// of `a` lands exactly on a vertex of `b`, so a one-way
			// vertex Hausdorff distance is 0.00 m and the old census
			// called this byte-identical. It is not: `b` carries a
			// vertex `a` has never heard of, and referencing one for the
			// other loses ground. Three real strands in one
			// real-data test set are this shape.
			"a subset of b — one-way zero, not equal",
			[]Point{dupPt(54.0, -3.0), dupPt(54.2, -3.2)},
			base, false, false,
		},
		{
			// The other half of the same defect: the extra vertex is on
			// the side being scored, so the one-way distance is nonzero
			// only if you measure in the direction nobody measured.
			"b subset of a", base,
			[]Point{dupPt(54.0, -3.0), dupPt(54.2, -3.2)},
			false, false,
		},
		{
			// ⚠ NO TOLERANCE. A displacement far below any tolerance
			// anyone would be tempted to add — roughly a nanometre —
			// must still read as different. If this row ever goes
			// green, a tolerance has been introduced.
			"one vertex off by 1e-14 degrees",
			[]Point{dupPt(54.0, -3.0), dupPt(54.1+1e-14, -3.1), dupPt(54.2, -3.2)},
			base, false, false,
		},
		{
			"same vertices, different order — not a reversal",
			[]Point{dupPt(54.1, -3.1), dupPt(54.0, -3.0), dupPt(54.2, -3.2)},
			base, false, false,
		},
		{
			"lat and lon swapped — not equal",
			[]Point{dupPt(-3.0, 54.0), dupPt(-3.1, 54.1), dupPt(-3.2, 54.2)},
			base, false, false,
		},
		{
			// A palindrome matches forward first. Reversed must not be
			// reported, or the direction count would be inflated by
			// arcs that need no direction at all.
			"palindrome reports forward, not reversed",
			[]Point{dupPt(54.0, -3.0), dupPt(54.1, -3.1), dupPt(54.0, -3.0)},
			[]Point{dupPt(54.0, -3.0), dupPt(54.1, -3.1), dupPt(54.0, -3.0)},
			true, false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DupVertexSequenceEqual(c.a, c.b)
			if got.Equal != c.want {
				t.Errorf("Equal = %v, want %v", got.Equal, c.want)
			}
			if got.Equal && got.Reversed != c.reversed {
				t.Errorf("Reversed = %v, want %v", got.Reversed, c.reversed)
			}
		})
	}
}

// TestDupVertexSequenceEqual_Elevation pins that elevation is REPORTED and
// never decides. A pair equal in lat/lon and differing in elevation is still
// Equal — that is the substitution the caller must rule on, and burying it in the
// verdict would answer the question by accident.
func TestDupVertexSequenceEqual_Elevation(t *testing.T) {
	a := []Point{dupPtEle(54.0, -3.0, 100), dupPtEle(54.1, -3.1, 200)}
	same := []Point{dupPtEle(54.0, -3.0, 100), dupPtEle(54.1, -3.1, 200)}
	diff := []Point{dupPtEle(54.0, -3.0, 100), dupPtEle(54.1, -3.1, 999)}
	none := []Point{dupPt(54.0, -3.0), dupPt(54.1, -3.1)}

	if v := DupVertexSequenceEqual(a, same); !v.Equal || !v.EleEqual {
		t.Errorf("matching elevations: %+v, want Equal and EleEqual", v)
	}
	if v := DupVertexSequenceEqual(a, diff); !v.Equal || v.EleEqual {
		t.Errorf("differing elevations: %+v, want Equal without EleEqual — elevation must not decide the match", v)
	}
	if v := DupVertexSequenceEqual(a, none); !v.Equal || v.EleEqual {
		t.Errorf("one side without elevation: %+v, want Equal without EleEqual", v)
	}
	// Reversed matches must compare elevation in the direction the match
	// held, not positionally.
	revSame := []Point{dupPtEle(54.1, -3.1, 200), dupPtEle(54.0, -3.0, 100)}
	if v := DupVertexSequenceEqual(a, revSame); !v.Equal || !v.Reversed || !v.EleEqual {
		t.Errorf("reversed with matching elevations: %+v, want Equal, Reversed and EleEqual", v)
	}
	revDiff := []Point{dupPtEle(54.1, -3.1, 100), dupPtEle(54.0, -3.0, 200)}
	if v := DupVertexSequenceEqual(a, revDiff); !v.Equal || !v.Reversed || v.EleEqual {
		t.Errorf("reversed with elevations that only match positionally: %+v, want Equal, Reversed, NOT EleEqual", v)
	}

	// ⚠ NaN IS ABSENT. Two pips that both lack elevation must compare EQUAL,
	// or data with no elevation anywhere reports "0 of 10 match" — which
	// is what this predicate said about a real test set before this case
	// existed, and it would have sent the caller to rule on a substitution that is
	// not happening.
	nan := []Point{dupPtEle(54.0, -3.0, math.NaN()), dupPtEle(54.1, -3.1, math.NaN())}
	nan2 := []Point{dupPtEle(54.0, -3.0, math.NaN()), dupPtEle(54.1, -3.1, math.NaN())}
	if v := DupVertexSequenceEqual(nan, nan2); !v.Equal || !v.EleEqual {
		t.Errorf("both sides NaN elevation: %+v, want Equal and EleEqual — NaN means absent, and absent matches absent", v)
	}
	if v := DupVertexSequenceEqual(nan, a); !v.Equal || v.EleEqual {
		t.Errorf("NaN against a real elevation: %+v, want Equal without EleEqual", v)
	}
	if v := DupVertexSequenceEqual(nan, none); !v.Equal || !v.EleEqual {
		t.Errorf("NaN against nil: %+v, want Equal and EleEqual — both are absent", v)
	}
}
