package merge

import (
	"errors"
	"fmt"
	"math"

	"github.com/dave/vnx/go/pkg/detmath"
)

// ErrWalkHasNoLength marks a candidate whose vertices are all coincident — an
// arc that is geometrically a POINT.
//
// It is a sentinel rather than a plain error because its two readers want
// opposite things. [ProjectProbeOntoWalk] cannot answer "where on this walk"
// for it (Fraction would be 0/0) and must say so. [NearestCanonArc]
// must SKIP it, because such arcs are ordinary committed geometry and
// raising would abort a valid import over an arc it was never going to
// choose. Without the sentinel the caller would have to match on error text,
// or swallow every projection failure to survive this one — and the second is
// how the two genuine contract violations would go silent.
var ErrWalkHasNoLength = errors.New("canon vertex walk has zero length")

// NearestCanonArc projects the probe (lat, lon) onto each candidate's vertex
// walk and returns the nearest one within radiusM (`<=`), ties to the earlier
// candidate (strict `<`). A zero-length candidate is skipped, not raised.
func NearestCanonArc(candidates []CanonArc, lat, lon, radiusM float64) (NearbyCanonArc, bool, error) {
	var (
		best  NearbyCanonArc
		bestD = math.MaxFloat64
		found bool
	)
	for _, cand := range candidates {
		proj, err := ProjectProbeOntoWalk(cand.Vertices, lat, lon)
		if errors.Is(err, ErrWalkHasNoLength) {
			// NOT a candidate, and NOT an error. An arc whose vertices are
			// all coincident is a POINT: it has no interior for a terminal to
			// T onto, so it cannot answer the question this function asks.
			// Skipping it is the correct answer, not a swallowed failure.
			//
			// It has to be skipped rather than raised because such arcs are
			// ORDINARY COMMITTED GEOMETRY — a GPX route of two or more
			// identical points imports and commits today — and raising aborted
			// a whole unrelated import. Reproduced: the degenerate arc only
			// has to fall in the probe's search BOX while sitting outside the
			// 15 m disc. Inside the disc it was already harmless, because its
			// own endpoints are nodes and the caller's node search declines first.
			// That corner region is the entire blast radius, and it made a
			// legal input fail a valid import.
			//
			// Nothing is lost by skipping. The degenerate arc's endpoints
			// ARE nodes, so a terminal genuinely near it is served by
			// the caller's node search and the endpoint warp — the path that already
			// handles "there is a node here", which is all this geometry is.
			//
			// The other two degenerate conditions stay ERRORS: a walk under two
			// vertices and a non-finite vertex both violate the candidate
			// list's contract, so they are impossible input
			// rather than legal geometry that fails to qualify.
			continue
		}
		if err != nil {
			return NearbyCanonArc{}, false, fmt.Errorf(
				"project probe onto canon arc %d: %w", cand.ID, err,
			)
		}
		if proj.DistM <= radiusM && proj.DistM < bestD {
			bestD = proj.DistM
			best = NearbyCanonArc{
				ID:        cand.ID,
				DistM:     proj.DistM,
				Fraction:  proj.Fraction,
				Lat:       proj.Lat,
				Lon:       proj.Lon,
				PriorLenM: proj.PriorLenM,
				NextLenM:  proj.NextLenM,
			}
			found = true
		}
	}
	if !found {
		return NearbyCanonArc{}, false, nil
	}
	return best, true, nil
}

