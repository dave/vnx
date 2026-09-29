package vnx

import (
	"fmt"
	"math"
	"strings"
)

// Position is one point on the wire: spec §4.1's fixed positional channels
// [lon, lat, ele, time_ms, ...].
//
// ⚠ COORDINATE ORDER IS lon, lat — GeoJSON order, and the REVERSE of the
// internal lat-first Pip and area.polygon. This is the single most likely
// transposition bug in the whole format, which is why the conformance corpus
// carries a case that renders visibly wrong when the two are swapped.
//
// Ele and Time are pointers because their absence is meaningful and common,
// not an error. Absent elevation is a first-class internal state with two
// on-disk encodings — a canonical NaN inside a pip blob, SQL NULL on a node row
// — and a pip inside a time-bearing track segment may legitimately carry no
// time at all, which is why the internal codec keeps a MinInt64 sentinel. That
// sentinel must NEVER reach the wire: it exceeds 2^53 and JavaScript would
// mangle it silently. On the wire, absent is null.
type Position struct {
	Lon, Lat float64
	Ele      *float64 // nil: elevation unknown
	Time     *int64   // nil: no time; otherwise Unix milliseconds
}

// Equal compares by value, following the pointers. Positions are compared by
// VALUE and not by identity throughout this package — spec §3.2 requires it for
// shared highlight handles, where two links referencing one turnaround point are
// recognised only by their coordinates being equal.
func (p Position) Equal(q Position) bool {
	if p.Lon != q.Lon || p.Lat != q.Lat {
		return false
	}
	if (p.Ele == nil) != (q.Ele == nil) || (p.Ele != nil && *p.Ele != *q.Ele) {
		return false
	}
	if (p.Time == nil) != (q.Time == nil) || (p.Time != nil && *p.Time != *q.Time) {
		return false
	}
	return true
}

func (p Position) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%v, %v", p.Lon, p.Lat)
	if p.Ele != nil {
		fmt.Fprintf(&b, ", %v", *p.Ele)
	} else if p.Time != nil {
		b.WriteString(", null")
	}
	if p.Time != nil {
		fmt.Fprintf(&b, ", %d", *p.Time)
	}
	b.WriteString("]")
	return b.String()
}

// decodePosition reads one position array. It returns an error rather than a
// diagnostic: a position that cannot be read is an unusable VALUE, and spec
// §6.2 has the caller drop the containing element and carry on. Deciding what
// to drop belongs to the caller, which knows what the position was part of.
func decodePosition(arr []any, path string) (Position, error) {
	if len(arr) < 2 {
		return Position{}, fmt.Errorf("position at %s has %d elements, needs at least 2 ([lon, lat])", path, len(arr))
	}
	lon, err := requiredNumber(arr[0], path, "longitude")
	if err != nil {
		return Position{}, err
	}
	lat, err := requiredNumber(arr[1], path, "latitude")
	if err != nil {
		return Position{}, err
	}
	if lon < -180 || lon > 180 {
		return Position{}, fmt.Errorf("longitude %v at %s is off the globe", lon, path)
	}
	if lat < -90 || lat > 90 {
		return Position{}, fmt.Errorf("latitude %v at %s is off the globe", lat, path)
	}
	p := Position{Lon: lon, Lat: lat}

	if len(arr) >= 3 {
		ele, ok, err := optionalNumber(arr[2], path, "elevation")
		if err != nil {
			return Position{}, err
		}
		if ok {
			p.Ele = &ele
		}
	}
	if len(arr) >= 4 {
		ms, ok, err := optionalNumber(arr[3], path, "time")
		if err != nil {
			return Position{}, err
		}
		if ok {
			t := int64(ms)
			p.Time = &t
		}
	}
	// Positions beyond index 3 are deliberately ignored: a reader MUST tolerate
	// channels it does not know, so a future fifth channel is additive rather
	// than a flag day. Not a diagnostic — this is expected, not a defect.
	return p, nil
}

func requiredNumber(v any, path, what string) (float64, error) {
	n, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("%s at %s is %s, expected a number", what, path, jsonKind(v))
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, fmt.Errorf("%s at %s is not finite", what, path)
	}
	return n, nil
}

// optionalNumber reads a channel that may be null. ok is false for null, which
// is how absence is spelled from index 2 up.
func optionalNumber(v any, path, what string) (n float64, ok bool, err error) {
	if v == nil {
		return 0, false, nil
	}
	n, err = requiredNumber(v, path, what)
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}
