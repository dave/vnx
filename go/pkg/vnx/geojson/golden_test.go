package geojson

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

const (
	goldenPath    = "testdata/library.golden.geojson"
	regenerateEnv = "VNX_GEOJSON_REGENERATE_GOLDEN"
)

// TestFixtureMatchesGolden compares BYTES. Layout, key order, number rendering
// and the sorted arc order are all part of what this writer promises, and a
// structural comparison would let every one of them drift unnoticed.
//
// ⚠ REGENERATION DELIBERATELY FAILS THE RUN. Spec §6.1: a golden must never be
// refreshed as a side effect of making a suite green, because that deletes the
// only evidence of what the writer used to produce. The env var rewrites the
// file and then fails, so the diff has to be looked at by someone.
func TestFixtureMatchesGolden(t *testing.T) {
	var buf bytes.Buffer
	report, err := Write(&buf, fixture())
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	got := buf.Bytes()

	if os.Getenv(regenerateEnv) != "" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("%s was set: %s has been REWRITTEN from this run (%d bytes, %d features).\n"+
			"This run is failed on purpose, so that regenerating can never be the thing that makes a red test green.\n"+
			"Read `git diff` on the golden, satisfy yourself that every changed byte is a deliberate change, and re-run WITHOUT %s.",
			regenerateEnv, goldenPath, len(got), report.Emitted, regenerateEnv)
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v\nIf it is genuinely missing, capture it with %s=1 — do not write one by hand.",
			goldenPath, err, regenerateEnv)
	}
	if bytes.Equal(got, want) {
		return
	}
	t.Errorf("the fixture export no longer matches %s.\n"+
		"\n"+
		"This means ONE of two things and you have to decide which:\n"+
		"  1. A REGRESSION. The writer changed output it was not meant to change. Fix the code.\n"+
		"  2. A DELIBERATE CHANGE, in which case regenerate with %s=1 and commit the diff as its own visible decision.\n"+
		"\n%s", goldenPath, regenerateEnv, firstDifference(want, got))
}

// firstDifference names the line that diverged, because a whole-file dump of a
// FeatureCollection tells a reader nothing.
func firstDifference(want, got []byte) string {
	w := strings.Split(string(want), "\n")
	g := strings.Split(string(got), "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return "first difference at line " + itoa(i+1) + ":\n  want: " + wl + "\n  got:  " + gl
		}
	}
	return "the lines match but the bytes do not — check the trailing newline"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
