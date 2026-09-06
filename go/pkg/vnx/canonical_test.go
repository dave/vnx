package vnx

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"testing"
)

const examplePath = "../../../corpus/accept/example-network.vnx"

// The corpus lives here, in the repo, and this is its first file. The copy
// under ~/src/knowledge is the design artefact; from now on THIS one is
// authoritative, and once F14 exists it becomes generated rather than
// hand-maintained — a hand-written file trusted as canonical is exactly what
// produced the number contradiction the audit found.
func TestExampleIsCanonical(t *testing.T) {
	want, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Canonical(want)
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the corpus example is not in canonical form; first difference at byte %d", firstDiff(got, want))
	}
}

func TestCanonicalIsIdempotent(t *testing.T) {
	src, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	once, err := Canonical(src)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Canonical(once)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(once, twice) {
		t.Fatalf("Canonical is not idempotent; differs at byte %d", firstDiff(once, twice))
	}
}

// The real work: a document whose keys arrive in any order must canonicalise to
// the same bytes. Without this the canonical form buys nothing — an agent could
// not tell its own edit from formatting churn, and neither could a reviewer.
func TestShuffledKeysCanonicaliseIdentically(t *testing.T) {
	src, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Canonical(src)
	if err != nil {
		t.Fatal(err)
	}
	// json.Marshal of a map[string]any emits keys sorted lexicographically,
	// which is a DIFFERENT order from ours (scalars first), so re-marshalling
	// genuinely shuffles relative to canonical form.
	var tree any
	if err := json.Unmarshal(src, &tree); err != nil {
		t.Fatal(err)
	}
	shuffled, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(shuffled, want) {
		t.Fatal("fixture is not actually shuffled — this test would pass vacuously")
	}
	got, err := Canonical(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("shuffled input did not reproduce canonical form; differs at byte %d", firstDiff(got, want))
	}
}

