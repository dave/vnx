package vnx_test

// The container READER against §7, on archives no conforming writer can produce.
//
// ⚠ EVERY ARCHIVE HERE IS BUILT BY HAND, and that is what the tests are for. §7
// has a reader accept a far wider range than §7.1 lets a writer emit: a deferred
// size, an over-declared size, ZIP64 records, a deflated entry, an unusual
// compression method. Round-tripping our own writer cannot reach any of them,
// because the writer is forbidden to make them — so a reader tested only against
// what we write is tested against the narrowest input it will ever see.
//
// The reason to care is not tidiness. Until this file existed the read path was
// archive/zip, which REFUSES a conforming archive: it reads an entry's declared
// byte count, checksums what it read, and so consumes past the payload of an
// entry that over-declares. Nothing in the suite noticed, because nothing in the
// suite could build one.

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"hash/crc32"
	"runtime"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

const smallDoc = "{\n  \"vnx\": \"0.10.0\"\n}\n"

// entrySpec is one entry to assemble, with every field a conforming writer would
// fix left open so a test can lie about exactly one of them.
type entrySpec struct {
	name    string
	payload []byte
	method  uint16
	flags   uint16

	// Declared sizes. Zero means "use len(payload)"; setting them is how an
	// archive over-declares.
	declaredSize uint32
	// deferred writes the local header with bit 3 set, zeros for the CRC and
	// both sizes, and a data descriptor after the payload. The central directory
	// still carries the truth, which is where §7 tells a reader to look.
	deferred bool
	// zip64 saturates the central directory's sizes and offset and moves them
	// into a ZIP64 extra field.
	zip64 bool
	// crcOverride replaces the checksum in both headers.
	crcOverride *uint32
	// inflatedSize replaces the declared UNCOMPRESSED size, which for a deflated
	// entry is the number a reader must not allocate from.
	inflatedSize *uint32
}

type assembleOpts struct {
	// comment is the archive comment, which a reader must not mistake for the
	// end of the archive.
	comment string
	// saturateEOCD writes 0xffff/0xffffffff in the end-of-central-directory
	// record and adds the ZIP64 record and locator the fields point at.
	saturateEOCD bool
}

func le16(b *bytes.Buffer, v uint16) { _ = binary.Write(b, binary.LittleEndian, v) }
func le32(b *bytes.Buffer, v uint32) { _ = binary.Write(b, binary.LittleEndian, v) }
func le64(b *bytes.Buffer, v uint64) { _ = binary.Write(b, binary.LittleEndian, v) }

// assemble writes a ZIP archive from specs, lies included.
func assemble(specs []entrySpec, opts assembleOpts) []byte {
	var out, directory bytes.Buffer
	for _, s := range specs {
		crc := crc32.ChecksumIEEE(s.payload)
		if s.crcOverride != nil {
			crc = *s.crcOverride
		}
		size := uint32(len(s.payload))
		if s.declaredSize != 0 {
			size = s.declaredSize
		}
		inflated := size
		if s.inflatedSize != nil {
			inflated = *s.inflatedSize
		}
		flags := s.flags
		if s.deferred {
			flags |= 0x0008
		}
		offset := uint32(out.Len())

		le32(&out, 0x04034b50)
		le16(&out, 20)
		le16(&out, flags)
		le16(&out, s.method)
		le16(&out, 0)
		le16(&out, 0x0021)
		if s.deferred {
			le32(&out, 0)
			le32(&out, 0)
			le32(&out, 0)
		} else {
			le32(&out, crc)
			le32(&out, size)
			le32(&out, inflated)
		}
		le16(&out, uint16(len(s.name)))
		le16(&out, 0)
		out.WriteString(s.name)
		out.Write(s.payload)
		if s.deferred {
			le32(&out, 0x08074b50)
			le32(&out, crc)
			le32(&out, uint32(len(s.payload)))
			le32(&out, inflated)
		}

		var extra bytes.Buffer
		centralSize, centralInflated, centralOffset := size, inflated, offset
		if s.zip64 {
			centralSize, centralInflated, centralOffset = 0xffffffff, 0xffffffff, 0xffffffff
			le16(&extra, 0x0001)
			le16(&extra, 24)
			le64(&extra, uint64(inflated))
			le64(&extra, uint64(size))
			le64(&extra, uint64(offset))
		}
		le32(&directory, 0x02014b50)
		le16(&directory, 20)
		le16(&directory, 20)
		le16(&directory, flags)
		le16(&directory, s.method)
		le16(&directory, 0)
		le16(&directory, 0x0021)
		le32(&directory, crc)
		le32(&directory, centralSize)
		le32(&directory, centralInflated)
		le16(&directory, uint16(len(s.name)))
		le16(&directory, uint16(extra.Len()))
		le16(&directory, 0)
		le16(&directory, 0)
		le16(&directory, 0)
		le32(&directory, 0)
		le32(&directory, centralOffset)
		directory.WriteString(s.name)
		directory.Write(extra.Bytes())
	}

	directoryOffset := uint32(out.Len())
	out.Write(directory.Bytes())

	count, dirSize, dirOffset := uint16(len(specs)), uint32(directory.Len()), directoryOffset
	if opts.saturateEOCD {
		zip64At := uint64(out.Len())
		le32(&out, 0x06064b50)
		le64(&out, 44)
		le16(&out, 45)
		le16(&out, 45)
		le32(&out, 0)
		le32(&out, 0)
		le64(&out, uint64(len(specs)))
		le64(&out, uint64(len(specs)))
		le64(&out, uint64(directory.Len()))
		le64(&out, uint64(directoryOffset))
		le32(&out, 0x07064b50)
		le32(&out, 0)
		le64(&out, zip64At)
		le32(&out, 1)
		count, dirSize, dirOffset = 0xffff, 0xffffffff, 0xffffffff
	}

	le32(&out, 0x06054b50)
	le16(&out, 0)
	le16(&out, 0)
	le16(&out, count)
	le16(&out, count)
	le32(&out, dirSize)
	le32(&out, dirOffset)
	le16(&out, uint16(len(opts.comment)))
	out.WriteString(opts.comment)
	return out.Bytes()
}

