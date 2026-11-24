package merge

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"testing"
)

// The MemCanon benchmarks time a MemCanon at the scale the merge meets:
// several thousand arcs in one area plus one arc of about 2.8M pips through
// it (a long recording). Run once with
//
//	go test ./pkg/merge -run '^$' -bench '^BenchmarkMemCanon_' -benchtime=1x

const (
	benchArcs    = 6000
	benchBigPips = 2_800_000
	benchLat0    = 46.0
	benchLon0    = 7.5
	benchSpan    = 0.2 // degrees, about 20 km
	benchBigID   = ArcID(benchArcs + 1)
)

var (
	benchSnapOnce sync.Once
	benchSnap     MemCanonSnapshot
)

// benchSnapshot is the benchmarks' canon, built once. NewMemCanon copies it,
// so each benchmark can load its own.
func benchSnapshot() MemCanonSnapshot {
	benchSnapOnce.Do(func() {
		r := rand.New(rand.NewSource(3))
		s := &benchSnap
		nodeID := NodeID(0)
		addNode := func(lat, lon float64) Node {
			nodeID++
			n := Node{ID: nodeID, Lat: lat, Lon: lon, Ele: f64(1500)}
			s.Nodes = append(s.Nodes, n)
			return n
		}
		for i := range benchArcs {
			lat, lon := benchLat0+benchSpan*r.Float64(), benchLon0+benchSpan*r.Float64()
			pts := make([]Point, 2+r.Intn(200))
			for k := range pts {
				lat += 0.00005 * r.NormFloat64()
				lon += 0.00005 * r.NormFloat64()
				pts[k] = Point{Lat: lat, Lon: lon, Ele: f64(1500 + float64(k))}
			}
			start, end := addNode(pts[0].Lat, pts[0].Lon), addNode(pts[len(pts)-1].Lat, pts[len(pts)-1].Lon)
			interior, stats, bounds := arcFromPoints(pts, start, end)
			s.Arcs = append(s.Arcs, MemCanonArc{ID: ArcID(i + 1), StartNodeID: start.ID, EndNodeID: end.ID,
				Polyline: interior, Stats: stats, Bounds: bounds})
		}
		big := make([]Pip, benchBigPips)
		// The last pip is kept in the loop: go1.27.1's arm64 assembler fails on
		// big[benchBigPips-1] ("FLDPD 67199976(R0): constant is not in pool",
		// an offset too large for the instruction).
		var last Pip
		for k := range big {
			a := float64(k) / benchBigPips * 2 * math.Pi
			big[k] = Pip{Lat: benchLat0 + benchSpan/2 + benchSpan/2*math.Sin(37*a), Lon: benchLon0 + benchSpan/2 + benchSpan/2*math.Sin(41*a), Ele: 1500}
			last = big[k]
		}
		first := big[0]
		const endGap = 1e-5 // the end nodes sit just off the first and last pips
		bs, be := addNode(first.Lat-endGap, first.Lon), addNode(last.Lat+endGap, last.Lon)
		bigStats, bigBounds := ArcWalkStatsAndBounds(bs, be, big)
		s.Arcs = append(s.Arcs, MemCanonArc{ID: benchBigID, StartNodeID: bs.ID, EndNodeID: be.ID,
			Polyline: big, Stats: bigStats, Bounds: bigBounds})
		s.NextNodeID, s.NextArcID = nodeID+1, benchArcs+2
	})
	return benchSnap
}

func benchCanon(b *testing.B) *MemCanon {
	b.Helper()
	snap := benchSnapshot()
	c, err := NewMemCanon(snap)
	if err != nil {
		b.Fatal(err)
	}
	return c
}

// benchProbe is a 25 m node-search box at a random point of the area.
func benchProbe(r *rand.Rand) Bounds {
	lat, lon := benchLat0+benchSpan*r.Float64(), benchLon0+benchSpan*r.Float64()
	minLat, maxLat, minLon, maxLon := nodeSearchBox(lat, lon, 25)
	return Bounds{MinLat: minLat, MaxLat: maxLat, MinLon: minLon, MaxLon: maxLon}
}

