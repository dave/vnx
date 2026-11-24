package merge

// What ONE leg's corridor solve actually allocates, term by term.
//
// WHY THIS EXISTS. A slot count and a live-heap ceiling taken on different
// legs cannot be subtracted, so a claim like "the state lists are the solve's
// dominant live term" cannot be checked from them. This file reports every
// term of one leg's solve at once, so the slot count and the ceiling are taken
// on the SAME leg and the residual is a number rather than an assumption.
//
// ⚠ EVERY FIGURE IS A MEASURED CAPACITY TIMES A WIDTH TAKEN FROM THE TYPE. No
// term here is predicted, and no term is a fraction of another. The two map
// terms are the exception and they are named as estimates, because a Go map's
// internal size is not observable from outside it — see
// [corridorMapEntryBytes].
//
// ⚠ IT MEASURES WITHOUT PERTURBING, like the state-list instrument it extends:
// the gather reads len and cap, allocates only the returned struct, and runs
// where the search has finished so nothing it reports can still move.

import "unsafe"

// Element and header widths, taken from the types rather than written down.
const (
	corridorPackedStateBytes = int(unsafe.Sizeof(CorridorPackedState(0)))
	corridorFloat64Bytes     = int(unsafe.Sizeof(float64(0)))
	corridorInt64Bytes       = int(unsafe.Sizeof(int64(0)))
	corridorInt32Bytes       = int(unsafe.Sizeof(int32(0)))
	corridorIntBytes         = int(unsafe.Sizeof(int(0)))
	corridorPtrBytes         = int(unsafe.Sizeof((*int)(nil)))
	corridorSliceHdrBytes    = int(unsafe.Sizeof([]int32(nil)))
	corridorSolvePieceBytes  = int(unsafe.Sizeof(SolvePiece{}))
	// The search heap: one packed key per entry, one position per slot
	// ([corridorHeap]). These replaced corridorPQItemBytes (16 B per lazy-queue
	// item).
	corridorHeapEntryBytes = int(unsafe.Sizeof(int64(0)))
	corridorHeapPosBytes   = int(unsafe.Sizeof(corridorHeapNever))
	corridorIvBoundsBytes  = int(unsafe.Sizeof(CorridorIvBounds{}))
)

// corridorMapEntryBytes is an ESTIMATE, and the only estimate in this file.
//
// A Go map (swiss tables, Go 1.24+) stores its entries in groups of 8 slots,
// each group carrying 8 control bytes beside its 8 keys and 8 values, at a
// maximum load factor of 7/8. So one live entry costs about (1 + key + value) ×
// 8/7, plus a directory that is small beside it. The figure is therefore right
// to within a few per cent and is NOT exact; anything computed from it says
// "estimated" where it is reported.
func corridorMapEntryBytes(keyBytes, valBytes int) int {
	return (1 + keyBytes + valBytes) * 8 / 7
}

// corridorProjectedSlotBytes is what the six slot-sized tables of a solve with
// this shape will occupy: statesBy, dist, prev and pieceSlots, each counted as
// its backing array plus its outer slice headers, and the search heap — its
// entry array at capacity slots, and its per-layer position table.
//
// ⚠ THE HEAP IS IN THE PROJECTION. A lazy queue grows with the number of
// pushes, which the count pass cannot see, so it would need a mid-solve budget
// of its own. The indexed heap holds each state at most once and is allocated
// once, at capacity slots, before the first relax — so its bytes are known
// here, and this is where they are refused.
//
// ⚠ IT IS THE SAME ARITHMETIC THE REFUSAL GATE USES AND THE INSTRUMENT CHECKS.
// The gate in [solveCorridorPath] calls it BEFORE those six tables are
// allocated (statesBy, dist, prev, pieceSlots, and the heap's entries and
// positions), from the count pass's own totals; [CorridorSolveFootprint]
// measures the same six afterwards from their real capacities, and
// TestCorridorSolve_Footprint asserts the two agree exactly. A projection nothing checks against the allocation would be a guess
// with a function signature.
//
// slots is Σ_l len(statesBy[l]) and pieces is len(sps). The pieceSlots entry
// count is DERIVED, not passed: every layer holds OFF plus two states per piece
// covering it, so slots = layers + 2 × Σ_i pieceSpan[i] and the pieceSlots
// entries are exactly (slots − layers) / 2.
func corridorProjectedSlotBytes(layers, slots, pieces int) int {
	pieceSlotEntries := (slots - layers) / 2
	return slots*corridorPackedStateBytes + layers*corridorSliceHdrBytes + // statesBy
		slots*corridorFloat64Bytes + layers*corridorSliceHdrBytes + // dist
		slots*corridorInt64Bytes + layers*corridorSliceHdrBytes + // prev
		pieceSlotEntries*corridorInt32Bytes + pieces*corridorSliceHdrBytes + // pieceSlots
		slots*corridorHeapEntryBytes + // heap entries, capacity = slots
		slots*corridorHeapPosBytes + layers*corridorSliceHdrBytes // heap positions
}