func stored(name, body string) entrySpec {
	return entrySpec{name: name, payload: []byte(body)}
}

func readArchive(t *testing.T, raw []byte) (*vnx.Container, []vnx.Diagnostic) {
	t.Helper()
	c, diags, err := vnx.DecodeContainer(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("the reader refused an archive §7 requires it to accept: %v", err)
	}
	return c, diags
}

// TestAnOverDeclaredSizeIsAccepted is §7's size rule, and it replaces a test that
// pinned the opposite because archive/zip could not do this.
//
// ⚠ THE DECLARED SIZE IS A CEILING AND THE CHECKSUM IS THE AUTHORITY. Reading the
// declared count consumes past the payload, so the CRC then disagrees and a
// conforming archive is refused — which is precisely the failure this replaces.
// §7 forbids refusing on the discrepancy and does not require noticing it.
func TestAnOverDeclaredSizeIsAccepted(t *testing.T) {
	e := stored("doc.vnx", smallDoc)
	e.declaredSize = uint32(len(smallDoc)) + 7
	c, diags := readArchive(t, assemble([]entrySpec{e}, assembleOpts{}))
	if c.Document.Version != "0.10.0" {
		t.Errorf("version = %q, want 0.10.0", c.Document.Version)
	}
	if len(diags) != 0 {
		t.Errorf("an accepted archive produced diagnostics: %v", diags)
	}
}

// TestAShortDeclaredSizeIsRefused. The other half of the same rule: §7 does not
// ask a reader to accept a WRONG payload, and it needs no size comparison to
// catch one. A declaration short of the payload leaves the CRC disagreeing.
func TestAShortDeclaredSizeIsRefused(t *testing.T) {
	e := stored("doc.vnx", smallDoc)
	e.declaredSize = uint32(len(smallDoc)) - 4
	raw := assemble([]entrySpec{e}, assembleOpts{})
	if _, _, err := vnx.DecodeContainer(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Error("a payload four bytes shorter than the document was accepted; the CRC must catch it")
	}
}

