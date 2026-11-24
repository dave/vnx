package merge

import ()

// smoothstepMaxSlope is max|f'(t)| for f(t) = 3t^2 - 2t^3, which is 1.5 at
// t = 0.5 (f'(t) = 6t(1-t)).
//
// NOT A BAND — it is a property of the blend shape. The displacement
// profile along the leg is D(s) = v*w(s), so |dD/ds| <= smoothstepMaxSlope *
// d / zone. Consecutive vertices can therefore swap order — the very
// inversion the blend exists to prevent — as soon as that bound exceeds 1,
// i.e. whenever zone < 1.5 * d. warpZoneCanAbsorb tests exactly that
// inequality, and it is what a capped zone can fail: see widenedZoneFactor's
// doc for why 1.5*d is a FLOOR the zone can fall below (on a genuinely
// too-short leg) rather than a value it is chosen to hit.
//
// This constant does not size the zone. Sizing the zone to exactly 1.5*d
// would sit on the boundary of correctness: the forward-progress factor at
// zone=1.5d is EXACTLY zero, which makes the taper visibly abrupt.
// widenedZoneFactor (5.0) sizes the zone instead, deliberately well clear of
// that boundary (at zone=5d the factor is 1-1.5/5=0.7, comfortably smooth).
// This constant's job is purely diagnostic: it is what warpZoneCanAbsorb
// checks to prove whatever zone zoneFor computed (flat, or widened-and-capped)
// is actually safe — it does not SET anything.
//
// Getting this wrong is easy: testing zone >= displacement is linear-ramp
// reasoning (max slope 1.0) applied to a curve that is not a linear ramp. It
// admits the whole band d <= zone < 1.5d — e.g. a 60 m leg with a terminal
// 5 m from a node gives zone = 6 and a local stretch factor of 1.25.
const smoothstepMaxSlope = 1.5

// adoptedTerminalCount reports how many of a leg's two ends WarpReport
// resolved as a JOIN rather than a mint. It is the producer of the import's
// adopted-terminals counter, counted here — the same call site
// StartNode/EndNode are set at — because this is the one place that decides
// mint vs adopt for a leg's own terminals; nothing downstream may re-derive
// it. A track ending on an existing network node SHARES that node.
func adoptedTerminalCount(rep WarpReport, ringMergedByGap bool) int {
	// A GAP MERGE ADOPTS NOTHING — it MINTS.
	//
	// An adopted terminal means one thing: this terminal slot joined a node
	// that already existed, so it did not mint one. The node accounting
	// subtracts it for exactly that reason.
	//
	// The caller's self-merge branch is reached ONLY when neither end
	// resolved to any existing node, and it then creates a brand-new node and
	// points BOTH ends at it. Both ends therefore come back non-nil while
	// nothing was adopted at all, and counting them says this leg avoided two
	// mints when it in fact performed one.
	//
	// The ring-merge correction already carries this case's whole
	// adjustment: two terminal SLOTS resolving to ONE minted node is "one
	// fewer than two per source leg", i.e. −1. Counting the ends as adopted
	// as well applies −2 on top, so the leg is corrected by −3 where the truth
	// is −1 — a residual of +2 per occurrence, which silently corrupts the
	// node accounting.
	//
	// ⚠ That correction assumes the warp report never marks a gap-merged
	// slot as adopted. The self-merge runs BEFORE this count and DOES mark
	// both slots, so without this guard the same correction would land twice.
	//
	// The flag is a REQUIRED parameter rather than a field read off rep,
	// because rep cannot express the difference: a gap-merged rep and a
	// genuinely-twice-adopted rep are bit-identical. Making it a parameter
	// also means a new call site cannot forget to answer the question.
	if ringMergedByGap {
		return 0
	}
	n := 0
	if rep.StartNode != nil {
		n++
	}
	if rep.EndNode != nil {
		n++
	}
	return n
}

// effectiveWarpNodeID returns whichever node id a side carries — the FIXED
// one from an earlier resolution if set, otherwise a newly-found target's
// id — and whether that id came from THIS call's own new lookup (true) or
// was already fixed beforehand (false). Neither set returns (nil, false).
func effectiveWarpNodeID(fixed *NodeID, newTarget *warpTarget) (*NodeID, bool) {
	if fixed != nil {
		return fixed, false
	}
	if newTarget != nil {
		id := newTarget.NodeID
		return &id, true
	}
	return nil, false
}

// warpZoneCanAbsorb reports whether a taper zone of `zone` metres can absorb a
// `displacement` metre translation without the blend locally stretching the leg
// by more than it is long — which is what would let consecutive vertices swap
// order.
//
// The warp itself does not consult it: there is one method for moving a
// node, and it always tapers, whatever the answer would be. A leg below the
// floor simply folds, and such folds are accepted. It is kept as the named
// statement of the no-fold inequality that the warp code and its tests cite.
//
// Written as `>=` on the product rather than `<` on a ratio so that a NaN
// displacement returns false rather than sneaking through — every comparison
// against NaN is false, so a guard phrased the other way round would admit it.
func warpZoneCanAbsorb(zone, displacement float64) bool {
	return smoothstepMaxSlope*displacement <= zone
}
