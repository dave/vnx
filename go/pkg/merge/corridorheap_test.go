package merge

import (
	"math"
	"strings"
	"testing"
)

// heapOp is one step of a corridorHeap program: lower a key's dist to val, or
// pop.
type heapOp struct {
	pop bool
	key int64
	val float64
}

// resetDist fills a dist table with +Inf, as the solve starts one.
func resetDist(dist [][]float64) {
	for l := range dist {
		for s := range dist[l] {
			dist[l][s] = math.Inf(1)
		}
	}
}

// skipImprove reports whether a program step must be skipped by every runner:
// its key was popped already, or the step would not strictly lower the key's
// current dist. Improving a popped key is the error case, tested on its own in
// TestCorridorHeap_PoppedKeyImprovedAgainIsAnError.
func skipImprove(op heapOp, dist [][]float64, popped map[int64]bool) bool {
	if popped[op.key] {
		return true
	}
	l, s := unpackLayerSlot(op.key)
	return !(op.val < dist[l][s])
}

// runCorridorHeap drives the indexed heap. It returns the popped keys and how
// many pops were tie-decided (the next entry in (dist, key) order had the same
// dist), so the caller can assert the program exercised the tie rule at all.
func runCorridorHeap(t *testing.T, ops []heapOp, dist [][]float64, capacity int) (out []int64, ties int) {
	t.Helper()
	resetDist(dist)
	q, err := newCorridorHeap(dist, capacity)
	if err != nil {
		t.Fatal(err)
	}
	popped := map[int64]bool{}
	for _, op := range ops {
		if op.pop {
			if q.Len() == 0 {
				continue
			}
			k := q.Pop()
			popped[k] = true
			out = append(out, k)
			if q.Len() > 0 {
				l0, s0 := unpackLayerSlot(k)
				l1, s1 := unpackLayerSlot(q.H[0])
				if dist[l0][s0] == dist[l1][s1] {
					ties++
				}
			}
			continue
		}
		if skipImprove(op, dist, popped) {
			continue
		}
		l, s := unpackLayerSlot(op.key)
		dist[l][s] = op.val
		if _, err := q.Improve(op.key); err != nil {
			t.Fatalf("improve: %v", err)
		}
	}
	for q.Len() > 0 {
		k := q.Pop()
		popped[k] = true
		out = append(out, k)
	}
	return out, ties
}

// runSortedHeapRef is the reference: the set of live keys, and pop takes the
// minimum under (dist, key) by a linear scan. No heap, no shared code.
func runSortedHeapRef(ops []heapOp, dist [][]float64) []int64 {
	resetDist(dist)
	live := map[int64]bool{}
	popped := map[int64]bool{}
	var out []int64
	popMin := func() {
		best, have := int64(0), false
		for k := range live {
			if !have {
				best, have = k, true
				continue
			}
			lk, sk := unpackLayerSlot(k)
			lb, sb := unpackLayerSlot(best)
			dk, db := dist[lk][sk], dist[lb][sb]
			if dk < db || (dk == db && k < best) {
				best = k
			}
		}
		delete(live, best)
		popped[best] = true
		out = append(out, best)
	}
	for _, op := range ops {
		if op.pop {
			if len(live) > 0 {
				popMin()
			}
			continue
		}
		if skipImprove(op, dist, popped) {
			continue
		}
		l, s := unpackLayerSlot(op.key)
		dist[l][s] = op.val
		live[op.key] = true
	}
	for len(live) > 0 {
		popMin()
	}
	return out
}

// runLazyPQFiltered drives the old lazy queue, [corridorPQ] with its rule order,
// the way the solve does: every improvement pushes an item, and a popped item
// is live only if it still carries its key's current dist. Stale pops are
// discarded and do not count as a pop of the program.
func runLazyPQFiltered(ops []heapOp, dist [][]float64) []int64 {
	resetDist(dist)
	pq := corridorPQ{}
	popped := map[int64]bool{}
	var out []int64
	popLive := func() bool {
		for len(pq) > 0 {
			it := pq.pop()
			l, s := unpackLayerSlot(it.key)
			if it.dist != dist[l][s] || popped[it.key] {
				continue // stale
			}
			popped[it.key] = true
			out = append(out, it.key)
			return true
		}
		return false
	}
	for _, op := range ops {
		if op.pop {
			popLive()
			continue
		}
		if skipImprove(op, dist, popped) {
			continue
		}
		l, s := unpackLayerSlot(op.key)
		dist[l][s] = op.val
		pq.push(corridorPQItem{key: op.key, dist: op.val})
	}
	for popLive() {
	}
	return out
}

