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

// The document inside the archives is stored in canonical form (spec §4.5),
// like every other accept file in the corpus.
const doc = `{
  "profile": "full",
  "vnx": "0.1.0",
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
		{name: "doc.vnx", body: `{"vnx":"0.1.0"}`, method: zip.Deflate},
	})
	writeZip64("accept/container-zip64.vnz", []entry{
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
