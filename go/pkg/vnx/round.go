package vnx

import (
	"math"
	"strconv"
)

// Rounding at the serialisation boundary (spec §4.1).
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

// RoundCoordinate rounds a longitude or latitude for the wire.
func RoundCoordinate(v float64) float64 { return roundTo(v, CoordinateDecimals) }

// RoundElevation rounds an elevation for the wire.
func RoundElevation(v float64) float64 { return roundTo(v, ElevationDecimals) }

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
// NEGATIVE ZERO IS NORMALISED TO POSITIVE ZERO. -0.0000004 rounds to -0, which
// is the same value as 0 and carries no information; doing it here means all
// four writers agree without each remembering to.
func roundTo(v float64, places int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	// ⚠ FORMAT AND PARSE, NOT MULTIPLY AND DIVIDE. Spec §4.1 defines the
	// rounded value as a RESULT — the value with at most `places` decimals
	// nearest to the exact binary64, ties to even — and names
	// scale-round-unscale as non-conforming, because the scaled product is
	// itself a binary64 and is not always the exact scaled value. So the
	// multiplication manufactures ties the true decimal never had and destroys
	// ones it did.
	//
	// `math.RoundToEven(v*scale) / scale` disagrees with corpus/numbers/ on 175
	// of its rows: 0.15 gives 0.2 where the table says 0.1, 0.05 gives 0, 0.45
	// gives 0.4. Every one of them is a value §4.1 argues from. It also loses
	// precision by MAGNITUDE rather than at a tie, diverging from exact
	// arithmetic 2,902 times in the 1e14–1e15 range.
	//
	// strconv.FormatFloat is correctly rounded and breaks a tie to even, which
	// is the operation the table's generator uses, so the library and the table
	// now perform the same one rather than two that agree on the easy cases.
	s, err := strconv.ParseFloat(strconv.FormatFloat(v, 'f', places, 64), 64)
	if err != nil {
		// FormatFloat cannot produce something ParseFloat rejects for a finite
		// v, and v is finite here. Returning v unrounded would be a silent
		// fallback in a writer, so this is loud instead.
		panic("vnx: BUG: rounding " + strconv.FormatFloat(v, 'g', -1, 64) + ": " + err.Error())
	}
	r := s
	// ⚠ ROUNDING CREATES NEGATIVE ZEROS THAT WERE NEVER IN THE MODEL.
	// math.Round(-0.4) is -0, and -0/10 is -0, so rounding -0.04 to one place
	// yields a zero with its sign bit set — as does -4e-08 to six places. Spec
	// §4.1 requires rounding to happen in the emit path, so a writer that collapsed
	// -0 only across the values it was HANDED would emit `-0` for a value it
	// produced itself.
	//
	// `r == 0` is true for both zeros in Go, so this one comparison catches it.
	if r == 0 {
		return 0
	}
	return r
}
