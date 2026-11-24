package merge

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A cancel stops one bridge's gap integral within a bound. Pricing a bridge
// integrates over every canon edge and source arc in its gap, and
// EnvelopeMinCost is O(n^2) in the pieces those make; on a 270k-point leg one
// bridge ran for 1h29m after a cancel (2026-10-26) because nothing inside it
// read ctx.

// denseGap is a canon edge with n source arcs running its whole length, a
// metre apart: every piece covers the whole edge, so every pair is compared.
// At 36,000 arcs one uncancelled integral takes about 9 s on a desktop machine.
func denseGap(n int) (a, b Point, srcArcs [][2]Point, edgeLenM float64) {
	a, b = mPt(-500, 0), mPt(500, 0)
	for i := 0; i < n; i++ {
		y := float64(i%40) * 0.5
		srcArcs = append(srcArcs, [2]Point{mPt(-500, y+0.1), mPt(500, y-0.1)})
	}
	return a, b, srcArcs, HaversineMetres(a.Lat, a.Lon, b.Lat, b.Lon)
}

func TestCanonGapCost_CancelStopsWithinASecond(t *testing.T) {
	a, b, src, l := denseGap(36_000)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := CanonGapCost(ctx, a, b, src, l, 0, 1, CorridorRadiusM, 2)
	took := time.Since(start)
	if err == nil {
		t.Fatalf("the integral ran to the end (%v) through a cancel at 100ms", took)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if after := took - 100*time.Millisecond; after > time.Second {
		t.Errorf("the gap integral stopped %v after the cancel; want under 1s", after)
	}
}

func TestImportGapCost_CancelStopsWithinASecond(t *testing.T) {
	a, b, canon, _ := denseGap(36_000)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	lenM := HaversineMetres(a.Lat, a.Lon, b.Lat, b.Lon)
	_, err := ImportGapCost(ctx, a, b, canon, lenM, CorridorRadiusM, 2)
	took := time.Since(start)
	if err == nil {
		t.Fatalf("the integral ran to the end (%v) through a cancel at 100ms", took)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if after := took - 100*time.Millisecond; after > time.Second {
		t.Errorf("the gap integral stopped %v after the cancel; want under 1s", after)
	}
}