// TestAStoredEntrysTwoDeclaredSizesMayDisagree is §7's third size rule, and it
// replaces a check that refused on exactly this. For a stored entry the two
// declared sizes are one quantity, so a disagreement IS a defect in the archive
// — but it is a defect in a size field, and §7 forbids refusing on it where the
// CRC over the bytes present agrees.
//
// ⚠ THE FAULT IS IN THE UNCOMPRESSED FIELD AND THE PAYLOAD IS WHOLE, which is
// what separates this from TestAShortDeclaredSizeIsRefused below it: nothing is
// wrong with the bytes, so nothing may be refused or dropped. §7 permits a
// diagnostic and does not require one; corpus/accept/container-sizes-disagree.vnz
// expects none, and this pins the same.
//
// corpus/accept/container-sizes-disagree.vnz covers the root document. This adds
// the ATTACHMENT, which the corpus file cannot separate from it: a reader that
// dropped such an attachment with a diagnostic would still satisfy the corpus
// case's root, and §7 forbids the drop.
func TestAStoredEntrysTwoDeclaredSizesMayDisagree(t *testing.T) {
	inflated := uint32(len(smallDoc)) + 7
	root := stored("doc.vnx", smallDoc)
	root.inflatedSize = &inflated

	const body = "the bytes as written"
	attachmentInflated := uint32(len(body)) + 11
	attachment := stored("photos/summit.jpg", body)
	attachment.inflatedSize = &attachmentInflated

	c, diags := readArchive(t, assemble([]entrySpec{root, attachment}, assembleOpts{}))
	if c.Document.Version != "0.10.0" {
		t.Errorf("version = %q, want 0.10.0", c.Document.Version)
	}
	if len(c.Attachments) != 1 || c.Attachments[0].Path != "photos/summit.jpg" {
		t.Fatalf("attachments = %q, want photos/summit.jpg: §7 forbids dropping it", c.Attachments)
	}
	got, err := c.Attachments[0].Bytes()
	if err != nil {
		t.Fatalf("reading the attachment: %v", err)
	}
	if string(got) != body {
		t.Errorf("attachment content = %q, want %q: the bytes PRESENT are what is read", got, body)
	}
	if len(diags) != 0 {
		t.Errorf("an accepted archive produced diagnostics: %v", diags)
	}
}

// TestSizesDisagreeingDoesNotSuspendTheChecksum. The companion to the rule
// above, and the thing the corpus file cannot say alone: not comparing the two
// declared sizes removes a check on a HEADER FIELD and leaves the CRC standing.
//
// ⚠ WITHOUT THIS, A READER THAT CHECKED NOTHING WOULD PASS EVERY OTHER TEST
// HERE. The corpus accept case proves only that a conforming archive is read.
// This proves the same archive shape is still refused once its BYTES are wrong,
// so the change is a narrowed check rather than an abandoned one.
func TestSizesDisagreeingDoesNotSuspendTheChecksum(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
		make func() entrySpec
	}{{
		// The sizes disagree AND a payload byte is wrong. The header now says
		// nothing a reader would refuse on, so only the checksum can catch it.
		name: "corrupt payload",
		want: "fails its CRC",
		make: func() entrySpec {
			e := stored("doc.vnx", strings.Replace(smallDoc, "0.10.0", "0.10.1", 1))
			crc := crc32.ChecksumIEEE([]byte(smallDoc))
			inflated := uint32(len(smallDoc)) + 7
			e.crcOverride, e.inflatedSize = &crc, &inflated
			return e
		},
	}, {
		// Sizes disagreeing does not license reading fewer bytes than the
		// compressed declaration either: the payload is four bytes short of what
		// the CRC was computed over.
		name: "short payload",
		want: "fails its CRC",
		make: func() entrySpec {
			e := stored("doc.vnx", smallDoc)
			e.declaredSize = uint32(len(smallDoc)) - 4
			inflated := uint32(len(smallDoc)) + 7
			e.inflatedSize = &inflated
			return e
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			raw := assemble([]entrySpec{tc.make()}, assembleOpts{})
			_, _, err := vnx.DecodeContainer(bytes.NewReader(raw), int64(len(raw)))
			if err == nil {
				t.Fatal("a corrupt entry was accepted; dropping the size comparison must not drop the CRC")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal is %q, which does not name the checksum as the fault", err)
			}
		})
	}
}

// TestADeferredSizeIsRead. §7: a reader MUST accept an archive whose entries
// defer their sizes to a data descriptor, and MUST take the sizes from the
// CENTRAL DIRECTORY rather than from a local header.
//
// ⚠ THE LOCAL HEADER HERE DECLARES ZERO, so a reader that trusts it reads an
// empty document and reports a malformed one — and a reader that trusts it while
// also comparing sizes refuses the archive for a "surprising" size. Both are the
// hardening §7 warns against.
func TestADeferredSizeIsRead(t *testing.T) {
	e := stored("doc.vnx", smallDoc)
	e.deferred = true
	c, _ := readArchive(t, assemble([]entrySpec{e}, assembleOpts{}))
	if c.Document.Version != "0.10.0" {
		t.Errorf("version = %q, want 0.10.0 — the sizes must come from the central directory", c.Document.Version)
	}
}

