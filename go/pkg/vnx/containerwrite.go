package vnx

// WRITING a container — the other half of container.go.
//
// ⚠ IT IS A WRAPPER, NEVER A SECOND ENCODER. The whole reason this file is
// zip plumbing and nothing else is that the compressed spelling of a format
// must be the SAME BYTES as the plain one. A container writer that built its
// own document — or reached for its own number formatting — would be a second
// encoding path, and the coordinate-rounding obligation §4.5 puts on the
// writers (round.go, and the corpus-wide assertion in
// writer_precision_test.go) would hold on one path and not the other, with
// nothing to notice. So `produce` is the plain writer, unchanged, handed a
// different io.Writer.
//
// ⚠ AND IT BUFFERS, where an earlier version streamed. §7.1 requires the CRC
// and both sizes in the LOCAL HEADER and forbids general purpose bit 3, so an
// entry's length and checksum must be known before its header can be written.
// Streaming was available only through the data descriptor that rule rules out.
// The byte count vnxbridge's chunkWriter keeps still means what it always meant
// — bytes into the temporary, never bytes of document — but it now arrives in
// one block at the end of the entry rather than in chunks, so cancellation
// between chunks no longer interrupts a container export mid-document. The
// uncompressed path is unchanged and still streams.

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// Attachment is one file stored beside the document in a `.vnz`.
type Attachment struct {
	Name string
	Body []byte
}

// WriteContainer wraps what `produce` writes in a zip archive holding exactly
// one entry, called `name`, at the archive root.
func WriteContainer(w io.Writer, name string, produce func(io.Writer) error) error {
	return WriteContainerWithAttachments(w, name, produce, nil)
}

// WriteContainerWithAttachments writes the document `produce` emits as the root
// entry of an archive, with `attachments` stored beside it.
//
// ⚠ THE ROOT NAME MUST BE AT THE ROOT, AND THAT IS CHECKED RATHER THAN ASSUMED.
// [pickRootDocument] means "at the root" literally — a document at a/doc.vnx is
// found only by the third-party fallback, and only if nothing else in the
// archive ends in .vnx. A writer that put its own document in a subdirectory
// would produce a file our own reader treats as somebody else's. The same check
// the READ side runs on every entry ([checkEntryPath], inside
// [checkAttachmentName]) runs here too, so the names this library writes are
// names it would accept.
//
// Duplicate names are refused for the reason §7 gives the reader: language
// runtimes resolve a repeated name differently, so one archive would yield
// different documents in different languages. A writer that can emit such an
// archive hands that problem to every reader downstream.
//
// A producer error comes back AS IT IS, unwrapped: an encoder that refused a
// document has not failed to make an archive, and renaming its refusal would
// send the caller looking at the zip layer for a defect that is not there.
func WriteContainerWithAttachments(w io.Writer, name string, produce func(io.Writer) error, attachments []Attachment) error {
	if err := checkAttachmentName(name); err != nil {
		return err
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("%w: root document %q is not at the archive root", ErrContainer, name)
	}
	seen := map[string]bool{strings.ToLower(name): true}
	entries := make([]zipEntry, 0, len(attachments))
	for _, a := range attachments {
		if err := checkAttachmentName(a.Name); err != nil {
			return err
		}
		key := strings.ToLower(a.Name)
		if seen[key] {
			return fmt.Errorf("%w: duplicate entry %q", ErrContainer, a.Name)
		}
		seen[key] = true
		entries = append(entries, zipEntry{name: a.Name, body: a.Body})
	}

	var doc bytes.Buffer
	if err := produce(&doc); err != nil {
		return err
	}
	return writeZipArchive(w, sortZipEntries(zipEntry{name: name, body: doc.Bytes()}, entries))
}

// EncodeContainerTo writes a Document to w as a `.vnz` archive.
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
