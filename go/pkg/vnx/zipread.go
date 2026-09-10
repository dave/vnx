package vnx

// READING a ZIP archive, written here rather than taken from archive/zip.
//
// ⚠ archive/zip CANNOT READ A CONFORMING ARCHIVE. §7 forbids a reader to refuse
// an entry on the ground that its declared size exceeds the bytes present where
// the CRC over those bytes agrees. archive/zip reads the DECLARED count,
// checksums what it read, and so consumes past the payload and reports
// "zip: checksum error" — it detects the discrepancy and refuses on it, which is
// exactly and only what the rule forbids. That is a gap in the read path, not a
// deliberate asymmetry, and it is why this file exists beside zipwrite.go.
//
// Two more of §7's questions archive/zip answers on its own terms rather than
// the format's, both of which the Kotlin seat hit first:
//
//   - An unusable COMPRESSION METHOD is a per-entry fact, and what to do about it
//     depends on WHICH entry it is: the root document refuses the archive, an
//     attachment is dropped with a diagnostic. A reader that fails the stream at
//     the first unreadable entry takes the rest of the archive with it.
//   - A DUPLICATE name must be seen twice, in archive order, so it can be
//     refused. A reader that resolves duplicates before the caller sees them
//     decides, silently, a question §7 answers explicitly.
//
// Sizes come from the CENTRAL DIRECTORY, which §7 requires and which is what
// makes an archive written with a data descriptor read at all: its local headers
// declare zero.
//
// It reads through io.ReaderAt and never holds the archive. Headers are read
// where they are and an entry's payload is read when it is asked for, so opening
// a container of photographs costs the central directory rather than the
// library.

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

// The [APPNOTE] record signatures and fixed header sizes this reader walks.
const (
	sigLocalHeader         = 0x04034b50
	sigCentralHeader       = 0x02014b50
	sigEndOfDirectory      = 0x06054b50
	sigZip64EndOfDirectory = 0x06064b50
	sigZip64Locator        = 0x07064b50
	localHeaderSize        = 30
	centralHeaderSize      = 46
	endOfDirectorySize     = 22

	zipMethodStored = 0
)

const (
	zipMethodDeflate = 8
	zipFlagUTF8      = 0x0800
)

// zipEntryHeader is one entry as the central directory states it.
type zipEntryHeader struct {
	name         string
	method       uint16
	flags        uint16
	crc          uint32
	compressed   uint64
	uncompressed uint64
	localOffset  uint64
	// payloadBound is where this entry's payload cannot extend past: the nearest
	// following local header, or the central directory. It is what lets an entry
	// declaring more bytes than it holds be READ rather than refused — see
	// [readZipEntry].
	payloadBound uint64
}

// usableMethod reports whether §7 lets this entry be read at all. It does NOT
// decide what happens to an entry that cannot be: that depends on whether it is
// the root document, and the decision belongs to the caller.
func (e zipEntryHeader) usableMethod() bool {
	return e.method == zipMethodStored || e.method == zipMethodDeflate
}

func (e zipEntryHeader) isDirectory() bool {
	return len(e.name) > 0 && e.name[len(e.name)-1] == '/' && e.uncompressed == 0
}

func zipMalformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrContainer, fmt.Sprintf(format, args...))
}

