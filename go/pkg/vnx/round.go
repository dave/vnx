package vnx

import "math"

// Rounding at the serialisation boundary (Dave's ruling, 2026-09-03).
//
// Every writer rounds coordinates to CoordinateDecimals and elevations to
// ElevationDecimals AT THE WRITE. A [Document] holds full binary64 precision in
// memory and Decode reads it back at full precision; only the encoders round.
//
// ⚠ THE NUMBERS ARE NOT A STYLE CHOICE. 6 decimal places of latitude is about
// 11 cm on the ground; a consumer GPS is accurate to ±3–5 m. Everything below
// the sixth place is the arithmetic's noise, not the instrument's reading, and
// publishing `-3.269679000000008` as though it were a measurement is a claim
// the data cannot support. It is also most of the file: a fully populated pip
// is 57 bytes at full precision and 27 at 6/1.
//
// ⚠ ROUND LAST, AFTER ALL GEOMETRY LOGIC. Ring winding and the arc-boundary
// dedup in gpx.routePips and geojson.appendJoined compare positions BY FULL
// VALUE, and SubPolyline cuts an arc at a chain's endpoints by projecting onto
// it. Rounding before any of them would change which points those comparisons
// consider equal, or move the point a cut lands on, which is a change to the
// geometry rather than to its spelling. Every call below therefore sits in the
// emit path, never in the model.
//
// ⚠ ONE IMPLEMENTATION, FOUR CALLERS — vnx, geojson, gpx and kml. The same
// discipline DocumentIsEmpty follows and for the same reason: four writers that
// can answer differently eventually do, and a coordinate that differs in the
// last place between two exports of one library is exactly the kind of
// disagreement nobody notices until a diff is unreadable.
const (
	// CoordinateDecimals is the precision longitude and latitude are written
	// at, in decimal places. ~11 cm.
	CoordinateDecimals = 6
	// ElevationDecimals is the precision elevation is written at, in decimal
	// places. ~10 cm, which is finer than any barometric or DEM source we read.
	ElevationDecimals = 1
)

// roundScale is the largest magnitude roundTo will scale. Above it a float64
// has no fractional part left to drop (the spacing at 1e15 is 0.125), so
// rounding is a no-op — and multiplying by the scale would be the only thing
// that could happen, which for a value near math.MaxFloat64 overflows to +Inf.
//
// ⚠ THAT OVERFLOW WOULD MANUFACTURE A REFUSAL. finite() and every writer's own
// non-finite check reject ±Inf, so without this bound an elevation of
// 1.7976931348623157e308 — which the oracle carries — would go from a written
// value to a failed export. A rounding helper must not change WHETHER a
// document can be written, only how many digits it takes.
const roundScale = 1e15

// RoundCoordinate rounds a longitude or latitude for the wire.
func RoundCoordinate(v float64) float64 { return roundTo(v, 1e6) }

// RoundElevation rounds an elevation for the wire.
func RoundElevation(v float64) float64 { return roundTo(v, 10) }

// RoundPosition returns p with its coordinates and elevation rounded for the
// wire. It COPIES: the elevation pointer is new, so a writer cannot round the
// caller's Document out from under it, and an ABSENT elevation stays absent —
// nil in, nil out. Rounding a missing elevation into 0.0 would put every
// unknown point at sea level, which is the fallback this format's *float64
// exists to prevent.
//
// Time is untouched. It is an integer count of milliseconds, and there is
// nothing below its last digit to round away.
func RoundPosition(p Position) Position {
	out := Position{
		Lon:  RoundCoordinate(p.Lon),
		Lat:  RoundCoordinate(p.Lat),
		Time: p.Time,
		// Channels is a spelling, not a value, and rounding does not change it.
		// Dropping it here silently un-did Position.Channels for every position
		// on the wire, because rounding sits between the model and the emitter.
		Channels: p.Channels,
	}
	if p.Ele != nil {
		e := RoundElevation(*p.Ele)
		out.Ele = &e
	}
	return out
}

// roundTo rounds v to the decimal grid `scale` names.
//
// ⚠ NaN AND ±Inf PASS THROUGH UNCHANGED, deliberately. Each writer refuses a
// non-finite coordinate with a message naming the channel; a helper that
// mapped one to 0 here would turn a caught bug into a point off the coast of
// Africa, reported as a successful export.
//
// The result is the float64 nearest to an exact n/scale, which is the same
// float64 that parsing the n-over-scale decimal string yields — so the shortest
// round-tripping spelling of it, which is what every writer here emits, has at
// most that many decimal places. That is the whole mechanism by which the
// digits actually leave the file.
//
// NEGATIVE ZERO IS NORMALISED TO POSITIVE ZERO. -0.0000004 rounds to -0, and
// canonical form spells that `0` (canonical.go's ECMAScript rule); doing it
// here means all four writers agree without each remembering to.
func roundTo(v float64, scale float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) >= roundScale {
		return v
	}
	r := math.Round(v*scale) / scale
	if r == 0 {
		return 0
	}
	return r
}
