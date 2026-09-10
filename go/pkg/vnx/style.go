package vnx

import "fmt"

// Style is an item's appearance, and it is OPAQUE.
//
// ⚠ IT IS A MAP AND NOT A STRUCT, and that is spec §3.10's ruling rather than a
// convenience. A style object is opaque in the way a data record body is: a
// reader carries every key through a read-modify-write and reports none of them
// as unrecognised. Style vocabulary grows faster than structural vocabulary, so
// a fixed key set would have every implementation diagnose a valid document the
// day one of them adds a style key, until all of them catch up.
//
// Typed structs lost two things at once, and the second is the larger bug:
//
//   - PRESENCE. `"style": {}` and no `style` key at all were the same value, so
//     a caller reading a document could not tell which it had been given. §3.1
//     lets a WRITER spell the two alike; it does not let a READER collapse them
//     before the caller sees them.
//   - EVERY KEY THE FORMAT HAS NOT NAMED. A struct with named fields silently
//     discarded anything else, in a region the specification declares opaque
//     and where §3.4's own example prints `"style": { }`. Nobody reported that
//     one, because a dropped key looks exactly like a key that was never there.
//
// The four other implementations model a style as an opaque value already;
// this package was the only one that did not.
type Style map[string]any

// String reads a string-valued style key. A missing key and a key of another
// type both read as absent, which is what a lossy converter wants: it is asking
// whether it has something usable, not diagnosing the document.
func (s Style) String(key string) (string, bool) {
	v, ok := s[key].(string)
	return v, ok
}

// Float reads a number-valued style key. JSON numbers arrive as float64 through
// the decoder and as int from a caller that built the map itself, so both are
// accepted.
func (s Style) Float(key string) (float64, bool) {
	switch v := s[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	}
	return 0, false
}

// Bool reads a boolean-valued style key.
func (s Style) Bool(key string) (bool, bool) {
	v, ok := s[key].(bool)
	return v, ok
}

// checkStyle validates the one style value this format states a range for, and
// nothing else.
//
// ⚠ `importance` IS THE ONLY EXCEPTION TO OPACITY, and §3.10 says why: opaque
// means unrecognised keys are not reported, not that a value is exempt from a
// range this document states normatively. §3.6 states one for `importance` — an
// integer from 1 to 5 — and it applies wherever `importance` appears in a STYLE,
// including a style key this format does not otherwise document.
//
// It does NOT apply inside a data record body. §6.2 forbids inspecting one, and
// that prohibition is the mechanism by which a record of an unrecognised type
// survives a read-modify-write, so it wins.
//
// `icon-size` is deliberately unchecked. No range is stated for it, so
// `"icon-size": "large"` passes unremarked — four seats have arrived at that
// question independently and the answer is the same each time.
func checkStyle(s Style, at string, diags []Diagnostic) (Style, []Diagnostic) {
	v, present := s["importance"]
	if !present {
		return s, diags
	}
	n, ok := v.(float64)
	if !ok || n != float64(int(n)) || n < 1 || n > 5 {
		delete(s, "importance")
		return s, append(diags, Diagnostic{Severity: Warning, Path: at + "/importance",
			Code:    CodeWrongTypeOptional,
			Message: fmt.Sprintf("`importance` is %v, expected an integer from 1 to 5 — field dropped, item kept", v)})
	}
	return s, diags
}

// decodeStyle reads an item's optional `style` object.
//
// An absent key gives a nil Style and an empty object gives a non-nil empty one,
// so the encoder can tell them apart (§3.1).
func decodeStyle(obj map[string]any, path string, diags []Diagnostic) (Style, []Diagnostic) {
	v, present := obj["style"]
	if !present {
		return nil, diags
	}
	st, ok := v.(map[string]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path + "/style",
			Code:    CodeWrongTypeOptional,
			Message: fmt.Sprintf("`style` is %s, expected an object — field dropped, item kept", jsonKind(v))})
	}
	return checkStyle(Style(st), path+"/style", diags)
}
