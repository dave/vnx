package vnx

// WRITING a container — the other half of container.go.
//
// ⚠ IT IS A WRAPPER, NEVER A SECOND ENCODER. The whole reason this file is zip
// plumbing and nothing else is that the compressed spelling of a format must be
// the same document as the plain one. A container writer that built its own
// document — or reached for its own number formatting — would be a second
// encoding path, and the coordinate-rounding obligation §4.1 puts on the
// writers (round.go, and the corpus-wide assertion in writer_precision_test.go)
// would hold on one path and not the other, with nothing to notice. So
// `produce` is the plain writer, unchanged, handed a different io.Writer.
//
// ⚠ AND IT STREAMS. §7.1 constrains no header field that must be known before
// its payload, so an entry is compressed as it is written and neither the
// document nor an attachment is ever held whole.

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Attachment is one file stored beside the document in a `.vnz`.
//
// Body is read once, in full, when the archive is written.
type Attachment struct {
	Name string
	Body io.Reader
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
// The document is written FIRST, which §7.1 requires: §7's fallback takes the
// first `.vnx` entry in archive order, so writing the document first makes the
// fallback and the preferred-name rule land on the same entry. The attachments
// follow in name order, so that two exports of one library agree with each
// other whatever order the caller supplied.
//
// Every entry's modification time stays at its zero value, so two archives of
// one document are one file. [WriteContainerAt] stamps a time instead.
func WriteContainerWithAttachments(w io.Writer, name string, produce func(io.Writer) error, attachments []Attachment) error {
	return writeContainer(w, time.Time{}, name, produce, attachments)
}

// WriteContainerAt is [WriteContainerWithAttachments] with every entry dated
// `modified`, for a caller whose archive is a file for a person: with no date,
// an unzip tool shows 30 November 1979, what the empty DOS date fields decode to.
//
// The DOS date fields hold the wall clock of modified's location. A zero time
// is refused: it is the same 1979 date, reached by a caller that meant a date.
func WriteContainerAt(w io.Writer, modified time.Time, name string, produce func(io.Writer) error, attachments []Attachment) error {
	if modified.IsZero() {
		return fmt.Errorf("%w: entry %q has a zero modification time", ErrContainer, name)
	}
	return writeContainer(w, modified, name, produce, attachments)
}

func writeContainer(w io.Writer, modified time.Time, name string, produce func(io.Writer) error, attachments []Attachment) error {
	if err := checkAttachmentName(name); err != nil {
		return err
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("%w: root document %q is not at the archive root", ErrContainer, name)
	}
	seen := map[string]bool{strings.ToLower(name): true}
	rest := make([]Attachment, len(attachments))
	copy(rest, attachments)
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name })

	entries := make([]archiveEntry, 0, len(rest)+1)
	entries = append(entries, archiveEntry{name: name, body: produce})
	for _, a := range rest {
		if err := checkAttachmentName(a.Name); err != nil {
			return err
		}
		key := strings.ToLower(a.Name)
		if seen[key] {
			return fmt.Errorf("%w: duplicate entry %q", ErrContainer, a.Name)
		}
		seen[key] = true
		if a.Body == nil {
			return fmt.Errorf("%w: attachment %q has no body", ErrContainer, a.Name)
		}
		body := a.Body
		entries = append(entries, archiveEntry{name: a.Name, body: func(w io.Writer) error {
			_, err := io.Copy(w, body)
			return err
		}})
	}
	return writeZipArchive(w, modified, entries)
}

// EncodeContainerTo writes a Document to w as a `.vnz` archive.
//
// It is [EncodeTo] inside [WriteContainer] and nothing else, so a `.vnz` holds
// exactly the document a `.vnx` of the same model holds. [DecodeContainer]
// reads what this writes; TestEncodeContainerToIsReadBackByDecodeContainer
// proves the pair rather than asserting it.
func EncodeContainerTo(w io.Writer, doc *Document) error {
	return WriteContainer(w, RootDocumentName, func(inner io.Writer) error {
		return EncodeTo(inner, doc)
	})
}
