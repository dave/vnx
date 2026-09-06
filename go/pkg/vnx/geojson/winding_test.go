package geojson

import (
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

// Winding is the cell of this writer most likely to be wrong and INVISIBLE
// (spec §3.3): a clockwise ring parses everywhere, draws in most viewers, and
// is a hole rather than an island in anything that honours RFC 7946 §3.1.6.
// Nothing about the file looks wrong.

// ccw and cw are the same square, wound the two ways.
func ccw() []vnx.Position {
	return []vnx.Position{pos(0, 0, nil), pos(1, 0, nil), pos(1, 1, nil), pos(0, 1, nil), pos(0, 0, nil)}
}

func cw() []vnx.Position {
	return []vnx.Position{pos(0, 0, nil), pos(0, 1, nil), pos(1, 1, nil), pos(1, 0, nil), pos(0, 0, nil)}
}

func TestSignedAreaKnowsWhichWayRoundItIs(t *testing.T) {
	if signedArea(ccw()) <= 0 {
		t.Errorf("counterclockwise ring measured %v, wanted positive", signedArea(ccw()))
	}
	if signedArea(cw()) >= 0 {
		t.Errorf("clockwise ring measured %v, wanted negative", signedArea(cw()))
	}
}

// A REVERSED RING IS NORMALISED, and the test is written so that a writer
// which merely copied the input would fail: the two documents must produce the
// same coordinates.
func TestReversedRingIsNormalised(t *testing.T) {
	forward, _ := writeDoc(t, &vnx.Document{Areas: []vnx.Area{{Name: "a", Polygon: ccw()}}})
	reverse, _ := writeDoc(t, &vnx.Document{Areas: []vnx.Area{{Name: "a", Polygon: cw()}}})
	if forward != reverse {
		t.Errorf("a clockwise ring produced different output from the same ring wound counterclockwise;\n"+
			"RFC 7946 §3.1.6 requires the right-hand rule, so both must come out counterclockwise.\n"+
			"ccw: %s\ncw:  %s", forward, reverse)
	}
	if !strings.Contains(forward, `[[0,0],[1,0],[1,1],[0,1],[0,0]]`) {
		t.Errorf("wanted the counterclockwise ring, got %s", forward)
	}
}

// The same rule for an in-network area's own Feature. A polygon that reaches
// the file by a second path is a second chance to get the winding wrong.
func TestReversedRingInsideANetworkIsNormalised(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Networks: []vnx.Network{{
		Name:  "n",
		Areas: []vnx.Area{{Name: "boggy", Polygon: cw()}},
	}}})
	if !strings.Contains(out, `[[0,0],[1,0],[1,1],[0,1],[0,0]]`) {
		t.Errorf("an in-network area kept its clockwise winding: %s", out)
	}
}

// A degenerate ring has no winding to correct. Reversing it or refusing it
// would both be inventing an answer to a question the geometry does not ask.
func TestDegenerateRingIsLeftAlone(t *testing.T) {
	flat := []vnx.Position{pos(0, 0, nil), pos(1, 1, nil), pos(2, 2, nil), pos(0, 0, nil)}
	ring, ok := ringFor(flat)
	if !ok {
		t.Fatal("a closed four-position ring is a linear ring even with zero area")
	}
	for i := range flat {
		if ring[i] != flat[i] {
			t.Fatalf("a zero-area ring was reordered: %v", ring)
		}
	}
}
