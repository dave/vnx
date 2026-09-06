package kml

import (
	"math"
	"strconv"
	"strings"
)

// Color converts a Viatrail "#RRGGBB" colour and an optional 0..1 opacity into
// the "aabbggrr" string KML wants. ok is false when nothing usable can be
// built, and the caller MUST then omit the whole <Style> element.
//
// ⚠ IT IS THE INVERSE OF importer.ConvertKMLColor AND IT IS WRITTEN OUT AGAIN
// HERE ON PURPOSE. This package is stdlib-only (spec 5b §4.1) and
// pkg/core/importer is not reachable from it — importing it would drag `db` in
// behind it and break the reference package's whole premise. Two
// implementations of one conversion is a real cost; the alternative was worse.
//
// ⚠ KML'S BYTE ORDER IS BACKWARDS FROM OURS AND FROM EVERY OTHER FORMAT HERE:
// alpha, blue, green, red. A writer that emitted "aarrggbb" produces a file
// that opens without complaint and draws every red trail blue, which no test
// that only checks well-formedness can see.
//
// ⚠ AN UNUSABLE COLOUR OMITS THE STYLE AND NEVER DEFAULTS TO ONE (spec 5b T5).
// A defaulted colour is a silent lie about what the user chose: they would see
// a red line in Google Earth for a track they had made blue, with nothing
// anywhere saying a colour had been substituted. Omitting the Style leaves
// Google Earth's own default, which is visibly "no colour was carried".
//
// ⚠ AN OPACITY OUTSIDE 0..1 IS UNUSABLE, NOT CLAMPED. Clamping is the fallback
// shape the house rule forbids: it would turn a meaningless number into a
// plausible picture. NaN fails the same range test, because NaN compares false
// against everything.
func Color(hex string, opacity *float64) (string, bool) {
	rgb, ok := parseHex(hex)
	if !ok {
		return "", false
	}
	alpha := 255
	if opacity != nil {
		if !(*opacity >= 0 && *opacity <= 1) {
			return "", false
		}
		alpha = int(math.Round(*opacity * 255))
	}
	return byteHex(alpha) + byteHex(rgb[2]) + byteHex(rgb[1]) + byteHex(rgb[0]), true
}

// parseHex reads "#RRGGBB" or the "#RGB" shorthand, with or without the hash,
// in either case. The shorthand is accepted because `vnx convert` reads files
// this app did not write, and refusing a colour every CSS parser accepts would
// drop a style the document plainly carried.
func parseHex(s string) ([3]int, bool) {
	s = strings.TrimPrefix(s, "#")
	switch len(s) {
	case 3:
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
	default:
		return [3]int{}, false
	}
	var out [3]int
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = int(v)
	}
	return out, true
}

func byteHex(v int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[(v>>4)&0xf], digits[v&0xf]})
}
