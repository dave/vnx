package merge

// ---- the lazy queue, kept as a TEST REFERENCE --------------------------
//
// corridorPQ was the corridor solve's search queue until
// the indexed [CorridorHeap] replaced it.
// It lives on here, verbatim from production less its unused init, for one
// reason: it is the "lazy queue filtered to live pops" arm of
// TestCorridorHeap_PopOrderMatchesReferences, the three-way agreement the heap's
// byte-identity rests on. That test also checks this arm against its sorted
// (dist, key) reference, so the arm stays a trustworthy reference rather than a
// second copy of whatever the heap does.
// Its comments are production's at the time and speak of the solve in the
// present tense.

// corridorPQItem is stored BY VALUE. It used to be a pointer with an `idx`
// field maintained by swap for heap.Fix — but nothing has ever called
// heap.Fix here (this Dijkstra pushes a duplicate and discards the stale pop
// instead), so `idx` was dead weight and the pointer meant one heap
// allocation per push. Measured on a 5,000-point OSM track: runtime.madvise
// alone was 29% of the import, most of it this.
type corridorPQItem struct {
	key  int64 // packed (layer,state) — see packCorridorNode
	dist float64
}

// corridorPQ is a binary min-heap driven by its own typed methods.
//
// ⚠ POP ORDER IS LOAD-BEARING, AND IT IS NOW A RULE: cost, then the packed
// (layer, slot) key (see less). A tie moves committed geometry by two routes:
// relax keeps the FIRST arrival within 1e-12 as prev, so which of two equal-cost
// states is expanded first decides the predecessor a later state keeps; and
// genericDone/minCandAt take the first state popped at a layer as that layer's
// fan-out source. The audit that motivated the rule found the prev route is
// the common one — the moved solves it examined chose a different piece at a
// hand-off.
//
// up/down below are container/heap's up/down ported line for line,
// with Less and Swap written in as the concrete less/swap methods. Under a
// total order that no longer matters for correctness — every correct min-heap
// pops the same sequence — but the port is kept as it was so the only change
// the rule made is the rule. TestCorridorHeap_PopOrderMatchesReferences checks
// the pop sequence against a sorted (dist, key) reference.
//
// The reason for the port is allocation: heap.Push/heap.Pop take and return
// `any`, so every push and every pop boxed a corridorPQItem on the heap —
// 1,088 MB of churn, 20.6% of all allocation on the profiled dwell leg.
type corridorPQ []corridorPQItem

// less orders the queue by cost, then by packed (layer, slot) key.
//
// ⚠ THE KEY IS THE TIE RULE, AND IT IS WHAT MAKES THE ORDER A TOTAL ORDER.
// Cost alone left ties to the heap's layout, which depended on every stale
// duplicate ever pushed, so no queue without those duplicates could reproduce
// it. relax pushes only on a strict improvement, so no two LIVE items share both
// cost and key; with the key as the second field every correct min-heap pops the
// same sequence of live states — which is what lets an indexed heap be proved
// byte-identical to this one. On equal cost
// the earlier layer comes off first, and within a layer the state that comes
// first in statesBy.
//
// Exact comparison, no tolerance. Two costs that differ by one ULP, because they
// were summed in a different order, are ordered by cost, not by key. That was
// already true before the rule, and it stays out of this change.
func (pq corridorPQ) less(i, j int) bool {
	if pq[i].dist != pq[j].dist {
		return pq[i].dist < pq[j].dist
	}
	return pq[i].key < pq[j].key
}
func (pq corridorPQ) swap(i, j int) { pq[i], pq[j] = pq[j], pq[i] }

func (pq *corridorPQ) push(x corridorPQItem) {
	*pq = append(*pq, x)
	pq.up(len(*pq) - 1)
}

func (pq *corridorPQ) pop() corridorPQItem {
	old := *pq
	n := len(old) - 1
	old.swap(0, n)
	old.down(0, n)
	it := old[n]
	*pq = old[:n]
	return it
}

func (pq corridorPQ) up(j int) {
	for {
		i := (j - 1) / 2 // parent
		if i == j || !pq.less(j, i) {
			break
		}
		pq.swap(i, j)
		j = i
	}
}

func (pq corridorPQ) down(i0, n int) bool {
	i := i0
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 { // j1 < 0 after int overflow
			break
		}
		j := j1 // left child
		if j2 := j1 + 1; j2 < n && pq.less(j2, j1) {
			j = j2 // = 2*i + 2  // right child
		}
		if !pq.less(j, i) {
			break
		}
		pq.swap(i, j)
		i = j
	}
	return i > i0
}
