package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture holds one of nearly everything: an area, a network and an
// application key (none of which GPX can carry), plus a waypoint, a plan and a track (all of
// which it can). Converting it therefore exercises BOTH halves of §2.2 —
// "write what fits" and "diagnose the rest" — in one run, which a fixture of
// only-supported or only-unsupported items could not.
const richFixture = "../../../corpus/accept/example-network.vnx"

// containerFixture is a .vnz — a zip container — and it is here because
// `convert` gets container input for free ONLY IF it reuses read(). A
// re-implementation that called vnx.Decode on the bytes would pass every other
// test in this file and fail this one.
const containerFixture = "../../../corpus/accept/container-root-doc.vnz"

// run executes the built CLI and returns stdout, stderr and the exit code
// SEPARATELY. CombinedOutput cannot be used anywhere in this file: the whole
// contract under test is that diagnostics go to stderr and only the document
// goes to stdout, and combining the two streams destroys the evidence.
func run(t *testing.T, bin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	_ = cmd.Run()
	return out.String(), errb.String(), cmd.ProcessState.ExitCode()
}

func TestConvertWritesEachFormatWithNoDatabase(t *testing.T) {
	bin := build(t)
	for _, c := range []struct {
		to, ext, prefix string
	}{
		{"gpx", ".gpx", `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<gpx `},
		{"kml", ".kml", `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<kml `},
		{"geojson", ".geojson", `{"type":"FeatureCollection"`},
	} {
		t.Run(c.to, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "out"+c.ext)
			_, stderr, code := run(t, bin, "convert", "--to", c.to, "--out", dest, richFixture)
			if code != 0 {
				t.Fatalf("exit %d\n%s", code, stderr)
			}
			got, err := os.ReadFile(dest)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if !strings.HasPrefix(string(got), c.prefix) {
				head := string(got)
				if len(head) > 100 {
					head = head[:100]
				}
				t.Errorf("--to %s wrote %q, which is not a %s file", c.to, head, c.to)
			}
		})
	}
}

// TestConvertWritesTheDocumentToStdoutWhenOutIsOmitted is the redirection
// contract: `vnx convert --to gpx file.vnx > out.gpx` must produce a usable
// file, which means NOTHING but the document may reach stdout.
func TestConvertWritesTheDocumentToStdoutWhenOutIsOmitted(t *testing.T) {
	bin := build(t)
	stdout, stderr, code := run(t, bin, "convert", "--to", "gpx", richFixture)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.HasPrefix(stdout, `<?xml version="1.0" encoding="UTF-8"?>`) {
		head := stdout
		if len(head) > 100 {
			head = head[:100]
		}
		t.Fatalf("stdout does not begin with the document: %q", head)
	}
	// The fixture's area, network and application key are all dropped, so this run
	// definitely HAS diagnostics — which is what makes the next assertion
	// meaningful rather than vacuous.
	if stderr == "" {
		t.Fatal("no diagnostics on stderr for a fixture holding an area, a network and an " +
			"application key — the check below would then prove nothing")
	}
	for _, code := range []string{"gpx.area-unsupported", "gpx.network-unsupported", "app-key.unsupported"} {
		if strings.Contains(stdout, code) {
			t.Errorf("diagnostic code %q reached STDOUT — a redirected `convert` would write it "+
				"into the middle of the user's .gpx file", code)
		}
	}
}

// TestConvertDiagnosticsCarryTheirCode is the affordance the public CLI exists
// for: an agent matches a stable token instead of parsing English prose.
func TestConvertDiagnosticsCarryTheirCode(t *testing.T) {
	bin := build(t)
	dest := filepath.Join(t.TempDir(), "out.gpx")
	_, stderr, code := run(t, bin, "convert", "--to", "gpx", "--out", dest, richFixture)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	for _, want := range []string{
		"gpx.area-unsupported", "/areas/0",
		"gpx.network-unsupported", "/networks/0",
		"app-key.unsupported", "/app.viatrail.rulesets",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not carry %q:\n%s", want, stderr)
		}
	}
	// One per line: a diagnostic split over two lines cannot be grepped, and a
	// run's diagnostics are what a script reads.
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if !strings.HasPrefix(line, "warning ") && !strings.HasPrefix(line, "info ") {
			t.Errorf("stderr line %q does not begin with a severity — the format is "+
				"`severity [code] path: message`", line)
		}
	}
}