// TestAZip64ArchiveIsRead. §7 requires a reader to understand the ZIP64
// extensions and §7.1 tells a writer not to emit them, so this is unreachable
// from our own writer by design.
//
// ⚠ AN ARCHIVE WITH ZIP64 RECORDS READS CORRECTLY AGAINST A PARSER THAT IGNORES
// EXTRA FIELDS, which is why the fixture saturates BOTH the entry's fields and
// the end-of-central-directory record. A case that saturated neither would pass
// against a reader with no ZIP64 support at all: the fixture would carry the
// records and nothing would consult them.
func TestAZip64ArchiveIsRead(t *testing.T) {
	e := stored("doc.vnx", smallDoc)
	e.zip64 = true
	raw := assemble([]entrySpec{e}, assembleOpts{saturateEOCD: true})
	if !bytes.Contains(raw, []byte{0x50, 0x4b, 0x06, 0x06}) {
		t.Fatal("the fixture carries no ZIP64 end-of-central-directory record, so it tests nothing")
	}
	c, _ := readArchive(t, raw)
	if c.Document.Version != "0.10.0" {
		t.Errorf("version = %q, want 0.10.0", c.Document.Version)
	}
}

// TestAnUnusableMethodDependsOnWhichEntryCarriesIt is §7's split remedy: the root
// document refuses the archive, an attachment is dropped with a diagnostic and
// the document is still returned.
//
// ⚠ THE ASYMMETRY IS THE RULE, NOT AN IMPLEMENTATION DETAIL. A reader that fails
// the stream at the first unreadable entry takes the rest of the archive with it,
// and refusing a whole library because one photograph used an unusual method
// would be disproportionate.
func TestAnUnusableMethodDependsOnWhichEntryCarriesIt(t *testing.T) {
	const bzip2Method = 12

	// ⚠ THE PAYLOAD IS A VALID DOCUMENT AND THE METHOD IS THE ONLY FAULT. An
	// archive whose bzip2-labelled entry held unparseable bytes would be refused
	// by the JSON decoder instead, and this would pass without the method rule
	// existing at all. The refusal must also NAME the method: "not valid deflate
	// data" describes a corrupt archive rather than one using a method this format
	// does not carry, and a caller acts on the difference.
	root := stored("doc.vnx", smallDoc)
	root.method = bzip2Method
	raw := assemble([]entrySpec{root}, assembleOpts{})
	_, _, err := vnx.DecodeContainer(bytes.NewReader(raw), int64(len(raw)))
	if err == nil {
		t.Error("an archive whose ROOT DOCUMENT uses an unusable method was accepted; there is no document to return")
	} else if !strings.Contains(err.Error(), "compression method 12") {
		t.Errorf("the refusal is %q, which does not say the method is the fault", err)
	}

	bad := stored("photos/a b~c.jpg", "would be bzip2")
	bad.method = bzip2Method
	c, diags := readArchive(t, assemble([]entrySpec{stored("doc.vnx", smallDoc), bad, stored("ok.jpg", "fine")}, assembleOpts{}))
	if len(c.Attachments) != 1 || c.Attachments[0].Path != "ok.jpg" {
		t.Errorf("attachments = %q, want just ok.jpg: the unreadable one is dropped", c.Attachments)
	}
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %v, want exactly one for the dropped attachment", diags)
	}
	// §6.3: a fault in an archive entry is located in the separate `#/entries/`
	// space, with the name escaped per RFC 6901 — `~` before `/`, or `a/b`
	// escapes to `a~1b` and then to `a~01b`.
	if want := "#/entries/photos~1a b~0c.jpg"; diags[0].Path != want {
		t.Errorf("path = %q, want %q", diags[0].Path, want)
	}
	if diags[0].Severity != vnx.Warning {
		t.Errorf("severity = %v, want Warning: something the file carried is not in the document", diags[0].Severity)
	}
}

// TestANonASCIINameWithoutTheUTF8FlagIsRefused. The historical alternative
// encoding is CP437, and guessing between the two silently changes an
// attachment's name. A pure-ASCII name means the same thing either way, so only
// a non-ASCII one is ambiguous. Mirrors the Kotlin seat's committed behaviour.
func TestANonASCIINameWithoutTheUTF8FlagIsRefused(t *testing.T) {
	ascii := stored("plain.jpg", "x")
	nonASCII := stored("caf\xc3\xa9.jpg", "x")
	for _, e := range []entrySpec{ascii, nonASCII} {
		raw := assemble([]entrySpec{stored("doc.vnx", smallDoc), e}, assembleOpts{})
		_, _, err := vnx.DecodeContainer(bytes.NewReader(raw), int64(len(raw)))
		wantRefusal := e.name == nonASCII.name
		if wantRefusal && err == nil {
			t.Errorf("%q was accepted without the UTF-8 filename flag", e.name)
		}
		if !wantRefusal && err != nil {
			t.Errorf("%q was refused and it is pure ASCII, which means the same under either encoding: %v", e.name, err)
		}
	}
}

