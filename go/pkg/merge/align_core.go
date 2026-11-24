package merge

import (
	"fmt"
)

// alignDecline names why a run boundary produced no cut. Every value except
// AlignCutMade means committed geometry was NOT touched.
type alignDecline int

const (
	AlignCutMade alignDecline = iota

	// The boundary is at (or within MeasureEps of) the canon arc's own start or
	// end node. The node the align step wants is already there — the whole point of the pass
	// is satisfied, so there is nothing to do.
	AlignDeclineAtNode

	// The boundary is interior to its parent but within EndpointWarpRadiusM of
	// one of its ENDS, so it is ABSORBED onto that end's node instead of cutting.
	// Node names the node absorbed to; AbsorbedM is how far the boundary moved.
	AlignDeclineAbsorbedToNode

	// The boundary is EndpointWarpRadiusM..MinRunM (15–25 m) from its parent's
	// nearest end — too far to absorb (that would displace the join beyond the
	// 15 m budget), but this band is NOT a length judgement about the
	// stub the way MinRunM's own name suggests. It exists because
	// EndpointWarpRadiusM and the follow's StayM are the SAME number (15 m): an
	// entered run stays classified ON until it has drifted StayM past where the
	// leg actually left its candidate, so this band is where the matcher
	// NOTICED the leg leave, not where it left. Cutting there mints a node at
	// the noticing point; the next import's matcher then locks onto that
	// stub and mints ANOTHER one at ITS noticing point, which is the align-cut
	// non-idempotence this decline exists to prevent. No node is named — see
	// [AlignBoundary.seamNode] — so the leg's own piece simply continues
	// through the boundary, same as any other declined cut.
	AlignDeclineStayBandStub

	// The boundary is INTERIOR to its own parent (AlignDeclineAtNode does not
	// apply — this is not the parent's own start or end) but some OTHER node,
	// already in the network, sits within EndpointWarpRadiusM (0 m in this
	// specific case — see AlignDeclineAbsorbedToNode for the >0 arm of the
	// same search). Minting here would put a second node 0.000 m from the
	// first, which terminal adoption cannot catch because neither end of THIS
	// cut is a leg terminal. The one-node rule covers this and the absorb with
	// a single nearby-node search.
	AlignDeclineAtExistingNode
)

func (d alignDecline) String() string {
	switch d {
	case AlignCutMade:
		return "cut"
	case AlignDeclineAtNode:
		return "the boundary is already at a node"
	case AlignDeclineAbsorbedToNode:
		return "absorbed onto the adjacent node"
	case AlignDeclineStayBandStub:
		return "a piece would be shorter than MinRun (StayM hysteresis band)"
	case AlignDeclineAtExistingNode:
		return "an existing node already sits at this exact coordinate"
	}
	return fmt.Sprintf("alignDecline(%d)", int(d))
}

