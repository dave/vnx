package vnx

import (
	"fmt"
	"time"
)

// Segment is one contiguous run of a recording.
//
// ⚠ THE GAP BETWEEN SEGMENT k AND k+1 IS A PAUSE. It is never geometry and
// must never be drawn or walked. That is why segments stay separate here
// instead of being concatenated into one point list — concatenating them draws
// a straight line across wherever the walker stopped for lunch.
type Segment struct {
	Pips []Position
}

// Span is the time from a segment's first pip to its last. known is false when
// the segment carries no time, or holds fewer than two timed pips — a single
// fix has no span.
func (s Segment) Span() (d time.Duration, known bool) {
	var first, last *int64
	for i := range s.Pips {
		if t := s.Pips[i].Time; t != nil {
			if first == nil {
				first = t
			}
			last = t
		}
	}
	if first == nil || last == nil || first == last {
		return 0, false
	}
	return time.Duration(*last-*first) * time.Millisecond, true
}

// Track is a recorded hike.
type Track struct {
	Folder   string
	Name     string
	Notes    string
	Hidden   bool
	Pinned   Timestamp // "" is not pinned (§3.8)
	Started  Timestamp // "" is absent
	Ended    Timestamp // "" is absent
	Style    Style
	Segments []Segment
	Data     []Record
}

// MovingTime derives the figure the app shows: the sum over segments of
// (last pip time - first pip time).
//
// It is deliberately NOT the wall-clock span between Started and Ended, which
// the pauses make longer. The app's own `duration_s` is moving time for exactly
// this reason — wall clock is recoverable from the two stamps at any point,
// moving time is not, because the fixes it comes from are consumed at
// finalisation. Deriving it here rather than carrying it keeps the format free
// of cached arithmetic a reader would have to trust over its own.
//
// ⚠ known is FALSE when no segment carries time — an imported GPX track has no
// pip times at all. The caller must show that as unknown and never as zero:
// zero moving time is a plausible-looking wrong answer, which is worse than an
// absent one. Returning (0, false) rather than just 0 is what makes the
// distinction impossible to ignore at the call site.
func (t Track) MovingTime() (d time.Duration, known bool) {
	// ⚠ EVERY SEGMENT, OR NOTHING. Spec §3.5 makes moving time derivable only
	// when every segment carries a time on its first and last pip, and a
	// PARTIAL sum is the answer it forbids most strongly — worse than the zero
	// it also forbids, because a partial sum is indistinguishable from a
	// complete one and no caller can tell which it was given, where zero is at
	// least recognisable as suspicious.
	//
	// This summed the segments that had times and returned known. Two other
	// implementations did the same, independently, and no corpus tier could see
	// it: every track in the corpus is fully timed or fully untimed.
	//
	// A track with no segments is unknown for the same reason, which falls out
	// of the loop rather than needing a case.
	if len(t.Segments) == 0 {
		return 0, false
	}
	for _, seg := range t.Segments {
		span, ok := seg.timedSpan()
		if !ok {
			return 0, false
		}
		d += span
	}
	return d, true
}

// timedSpan is Span with the single-pip case answered rather than conflated.
//
// ⚠ Span RETURNS (0, false) FOR A SEGMENT WITH ONE TIMED PIP, which reads as
// "no times" and is not. Spec §3.5 asks whether the FIRST and LAST pip carry
// times; in a one-pip segment they are the same pip, so the segment is timed and
// its span is legitimately zero. Distinguishing the two matters now that a
// single untimed segment makes a whole track unknown: conflating them would turn
// every track containing a one-pip segment unknown, which is a different wrong
// answer from the one being fixed.
func (s Segment) timedSpan() (time.Duration, bool) {
	// ⚠ AN EMPTY SEGMENT IS NOT AN UNMEASURED SPAN. It has no span to measure,
	// so it contributes zero and does not make the track unknown. Spec §3.5 says
	// so explicitly rather than leaving it implicit, because the moving-time
	// rule reads either way and two libraries would have differed on a track
	// containing an empty segment with nothing red.
	if len(s.Pips) == 0 {
		return 0, true
	}
	first, last := s.Pips[0].Time, s.Pips[len(s.Pips)-1].Time
	if first == nil || last == nil {
		return 0, false
	}
	return time.Duration(*last-*first) * time.Millisecond, true
}

var trackKeys = map[string]bool{
	"folder": true, "name": true, "notes": true, "hidden": true, "pinned": true,
	"started": true, "ended": true, "style": true, "segments": true, "data": true,
}

// trackFromObject builds one track from its object, read with every key but
// `segments`, which the streaming reader has read already: segs and segDiags
// are its result, and segDiags take `segments`' place in the order.
func trackFromObject(obj map[string]any, at string, segs []Segment, segDiags, diags []Diagnostic) (Track, []Diagnostic) {
	var tr Track
	tr.Folder, diags = stringField(obj, "folder", diags)
	tr.Name, diags = stringField(obj, "name", diags)
	tr.Notes, diags = stringField(obj, "notes", diags)
	tr.Hidden, diags = boolField(obj, "hidden", at, diags)
	tr.Pinned, diags = timeField(obj, "pinned", at, diags)
	tr.Started, diags = timeField(obj, "started", at, diags)
	tr.Ended, diags = timeField(obj, "ended", at, diags)
	tr.Style, diags = decodeStyle(obj, at, diags)
	tr.Segments, diags = segs, append(diags, segDiags...)
	tr.Data, diags = dataField(obj, at, diags)
	diags = reportUnknownKeys(obj, at, trackKeys, diags)
	return tr, diags
}

// segmentKeys is the one key a segment carries (SPEC §3.10).
var segmentKeys = map[string]bool{"pips": true}

// timeField reads an optional record-level timestamp. An unusable one drops the
// TIMESTAMP and keeps the item: a track whose `started` is malformed still has
// all its geometry, and losing that would be a wildly disproportionate response
// to a bad date. The check is §4.3's shape and nothing more: a value that names
// no calendar instant is kept, verbatim.
func timeField(obj map[string]any, key, path string, diags []Diagnostic) (Timestamp, []Diagnostic) {
	v, present := obj[key]
	if !present {
		return "", diags
	}
	s, ok := v.(string)
	if !ok {
		return "", append(diags, Diagnostic{Severity: Warning, Path: path + "/" + key,
			Message: fmt.Sprintf("expected a timestamp string, found %s — ignored", jsonKind(v))})
	}
	t := Timestamp(s)
	if err := t.Valid(); err != nil {
		return "", append(diags, Diagnostic{Severity: Warning, Path: path + "/" + key,
			Code: CodeTimestampNotUTC, Message: err.Error() + " — ignored"})
	}
	return t, diags
}
