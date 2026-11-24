package merge

// The endpoint warp.
//
// When a leg terminal lands within EndpointWarpRadiusM of an existing canon
// node, translate the terminal exactly onto that node and blend the same
// translation vector inward over a taper zone measured from the leg's first
// (or last) point (see zoneFor's doc for the exact zone formula). This runs
// on the RAW SOURCE, BEFORE the follow.
//
// The ordering is the safety argument, not an implementation detail. A taper
// reaches inward, and the points it reaches are exactly the ones the follow
// may place bit-exactly on canon; warping afterwards would move them back
// off. Running first means the follow has nothing to undo: it runs last, so
// wherever the warped geometry coincides with canon it is still rewritten
// bit-exactly onto canon. The follow leaves terminals verbatim (its point
// comparison is bitwise), so the endpoint the warp placed on a node survives
// to the commit unchanged.
//
// ⚠ Read this before touching applyWarpAtEnd or warpLegEndpoints.
//
// OVERLAPPING ZONES. The taper zone is 5x the displacement, capped at the
// leg's own length (see zoneFor's and widenedZoneFactor's docs), so the two
// zones on a short leg can OVERLAP. If each end wrote final positions, the
// second end would clobber the first's contribution wherever they overlap.
// Erroring would abort a whole import on a genuine few-centimetre leg, and
// scaling the two zones down treats the leg's length as a resource to be
// rationed, when the actual defect is the clobbering itself.
//
// So the ends accumulate. Each end computes a DISPLACEMENT FIELD from the
// unwarped src/cum (unchanged) instead of writing `out` directly, and the
// two fields are SUMMED once, after both ends have run (see
// warpLegEndpoints' own comment for why this makes each terminal land
// bit-exact on its target REGARDLESS of how much the two zones overlap, by
// construction, not by assertion, though the assertion stays too: see
// checkTerminalsExact). A point inside both zones receives the SUM of both
// ends' displacements, the physically correct answer for a point being
// pulled from two directions at once.
//
// ONE METHOD FOR MOVING A NODE. There is one way to move a terminal onto a
// node: the warp with its taper. There is no snap outcome and no decline
// outcome; given a target, applyWarpAtEnd tapers.
//
// A leg shorter than 1.5x its own displacement cannot taper without
// inverting even using its WHOLE length as the zone (warpZoneCanAbsorb's own
// inequality). Such a leg is not special-cased: it goes through the same
// taper, and may fold back on itself. That fold is accepted as an observed
// outcome rather than prevented by pre-processing the leg.

import (
	"context"
	"fmt"
)

// There is deliberately no floor on the zone width (such as a fixed
// fraction of the leg's length). A floor would make a 1 m correction on a
// 13.5 km leg taper over 1,350 m of trail regardless of how small the
// actual move was.
//
// A floor is not needed. The absorb condition only needs
// zone >= 1.5*displacement (warpZoneCanAbsorb's own inequality), and
// widenedZoneFactor*displacement (5x, chosen for smoothness margin, not
// correctness) already satisfies that on its own.
//
// Measured on several test areas: removing a 10% floor cut the unexplained
// growth in total line length from several percent to well under one
// percent, and node counts fell further without it, so it was never buying
// deduplication either; it only inflated the zone.
//
// See warpZoneCanAbsorb's doc for the inequality zoneFor's formula has to
// satisfy:
// zone = min(legLen, widenedZoneFactor*displacement).

