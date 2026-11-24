package merge

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// allowedImport is the one non-stdlib import merge may have.
const allowedImport = "github.com/dave/vnx/go/pkg/detmath"

// TestImportsAreStdlibOnly keeps merge dependency-free: every source and
// test file in the package may import the standard library and vnx's
// pkg/detmath, and nothing else.
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
			// An external test package (an Example) imports the package itself.
			self := f.Name.Name == "merge_test" && path == "github.com/dave/vnx/go/pkg/merge"
			if (path == "C" || strings.Contains(first, ".")) && path != allowedImport && !self {
				t.Errorf("%s imports %q — pkg/merge may import the standard library and %s only", name, path, allowedImport)
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