// corridorSwitchIndexUse is the lazily built per-layer switch indexes' size,
// summed where their type is in scope (it is local to [solveCorridorPath]) and
// handed over as numbers.
type corridorSwitchIndexUse struct {
	Layers  int // layers whose index was built at all — they are built on demand
	Entries int // map entries across those layers, one per (layer, candidate)
	SlotCap int // Σ cap of the per-candidate slot lists
}

// CorridorSolveFootprint is one leg's solve, in bytes, by term.
type CorridorSolveFootprint struct {
	Layers    int // len(bps)
	Pieces    int // len(sps), the state alphabet
	Intervals int
	SlotsLen  int // Σ_l len(statesBy[l]) — the census's States for this leg
	SlotsCap  int // Σ_l cap(statesBy[l]) — the memory

	// ProjectedSlotBytes is what the refusal gate projected for this leg's
	// six slot tables, from the count pass, before they were allocated. A
	// test compares it with SlotTableBytes, what they measure afterwards.
	ProjectedSlotBytes int

	// The six slot-sized tables, separately. Each includes its own outer
	// slice headers, so the six sum to [corridorProjectedSlotBytes].
	// ⚠ StatesBytes includes the OUTER SLICE HEADERS, so it exceeds the
	// state-list bytes by exactly layers x 24 B — 9,033,036 against
	// 8,971,860 on the largest test leg, a difference of 2,549 x 24 =
	// 61,176. Both figures are right; they count different things. Recorded so
	// nobody chases 61 KB of imaginary drift.
	StatesBytes     int
	DistBytes       int
	PrevBytes       int
	PieceSlotsBytes int
	PieceSlotsCap   int
	// The search heap ([corridorHeap]): its entry array at its fixed capacity,
	// and its position table (one int32 per slot, plus per-layer headers).
	HeapBytes    int
	HeapPosBytes int

	// The state alphabet, which outlives the mint and is live for the whole
	// solve (the solve releases ivs[].Pieces, not this copy).
	PieceBytes int

	// The heap's capacity and its high-water mark. The bytes are HeapBytes,
	// fixed at construction; the peak is a count, checked against States.
	HeapCap   int
	PQPeakLen int

	// The lazily built switch indexes: the slot lists exactly, their maps
	// estimated.
	SwitchLayers      int
	SwitchEntries     int
	SwitchSlotCap     int
	SwitchSliceBytes  int
	SwitchMapBytesEst int

	// Per-layer and per-piece bookkeeping that is NOT slot-sized but is live
	// for the whole solve.
	LayerBookBytes  int
	PieceBookBytes  int
	IvBoundsBytes   int
	LayerOfBytesEst int
}

// SlotTableBytes is the six slot-sized tables — the quantity the refusal gate
// projects.
func (f CorridorSolveFootprint) SlotTableBytes() int {
	return f.StatesBytes + f.DistBytes + f.PrevBytes + f.PieceSlotsBytes + f.HeapBytes + f.HeapPosBytes
}

// AccountedBytes is everything above: the solve's own allocations, as far as
// this instrument can see them.
//
// It does NOT include what the solve did not allocate and cannot free — the
// prepped candidates, the source measure, the parsed geometry, the open
// database — so it is never expected to equal a live-heap ceiling. The
// difference is the residual, and reporting it is the point.
func (f CorridorSolveFootprint) AccountedBytes() int {
	return f.SlotTableBytes() + f.PieceBytes +
		f.SwitchSliceBytes + f.SwitchMapBytesEst +
		f.LayerBookBytes + f.PieceBookBytes + f.IvBoundsBytes + f.LayerOfBytesEst
}