// TestCorridorHeap_PopOrderMatchesReferences is the differential gate: the
// indexed heap, a sorted (dist, key) reference and the lazy queue filtered to
// its live pops must pop the same sequence of keys. That three-way agreement
// is the property the heap's byte-identity proof rests on.
//
// The tie rule is the subject, so ties are planned rather than drawn: each
// program below is one case — strict order, a tie decided by key (within a
// layer and across layers), a decrease, a stale lazy entry after an update,
// one slot, and a full heap whose sift-down must pick the right child. The tie
// programs must actually be tie-decided, and that is asserted. A pop on an
// empty queue has no case: every runner skips it before it reaches the heap.
//
// Red proofs: 2026-09-23 (detached worktree), corridorHeap.less put back to
// cost only — then "run 0 (6×4, 67 ops, 7 values): pop 0 differs: heap (layer
// 3, slot 1) vs sorted (dist, key) reference (layer 0, slot 3)". 2026-10-05,
// on this case list: the same mutation fails "tie within a layer"; with
// decrease not sifting up, "decrease" fails; with down always taking the left
// child, "full heap" fails.
func TestCorridorHeap_PopOrderMatchesReferences(t *testing.T) {
	pop := heapOp{pop: true}
	imp := func(layer int, slot int32, v float64) heapOp {
		return heapOp{key: packLayerSlot(layer, slot), val: v}
	}
	cases := []struct {
		name          string
		layers, slots int
		ops           []heapOp
		wantTies      bool
	}{
		{"strict order", 1, 3, []heapOp{imp(0, 0, 3), imp(0, 1, 1), imp(0, 2, 2), pop, pop, pop}, false},
		{"tie within a layer", 1, 5, []heapOp{imp(0, 4, 1), imp(0, 3, 1), imp(0, 2, 1), imp(0, 1, 1), imp(0, 0, 1), pop, pop, pop, pop, pop}, true},
		{"tie across layers", 3, 2, []heapOp{imp(2, 0, 4), imp(1, 1, 4), imp(0, 1, 4), imp(1, 0, 4), pop, pop, pop, pop}, true},
		{"decrease", 1, 3, []heapOp{imp(0, 0, 5), imp(0, 1, 3), imp(0, 2, 4), imp(0, 0, 1), pop, pop, pop}, false},
		{"stale entry after an update", 1, 2, []heapOp{imp(0, 0, 5), imp(0, 1, 3), imp(0, 0, 2), pop, imp(0, 1, 2), pop}, false},
		{"one slot", 1, 1, []heapOp{imp(0, 0, 7), imp(0, 0, 2), pop}, false},
		{"full heap", 2, 3, []heapOp{
			imp(0, 0, 1), imp(0, 1, 6), imp(0, 2, 5), imp(1, 0, 9), imp(1, 1, 8), imp(1, 2, 2),
			pop, pop, pop, pop, pop, pop}, false},
	}
	for _, c := range cases {
		dist := make([][]float64, c.layers)
		for l := range dist {
			dist[l] = make([]float64, c.slots)
		}
		got, ties := runCorridorHeap(t, c.ops, dist, c.layers*c.slots)
		ref := runSortedHeapRef(c.ops, dist)
		lazy := runLazyPQFiltered(c.ops, dist)
		if len(got) == 0 {
			t.Fatalf("%s: the heap popped nothing", c.name)
		}
		if c.wantTies && ties == 0 {
			t.Fatalf("%s: no pop was tie-decided — the case does not exercise the tie rule", c.name)
		}
		for name, want := range map[string][]int64{"sorted (dist, key) reference": ref, "lazy corridorPQ, live pops": lazy} {
			if len(got) != len(want) {
				t.Fatalf("%s: heap popped %d keys, %s popped %d", c.name, len(got), name, len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					gl, gs := unpackLayerSlot(got[i])
					wl, ws := unpackLayerSlot(want[i])
					t.Fatalf("%s: pop %d differs: heap (layer %d, slot %d) vs %s (layer %d, slot %d)",
						c.name, i, gl, gs, name, wl, ws)
				}
			}
		}
	}
}

