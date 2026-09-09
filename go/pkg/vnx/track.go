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
	Started  *time.Time
	Ended    *time.Time
	Style    ChainStyle
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
	if len(s.Pips) == 0 {
		return 0, false
	}
	first, last := s.Pips[0].Time, s.Pips[len(s.Pips)-1].Time
	if first == nil || last == nil {
		return 0, false
	}
	return time.Duration(*last-*first) * time.Millisecond, true
}

var trackKeys = map[string]bool{
	"folder": true, "name": true, "notes": true, "hidden": true,
	"started": true, "ended": true, "style": true, "segments": true, "data": true,
}

func decodeTracks(arr []any, path string, diags []Diagnostic) ([]Track, []Diagnostic) {
	out := make([]Track, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, ok := item.(map[string]any)
		if !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("track is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		var tr Track
		tr.Folder, diags = stringField(obj, "folder", diags)
		tr.Name, diags = stringField(obj, "name", diags)
		tr.Notes, diags = stringField(obj, "notes", diags)
		tr.Hidden, diags = boolField(obj, "hidden", at, diags)
		tr.Started, diags = timeField(obj, "started", at, diags)
		tr.Ended, diags = timeField(obj, "ended", at, diags)
		tr.Style, diags = decodeChainStyle(obj, at, diags)
		tr.Segments, diags = decodeSegments(obj["segments"], at+"/segments", diags)
		tr.Data, diags = dataField(obj, at, diags)
		diags = reportUnknownKeys(obj, at, trackKeys, diags)
		out = append(out, tr)
	}
	return out, diags
}

func decodeSegments(v any, path string, diags []Diagnostic) ([]Segment, []Diagnostic) {
	if v == nil {
		return nil, diags
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("`segments` is %s, expected an array — ignored", jsonKind(v))})
	}
	out := make([]Segment, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, isObj := item.(map[string]any)
		if !isObj {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("segment is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		pips, diags2 := decodePips(obj["pips"], at+"/pips", diags)
		diags = diags2
		out = append(out, Segment{Pips: pips})
	}
	return out, diags
}

// decodePips reads a position list. An unusable position drops THAT POSITION,
// not the whole run — one bad fix in a day's recording should cost one fix,
// which is the same trade importer/gpx.go makes with InvalidPointCount.
func decodePips(v any, path string, diags []Diagnostic) ([]Position, []Diagnostic) {
	if v == nil {
		return nil, diags
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path,
			Message: fmt.Sprintf("`pips` is %s, expected an array — ignored", jsonKind(v))})
	}
	out := make([]Position, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		posArr, isArr := item.([]any)
		if !isArr {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("pip is %s, expected an array — dropped", jsonKind(item))})
			continue
		}
		p, err := decodePosition(posArr, at)
		if err != nil {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: err.Error() + " — pip dropped"})
			continue
		}
		out = append(out, p)
	}
	return out, diags
}

// timeField reads an optional record-level timestamp. An unusable one drops the
// TIMESTAMP and keeps the item: a track whose `started` is malformed still has
// all its geometry, and losing that would be a wildly disproportionate response
// to a bad date.
func timeField(obj map[string]any, key, path string, diags []Diagnostic) (*time.Time, []Diagnostic) {
	v, present := obj[key]
	if !present {
		return nil, diags
	}
	s, ok := v.(string)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path + "/" + key,
			Message: fmt.Sprintf("expected a timestamp string, found %s — ignored", jsonKind(v))})
	}
	t, err := parseTimestamp(s)
	if err != nil {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path + "/" + key,
			Code: CodeTimestampNotUTC, Message: err.Error() + " — ignored"})
	}
	return &t, diags
}

// ChainStyle is the appearance of anything drawn as a line — a chain, a route,
// or a track.
type ChainStyle struct {
	Color     string
	Opacity   *float64
	Width     *float64
	LineStyle string
}

var chainStyleKeys = map[string]bool{"color": true, "opacity": true, "width": true, "line-style": true}

func decodeChainStyle(obj map[string]any, path string, diags []Diagnostic) (ChainStyle, []Diagnostic) {
	var s ChainStyle
	st, ok := obj["style"].(map[string]any)
	if !ok {
		return s, diags
	}
	at := path + "/style"
	s.Color, diags = stringField(st, "color", diags)
	s.LineStyle, diags = stringField(st, "line-style", diags)
	s.Opacity, diags = floatField(st, "opacity", at, diags)
	s.Width, diags = floatField(st, "width", at, diags)
	diags = reportUnknownKeys(st, at, chainStyleKeys, diags)
	return s, diags
}