// CanonWalkProjection is the foot of the perpendicular from a probe onto one
// candidate's vertex walk. Fields mirror [NearbyCanonArc] minus the id,
// which the caller supplies.
type CanonWalkProjection struct {
	DistM     float64
	Lat, Lon  float64
	PriorLenM float64
	NextLenM  float64
	Fraction  float64

	// SegIdx is the index of the LEG the foot of the perpendicular fell on:
	// the leg from verts[SegIdx] to verts[SegIdx+1], so it is in
	// [0, len(verts)-2].
	//
	// This is the INSERTION POSITION — where a caller splices a vertex at
	// (Lat, Lon) into the walk — and it is a different question from "which
	// vertex is nearest the probe". On a leg kilometres long the foot can
	// sit mid-leg while the nearest vertex is at either end; SegIdx answers
	// the first, [NodeNearPath].PointIdx answers the second, and neither
	// substitutes for the other.
	//
	// It is reported HERE, by the projection itself, rather than re-derived
	// by a caller from PriorLenM against a cumulative array of its own:
	// position along the walk and distance to the walk are the same
	// measurement, and a caller that rebuilt the cumsum would be a second
	// along-walk ruler in a second function, free to diverge.
	//
	// A foot landing exactly on a shared vertex reaches it as the earlier
	// leg's t=1 before the later leg's t=0, at an identical cumulative
	// distance, and the strict `<` below keeps the earlier — so SegIdx names
	// the EARLIER of the two legs there. That is the same tie the function's
	// own doc already describes for Lat/Lon; SegIdx does not introduce a new
	// one.
	SegIdx int

	// LegT is where the foot fell on that leg: 0 at verts[SegIdx], 1 at
	// verts[SegIdx+1], exactly 0 or exactly 1 when the foot is clamped to an
	// end of the leg (then Lat and Lon are that vertex, bit for bit). It is
	// the t the function solved, so a caller never re-derives it from
	// distances.
	LegT float64
}

// ProjectProbeOntoWalk returns the point on `verts` closest to
// (probeLat, probeLon), its haversine distance, and its position along the
// walk's cumulative haversine length — as an arc length (PriorLenM /
// NextLenM / Fraction) and as the index of the leg it fell on (SegIdx).
// A check that measures a node against EVERY arc, not just the nearest,
// calls it directly, so that it measures on the same ruler as the merge.
//
// Frame. Each leg is solved in a local equirectangular frame CENTRED ON THE
// PROBE — the probe is the origin, latitude scales by a fixed metres-per-
// degree and longitude by that times cos(probe latitude). At the 15 m scales
// the T-junction split works at, the frame's departure from the sphere is far below a
// millimetre. Longitude DIFFERENCES are wrapped into [-180, 180] before
// scaling, so a leg on the far side of the ±180 seam is 11 m away rather
// than 40 000 km; the interpolated longitude is wrapped back on the way out.
//
// The frame is genuinely degenerate within metres of a pole, where cos(lat)
// collapses and points a couple of metres apart differ by up to 180° of
// longitude. There the leg parameter it picks may not be the true metric
// optimum. What holds everywhere is that DistM is the exact haversine
// distance to the point actually returned, so the caller's radius test and
// every nearest-wins comparison are made on a real distance and never on a
// frame artefact — the same division of labour as the node search's box and
// refine. Trail data rarely visits the poles; this is stated so a
// future reader does not mistake the approximation for an exact solve.
//
// t is clamped to [0, 1] so a perpendicular falling beyond a leg resolves to
// the nearer VERTEX, and at t of exactly 0 or 1 that vertex's stored
// coordinates are returned bitwise rather than through a lerp that merely
// rounds to them. A vertex shared by two legs is reached as the first leg's
// t=1 and the second's t=0 with identical cumulative distance, so the strict
// `<` that keeps the earlier leg changes nothing.
//
// Degenerate input errors rather than reaching the caller as a clean miss —
// but the three cases do NOT all mean the same thing, and the caller treats
// them differently.
//
// Two are impossible input and stay hard errors here and above. A walk of
// fewer than two vertices breaks the candidate list's contract
// ([start node, pips…, end node] is at least two). A vertex that is not a
// finite coordinate on the globe loses every float comparison, so the
// candidate would quietly never win.
//
// The third — a walk of zero total length — is different, and calling it an
// error cost a valid import. Fraction really would be 0/0, so this function
// still cannot answer and still returns [ErrWalkHasNoLength]; but such an
// arc is ordinary committed geometry (a GPX route of two or more
// identical points imports today), so [NearestCanonArc] SKIPS it
// rather than propagating. See the skip's comment for why nothing is lost.
//
// A zero-length LEG — two identical consecutive vertices inside a walk that
// does have length — is not degenerate at all: it is ordinary imported
// geometry, it contributes no length, and it is handled below without
// dividing by zero.
func ProjectProbeOntoWalk(verts []Point, probeLat, probeLon float64) (CanonWalkProjection, error) {
	return ProjectProbeOntoWalkRange(verts, probeLat, probeLon, 0, len(verts)-2)
}

