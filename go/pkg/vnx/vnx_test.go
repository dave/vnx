package vnx

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeEnvelope(t *testing.T) {
	src := `{"vnx":"0.9.0","generator":"Viatrail 0.1.93","created":"2026-08-31T06:45:12Z"}`
	doc, diags, err := Decode([]byte(src))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("clean document produced %d diagnostics: %v", len(diags), diags)
	}
	if doc.Version != "0.9.0" || doc.Generator != "Viatrail 0.1.93" {
		t.Fatalf("envelope not carried: %+v", doc)
	}
}

func TestDecodeRefusals(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want error
	}{
		{"newer minor", `{"vnx":"0.10.0"}`, ErrUnsupportedVersion},
		{"newer major", `{"vnx":"1.0.0"}`, ErrUnsupportedVersion},
		{"two components", `{"vnx":"0.1"}`, ErrUnsupportedVersion},
		{"four components", `{"vnx":"0.1.0.0"}`, ErrUnsupportedVersion},
		{"non-numeric patch", `{"vnx":"0.1.x"}`, ErrUnsupportedVersion},
		{"empty patch", `{"vnx":"0.1."}`, ErrUnsupportedVersion},
		{"signed patch", `{"vnx":"0.1.+1"}`, ErrUnsupportedVersion},
		{"missing version", `{}`, ErrNotVNX},
		{"not an object", `[]`, ErrNotVNX},
		{"malformed json", `{`, ErrMalformed},
		{"version not a string", `{"vnx":1}`, ErrNotVNX},
		{"unparseable version", `{"vnx":"zero point one"}`, ErrUnsupportedVersion},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, _, err := Decode([]byte(c.src))
			if !errors.Is(err, c.want) {
				t.Fatalf("got err %v, want %v", err, c.want)
			}
			if doc != nil {
				t.Fatalf("refused document must not be returned, got %+v", doc)
			}
		})
	}
}

// TestOlderMinorAccepted pins the other half of §2.1: a reader accepts a MAJOR
// and MINOR at or below its own. Only NEWER is refused.
// TestOlderMinorRefused pins spec §2.2's symmetry, which reversed this test.
//
// ⚠ THROUGH 0.x EACH MINOR IS ITS OWN SERIES. The minor number is a breaking
// axis there, which means a construct may have CHANGED MEANING between 0.0 and
// 0.1 rather than merely having been added — so a 0.1 reader can no more assume
// it understands 0.0 than 0.2. This test asserted the opposite, on the reading
// that an earlier version is a subset of a later one. That reading is what a
// breaking axis denies.
func TestOlderMinorRefused(t *testing.T) {
	for _, declared := range []string{"0.1.0", "0.1.99", "0.0.9"} {
		_, _, err := Decode([]byte(`{"vnx":"` + declared + `"}`))
		if err == nil {
			t.Errorf("%s must be refused by a %s reader that supports the 0.9 series only", declared, Version)
			continue
		}
		// The refusal must name the version, so whoever holds the file knows
		// what they are holding rather than only that it failed.
		if !strings.Contains(err.Error(), declared) {
			t.Errorf("refusal does not name the version: %v", err)
		}
	}
}

// TestVersionRefusalSaysWhichWay pins that each version refusal names its own
// reason, so a caller can tell the person holding the file the true remedy. A
// newer file needs a newer reader; an older-series file needs an older one, and
// updating does not help; a declaration that is not a version is damage. All
// three are still ErrUnsupportedVersion, so a caller that matches only that
// sentinel behaves as before.
//
// ⚠ EACH CASE ASSERTS THE TWO SIBLINGS ARE ABSENT. A refinement that every
// refusal carried would pass the positive half and still collapse the three
// reasons into one.
func TestVersionRefusalSaysWhichWay(t *testing.T) {
	reasons := []error{ErrNewerVersion, ErrOlderVersion, ErrInvalidVersion}
	cases := []struct {
		declared string
		want     error
	}{
		{"0.10.0", ErrNewerVersion},
		{"1.0.0", ErrNewerVersion},
		{"0.1.0", ErrOlderVersion},
		{"0.1.99", ErrOlderVersion},
		{"0.0.9", ErrOlderVersion},
		{"0.1", ErrInvalidVersion},
		{"9.9", ErrInvalidVersion},
		{"0.1.x", ErrInvalidVersion},
		{"zero point one", ErrInvalidVersion},
	}
	for _, c := range cases {
		t.Run(c.declared, func(t *testing.T) {
			_, _, err := Decode([]byte(`{"vnx":"` + c.declared + `"}`))
			if !errors.Is(err, ErrUnsupportedVersion) {
				t.Fatalf("got %v, want an ErrUnsupportedVersion", err)
			}
			for _, r := range reasons {
				if got, want := errors.Is(err, r), r == c.want; got != want {
					t.Errorf("errors.Is(err, %v) = %v, want %v; err = %v", r, got, want, err)
				}
			}
		})
	}
}