// warpLegEndpointsToNodes is the endpoint warp's entry point: it resolves
// each of the leg's terminals to the nearest existing node within
// EndpointWarpRadiusM and applies the warp. A terminal with no node in range
// is left untouched, bitwise, along with its whole taper zone.
//
// Call this BEFORE the follow, on the raw source. See the file comment for
// why the ordering is the safety argument rather than a detail.
//
// Every error aborts the merge and the caller discards its writes. There is
// no fallback to committing the leg unwarped: a failure here means we could
// not determine where the endpoint belongs, and guessing is exactly what
// this step avoids. Note that a taper zone too short to absorb its
// displacement is NOT an error (see warpZoneCanAbsorb).
func warpLegEndpointsToNodes(
	ctx context.Context,
	canon Canon,
	pts []Point,
) ([]Point, WarpReport, error) {
	if len(pts) < 2 {
		return nil, WarpReport{}, fmt.Errorf(
			"endpoint warp: leg has %d points, need at least 2", len(pts))
	}

	// Both terminals are resolved against the canon as it stands BEFORE
	// either is moved. Neither of this leg's own nodes exists yet (they are
	// minted later, when the leg is written, after the geometry is final),
	// so a leg can never warp onto itself. That also means a closed loop
	// merged into an empty canon keeps its two ends apart; closing a loop
	// onto itself is out of scope here.
	start, err := resolveWarpTarget(ctx, canon, pts[0])
	if err != nil {
		return nil, WarpReport{}, err
	}
	end, err := resolveWarpTarget(ctx, canon, pts[len(pts)-1])
	if err != nil {
		return nil, WarpReport{}, err
	}

	// No pre-processing special-cases a leg below the taper floor: every
	// leg goes through the same taper, whatever its length. See
	// warpZoneCanAbsorb's doc for what "below the floor" means: a leg that
	// may fold, which is an accepted outcome. (Collapsing such a leg to its
	// two terminals would manufacture a two-point leg out of a real
	// multi-point one, trip the same-node guard below, and mint a fresh node
	// a few metres from an existing one, breaking the one-node rule.)

	// ⛔ A TWO-POINT LEG CANNOT ADOPT ONE NODE AT BOTH ENDS; trying to would
	// abort the whole import.
	//
	// Both ends adopting one node is ordinarily fine and means the leg is a closed
	// loop off a junction. That is stored by cutting the ring at an interior
	// vertex, which mints one node and changes no geometry. A leg of two points
	// HAS no interior vertex, so there is nothing to cut at, and [furthestVertex]
	// correctly refuses: it will not invent a coordinate the source never had.
	// The refusal would then fail the whole import for one degenerate leg
	// (two points, both within the radius of one node).
	//
	// So one end declines. Not "the end": the FARTHER one, because the warp
	// exists to make a small correction and declining the larger displacement is
	// the one that keeps the leg closest to what the file actually said. Ties go
	// to declining the end, which only needs to be deterministic.
	//
	// ⚠ IT DECLINES THE TARGET, NOT JUST THE JOIN, and that distinction is load-
	// bearing. Dropping the node id alone would leave the point still WARPED onto
	// the node's coordinate and then mint a second node there: two nodes at
	// one lat/lon and an arc of zero length, which is a worse defect than the
	// abort. Declining the target leaves the terminal where the file put it.
	if start != nil && end != nil && start.NodeID == end.NodeID && len(pts) < 3 {
		last := len(pts) - 1
		dStart := HaversineMetres(pts[0].Lat, pts[0].Lon, start.Lat, start.Lon)
		dEnd := HaversineMetres(pts[last].Lat, pts[last].Lon, end.Lat, end.Lon)
		if dStart <= dEnd {
			end = nil
		} else {
			start = nil
		}
	}

	out, rep, err := warpLegEndpoints(pts, start, end)
	if err != nil {
		return nil, WarpReport{}, err
	}
	// The one hook into the real warp path, nil unless a test installs
	// one. `pts` here is the raw leg, unmodified, so an observer sees
	// exactly what warpLegEndpoints itself tapered. Folds are accepted;
	// the observer counts them, it does not assert zero.
	// The node identities travel with the report, so the commit can join what
	// this step chose. Set INDEPENDENTLY of StartWarped/EndWarped: a terminal
	// already sitting exactly on the node needs no displacement and is not
	// "warped", but it must still join.
	//
	// UNCONDITIONAL. If a side had a target, it joins. A terminal that joined
	// a node without landing on it would store the node's coordinates as the
	// arc's start/end while the geometry stays unwarped: an untapered jump.
	// checkTerminalsExact asserts on every call that a non-nil target lands
	// bit-exact, so the property holds by construction and needs no gate
	// here.
	if start != nil {
		id := start.NodeID
		rep.StartNode = &id
	}
	if end != nil {
		id := end.NodeID
		rep.EndNode = &id
	}
	return out, rep, nil
}

// There is deliberately no step that collapses a leg to its two terminals
// when its own length cannot absorb a resolved displacement. Folds are
// accepted (see warpZoneCanAbsorb's doc), so nothing downstream depends on a
// leg being able to absorb its own displacement. Such a collapse would also
// manufacture a two-point leg out of a real multi-point one, tripping the
// two-point same-node guard into declining an adoption it should take and
// minting a node beside an existing one, against the one-node rule.