// corridorSolveFootprintOf measures the finished solve.
//
// The caller passes the tables themselves, so every figure is a real capacity;
// the switch indexes arrive pre-summed because their type is function-local.
func corridorSolveFootprintOf(
	bps []float64,
	statesBy [][]CorridorPackedState,
	sps []SolvePiece,
	dist [][]float64,
	prev [][]int64,
	pieceSlots [][]int32,
	ivBounds []CorridorIvBounds,
	sw corridorSwitchIndexUse,
	pq *corridorHeap,
	pqPeakLen int,
) CorridorSolveFootprint {
	f := CorridorSolveFootprint{
		Layers:    len(bps),
		Pieces:    len(sps),
		Intervals: len(ivBounds),
		HeapCap:   cap(pq.H),
		PQPeakLen: pqPeakLen,
	}
	for _, ss := range statesBy {
		f.SlotsLen += len(ss)
		f.SlotsCap += cap(ss)
	}
	var distCap, prevCap int
	for l := range dist {
		distCap += cap(dist[l])
		prevCap += cap(prev[l])
	}
	for _, sl := range pieceSlots {
		f.PieceSlotsCap += cap(sl)
	}

	f.StatesBytes = f.SlotsCap*corridorPackedStateBytes + len(statesBy)*corridorSliceHdrBytes
	f.DistBytes = distCap*corridorFloat64Bytes + len(dist)*corridorSliceHdrBytes
	f.PrevBytes = prevCap*corridorInt64Bytes + len(prev)*corridorSliceHdrBytes
	f.PieceSlotsBytes = f.PieceSlotsCap*corridorInt32Bytes + len(pieceSlots)*corridorSliceHdrBytes

	f.PieceBytes = cap(sps) * corridorSolvePieceBytes
	var posCap int
	for _, p := range pq.Pos {
		posCap += cap(p)
	}
	f.HeapBytes = cap(pq.H) * corridorHeapEntryBytes
	f.HeapPosBytes = posCap*corridorHeapPosBytes + len(pq.Pos)*corridorSliceHdrBytes

	f.SwitchLayers, f.SwitchEntries, f.SwitchSlotCap = sw.Layers, sw.Entries, sw.SlotCap
	f.SwitchSliceBytes = sw.SlotCap*corridorInt32Bytes + sw.Entries*corridorSliceHdrBytes
	f.SwitchMapBytesEst = sw.Entries * corridorMapEntryBytes(corridorIntBytes, corridorSliceHdrBytes)

	// bps, statesAt, genericDone, runnerUpDone, minCandAt, the switchIdx
	// pointer array, and the outer arrays of dist/prev/statesBy are already
	// counted in their own terms above.
	layers := len(bps)
	f.LayerBookBytes = layers*corridorFloat64Bytes + // bps
		layers*corridorInt32Bytes + // statesAt
		2*layers + // genericDone, runnerUpDone (one bool each)
		layers*corridorIntBytes + // minCandAt
		layers*corridorPtrBytes // switchIdx
	f.PieceBookBytes = 2 * len(sps) * corridorInt32Bytes // pieceLoLayer, pieceSpan
	f.IvBoundsBytes = cap(ivBounds) * corridorIvBoundsBytes
	f.LayerOfBytesEst = layers * corridorMapEntryBytes(corridorFloat64Bytes, corridorIntBytes)
	return f
}

// corridorSolveBudgetUnset is the "no budget agreed" value.
const corridorSolveBudgetUnset = -1

