package vnx_test

// The archive BYTES, field by field, against spec §7.1's table.
//
// ⚠ THIS FILE WALKS THE LOCAL HEADERS, AND THAT IS THE POINT OF IT. Every other
// container assertion in this package resolves through the CENTRAL DIRECTORY —
// archive/zip's reader reads that and reports what it finds there, and
// DecodeContainer goes through archive/zip. So until this file existed, a local
// header could carry any version, any date, a spurious extra field or a
// deferred-size flag, and every test in the package would still pass. §7.1
// fixes each field "in both the local and the central header where a field
// appears in both"; only one of the two was ever being read.
//
// The other half is the same asymmetry from the other side: these assertions
// read the bytes rather than a reader's account of them, and TestArchiveIsRead
// ByArchiveZip keeps a general-purpose implementation on the read side so a
// hand-written writer cannot drift into a private dialect that only our own
// reader accepts.

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"
	"sort"
	"strings"
	"testing"
	"unicode/utf16"

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

// walkLocalHeaders reads the archive forwards, header by header, the way a
// forward-only reader must.
//
// It follows each header's OWN name and extra lengths and its own compressed
// size to find the next one, which is exactly what §7.1's ban on general
// purpose bit 3 is for: with the sizes deferred to a descriptor this walk would
// have to scan for the next signature, and for a stored entry the payload can
// contain that signature.
func walkLocalHeaders(t *testing.T, raw []byte) []zipHeader {
	t.Helper()
	var out []zipHeader
	p := 0
	for p+30 <= len(raw) && binary.LittleEndian.Uint32(raw[p:]) == 0x04034b50 {
		h := zipHeader{
			signature:    binary.LittleEndian.Uint32(raw[p:]),
			needed:       binary.LittleEndian.Uint16(raw[p+4:]),
			flags:        binary.LittleEndian.Uint16(raw[p+6:]),
			method:       binary.LittleEndian.Uint16(raw[p+8:]),
			modTime:      binary.LittleEndian.Uint16(raw[p+10:]),
			modDate:      binary.LittleEndian.Uint16(raw[p+12:]),
			crc:          binary.LittleEndian.Uint32(raw[p+14:]),
			compressed:   binary.LittleEndian.Uint32(raw[p+18:]),
			uncompressed: binary.LittleEndian.Uint32(raw[p+22:]),
			extraLen:     binary.LittleEndian.Uint16(raw[p+28:]),
		}
		nameLen := int(binary.LittleEndian.Uint16(raw[p+26:]))
		start := p + 30 + nameLen + int(h.extraLen)
		if start+int(h.compressed) > len(raw) {
			t.Fatalf("entry %d claims %d bytes of payload and the archive is %d bytes long",
				len(out), h.compressed, len(raw))
		}
		h.name = string(raw[p+30 : p+30+nameLen])
		h.body = raw[start : start+int(h.compressed)]
		out = append(out, h)
		p = start + int(h.compressed)
	}
	if len(out) == 0 {
		t.Fatal("no local header at offset 0, so this walk measured nothing")
	}
	return out
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
	doc := &vnx.Document{Version: "0.1.0", Waypoints: []vnx.Waypoint{{Name: "w", At: vnx.Position{Lon: -3, Lat: 57}}}}
	err := vnx.WriteContainerWithAttachments(&b, vnx.RootDocumentName, func(w io.Writer) error {
		return vnx.EncodeTo(w, doc)
	}, attachments)
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestEverySpec71FieldIsFixedInBothHeaders checks §7.1's table against the
// bytes, in the local header and the central directory both.
//
// ⚠ THE FIELDS THAT ENCODE THE MACHINE ARE THE ONES THAT BITE. A deterministic
// writer on one machine can still be non-deterministic across a fleet: create
// system is 3 on macOS and Linux and 0 on Windows, and external attributes
// carry whatever file mode the writing library stamps in. Neither is visible
// from inside one machine, because writing the same archive twice in a row is
// perfectly reproducible either way — which is why this test reads the field
// rather than comparing two of our own archives.
func TestEverySpec71FieldIsFixedInBothHeaders(t *testing.T) {
	raw := attachmentFixture(t, []vnx.Attachment{{Name: "photo.jpg", Body: []byte("not really a jpeg")}})
	locals := walkLocalHeaders(t, raw)
	centrals := walkCentralDirectory(t, raw)
	if len(locals) != 2 || len(centrals) != 2 {
		t.Fatalf("want two entries, got %d local headers and %d central ones", len(locals), len(centrals))
	}
	for i := range locals {
		l, c := locals[i], centrals[i]
		if l.name != c.name {
			t.Fatalf("entry %d is %q locally and %q in the directory", i, l.name, c.name)
		}
		for _, f := range []struct {
			what string
			got  any
			want any
		}{
			// version needed, in both headers: 1.0, which is what a stored-only
			// archive requires. The field states a requirement, not a habit.
			{"local version needed", l.needed, uint16(10)},
			{"central version needed", c.needed, uint16(10)},
			// version made by: 10, with the create-system byte zero.
			{"version made by", c.madeBy, uint16(10)},
			// the UTF-8 filename flag, and nothing else. Bit 3 in particular is
			// clear, which is what makes walkLocalHeaders able to walk.
			{"local flags", l.flags, uint16(0x0800)},
			{"central flags", c.flags, uint16(0x0800)},
			{"local method", l.method, uint16(0)},
			{"central method", c.method, uint16(0)},
			{"local mod time", l.modTime, uint16(0x0000)},
			{"local mod date", l.modDate, uint16(0x0021)},
			{"central mod time", c.modTime, uint16(0x0000)},
			{"central mod date", c.modDate, uint16(0x0021)},
			{"local extra length", l.extraLen, uint16(0)},
			{"central extra length", c.extraLen, uint16(0)},
			{"entry comment length", c.commentLen, uint16(0)},
			{"the disk the entry starts on", c.diskStart, uint16(0)},
			{"internal attributes", c.internal, uint16(0)},
			{"external attributes", c.external, uint32(0)},
		} {
			if f.got != f.want {
				t.Errorf("%s of %q = %v, want %v (spec §7.1)", f.what, l.name, f.got, f.want)
			}
		}
		// The CRC and both sizes are in the LOCAL header, not deferred, and the
		// two accounts of one entry agree.
		if want := crc32.ChecksumIEEE(l.body); l.crc != want {
			t.Errorf("the local CRC of %q is %08x and its payload checksums to %08x", l.name, l.crc, want)
		}
		if l.crc != c.crc {
			t.Errorf("%q: CRC %08x locally, %08x in the directory", l.name, l.crc, c.crc)
		}
		if l.compressed != l.uncompressed {
			t.Errorf("%q is stored and declares %d compressed against %d uncompressed",
				l.name, l.compressed, l.uncompressed)
		}
		if int(l.compressed) != len(l.body) {
			t.Errorf("%q declares %d bytes and carries %d", l.name, l.compressed, len(l.body))
		}
		if l.compressed != c.compressed || l.uncompressed != c.uncompressed {
			t.Errorf("%q: sizes %d/%d locally, %d/%d in the directory",
				l.name, l.compressed, l.uncompressed, c.compressed, c.uncompressed)
		}
	}
}

// TestEntryOrderIsTheRootThenCodePointSortedAttachments.
//
// ⚠ THE FIXTURE IS BUILT TO SEPARATE CODE-POINT ORDER FROM UTF-16 CODE-UNIT
// ORDER, and it asserts that it does. A name in U+E000..U+FFFF sorts BELOW an
// astral character by code point and ABOVE it by UTF-16 code unit, because the
// high surrogate begins at U+D800. Every ASCII-only fixture agrees under both
// comparators, so it would pass against a writer sorting either way — the same
// shape as a rounding test over data that rounds identically under two routes.
// The check below fails if a later edit makes the fixture agree with itself.
func TestEntryOrderIsTheRootThenCodePointSortedAttachments(t *testing.T) {
	names := []string{"b.jpg", "a/photo.jpg", "\uff3a.txt", "\U0001f600.txt"}
	attachments := make([]vnx.Attachment, len(names))
	for i, n := range names {
		attachments[i] = vnx.Attachment{Name: n, Body: []byte(n)}
	}
	wantCodePoint := []string{vnx.RootDocumentName, "a/photo.jpg", "b.jpg", "\uff3a.txt", "\U0001f600.txt"}

	// What a UTF-16 code-unit sort would have produced, computed rather than
	// asserted, so the fixture's discriminating power is measured here.
	utf16Order := append([]string(nil), names...)
	sort.Slice(utf16Order, func(i, j int) bool {
		a, b := utf16.Encode([]rune(utf16Order[i])), utf16.Encode([]rune(utf16Order[j]))
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})
	wantUTF16 := append([]string{vnx.RootDocumentName}, utf16Order...)
	if strings.Join(wantUTF16, "\x00") == strings.Join(wantCodePoint, "\x00") {
		t.Fatal("this fixture sorts the same way under both comparators, so it cannot tell them " +
			"apart — restore a name above U+FFFF beside one in U+E000..U+FFFF")
	}

	raw := attachmentFixture(t, attachments)
	var got []string
	for _, h := range walkLocalHeaders(t, raw) {
		got = append(got, h.name)
	}
	if strings.Join(got, "\x00") != strings.Join(wantCodePoint, "\x00") {
		t.Errorf("archive order is %q, want %q (spec §7.1: the root document first, then the "+
			"attachments in code-point order; %q is what a UTF-16 code-unit sort gives)",
			got, wantCodePoint, wantUTF16)
	}
	var directory []string
	for _, h := range walkCentralDirectory(t, raw) {
		directory = append(directory, h.name)
	}
	if strings.Join(directory, "\x00") != strings.Join(got, "\x00") {
		t.Errorf("the central directory lists %q where the entries are in the order %q", directory, got)
	}
}

// TestTheArchiveDoesNotDependOnTheOrderTheCallerSupplied. The reason §7.1 sorts
// at all: caller order is not a property of the content, and two exporters over
// one library — one iterating a map, one a sorted list — must not emit two
// archives for one document.
func TestTheArchiveDoesNotDependOnTheOrderTheCallerSupplied(t *testing.T) {
	forward := []vnx.Attachment{
		{Name: "a.jpg", Body: []byte("a")},
		{Name: "b.jpg", Body: []byte("b")},
		{Name: "\uff3a.txt", Body: []byte("z")},
	}
	reversed := []vnx.Attachment{forward[2], forward[1], forward[0]}
	if !bytes.Equal(attachmentFixture(t, forward), attachmentFixture(t, reversed)) {
		t.Error("the same document and attachments in two input orders gave two archives")
	}
}

// TestArchiveIsReadByArchiveZip keeps a general-purpose reader on the other side
// of a hand-written writer.
//
// ⚠ WITHOUT THIS, EVERY READER IN THE TEST SUITE IS OURS. A writer checked only
// against the fields we chose to look at, read only by the reader we wrote, can
// agree with itself about a dialect no other tool accepts. archive/zip shares no
// code with zipwrite.go and was written from [APPNOTE], which is what makes its
// agreement worth something. Carried over from the Kotlin seat, which reads
// every archive it writes back through java.util.zip for the same reason.
func TestArchiveIsReadByArchiveZip(t *testing.T) {
	bodies := map[string]string{"a/photo.jpg": "one", "\U0001f600.txt": "two"}
	var attachments []vnx.Attachment
	for n, b := range bodies {
		attachments = append(attachments, vnx.Attachment{Name: n, Body: []byte(b)})
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
		t.Errorf("attachments = %q, want the two that were written", c.Attachments)
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
			[]vnx.Attachment{{Name: name, Body: []byte("x")}})
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
			attachments = append(attachments, vnx.Attachment{Name: n, Body: []byte("x")})
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
	raw := attachmentFixture(t, []vnx.Attachment{{Name: "a.vnx", Body: []byte("{}\n")}})
	locals := walkLocalHeaders(t, raw)
	if locals[0].name != vnx.RootDocumentName {
		t.Errorf("first entry is %q; the root document comes first so §7's fallback lands on it",
			locals[0].name)
	}
	c, _, err := vnx.DecodeContainer(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Attachments) != 1 || c.Attachments[0] != "a.vnx" {
		t.Errorf("attachments = %q, want [a.vnx]", c.Attachments)
	}
}