// AlignBoundary is what the align step did at ONE measure boundary of one ON run.
//
// Parent is the arc the boundary was resolved against AT THE MOMENT OF THE
// ATTEMPT, which for any boundary after the first cut on the same canon arc
// is a half a previous cut minted — NOT the id the run named. MeasureM is the measure
// along that parent. Both are recorded rather than derived because a reader
// working back from the finished network cannot recover either.
//
// ⚠ Run/AtRunEnd EXIST FOR THE LEG CUT, AND FLOATS ARE NOT AN ADEQUATE KEY. The leg cut has to ask
// "which node did the boundary at the start of run 3 resolve to". Answering it by
// matching MeasureM back against the run's MeasureStart/MeasureEnd would be a float equality
// on a quantity that has been through a split's measure→fraction→measure round trip, and
// it would tie for a canon arc two runs both bound at the same measure. The
// identity is recorded at the point it is known instead.
type AlignBoundary struct {
	RunCanonID ArcID   // the id the run named, before any of this leg's cuts
	Parent     ArcID   // the arc actually cut or declined against
	Run        int     // index into the leg's runs
	AtRunEnd   bool    // false = this is the run's MeasureStart end, true = its MeasureEnd end
	MeasureM   float64 // measure along Parent
	TotalM     float64 // Parent's whole measure length
	Decline    alignDecline

	// Node is the canon node AT this boundary — the one a cut minted, the one
	// already there when the boundary declined at an end, or the one it was
	// ABSORBED onto. EVERY outcome names a node now; read it through
	// [AlignBoundary.seamNode], which still returns the bool so a future
	// nodeless outcome cannot be threaded silently.
	Node NodeID

	// AbsorbedM is how far the boundary moved to reach Node — a straight-line
	// (chord) distance now, since Node comes from a spatial search
	// (the caller's node search), not an along-canon measure.
	// Set only for AlignDeclineAbsorbedToNode, and bounded by
	// EndpointWarpRadiusM. It is the displacement the leg cut's own leg-warp
	// applies to the leg, so it is the quantity to measure when
	// asking whether absorbing moved geometry it should not have.
	AbsorbedM float64

	// NodeLat/NodeLon are Node's OWN coordinates, exactly as the node search
	// returned them (or the parent's end-node lookup, for a boundary already at
	// its parent's own end) — never recomputed from Node afterwards, so the leg-warp
	// target the leg cut builds from this is bit-identical to what the node search itself
	// found. Set whenever Node is (every non-declined outcome); meaningless
	// otherwise, same as AbsorbedM.
	NodeLat, NodeLon float64

	// Lat/Lon is the boundary's OWN coordinate on its parent — the point the run
	// actually reached, before any decline moved the answer to a node some
	// distance away. Recorded here because it is known exactly once (the
	// at-existing-node check computes it) and is NOT recoverable afterwards: a
	// later cut in this same pass deletes the parent arc the measure was taken
	// on, and a decline records only the node it chose, not the place it was
	// asked about. Re-resolving a boundary needs the place, not the answer.
	//
	// Zero only for the two early returns above it (the parent arc vanished, or
	// the boundary was already at one of its parent's own end nodes), neither of
	// which is re-resolved.
	Lat, Lon float64

	// Set only when Decline == AlignCutMade.
	Prior ArcID
	Next  ArcID

	// ConsumedParentVertex reports whether the split consumed an existing
	// vertex of the parent arc (the split point sat exactly on one).
	// Set only when Decline == AlignCutMade.
	ConsumedParentVertex bool

	// LineageCutConnectorPairs is (mintedNodeID, nearbyNodeID) for every node
	// left inside EndpointWarpRadiusM of the node THIS boundary's cut minted,
	// and it is set only for one outcome: the absorb search found a node
	// inside the radius, refused it because it is not on the ridden lineage,
	// and cut instead. The caller collects these into the import's result.
	//
	// Empty for every other outcome, including an ordinary cut with nothing
	// nearby — a cut that had nothing to refuse has nothing to record.
	LineageCutConnectorPairs [][2]NodeID
}

// seamNode reports the canon node this boundary sits on, and whether there is
// one.
//
// ⚠ SINCE THE ABSORB, EVERY OUTCOME NAMES A NODE — cut, at-a-node, at an
// existing node, or absorbed onto an adjacent one — so the false arm is
// currently unreachable. It stays because the bool is what makes a nodeless
// outcome IMPOSSIBLE TO THREAD rather than merely unlikely: NodeID's zero value
// is a plausible-looking id, and a caller reading .Node directly would write
// node 0 into an arc. A future decline that cannot name a node gets the
// false arm for free, and the leg cut drops that seam instead of minting a
// node nobody shares.
func (b AlignBoundary) SeamNode() (NodeID, bool) {
	if b.Node == 0 {
		return 0, false
	}
	return b.Node, true
}

// AlignReport is what the align step did for ONE leg.
//
// Cuts is the count the node accounting reconciles against and the leg cut
// consumes: the number of nodes the align step actually put into canon for
// this leg. It is NOT the number of ON-run boundaries — most boundaries
// decline, and the first leg across a junction mints the node every later leg
// then lands on, so a count of boundaries over unsplit canon is an upper bound
// rather than a prediction.
type AlignReport struct {
	Boundaries []AlignBoundary
	Cuts       int

	// LineageCutConnectorPairs is every boundary's own
	// [AlignBoundary.LineageCutConnectorPairs], concatenated in cut order —
	// the leg's whole record, ready to be folded into the import's result by
	// the caller.
	LineageCutConnectorPairs [][2]NodeID

	// Lineages has one entry per ORIGINAL canon arc this leg's runs named, in
	// the same ascending-id order the cutting loop used. See [AlignLineage].
	Lineages []AlignLineage
}

// lineageFor returns the cut tree of the arc a run NAMED. The bool is false
// only for an arc no ON run of this leg bound, which is a caller error rather
// than a data condition — every ON run contributes its CanonID to the map the
// cutting loop iterates.
func (r AlignReport) lineageFor(canon ArcID) (AlignLineage, bool) {
	for _, l := range r.Lineages {
		if l.Canon == canon {
			return l, true
		}
	}
	return AlignLineage{}, false
}

