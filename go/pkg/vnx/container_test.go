package vnx

import (
	"archive/zip"
	"bytes"
	"io"
	"runtime"
	"strings"
	"testing"
)

func makeZip(t *testing.T, entries [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const minimalDoc = `{"vnx":"0.1.0","generator":"test"}`

func TestContainerPrefersDocAtRoot(t *testing.T) {
	z := makeZip(t, [][2]string{{"other.vnx", `{"vnx":"0.1.0","generator":"other"}`}, {"doc.vnx", minimalDoc}})
	c, _, err := DecodeContainer(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	if c.Document.Generator != "test" {
		t.Fatalf("picked the wrong entry: %q", c.Document.Generator)
	}
}

// The fallback is deliberate and Dave ruled it: a third-party writer not using
// the conventional root name is an EXPECTED absence, which is exactly where the
// house rule puts a fallback rather than an error.
func TestContainerFallsBackToFirstVnxInArchiveOrder(t *testing.T) {
	z := makeZip(t, [][2]string{{"a/first.vnx", minimalDoc}, {"b/second.vnx", `{"vnx":"0.1.0","generator":"second"}`}})
	c, _, err := DecodeContainer(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	if c.Document.Generator != "test" {
		t.Fatalf("archive order not honoured: %q", c.Document.Generator)
	}
}

// ⚠ importer/kmz.go's comment says "prefer doc.kml IN THE ROOT" but its code
// tests filepath.Base, which matches at any depth and lets the LAST match win.
// Copy the intent, not the code: doc.vnx in a subdirectory is NOT the root
// document, so a root .vnx that precedes it wins on archive order.
func TestSubdirectoryDocVnxDoesNotBeatARootEntryThatComesFirst(t *testing.T) {
	z := makeZip(t, [][2]string{{"root.vnx", minimalDoc}, {"nested/doc.vnx", `{"vnx":"0.1.0","generator":"nested"}`}})
	c, _, err := DecodeContainer(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	if c.Document.Generator != "test" {
		t.Fatalf("a nested doc.vnx must not be treated as the root document, got %q", c.Document.Generator)
	}
}

func TestContainerRootNameIsCaseInsensitive(t *testing.T) {
	z := makeZip(t, [][2]string{{"other.vnx", `{"vnx":"0.1.0","generator":"other"}`}, {"DOC.VNX", minimalDoc}})
	c, _, err := DecodeContainer(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	if c.Document.Generator != "test" {
		t.Fatalf("got %q", c.Document.Generator)
	}
}

// Duplicate entry names resolve differently in Go and Python, so one archive
// would yield different documents in different languages. Refused, not
// arbitrated.
func TestDuplicateEntriesRefused(t *testing.T) {
	z := makeZip(t, [][2]string{{"doc.vnx", minimalDoc}, {"doc.vnx", `{"vnx":"0.1.0","generator":"twin"}`}})
	if _, _, err := DecodeContainer(bytes.NewReader(z), int64(len(z))); err == nil {
		t.Fatal("duplicate entry names must be refused")
	}
}

// Zip-slip. kmz.go is safe only because it never extracts; VNZ with attachments
// will, so the paths are checked at read time rather than at extract time.
func TestUnsafePathsRefused(t *testing.T) {
	for _, bad := range []string{"../escape.png", "/absolute.png", `C:\drive.png`, "a/../../up.png"} {
		t.Run(bad, func(t *testing.T) {
			z := makeZip(t, [][2]string{{"doc.vnx", minimalDoc}, {bad, "payload"}})
			_, _, err := DecodeContainer(bytes.NewReader(z), int64(len(z)))
			if err == nil {
				t.Fatalf("unsafe path %q must be refused", bad)
			}
			if !strings.Contains(err.Error(), "path") {
				t.Fatalf("the error should name the problem, got %v", err)
			}
		})
	}
}

func TestContainerWithNoDocumentRefused(t *testing.T) {
	z := makeZip(t, [][2]string{{"readme.txt", "nothing here"}})
	if _, _, err := DecodeContainer(bytes.NewReader(z), int64(len(z))); err == nil {
		t.Fatal("an archive with no .vnx entry must be refused")
	}
}

// TestContainerAttachmentsCarryTheirBytes.
//
// ⚠ THE CONTENT IS ASSERTED, NOT ONLY THE PATH. This test checked the name
// alone until 2026-09-12, and passed the whole time the library had no way at
// all to hand an attachment's bytes to a caller — a reader that listed the
// entries and threw the files away was indistinguishable from one that read
// them.
func TestContainerAttachmentsCarryTheirBytes(t *testing.T) {
	z := makeZip(t, [][2]string{{"doc.vnx", minimalDoc}, {"photos/summit.jpg", "jpeg"}})
	c, _, err := DecodeContainer(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Attachments) != 1 || c.Attachments[0].Path != "photos/summit.jpg" {
		t.Fatalf("attachments = %v", c.Attachments)
	}
	body, err := c.Attachments[0].Bytes()
	if err != nil {
		t.Fatalf("the attachment was listed and then could not be produced: %v", err)
	}
	if got := string(body); got != "jpeg" {
		t.Errorf("attachment bytes = %q, want %q", got, "jpeg")
	}
	// The handle streams too, and the two must agree — a caller choosing Open
	// over Bytes must not get different content.
	rc, err := c.Attachments[0].Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	streamed, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	if string(streamed) != string(body) {
		t.Errorf("Open gave %q and Bytes gave %q", streamed, body)
	}
}

// A container is still a reader, so spec §6.3 binds: diagnostics come back
// ALONGSIDE the document. This is a regression test with a real history —
// DecodeContainer used to return the document and the attachment names only,
// discarding diagnostics behind a comment that pointed at a
// DecodeContainerWithDiagnostics nobody ever wrote. The effect was that `vnx
// validate` called a .vnz clean however broken the document inside it was, and
// nothing failed, because no test ever looked.
func TestContainerSurfacesDocumentDiagnostics(t *testing.T) {
	z := makeZip(t, [][2]string{{"doc.vnx", `{"vnx":"0.1.0","nmae":"typo"}`}})
	c, diags, err := DecodeContainer(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	if c.Document == nil {
		t.Fatal("no document")
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want the document's 1: %v", len(diags), diags)
	}
	if diags[0].Path != "/nmae" {
		t.Fatalf("diagnostic path = %q, want /nmae", diags[0].Path)
	}
}

// TestDecodeContainerDoesNotMaterialiseAttachments is the regression guard for
// the reason the handle exists.
//
// ⚠ IT MEASURES THE ALLOCATION RATHER THAN ASSERTING THE TYPE. A reader that
// returned handles and filled them by reading every attachment into a cache
// first would satisfy any assertion about the API's shape while costing exactly
// what the handle was introduced to stop. Measured on the change that
// introduced this: materialising five 10 MB attachments retained 50.0 MB and
// took peak heap from 50.3 MB to 157.6 MB.
//
// The bound is deliberately loose. What is being caught is a whole attachment
// becoming resident, which is three orders of magnitude above the streaming
// cost; a tight bound here would fail on an unrelated change to a buffer size
// and teach the next person to raise it.
func TestDecodeContainerDoesNotMaterialiseAttachments(t *testing.T) {
	const attachmentSize = 8 << 20
	big := string(make([]byte, attachmentSize))
	z := makeZip(t, [][2]string{{"doc.vnx", minimalDoc}, {"big.bin", big}})

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	c, _, err := DecodeContainer(bytes.NewReader(z), int64(len(z)))
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Attachments) != 1 {
		t.Fatalf("attachments = %v, want the one that was written", c.Attachments)
	}
	const bound = attachmentSize / 4
	if grew := after.TotalAlloc - before.TotalAlloc; grew > bound {
		t.Errorf("decoding an archive with one %d-byte attachment allocated %d bytes, over the %d bound: "+
			"the attachment is being materialised rather than streamed", attachmentSize, grew, bound)
	}

	// ⚠ AND THE CONTENT IS STILL REACHABLE. Half of this guard is that nothing
	// was kept; the other half is that the handle still works, or the cheap
	// answer would be to return nothing at all.
	body, err := c.Attachments[0].Bytes()
	if err != nil {
		t.Fatalf("the handle could not produce the attachment: %v", err)
	}
	if len(body) != attachmentSize {
		t.Errorf("the handle produced %d bytes, want %d", len(body), attachmentSize)
	}
}