// benchRows runs a read b.N times over fresh probes and reports rows per call.
func benchRows(b *testing.B, probe func(*rand.Rand) Bounds, read func(c *MemCanon, q Bounds) (int, error)) {
	c := benchCanon(b)
	r := rand.New(rand.NewSource(5))
	rows := 0
	b.ResetTimer()
	for range b.N {
		n, err := read(c, probe(r))
		if err != nil {
			b.Fatal(err)
		}
		rows += n
	}
	b.ReportMetric(float64(rows)/float64(b.N), "rows/op")
}

func BenchmarkMemCanon_Load(b *testing.B) {
	snap := benchSnapshot()
	b.ResetTimer()
	for range b.N {
		if _, err := NewMemCanon(snap); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemCanon_NodesInBox25m(b *testing.B) {
	benchRows(b, benchProbe, func(c *MemCanon, q Bounds) (int, error) {
		got, err := c.NodesInBox(context.Background(), q)
		return len(got), err
	})
}

func BenchmarkMemCanon_ArcIDsInRect25m(b *testing.B) {
	benchRows(b, benchProbe, func(c *MemCanon, q Bounds) (int, error) {
		got, err := c.ArcIDsInRect(context.Background(), q)
		return len(got), err
	})
}

func BenchmarkMemCanon_ArcIDsInRect1km(b *testing.B) {
	window := func(r *rand.Rand) Bounds {
		q := benchProbe(r)
		q.MaxLat += 0.009
		q.MaxLon += 0.013
		return q
	}
	benchRows(b, window, func(c *MemCanon, q Bounds) (int, error) {
		got, err := c.ArcIDsInRect(context.Background(), q)
		return len(got), err
	})
}

// BenchmarkMemCanon_ArcsNearPoint25m: the cost is building the 2.8M-vertex
// CanonArc whenever the big arc qualifies; bigarc/op is how often it does.
func BenchmarkMemCanon_ArcsNearPoint25m(b *testing.B) {
	big := 0
	benchRows(b, benchProbe, func(c *MemCanon, q Bounds) (int, error) {
		got, err := c.ArcsNearPoint(context.Background(), q)
		for _, a := range got {
			if a.ID == benchBigID {
				big++
			}
		}
		return len(got), err
	})
	b.ReportMetric(float64(big)/float64(b.N), "bigarc/op")
}

func BenchmarkMemCanon_ArcsNearPointEmpty(b *testing.B) {
	outside := func(r *rand.Rand) Bounds {
		q := benchProbe(r)
		q.MinLat, q.MaxLat = q.MinLat+benchSpan, q.MaxLat+benchSpan
		return q
	}
	benchRows(b, outside, func(c *MemCanon, q Bounds) (int, error) {
		got, err := c.ArcsNearPoint(context.Background(), q)
		return len(got), err
	})
}

// BenchmarkMemCanon_AddArc50: PrepareArc then AddArc of a 52-point arc,
// both ends minted.
func BenchmarkMemCanon_AddArc50(b *testing.B) {
	ctx := context.Background()
	c := benchCanon(b)
	r := rand.New(rand.NewSource(7))
	b.ResetTimer()
	for range b.N {
		lat, lon := benchLat0+benchSpan*r.Float64(), benchLon0+benchSpan*r.Float64()
		pts := make([]Point, 52)
		for k := range pts {
			pts[k] = Point{Lat: lat + 0.00003*float64(k), Lon: lon + 0.00002*float64(k)}
		}
		a, err := PrepareArc(ctx, c, pts, nil, nil)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := c.AddArc(ctx, a); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMemCanon_SplitBigArc: splitArcAt at the middle of the 2.8M-pip
// arc, on a fresh canon each time (the load is not timed).
func BenchmarkMemCanon_SplitBigArc(b *testing.B) {
	ctx := context.Background()
	for range b.N {
		b.StopTimer()
		c := benchCanon(b)
		b.StartTimer()
		if _, err := splitArcAt(ctx, c, benchBigID, 0.5); err != nil {
			b.Fatal(err)
		}
	}
}
