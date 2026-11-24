package merge

// corridorbridge_helpers_test.go — the one measurement helper that outlived
// the fabrication instrument.
//
// The rest of the corridor matcher's development instruments were deleted
// 2026-09-01 (the matcher has shipped and its
// constants are fixed).
//
// [corridorsolve_bridge_test.go] — a LIVE corridor test, not a development
// instrument — is the remaining caller.

import (
	"math"
)

// fabricationSampleStepM is the measure-length step [bridgedMetresForRun]
// samples at.
//
// 0.5 m against R = 15 m. Every sample's distance is EXACT; the only
// approximation is WHERE along the measure a distance>R crossing falls, via
// the midpoint rule, so worst-case error is bounded by one step per crossing —
// at most ~3.3% of R. This is an OFFLINE instrument: nothing here feeds a
// matching decision, so the constant is smuggled into how finely the probe
// reports, never into the thing being measured.
const fabricationSampleStepM = 0.5

// bridgedMetresForRun dense-samples one ON run's adopted canon measure
// [run.MeasureStart, run.MeasureEnd] and returns (measure length, bridged
// length): the metres of adopted canon that lie further than r from the source
// polyline, and are therefore drawn without a source point vouching for them.
//
// Sampling is midpoint, n = ceil(length/stepM) sub-intervals, so a bridged
// stretch is resolved to within one stepM at each end.
func bridgedMetresForRun(pc *PreppedCandidate, run followRun, srcPoly *hintPoly, r, stepM float64) (onM, bridgedM float64) {
	lo, hi := run.MeasureStart, run.MeasureEnd
	if lo > hi {
		lo, hi = hi, lo
	}
	length := hi - lo
	if length <= 0 {
		return 0, 0
	}
	n := int(math.Ceil(length / stepM))
	if n < 1 {
		n = 1
	}
	sub := length / float64(n)
	hint := 0
	for i := 0; i < n; i++ {
		mid := lo + (float64(i)+0.5)*sub
		lat, lon := pc.interpOnCanon(mid)
		var d float64
		d, hint = srcPoly.NearestDist(lat, lon, hint)
		if d > r {
			bridgedM += sub
		}
	}
	return length, bridgedM
}
