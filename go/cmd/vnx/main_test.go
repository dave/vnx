package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// build compiles the CLI once for the shell-level tests. Testing the built
// binary rather than the functions is deliberate: exit codes and stdout are
// the contract an agent or a script actually sees.
func build(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "vnx")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func TestValidateCleanExitsZero(t *testing.T) {
	bin := build(t)
	out, err := exec.Command(bin, "validate", "../../../corpus/accept/example-network.vnx").CombinedOutput()
	if err != nil {
		t.Fatalf("clean document must exit zero: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok, vnx 0.10") {
		t.Fatalf("got %s", out)
	}
}

func TestValidateRefusalExitsNonZero(t *testing.T) {
	bin := build(t)
	f := filepath.Join(t.TempDir(), "newer.vnx")
	if err := os.WriteFile(f, []byte(`{"vnx":"9.9.9"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "validate", f).CombinedOutput()
	if err == nil {
		t.Fatalf("a refused document must exit non-zero, got:\n%s", out)
	}
	if !strings.Contains(string(out), "newer reader") {
		t.Fatalf("the error must say what to do about it, got %s", out)
	}
}

// Diagnostics alone are NOT a failure — the document read, with things
// reported. Only a refusal is a non-zero exit, or every warning would look like
// a broken file to a script.
func TestDiagnosticsDoNotFailTheExitCode(t *testing.T) {
	bin := build(t)
	f := filepath.Join(t.TempDir(), "warn.vnx")
	if err := os.WriteFile(f, []byte(`{"vnx":"0.10.0","nmae":"typo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "validate", f).CombinedOutput()
	if err != nil {
		t.Fatalf("diagnostics are not failures: %v\n%s", err, out)
	}
}

// The highest-value agent affordance in the spec: a diagnostic carries a
// document PATH, not just prose.
func TestJSONOutputCarriesDiagnosticPaths(t *testing.T) {
	bin := build(t)
	f := filepath.Join(t.TempDir(), "warn.vnx")
	if err := os.WriteFile(f, []byte(`{"vnx":"0.10.0","nmae":"typo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "validate", "--json", f).Output()
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		OK          bool `json:"ok"`
		Diagnostics []struct {
			Code, Path, Message string
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatalf("--json must emit parseable JSON: %v\n%s", err, out)
	}
	if len(rep.Diagnostics) != 1 || rep.Diagnostics[0].Path != "/nmae" {
		t.Fatalf("got %+v", rep.Diagnostics)
	}
}

// TestJSONDiagnosticCarriesCodeAndNoSeverity pins §6.3's field set on the
// machine-readable output.
//
// ⚠ IT ASSERTS ON THE RAW KEYS, not on a decoded struct, and that is the only
// shape that works. encoding/json ignores a key the target struct does not
// name, so a struct-based test cannot see an extra `severity` at all — which is
// exactly how this tool emitted one for as long as it did while
// TestJSONOutputCarriesDiagnosticPaths stayed green beside it.
//
// The two halves are both spec, both §6.3: "A diagnostic carries no severity",
// and `code` is the stable token a conformance case compares and a script
// matches on.
func TestJSONDiagnosticCarriesCodeAndNoSeverity(t *testing.T) {
	bin := build(t)
	out, err := exec.Command(bin, "validate", "--json", "../../../corpus/diagnose/walk-not-connected.vnx").Output()
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatalf("--json must emit parseable JSON: %v\n%s", err, out)
	}
	if len(rep.Diagnostics) == 0 {
		t.Fatalf("the fixture must raise a diagnostic, or this test measures nothing:\n%s", out)
	}
	for i, d := range rep.Diagnostics {
		if _, present := d["severity"]; present {
			t.Errorf("diagnostic %d carries a `severity` key; §6.3 says a diagnostic carries no severity", i)
		}
		code, present := d["code"]
		if !present {
			t.Errorf("diagnostic %d carries no `code` key; §6.3 defines it and a script matches on it", i)
			continue
		}
		if code != "walk-not-connected" {
			t.Errorf("diagnostic %d code = %v, want the token the corpus pins", i, code)
		}
	}
}

func TestOutlineOmitsGeometry(t *testing.T) {
	bin := build(t)
	out, err := exec.Command(bin, "outline", "../../../corpus/accept/example-network.vnx").Output()
	if err != nil {
		t.Fatal(err)
	}
	// A coordinate in the outline would mean geometry leaked into the summary,
	// which defeats its purpose for a large document.
	if strings.Contains(string(out), "57.1") || strings.Contains(string(out), "-3.6") {
		t.Fatalf("outline must not carry coordinates:\n%s", out)
	}
	if !strings.Contains(string(out), "4 nodes, 3 arcs") {
		t.Fatalf("got %s", out)
	}
}

func TestUnknownCommandExitsTwo(t *testing.T) {
	bin := build(t)
	cmd := exec.Command(bin, "frobnicate")
	_ = cmd.Run()
	if code := cmd.ProcessState.ExitCode(); code != 2 {
		t.Fatalf("usage errors exit 2, got %d", code)
	}
}
