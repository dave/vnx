package merge

// LegSeam is one interior cut of an assembled leg: the point it falls on, and
// the canon node both sides must terminate on there.
//
// ⚠ HasNode IS NOT "did we find one" — it is which of two DIFFERENT seams this
// is. An ON/canon boundary carries canon's node id, so the leg joins the network
// there. An ON/OFF boundary where the align pass declined to cut (a stub under
// MinRun) has no canon node at all, and the leg mints ONE of its own that both
// pieces share: still a seam, still shared, but private to this leg. Reading a
// zero NodeID as the first case would thread node 0 into an arc write.
type LegSeam struct {
	Idx     int // index into AssembledLeg.Points — the shared point
	Node    NodeID
	HasNode bool

	// Adopted says whether Node existed in canon BEFORE this leg's own commit —
	// as against one THIS SAME LEG minted, moments earlier, by its own align
	// pass. Meaningless when !HasNode: a seam with no canon node is the leg's
	// own private mint by definition and is never adopted.
	//
	// ⚠ NOT A DIRECT READING OF THE BOUNDARY'S OWN alignBoundary.Decline — see
	// [legSeamsFromAlign]. The align pass cuts one leg's boundaries in ITS OWN
	// order (descending measure, tied by run/end), which is not leg-walk order,
	// so a boundary THIS LEG's pass cuts can be read by a LATER boundary of the
	// SAME pass as "the boundary is already at a node" or "an existing node
	// sits here" — its own coordinate search finding exactly what this leg just
	// wrote a moment before. Taking that later boundary's Decline at face value
	// would call a node this same leg minted "adopted". legSeamsFromAlign
	// closes the gap by checking every node this leg's align pass minted, not
	// just the one boundary record being read.
	Adopted bool

	// Absorbed / AbsorbedM carry [alignBoundary.Decline] ==
	// alignDeclineAbsorbedToNode / [alignBoundary.AbsorbedM] straight through —
	// the align pass's own record of a boundary that moved off its computed
	// position to reach Node, and by how far. Meaningless when !HasNode, same
	// as Adopted.
	//
	// Unlike Adopted, this needs no mintedThisLeg correction: absorption only
	// fires against a node "already there" at the moment of the search —
	// whether that node predates this leg or was minted by an EARLIER boundary
	// of this same leg's own descending-measure pass makes no difference to whether
	// THIS boundary moved to reach it, only to whether the node itself counts as
	// adopted. The two facts are independent and both worth keeping: a boundary
	// can be absorbed onto a node this same leg just minted (not adopted, but
	// absorbed).
	Absorbed  bool
	AbsorbedM float64

	// NodeLat/NodeLon are Node's own coordinates, carried straight through from
	// [alignBoundary.NodeLat]/[alignBoundary.NodeLon] — never recomputed from
	// Node here, so a leg-warp target the caller builds from this is
	// bit-identical to what the align pass's own node search found.
	// Meaningless when !HasNode, same as Adopted/Absorbed/AbsorbedM.
	NodeLat, NodeLon float64
}
