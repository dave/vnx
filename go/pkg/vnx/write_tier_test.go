package vnx

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The writer cases (corpus/write, spec §8).
//
// Each `X.in.vnx` is a document and `X.written.vnx` is the document a conforming
// writer produces from it. The runner decodes the input, WRITES, and compares
// the two as JSON trees.
//
// ⚠ IT IS THE ONLY TIER THAT REACHES §4.1's EMIT-BOUNDARY RULES. Every other
// tier re-encodes what it decoded, which applies rounding to values that already
// satisfy it and shortening to positions already short, so a defect in either
// rule is invisible outside this directory and corpus/numbers/.
//
// ⚠ AND SO IT NORMALISES ALMOST NOTHING. The accept tier rounds both sides and
// drops a trailing `null` from both, because it compares a document against a
// re-encoding of itself. Doing that here would normalise away the two rules
// under test. The only normalisations are the ones that are not about the emit
// boundary at all: §3.1's absent-versus-empty, and channels this version does
// not name.
const writeTierDir = corpusDir + "/write"

type writeCase struct {
	name     string
	in       []byte
	written  []byte
	expect   expectation
	manifest *attachmentManifest // nil for a document case
}

// attachmentManifest is the third input file a container case carries.
//
// The attachments are in a manifest rather than in real files beside the
// sidecar, and that is not tidiness: one is named with U+1F600, and a filename
// is subject to the checkout filesystem's Unicode normalisation and case
// folding, so a real file could arrive under a name that is not the one the case
// means. Inside JSON the name is escaped text that nothing can alter.
type attachmentManifest struct {
	Root        string `json:"root"`
	Attachments []struct {
		Name string `json:"name"`
		Body string `json:"body"`
	} `json:"attachments"`
}

func TestWriterCases(t *testing.T) {
	cases := loadWriteCases(t)
	if len(cases) == 0 {
		t.Fatal("no writer cases found — dead probe, not a clean result")
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.manifest != nil {
				runWriteContainer(t, c)
				return
			}
			runWriteDocument(t, c)
		})
	}
}

func runWriteDocument(t *testing.T, c writeCase) {
	t.Helper()
	opt := treeOptions{}

	// The input must NOT already be the written form, or the case proves
	// nothing: a writer that echoed its input would pass.
	diff, err := compareDocuments(c.in, c.written, opt)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Fatal("the input is already the written form; this case would pass vacuously")
	}

	got := writeThrough(t, c.in)
	diff, err = compareDocuments(got, c.written, opt)
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Errorf("the written document is not the one the case expects: %s", diff)
	}

	// And the written form is a fixed point: rounding and shortening are both
	// idempotent, so a case that fails this is a defect in the fixture rather
	// than in the writer.
	again := writeThrough(t, c.written)
	diff, err = compareDocuments(again, c.written, opt)
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Errorf("the expected output is not a fixed point of the writer: %s", diff)
	}
}

// writeThrough decodes a document and writes it back — the operation the tier
// tests. A diagnostic is a failure here: a writer case's input is a document the
// reader reads cleanly.
func writeThrough(t *testing.T, data []byte) []byte {
	t.Helper()
	doc, diags, err := Decode(data)
	if err != nil {
		t.Fatalf("the input must be read before it can be written: %v", err)
	}
	for _, d := range diags {
		t.Errorf("the input must read cleanly, but reported: %s", d)
	}
	out, err := Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return out
}

