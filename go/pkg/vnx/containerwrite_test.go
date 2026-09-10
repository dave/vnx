package vnx_test

// The container WRITER, against the container reader that already existed.
//
// ⚠ THE ASSERTIONS GO THROUGH DecodeContainer, NOT THROUGH archive/zip. A test
// that opened the archive itself and compared entry names would agree with the
// writer about a convention the READER might not share — and "the reader finds
// the document" is the only property that matters. The one place this file does
// reach for archive/zip is the byte-equality test, where the question genuinely
// is "what is inside the entry".

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

func containerFixture() *vnx.Document {
	at := func(lon, lat float64) vnx.Position { return vnx.Position{Lon: lon, Lat: lat} }
	return &vnx.Document{
		Generator: "containerwrite_test",
		Waypoints: []vnx.Waypoint{{Name: "Ben Macdui", At: at(-3.6689, 57.0704)}},
		Tracks: []vnx.Track{{
			Name:     "Cairn Gorm Day",
			Segments: []vnx.Segment{{Pips: []vnx.Position{at(-3.67, 57.11), at(-3.668, 57.112)}}},
		}},
	}
}

// TestEncodeContainerToIsReadBackByDecodeContainer is the pair test: what this
// library writes, this library reads.
func TestEncodeContainerToIsReadBackByDecodeContainer(t *testing.T) {
	doc := containerFixture()
	var buf bytes.Buffer
	if err := vnx.EncodeContainerTo(&buf, doc); err != nil {
		t.Fatalf("EncodeContainerTo: %v", err)
	}

	c, diags, err := vnx.DecodeContainer(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("DecodeContainer refused the archive we just wrote: %v", err)
	}
	for _, d := range diags {
		t.Errorf("reading back our own archive raised a diagnostic: %s", d.Message)
	}
	if len(c.Attachments) != 0 {
		t.Errorf("the archive carried %d attachment(s); it holds one document and nothing else: %v",
			len(c.Attachments), c.Attachments)
	}
	if len(c.Document.Waypoints) != 1 || c.Document.Waypoints[0].Name != "Ben Macdui" {
		t.Fatalf("the waypoint did not survive the container: %+v", c.Document.Waypoints)
	}
	// The geometry, not only the count — a container that carried an empty
	// document back would pass a count check on a one-item fixture only if the
	// count were the sole assertion.
	if len(c.Document.Tracks) != 1 || len(c.Document.Tracks[0].Segments) != 1 ||
		len(c.Document.Tracks[0].Segments[0].Pips) != 2 {
		t.Fatalf("the track's geometry did not survive the container: %+v", c.Document.Tracks)
	}
}

