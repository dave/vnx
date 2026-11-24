package merge_test

import (
	"context"
	"fmt"

	"github.com/dave/vnx/go/pkg/merge"
)

// Two legs merge into an empty memory canon. The second retraces part of the
// first, so it shares the first leg's line instead of adding a copy of it.
func ExampleAddLeg() {
	ctx := context.Background()
	canon, err := merge.NewMemCanon(merge.MemCanonSnapshot{})
	if err != nil {
		panic(err)
	}
	opts := merge.LegOptions{FillElevation: merge.NoFill}

	// A 1 km line running east.
	first := []merge.Point{{Lat: 54.5, Lon: -3.0}, {Lat: 54.5, Lon: -2.9845}}
	if _, err := merge.AddLeg(ctx, canon, first, opts); err != nil {
		panic(err)
	}
	snap, err := canon.Snapshot()
	if err != nil {
		panic(err)
	}
	fmt.Printf("after the first leg: %d nodes, %d arcs\n", len(snap.Nodes), len(snap.Arcs))

	// Before the second leg, keep a snapshot: a failed leg poisons the canon.
	before := snap
	second := []merge.Point{{Lat: 54.5, Lon: -2.995}, {Lat: 54.5, Lon: -2.99}, {Lat: 54.51, Lon: -2.99}}
	if _, err := merge.AddLeg(ctx, canon, second, opts); err != nil {
		if canon, err = merge.NewMemCanon(before); err != nil {
			panic(err)
		}
	}
	if snap, err = canon.Snapshot(); err != nil {
		panic(err)
	}
	fmt.Printf("after the second leg: %d nodes, %d arcs\n", len(snap.Nodes), len(snap.Arcs))
	// Output:
	// after the first leg: 2 nodes, 1 arcs
	// after the second leg: 5 nodes, 4 arcs
}
