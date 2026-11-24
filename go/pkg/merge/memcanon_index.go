package merge

import "math"

// This file holds MemCanon's spatial indexes: a grid of node positions, and
// a grid of boxes (whole-arc boxes and stretch boxes). Both answer a query
// with a superset found through the grid, then test every candidate exactly.

// memCellDeg is the side of one grid cell in degrees (about 1 km of
// latitude). It moves speed only: every query tests its candidates exactly.
const memCellDeg = 0.01

// memMaxCell bounds a cell coordinate, so an infinite or huge value still
// maps to a cell. Clamping is monotone, so a range query stays a superset.
const memMaxCell = 1 << 40

// memBigBoxCells is the most cells a box is filed under. A box that covers
// more goes on a list every query scans.
const memBigBoxCells = 64

type cellKey struct{ lat, lon int64 }

func cellOf(v float64) int64 {
	q := math.Floor(v / memCellDeg)
	switch {
	case q > memMaxCell:
		return memMaxCell
	case q < -memMaxCell:
		return -memMaxCell
	}
	return int64(q)
}

// cellSpan is the cells [lo, hi] covers on one axis, and how many.
func cellSpan(lo, hi float64) (from, to int64, n float64) {
	from, to = cellOf(lo), cellOf(hi)
	return from, to, float64(to-from) + 1
}

// box64 is a box in float64 degrees, inclusive at both ends.
type box64 struct{ minLat, maxLat, minLon, maxLon float64 }

func (b box64) meets(q box64) bool {
	return b.maxLat >= q.minLat && b.minLat <= q.maxLat &&
		b.maxLon >= q.minLon && b.minLon <= q.maxLon
}

func (b box64) hasNaN() bool {
	return math.IsNaN(b.minLat) || math.IsNaN(b.maxLat) || math.IsNaN(b.minLon) || math.IsNaN(b.maxLon)
}

func boundsBox(b Bounds) box64 {
	return box64{minLat: b.MinLat, maxLat: b.MaxLat, minLon: b.MinLon, maxLon: b.MaxLon}
}

// boxIndex files boxes under the grid cells they cover.
type boxIndex[K comparable] struct {
	cells map[cellKey][]K
	boxes map[K]box64
	big   map[K]struct{}
}

func newBoxIndex[K comparable]() boxIndex[K] {
	return boxIndex[K]{cells: map[cellKey][]K{}, boxes: map[K]box64{}, big: map[K]struct{}{}}
}

// gridded reports whether b is filed in cells (true) or on the big list.
func gridded(b box64) bool {
	if b.hasNaN() || b.minLat > b.maxLat || b.minLon > b.maxLon {
		return false
	}
	_, _, nLat := cellSpan(b.minLat, b.maxLat)
	_, _, nLon := cellSpan(b.minLon, b.maxLon)
	return nLat*nLon <= memBigBoxCells
}

func (x *boxIndex[K]) add(k K, b box64) {
	x.boxes[k] = b
	if !gridded(b) {
		x.big[k] = struct{}{}
		return
	}
	la0, la1, _ := cellSpan(b.minLat, b.maxLat)
	lo0, lo1, _ := cellSpan(b.minLon, b.maxLon)
	for la := la0; la <= la1; la++ {
		for lo := lo0; lo <= lo1; lo++ {
			c := cellKey{la, lo}
			x.cells[c] = append(x.cells[c], k)
		}
	}
}

func (x *boxIndex[K]) remove(k K) {
	b, ok := x.boxes[k]
	if !ok {
		return
	}
	delete(x.boxes, k)
	if !gridded(b) {
		delete(x.big, k)
		return
	}
	la0, la1, _ := cellSpan(b.minLat, b.maxLat)
	lo0, lo1, _ := cellSpan(b.minLon, b.maxLon)
	for la := la0; la <= la1; la++ {
		for lo := lo0; lo <= lo1; lo++ {
			c := cellKey{la, lo}
			ks := x.cells[c]
			for i, have := range ks {
				if have == k {
					ks[i] = ks[len(ks)-1]
					ks = ks[:len(ks)-1]
					break
				}
			}
			if len(ks) == 0 {
				delete(x.cells, c)
			} else {
				x.cells[c] = ks
			}
		}
	}
}

// query calls visit for every box that meets q, inclusive at both ends. A key
// may be visited more than once.
func (x *boxIndex[K]) query(q box64, visit func(K)) {
	if q.hasNaN() {
		return // a NaN bound compares false with everything
	}
	la0, la1, nLat := cellSpan(q.minLat, q.maxLat)
	lo0, lo1, nLon := cellSpan(q.minLon, q.maxLon)
	if q.minLat > q.maxLat || q.minLon > q.maxLon || nLat*nLon > float64(len(x.boxes)) {
		for k, b := range x.boxes {
			if b.meets(q) {
				visit(k)
			}
		}
		return
	}
	for la := la0; la <= la1; la++ {
		for lo := lo0; lo <= lo1; lo++ {
			for _, k := range x.cells[cellKey{la, lo}] {
				if x.boxes[k].meets(q) {
					visit(k)
				}
			}
		}
	}
	for k := range x.big {
		if x.boxes[k].meets(q) {
			visit(k)
		}
	}
}
