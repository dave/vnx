// Command makevnz regenerates the .vnz archives in the conformance corpus.
//
// They are committed as binaries because the corpus must be runnable by an
// implementation in any language, which cannot be asked to build a Go program
// first. This generator is committed beside them so the bytes are reproducible
// and reviewable rather than mysterious. It lives under testdata so the go tool
// neither builds nor vets it as part of the package.
//
//	go run ./tools/makevnz        (from testdata/corpus)
package main

import (
	"archive/zip"
	"hash/crc32"
	"log"
	"os"
)

type entry struct {
	name   string
	body   string
	method uint16
}

// The document inside the archives is a plain accept-tier document,
// like every other accept file in the corpus.
const doc = `{
  "vnx": "0.8.0",
  "waypoints": [
    {
      "name": "In a container",
      "at": [-3.675, 57.1201]
    }
  ]
}
`

func main() {
	write("accept/container-root-doc.vnz", []entry{
		{name: "doc.vnx", body: doc, method: zip.Deflate},
		{name: "attachments/photo.txt", body: "not really a photo\n", method: zip.Deflate},
	})
	write("accept/container-named-document.vnz", []entry{
		{name: "some-export.vnx", body: doc, method: zip.Deflate},
	})
	write("reject/container-escapes.vnz", []entry{
		{name: "doc.vnx", body: doc, method: zip.Deflate},
		{name: "../../etc/passwd", body: "escaped\n", method: zip.Deflate},
	})
	write("reject/container-duplicate-entries.vnz", []entry{
		{name: "doc.vnx", body: doc, method: zip.Deflate},
		{name: "doc.vnx", body: `{"vnx":"0.8.0"}`, method: zip.Deflate},
	})
	// The two names differ only in case. §7 compares entry names
	// case-insensitively, so this is a duplicate exactly as the identical pair
	// above is, and a duplicate check keyed on the exact name accepts it. The
	// attachment rather than the root document carries the collision, so the
	// root-document rule's own case-insensitive match cannot refuse it first.
	write("reject/container-duplicate-entries-case.vnz", []entry{
		{name: "doc.vnx", body: doc, method: zip.Deflate},
		{name: "photos/summit.jpg", body: "not really a photo\n", method: zip.Deflate},
		{name: "photos/Summit.jpg", body: "not really a photo either\n", method: zip.Deflate},
	})
	// The root document is deliberately NOT the first entry. At offset 0 a
	// reader that loses the local-header offset altogether and defaults to
	// zero still finds it, so the masked offset would be untested on the one
	// entry that matters. A lost value that happens to be correct is not a
	// test. This archive is input rather than writer output, so §7.1's
	// root-document-first rule does not apply to it, and §7 selects the root
	// by name at any position.
	writeUnusableMethod("diagnose/container-unusable-method.vnz")
	writeZip64("accept/container-zip64.vnz", []entry{
		{name: "attachments/first.txt", body: "written before the document, so its offset is not zero\n", method: zip.Store},
		{name: "doc.vnx", body: doc, method: zip.Store},
		{name: "attachments/photo.txt", body: "not really a photo\n", method: zip.Store},
	})
}

func write(path string, entries []entry) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{
			Name:   e.name,
			Method: e.method,
			Flags:  0x800, // UTF-8 filename flag, per spec §7
		})
		if err != nil {
			log.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			log.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", path)
}

