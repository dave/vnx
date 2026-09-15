package vnx

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Timestamp is a record-level timestamp (§4.3): `started` and `ended` on a
// track. The empty value is absent.
//
// ⚠ IT IS A STRING, AND IT IS KEPT VERBATIM. §4.3 checks a record-level
// timestamp for SHAPE ONLY and forbids a reader to refuse or diagnose one on
// calendar grounds: `2026-13-45T99:99:99Z` is accepted. A reader that parsed
// into time.Time could not hold that value at all, and one that formatted on
// the way out re-spelled `.250Z` as `.25Z`, which the accept tier's tree
// comparison rightly calls a different document. Parsing is the caller's
// business, through Time, and so is what to do with a date no calendar has.
type Timestamp string

// timestampShape is §4.3's grammar, the same pattern the JSON Schema states.
// ASCII digits only.
var timestampShape = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?Z$`)

// Valid reports whether t matches §4.3's grammar. It checks shape and nothing
// else, so a calendar-invalid value is valid.
func (t Timestamp) Valid() error {
	s := string(t)
	switch {
	case s == "":
		return errors.New("empty timestamp")
	case !strings.HasSuffix(s, "Z"):
		return fmt.Errorf("timestamp %q must be UTC with a Z suffix", s)
	case !timestampShape.MatchString(s):
		return fmt.Errorf("timestamp %q must be YYYY-MM-DDTHH:MM:SS with optional fractional seconds and a Z suffix", s)
	}
	return nil
}

// Time parses t as a calendar instant in UTC. It fails on a value outside
// §4.3's grammar and on one that matches the grammar but names no instant,
// which a document may legally carry.
func (t Timestamp) Time() (time.Time, error) {
	if err := t.Valid(); err != nil {
		return time.Time{}, err
	}
	tm, err := time.Parse(time.RFC3339Nano, string(t))
	if err != nil {
		return time.Time{}, fmt.Errorf("timestamp %q: %w", string(t), err)
	}
	return tm.UTC(), nil
}

// TimestampOf spells tm in §4.3's grammar: UTC, `Z`, and fractional seconds
// only when there are any, with trailing zeros trimmed.
//
// ⚠ A SECONDS-ONLY LAYOUT TRUNCATES SILENTLY. A caller holding UnixMilli would
// lose the milliseconds on the way out and read back a different instant.
func TimestampOf(tm time.Time) Timestamp {
	tm = tm.UTC()
	if tm.Nanosecond() == 0 {
		return Timestamp(tm.Format("2006-01-02T15:04:05Z"))
	}
	return Timestamp(tm.Format("2006-01-02T15:04:05.999999999Z"))
}