// TestCorridorHeap_PoppedKeyImprovedAgainIsAnError pins the popped-key rule (2026-09-23):
// a key popped and then improved again is an error from every entry point, not
// a re-insert.
//
// Red proof, 2026-09-23 (detached worktree): with pop leaving the position at
// -1 instead of -2, the popped key is re-inserted and this fails —
// "insert of a popped key returned nil; it must be an error" (which of the
// three entry points reports first follows map order).
func TestCorridorHeap_PoppedKeyImprovedAgainIsAnError(t *testing.T) {
	dist := [][]float64{{math.Inf(1), math.Inf(1)}}
	q, err := newCorridorHeap(dist, 2)
	if err != nil {
		t.Fatal(err)
	}
	a, b := packLayerSlot(0, 0), packLayerSlot(0, 1)
	dist[0][0], dist[0][1] = 1, 2
	for _, k := range []int64{a, b} {
		if _, err := q.Improve(k); err != nil {
			t.Fatalf("improve: %v", err)
		}
	}
	if k := q.Pop(); k != a {
		t.Fatalf("popped %d, want %d", k, a)
	}
	dist[0][0] = 0.5 // only a negative edge could do this
	for name, call := range map[string]func(int64) error{
		"improve":  func(k int64) error { _, err := q.Improve(k); return err },
		"insert":   q.Insert,
		"decrease": q.Decrease,
	} {
		err := call(a)
		if err == nil {
			t.Fatalf("%s of a popped key returned nil; it must be an error", name)
		}
		if !strings.Contains(err.Error(), "after it was popped") {
			t.Fatalf("%s of a popped key: wrong error %q", name, err)
		}
	}
	if q.Len() != 1 {
		t.Fatalf("heap holds %d entries after the refused improvements, want 1 — a refusal must not change the heap", q.Len())
	}
}

// TestCorridorHeap_OverCapacityIsAnError: the heap never grows. An insert past
// the capacity fixed at construction is an error, because it means the
// capacity projection under-counted the states the search reached.
func TestCorridorHeap_OverCapacityIsAnError(t *testing.T) {
	dist := [][]float64{{1, 2, 3}}
	q, err := newCorridorHeap(dist, 2)
	if err != nil {
		t.Fatal(err)
	}
	for s := int32(0); s < 2; s++ {
		if err := q.Insert(packLayerSlot(0, s)); err != nil {
			t.Fatalf("insert %d: %v", s, err)
		}
	}
	err = q.Insert(packLayerSlot(0, 2))
	if err == nil || !strings.Contains(err.Error(), "fixed capacity") {
		t.Fatalf("insert past capacity: got %v, want a fixed-capacity error", err)
	}
	if cap(q.H) != 2 || q.Len() != 2 {
		t.Fatalf("heap cap %d len %d after the refused insert, want 2 and 2", cap(q.H), q.Len())
	}
}

// TestCorridorHeap_MisusedEntryPointsAreErrors: insert of a key already in the
// heap and decrease of a key never inserted are caller bugs, reported rather
// than repaired.
func TestCorridorHeap_MisusedEntryPointsAreErrors(t *testing.T) {
	dist := [][]float64{{1, 2}}
	q, err := newCorridorHeap(dist, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Decrease(packLayerSlot(0, 0)); err == nil {
		t.Fatal("decrease of a never-inserted key returned nil")
	}
	if err := q.Insert(packLayerSlot(0, 0)); err != nil {
		t.Fatal(err)
	}
	if err := q.Insert(packLayerSlot(0, 0)); err == nil {
		t.Fatal("second insert of a key in the heap returned nil")
	}
	if _, err := newCorridorHeap(dist, math.MaxInt32+1); err == nil {
		t.Fatal("a capacity past int32 returned nil")
	}
}

// TestCorridorHeap_NoAllocations: after construction, improve (and so the
// insert and decrease it dispatches to) and pop allocate nothing — the property
// corridorPQ was ported off container/heap for. It drives improve because that
// is the call the solve's relax makes.
func TestCorridorHeap_NoAllocations(t *testing.T) {
	const layers, slots = 4, 64
	dist := make([][]float64, layers)
	for l := range dist {
		dist[l] = make([]float64, slots)
	}
	q, err := newCorridorHeap(dist, layers*slots)
	if err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		resetDist(dist)
		for l := 0; l < layers; l++ {
			for s := int32(0); s < slots; s++ {
				dist[l][s] = float64((int(s)*7 + l) % 13)
				if _, err := q.Improve(packLayerSlot(l, s)); err != nil {
					panic(err)
				}
			}
		}
		for l := 0; l < layers; l++ {
			for s := int32(0); s < slots; s += 3 {
				dist[l][s] -= 0.5
				if _, err := q.Improve(packLayerSlot(l, s)); err != nil {
					panic(err)
				}
			}
		}
		for q.Len() > 0 {
			q.Pop()
		}
		// Reset positions for the next run. Test-only: the solve uses a heap
		// once.
		for l := range q.Pos {
			for s := range q.Pos[l] {
				q.Pos[l][s] = corridorHeapNever
			}
		}
	})
	if allocs != 0 {
		t.Fatalf("%.1f allocations per insert/decrease/pop cycle, want 0", allocs)
	}
}
