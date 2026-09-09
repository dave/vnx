package vnx

import (
	"strings"
	"unicode/utf8"
)

// canonicalString renders s as a canonical VNX string (spec §4.5.5): escape
// only what RFC 8259 requires, short escapes where they exist, lowercase
// \u00xx otherwise, everything else raw.
//
// ⚠ NEITHER json.Marshal NOR json.Encoder CAN PRODUCE THESE BYTES, measured
// 2026-09-09 — which is why this exists rather than a flag on the stdlib.
//
//	character        json.Marshal      SetEscapeHTML(false)   §4.5.5 wants
//	< > &            < ...        raw                    raw
//	U+2028 U+2029      ...          ... (still)     raw
//	backspace, FF    short escape      short escape           short escape
//	other controls   lowercase \u00xx  lowercase \u00xx       lowercase \u00xx
//	non-ASCII        raw               raw                    raw
//
// The line-separator pair is the one no option reaches: Go escapes U+2028 and
// U+2029 unconditionally, because they are legal in JSON and illegal inside a
// JavaScript string literal, so Go would rather its output could be pasted into
// a script tag. That is a sound default for a general encoder and the wrong
// answer for a format that pins its bytes.
//
// ⚠ THE REPORTED DEFECT WAS THE WRONG ONE, and the correction is worth keeping:
// Go was said to write backspace and form feed as  and  where three
// other implementations write \b and \f. It does not — it writes the short
// escapes, and has all along. Go's real divergence is the HTML trio and the
// line separators, which no other implementation escapes. Measured before
// changing anything.
//
// §4.5.6 already says every implementation writes its own serialiser because of
// key order. Strings are the second reason, and Go is not exempt from either.
func canonicalString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				const hex = "0123456789abcdef"
				b.WriteString(`\u00`)
				b.WriteByte(hex[(r>>4)&0xf])
				b.WriteByte(hex[r&0xf])
				continue
			}
			// A byte that is not valid UTF-8 arrives here as RuneError, which
			// is what a range loop substitutes. Writing it out reproduces the
			// replacement character rather than the original byte; that is the
			// same thing every JSON encoder does, and a VNX document is UTF-8
			// by §4.5.5 so the input was already invalid.
			if r == utf8.RuneError {
				b.WriteRune(utf8.RuneError)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