// HOW THE BUDGET BELOW IS TO BE SET — written down before the number exists, so
// that it cannot arrive as a constant with no derivation.
//
// It is a function of ONE measured quantity and ONE stated policy factor:
//
//	Budget_A = ceil( H x corridorProjectedSlotBytes(L*, S*, P*) )
//
// where (L*, S*, P*) are the layers, slots and alphabet size of THE LARGEST LEG
// WE INTEND TO SERVE, measured by [corridorSolveFootprintOf], and H is headroom
// over that leg.
//
// ⚠ THERE IS NO SECOND BUDGET ON THE SEARCH QUEUE. A lazy queue grows with the
// number of pushes, which the count pass cannot see, so it would need a
// mid-solve bound against its own length. The indexed heap holds each state at
// most once and is allocated at full size before the first relax, so a
// mid-solve check would compare a length against a budget after every byte was
// already held and could prevent nothing. Its bytes are in
// [corridorProjectedSlotBytes] instead, and an overflow is the heap's own
// capacity error.
//
// ⚠ H IS DETERMINED BY THE DEVICE, NOT CHOSEN. The budget's job is to refuse what
// the device cannot serve, not to allow twice whatever our fixtures happen to
// contain, so once an envelope E exists the arithmetic runs the other way and H
// falls out of it:
//
//	available = E − baseline − F_det
//	Budget_A  must fit inside available,  and H follows
//
// It might be 2. It might be 1.1. It might be below 1, and that is not a failure
// of the method — reading a threshold off the device IS the method. What is
// forbidden is different: shrinking H to make an inconvenient inequality close
// while still claiming the fixture fits. If E is smaller than the worst leg's own
// accounted cost, the finding is "we cannot serve our own worst fixture on that
// device", reported as that.
//
// MEASURED with the indexed heap on the largest test leg (2,549 layers, 26,891
// pieces, 2,242,965 slots, heap peak 2,172,065):
//
//	slot tables, six terms     73.57 MiB   (77,145,800 B — heap 25.72 MiB of it)
//	F_det                      23.06 MiB   (24,183,085 B)
//	whole leg, accounted       96.63 MiB
//
// so the inequality to solve when E arrives is
//
//	H x 77,145,800 B  <=  E − baseline − 24,183,085 B
//
// which needs E − baseline of at least 96.63 MiB for H = 1 and 170.2 MiB for
// H = 2.
//
// With the earlier lazy queue the same leg read: four slot tables 47.84 MiB,
// the lazy queue at peak 80.84 MiB held (66.52 MiB by len), F_det 22.04 MiB,
// accounted 150.72 MiB — and needed 114.36 MiB for H = 1. (F_det also differs
// by 1.02 MiB because each piece's band regime since gained its linear offset:
// SolvePiece 176 B, was 136 B.)
//
// Three properties this shape has on purpose:
//
//   - Budget_A is expressed in the gate's OWN arithmetic
//     ([corridorProjectedSlotBytes]), so the budget cannot drift from the thing
//     it guards.
//   - It carries no multiplier for anything the projection cannot see, and must
//     never carry one: a factor fitted to our corpus is nearestRunHaloEdges
//     again — it works on the fixtures we happen to own, fails silently when
//     the data changes, and nobody can re-derive it. What the projection does
//     not cover (the alphabet, switchIdx, bookkeeping) is F_det, measured.
//   - It is not a function of a live-heap ceiling. That quantity was measured
//     moving 20% between two runs of one build (16.30 -> 19.61 MB on a small
//     test leg, residual −0.61 -> +2.69 MB), so a budget fitted to it would be
//     fitted to noise. The ceiling's job is to confirm that nothing large is
//     unaccounted for, and nothing else.
//
// ⚠ WHAT THIS SHAPE STILL DOES NOT ANSWER. It says how much bigger than our
// worst known leg we tolerate. It does not say what the DEVICE can afford, which
// is the question that actually protects a user, and which needs an envelope E —
// the live heap one leg's solve may use on the smallest supported device. With E
// the check is arithmetic:
//
//	Budget_A + F <= E , F = the measured fixed overhead
//	                                  (pre-solve baseline, alphabet, switchIdx,
//	                                   bookkeeping — all in the footprint table)
//
// If that fails, the honest answer is "we cannot serve our own worst fixture on
// that device", NOT a smaller H. Lowering H to make the sum fit is how a budget
// becomes a fitted constant that refuses imports which work today. We already own
// one constant fitted to our own test data — nearestRunHaloEdges = 8, documented
// as a fudge, which fails silently when lowered — and that is the whole argument
// against acquiring a second.
//
// ⚠ E IS A READING, NOT A DEVICE CONSTANT. Some platforms can be asked directly
// (iOS's os_proc_available_memory() returns the bytes THIS process may still
// use before it is killed), so the inequality belongs against a headroom
// reading taken at the START OF THE LEG. A constant derived from the smallest
// supported device must assume the worst about everything else in the process,
// so it would refuse imports that would have worked — the same over-refusal
// these budgets exist to avoid, arrived at from the other side.
//
// ⚠ NOT EVERY PLATFORM HAS SUCH A READING. Android has no per-process headroom
// API (ActivityManager's availMem is device-wide), so do not assume symmetry: a
// budget that silently reads 0 there would refuse every leg.

// THE SLOT-TABLE BUDGET ([TestHooks].SolveSlotTableBudgetBytes) caps what one leg's six slot-sized tables —
// the four state tables and the search heap — may allocate. The gate that reads it is in [solveCorridorPath], between the
// count pass and the allocation.
//
// ⛔⛔ THERE IS NO VALUE YET, AND THAT IS NOT A DECISION. Production runs with
// NO BUDGET (0, or corridorSolveBudgetUnset): the gate is wired, measured and red-proofed, and it refuses
// nothing until a number is agreed against the same-leg accounting
// (TestCorridorSolve_Footprint). As shipped it protects nobody — it is here so
// the mechanism can be reviewed before a number is chosen, and so that no
// plausible-looking constant can be read as settled. REPLACE IT.
//
// ⚠ IT BOUNDS THE PROJECTABLE PART, WHICH INCLUDES THE SEARCH HEAP. It does not
// cover the piece alphabet, the lazily built switchIdx, or the bookkeeping —
// F_det in the accounting — so it is still not the solve's whole footprint.
//
// A test option, not a constant, so the refusal can be red-proofed by setting
// it for one solve. Production never sets it.
