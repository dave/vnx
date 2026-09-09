package vnx

// WRITING a container — the other half of container.go.
//
// ⚠ IT IS A WRAPPER, NEVER A SECOND ENCODER. The whole reason this file is
// nine lines of zip plumbing and nothing else is that the compressed spelling
// of a format must be the SAME BYTES as the plain one, deflated. A container
// writer that built its own document — or reached for its own number
// formatting — would be a second encoding path, and the coordinate-rounding
// obligation §4.5 puts on the writers (round.go, and the corpus-wide assertion
// in writer_precision_test.go) would hold on one path and not the other, with
// nothing to notice. So `produce` is the plain writer, unchanged, handed a
// different io.Writer.
//
// ⚠ AND IT STREAMS, because zip.Writer only ever needs an io.Writer: sizes and
// CRCs go into a data descriptor after each entry rather than into a header
// that would have to be seeked back to. So the bytes reach the destination as
// the encoder produces them, exactly as the uncompressed path does, and
// vnxbridge's chunkWriter keeps counting them and keeps checking for
// cancellation between chunks. What the byte count MEANS is unchanged — it was
// always "bytes into the temporary", never "bytes of document".

import (
	"archive/zip"
	"fmt"
	"io"
	"strings"
)

// WriteContainer wraps what `produce` writes in a zip archive holding exactly
// one entry, called `name`, at the archive root.
//
// ⚠ THE NAME MUST BE AT THE ROOT, AND THAT IS CHECKED RATHER THAN ASSUMED.
// [pickRootDocument] means "at the root" literally — a document at a/doc.vnx
// is found only by the third-party fallback, and only if nothing else in the
// archive ends in .vnx. A writer that put its own document in a subdirectory
// would produce a file our own reader treats as somebody else's. The same
// check the READ side runs on every entry ([checkEntryPath]) runs here too, so
// the names this library writes are names it would accept.
//
// A producer error comes back AS IT IS, unwrapped: an encoder that refused a
// document has not failed to make an archive, and renaming its refusal would
// send the caller looking at the zip layer for a defect that is not there.
func WriteContainer(w io.Writer, name string, produce func(io.Writer) error) error {
	if err := checkEntryPath(name); err != nil {
		return err
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("%w: root document %q is not at the archive root", ErrContainer, name)
	}
	zw := zip.NewWriter(w)
	// ⚠ CreateHeader RATHER THAN Create, AND EVERY FIELD HERE IS FIXED BY SPEC
	// §7.1, WHICH REQUIRES TWO CONFORMING WRITERS TO PRODUCE THE SAME BYTES.
	//
	// Method: Store, not Deflate. DEFLATE HAS NO CANONICAL OUTPUT — for one
	// corpus document Go's compress/flate emits 116 bytes where zlib emits 108,
	// and no zlib level reproduces the other. Java and Python agree because both
	// are zlib; Go is clean-room and agrees with neither. A deflated archive is
	// byte-reproducible only by the language that wrote it.
	//
	// ⚠ THIS WRITER DOES NOT YET MEET §7.1 IN FULL, and the gap is pinned by
	// TestContainerFieldsArchiveZipOverrides rather than left to be discovered.
	// archive/zip overwrites the version fields and, if Modified is SET to the
	// MS-DOS epoch, adds a nine-byte extended-timestamp extra field that §7.1
	// forbids — so the obvious repair of the date breaks a different rule. A
	// zero Modified writes MS-DOS date 0x0000, which decodes to 1979-11-30 and
	// is not the epoch either. Meeting §7.1 needs a zip writer of our own, the
	// same conclusion §4.5.5 reached about encoding/json.
	//
	// CreatorVersion and ReaderVersion are 10, that is 1.0, because a
	// stored-only archive requires 1.0 and the field states a requirement rather
	// than a habit. ExternalAttrs and CreatorVersion's system byte stay zero:
	// they encode the WRITING MACHINE rather than the content, and left alone
	// they make one library on two platforms emit two different archives for one
	// document.
	entry, err := zw.CreateHeader(&zip.FileHeader{
		Name:           name,
		Method:         zip.Store,
		CreatorVersion: 10,
		ReaderVersion:  10,
		ExternalAttrs:  0,
	})
	if err != nil {
		return fmt.Errorf("%w: creating %q in the archive: %s", ErrContainer, name, err)
	}
	if err := produce(entry); err != nil {
		return err
	}
	// Close writes the central directory. Without it the archive is a prefix of
	// one, and every reader refuses it — including ours.
	if err := zw.Close(); err != nil {
		return fmt.Errorf("%w: finishing the archive: %s", ErrContainer, err)
	}
	return nil
}

// EncodeContainerTo streams a Document to w as a `.vnz` archive.
//
// It is [EncodeTo] inside [WriteContainer] and nothing else, so a `.vnz` holds
// byte-for-byte what a `.vnx` of the same document holds. [DecodeContainer]
// reads what this writes; TestVNZRoundTripsThroughOurOwnImporter proves the
// pair rather than asserting it.
func EncodeContainerTo(w io.Writer, doc *Document) error {
	return WriteContainer(w, RootDocumentName, func(inner io.Writer) error {
		return EncodeTo(inner, doc)
	})
}
