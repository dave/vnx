package merge

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// raceLeg is a short straight leg, about 130 m east-west.
func raceLeg() []Point {
	pts := make([]Point, 20)
	for i := range pts {
		pts[i] = Point{Lat: 51.5, Lon: -0.1 + float64(i)*0.0001}
	}
	return pts
}

// raceCanon is a canon holding one lone node about 2 m north of raceLeg's
// middle, so the symmetric snap has a candidate and its observer fires.
func raceCanon() (*MemCanon, error) {
	return newMemCanon(MemCanonSnapshot{
		Nodes:      []Node{{ID: 1, Lat: 51.50002, Lon: -0.1 + 9.5*0.0001}},
		NextNodeID: 2,
		NextArcID:  1,
	})
}

// TestAddLeg_ConcurrentCallsSeeTheirOwnLabel runs AddLeg on two canons at
// once, each call with its own symmetric-snap observer on its own context.
// Each observer must fire once per leg and be told its own call's label,
// never the other call's. Run it under -race: when the label was package
// state, the detector flagged AddLeg's write of it, and the labels crossed.
func TestAddLeg_ConcurrentCallsSeeTheirOwnLabel(t *testing.T) {
	const legs = 200
	labels := []string{"A", "B"}
	seen := make([]map[string]int, len(labels))
	errs := make([]error, len(labels))
	var wg sync.WaitGroup
	for g, label := range labels {
		seen[g] = map[string]int{}
		ctx := WithTestHooks(context.Background(), TestHooks{
			SymmetricSnap: func(leg string, _ SymmetricSnapReport) { seen[g][leg]++ },
		})
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < legs; i++ {
				canon, err := raceCanon()
				if err != nil {
					errs[g] = err
					return
				}
				_, err = addLeg(ctx, canon, raceLeg(), LegOptions{FillElevation: NoFill, Label: label})
				if err != nil {
					errs[g] = fmt.Errorf("%s leg %d: %w", label, i, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	for g, label := range labels {
		if errs[g] != nil {
			t.Fatal(errs[g])
		}
		if seen[g][label] != legs || len(seen[g]) != 1 {
			t.Errorf("call %s's observer was told labels %v, want only %s, %d times", label, seen[g], label, legs)
		}
	}
}
