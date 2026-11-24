package merge

import (
	"fmt"
	"math"
)

// corridorHeap is an INDEXED binary min-heap over packed (layer, slot) keys —
// the corridor solve's search queue. It replaced a lazy queue (corridorPQ,
// kept in the tests as the reference).
//
// WHY INDEXED. The lazy queue has no decrease-key: every improvement pushes a
// new item and leaves the old one to be discarded when it pops, so it grows with
// the number of improvements, not the number of states — 4,359,193 items for
// 2,242,965 states on the largest test fixture's worst leg, 80.84 MiB with append's
// overshoot. Here each state has at most one entry, and pos records where it is,
// so an improvement moves the existing entry up instead of adding one. The heap
// can then never hold more entries than there are states, and it is allocated
// once, at a capacity fixed by the caller, with no append growth.
//
// ⚠ POP ORDER IS LOAD-BEARING, AND IT IS A RULE: cost, then key (see less). A
// tie moves committed geometry by two routes: relax keeps the FIRST arrival
// within 1e-12 as prev, so which of two equal-cost states is expanded first
// decides the predecessor a later state keeps; and genericDone/minCandAt take
// the first state popped at a layer as that layer's fan-out source. The rule was
// given to the lazy queue first; this heap pops the same sequence of live
// states under it — TestCorridorHeap_PopOrderMatchesReferences checks both against a sorted
// reference, and a per-leg digest once checked it on every solved leg of the
// test corpora.
//
// It does not store costs. It reads each entry's cost from dist, the solve's own
// table, so the caller lowers dist[layer][slot] FIRST and then calls improve.
//
// ⚠ NO container/heap AND NO any, for the same allocation reason corridorPQ's
// comment gives: after construction no operation allocates
// (TestCorridorHeap_NoAllocations).
type corridorHeap struct {
	// h holds the keys, heap-ordered. Its capacity is fixed at construction
	// and never grows: insert past it is an error.
	H []int64
	// pos is shaped like dist: pos[layer][slot] is the key's index in h, or
	// corridorHeapNever / corridorHeapPopped.
	Pos  [][]int32
	dist [][]float64
}

const (
	// corridorHeapNever marks a key that has never been inserted.
	corridorHeapNever int32 = -1
	// corridorHeapPopped marks a key that has been popped. Its dist is final;
	// an improvement to it is an error (see improve).
	corridorHeapPopped int32 = -2
)

// newCorridorHeap allocates the heap for a table shaped like dist, with room for
// capacity entries. Both allocations happen here and nowhere else.
//
// capacity is the caller's projection of the states the search can reach — in
// the solve, the count pass's projectedSlots. It must fit int32, because pos
// stores heap indices in 4 bytes; a larger capacity is an error, not a silent
// truncation.
func newCorridorHeap(dist [][]float64, capacity int) (*corridorHeap, error) {
	if capacity < 0 || capacity > math.MaxInt32 {
		return nil, fmt.Errorf("corridor heap: capacity %d does not fit the int32 position table", capacity)
	}
	pos := make([][]int32, len(dist))
	for l := range dist {
		p := make([]int32, len(dist[l]))
		for i := range p {
			p[i] = corridorHeapNever
		}
		pos[l] = p
	}
	return &corridorHeap{H: make([]int64, 0, capacity), Pos: pos, dist: dist}, nil
}

func (q *corridorHeap) Len() int { return len(q.H) }

// less orders the heap by cost, then by packed (layer, slot) key.
//
// ⚠ THE KEY IS THE TIE RULE, AND IT IS WHAT MAKES THE ORDER A TOTAL ORDER.
// A key is in the heap at most once, so no two entries share it, and with the
// key as the second field every correct min-heap pops the same sequence — which
// is what let this heap be proved byte-identical to the lazy queue it replaced.
//
// Exact comparison, no tolerance. Two costs that differ by one ULP, because they
// were summed in a different order, are ordered by cost, not by key.
func (q *corridorHeap) less(a, b int64) bool {
	la, sa := unpackLayerSlot(a)
	lb, sb := unpackLayerSlot(b)
	da, db := q.dist[la][sa], q.dist[lb][sb]
	if da != db {
		return da < db
	}
	return a < b
}

func (q *corridorHeap) swap(i, j int) {
	q.H[i], q.H[j] = q.H[j], q.H[i]
	li, si := unpackLayerSlot(q.H[i])
	lj, sj := unpackLayerSlot(q.H[j])
	q.Pos[li][si] = int32(i)
	q.Pos[lj][sj] = int32(j)
}