// readZipDirectory reads every central directory entry, in archive order,
// duplicates included.
func readZipDirectory(r io.ReaderAt, size int64) ([]zipEntryHeader, error) {
	count, directoryOffset, directorySize, err := findEndOfCentralDirectory(r, size)
	if err != nil {
		return nil, err
	}
	if directoryOffset+directorySize > uint64(size) {
		return nil, zipMalformed("the central directory is declared at %d for %d bytes and the archive is %d bytes",
			directoryOffset, directorySize, size)
	}
	directory := make([]byte, directorySize)
	if _, err := r.ReadAt(directory, int64(directoryOffset)); err != nil {
		return nil, zipMalformed("reading the central directory: %s", err)
	}

	entries := make([]zipEntryHeader, 0, count)
	p := 0
	for i := uint64(0); i < count; i++ {
		if p+centralHeaderSize > len(directory) || binary.LittleEndian.Uint32(directory[p:]) != sigCentralHeader {
			return nil, zipMalformed("central directory entry %d is truncated or has a bad signature", i+1)
		}
		e := zipEntryHeader{
			flags:        binary.LittleEndian.Uint16(directory[p+8:]),
			method:       binary.LittleEndian.Uint16(directory[p+10:]),
			crc:          binary.LittleEndian.Uint32(directory[p+16:]),
			compressed:   uint64(binary.LittleEndian.Uint32(directory[p+20:])),
			uncompressed: uint64(binary.LittleEndian.Uint32(directory[p+24:])),
			localOffset:  uint64(binary.LittleEndian.Uint32(directory[p+42:])),
		}
		nameLen := int(binary.LittleEndian.Uint16(directory[p+28:]))
		extraLen := int(binary.LittleEndian.Uint16(directory[p+30:]))
		commentLen := int(binary.LittleEndian.Uint16(directory[p+32:]))
		if p+centralHeaderSize+nameLen+extraLen+commentLen > len(directory) {
			return nil, zipMalformed("central directory entry %d runs past the directory", i+1)
		}
		rawName := directory[p+centralHeaderSize : p+centralHeaderSize+nameLen]
		name, err := decodeEntryName(rawName, e.flags)
		if err != nil {
			return nil, err
		}
		e.name = name
		extra := directory[p+centralHeaderSize+nameLen : p+centralHeaderSize+nameLen+extraLen]
		if err := e.applyZip64(extra); err != nil {
			return nil, err
		}
		entries = append(entries, e)
		p += centralHeaderSize + nameLen + extraLen + commentLen
	}

	// The bound on each payload: the nearest local header after this one, or the
	// central directory. Computed here, once, because it takes every entry to
	// know it and every entry needs it.
	for i := range entries {
		bound := directoryOffset
		for _, other := range entries {
			if other.localOffset > entries[i].localOffset && other.localOffset < bound {
				bound = other.localOffset
			}
		}
		entries[i].payloadBound = bound
	}
	return entries, nil
}

// applyZip64 replaces the saturated 32-bit fields from the ZIP64 extra field.
//
// §7 requires a reader to read a ZIP64 archive and §7.1 tells a writer not to
// make one, which is the ordinary asymmetry. The fields appear in a fixed order
// and carry no tags of their own, so only the saturated ones are present.
func (e *zipEntryHeader) applyZip64(extra []byte) error {
	wantUncompressed := e.uncompressed == 0xffffffff
	wantCompressed := e.compressed == 0xffffffff
	wantOffset := e.localOffset == 0xffffffff
	if !wantUncompressed && !wantCompressed && !wantOffset {
		return nil
	}
	for p := 0; p+4 <= len(extra); {
		id := binary.LittleEndian.Uint16(extra[p:])
		size := int(binary.LittleEndian.Uint16(extra[p+2:]))
		if p+4+size > len(extra) {
			break
		}
		if id != 0x0001 {
			p += 4 + size
			continue
		}
		field := extra[p+4 : p+4+size]
		take := func(dst *uint64) error {
			if len(field) < 8 {
				return zipMalformed("the ZIP64 extra field of %q is too short for the fields it must carry", e.name)
			}
			*dst = binary.LittleEndian.Uint64(field)
			field = field[8:]
			return nil
		}
		for _, want := range []struct {
			on  bool
			dst *uint64
		}{{wantUncompressed, &e.uncompressed}, {wantCompressed, &e.compressed}, {wantOffset, &e.localOffset}} {
			if !want.on {
				continue
			}
			if err := take(want.dst); err != nil {
				return err
			}
		}
		return nil
	}
	return zipMalformed("a field of %q is saturated and the entry carries no ZIP64 extra field", e.name)
}

