package vnx

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// RootDocumentName is the conventional name of the document inside a .vnz
// archive, mirroring KMZ's doc.kml.
const RootDocumentName = "doc.vnx"

// ErrContainer means the archive itself is unusable — no document, duplicate
// entries, an unsafe path. These are refusals, not diagnostics: nothing about
// the library can be read at all.
var ErrContainer = errors.New("vnx: unusable container")

// Container is a decoded .vnz archive: the document, and the names of the
// attachments stored beside it.
type Container struct {
	Document    *Document
	Attachments []string
}

// DecodeContainer reads a .vnz archive.
//
// It returns the same three things Decode does, for the same reason: spec §6.3
// requires a reader to hand back diagnostics ALONGSIDE the document, and a
// container is still a reader. An earlier version of this function returned the
// document and attachment names only, and dropped the diagnostics on the floor
// behind a comment naming a DecodeContainerWithDiagnostics that was never
// written — so `vnx validate` on a .vnz reported a clean file no matter what
// the document inside it said.
//
// Root-document lookup follows KMZ, fallback included: prefer doc.vnx AT THE
// ARCHIVE ROOT, otherwise the first .vnx entry in archive order, compared
// case-insensitively. The fallback is deliberate — a third-party writer not
// using the conventional name is an EXPECTED absence, which is where the house
// rule puts a fallback rather than an error.
//
// ⚠ COPY KMZ'S INTENT, NOT ITS CODE. importer/kmz.go:27-36 says "prefer
// doc.kml in the root" and then tests filepath.Base, which matches at any depth
// and lets the LAST match win — so an archive holding a/doc.kml and b/doc.kml
// silently picks b. Here "at the root" means what it says.
func DecodeContainer(r io.ReaderAt, size int64) (*Container, []Diagnostic, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrContainer, err)
	}

	seen := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		name := f.Name
		if err := checkEntryPath(name); err != nil {
			return nil, nil, err
		}
		key := strings.ToLower(name)
		if seen[key] {
			// Go and Python resolve duplicates differently, so one archive
			// would yield different documents in different languages. Refuse
			// rather than arbitrate.
			return nil, nil, fmt.Errorf("%w: duplicate entry %q", ErrContainer, name)
		}
		seen[key] = true
	}

	root := pickRootDocument(zr.File)
	if root == nil {
		return nil, nil, fmt.Errorf("%w: no .vnx document in the archive", ErrContainer)
	}

	rc, err := root.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: opening %q: %s", ErrContainer, root.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: reading %q: %s", ErrContainer, root.Name, err)
	}

	doc, diags, err := Decode(data)
	if err != nil {
		return nil, nil, err
	}

	attachments := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		if f != root && !f.FileInfo().IsDir() {
			attachments = append(attachments, f.Name)
		}
	}
	return &Container{Document: doc, Attachments: attachments}, diags, nil
}

func pickRootDocument(files []*zip.File) *zip.File {
	var firstVNX *zip.File
	for _, f := range files {
		name := strings.ToLower(f.Name)
		if !strings.HasSuffix(name, ".vnx") {
			continue
		}
		// "At the root" means no directory component at all — not
		// filepath.Base matching anywhere in the tree.
		if name == RootDocumentName {
			return f
		}
		if firstVNX == nil {
			firstVNX = f
		}
	}
	return firstVNX
}

// checkEntryPath rejects anything that could escape an extraction directory.
//
// kmz.go is safe from zip-slip only because it never extracts to disk; a VNZ
// with attachments will, so the check belongs here at read time rather than
// being left to whoever extracts later and may forget.
func checkEntryPath(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty entry path", ErrContainer)
	}
	if strings.ContainsRune(name, '\\') {
		return fmt.Errorf("%w: entry path %q uses backslashes; paths are forward-slash separated", ErrContainer, name)
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("%w: entry path %q is absolute", ErrContainer, name)
	}
	if len(name) >= 2 && name[1] == ':' {
		return fmt.Errorf("%w: entry path %q carries a drive letter", ErrContainer, name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return fmt.Errorf("%w: entry path %q escapes the archive", ErrContainer, name)
		}
	}
	if cleaned := path.Clean(name); strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return fmt.Errorf("%w: entry path %q escapes the archive", ErrContainer, name)
	}
	return nil
}