// TestTheContainerEntryHoldsExactlyThePlainEncoding is the guard that keeps
// the compressed path from becoming a SECOND ENCODER.
//
// ⚠ THIS IS WHAT EXTENDS writer_precision_test.go's OBLIGATION OVER THE
// CONTAINER. That test reads the emitted bytes of every accept-corpus fixture
// through all four writers and asserts no coordinate carries more than six
// decimals. It cannot run over a `.vnz` — its readers parse JSON and XML, not
// zip — so instead of restating the obligation for the archive, this asserts
// the stronger and simpler fact it follows from: the bytes inside the entry are
// BYTE FOR BYTE what EncodeTo produces. A rounding rule that holds on the plain
// path therefore holds on this one, and it cannot come to hold on only one of
// them without this test going red.
func TestTheContainerEntryHoldsExactlyThePlainEncoding(t *testing.T) {
	doc := containerFixture()

	var plain bytes.Buffer
	if err := vnx.EncodeTo(&plain, doc); err != nil {
		t.Fatalf("EncodeTo: %v", err)
	}
	var archived bytes.Buffer
	if err := vnx.EncodeContainerTo(&archived, doc); err != nil {
		t.Fatalf("EncodeContainerTo: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(archived.Bytes()), int64(archived.Len()))
	if err != nil {
		t.Fatalf("the archive is not a zip: %v", err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("the archive holds %d entries; it must hold exactly the document", len(zr.File))
	}
	if zr.File[0].Name != vnx.RootDocumentName {
		t.Fatalf("the entry is called %q; a .vnz holds %q at the root",
			zr.File[0].Name, vnx.RootDocumentName)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	var got bytes.Buffer
	if _, err := got.ReadFrom(rc); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), plain.Bytes()) {
		t.Errorf("the archived document is not the plain encoding.\n  archived: %q\n  plain:    %q\n"+
			"The container must WRAP the encoder, never re-run it: a second encoding path is a "+
			"second answer to every rule §4.5 puts on the writers, coordinate rounding included.",
			got.String(), plain.String())
	}
	// Liveness: an empty plain encoding would make the comparison vacuous.
	if plain.Len() == 0 {
		t.Fatal("the plain encoding is empty — this comparison would pass over nothing")
	}
}

// TestTheArchiveIsDeterministic. Two exports of one document are one file.
//
// The zip entry carries a modification time, and stamping "now" into it is the
// obvious way to lose this — which is why WriteContainer leaves Modified at its
// zero value rather than taking Create's word for it.
func TestTheArchiveIsDeterministic(t *testing.T) {
	doc := containerFixture()
	var a, b bytes.Buffer
	if err := vnx.EncodeContainerTo(&a, doc); err != nil {
		t.Fatal(err)
	}
	if err := vnx.EncodeContainerTo(&b, doc); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two archives of one document differ; something in the zip header is not a constant")
	}
}

// TestWriteContainerRefusesANameThatIsNotAtTheRoot.
//
// ⚠ THE REFUSAL IS NOT PEDANTRY, and pickRootDocument is why. "At the root"
// there means no directory component at all: a document written to a/doc.vnx
// is found only by the third-party FALLBACK, and only while nothing else in the
// archive ends in .vnx. So a writer that nested the document would produce a
// file our own reader treats as somebody else's — which works until the day it
// silently does not.
func TestWriteContainerRefusesANameThatIsNotAtTheRoot(t *testing.T) {
	for _, name := range []string{"a/doc.vnx", "./doc.vnx", "../doc.vnx", "", "/doc.vnx", `a\doc.vnx`} {
		var buf bytes.Buffer
		err := vnx.WriteContainer(&buf, name, func(w io.Writer) error {
			_, err := w.Write([]byte("{}"))
			return err
		})
		if err == nil {
			t.Errorf("WriteContainer accepted the entry name %q; it must be a plain name at the "+
				"archive root", name)
			continue
		}
		if !strings.Contains(err.Error(), "vnx: unusable container") {
			t.Errorf("WriteContainer(%q) refused without ErrContainer: %v", name, err)
		}
	}
}

// TestAProducerRefusalComesBackUnwrapped. An encoder that refused a document
// has not failed to make an archive, and dressing its error as a container
// failure sends the next reader to the zip layer for a defect that is not
// there.
func TestAProducerRefusalComesBackUnwrapped(t *testing.T) {
	sentinel := errors.New("the encoder said no")
	var buf bytes.Buffer
	err := vnx.WriteContainer(&buf, vnx.RootDocumentName, func(io.Writer) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("WriteContainer returned %v; the producer's own error must survive errors.Is", err)
	}
}

// TestContainerHeaderFieldsAreFixed pins spec §7.1: two conforming writers
// produce the same bytes for one document, so every header field is fixed.
//
// ⚠ THE FIELDS THAT ENCODE THE MACHINE ARE THE ONES THAT BITE. A deterministic
// writer on one machine can still be non-deterministic across a fleet: create
// system is 3 on macOS and Linux and 0 on Windows, and external attributes
// carry whatever file mode the writing library stamps in. Neither is visible
// from inside one machine, because writing the same archive twice in a row is
// perfectly reproducible either way.
func TestContainerHeaderFieldsAreFixed(t *testing.T) {
	var b bytes.Buffer
	doc := &vnx.Document{Version: "0.1.0", Waypoints: []vnx.Waypoint{{Name: "w", At: vnx.Position{Lon: -3, Lat: 57}}}}
	if err := vnx.EncodeContainerTo(&b, doc); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("want one entry, got %d", len(zr.File))
	}
	f := zr.File[0].FileHeader
	// Stored, because deflate has no canonical output across languages.
	if f.Method != zip.Store {
		t.Errorf("method = %d, want %d (stored)", f.Method, zip.Store)
	}
	if f.ExternalAttrs != 0 {
		t.Errorf("external attributes = %#x, want 0 — this carries the writing "+
			"library's file mode and is a property of the machine, not the document", f.ExternalAttrs)
	}
	// A constant rather than the moment the writer ran, which is the property
	// that matters most and the one this writer does hold.
	if got := f.Modified.UTC(); got.Year() > 1980 {
		t.Errorf("modified = %v, which is a real time rather than a fixed one", got)
	}
	if len(f.Extra) != 0 {
		t.Errorf("extra field is %d bytes, want none", len(f.Extra))
	}
	if f.Comment != "" {
		t.Errorf("comment = %q, want empty", f.Comment)
	}
}