// decodeEntryName holds a name to what the UTF-8 filename flag claims about it.
//
// §7.1 sets the flag; §7 does not require a reader to refuse an archive without
// it. A name that is pure ASCII means the same thing under either encoding, so
// only a non-ASCII name is ambiguous — and the historical alternative is CP437,
// where guessing silently changes an attachment's name. Mirrors the Kotlin
// seat's committed behaviour rather than inventing a third answer.
func decodeEntryName(raw []byte, flags uint16) (string, error) {
	if flags&zipFlagUTF8 == 0 {
		for _, b := range raw {
			if b&0x80 != 0 {
				return "", zipMalformed("entry name %q is not ASCII and the archive does not set the UTF-8 filename flag", raw)
			}
		}
	}
	return string(raw), nil
}

// readZipEntry returns one entry's bytes, verified against its CRC.
//
// ⚠ THE DECLARED SIZE IS A CEILING, NOT A LENGTH, WHICH IS §7'S SIZE RULE. An
// entry declaring more bytes than it holds is read as the bytes that are there,
// bounded by the next local header or the central directory, and the CHECKSUM
// decides whether that was the whole payload. Reading the declared count instead
// consumes past the payload and produces a checksum error on a conforming
// archive, which is what archive/zip does. §7 forbids refusing on the
// discrepancy; it does not require noticing it, and this reader notices only
// because the bound is already in hand.
//
// A SHORT declaration needs nothing special: the CRC over too few bytes
// disagrees, so the same check catches it.
func readZipEntry(r io.ReaderAt, e zipEntryHeader) ([]byte, error) {
	if !e.usableMethod() {
		return nil, zipMalformed("entry %q uses compression method %d, which is neither stored nor deflate", e.name, e.method)
	}
	header := make([]byte, localHeaderSize)
	if _, err := r.ReadAt(header, int64(e.localOffset)); err != nil {
		return nil, zipMalformed("the local header for %q is missing or truncated: %s", e.name, err)
	}
	if binary.LittleEndian.Uint32(header) != sigLocalHeader {
		return nil, zipMalformed("the local header for %q has a bad signature", e.name)
	}
	// The LOCAL header's own name and extra lengths say where the payload
	// starts. They are different fields from the central directory's, and an
	// archive may legitimately carry an extra field in one and not the other.
	nameLen := uint64(binary.LittleEndian.Uint16(header[26:]))
	extraLen := uint64(binary.LittleEndian.Uint16(header[28:]))
	dataStart := e.localOffset + localHeaderSize + nameLen + extraLen
	if dataStart > e.payloadBound {
		return nil, zipMalformed("the payload of %q starts past where it must end", e.name)
	}
	n := e.compressed
	if available := e.payloadBound - dataStart; n > available {
		n = available
	}
	raw := make([]byte, n)
	if _, err := r.ReadAt(raw, int64(dataStart)); err != nil && !(err == io.EOF && uint64(len(raw)) == n) {
		return nil, zipMalformed("reading the payload of %q: %s", e.name, err)
	}

	var body []byte
	switch e.method {
	case zipMethodStored:
		// A stored entry's two sizes are one quantity by definition, so a header
		// claiming otherwise is lying about one of them. Over-declaring both is
		// the case §7 accepts and is not this.
		if e.compressed != e.uncompressed {
			return nil, zipMalformed("%q is stored and declares %d compressed against %d uncompressed",
				e.name, e.compressed, e.uncompressed)
		}
		body = raw
	default:
		inflated, err := inflateBounded(e.name, raw, e.uncompressed)
		if err != nil {
			return nil, err
		}
		body = inflated
	}
	if got := crc32.ChecksumIEEE(body); got != e.crc {
		return nil, zipMalformed("%q fails its CRC: the archive says %08x and the bytes are %08x", e.name, e.crc, got)
	}
	return body, nil
}