// TestConvertReadsAContainer. `.vnz` support is free ONLY through read(); this
// is what says the free thing was actually taken.
func TestConvertReadsAContainer(t *testing.T) {
	bin := build(t)
	dest := filepath.Join(t.TempDir(), "out.gpx")
	_, stderr, code := run(t, bin, "convert", "--to", "gpx", "--out", dest, containerFixture)
	if code != 0 {
		t.Fatalf("a .vnz container was refused: exit %d\n%s", code, stderr)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(got), "<wpt") {
		t.Errorf("the container's waypoint did not reach the GPX:\n%s", got)
	}
}

// TestConvertRefusesAnUnknownFormatAndWritesNothing. An unknown --to must not
// fall back to any format: a user who asked for a shapefile and got GPX named
// .shp has been told nothing.
func TestConvertRefusesAnUnknownFormatAndWritesNothing(t *testing.T) {
	bin := build(t)
	for _, to := range []string{"shapefile", "GPX", "vnx", ""} {
		dest := filepath.Join(t.TempDir(), "out.bin")
		args := []string{"convert", "--out", dest, richFixture}
		if to != "" {
			args = append([]string{"convert", "--to", to, "--out", dest}, richFixture)
		}
		_, stderr, code := run(t, bin, args...)
		if code == 0 {
			t.Errorf("--to %q was accepted", to)
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Errorf("--to %q left something at the destination (%v)", to, err)
		}
		if !strings.Contains(stderr, "gpx") {
			t.Errorf("--to %q refused without naming the formats that do work:\n%s", to, stderr)
		}
	}
}

// ⚠ "vnx" IS IN THAT REFUSED LIST DELIBERATELY. `convert` converts a VNX file
// into something else; `vnx fmt` is what rewrites a VNX file as VNX. Accepting
// --to vnx would give the CLI two commands that do the same job.