// TestTheEndRecordIsNotFoundInsideAnArchiveComment.
//
// ⚠ SCANNING BACKWARDS FOR FOUR BYTES IS NOT ENOUGH. The signature can occur
// inside an archive comment, and for a STORED entry it can occur inside a
// payload — this format's own documents are text, but an attachment is arbitrary
// bytes. The comment length must account for exactly the bytes after the record,
// which is what distinguishes the real one.
func TestTheEndRecordIsNotFoundInsideAnArchiveComment(t *testing.T) {
	comment := "PK\x05\x06 and then some trailing prose"
	c, _ := readArchive(t, assemble([]entrySpec{stored("doc.vnx", smallDoc)}, assembleOpts{comment: comment}))
	if c.Document.Version != "0.10.0" {
		t.Errorf("version = %q, want 0.10.0", c.Document.Version)
	}
}

// TestAnEOCDSignatureInsideAPayloadDoesNotEndTheArchive. The same hazard from the
// other direction, and the one §7.1's ban on general purpose bit 3 exists for: a
// stored payload may contain any signature at all.
func TestAnEOCDSignatureInsideAPayloadDoesNotEndTheArchive(t *testing.T) {
	c, diags := readArchive(t, assemble([]entrySpec{
		stored("doc.vnx", smallDoc),
		stored("hostile.bin", "PK\x05\x06\x00\x00\x00\x00\x01\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"),
	}, assembleOpts{}))
	if len(diags) != 0 {
		t.Errorf("diagnostics = %v, want none", diags)
	}
	if len(c.Attachments) != 1 {
		t.Errorf("attachments = %v, want the one that was written", c.Attachments)
	}
}

// TestADeclaredInflatedSizeIsALimitNotAnAllocation.
//
// ⚠ THE NUMBER COMES FROM THE ARCHIVE. A twenty-byte entry may declare two
// gigabytes, so a reader that sizes a buffer from it hands an attacker the
// process rather than the document. The Kotlin seat measured exactly that archive
// and took an OutOfMemoryError, which escapes most catch blocks.
//
// ⚠ AND THE REFUSAL ALONE DOES NOT TEST THIS. A reader that allocated the
// declared two gigabytes and then refused would pass an assertion on the error,
// which is why the fixture is a VALID deflate stream — an invalid one is refused
// by the inflater before any size is consulted — and why the allocation is
// measured. The entry inflates to one byte and declares 2 GiB.
func TestADeclaredInflatedSizeIsALimitNotAnAllocation(t *testing.T) {
	huge := uint32(2 << 30)
	e := entrySpec{name: "doc.vnx", payload: deflateBytes(t, "x"), method: 8, inflatedSize: &huge}
	crc := crc32.ChecksumIEEE([]byte("x"))
	e.crcOverride = &crc
	raw := assemble([]entrySpec{e}, assembleOpts{})

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, _, err := vnx.DecodeContainer(bytes.NewReader(raw), int64(len(raw)))
	runtime.ReadMemStats(&after)

	if err == nil {
		t.Fatal("an entry declaring two gigabytes of inflated output was accepted")
	}
	if !strings.Contains(err.Error(), "vnx: unusable container") {
		t.Errorf("refused without ErrContainer: %v", err)
	}
	const bound = 64 << 20
	if grew := after.TotalAlloc - before.TotalAlloc; grew > bound {
		t.Errorf("reading a 1-byte entry declaring %d bytes allocated %d, over the %d bound: the "+
			"declared size is being used as an allocation rather than as a limit", huge, grew, bound)
	}
}

