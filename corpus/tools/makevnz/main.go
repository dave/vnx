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