// inflateBounded uses the declared size as a LIMIT rather than as an allocation.
//
// ⚠ THE DECLARED SIZE COMES FROM THE ARCHIVE, so a hostile twenty-byte entry can
// declare two gigabytes. Sizing a buffer from it hands an attacker the process
// rather than the document. The Kotlin seat measured exactly that archive on
// 2026-09-10 and took an OutOfMemoryError, which escapes most catch blocks; Go
// has no equivalent escape but it has the same allocation.
//
// §9's resource bound for a merely large HONEST archive is a separate matter and
// this library still does not implement one.
func inflateBounded(name string, raw []byte, declared uint64) ([]byte, error) {
	fr := flate.NewReader(bytes.NewReader(raw))
	defer fr.Close()
	// One byte past the declaration, so overrunning it is observable rather than
	// silently truncated.
	body, err := io.ReadAll(io.LimitReader(fr, int64(declared)+1))
	if err != nil {
		return nil, zipMalformed("%q is not valid deflate data: %s", name, err)
	}
	if uint64(len(body)) > declared {
		return nil, zipMalformed("%q inflates to more than the %d bytes its header declares", name, declared)
	}
	if uint64(len(body)) != declared {
		return nil, zipMalformed("%q inflated to %d bytes and its header declares %d", name, len(body), declared)
	}
	return body, nil
}

// findEndOfCentralDirectory locates the record and follows the ZIP64 records
// where the 32-bit fields are saturated.
func findEndOfCentralDirectory(r io.ReaderAt, size int64) (count, offset, directorySize uint64, err error) {
	tailLen := int64(endOfDirectorySize + 0xffff)
	if tailLen > size {
		tailLen = size
	}
	if tailLen < endOfDirectorySize {
		return 0, 0, 0, zipMalformed("the file is %d bytes, which is too short to be a ZIP archive", size)
	}
	tail := make([]byte, tailLen)
	if _, err := r.ReadAt(tail, size-tailLen); err != nil {
		return 0, 0, 0, zipMalformed("reading the end of the file: %s", err)
	}
	found := -1
	for i := len(tail) - endOfDirectorySize; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:]) != sigEndOfDirectory {
			continue
		}
		// The comment length must account for exactly the bytes after the
		// record, which is what tells a real record from the same four bytes
		// occurring inside an archive comment or a stored payload.
		if int(binary.LittleEndian.Uint16(tail[i+20:])) == len(tail)-i-endOfDirectorySize {
			found = i
			break
		}
	}
	if found < 0 {
		return 0, 0, 0, zipMalformed("no end-of-central-directory record, so this is not a ZIP archive")
	}
	count = uint64(binary.LittleEndian.Uint16(tail[found+8:]))
	directorySize = uint64(binary.LittleEndian.Uint32(tail[found+12:]))
	offset = uint64(binary.LittleEndian.Uint32(tail[found+16:]))
	if count != 0xffff && directorySize != 0xffffffff && offset != 0xffffffff {
		return count, offset, directorySize, nil
	}

	locatorAt := size - tailLen + int64(found) - 20
	if locatorAt < 0 {
		return 0, 0, 0, zipMalformed("the archive saturates a 32-bit field and carries no ZIP64 locator")
	}
	locator := make([]byte, 20)
	if _, err := r.ReadAt(locator, locatorAt); err != nil {
		return 0, 0, 0, zipMalformed("reading the ZIP64 locator: %s", err)
	}
	if binary.LittleEndian.Uint32(locator) != sigZip64Locator {
		return 0, 0, 0, zipMalformed("the archive saturates a 32-bit field and carries no ZIP64 locator")
	}
	zip64At := binary.LittleEndian.Uint64(locator[8:])
	if zip64At+56 > uint64(size) {
		return 0, 0, 0, zipMalformed("the ZIP64 locator points past the end of the archive")
	}
	record := make([]byte, 56)
	if _, err := r.ReadAt(record, int64(zip64At)); err != nil {
		return 0, 0, 0, zipMalformed("reading the ZIP64 end-of-central-directory record: %s", err)
	}
	if binary.LittleEndian.Uint32(record) != sigZip64EndOfDirectory {
		return 0, 0, 0, zipMalformed("the ZIP64 locator does not point at a ZIP64 end-of-central-directory record")
	}
	return binary.LittleEndian.Uint64(record[32:]),
		binary.LittleEndian.Uint64(record[48:]),
		binary.LittleEndian.Uint64(record[40:]), nil
}
