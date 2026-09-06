package kml

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// libRoot is this library's own import path — see pkg/vnx/vnx_test.go.
const libRoot = "github.com/dave/vnx/go/pkg/vnx"

// TestImportsAreStdlibOnly is the local half of cmd/vnx/purity_test.go, the
// same guard gpx, geojson and vnx itself carry. That guard enumerates the
// reference CLI's whole in-module dependency set; this one fails in the package
// that broke it, which is where the fix belongs.
//
// ⚠ kml WENT WITHOUT IT UNTIL 2026-09-04, and cmd/vnx's guard does not cover the
// gap: moduleDeps there follows only imports carrying the module prefix, so a
// THIRD-PARTY import added here enters nothing and trips nothing. Spec §4.1 says
// all three writers are stdlib-only; it was enforced on two of them and merely
// true of the third.
//
// ⚠ AND IT COVERS _test.go FILES, since 2026-09-06 — see pkg/vnx's own guard
// for why that hole mattered. A test file may reach the library's sibling
// packages; a source file may reach only the parent.
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
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
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
			if path == libRoot {
				continue // the parent, and the only in-module import allowed
			}
			if isTest && strings.HasPrefix(path, libRoot+"/") {
				continue // a sibling package of the same library
			}
			first := path
			if i := strings.Index(path, "/"); i >= 0 {
				first = path[:i]
			}
			if strings.Contains(first, ".") {
				t.Errorf("%s imports %q — pkg/vnx/kml may import stdlib and pkg/vnx only", name, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("guard inspected zero source files — dead probe, not a clean tree")
	}
	if checkedTests == 0 {
		t.Fatal("guard inspected zero test files — the _test.go coverage added on 2026-09-06 is gone")
	}
}
