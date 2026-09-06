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
	// ⚠ CreateHeader RATHER THAN Create, AND Modified IS LEFT AT ITS ZERO
	// VALUE ON PURPOSE. Create would do the same thing today, but a zip entry
	// carries a modification time, and stamping "now" into it would make two
	// exports of the same library differ byte for byte — which is the property
	// encode_determinism_test.go exists to hold for the document inside. A
	// zero Modified writes the MS-DOS epoch, which is a constant.
	entry, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
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
