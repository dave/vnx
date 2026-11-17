// Package vnxschema_test validates the VNX JSON Schema against the conformance
// corpus.
//
// ⚠ IT LIVES HERE, NOT IN pkg/vnx, ON PURPOSE. A JSON Schema validator is a
// third-party dependency, and pkg/vnx imports the standard library and nothing
// else — it IS the reference implementation, and that is the property everyone
// reimplementing this format relies on.
//
// This comment used to end "the stdlib guard in pkg/vnx skips _test.go files,
// so putting this there would have compiled and passed while quietly making
// the extracted library undeliverable. The guard's blind spot is not a
// licence." It was right, and it was also the clearest statement anyone had
// written of a hole nobody had closed. It is closed: since 2026-09-06 the
// guards read test files, so the mistake this package was avoiding by hand now
// reddens by itself.
//
// ⚠ AND THIS PACKAGE IS THE ONE REASON go.mod HAS A require BLOCK. Being under
// internal/ it is unimportable from outside the module, and under module graph
// pruning jsonschema/v6 never enters a consumer's build — so the library is
// still stdlib-only for anyone depending on it. It is a package here rather
// than a module of its own so `go test ./...` keeps running it; a nested module
// is how a check like this quietly stops being run.
package vnxschema_test

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	schemaPath = "../../../corpus/schema/vnx-0.11.schema.json"
	corpusDir  = "../../../corpus"
)

type expectation struct {
	Kind   string `json:"kind"`
	Schema string `json:"schema"`
}

// Every accept file validates, and every case's recorded verdict is the one the
// validator actually reaches. The second half is the part that matters: a
// schema nobody has measured against the corpus is a document, not a check.
func TestSchemaAgreesWithTheCorpus(t *testing.T) {
	schema := compile(t)

	checked := map[string]int{}
	for _, kind := range []string{"accept", "reject", "diagnose"} {
		dir := filepath.Join(corpusDir, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".vnx") {
				continue // the schema describes a document, not an archive
			}
			t.Run(kind+"/"+name, func(t *testing.T) {
				exp := loadExpectation(t, filepath.Join(dir, strings.TrimSuffix(name, ".vnx")+".expect.json"))
				data, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				var doc any
				parseErr := json.Unmarshal(data, &doc)

				if exp.Schema == "not-json" {
					if parseErr == nil {
						t.Fatal("recorded as not-json, but it parses — the corpus and the file disagree")
					}
					return
				}
				if parseErr != nil {
					t.Fatalf("expected schema verdict %q, but the file is not JSON: %v", exp.Schema, parseErr)
				}

				err = schema.Validate(doc)
				switch exp.Schema {
				case "valid":
					if err != nil {
						t.Fatalf("recorded as schema-valid, but the schema rejected it:\n%v", err)
					}
				case "invalid":
					if err == nil {
						t.Fatal("recorded as schema-invalid, but the schema accepted it")
					}
				}
			})
			checked[kind]++
		}
	}
	// The control. A run that found no files would report a clean sweep.
	for _, kind := range []string{"accept", "reject", "diagnose"} {
		if checked[kind] == 0 {
			t.Fatalf("validated zero %s documents — dead probe, not a clean result", kind)
		}
	}
	t.Logf("validated %d accept, %d reject, %d diagnose documents",
		checked["accept"], checked["reject"], checked["diagnose"])
}