// warpUnresolvedLegTerminals closes a sequencing gap the endpoint warp
// (above) cannot see: a node that comes to exist WHILE THIS SAME LEG is being
// merged (minted by the align step's cut at one of this leg's own followed-run
// boundaries) did not exist yet when warpLegEndpointsToNodes searched,
// moments earlier, on the raw pre-follow source. The rule: whenever a node
// is created, check whether it is within EndpointWarpRadiusM of another and
// warp onto it if it is.
//
// PLACEMENT IS THE WHOLE POINT. Call this after the boundary warp and before
// the leg's pieces are written: AFTER the follow and the align step's cuts
// have run, so every node the align step just minted for THIS leg is
// visible, and BEFORE the commit, so the leg's own outer two points are
// still a mutable array a taper can reach into.
//
// ⚠ THIS IS NOT THE WRITER, AND THAT DISTINCTION MATTERS. The host's writer
// can only JUMP a terminal onto a newly-found node's raw coordinates: it
// discards the leg's own point in favour of the reused node's position with
// no blend, an untapered jump of up to EndpointWarpRadiusM that breaks the
// rule that a leg terminal never moves without a taper. Doing the lookup
// there was measured to change the committed length of many legs, by over
// 100 m on one track. This function instead reuses the SAME taper primitive
// the endpoint warp uses (warpLegEndpoints), so an adoption here is
// structurally indistinguishable from one the endpoint warp made a moment
// earlier; the only difference is WHEN the node became visible.
//
// ONLY THE SIDE(S) STILL NIL IN `rep` ARE LOOKED UP. A side the endpoint warp
// already resolved is a done, immutable decision (its warp already ran and
// wrote its own bit-exact terminal) and this function never re-touches it.
//
// THE SAME TWO-POINT SAME-NODE CARVE-OUT AS THE ENDPOINT WARP, GENERALISED. A
// 2-point piece cannot store both its ends as one node (the store forbids it;
// there is no interior vertex to bisect at). The endpoint warp's guard only
// compares two values it JUST resolved together; here, one side may already
// be FIXED (from the endpoint warp's earlier resolution) and the other newly
// found. A fixed side can never be the one declined, because it is already a
// committed decision this function has no authority to undo. Only a side
// THIS function newly resolves may be declined, and only when it collides
// with whichever id the OTHER side already carries (fixed or newly found).
func warpUnresolvedLegTerminals(
	ctx context.Context,
	canon Canon,
	pts []Point,
	rep WarpReport,
	multiPiece bool,
) ([]Point, WarpReport, bool, error) {
	st := stateOf(ctx)
	if len(pts) < 2 {
		return nil, rep, false, fmt.Errorf("unresolved terminals: leg has %d point(s), need at least 2", len(pts))
	}
	if rep.StartNode != nil && rep.EndNode != nil {
		return pts, rep, false, nil // both terminals already resolved — nothing for this step to do
	}

	lookup := func(p Point, side string) (*warpTarget, error) {
		target, err := resolveWarpTarget(ctx, canon, p)
		if err != nil {
			return nil, fmt.Errorf("unresolved terminals: %s terminal lookup: %w", side, err)
		}
		if st.hooks.legTerminalGap != nil {
			var id NodeID
			var dist float64
			if target != nil {
				id, dist = target.NodeID, HaversineMetres(p.Lat, p.Lon, target.Lat, target.Lon)
			}
			st.hooks.legTerminalGap(st.leg, side, target != nil, id, dist)
		}
		return target, nil
	}

	var newStart, newEnd *warpTarget
	var err error
	if rep.StartNode == nil {
		if newStart, err = lookup(pts[0], "start"); err != nil {
			return nil, rep, false, err
		}
	}
	if rep.EndNode == nil {
		if newEnd, err = lookup(pts[len(pts)-1], "end"); err != nil {
			return nil, rep, false, err
		}
	}

	// The two-point same-node carve-out: see the doc above for why a FIXED
	// side (already resolved by the endpoint warp) can never be the one declined.
	if len(pts) < 3 {
		effStart, startIsNew := effectiveWarpNodeID(rep.StartNode, newStart)
		effEnd, endIsNew := effectiveWarpNodeID(rep.EndNode, newEnd)
		if effStart != nil && effEnd != nil && *effStart == *effEnd {
			decline := func(side string, id NodeID) {
			}
			switch {
			case startIsNew && endIsNew:
				// Both newly found this call and identical: same tie-break as
				// the endpoint warp's own guard: decline the farther of the two.
				last := len(pts) - 1
				dStart := HaversineMetres(pts[0].Lat, pts[0].Lon, newStart.Lat, newStart.Lon)
				dEnd := HaversineMetres(pts[last].Lat, pts[last].Lon, newEnd.Lat, newEnd.Lon)
				if dStart <= dEnd {
					decline("end", newEnd.NodeID)
					newEnd = nil
				} else {
					decline("start", newStart.NodeID)
					newStart = nil
				}
			case startIsNew:
				// The end side is FIXED (from the endpoint warp or otherwise): start must yield;
				// an already-committed decision cannot be undone by this function.
				decline("start", newStart.NodeID)
				newStart = nil
			case endIsNew:
				decline("end", newEnd.NodeID)
				newEnd = nil
				// default: neither side is new (both already fixed and equal):
				// cannot happen, the endpoint warp's own guard already prevents that state.
			}
		}
	}

	// The multi-piece generalisation of what the single-piece commit already
	// does when a piece's own two ends are close: merge them into one node.
	// That single-piece check requires both ends unresolved ON ONE PIECE; a
	// multi-piece leg's two OUTER ends live on DIFFERENT pieces by
	// construction (an interior seam sits between them), so it can never
	// compare them to each other at all, and nothing else in the pipeline
	// does either.
	//
	// Only reached when NEITHER side found a real, EXISTING third-party
	// node above: that always wins, the same priority the single-piece check
	// gives an upstream resolution. And only for a genuinely multi-piece leg:
	// a single-piece leg's own close-ends case belongs to the single-piece
	// check, and must not be duplicated or raced here.
	if newStart == nil && newEnd == nil && rep.StartNode == nil && rep.EndNode == nil &&
		multiPiece && len(pts) >= 3 {
		last := len(pts) - 1
		chord := HaversineMetres(pts[0].Lat, pts[0].Lon, pts[last].Lat, pts[last].Lon)
		// THE SAME TWO GATES AS THE SINGLE-PIECE CHECK, REPLICATED EXACTLY,
		// NOT LOOSENED. legDepartsRadius asks whether this leg's own source,
		// anywhere, genuinely got away from where it started; skipping it
		// would ALSO merge a straight multi-piece spur whose own two ends
		// merely happen to sit close, which is a separate question this
		// function does not decide.
		if chord <= EndpointWarpRadiusM && legDepartsRadius(pts, 0, last, EndpointWarpRadiusM) {
			mintedID, err := canon.AddNode(ctx, pts[0].Lat, pts[0].Lon, pts[0].Ele)
			if err != nil {
				return nil, rep, false, fmt.Errorf("unresolved terminals: mint the multi-piece leg's own shared terminal node: %w", err)
			}
			// pts[0] IS ALREADY the minted node's own coordinate (copied
			// verbatim above): ZERO displacement there, not a jump, so the
			// start side needs no target/taper at all. Only the far end
			// tapers into it.
			shared := &warpTarget{Lat: pts[0].Lat, Lon: pts[0].Lon, NodeID: mintedID}
			out, blendRep, err := warpLegEndpoints(pts, nil, shared)
			if err != nil {
				return nil, rep, false, fmt.Errorf("unresolved terminals: taper the far end onto the shared terminal: %w", err)
			}
			final := rep
			final.StartNode = &mintedID
			final.EndNode = &mintedID
			final.EndWarped = blendRep.EndWarped
			return out, final, true, nil
		}
	}

	if newStart == nil && newEnd == nil {
		return pts, rep, false, nil
	}

	out, blendRep, err := warpLegEndpoints(pts, newStart, newEnd)
	if err != nil {
		return nil, rep, false, fmt.Errorf("unresolved terminals: %w", err)
	}
	// warpLegEndpoints itself only reports StartWarped/EndWarped. Setting
	// StartNode/EndNode from the TARGET's own id, unconditionally whenever
	// that side had a target, is warpLegEndpointsToNodes's own contract
	// (see its "UNCONDITIONAL" comment), reproduced here rather than read
	// off blendRep, which never carries it.
	final := rep
	if newStart != nil {
		id := newStart.NodeID
		final.StartNode = &id
		final.StartWarped = blendRep.StartWarped
	}
	if newEnd != nil {
		id := newEnd.NodeID
		final.EndNode = &id
		final.EndWarped = blendRep.EndWarped
	}
	return out, final, false, nil
}

func resolveWarpTarget(
	ctx context.Context,
	canon Canon,
	p Point,
) (*warpTarget, error) {
	n, ok, err := nearestNode(
		ctx, canon, p.Lat, p.Lon, EndpointWarpRadiusM, nil)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	// TWO THINGS ARE DELIVERED HERE AND BOTH ARE PART OF THE CONTRACT: the node's
	// coordinates EXACTLY as stored, which make the incoming endpoint bitwise
	// coincident with it, AND the node's IDENTITY, which is what the commit joins.
	//
	// ⚠ Coincidence alone is NOT the whole deliverable. Dropping n.ID would
	// leave the terminal on the node's coordinates without joining it, and
	// the leg would mint a second node at the same place.
	if n.ID <= 0 {
		return nil, fmt.Errorf(
			"warp target: node search returned id %d for the target at (%v,%v) — a terminal cannot join a node with no identity",
			int64(n.ID), n.Lat, n.Lon)
	}
	target := &warpTarget{Lat: n.Lat, Lon: n.Lon, NodeID: n.ID}
	return target, nil
}
