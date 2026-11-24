package merge

import (
	"fmt"
)

// TjunctionDecline names why the T-junction split left a terminal alone. Every value except
// TjNoDecline means committed geometry was NOT touched.
type TjunctionDecline int

const (
	TjNoDecline TjunctionDecline = iota

	// A node is already within EndpointWarpRadiusM — the endpoint warp has this terminal, and
	// splitting would mint a second node beside an existing one for nothing.
	TjDeclineNodeInRadius

	// No canon arc passes within EndpointWarpRadiusM. Nothing to join.
	TjDeclineNoCanonInRadius

	// The closest approach is a walk TERMINAL vertex (fraction exactly 0 or 1),
	// so there is no interior to split.
	TjDeclineTerminalVertex

	// A node is already within EndpointWarpRadiusM of the PROJECTION — the
	// point the split would actually mint at, which is a different place from
	// the terminal TjDeclineNodeInRadius asks about. See the check itself.
	TjDeclineNodeNearProjection
)

func (d TjunctionDecline) String() string {
	switch d {
	case TjNoDecline:
		return "split"
	case TjDeclineNodeInRadius:
		return "a node is already within the warp radius"
	case TjDeclineNoCanonInRadius:
		return "no canon arc within the warp radius"
	case TjDeclineTerminalVertex:
		return "the closest approach is a walk terminal vertex"
	case TjDeclineNodeNearProjection:
		return "a node is already within the warp radius of the projection"
	}
	return fmt.Sprintf("tjunctionDecline(%d)", int(d))
}

// TjunctionTerminal is what the T-junction split did at ONE leg terminal.
type TjunctionTerminal struct {
	Split   bool
	Decline TjunctionDecline

	// The candidate considered. Zero when none was found (TjDeclineNodeInRadius
	// or TjDeclineNoCanonInRadius), since those two decline before the search
	// or on its empty answer.
	ArcID     ArcID
	ProjDistM float64 // terminal → projection
	StubLenM  float64 // the shorter of the two halves a split would produce

	// Set only when Split. MintedDistM is terminal → the node ACTUALLY minted,
	// which is not quite the projection (see the verify step).
	NodeID      NodeID
	MintedDistM float64

	// The two halves the split wrote in place of ArcID, in the parent's own
	// start→end walk order. Set only when Split.
	//
	// The one field here with a PRODUCTION consumer: the caller's leg→arcs
	// mapping must re-point the leg whose geometry this split just cut, and the
	// parent id it recorded no longer names an arc. Everything else on this
	// struct is for tests and diagnostics only.
	PriorHalfID ArcID
	NextHalfID  ArcID

	// ConsumedParentVertex reports whether the split consumed an existing
	// vertex of the parent arc (the split point sat exactly on one).
	// A production consumer like the two ids above: it is recorded on the seam so
	// anything reconstructing the parent knows whether undoing this cut must keep
	// the join or drop it. Set only when Split.
	ConsumedParentVertex bool
}

// TjunctionReport records what the T-junction split did to a leg, first
// terminal then last. It exists so TESTS can observe the outcome — which
// decline fired, how far the projection was, how short the stub would have
// been — directly instead of inferring it from geometry.
//
// Production reads ONE thing from it: the ids of the two halves a split
// wrote, which the caller's leg→arcs mapping needs because the split deleted
// the parent id that mapping recorded. No other field has a production
// consumer.
//
// It mirrors the endpoint warp's WarpReport, including its rule: no count
// here gets a production consumer or reaches the UI unless something actually
// needs it.
type TjunctionReport struct {
	Start TjunctionTerminal
	End   TjunctionTerminal
}

// Cuts is how many nodes this leg's T-junction split actually put into canon:
// 0, 1 or 2.
//
// It reads the Split bools rather than adding a counter to the struct, which
// keeps the rule above — and it reads the same field the caller's leg→arcs
// mapping iterates, so the two records of one cut cannot disagree about
// whether it happened. Its consumer is the import's T-junction cut counter.
func (r TjunctionReport) Cuts() int {
	n := 0
	for _, term := range [2]TjunctionTerminal{r.Start, r.End} {
		if term.Split {
			n++
		}
	}
	return n
}

// tjunctionDeclineFor applies the two decline rules to a candidate that is
// already known to be within the radius. Pure, so every rule can be pinned
// directly.
//
// Order matters only for what gets REPORTED, never for the outcome: a candidate
// failing both rules is declined either way.
func tjunctionDeclineFor(cand NearbyCanonArc) TjunctionDecline {
	// 3. A fraction of exactly 0 or 1 means the closest approach is a walk
	//    TERMINAL vertex — there is no interior there to split, and
	//    ValidateSnapFraction rejects both, so attempting it would abort the
	//    user's whole import with an error about an internal precondition.
	//    A DECLINE, not an error: nothing about the input is wrong.
	if cand.Fraction == 0 || cand.Fraction == 1 {
		return TjDeclineTerminalVertex
	}

	// There is no "would the endpoint warp move this terminal at all?" check.
	// The warp cannot decline: there is one method for moving a node, and
	// every terminal it resolves a target for reaches it. A node this split
	// mints is always within EndpointWarpRadiusM of the terminal (the split
	// verifies it), so the warp's target search will always find it, and it
	// will always be used. A split can therefore never leave a spurious
	// mid-trail node that the warp never reaches.

	return TjNoDecline
}

// LegMeasureLengthM is the leg's cumulative haversine measure length — the same
// quantity, summed in the same order with the same primitive, that
// WarpLegEndpoints derives its taper zone from. Both must see the same number,
// or the T-junction split and the endpoint warp would disagree about the
// leg's length.
func LegMeasureLengthM(pts []Point) float64 {
	total := 0.0
	for k := 1; k < len(pts); k++ {
		total += HaversineMetres(
			pts[k-1].Lat, pts[k-1].Lon, pts[k].Lat, pts[k].Lon)
	}
	return total
}
