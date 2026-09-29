package geojson

import (
	"fmt"

	"github.com/dave/vnx/go/pkg/vnx"
)

// ringFor validates an area's ring, closes it, and returns it wound the way
// RFC 7946 requires. ok is false when the ring cannot be a Polygon at all.
//
// ⚠ RFC 7946 §3.1.6 REQUIRES THE RIGHT-HAND RULE: an exterior ring runs
// COUNTERCLOCKWISE. Our areas carry no winding guarantee — the model stores
// whatever the user drew or an import produced — so a reversed ring emitted
// verbatim is a file that parses everywhere and is a hole rather than an
// island in anything that honours the rule. It is the cell of this writer most
// likely to be wrong and invisible, which is why there is a test with a
// deliberately reversed ring.
//
// ⚠ VNX STORES A RING OPEN AND GEOJSON WRITES IT CLOSED (SPEC §3.7, RFC 7946
// §3.1.6). Closing it here is the format boundary, not a repair: the boundary
// runs from the last position back to the first in both, and GeoJSON spells
// that edge by repeating the first position. A ring that arrives ALREADY
// closed is refused rather than closed twice — it cannot come from a file,
// because the reader drops the repeat, so only a hand-built Document reaches
// here with one, and doubling its closing position would be a fallback hiding
// that.
func ringFor(ring []vnx.Position) ([]vnx.Position, bool) {
	if len(ring) < 3 || closed(ring) {
		return nil, false
	}
	out := make([]vnx.Position, 0, len(ring)+1)
	out = append(append(out, ring...), ring[0])
	if signedArea(out) < 0 {
		return reversed(out), true
	}
	// ⚠ A ZERO SIGNED AREA IS LEFT ALONE. A degenerate ring — every position
	// collinear, or the same point four times — has no winding to correct, and
	// reversing it or refusing it would both be inventing an answer to a
	// question the geometry does not ask. It is emitted as it stands; RFC 7946
	// asks for four closed positions, not for area.
	return out, true
}

func closed(ring []vnx.Position) bool {
	n := len(ring)
	return n >= 2 && ring[0].Lon == ring[n-1].Lon && ring[0].Lat == ring[n-1].Lat
}

// ringProblem is the Message for a ring that ringFor refused. It says WHICH of
// the two rules failed: "no geometry" alone would send a reader looking for a
// missing polygon when the polygon is there and closed.
func ringProblem(ring []vnx.Position) string {
	if len(ring) < 3 {
		return fmt.Sprintf("polygon has %d position(s); a ring needs at least 3", len(ring))
	}
	return "polygon is closed: a VNX ring is stored open, and its last position must not repeat the first"
}

// signedArea is the shoelace sum over lon/lat. Positive is counterclockwise —
// the right-hand rule — and negative is clockwise.
//
// ⚠ IT IS COMPUTED IN DEGREES, NOT ON THE SPHERE, and that is enough for what
// it is asked. The question is a SIGN, and a projection that preserves
// orientation preserves the sign; the magnitude is never used. Doing this
// properly on an ellipsoid would buy a number nothing reads.
//
// ⚠ IT IS COMPUTED ON THE ROUNDED COORDINATES, WHICH ARE THE ONES WRITTEN.
// This is the ONE place rounding has to come before a geometry decision, and
// the reason is that the decision is ABOUT the bytes: RFC 7946 §3.1.6 constrains
// the winding of the ring in the FILE, so a sign taken from coordinates the file
// does not contain answers the wrong question. It matters below about a metre —
// snapping to an 11 cm grid can move a vertex from one side of a near-collinear
// edge to the other, and a sliver that is counterclockwise at full precision is
// then written clockwise. The positions themselves are NOT rounded here; only
// the sign is, and rounding is idempotent, so emit's own rounding writes the
// same values either way.
func signedArea(ring []vnx.Position) float64 {
	var sum float64
	for i := 0; i < len(ring)-1; i++ {
		a, b := vnx.RoundPosition(ring[i]), vnx.RoundPosition(ring[i+1])
		sum += a.Lon*b.Lat - b.Lon*a.Lat
	}
	return sum
}

func reversed(ps []vnx.Position) []vnx.Position {
	out := make([]vnx.Position, len(ps))
	for i := range ps {
		out[i] = ps[len(ps)-1-i]
	}
	return out
}
