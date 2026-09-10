package vnx

// A ZIP writer of our own, because archive/zip cannot write a conforming
// container.
//
// SPEC.md §7.1 fixes every header field an archive carries, so that two
// conforming writers produce the same bytes for the same document and
// attachments. archive/zip reaches three of those fields on its own terms:
// it overwrites the version fields with 20 where §7.1 fixes 10; a zero
// Modified writes MS-DOS date 0x0000, which decodes to 1979-11-30 rather than
// the 1980-01-01 the table fixes; and setting Modified to the epoch to repair
// that makes it add a nine-byte extended-timestamp extra field, which the same
// table forbids. None of the three is reachable through its API. This file is
// the same conclusion §4.5.5 reached about encoding/json for strings: the
// standard library is a general-purpose writer and the format wants one
// particular archive.
//
// The read side is zipread.go, and it is here for a stronger reason than this
// one: archive/zip cannot READ a conforming archive either. §7 forbids refusing
// an entry that declares more bytes than it holds where the CRC over the bytes
// present agrees, and archive/zip reads the declared count, checksums what it
// read, and refuses. So the pair is ours, and the asymmetry §7 and §7.1 describe
// lives in the two files rather than in one of them being borrowed.
//
// This writer buffers each entry whole. It cannot do otherwise: §7.1 requires
// the CRC and both sizes IN THE LOCAL HEADER and forbids general purpose bit 3,
// so the length and checksum of a payload must be known before its header is
// written. The uncompressed path (encode_stream.go) still streams, and a
// document large enough for that to matter is large enough that the caller
// should be writing a `.vnx`.

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"sort"
	"unicode/utf8"
)

const (
	sigLocalHeader         = 0x04034b50
	sigCentralHeader       = 0x02014b50
	sigEndOfDirectory      = 0x06054b50
	sigZip64EndOfDirectory = 0x06064b50
	sigZip64Locator        = 0x07064b50
	localHeaderSize        = 30
	centralHeaderSize      = 46
	endOfDirectorySize     = 22

	// §7.1's fixed fields, named rather than repeated as literals at the two
	// places each one is written. A field written twice from one constant cannot
	// disagree between the local header and the central directory.
	zipVersion      = 10     // 1.0, which is what a stored-only archive requires
	zipFlags        = 0x0800 // the UTF-8 filename flag, and nothing else
	zipMethodStored = 0
	zipModTime      = 0x0000 // 00:00:00
	zipModDate      = 0x0021 // 1980-01-01
)

// zipEntry is one entry to write, already in the order it will appear.
type zipEntry struct {
	name string
	body []byte
}

// sortZipEntries puts the root document first and the attachments after it in
// CODE-POINT ORDER, as §7.1 requires.
//
// Sorted rather than in the order the caller supplied, because caller order is
// not a property of the content: two exporters over one library, one iterating a
// map and one a sorted list, would otherwise emit different archives for the
// same document while satisfying every other field in §7.1's table.
//
// ⚠ THE ROOT DOCUMENT IS NOT PART OF THE SORT. §7's fallback takes the first
// `.vnx` entry in archive order, so putting the document first makes the
// fallback and the preferred-name rule land on the same entry. A single sort
// over all the names would put `a/photo.jpg` ahead of `doc.vnx`.
//
// sort.Strings IS code-point order here, and that is a fact about the input
// rather than about Go: comparing well-formed UTF-8 bytewise gives the same
// order as comparing code points, because the encoding is designed to. It is
// the languages with UTF-16 strings that need a comparator of their own —
// §4.5.1 says why, and an entry name is exactly where it bites, since a
// document's keys are ASCII and a name may hold anything. Names are checked for
// well-formedness by [checkAttachmentName] before they reach here, which is
// what makes the bytewise comparison equivalent rather than merely usual.
func sortZipEntries(root zipEntry, attachments []zipEntry) []zipEntry {
	rest := make([]zipEntry, len(attachments))
	copy(rest, attachments)
	sort.Slice(rest, func(i, j int) bool { return rest[i].name < rest[j].name })
	return append([]zipEntry{root}, rest...)
}

