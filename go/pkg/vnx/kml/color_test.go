package kml

import (
	"math"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

func TestColorInvertsTheByteOrderAndFoldsOpacity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		hex     string
		opacity *float64
		want    string
		ok      bool
	}{
		{name: "opaque", hex: "#E04535", want: "ff3545e0", ok: true},
		{name: "no hash", hex: "E04535", want: "ff3545e0", ok: true},
		{name: "lower case", hex: "#e04535", want: "ff3545e0", ok: true},
		{name: "shorthand", hex: "#f00", want: "ff0000ff", ok: true},
		{name: "half opacity", hex: "#000000", opacity: ptr(0.5), want: "80000000", ok: true},
		{name: "fully transparent", hex: "#ffffff", opacity: ptr(0.0), want: "00ffffff", ok: true},
		{name: "fully opaque", hex: "#ffffff", opacity: ptr(1.0), want: "ffffffff", ok: true},
		{name: "empty", hex: "", ok: false},
		{name: "not hex", hex: "#zzzzzz", ok: false},
		{name: "wrong length", hex: "#12345", ok: false},
		// An out-of-range opacity is UNUSABLE, not clamped: clamping would turn
		// a meaningless number into a plausible picture.
		{name: "opacity above one", hex: "#ffffff", opacity: ptr(1.5), ok: false},
		{name: "negative opacity", hex: "#ffffff", opacity: ptr(-0.1), ok: false},
		{name: "NaN opacity", hex: "#ffffff", opacity: ptr(math.NaN()), ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Color(tc.hex, tc.opacity)
			if ok != tc.ok {
				t.Fatalf("Color(%q, %v) ok = %v, want %v", tc.hex, tc.opacity, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("Color(%q, %v) = %q, want %q", tc.hex, tc.opacity, got, tc.want)
			}
		})
	}
}

// ⚠ THE BYTE ORDER IS THE ONE THING A WELL-FORMEDNESS CHECK CANNOT SEE. A
// writer emitting aarrggbb produces a file every parser accepts and every
// renderer draws in the wrong colour, so this asserts the swap directly rather
// than only through the golden.
func TestColorIsNotJustAlphaPrependedToTheHex(t *testing.T) {
	got, ok := Color("#112233", nil)
	if !ok {
		t.Fatal("Color refused a valid colour")
	}
	if strings.HasSuffix(got, "112233") {
		t.Fatalf("Color(%q) = %q — the RGB bytes were NOT reversed; KML is AABBGGRR", "#112233", got)
	}
	if got != "ff332211" {
		t.Fatalf("Color(%q) = %q, want ff332211", "#112233", got)
	}
}

// An unusable colour must omit the whole <Style>, never substitute one. A
// defaulted colour is a silent lie about what the user chose.
func TestUnusableColourOmitsTheStyleRatherThanDefaultingIt(t *testing.T) {
	out, _ := writeDoc(t, &vnx.Document{Plans: []vnx.Plan{plan(vnx.Plan{
		Name:  "Ridge",
		Style: vnx.Style{"color": "not-a-colour"}},
		lineArc{Pips: []vnx.Position{pos(-3, 57), pos(-3.1, 57.1)}},
	)}})
	if strings.Contains(out, "<Style>") {
		t.Fatalf("an unparseable colour produced a <Style>:\n%s", out)
	}
	if !strings.Contains(out, "<LineString>") {
		t.Fatalf("the item itself was dropped along with its colour:\n%s", out)
	}
}