// TestContainerFieldsArchiveZipOverrides pins the part of spec §7.1 this writer
// DOES NOT MEET, so the gap is a fact under test rather than a discovery waiting
// for the second implementation to compare bytes with us.
//
// ⚠ THIS TEST ASSERTS THE WRONG VALUES ON PURPOSE. When someone makes the writer
// conform, it goes red and they must delete it — which is the notification. A
// gap recorded only in a comment is a gap nobody is told about.
//
// archive/zip overwrites CreatorVersion and ReaderVersion with 20, where §7.1
// fixes 10: a stored-only archive requires 1.0, and the field states a
// requirement rather than a habit. And the date is MS-DOS 0x0000, decoding to
// 1979-11-30, where §7.1 fixes 0x0021 — 1980-01-01. Setting Modified to the
// epoch does not fix it: archive/zip then adds a nine-byte extended-timestamp
// extra field, which §7.1 forbids, so the obvious repair breaks a different
// rule. Meeting §7.1 needs a zip writer of our own, which is the same conclusion
// §4.5.5 reached about encoding/json for strings.
func TestContainerFieldsArchiveZipOverrides(t *testing.T) {
	var b bytes.Buffer
	doc := &vnx.Document{Version: "0.1.0", Waypoints: []vnx.Waypoint{{Name: "w", At: vnx.Position{Lon: -3, Lat: 57}}}}
	if err := vnx.EncodeContainerTo(&b, doc); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	f := zr.File[0].FileHeader
	if f.CreatorVersion != 20 || f.ReaderVersion != 20 {
		t.Errorf("versions are now %d/%d rather than 20/20; if they are 10/10 this "+
			"writer has become conforming and this test should be deleted",
			f.CreatorVersion, f.ReaderVersion)
	}
	if got := f.Modified.UTC().Format("2006-01-02"); got != "1979-11-30" {
		t.Errorf("date is now %s rather than 1979-11-30; if it is 1980-01-01 this "+
			"writer has become conforming and this test should be deleted", got)
	}
}

// overDeclaringArchive builds a stored archive whose declared sizes exceed the
// payload, with a CRC that agrees over the bytes actually present. Hand-built,
// because no committed fixture can carry this: a file in the corpus has a valid
// stream and a matching CRC by construction.
func overDeclaringArchive(doc []byte, name string) []byte {
	crc := crc32.ChecksumIEEE(doc)
	over := uint32(len(doc)) + 7

	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	le(uint32(0x04034b50))
	le(uint16(10))
	le(uint16(0x0800))
	le(uint16(0))
	le(uint16(0))
	le(uint16(0x0021))
	le(crc)
	le(over)
	le(over)
	le(uint16(len(name)))
	le(uint16(0))
	b.WriteString(name)
	b.Write(doc)
	off := b.Len()
	le(uint32(0x02014b50))
	le(uint16(10))
	le(uint16(10))
	le(uint16(0x0800))
	le(uint16(0))
	le(uint16(0))
	le(uint16(0x0021))
	le(crc)
	le(over)
	le(over)
	le(uint16(len(name)))
	le(uint16(0))
	le(uint16(0))
	le(uint16(0))
	le(uint16(0))
	le(uint32(0))
	le(uint32(0))
	b.WriteString(name)
	end := b.Len()
	le(uint32(0x06054b50))
	le(uint16(0))
	le(uint16(0))
	le(uint16(1))
	le(uint16(1))
	le(uint32(end - off))
	le(uint32(off))
	le(uint16(0))
	return b.Bytes()
}

// TestOverDeclaredSizeIsRefusedAndSpecSaysAccept pins a gap between this reader
// and spec §7, deliberately asserting the NON-CONFORMING behaviour so that
// fixing it turns this red and tells whoever did it to delete the test.
//
// ⚠ §7 REQUIRES ACCEPTANCE: a reader MUST NOT refuse an entry whose declared
// size exceeds the bytes it holds where the CRC over those bytes agrees, because
// a size field is not the authority on what an entry contains and the checksum
// is. This reader refuses, with "zip: checksum error", because archive/zip reads
// the DECLARED number of bytes and then checksums what it read — so it consumes
// past the payload and the CRC disagrees.
//
// That makes archive/zip unable to meet §7 on the READ side as well as §7.1 on
// the write side, which is a stronger claim than the one already recorded, and
// it exposes a question the rule does not answer: a reader that trusts the
// declared size has no other way to know where an entry ends, so "the bytes it
// holds" is not derivable from the archive alone. Raised rather than resolved.
//
// No corpus case can reach any of this, which is why it is a unit test.
func TestOverDeclaredSizeIsRefusedAndSpecSaysAccept(t *testing.T) {
	raw := overDeclaringArchive([]byte("{\n  \"vnx\": \"0.1.0\"\n}\n"), "doc.vnx")
	_, _, err := vnx.DecodeContainer(bytes.NewReader(raw), int64(len(raw)))
	if err == nil {
		t.Error("this reader now ACCEPTS an over-declared size, which is what §7 requires — " +
			"delete this test, it exists only to pin the gap")
	}
}