// WalkProjector is a walk checked, and its cumulative distances built, once,
// for a caller that projects many probes onto one walk (an editor drag, one
// projection per finger move). Project gives exactly what
// [ProjectProbeOntoWalk] gives: that function is NewWalkProjector then
// Project, so the two cannot disagree.
type WalkProjector struct {
	verts []Point
	cum   []float64 // cumulative haversine distance to each vertex
	total float64
}

// NewWalkProjector checks verts and builds the cumulative distances. It
// returns [ProjectProbeOntoWalk]'s errors for a degenerate walk, including
// [ErrWalkHasNoLength]. verts is kept, not copied: the caller must not change
// it.
func NewWalkProjector(verts []Point) (*WalkProjector, error) {
	if len(verts) < 2 {
		return nil, fmt.Errorf(
			"canon vertex walk has %d vertices, want at least 2", len(verts),
		)
	}
	for i, v := range verts {
		if math.IsNaN(v.Lat) || math.IsInf(v.Lat, 0) || v.Lat < -90 || v.Lat > 90 {
			return nil, fmt.Errorf(
				"canon vertex %d latitude %v is not a coordinate in [-90, 90]", i, v.Lat,
			)
		}
		if math.IsNaN(v.Lon) || math.IsInf(v.Lon, 0) || v.Lon < -180 || v.Lon > 180 {
			return nil, fmt.Errorf(
				"canon vertex %d longitude %v is not a coordinate in [-180, 180]", i, v.Lon,
			)
		}
	}

	// Cumulative haversine distance to each vertex, built exactly as
	// SplitPolylineAtFraction builds it, so the fraction returned here lands
	// where that function expects it.
	cum := make([]float64, len(verts))
	for i := 1; i < len(verts); i++ {
		cum[i] = cum[i-1] + HaversineMetres(
			verts[i-1].Lat, verts[i-1].Lon,
			verts[i].Lat, verts[i].Lon,
		)
	}
	total := cum[len(cum)-1]
	if total == 0 {
		return nil, fmt.Errorf(
			"%w: every one of its %d vertices is at (%v, %v)",
			ErrWalkHasNoLength, len(verts), verts[0].Lat, verts[0].Lon,
		)
	}
	return &WalkProjector{verts: verts, cum: cum, total: total}, nil
}

// Length is the walk's cumulative haversine length, the axis PriorLenM and
// NextLenM are measured on.
func (w *WalkProjector) Length() float64 {
	return w.total
}

// Project is [ProjectProbeOntoWalk] on the projector's walk.
func (w *WalkProjector) Project(probeLat, probeLon float64) (CanonWalkProjection, error) {
	return w.projectRange(probeLat, probeLon, 0, len(w.verts)-2)
}

