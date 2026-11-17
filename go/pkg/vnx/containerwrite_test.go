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
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/dave/vnx/go/pkg/vnx"
)

func containerFixture() *vnx.Document {
	at := func(lon, lat float64) vnx.Position { return vnx.Position{Lon: lon, Lat: lat} }
	return &vnx.Document{
		Generator: "containerwrite_test",
		Points:    []vnx.Point{{Name: "Ben Macdui", At: at(-3.6689, 57.0704)}},
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
	if len(c.Document.Points) != 1 || c.Document.Points[0].Name != "Ben Macdui" {
		t.Fatalf("the point did not survive the container: %+v", c.Document.Points)
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
			"second answer to every rule §4.1 puts on the writers, coordinate rounding included.",
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
// zero value rather than taking Create's word for it. WriteContainerAt is the
// one path that stamps a time, and only because its caller gave one.
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

// TestWriteContainerAtDatesEveryEntry reads the time back with archive/zip, and
// the DOS fields as well as Modified: a tool that ignores the extended timestamp
// shows the DOS fields, so they must hold the caller's wall clock.
func TestWriteContainerAtDatesEveryEntry(t *testing.T) {
	when := time.Date(2026, 11, 3, 14, 30, 16, 0, time.FixedZone("test", 2*60*60))
	var buf bytes.Buffer
	err := vnx.WriteContainerAt(&buf, when, vnx.RootDocumentName, func(w io.Writer) error {
		return vnx.EncodeTo(w, containerFixture())
	}, []vnx.Attachment{{Name: "photos/a.jpg", Body: strings.NewReader("a")}})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("got %d entries, want 2", len(zr.File))
	}
	wantDate := uint16((2026-1980)<<9 | 11<<5 | 3)
	wantTime := uint16(14<<11 | 30<<5 | 16/2)
	for _, f := range zr.File {
		if !f.Modified.Equal(when) {
			t.Errorf("%s: Modified = %v, want %v", f.Name, f.Modified, when)
		}
		if f.ModifiedDate != wantDate || f.ModifiedTime != wantTime {
			t.Errorf("%s: DOS date/time = %#x/%#x, want %#x/%#x", f.Name, f.ModifiedDate, f.ModifiedTime, wantDate, wantTime)
		}
	}
	if _, _, err := vnx.DecodeContainer(bytes.NewReader(buf.Bytes()), int64(buf.Len())); err != nil {
		t.Errorf("the dated archive does not read back: %v", err)
	}
}

// TestWriteContainerAtRefusesAZeroTime: a zero time is the 1979 date again.
func TestWriteContainerAtRefusesAZeroTime(t *testing.T) {
	var buf bytes.Buffer
	err := vnx.WriteContainerAt(&buf, time.Time{}, vnx.RootDocumentName, func(w io.Writer) error { return nil }, nil)
	if !errors.Is(err, vnx.ErrContainer) {
		t.Fatalf("got %v, want ErrContainer", err)
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