// AlignLeaf is ONE arc that covers part of an original canon arc once this
// leg's align step has finished cutting it. LoM/HiM are measures along the
// ORIGINAL arc (the id the run named), which is a coordinate system every leaf
// shares because the align step cuts in DESCENDING measure order and therefore only ever
// hands the next boundary a PRIOR half, whose start node is the original's.
//
// LoNode/HiNode are the nodes at those two measures: the original arc's own
// end nodes at 0 and TotalM, and the node a cut minted at each interior
// division. They are RECORDED HERE, at the one moment they are known, for the
// same reason [AlignBoundary.Node] is: a reader working back from the finished
// network cannot tell which of a node's incident arcs came from which cut, and
// [CanonArc] carries vertices and no node ids at all.
type AlignLeaf struct {
	Arc            ArcID
	LoM, HiM       float64
	LoNode, HiNode NodeID
	// The nodes' OWN coordinates, as the mint or the stored node gave them —
	// never recomputed from the geometry afterwards, the same rule
	// [AlignBoundary.NodeLat] states. The absorb search measures against these to
	// decide whether a nearby node is on the stretch the run actually rode.
	LoLat, LoLon float64
	HiLat, HiLon float64
}

// AlignLineage is the cut tree of ONE original canon arc, flattened to the
// leaves that survive, in ascending measure order and sharing a node at every
// division.
//
// ⚠ THIS IS WHAT RIDE RESOLUTION RESOLVES A RIDE AGAINST, AND IT IS THE REASON
// READING ONE BOUNDARY'S Prior/Parent IS WRONG. The align step cuts descending,
// so every later, lower cut DELETES the arc an earlier boundary recorded — on
// one test area that named a deleted arc for 14 of 21 ON pieces. The lineage
// names only arcs that are still there when the pass ends.
type AlignLineage struct {
	Canon  ArcID
	TotalM float64
	Leaves []AlignLeaf
}

// CanonMeasureLengthM is the cumulative great-circle length of a canon vertex list.
//
// ⚠ IT MUST AGREE WITH TWO OTHER SUMS, and it does so by being the same
// arithmetic over the same vertices with the same primitive:
//
//   - [PrepCandidates], which is where the run's measures came
//     from — same vertex list, same [HaversineMetres], same order;
//   - the store's polyline split, which is what
//     turns the fraction back into a position — same walk
//     ([start node, pips…, end node]), and HaversineMetres delegates to
//     the unexported haversineMetres that function uses, so there is one
//     implementation and one Earth radius.
//
// The round trip is therefore measure → measure/total → fraction*total → measure, exact up
// to float rounding. It is not exact to the bit, which is why the caller
// qualifies with MeasureEps rather than testing for equality.
func CanonMeasureLengthM(verts []Point) float64 {
	total := 0.0
	for i := 1; i < len(verts); i++ {
		total += HaversineMetres(verts[i-1].Lat, verts[i-1].Lon, verts[i].Lat, verts[i].Lon)
	}
	return total
}

// InterpolateMeasurePointM returns the lat/lon at `measure` metres along verts,
// walked in order — the SAME walk CanonMeasureLengthM sums and the SAME linear
// lat/lon interpolation the polyline split performs when it
// actually cuts (a measure-derived parameter t, then start + t*(end-start) on
// whichever pair of vertices brackets it). It exists to answer the
// at-existing-node check's question — "where would this cut land" — WITHOUT
// performing the cut, since asking after minting would mean writing the very
// duplicate node this decline exists to avoid.
//
// Separate from the split itself: that also builds the two interior pip
// slices a real split needs, which this caller has no use for and no
// business depending on the shape of.
func InterpolateMeasurePointM(verts []Point, measure float64) (lat, lon float64) {
	cum := make([]float64, len(verts))
	for i := 1; i < len(verts); i++ {
		cum[i] = cum[i-1] + HaversineMetres(verts[i-1].Lat, verts[i-1].Lon, verts[i].Lat, verts[i].Lon)
	}
	i := 1
	for i < len(verts)-1 && cum[i] < measure {
		i++
	}
	a, b := verts[i-1], verts[i]
	segLen := cum[i] - cum[i-1]
	t := 0.0
	if segLen > 0 {
		t = (measure - cum[i-1]) / segLen
	}
	return a.Lat + t*(b.Lat-a.Lat), a.Lon + t*(b.Lon-a.Lon)
}