// projectRange is [ProjectProbeOntoWalk] with the SEARCH — and
// only the search — restricted to legs [loSeg, hiSeg] inclusive.
//
// ⚠ THE CUMULATIVE ARRAY IS STILL BUILT OVER THE WHOLE WALK. The window
// narrows which legs may win; it does not renumber the axis. PriorLenM,
// NextLenM and Fraction therefore mean the same thing whatever the window is,
// which is the property [SplitPolylineAtFraction] consumes. Building the
// cumsum over the window instead would return a fraction of the window, and
// the caller would split the arc somewhere else entirely.
//
// An empty or inverted window is a caller bug and errors: it is the shape a
// mis-mapped locator has, and returning "no leg won" from it would read as
// "the probe is nowhere near this arc".
func (w *WalkProjector) projectRange(
	probeLat, probeLon float64, loSeg, hiSeg int,
) (CanonWalkProjection, error) {
	const metresPerDeg = math.Pi * 6371000.0 / 180.0
	verts, cum, total := w.verts, w.cum, w.total

	// detmath, and a float64 conversion on each product that meets an add
	// or a subtract: the same bits on arm64, which fuses x*y+z into one FMA,
	// as on amd64 (pkg/detmath).
	cosLat := detmath.Cos(probeLat * math.Pi / 180.0)

	if loSeg < 0 || hiSeg > len(verts)-2 || loSeg > hiSeg {
		return CanonWalkProjection{}, fmt.Errorf(
			"projection window [%d, %d] is not a leg range of a %d-vertex walk",
			loSeg, hiSeg, len(verts),
		)
	}

	out := CanonWalkProjection{DistM: math.MaxFloat64}
	for i := loSeg + 1; i <= hiSeg+1; i++ {
		a, b := verts[i-1], verts[i]

		// Probe-centred frame: the probe is (0, 0).
		aDLon := wrapLonDelta(a.Lon - probeLon)
		bDLon := wrapLonDelta(b.Lon - probeLon)
		ax, ay := float64(aDLon*metresPerDeg*cosLat), float64((a.Lat-probeLat)*metresPerDeg)
		bx, by := float64(bDLon*metresPerDeg*cosLat), float64((b.Lat-probeLat)*metresPerDeg)

		dx, dy := bx-ax, by-ay
		lenSq := float64(dx*dx) + float64(dy*dy)

		// A zero-length leg has no direction to project onto; its one point
		// is the answer for it, at t = 0.
		//
		// This guard changes no result and no test detects its removal
		// (measured): without it t is 0/0 = NaN, every comparison below
		// fails, and the leg is skipped — but the duplicated vertex is also
		// an endpoint of the adjacent leg, which reaches the same point with
		// the same cumulative distance. It stays because "skipped because
		// NaN loses every comparison" is the silent-fallback reasoning this
		// file exists to keep out, and it stops being harmless the moment
		// anyone uses t before that comparison.
		t := 0.0
		if lenSq > 0 {
			t = -(float64(ax*dx) + float64(ay*dy)) / lenSq
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
		}

		var pLat, pLon float64
		switch t {
		case 0:
			pLat, pLon = a.Lat, a.Lon
		case 1:
			pLat, pLon = b.Lat, b.Lon
		default:
			pLat = a.Lat + float64(t*(b.Lat-a.Lat))
			pLon = wrapLon(probeLon + (aDLon + float64(t*(bDLon-aDLon))))
		}

		d := HaversineMetres(probeLat, probeLon, pLat, pLon)
		if d < out.DistM {
			// At a vertex the prior length is that vertex's own, not
			// cum[i-1] + 1*(cum[i]-cum[i-1]), which can miss cum[i] by an
			// ulp: so the walk's end node (every leg after it of zero
			// length) is Fraction exactly 1, and its start exactly 0.
			prior := cum[i-1]
			if t == 1 {
				prior = cum[i]
			} else if t > 0 {
				prior = cum[i-1] + float64(t*(cum[i]-cum[i-1]))
			}
			out = CanonWalkProjection{
				DistM:     d,
				Lat:       pLat,
				Lon:       pLon,
				PriorLenM: prior,
				NextLenM:  total - prior,
				SegIdx:    i - 1,
				LegT:      t,
			}
		}
	}

	// Computed from the two halves rather than from `total`, so the identity
	// the caller's StubM relies on holds exactly.
	out.Fraction = out.PriorLenM / (out.PriorLenM + out.NextLenM)
	return out, nil
}

// ProjectProbeOntoWalkRange is [WalkProjector.projectRange] on a walk
// checked and summed for this one call.
func ProjectProbeOntoWalkRange(
	verts []Point, probeLat, probeLon float64, loSeg, hiSeg int,
) (CanonWalkProjection, error) {
	w, err := NewWalkProjector(verts)
	if err != nil {
		return CanonWalkProjection{}, err
	}
	return w.projectRange(probeLat, probeLon, loSeg, hiSeg)
}

// wrapLonDelta maps the difference of two longitudes onto the shorter way
// round the cylinder. Both inputs are in [-180, 180] (validated above and by
// the storage layer), so the difference is in [-360, 360] and one
// conditional adjustment is exact and sufficient.
//
// This is not a tolerance and not a fallback: -359.9999° and +0.0001° are
// the same displacement, and only the second is a distance.
func wrapLonDelta(d float64) float64 {
	if d > 180 {
		return d - 360
	}
	if d < -180 {
		return d + 360
	}
	return d
}

// wrapLon brings an interpolated longitude back into [-180, 180]. The input
// is a probe longitude in [-180, 180] plus a delta in [-180, 180], so again
// one adjustment covers it.
func wrapLon(lon float64) float64 {
	if lon > 180 {
		return lon - 360
	}
	if lon < -180 {
		return lon + 360
	}
	return lon
}