// TestConvertOfAnUndecodableFileRefusesAndLeavesTheDestinationAsItWas is the
// only refusal a decodable input can never reach — and the destination check
// is the point: exit 1 must mean nothing was written, including "the file that
// was already there is still the file that was already there".
func TestConvertOfAnUndecodableFileRefusesAndLeavesTheDestinationAsItWas(t *testing.T) {
	bin := build(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "future.vnx")
	if err := os.WriteFile(in, []byte(`{"vnx":"9.9.9"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.gpx")
	const previous = "the file that was already here\n"
	if err := os.WriteFile(dest, []byte(previous), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := run(t, bin, "convert", "--to", "gpx", "--out", dest, in)
	if code != 1 {
		t.Fatalf("a file that does not decode must exit 1, got %d\n%s", code, stderr)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != previous {
		t.Errorf("the destination was changed by a run that refused: %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "future.vnx" && e.Name() != "out.gpx" {
			t.Errorf("%s was left behind by a refused run", e.Name())
		}
	}
}

// TestConvertOfAFileWithDecodeDiagnosticsStillConverts. A decode diagnostic
// says something in the INPUT was dropped, not that the input is unreadable.
// Refusing on one would make `convert` useless on exactly the third-party
// files it exists to read.
func TestConvertOfAFileWithDecodeDiagnosticsStillConverts(t *testing.T) {
	bin := build(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "typo.vnx")
	// ⚠ "at" IS THE COORDINATE KEY, not "position". An earlier draft of this
	// test used the wrong one: the waypoint then decoded with no coordinate,
	// GPX emitted nothing, and the run still exited 0 with a header-only file
	// at --out. The test caught it; the CLI did not.
	doc := `{"vnx":"0.10.0","nmae":"typo","waypoints":[{"name":"Car park","at":[-3.7,57.1]}]}`
	if err := os.WriteFile(in, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.gpx")

	_, stderr, code := run(t, bin, "convert", "--to", "gpx", "--out", dest, in)
	if code != 0 {
		t.Fatalf("a decodable file with diagnostics must still convert, got exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "/nmae") {
		t.Errorf("the DECODE diagnostic did not reach stderr — only the writer's did:\n%s", stderr)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(got), "Car park") {
		t.Errorf("the waypoint did not reach the GPX:\n%s", got)
	}
}

// TestConvertIsInTheUsage. A command nobody can discover is a command nobody
// uses, and `vnx -h` is where an agent looks first.
func TestConvertIsInTheUsage(t *testing.T) {
	bin := build(t)
	stdout, _, code := run(t, bin, "--help")
	if code != 0 {
		t.Fatalf("--help exited %d", code)
	}
	for _, want := range []string{"convert", "--to", "gpx", "kml", "geojson"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the usage text does not mention %q:\n%s", want, stdout)
		}
	}
}

// ⚠ THE EMPTY-EXPORT REFUSAL, AND THE TWO HALVES IT KEEPS APART (spec 5b §2.2).
//
// An HONESTLY empty document converts and writes its empty file: the file says
// the document holds nothing, which is true. A NON-EMPTY document that the
// chosen format reduced to nothing is refused: that file says the document
// holds nothing while it holds plenty, and it is the file §2.2 calls "worse
// than an error, because it looks like a success".
//
// Both halves are asserted here because either one alone passes against the
// wrong implementation — "always refuse" satisfies the first, "never refuse"
// satisfies the second.

func TestADocumentTheFormatCanCarryNothingOfRefusesAndWritesNothing(t *testing.T) {
	bin := build(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "areas-only.vnx")
	// One area and nothing else. GPX has no polygon (§3.1), so the writer emits
	// nothing — while the DOCUMENT is plainly not empty.
	doc := `{"vnx":"0.10.0","areas":[{"name":"Deer stalking estate","polygon":` +
		`[[-3.74,57.09],[-3.72,57.09],[-3.72,57.075]]}]}`
	if err := os.WriteFile(in, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.gpx")

	_, stderr, code := run(t, bin, "convert", "--to", "gpx", "--out", dest, in)
	if code == 0 {
		t.Fatalf("a GPX file holding nothing was written as a success\n%s", stderr)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("a refusal wrote %s anyway: %v", dest, err)
	}
	// The temporary is the other half of "nothing was written".
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "areas-only.vnx" {
			t.Errorf("%s was left behind by a refused run", e.Name())
		}
	}
	// The casualties stop being decoration on a success and become the reason.
	if !strings.Contains(stderr, "gpx.area-unsupported") {
		t.Errorf("the refusal does not say what was dropped:\n%s", stderr)
	}
	if !strings.Contains(stderr, "gpx") {
		t.Errorf("the refusal does not name the format that could carry nothing:\n%s", stderr)
	}
}

// The same document to STDOUT. The bytes already handed to the pipe cannot be
// recalled — that is the honest cost of the redirect form — but the exit code
// must still say the export failed, so `convert … > out.gpx && next` stops.
func TestADocumentTheFormatCanCarryNothingOfAlsoRefusesOnStdout(t *testing.T) {
	bin := build(t)
	in := filepath.Join(t.TempDir(), "areas-only.vnx")
	doc := `{"vnx":"0.10.0","areas":[{"name":"Deer stalking estate","polygon":` +
		`[[-3.74,57.09],[-3.72,57.09],[-3.72,57.075]]}]}`
	if err := os.WriteFile(in, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := run(t, bin, "convert", "--to", "gpx", in); code == 0 {
		t.Fatalf("exit 0 on a conversion that placed nothing\n%s", stderr)
	}
}

// TestAnHonestlyEmptyDocumentStillConverts is the other half, and it is not
// optional: without it, a `convert` that refused every export would pass the
// test above.
func TestAnHonestlyEmptyDocumentStillConverts(t *testing.T) {
	bin := build(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "empty.vnx")
	if err := os.WriteFile(in, []byte(`{"vnx":"0.10.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ to, ext string }{{"gpx", ".gpx"}, {"kml", ".kml"}, {"geojson", ".geojson"}} {
		t.Run(c.to, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "out"+c.ext)
			_, stderr, code := run(t, bin, "convert", "--to", c.to, "--out", dest, in)
			if code != 0 {
				t.Fatalf("an empty document was refused: exit %d\n%s — the file says the "+
					"document holds nothing, which is true", code, stderr)
			}
			if _, err := os.Stat(dest); err != nil {
				t.Errorf("no file at %s: %v", dest, err)
			}
		})
	}
}