func TestCanonicalOrdersScalarsBeforeContainers(t *testing.T) {
	src := []byte(`{"zebra":[1],"apple":{"x":1},"name":"n","created":"c","aardvark":[2]}`)
	got, err := Canonical(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"created\": \"c\",\n  \"name\": \"n\",\n  \"aardvark\": [2],\n  \"apple\": {\n    \"x\": 1\n  },\n  \"zebra\": [1]\n}\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Arrays are meaningful everywhere they appear — pips, links, a route's arcs,
// segments — so canonicalisation MUST NOT reorder them.
func TestCanonicalNeverReordersArrays(t *testing.T) {
	src := []byte(`{"a":[3,1,2],"b":["z","a"]}`)
	got, err := Canonical(src)
	if err != nil {
		t.Fatal(err)
	}
	// Exact output, not a substring probe. Both arrays must keep their order,
	// and the number array must be inline while the string array is not — the
	// inline rule is "numbers and nulls only", so a string array proves the
	// rule is classifying by content rather than just collapsing short arrays.
	want := "{\n  \"a\": [3, 1, 2],\n  \"b\": [\n    \"z\",\n    \"a\"\n  ]\n}\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// ECMAScript number rendering, which Go's encoding/json implements and Python's
// json does not. Measured 2026-08-31: Python emits 340.0, 1e-06 and 1e-07 where
// Go and JS emit 340, 0.000001 and 1e-7.
func TestCanonicalNumberRendering(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`{"n":340.0}`, "340"},
		{`{"n":1.0}`, "1"},
		{`{"n":388.2}`, "388.2"},
		{`{"n":0.000001}`, "0.000001"},
		{`{"n":1e-7}`, "1e-7"},
		{`{"n":1778914991000}`, "1778914991000"},
	} {
		got, err := Canonical([]byte(c.in))
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if !bytes.Contains(got, []byte(": "+c.want+"\n")) {
			t.Errorf("%s rendered as %s, want %s", c.in, bytes.TrimSpace(got), c.want)
		}
	}
}

// Canonical form is VALUE-preserving, not text-preserving, and the difference
// is worth pinning. -3.6712345678901234 and -3.6712345678901235 are the SAME
// binary64 (verified: both are 0xc00d5eb03aa8431e); ECMAScript rendering emits
// the shorter spelling. So two documents that differ only in how a number was
// spelled canonicalise to identical bytes — which is precisely what makes the
// canonical form useful, and precisely what a text-preserving rule could not do.
func TestCanonicalNormalisesEquivalentSpellings(t *testing.T) {
	a, err := Canonical([]byte(`{"n":-3.6712345678901234}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Canonical([]byte(`{"n":-3.6712345678901235}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("equivalent spellings did not converge:\n%s\n%s", a, b)
	}
	// And the value must survive: re-parse and compare bits, not text.
	var got struct{ N float64 }
	if err := json.Unmarshal(a, &got); err != nil {
		t.Fatal(err)
	}
	if math.Float64bits(got.N) != math.Float64bits(-3.6712345678901234) {
		t.Fatalf("value changed: %b", math.Float64bits(got.N))
	}
}

func TestCanonicalRefusesNonFinite(t *testing.T) {
	if _, err := Canonical([]byte(`{"n":NaN}`)); err == nil {
		t.Fatal("NaN must be refused")
	}
}

func firstDiff(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// The inline rule (§4.5), which is what stops a document being six lines per
// position — and therefore fifty thousand lines for a day's recorded track.
//
// It classifies by CONTENT, not by key: an array holding only numbers and nulls
// goes on one line, wherever it appears. That matters because the formatter is
// not allowed to know which key is a position — a number array inside an opaque
// `data` record must be laid out by the same rule, since nothing here may
// inspect a record body beyond its @type.
func TestCanonicalPositionsAreOneLine(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{
			"a position",
			`{"at":[-3.6712,57.118,340]}`,
			"{\n  \"at\": [-3.6712, 57.118, 340]\n}\n",
		},
		{
			// An interior absence is spelled null. If null broke the inline
			// rule, a file's SHAPE would depend on its DATA — the same position
			// would lay out one way with an elevation and another without.
			"nulls count as numbers",
			`{"at":[-3.6712,57.118,null,1778569391000]}`,
			"{\n  \"at\": [-3.6712, 57.118, null, 1778569391000]\n}\n",
		},
		{
			"a list of positions keeps one per line",
			`{"pips":[[1,2],[3,4]]}`,
			"{\n  \"pips\": [\n    [1, 2],\n    [3, 4]\n  ]\n}\n",
		},
		{
			"strings are not numbers",
			`{"a":["x","y"]}`,
			"{\n  \"a\": [\n    \"x\",\n    \"y\"\n  ]\n}\n",
		},
		{
			"a mixed array is not inline",
			`{"a":[1,"x"]}`,
			"{\n  \"a\": [\n    1,\n    \"x\"\n  ]\n}\n",
		},
		{
			// The formatter cannot inspect a data record's body, so the rule
			// has to reach it by content alone.
			"a number array inside an opaque record",
			`{"data":[{"@type":"com.example.x","v":[1,2,3]}]}`,
			"{\n  \"data\": [\n    {\n      \"@type\": \"com.example.x\",\n      \"v\": [1, 2, 3]\n    }\n  ]\n}\n",
		},
		{
			"an empty array stays empty",
			`{"a":[]}`,
			"{\n  \"a\": []\n}\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := Canonical([]byte(c.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}

// The layout is a cross-language contract, so it is worth pinning what it is
// actually worth. Measured on the seeded library, 2026-09-01: 1,277 lines and
// 29,073 bytes became 641 lines and 20,909. This asserts the mechanism rather
// than the file, so it cannot rot when the seed changes.
func TestInlinePositionsRoughlyHalveTheFile(t *testing.T) {
	src, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Canonical(src)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Count(got, []byte("\n"))
	// Every position in the example is one line. If the inline rule regressed,
	// each would become at least four and the file would grow by more than half.
	if lines > 700 {
		t.Fatalf("the example canonicalises to %d lines; the inline rule has regressed", lines)
	}
}

// Negative zero is the one case where Go's encoder disagrees with the
// reference. `json.Marshal(-0.0)` emits `-0`; `JSON.stringify(-0)` emits `0`,
// and §4.5 pins the format to ECMAScript. The two spell the same value, so a
// disagreement here is byte-incompatibility bought for no information.
func TestNegativeZeroRendersAsZero(t *testing.T) {
	got, err := Canonical([]byte(`{"a":-0.0,"b":[-0.0,0.0]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": 0,\n  \"b\": [0, 0]\n}\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