// TestNewerPatchAccepted pins the third component's rule, which is the OPPOSITE
// of MINOR's: PATCH is not a breaking axis, so a 0.1.0 reader accepts a document
// written by a 0.1.3 writer and reads it with the rules it has. Anything 0.1.3
// added arrives as an unknown-key diagnostic (§6.2), not as a refusal.
//
// A reader that compared all three components would refuse this, and the refusal
// would be indistinguishable to a user from the correct 0.9.0 one — which is why
// it has its own test rather than riding on TestOlderMinorAccepted.
func TestNewerPatchAccepted(t *testing.T) {
	// ⚠ EVERY CASE IS IN THIS READER'S OWN MINOR SERIES. "0.0.99" used to be
	// here, which made the test's name wrong about what it covered: that is a
	// lower minor carrying a high patch, and spec §2.2 now refuses it, so it was
	// never testing the patch rule. It moved to TestOlderMinorRefused.
	for _, declared := range []string{"0.9.0", "0.9.1", "0.9.3", "0.9.99"} {
		doc, _, err := Decode([]byte(`{"vnx":"` + declared + `"}`))
		if err != nil {
			t.Fatalf("%s must be accepted by a %s reader: %v", declared, Version, err)
		}
		if doc.Version != declared {
			t.Errorf("declared version not carried: got %q, want %q", doc.Version, declared)
		}
	}
}

// TestTwoComponentVersionSaysWhatItWanted pins that the refusal of the old
// two-component spelling TELLS THE READER THE SHAPE IT EXPECTED. "0.1" is not
// coerced to "0.1.0" — that would be a guess about which patch the file was
// written against — so the message has to do the work the coercion would have
// done, which is tell whoever holds the file what to produce instead.
func TestTwoComponentVersionSaysWhatItWanted(t *testing.T) {
	_, _, err := Decode([]byte(`{"vnx":"0.1"}`))
	if err == nil {
		t.Fatal("a two-component version must be refused, not coerced")
	}
	msg := err.Error()
	for _, want := range []string{"MAJOR.MINOR.PATCH", Version} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal %q does not mention %q", msg, want)
		}
	}
}

// libRoot is this library's own import path — the root of the set of packages
// a file here is allowed to reach. It is a constant rather than a literal in
// four places because moving the library changes it, and a guard that keeps
// naming the old home stops guarding and starts refusing.
const libRoot = "github.com/dave/vnx/go/pkg/vnx"

// TestImportsAreStdlibOnly is the guard the plan's architecture note requires:
// pkg/vnx must be extractable as a standalone reference library, so it may not
// import anything outside the standard library — including other viatrail
// packages, stdlib-clean or not. Direct imports suffice: stdlib only imports
// stdlib, so a clean direct set is transitively clean.
//
// ⚠ IT COVERS _test.go FILES TOO. A third-party import in a test makes the
// library just as unshippable as one in a source file — whoever vendors it has
// to vendor that too — and a guard that read only the source files would let one
// in through the door nobody watches. This module has no dependencies at all,
// so there is no exemption to reason about.
//
// Test files MAY reach this library's own sibling packages: the external
// vnx_test package exercises the writers against the codec
// (writer_precision_test.go, writer_endpoint_test.go). Source files may not,
// and that asymmetry is deliberate — it keeps the dependency arrow pointing
// one way in shipped code while letting the tests span the whole library.
func TestImportsAreStdlibOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked, checkedTests := 0, 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		checked++
		isTest := strings.HasSuffix(name, "_test.go")
		if isTest {
			checkedTests++
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if isTest && (path == libRoot || strings.HasPrefix(path, libRoot+"/")) {
				continue
			}
			first := path
			if i := strings.Index(path, "/"); i >= 0 {
				first = path[:i]
			}
			if strings.Contains(first, ".") {
				t.Errorf("%s imports %q — pkg/vnx must import stdlib only", name, path)
			}
		}
	}
	// The control: a run that inspected no files would pass vacuously.
	if checked == 0 {
		t.Fatal("guard inspected zero source files — dead probe, not a clean tree")
	}
	// And the control on the widening itself. Without this, restoring the
	// _test.go skip would narrow the guard back to where it started and
	// nothing would go red to say so.
	if checkedTests == 0 {
		t.Fatal("guard inspected zero test files — the _test.go coverage added on 2026-09-06 is gone")
	}
}
