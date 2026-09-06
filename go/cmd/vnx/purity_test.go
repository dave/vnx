package main

import (
	gobuild "go/build"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// THE REFERENCE CLI MUST STAY TAG-FREE AND DATABASE-FREE.
//
// `vnx` is the published reference tool (spec §7). In Viatrail, where this file
// used to live, the concrete danger had a name: pkg/core/db carries an untagged
// init() that PANICS — "package db requires the 'fts5' build tag" — so a
// dependency on it would not have failed the build. `vnx validate` would
// compile, ship, and panic at startup on a machine with no database anywhere
// near it, and the binary would be cgo-bound through mattn/go-sqlite3 with its
// cross-compilability gone. `vnxdb` is a second binary rather than a `--db`
// flag here for exactly that reason.
//
// ⚠ THAT PARTICULAR IMPORT IS NOW IMPOSSIBLE, and its guard travelled anyway.
// Extracting this module (2026-09-06) put pkg/core/db in a different repository
// that nothing here can import, so the half of this file that asserted
// "cmd/vnxdb reaches pkg/core/db" retired with its subject — see
// TestTheImportWalkReportsExactlyWhatAPackageImports below for what replaced
// it, and why the liveness it provided is still here. What survives unchanged
// is the general ruling: the reference CLI's in-module dependency set is
// enumerated, so the NEXT heavy import is a deliberate edit to this file rather
// than a surprise. It walks the UNTAGGED import graph, independent of the tags
// this test itself was compiled with.

const modulePrefix = "github.com/dave/vnx/go/"

// TestReferenceCLIImportsOnlyTheReferencePackage pins the whole in-module
// dependency set of cmd/vnx, not just the absence of core/db. A ban list only
// catches the import somebody already thought of; this catches the next one.
func TestReferenceCLIImportsOnlyTheReferencePackage(t *testing.T) {
	deps := moduleDeps(t, "cmd/vnx")
	// ⚠ ENUMERATED, NEVER A `pkg/vnx/` PREFIX MATCH (spec 5b §4.4). `convert`
	// needs the three writer packages, so they were added here ONE AT A TIME in
	// T7. A prefix test would have been shorter and would have let every future
	// pkg/vnx subpackage in unnoticed — and making that a deliberate edit is
	// this guard's whole purpose. The writers are allowed for the same reason
	// pkg/vnx is: they are stdlib-only (spec §4.1), so they add no build tag,
	// no cgo and no database.
	want := map[string]bool{
		"pkg/vnx":         true,
		"pkg/vnx/gpx":     true,
		"pkg/vnx/kml":     true,
		"pkg/vnx/geojson": true,
	}
	var extra []string
	for p := range deps {
		if !want[p] {
			extra = append(extra, p)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("cmd/vnx must import only %s, but also reaches: %s\n"+
			"If a database is what you need, put it in cmd/vnxdb — see this file's doc comment.",
			strings.Join(sortedKeys(want), ", "), strings.Join(extra, ", "))
	}
	// Liveness: a walker that silently returned nothing would pass the check
	// above forever. Every allowed package is asserted PRESENT, not just
	// pkg/vnx — an allow-set entry for a package the CLI stopped importing is a
	// hole nobody would notice, because a wider allow-set never reddens.
	for _, p := range sortedKeys(want) {
		if !deps[p] {
			t.Errorf("the import walk did not reach %s — either the walk is broken, or %s is in "+
				"the allow-set and nothing imports it any more", p, p)
		}
	}
}

// TestTheImportWalkReportsExactlyWhatAPackageImports is the other half of that
// liveness assertion, and it is what this module can still say.
//
// Until the extraction it read "the same walk, pointed at the binary that IS
// allowed a database, must find one" — cmd/vnxdb reaching pkg/core/db. That
// binary and that package stayed in Viatrail, so the assertion had no subject
// here any more. What it was really buying was the guarantee that a walk which
// had quietly stopped following imports could not report every command pure,
// and that guarantee is bought below instead, from two packages whose in-module
// import sets are known independently of the walker.
//
// Exactness is deliberate, and it is strictly more than the old test asked:
// geojson's set must be exactly {pkg/vnx}, so a walk that under-reports fails
// on the missing entry and one that over-reports fails on the extra. pkg/vnx's
// must be EMPTY, which is the same fact its own TestImportsAreStdlibOnly
// asserts from the other side — by reading the files rather than by walking —
// so the two would not fail together for one shared reason.
func TestTheImportWalkReportsExactlyWhatAPackageImports(t *testing.T) {
	if got := sortedKeys(moduleDeps(t, "pkg/vnx/geojson")); strings.Join(got, ",") != "pkg/vnx" {
		t.Errorf("the walk makes pkg/vnx/geojson reach [%s], want exactly [pkg/vnx]", strings.Join(got, ", "))
	}
	if got := sortedKeys(moduleDeps(t, "pkg/vnx")); len(got) != 0 {
		t.Errorf("the walk makes pkg/vnx reach [%s]; the codec imports nothing in this module", strings.Join(got, ", "))
	}
}

// moduleDeps returns every in-module package the given package reaches,
// transitively, as module-relative paths. Imports outside the module (stdlib
// and third party) are not followed: the ruling is about what this module's
// own code drags in.
//
// It reads EVERY file in each directory, whatever tags or platform they are
// constrained to (see UseAllFiles below), and resolves import paths to
// directories itself rather than calling build.Context.Import, which in module
// mode shells out to `go list` and applies that invocation's own tags. Every in-module path maps to
// <module root>/<path minus prefix>, so the mapping is exact and the walk
// stays hermetic.
func moduleDeps(t *testing.T, rel string) map[string]bool {
	t.Helper()
	root := moduleRoot(t)
	ctx := gobuild.Default
	ctx.BuildTags = nil // ⚠ the untagged graph is the thing under test
	// ⚠ AND THE PLATFORM-INDEPENDENT ONE. gobuild.Default carries the HOST's
	// GOOS/GOARCH, and ImportDir applies them to `//go:build` lines and to the
	// `_linux.go` / `_darwin.go` filename convention. So an import of
	// pkg/core/db added in a `main_linux.go` would be invisible to this guard
	// on Dave's mac while shipping in the cross-compiled linux binary — and
	// cross-compilability is one of the two things this file exists to protect.
	// UseAllFiles reads every .go file in the directory whatever it is
	// constrained to, which is the same ruling BuildTags = nil makes about
	// tags, made about platforms. There are no platform-guarded files in
	// cmd/vnx today; this closes the hole before there are.
	//
	// Its one cost: a directory mixing package names — a `//go:build ignore`
	// tool file next to a library — makes ImportDir error rather than skip it.
	// That fails LOUDLY at the t.Fatalf below, which is the right failure for a
	// walk that can no longer see the whole graph.
	ctx.UseAllFiles = true
	seen := map[string]bool{}
	var walk func(rel string)
	walk = func(rel string) {
		pkg, err := ctx.ImportDir(filepath.Join(root, rel), 0)
		if err != nil {
			t.Fatalf("read package %s: %v", rel, err)
		}
		for _, imp := range pkg.Imports {
			if !strings.HasPrefix(imp, modulePrefix) {
				continue
			}
			child := strings.TrimPrefix(imp, modulePrefix)
			if seen[child] {
				continue
			}
			seen[child] = true
			walk(child)
		}
	}
	walk(rel)
	return seen
}

// moduleRoot finds the directory holding go.mod by walking up from the test's
// working directory (which is this file's own directory).
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