// NearestLineageNode returns the node of `lineage` closest to (lat,lon) within
// EndpointWarpRadiusM, and whether there is one.
//
// ⚠ THE CANDIDATES ARE THE TWO NODES ADJACENT TO THE BOUNDARY'S OWN MEASURE,
// NOT EVERY NODE ON THE LINEAGE, AND THE
// DIFFERENCE IS A HIGHLIGHT THAT WALKS GROUND THE GPS NEVER COVERED. "Somewhere on
// the ridden lineage" is not enough: canon that passes within
// EndpointWarpRadiusM of its OWN EARLIER NODE lets a run's boundary absorb
// backwards onto that node, and the ride then resolves to every leaf between
// the two — the whole loop instead of the stretch actually ridden. In one
// measured case a 105 m piece resolved to a 2,081 m highlight, and its leg's
// committed walk went from 5,097 m to 9,042 m, traversing a 2 km
// out-and-back twice.
//
// So the candidate set is the ends of the leaf that CONTAINS this boundary's
// measure — the node the run is walking towards and the node it is walking
// away from — which is node identity plus measure ORDER, and introduces no
// tolerance: "15 m, but only near the boundary's OWN PARENT arc's own end",
// not anywhere in the network.
//
// The metric is the same haversine chord the caller's node search
// picks nearest by, and the same radius; only the candidates differ.
//
// ⚠ A ZERO NODE ID IS A BROKEN LINEAGE, NOT AN EMPTY SLOT. Node ids come from
// the store and start at 1, so 0 is the Go zero value of a field
// that was never filled — an AlignLeaf whose end never resolved. Skipping it
// silently loses a candidate the caller believes it
// searched: the boundary then mints a node where an existing one was supposed
// to be found, which is exactly the duplicate node the one-node rule
// exists to prevent. Error, not fallback.
func NearestLineageNode(l AlignLineage, measure, lat, lon float64) (Node, float64, bool, error) {
	best, bestD, found := Node{}, 0.0, false
	var bad error
	consider := func(id NodeID, nlat, nlon float64) {
		if id == 0 {
			if bad == nil {
				bad = fmt.Errorf("lineage of canon arc %d carries a leaf end with node id 0 at (%.7f,%.7f) — node ids start at 1, so this end was never resolved and the existing-node search cannot say whether a node is already there",
					l.Canon, nlat, nlon)
			}
			return
		}
		d := HaversineMetres(lat, lon, nlat, nlon)
		if d > EndpointWarpRadiusM {
			return
		}
		if !found || d < bestD || (d == bestD && id < best.ID) {
			best, bestD, found = Node{ID: id, Lat: nlat, Lon: nlon}, d, true
		}
	}
	for _, leaf := range l.Leaves {
		// The leaf holding this measure. Half-open at the top so a measure
		// sitting exactly on a division belongs to one leaf, not two — and the
		// division's own node is an end of that leaf either way.
		if measure < leaf.LoM || (measure > leaf.HiM && leaf.HiM != l.TotalM) {
			continue
		}
		// ⚠ AND WITHIN THE RADIUS ALONG CANON, NOT ONLY AS THE CROW FLIES. The
		// adjacency rule alone is not enough, because a leaf can BE the whole
		// arc: canon that switches back brings its own end node within 15 m of a
		// point 100 m along itself, so a boundary there absorbs onto that end,
		// the ride resolves to the WHOLE arc, and a 102.6 m piece references a
		// 265.9 m arc. The chord is what the node search measures and the
		// measure is what the ride is built from; both have to be inside the
		// radius or the two disagree about what "at this boundary" means.
		//
		// SAME CONSTANT, SECOND METRIC — not a new tolerance. The chord in
		// AbsorbedM is a proxy for the along-canon reach; this is the proxy's
		// subject, checked directly.
		ignoreMeasure := A1AbsorbIgnoreMeasureForTests != nil && A1AbsorbIgnoreMeasureForTests()
		if ignoreMeasure {
			// RED-PROOF SWITCH, INERT IN PRODUCTION: restores the rule this
			// replaced — any node on the lineage, however far along canon —
			// so the guard it exists to make unnecessary can be seen firing.
			for _, l2 := range l.Leaves {
				consider(l2.LoNode, l2.LoLat, l2.LoLon)
				consider(l2.HiNode, l2.HiLat, l2.HiLon)
			}
			return best, bestD, found, bad
		}
		if leaf.LoM >= measure-EndpointWarpRadiusM {
			consider(leaf.LoNode, leaf.LoLat, leaf.LoLon)
		}
		if leaf.HiM <= measure+EndpointWarpRadiusM {
			consider(leaf.HiNode, leaf.HiLat, leaf.HiLon)
		}
		break
	}
	return best, bestD, found, bad
}

// A1AbsorbIgnoreMeasureForTests, when non-nil and true, drops the along-canon
// half of the absorb search's candidate rule — see NearestLineageNode. nil in
// production; a test installs it to prove the ride-resolution guard fires.
var A1AbsorbIgnoreMeasureForTests func() bool
