package vnx_test

// The archive against spec §7.1, which is five rules and nothing else.
//
// ⚠ §7.1 NO LONGER FIXES A FIELD TABLE, so this file does not walk one. Every
// other field of the archive is the writer's own business and two conforming
// writers are not expected to produce the same bytes. What is asserted here is
// the five: the root document named and first, the UTF-8 filename flag on every
// entry, unique names, safe attachment paths, and stored or deflated.
//
// The assertions read the archive's own bytes rather than a reader's account of
// them where the rule is about the bytes, and TestArchiveIsReadByArchiveZip
// keeps a general-purpose implementation on the read side so this writer cannot
// drift into a private dialect only our own reader accepts.

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

// zipHeader is one entry as the bytes state it, from either header.
type zipHeader struct {
	signature    uint32
	madeBy       uint16 // central only
	needed       uint16
	flags        uint16
	method       uint16
	modTime      uint16
	modDate      uint16
	crc          uint32
	compressed   uint32
	uncompressed uint32
	extraLen     uint16
	commentLen   uint16 // central only
	diskStart    uint16 // central only
	internal     uint16 // central only
	external     uint32 // central only
	name         string
	body         []byte // local only
}

// walkCentralDirectory reads the central directory from the end-of-central-
// directory record, so the two accounts of one entry can be compared.
func walkCentralDirectory(t *testing.T, raw []byte) []zipHeader {
	t.Helper()
	eocd := -1
	for i := len(raw) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(raw[i:]) == 0x06054b50 {
			eocd = i
			break
		}
	}
	if eocd < 0 {
		t.Fatal("no end-of-central-directory record")
	}
	if got := binary.LittleEndian.Uint16(raw[eocd+4:]); got != 0 {
		t.Errorf("this disk = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint16(raw[eocd+6:]); got != 0 {
		t.Errorf("the disk holding the central directory = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint16(raw[eocd+20:]); got != 0 {
		t.Errorf("archive comment length = %d, want none", got)
	}
	count := int(binary.LittleEndian.Uint16(raw[eocd+8:]))
	if total := int(binary.LittleEndian.Uint16(raw[eocd+10:])); total != count {
		t.Errorf("the record says %d entries on this disk and %d in total", count, total)
	}
	p := int(binary.LittleEndian.Uint32(raw[eocd+16:]))
	if size := int(binary.LittleEndian.Uint32(raw[eocd+12:])); p+size != eocd {
		t.Errorf("the central directory is declared as %d bytes at %d, which does not reach the record at %d",
			size, p, eocd)
	}
	var out []zipHeader
	for i := 0; i < count; i++ {
		if p+46 > len(raw) || binary.LittleEndian.Uint32(raw[p:]) != 0x02014b50 {
			t.Fatalf("central directory entry %d is truncated or misplaced", i)
		}
		h := zipHeader{
			signature:    binary.LittleEndian.Uint32(raw[p:]),
			madeBy:       binary.LittleEndian.Uint16(raw[p+4:]),
			needed:       binary.LittleEndian.Uint16(raw[p+6:]),
			flags:        binary.LittleEndian.Uint16(raw[p+8:]),
			method:       binary.LittleEndian.Uint16(raw[p+10:]),
			modTime:      binary.LittleEndian.Uint16(raw[p+12:]),
			modDate:      binary.LittleEndian.Uint16(raw[p+14:]),
			crc:          binary.LittleEndian.Uint32(raw[p+16:]),
			compressed:   binary.LittleEndian.Uint32(raw[p+20:]),
			uncompressed: binary.LittleEndian.Uint32(raw[p+24:]),
			extraLen:     binary.LittleEndian.Uint16(raw[p+30:]),
			commentLen:   binary.LittleEndian.Uint16(raw[p+32:]),
			diskStart:    binary.LittleEndian.Uint16(raw[p+34:]),
			internal:     binary.LittleEndian.Uint16(raw[p+36:]),
			external:     binary.LittleEndian.Uint32(raw[p+38:]),
		}
		nameLen := int(binary.LittleEndian.Uint16(raw[p+28:]))
		h.name = string(raw[p+46 : p+46+nameLen])
		// The recorded offset must be where the local header for this name is.
		off := int(binary.LittleEndian.Uint32(raw[p+42:]))
		if off+30 > len(raw) || binary.LittleEndian.Uint32(raw[off:]) != 0x04034b50 {
			t.Errorf("the offset recorded for %q does not point at a local header", h.name)
		} else if local := int(binary.LittleEndian.Uint16(raw[off+26:])); string(raw[off+30:off+30+local]) != h.name {
			t.Errorf("the offset recorded for %q points at the local header of %q",
				h.name, raw[off+30:off+30+local])
		}
		out = append(out, h)
		p += 46 + nameLen + int(h.extraLen) + int(h.commentLen)
	}
	return out
}

func attachmentFixture(t *testing.T, attachments []vnx.Attachment) []byte {
	t.Helper()
	var b bytes.Buffer
	doc := &vnx.Document{Version: "0.8.0", Waypoints: []vnx.Waypoint{{Name: "w", At: vnx.Position{Lon: -3, Lat: 57}}}}
	err := vnx.WriteContainerWithAttachments(&b, vnx.RootDocumentName, func(w io.Writer) error {
		return vnx.EncodeTo(w, doc)
	}, attachments)
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestEverySpec71RuleHolds checks the five rules §7.1 states.
//
// ⚠ THE DEFLATE ASSERTION IS THE ONE A REGRESSION SLIPS PAST. A writer switched
// to stored produces an archive every reader still accepts — §7 requires both —
// and every other test in this package would stay green while a `.vnz` stopped
// being smaller than the `.vnx` inside it.
func TestEverySpec71RuleHolds(t *testing.T) {
	raw := attachmentFixture(t, []vnx.Attachment{{Name: "photo.jpg", Body: strings.NewReader("not really a jpeg")}})
	centrals := walkCentralDirectory(t, raw)
	if len(centrals) != 2 {
		t.Fatalf("want two entries, got %d", len(centrals))
	}
	if centrals[0].name != vnx.RootDocumentName {
		t.Errorf("the first entry is %q; §7.1 requires %q, so §7's fallback lands on it",
			centrals[0].name, vnx.RootDocumentName)
	}
	for _, h := range centrals {
		if h.flags&0x0800 == 0 {
			t.Errorf("%q: the UTF-8 filename flag is not set (flags %#04x)", h.name, h.flags)
		}
		if h.method != zip.Deflate {
			t.Errorf("%q: method %d, want %d (deflate)", h.name, h.method, zip.Deflate)
		}
		if strings.Contains(h.name, `\\`) || strings.HasPrefix(h.name, "/") {
			t.Errorf("%q is not a safe relative entry path", h.name)
		}
	}
	seen := map[string]bool{}
	for _, h := range centrals {
		key := strings.ToLower(h.name)
		if seen[key] {
			t.Errorf("%q appears twice, compared case-insensitively", h.name)
		}
		seen[key] = true
	}
}

// TestTheUTF8FlagIsSetOnAnASCIIName, which is the half a general-purpose writer
// gets wrong.
//
// archive/zip sets the flag only for a name that NEEDS it — one outside ASCII —
// and §7.1 admits no such exception. An archive whose names all happen to be
// ASCII would therefore carry the flag nowhere, and nothing else in this file
// would notice, because the fixture that exercises the flag is the one with an
// emoji in it.
func TestTheUTF8FlagIsSetOnAnASCIIName(t *testing.T) {
	raw := attachmentFixture(t, []vnx.Attachment{{Name: "plain.txt", Body: strings.NewReader("ascii")}})
	for _, h := range walkCentralDirectory(t, raw) {
		if h.name != vnx.RootDocumentName && h.name != "plain.txt" {
			t.Fatalf("unexpected entry %q", h.name)
		}
		if h.flags&0x0800 == 0 {
			t.Errorf("%q is pure ASCII and carries no UTF-8 filename flag (%#04x)", h.name, h.flags)
		}
	}
}

// TestTheArchiveIsSmallerThanTheDocumentItHolds, which is what deflating is
// for and what the export screen's "Compressed file (VNZ)" claims.
func TestTheArchiveIsSmallerThanTheDocumentItHolds(t *testing.T) {
	doc := &vnx.Document{Version: "0.8.0"}
	for i := 0; i < 2000; i++ {
		doc.Waypoints = append(doc.Waypoints, vnx.Waypoint{
			Name: "a waypoint with a repetitive name",
			At:   vnx.Position{Lon: -3.671 + float64(i)/1e6, Lat: 57.118 + float64(i)/1e6},
		})
	}
	plain, err := vnx.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := vnx.EncodeContainerTo(&archive, doc); err != nil {
		t.Fatal(err)
	}
	if archive.Len() >= len(plain) {
		t.Errorf("the archive is %d bytes and the document is %d; a .vnz must be smaller than the .vnx it holds",
			archive.Len(), len(plain))
	}
	t.Logf("document %d bytes, archive %d bytes (%.1fx)", len(plain), archive.Len(), float64(len(plain))/float64(archive.Len()))
}

// TestTheRootDocumentIsFirstAndTheAttachmentsFollowInOneOrder.
//
// §7.1 requires only that the document is first. This writer also puts the
// attachments in name order, so that two exports of one library agree with each
// other — caller order is not a property of the content, and one exporter
// iterating a map beside another iterating a sorted list would otherwise emit
// two different archives for one document.
func TestTheRootDocumentIsFirstAndTheAttachmentsFollowInOneOrder(t *testing.T) {
	names := []string{"b.jpg", "a/photo.jpg", "\uff3a.txt", "\U0001f600.txt"}
	attachments := make([]vnx.Attachment, len(names))
	for i, n := range names {
		attachments[i] = vnx.Attachment{Name: n, Body: strings.NewReader(n)}
	}
	want := append([]string{vnx.RootDocumentName}, "a/photo.jpg", "b.jpg", "\uff3a.txt", "\U0001f600.txt")

	raw := attachmentFixture(t, attachments)
	var got []string
	for _, h := range walkCentralDirectory(t, raw) {
		got = append(got, h.name)
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("archive order is %q, want %q (the root document first, then the attachments by name)", got, want)
	}
}

// TestTheArchiveDoesNotDependOnTheOrderTheCallerSupplied. The reason §7.1 sorts
// at all: caller order is not a property of the content, and two exporters over
// one library — one iterating a map, one a sorted list — must not emit two
// archives for one document.
func TestTheArchiveDoesNotDependOnTheOrderTheCallerSupplied(t *testing.T) {
	bodies := []struct{ name, body string }{
		{"a.jpg", "a"},
		{"b.jpg", "b"},
		{"\uff3a.txt", "z"},
	}
	supply := func(order []int) []vnx.Attachment {
		out := make([]vnx.Attachment, 0, len(order))
		for _, i := range order {
			out = append(out, vnx.Attachment{Name: bodies[i].name, Body: strings.NewReader(bodies[i].body)})
		}
		return out
	}
	if !bytes.Equal(attachmentFixture(t, supply([]int{0, 1, 2})), attachmentFixture(t, supply([]int{2, 1, 0}))) {
		t.Error("the same document and attachments in two input orders gave two archives")
	}
}

// TestArchiveIsReadByArchiveZip keeps a general-purpose reader on the other side
// of a hand-written writer.
//
// ⚠ WITHOUT THIS, EVERY READER IN THE TEST SUITE IS OURS. zipread.go is a
// hand-written reader; a writer read only by it can agree with itself about a
// dialect no other tool accepts. archive/zip shares no code with zipread.go and
// was written from [APPNOTE], which is what makes its agreement worth
// something.
func TestArchiveIsReadByArchiveZip(t *testing.T) {
	bodies := map[string]string{"a/photo.jpg": "one", "\U0001f600.txt": "two"}
	var attachments []vnx.Attachment
	for n, b := range bodies {
		attachments = append(attachments, vnx.Attachment{Name: n, Body: strings.NewReader(b)})
	}
	raw := attachmentFixture(t, attachments)
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("archive/zip will not open what this writer produced: %v", err)
	}
	if len(zr.File) != 3 {
		t.Fatalf("archive/zip found %d entries, want 3", len(zr.File))
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("archive/zip will not open %q: %v", f.Name, err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("archive/zip will not read %q: %v", f.Name, err)
		}
		if want, ok := bodies[f.Name]; ok && string(got) != want {
			t.Errorf("%q read back as %q, want %q", f.Name, got, want)
		}
	}

	// And our own reader finds the document and lists the attachments.
	c, diags, err := vnx.DecodeContainer(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("reading our own archive produced diagnostics: %v", diags)
	}
	if len(c.Attachments) != 2 {
		t.Errorf("attachments = %v, want the two that were written", c.Attachments)
	}
}

// TestTheWriterRefusesWhatTheReaderWouldRefuse. A name this library writes is a
// name it would accept, so the checks are the read side's, plus the one the
// UTF-8 filename flag adds: §7.1 sets that flag on every entry, and a name that
// is not valid UTF-8 makes it a false statement about the bytes after it.
func TestTheWriterRefusesWhatTheReaderWouldRefuse(t *testing.T) {
	for _, name := range []string{
		"../escape.jpg", "/absolute.jpg", `back\slash.jpg`, "C:/drive.jpg", "",
		"bad\xff\xfename.jpg",
	} {
		var b bytes.Buffer
		err := vnx.WriteContainerWithAttachments(&b, vnx.RootDocumentName,
			func(w io.Writer) error { _, err := w.Write([]byte("{}\n")); return err },
			[]vnx.Attachment{{Name: name, Body: strings.NewReader("x")}})
		if err == nil {
			t.Errorf("the writer accepted the attachment name %q", name)
		}
	}
	// A duplicate, including against the root document's own name in another
	// case, for the reason §7 gives the reader: language runtimes resolve a
	// repeated name differently, so one archive would yield two documents.
	for _, dup := range [][]string{{"a.jpg", "a.jpg"}, {"DOC.VNX"}, {"x.jpg", "X.JPG"}} {
		var attachments []vnx.Attachment
		for _, n := range dup {
			attachments = append(attachments, vnx.Attachment{Name: n, Body: strings.NewReader("x")})
		}
		var b bytes.Buffer
		err := vnx.WriteContainerWithAttachments(&b, vnx.RootDocumentName,
			func(w io.Writer) error { _, err := w.Write([]byte("{}\n")); return err }, attachments)
		if err == nil {
			t.Errorf("the writer accepted %q beside %q", dup, vnx.RootDocumentName)
		}
	}
}

// TestAnAttachmentNamedLikeADocumentIsStillAnAttachment. §7's second rule: the
// root document is chosen by name and position, and every other entry is an
// attachment whatever its name ends in. The writer must not reorder on a
// suffix.
func TestAnAttachmentNamedLikeADocumentIsStillAnAttachment(t *testing.T) {
	raw := attachmentFixture(t, []vnx.Attachment{{Name: "a.vnx", Body: strings.NewReader("{}\n")}})
	locals := walkCentralDirectory(t, raw)
	if locals[0].name != vnx.RootDocumentName {
		t.Errorf("first entry is %q; the root document comes first so §7's fallback lands on it",
			locals[0].name)
	}
	c, _, err := vnx.DecodeContainer(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Attachments) != 1 || c.Attachments[0].Path != "a.vnx" {
		t.Errorf("attachments = %v, want [a.vnx]", c.Attachments)
	}
}
