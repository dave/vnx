package detmath

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestImportsAreStdlibOnly keeps detmath dependency-free: every source and
// test file in the package may import the standard library and nothing else.
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
		if strings.HasSuffix(name, "_test.go") {
			checkedTests++
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			first := path
			if i := strings.Index(path, "/"); i >= 0 {
				first = path[:i]
			}
			if strings.Contains(first, ".") {
				t.Errorf("%s imports %q — pkg/detmath may import the standard library only", name, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("guard inspected zero source files — dead probe, not a clean tree")
	}
	if checkedTests == 0 {
		t.Fatal("guard inspected zero test files")
	}
}