// writeZip64 builds an archive by hand that uses the ZIP64 extensions of
// [APPNOTE], which spec §7 requires every reader to accept.
//
// archive/zip cannot produce this file. It emits ZIP64 only when a value will
// not fit in 32 bits, so a fixture built with it would need a four-gigabyte
// entry — and a corpus that cannot be cloned is not a corpus. The structures
// are written out here instead.
//
// THE MASKING IS THE POINT, AND WHERE IT IS APPLIED IS THE POINT. Python's
// `force_zip64` masks the LOCAL header only, leaving the central directory
// holding real 32-bit values. Almost every reader takes sizes and offsets from
// the central directory, so such a fixture never asks the extra-field parser
// anything: an implementation can omit that parser entirely and stay green. So
// this archive does the reverse. The local headers carry true values and the
// CENTRAL DIRECTORY masks all three of compressed size, uncompressed size and
// local-header offset to 0xFFFFFFFF, moving them into a ZIP64 extra field.
//
// The end-of-central-directory record is masked too, so the central directory
// can only be found through the ZIP64 locator. A reader that skips either
// mechanism reads nothing.
//
// Entries are STORED. Deflate has no canonical output — for one of these
// documents Go's compress/flate emits 116 bytes and zlib 108, and no zlib level
// reproduces the other — so a hand-built archive that deflated would not be
// reproducible from this generator in any language but Go.
func writeZip64(path string, entries []entry) {
	var out []byte
	le16 := func(v uint16) { out = append(out, byte(v), byte(v>>8)) }
	le32 := func(v uint32) { out = append(out, byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }
	le64 := func(v uint64) {
		for i := 0; i < 8; i++ {
			out = append(out, byte(v>>(8*i)))
		}
	}

	type placed struct {
		e      entry
		crc    uint32
		size   uint64
		offset uint64
	}
	placedEntries := make([]placed, 0, len(entries))

	for _, e := range entries {
		body := []byte(e.body)
		p := placed{e: e, crc: crc32.ChecksumIEEE(body), size: uint64(len(body)), offset: uint64(len(out))}
		out = append(out, 'P', 'K', 3, 4)
		le16(45)    // version needed: 4.5, the ZIP64 minimum
		le16(0x800) // UTF-8 filename. No data descriptor: the sizes are known here.
		le16(0)     // stored
		le16(0)     // modification time  ) fixed at 1980-01-01 00:00:00, per §7.
		le16(0x21)  // modification date  ) MS-DOS date 0x0021 is 1980-01-01.
		le32(p.crc)
		le32(uint32(p.size)) // true sizes in the LOCAL header, deliberately
		le32(uint32(p.size))
		le16(uint16(len(e.name)))
		le16(0) // no local extra field
		out = append(out, e.name...)
		out = append(out, body...)
		placedEntries = append(placedEntries, p)
	}

	cdOffset := uint64(len(out))
	for _, p := range placedEntries {
		out = append(out, 'P', 'K', 1, 2)
		le16(45 | (3 << 8)) // version made by: Unix, 4.5
		le16(45)            // version needed
		le16(0x800)
		le16(0) // stored
		le16(0)
		le16(0x21)
		le32(p.crc)
		le32(0xFFFFFFFF) // compressed size, in the ZIP64 extra field below
		le32(0xFFFFFFFF) // uncompressed size, likewise
		le16(uint16(len(p.e.name)))
		le16(28) // extra length: 4 header + 24 payload
		le16(0)  // comment length
		le16(0)  // disk number start
		le16(0)  // internal attributes
		le32(0)  // external attributes
		le32(0xFFFFFFFF)
		out = append(out, p.e.name...)
		le16(1)  // ZIP64 extended information extra field
		le16(24) // three 8-byte values follow
		le64(p.size)
		le64(p.size)
		le64(p.offset)
	}
	cdSize := uint64(len(out)) - cdOffset

	eocd64 := uint64(len(out))
	out = append(out, 'P', 'K', 6, 6)
	le64(44) // size of the remainder of this record
	le16(45 | (3 << 8))
	le16(45)
	le32(0) // this disk
	le32(0) // disk with the central directory
	le64(uint64(len(placedEntries)))
	le64(uint64(len(placedEntries)))
	le64(cdSize)
	le64(cdOffset)

	out = append(out, 'P', 'K', 6, 7)
	le32(0)
	le64(eocd64)
	le32(1)

	out = append(out, 'P', 'K', 5, 6)
	le16(0)
	le16(0)
	le16(0xFFFF)     // entry counts masked, so the ZIP64 record is required
	le16(0xFFFF)     //
	le32(0xFFFFFFFF) // central directory size
	le32(0xFFFFFFFF) // central directory offset
	le16(0)          // no archive comment

	if err := os.WriteFile(path, out, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (zip64)", path)
}

// writeUnusableMethod builds an archive one of whose attachments uses a
// compression method that is neither stored nor deflate.
//
// §7's remedy is to DROP that attachment, record a diagnostic and return the
// document: refusing a whole library because one photograph used an unusual
// method would be disproportionate. Nothing in the corpus reached that remedy.
//
// archive/zip cannot write this: CreateHeader refuses a method it has no
// registered compressor for, which is every method but these two. The entry is
// written out by hand instead, and its body is never valid data for the method
// it claims — deliberately, because a reader must refuse it on the METHOD before
// it ever tries to decompress. A reader that inflated first would fail on the
// body and could report the wrong cause.
//
// ⚠ THE DROPPED ENTRY'S NAME CARRIES BOTH CHARACTERS RFC 6901 ESCAPES, and that
// is load-bearing. §6.3 puts a container diagnostic in the `#/entries/` pointer
// space, where the member name is ONE reference token: a `/` inside it MUST be
// written `~1` and a `~` MUST be written `~0`, in that order. `photos/a~b.jpg`
// therefore exercises both, and the ORDER too — escaping `/` first and `~`
// second turns `~1` into `~01`, so a reader that reverses the two produces a
// different path from one that does not.
//
// An attachment name is the one place in this format where a member name
// routinely contains a slash, and every attachment name without these two
// characters passes whether a reader escapes at all.
func writeUnusableMethod(path string) {
	const unusableMethod = 12 // bzip2: legal in APPNOTE, not available everywhere
	type raw struct {
		name   string
		body   string
		method uint16
	}
	entries := []raw{
		{"doc.vnx", doc, 0},
		{"photos/a~b.jpg", "not bzip2 data, and never decompressed\n", unusableMethod},
		{"notes/readable.txt", "kept, so the drop is provably scoped\n", 0},
	}

	var out []byte
	le16 := func(v uint16) { out = append(out, byte(v), byte(v>>8)) }
	le32 := func(v uint32) { out = append(out, byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }

	type placed struct {
		raw
		crc    uint32
		size   uint32
		offset uint32
	}
	var placedEntries []placed
	for _, e := range entries {
		p := placed{raw: e, crc: crc32.ChecksumIEEE([]byte(e.body)),
			size: uint32(len(e.body)), offset: uint32(len(out))}
		out = append(out, 'P', 'K', 3, 4)
		le16(20)
		le16(0x800)
		le16(e.method)
		le16(0)    // time  ) 1980-01-01 00:00:00, per §7.1
		le16(0x21) // date  )
		le32(p.crc)
		le32(p.size)
		le32(p.size)
		le16(uint16(len(e.name)))
		le16(0)
		out = append(out, e.name...)
		out = append(out, e.body...)
		placedEntries = append(placedEntries, p)
	}

	cdOffset := uint32(len(out))
	for _, p := range placedEntries {
		out = append(out, 'P', 'K', 1, 2)
		le16(20 | (3 << 8))
		le16(20)
		le16(0x800)
		le16(p.method)
		le16(0)
		le16(0x21)
		le32(p.crc)
		le32(p.size)
		le32(p.size)
		le16(uint16(len(p.name)))
		le16(0)
		le16(0)
		le16(0)
		le16(0)
		le32(0)
		le32(p.offset)
		out = append(out, p.name...)
	}
	cdSize := uint32(len(out)) - cdOffset

	out = append(out, 'P', 'K', 5, 6)
	le16(0)
	le16(0)
	le16(uint16(len(placedEntries)))
	le16(uint16(len(placedEntries)))
	le32(cdSize)
	le32(cdOffset)
	le16(0)

	if err := os.WriteFile(path, out, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (method %d attachment)", path, unusableMethod)
}