// The rules a schema cannot reach, asserted rather than merely written down in
// schema/README.md. Each of these documents passes the schema and is WRONG, and
// a reader is the only thing standing between them and a silently broken
// library. If the schema ever grows the reach to catch one, this test goes red
// and the README stops being true — which is the point.
func TestSchemaCannotCatchTheseAndSaysSo(t *testing.T) {
	schema := compile(t)
	for _, c := range []struct{ name, doc string }{
		{
			// The whole meaning of the format is that two highlights crossing one
			// trail name the SAME arc. A pass naming an arc that does not exist
			// is a broken reference, and JSON Schema has no cross-references.
			"a pass naming an arc that is not in the document",
			`{"vnx":"0.11.0","networks":[{"nodes":{"n1":[-3.68,57.12],"n2":[-3.66,57.1]},
			  "arcs":{"a1":{"from":"n1","to":"n2","pips":[]}},
			  "highlights":[{"walk":[{"arc":"ghost","from":"n1","to":"n2"}]}]}]}`,
		},
		{
			// A ring is stored open: the last position does not repeat the
			// first. That is a relationship between two values, and no
			// keyword states it.
			"a polygon whose last position repeats its first",
			`{"vnx":"0.11.0","areas":[{"polygon":[[-3.74,57.09],[-3.72,57.09],[-3.72,57.075],[-3.74,57.09]]}]}`,
		},
		{
			// 8000 km wrong, every number in range.
			"a transposed lon/lat pair",
			`{"vnx":"0.11.0","points":[{"name":"x","at":[57.1169,-3.6423]}]}`,
		},
		{
			// A walk must join: each pass enters its arc at the node the
			// previous one left by. That is a relationship between passes.
			"a walk that does not join",
			`{"vnx":"0.11.0","networks":[{"nodes":{"n1":[-3.68,57.12],"n2":[-3.66,57.1],"n3":[-3.64,57.08]},
			  "arcs":{"a1":{"from":"n1","to":"n2","pips":[]},"a2":{"from":"n2","to":"n3","pips":[]}},
			  "highlights":[{"walk":[{"arc":"a1","from":"n2"},{"arc":"a2","to":"n3"}]}]}]}`,
		},
		{
			// `from` and `to` appear only where the walk's one spelling puts
			// them, and where that is depends on each pass's place in the walk.
			"a from on a pass after the first",
			`{"vnx":"0.11.0","networks":[{"nodes":{"n1":[-3.68,57.12],"n2":[-3.66,57.1],"n3":[-3.64,57.08]},
			  "arcs":{"a1":{"from":"n1","to":"n2","pips":[]},"a2":{"from":"n2","to":"n3","pips":[]}},
			  "highlights":[{"walk":[{"arc":"a1","from":"n1"},{"arc":"a2","from":"n2","to":"n3"}]}]}]}`,
		},
		{
			// An arc must run between two DIFFERENT nodes. Two string values
			// being unequal is beyond the keywords a schema has.
			"a self-loop arc",
			`{"vnx":"0.11.0","networks":[{"nodes":{"n1":[-3.68,57.12]},
			  "arcs":{"a1":{"from":"n1","to":"n1","pips":[[-3.67,57.11]]}}}]}`,
		},
		{
			// Reader-relative: the schema has no idea which version the reader
			// implements, so it cannot refuse a document from the future.
			"a version far above any reader's",
			`{"vnx":"9.9.9"}`,
		},
		{
			// And it cannot refuse one from the past either, which is the half
			// that actually bit. Through 0.x each MINOR is its own series
			// (spec 2.2), so 0.1 is not an older spelling of 0.11 and a 0.11
			// reader must refuse it — but `0.1.9` satisfies the version
			// pattern exactly as `0.11.0` does. Five of the six implementations
			// once compared with "greater than" rather than "not equal";
			// corpus/reject/version-too-old.vnx is what catches it, and it is
			// recorded schema-VALID for the reason asserted here.
			"a version below the reader's series",
			`{"vnx":"0.1.9","points":[{"at":[-3.6712,57.118]}]}`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var doc any
			if err := json.Unmarshal([]byte(c.doc), &doc); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(doc); err != nil {
				t.Fatalf("the schema CAUGHT this, so schema/README.md is now wrong and should be corrected:\n%v", err)
			}
		})
	}
}

func compile(t *testing.T) *jsonschema.Schema {
	t.Helper()
	f, err := os.Open(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("vnx.json", doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("vnx.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func loadExpectation(t *testing.T, path string) expectation {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var e expectation
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	if e.Schema == "" {
		t.Fatalf("%s records no schema verdict", path)
	}
	return e
}

// TestTheSchemaValidatorIsTheOnlyThirdPartyImport keeps this package's
// exemption from widening into the module's.
//
// go.mod's require block exists for this package alone. That is safe only for
// as long as it stays true, and "stays true" is not something a comment can
// enforce: the four TestImportsAreStdlibOnly guards each read their OWN
// directory, so between them they cover pkg/vnx, the three writer packages and
// cmd/vnx — and nothing else in the module. A third-party import landing in a
// directory none of them watches would compile, pass, and quietly make the
// shipped library undeliverable to anyone who vendors it.
//
// So this one walks the whole module. Stdlib is recognised the way the go tool
// recognises it: an import path whose first segment carries no dot.
func TestTheSchemaValidatorIsTheOnlyThirdPartyImport(t *testing.T) {
	const (
		moduleRoot = "github.com/dave/vnx/go"
		exemptDir  = "internal/vnxschema"
	)

	fset := token.NewFileSet()
	checked := 0
	err := filepath.Walk("../..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel("../..", path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		checked++
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == moduleRoot || strings.HasPrefix(p, moduleRoot+"/") {
				continue
			}
			if !strings.Contains(strings.SplitN(p, "/", 2)[0], ".") {
				continue // stdlib
			}
			if filepath.ToSlash(filepath.Dir(rel)) == exemptDir {
				continue
			}
			t.Errorf("%s imports the third-party package %q.\n"+
				"go.mod's require block is %s's alone — it is safe only because that "+
				"package is under internal/ and is pruned out of every consumer's module "+
				"graph. An import here is not, and it makes the library undeliverable to "+
				"anyone who vendors it.", rel, p, exemptDir)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The control. A walk that found no files would report a clean sweep.
	if checked < 20 {
		t.Fatalf("parsed only %d .go files — dead probe, not a clean result", checked)
	}
	t.Logf("parsed %d .go files across the module", checked)
}