// runWriteContainer is the same case shape with an archive as its output.
//
// The archive's BYTES are not compared — §7.1 constrains five rules and leaves
// every other field to the writer, so two conforming writers are not expected to
// agree byte for byte. What is compared is what §7.1 does constrain: the root
// document's tree, the entry names, the attachment bodies, and the document
// being first.
func runWriteContainer(t *testing.T, c writeCase) {
	t.Helper()
	m := c.manifest

	want := readArchiveEntries(t, c.written)
	if len(want) == 0 {
		t.Fatal("the expected archive holds no entries")
	}
	if want[0].name != m.Root {
		t.Fatalf("the expected archive's first entry is %q, and §7.1 requires the root document %q", want[0].name, m.Root)
	}

	doc, diags, err := Decode(c.in)
	if err != nil {
		t.Fatalf("the input must be read before it can be written: %v", err)
	}
	for _, d := range diags {
		t.Errorf("the input must read cleanly, but reported: %s", d)
	}

	// The attachments are supplied REVERSED, which is what proves the writer
	// orders them itself rather than trusting the caller.
	attachments := make([]Attachment, 0, len(m.Attachments))
	for i := len(m.Attachments) - 1; i >= 0; i-- {
		a := m.Attachments[i]
		attachments = append(attachments, Attachment{Name: a.Name, Body: strings.NewReader(a.Body)})
	}
	var buf bytes.Buffer
	err = WriteContainerWithAttachments(&buf, m.Root, func(w io.Writer) error {
		return EncodeTo(w, doc)
	}, attachments)
	if err != nil {
		t.Fatalf("WriteContainerWithAttachments: %v", err)
	}
	got := readArchiveEntries(t, buf.Bytes())

	if len(got) != len(want) {
		t.Fatalf("the archive holds %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].name != want[i].name {
			t.Fatalf("entry %d is %q, want %q", i, got[i].name, want[i].name)
		}
	}
	if got[0].name != m.Root {
		t.Errorf("the first entry is %q; §7.1 requires the root document first", got[0].name)
	}

	diff, err := compareDocuments(got[0].body, want[0].body, treeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Errorf("the archive's root document is not the one the case expects: %s", diff)
	}

	if c.expect.Root != "" && got[0].name != c.expect.Root {
		t.Errorf("the root document is %q, and the sidecar names %q", got[0].name, c.expect.Root)
	}
	if want := c.expect.Attachments; want != nil {
		names := make([]string, 0, len(got)-1)
		for _, e := range got[1:] {
			names = append(names, e.name)
		}
		if strings.Join(names, ",") != strings.Join(*want, ",") {
			t.Errorf("attachments = %v, and the sidecar names %v", names, *want)
		}
	}

	// An attachment is bytes and is compared as bytes. Nothing in this format
	// interprets one.
	bodies := map[string]string{}
	for _, a := range m.Attachments {
		bodies[a.Name] = a.Body
	}
	for _, e := range got[1:] {
		body, named := bodies[e.name]
		if !named {
			t.Errorf("the archive carries %q, which the manifest does not name", e.name)
			continue
		}
		if string(e.body) != body {
			t.Errorf("%s: body is %q, want %q", e.name, e.body, body)
		}
	}

	// And the archive reads back through this library's own reader.
	ct, _, err := DecodeContainer(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("the archive this library wrote is one it cannot read: %v", err)
	}
	if DocumentIsEmpty(ct.Document) {
		t.Error("the archive read back as an empty document")
	}
}

// archiveEntry names collide with the writer's own type, so the test harness
// carries its own pair.
type readEntry struct {
	name string
	body []byte
}

// readArchiveEntries reads every entry of an archive, in archive order, through
// this library's own ZIP reader.
func readArchiveEntries(t *testing.T, data []byte) []readEntry {
	t.Helper()
	headers, err := readZipDirectory(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("reading the archive: %v", err)
	}
	out := make([]readEntry, 0, len(headers))
	for _, h := range headers {
		if h.isDirectory() {
			continue
		}
		body, err := readZipEntry(bytes.NewReader(data), h)
		if err != nil {
			t.Fatalf("reading entry %q: %v", h.name, err)
		}
		out = append(out, readEntry{name: h.name, body: body})
	}
	return out
}

func loadWriteCases(t *testing.T) []writeCase {
	t.Helper()
	entries, err := os.ReadDir(writeTierDir)
	if err != nil {
		t.Fatal(err)
	}
	var cases []writeCase
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".in.vnx") {
			continue
		}
		stem := strings.TrimSuffix(name, ".in.vnx")
		c := writeCase{name: stem}
		c.in = mustRead(t, filepath.Join(writeTierDir, name))

		// A case with no sidecar is a failure, not a skip, as everywhere else.
		sidecar := filepath.Join(writeTierDir, stem+".expect.json")
		raw, err := os.ReadFile(sidecar)
		if err != nil {
			t.Fatalf("%s has no expectation beside it: %v", name, err)
		}
		var exp expectation
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&exp); err != nil {
			t.Fatalf("%s: %v", sidecar, err)
		}
		if exp.Kind != "write" {
			t.Fatalf("%s declares kind %q but sits in write/", sidecar, exp.Kind)
		}
		if strings.TrimSpace(exp.Note) == "" {
			t.Fatalf("%s has no note; a corpus case that does not say why it exists cannot be maintained", sidecar)
		}
		c.expect = exp

		manifestPath := filepath.Join(writeTierDir, stem+".attachments.json")
		if _, err := os.Stat(manifestPath); err == nil {
			var m attachmentManifest
			if err := json.Unmarshal(mustRead(t, manifestPath), &m); err != nil {
				t.Fatalf("%s: %v", manifestPath, err)
			}
			c.manifest = &m
			c.written = mustRead(t, filepath.Join(writeTierDir, stem+".written.vnz"))
		} else {
			c.written = mustRead(t, filepath.Join(writeTierDir, stem+".written.vnx"))
		}
		cases = append(cases, c)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].name < cases[j].name })
	return cases
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