// writeZipArchive writes entries as a stored ZIP archive meeting §7.1.
func writeZipArchive(w io.Writer, entries []zipEntry) error {
	var directory []byte
	offset := 0
	sw := Stick(w)

	for _, e := range entries {
		name := []byte(e.name)
		size, err := zip32(len(e.body), "entry "+e.name)
		if err != nil {
			return err
		}
		start, err := zip32(offset, "the offset of entry "+e.name)
		if err != nil {
			return err
		}
		sum := crc32.ChecksumIEEE(e.body)

		local := make([]byte, 0, localHeaderSize)
		local = le32(local, sigLocalHeader)
		local = le16(local, zipVersion)
		local = le16(local, zipFlags)
		local = le16(local, zipMethodStored)
		local = le16(local, zipModTime)
		local = le16(local, zipModDate)
		local = le32(local, sum)
		local = le32(local, size) // compressed
		local = le32(local, size) // uncompressed: the same quantity, stored
		local = le16(local, uint16(len(name)))
		local = le16(local, 0) // no extra field
		sw.WriteBytes(local)
		sw.WriteBytes(name)
		sw.WriteBytes(e.body)
		offset += len(local) + len(name) + len(e.body)

		directory = le32(directory, sigCentralHeader)
		directory = le16(directory, zipVersion) // version made by, create system 0
		directory = le16(directory, zipVersion) // version needed
		directory = le16(directory, zipFlags)
		directory = le16(directory, zipMethodStored)
		directory = le16(directory, zipModTime)
		directory = le16(directory, zipModDate)
		directory = le32(directory, sum)
		directory = le32(directory, size)
		directory = le32(directory, size)
		directory = le16(directory, uint16(len(name)))
		directory = le16(directory, 0) // extra
		directory = le16(directory, 0) // comment
		directory = le16(directory, 0) // the disk this entry starts on
		directory = le16(directory, 0) // internal attributes
		directory = le32(directory, 0) // external attributes: a file mode belongs
		directory = le32(directory, start)
		directory = append(directory, name...)
	}

	directoryOffset, err := zip32(offset, "the central directory")
	if err != nil {
		return err
	}
	count := len(entries)
	if count > 0xffff {
		return fmt.Errorf("%w: %d entries is more than a non-ZIP64 archive can count, and §7.1 does not write ZIP64 records", ErrContainer, count)
	}
	sw.WriteBytes(directory)

	end := make([]byte, 0, endOfDirectorySize)
	end = le32(end, sigEndOfDirectory)
	end = le16(end, 0) // this disk
	end = le16(end, 0) // the disk holding the central directory
	end = le16(end, uint16(count))
	end = le16(end, uint16(count))
	end = le32(end, uint32(len(directory)))
	end = le32(end, directoryOffset)
	end = le16(end, 0) // no archive comment
	sw.WriteBytes(end)
	return sw.err
}

// zip32 refuses what a non-ZIP64 archive cannot express, rather than truncating
// it.
//
// §7.1 says ZIP64 records are not written, and the 32-bit fields they exist to
// escape hold sizes and offsets. Writing the low 32 bits of a larger number
// produces an archive that looks valid and decodes to the wrong bytes, so the
// only honest answer at 4 GiB is a refusal. §7 requires a READER to understand
// ZIP64, so a container that big is legal — it is this writer that cannot make
// one.
func zip32(v int, what string) (uint32, error) {
	if v > 0xffffffff {
		return 0, fmt.Errorf("%w: %s is %d bytes, which needs the ZIP64 extensions §7.1 does not write", ErrContainer, what, v)
	}
	return uint32(v), nil
}

func le16(b []byte, v uint16) []byte {
	return binary.LittleEndian.AppendUint16(b, v)
}

func le32(b []byte, v uint32) []byte {
	return binary.LittleEndian.AppendUint32(b, v)
}

// checkAttachmentName holds an attachment name to what §7 lets a reader accept
// plus what the UTF-8 filename flag claims.
//
// The path rules are [checkEntryPath], the same check the read side runs, so a
// name this library writes is a name it would accept. Well-formedness is the
// addition: §7.1 sets the UTF-8 flag on every entry, and a name that is not
// valid UTF-8 makes that flag a false statement about the bytes that follow it.
// It is also what makes the bytewise sort in [sortZipEntries] code-point order.
func checkAttachmentName(name string) error {
	if err := checkEntryPath(name); err != nil {
		return err
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: entry name %q is not valid UTF-8, and §7.1 sets the UTF-8 filename flag on every entry", ErrContainer, name)
	}
	return nil
}
