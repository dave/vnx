package kml

import (
	"bytes"
	"os"
	"testing"
)

// The golden file (spec 5b §6.1), compared BYTE FOR BYTE.
//
// ⚠ REGENERATION FAILS THE RUN ON PURPOSE. If regenerating could leave the
// suite green, then "make the test pass" and "rewrite the reference until it
// agrees" become the same gesture, and the golden stops being evidence of
// anything. This is spec 5's pattern and it held for fifteen tasks.
const (
	goldenPath = "testdata/library.golden.kml"
	regenerate = "VNX_KML_REGENERATE_GOLDEN"
)

func TestFixtureMatchesGolden(t *testing.T) {
	var buf bytes.Buffer
	report, err := WriteWith(&buf, fixture(), fixtureOptions())
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	got := buf.Bytes()

	if os.Getenv(regenerate) != "" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("%s was set: %s has been REWRITTEN from this run (%d bytes, %d objects).\n"+
			"This run is failed on purpose, so that regenerating can never be the thing that makes a red test green.\n"+
			"Read `git diff` on the golden, satisfy yourself that every changed byte is a deliberate change, and re-run WITHOUT %s.",
			regenerate, goldenPath, len(got), report.Emitted, regenerate)
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v\nIf it is genuinely missing, capture it with %s=1 — do not write one by hand.",
			goldenPath, err, regenerate)
	}
	if bytes.Equal(got, want) {
		return
	}
	t.Errorf("the fixture no longer writes %s.\n\n"+
		"This means ONE of two things and you have to decide which:\n"+
		"  1. A REGRESSION — the writer changed output it was not meant to change. Fix the code.\n"+
		"  2. A DELIBERATE change — then regenerate with %s=1 and review the diff byte by byte.\n\n"+
		"MAKING THIS GREEN BY REWRITING THE REFERENCE IS THE ONE THING THAT IS NOT ALLOWED.\n\n"+
		"--- want (%d bytes) ---\n%s\n--- got (%d bytes) ---\n%s",
		goldenPath, regenerate, len(want), want, len(got), got)
}
