package vnx

// Writing a container, with archive/zip.
//
// The read side is zipread.go and is NOT archive/zip, for three reader rules
// §7 states that archive/zip cannot meet: it refuses an entry declaring more
// bytes than it holds where §7 requires acceptance, it cannot give a per-entry
// unusable-method remedy, and it collapses a duplicate name rather than seeing
// it twice. The asymmetry §7 and §7.1 describe therefore lives in the two files
// rather than in one of them being borrowed: what a reader accepts is wider
// than what a writer emits, so the writer is the half the standard library can
// do.

import (
	"archive/zip"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

// utf8NameFlag is general purpose bit 11, which §7.1 requires on every entry.
//
// It is set explicitly because archive/zip sets it only for a name that NEEDS
// it — a name outside ASCII — and §7.1 admits no such exception. CreateHeader
// clears the bit only for a header marked NonUTF8, so a flag set here survives.
const utf8NameFlag = 0x800

// archiveEntry is one entry to write. The body is a function rather than a
// buffer so that a large attachment is never held whole: DEFLATE has no
// header field that must be known in advance, so the payload can be copied
// straight through the compressor.
type archiveEntry struct {
	name string
	body func(io.Writer) error
}

// writeZipArchive writes entries as a deflated ZIP archive meeting §7.1.
//
// Every entry carries `modified`. Its zero value writes no date at all, which
// keeps the header constant.
//
// A producer error comes back AS IT IS, unwrapped: an encoder that refused a
// document has not failed to make an archive, and renaming its refusal would
// send the caller looking at the zip layer for a defect that is not there.
func writeZipArchive(w io.Writer, modified time.Time, entries []archiveEntry) error {
	zw := zip.NewWriter(w)
	for _, e := range entries {
		ew, err := zw.CreateHeader(&zip.FileHeader{
			Name:     e.name,
			Method:   zip.Deflate,
			Flags:    utf8NameFlag,
			Modified: modified,
		})
		if err != nil {
			return fmt.Errorf("%w: entry %q: %s", ErrContainer, e.name, err)
		}
		if err := e.body(ew); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("%w: %s", ErrContainer, err)
	}
	return nil
}

// checkAttachmentName holds an attachment name to what §7 lets a reader accept
// plus what the UTF-8 filename flag claims.
//
// The path rules are [checkEntryPath], the same check the read side runs, so a
// name this library writes is a name it would accept. Well-formedness is the
// addition: §7.1 sets the UTF-8 flag on every entry, and a name that is not
// valid UTF-8 makes that flag a false statement about the bytes that follow it.
func checkAttachmentName(name string) error {
	if err := checkEntryPath(name); err != nil {
		return err
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: entry name %q is not valid UTF-8, and §7.1 sets the UTF-8 filename flag on every entry", ErrContainer, name)
	}
	return nil
}