func (q *corridorHeap) up(j int) {
	for j > 0 {
		i := (j - 1) / 2 // parent
		if !q.less(q.H[j], q.H[i]) {
			break
		}
		q.swap(i, j)
		j = i
	}
}

func (q *corridorHeap) down(i int) {
	n := len(q.H)
	for {
		j1 := 2*i + 1
		if j1 >= n {
			break
		}
		j := j1 // left child
		if j2 := j1 + 1; j2 < n && q.less(q.H[j2], q.H[j1]) {
			j = j2 // right child
		}
		if !q.less(q.H[j], q.H[i]) {
			break
		}
		q.swap(i, j)
		i = j
	}
}

// insert adds a key that has never been in the heap. The caller has already
// written its dist.
//
// Every refusal is an error, never a repair. Past the fixed capacity it means
// the capacity projection was wrong. A key already in the heap means the caller
// should have called decrease. A popped key is the case [improve] describes.
func (q *corridorHeap) Insert(key int64) error {
	l, s := unpackLayerSlot(key)
	switch p := q.Pos[l][s]; {
	case p == corridorHeapPopped:
		return q.poppedErr(key)
	case p >= 0:
		return fmt.Errorf("corridor heap: insert of (layer %d, slot %d), which is already in the heap at %d — use decrease", l, s, p)
	}
	n := len(q.H)
	if n == cap(q.H) {
		return fmt.Errorf("corridor heap: insert of (layer %d, slot %d) would exceed the heap's fixed capacity of %d entries", l, s, cap(q.H))
	}
	q.H = q.H[:n+1]
	q.H[n] = key
	q.Pos[l][s] = int32(n)
	q.up(n)
	return nil
}

// decrease restores heap order after the caller has LOWERED the dist of a key
// that is in the heap. Only a decrease is supported: a raised dist would need a
// sift down, and the solve never raises one.
func (q *corridorHeap) Decrease(key int64) error {
	l, s := unpackLayerSlot(key)
	switch p := q.Pos[l][s]; {
	case p == corridorHeapPopped:
		return q.poppedErr(key)
	case p == corridorHeapNever:
		return fmt.Errorf("corridor heap: decrease of (layer %d, slot %d), which was never inserted — use insert", l, s)
	default:
		q.up(int(p))
		return nil
	}
}

// improve is the one heap call relax makes after lowering dist[layer][slot]:
// insert for a key never seen, decrease for a key in the heap. inserted reports
// which one it was — true only when a new entry was added — so the solve's
// census can count inserts without reading pos itself.
//
// ⚠ A POPPED KEY IS AN ERROR. A popped
// state's dist is final under Dijkstra, and only a negative edge cost can
// improve it again. The lazy queue would have pushed it again without comment;
// here it is reported, so a broken cost term is caught the day one appears
// instead of silently re-expanding.
//
// ⚠ THIS ERROR HAS ALREADY CAUGHT ONE. When the heap was introduced, ordinary
// forward imports of two test tracks failed on it: the emission integral
// cancelled to small negative values on tiny pieces, and the lazy queue had
// been improving 366 popped states on those two imports alone. The integral
// was made non-negative by construction, after which 1,231 solves across the
// test corpora measured 0 re-improvements.
func (q *corridorHeap) Improve(key int64) (inserted bool, err error) {
	l, s := unpackLayerSlot(key)
	if q.Pos[l][s] >= 0 {
		return false, q.Decrease(key)
	}
	if err := q.Insert(key); err != nil {
		return false, err
	}
	return true, nil
}

// pop removes and returns the minimum key, and marks it popped. The heap must
// not be empty; the caller loops on len.
func (q *corridorHeap) Pop() int64 {
	top := q.H[0]
	n := len(q.H) - 1
	q.swap(0, n)
	q.H = q.H[:n]
	if n > 0 {
		q.down(0)
	}
	l, s := unpackLayerSlot(top)
	q.Pos[l][s] = corridorHeapPopped
	return top
}

func (q *corridorHeap) poppedErr(key int64) error {
	l, s := unpackLayerSlot(key)
	return fmt.Errorf("corridor heap: (layer %d, slot %d) was improved to %v after it was popped — its dist was final, so an edge cost is negative", l, s, q.dist[l][s])
}
