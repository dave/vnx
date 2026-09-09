package vnx

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
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

	// Channels is how many positional channels the DOCUMENT carried for this
	// position, and it is load-bearing model state rather than bookkeeping.
	//
	// ⚠ [lon, lat, null] and [lon, lat] say the same thing and a reader takes
	// either, but they are different BYTES, and re-encoding a document must
	// reproduce the bytes it read. Spec §4.1 makes shortening a rule for a
	// WRITER emitting a model, not part of canonical form: a formatter
	// re-renders the spelling the document carries, exactly as §4.5.4 makes
	// rounding an emit-path rule that a reader and a formatter never apply.
	//
	// So a model that normalises [lon, lat, null] to [lon, lat] cannot
	// re-encode the accept corpus, and `Ele *float64` alone IS that model —
	// which is the obvious modelling choice every implementation will make
	// first. accept/absent-elevation-and-time.vnx is the file that catches it.
	//
	// Zero means "not read from a document": a position built in code carries
	// no spelling, so a writer emits the shortest one. Decode sets it, capped
	// at 4 — a reader ignores channels beyond those it knows (§4.1), and a
	// channel it ignored is one it cannot reproduce, so an accept file may not
	// carry more than four.
	Channels int
}

// Equal compares by value, following the pointers. Positions are compared by
// VALUE and not by identity throughout this package — spec §3.2 requires it for
// shared chain handles, where two links referencing one turnaround point are
// recognised only by their coordinates being equal.
// Channels is deliberately NOT compared: it is a spelling, not a value, and
// two positions differing only in whether an absent elevation was written as
// `null` denote the same point. A chain's shared turnaround handle is
// recognised by coordinates alone (§3.3.3) and must stay so.
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
	p := Position{Lon: lon, Lat: lat, Channels: min(len(arr), 4)}

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

// parseTimestamp reads a record-level timestamp. Spec §4.3 permits ISO 8601 in
// UTC with a Z suffix and optional fractional seconds — nothing else. An offset
// like +01:00 is refused deliberately: pinning one grammar is cheaper than five
// implementations each discovering their platform's date parser the hard way.
func parseTimestamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty timestamp")
	}
	if !strings.HasSuffix(s, "Z") {
		return time.Time{}, fmt.Errorf("timestamp %q must be UTC with a Z suffix", s)
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}
