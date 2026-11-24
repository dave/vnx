package merge

import (
	"fmt"
	"math"
)

// DropPipsOnTerminalNodes removes interior pips that sit EXACTLY on the node
// this arc terminates at.
//
// ⚠ THIS IS NOT A TOLERANCE AND MUST NOT BECOME ONE. The comparison is float64
// equality, because the only thing that produces such a pip is the endpoint
// snap ASSIGNING the node's own coordinate — the two values are equal bit for
// bit, not merely close. A pip 5 cm from the node is real geometry and a
// matcher verdict this writer has no business overruling; a test pins that.
//
// ⚠ WHY THE INGEST FILTER DOES NOT COVER IT. The coincident-point filter
// removes points that already sat on top of each other, and it runs at
// ingest — BEFORE the endpoint warp. When
// a caller then names an existing node for a terminal, this writer takes that
// NODE's coordinate as the endpoint and keeps points[1:len-1] as interior. A
// source point the snap has just pulled onto the node therefore becomes a
// second vertex at the identical coordinate, created after the only filter that
// would have caught it.
//
// ⚠ WHY IT MATTERS BEYOND TIDINESS. Ride resolution references an existing
// canon arc instead of copying it only when the two vertex sequences are
// EQUAL, so one redundant pip makes an otherwise identical line fail to match
// and a full parallel copy is committed beside canon. Measured on a test area:
// two arcs were the same 5.9 km path at 0.00 m separation, differing by
// exactly one leading pip bitwise equal to the start node. Three of the six
// surviving duplicate pairs had this shape, including the two longest.
//
// Only the runs adjacent to each terminal are removed. An interior pip that
// happens to equal a terminal coordinate elsewhere along the line is a genuine
// self-touch and is left alone.
func DropPipsOnTerminalNodes(interior []Pip, startNode, endNode Node) []Pip {
	onNode := func(p Pip, n Node) bool { return p.Lat == n.Lat && p.Lon == n.Lon }
	lo := 0
	for lo < len(interior) && onNode(interior[lo], startNode) {
		lo++
	}
	hi := len(interior)
	for hi > lo && onNode(interior[hi-1], endNode) {
		hi--
	}
	if lo == 0 && hi == len(interior) {
		return interior
	}
	return interior[lo:hi]
}

// CheckEndpointOwner refuses an endpoint node the canon does not own: an arc
// may not terminate on another owner's node. owner and canon name the two
// owners for the message.
func CheckEndpointOwner(want NodeID, inCanon bool, owner, canon string) error {
	if !inCanon {
		return fmt.Errorf(
			"node %d is owned by %s, not by %s — an arc may not terminate on another owner's node",
			want, owner, canon)
	}
	return nil
}

// ElevOrNaN dereferences a *float64 to a float64, mapping nil to NaN
// — the [ElevDelta] convention for missing elevation.
func ElevOrNaN(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}