// TestADuplicateNameIsSeenTwice. §7 refuses a duplicate, which a reader can only
// do if it sees both entries: a reader that resolves a repeated name before the
// caller sees it decides, silently and differently between runtimes, a question
// §7 answers explicitly.
func TestADuplicateNameIsSeenTwice(t *testing.T) {
	for _, pair := range [][2]string{{"a.jpg", "a.jpg"}, {"a.jpg", "A.JPG"}, {"doc.vnx", "DOC.VNX"}} {
		specs := []entrySpec{stored("doc.vnx", smallDoc)}
		if pair[0] != "doc.vnx" {
			specs = append(specs, stored(pair[0], "one"))
		}
		specs = append(specs, stored(pair[1], "two"))
		raw := assemble(specs, assembleOpts{})
		if _, _, err := vnx.DecodeContainer(bytes.NewReader(raw), int64(len(raw))); err == nil {
			t.Errorf("%q beside %q was accepted", pair[1], pair[0])
		}
	}
}

// TestADeflatedEntryIsRead. §7 requires stored AND deflate on the read side,
// while §7.1 forbids a writer to deflate, so this is the other rule our own
// writer cannot reach.
func TestADeflatedEntryIsRead(t *testing.T) {
	// Compressible, and still a document: a deflated entry the decoder rejects
	// would pass this test for the wrong reason.
	body := "{\n  \"vnx\": \"0.10.0\",\n  \"generator\": \"" + strings.Repeat("compressible ", 40) + "\"\n}\n"
	deflated := deflateBytes(t, body)
	e := entrySpec{name: "doc.vnx", payload: deflated, method: 8}
	inflated := uint32(len(body))
	e.inflatedSize = &inflated
	crc := crc32.ChecksumIEEE([]byte(body))
	e.crcOverride = &crc
	c, _ := readArchive(t, assemble([]entrySpec{e}, assembleOpts{}))
	if c.Document.Version != "0.10.0" {
		t.Errorf("version = %q, want 0.10.0", c.Document.Version)
	}
}

func deflateBytes(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w, err := flate.NewWriter(&b, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestACorruptAttachmentIsDroppedAndTheRootStillRefuses.
//
// §7 gives an attachment whose bytes cannot be recovered the SAME remedy as one
// whose compression method is unusable: drop it, raise a diagnostic, return the
// document. A damaged photograph must not cost a caller the trail geometry
// beside it.
//
// ⚠ THIS WAS A REFUSAL IN GO FOR ONE COMMIT (6dfebeb), reasoned from
// rust/src/vnz.rs, and Rust was the outlier — Swift, JavaScript and Kotlin all
// dropped. §7 ruled for the majority on 2026-09-12. The corpus case is
// diagnose/container-corrupt-attachment.vnz; this test is the unit-level half,
// and it pins the two things the corpus case cannot say in one file: that the
// CRC is still CHECKED rather than skipped, and that the ROOT document keeps
// the opposite answer.
func TestACorruptAttachmentIsDroppedAndTheRootStillRefuses(t *testing.T) {
	wrong := uint32(0xdeadbeef)

	bad := stored("photos/summit.jpg", "the bytes as written")
	bad.crcOverride = &wrong
	c, diags := readArchive(t, assemble([]entrySpec{
		stored("doc.vnx", smallDoc), bad, stored("ok.jpg", "fine"),
	}, assembleOpts{}))

	// ⚠ THE DROP IS NOT AN EMPTY ATTACHMENT. An entry returned with no bytes is
	// indistinguishable from one the archive never carried, so the damaged name
	// must be ABSENT rather than present-and-empty.
	if len(c.Attachments) != 1 || c.Attachments[0].Path != "ok.jpg" {
		t.Errorf("attachments = %v, want just ok.jpg: the corrupt one is dropped, not blanked", c.Attachments)
	}
	if c.Document == nil || c.Document.Version != "0.10.0" {
		t.Errorf("document = %+v; §7 returns it, damaged photograph or not", c.Document)
	}
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %v, want exactly one for the dropped attachment", diags)
	}
	if diags[0].Code != vnx.CodeAttachmentUnreadable {
		t.Errorf("code = %q, want %q — one code covers both ways to fail to recover an attachment",
			diags[0].Code, vnx.CodeAttachmentUnreadable)
	}
	if want := "#/entries/photos~1summit.jpg"; diags[0].Path != want {
		t.Errorf("path = %q, want %q", diags[0].Path, want)
	}

	// The root document is §7's exception, and for a reason that does not apply
	// to an attachment: there is no document to return, so there is nothing a
	// diagnostic could be attached to.
	root := stored("doc.vnx", smallDoc)
	root.crcOverride = &wrong
	raw := assemble([]entrySpec{root}, assembleOpts{})
	if _, _, err := vnx.DecodeContainer(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Error("an archive whose ROOT DOCUMENT fails its CRC was accepted; there is no document to return")
	}
}
