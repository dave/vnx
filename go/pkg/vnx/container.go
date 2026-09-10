package vnx

import (
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
// ⚠ THE ZIP MECHANICS ARE zipread.go, NOT archive/zip, and the reason is §7's
// size rule: a reader MUST NOT refuse an entry that declares more bytes than it
// holds where the CRC over the bytes present agrees. archive/zip reads the
// declared count and checksums what it read, so it consumes past the payload and
// refuses a conforming archive with "zip: checksum error". Two more of §7's
// answers it takes on its own terms: an unusable compression method is a
// per-entry fact here and a dead stream there, and a duplicate name must be
// visible twice to be refused at all.
//
// ⚠ COPY KMZ'S INTENT, NOT ITS CODE. importer/kmz.go:27-36 says "prefer
// doc.kml in the root" and then tests filepath.Base, which matches at any depth
// and lets the LAST match win — so an archive holding a/doc.kml and b/doc.kml
// silently picks b. Here "at the root" means what it says.
func DecodeContainer(r io.ReaderAt, size int64) (*Container, []Diagnostic, error) {
	entries, err := readZipDirectory(r, size)
	if err != nil {
		return nil, nil, err
	}

	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if err := checkEntryPath(e.name); err != nil {
			return nil, nil, err
		}
		key := strings.ToLower(e.name)
		if seen[key] {
			// Go and Python resolve duplicates differently, so one archive
			// would yield different documents in different languages. Refuse
			// rather than arbitrate.
			return nil, nil, fmt.Errorf("%w: duplicate entry %q", ErrContainer, e.name)
		}
		seen[key] = true
	}

	root := pickRootDocument(entries)
	if root == nil {
		return nil, nil, fmt.Errorf("%w: no .vnx document in the archive", ErrContainer)
	}
	// §7 splits an unusable compression method by WHICH entry carries it: an
	// attachment is dropped below, and for the root document there is no document
	// to return, so the archive is refused. That refusal comes from
	// [readZipEntry], which names the entry and the method. A guard here as well
	// would only restate it, and red-proofed it changed nothing but the wording.
	data, err := readZipEntry(r, *root)
	if err != nil {
		return nil, nil, err
	}

	doc, diags, err := Decode(data)
	if err != nil {
		return nil, nil, err
	}

	attachments := make([]string, 0, len(entries))
	for i := range entries {
		e := entries[i]
		if &entries[i] == root || e.isDirectory() {
			continue
		}
		// An attachment that cannot be decompressed is DROPPED with a
		// diagnostic, and the document is still returned: refusing a whole
		// library because one photograph used an unusual method would be
		// disproportionate.
		//
		// ⚠ THE PATH IS IN THE SEPARATE `#/entries/` SPACE OF §6.3, because an
		// archive entry is not part of the document and no pointer into the
		// document can locate one. The escaping ORDER matters and the corpus case
		// exercises it: `~` becomes `~0` before `/` becomes `~1`. Reversed, the
		// second pass escapes the `~1` the first pass just wrote, so
		// `photos/a~b.jpg` comes out as `photos~01a~0b.jpg`.
		if !e.usableMethod() {
			diags = append(diags, Diagnostic{
				Severity: Warning,
				Code:     CodeAttachmentUnreadable,
				Path:     "#/entries/" + escapePointerToken(e.name),
				Message: fmt.Sprintf("attachment %q uses compression method %d, which is neither stored nor deflate, so it was dropped",
					e.name, e.method),
			})
			continue
		}
		attachments = append(attachments, e.name)
	}
	return &Container{Document: doc, Attachments: attachments}, diags, nil
}

// escapePointerToken escapes one JSON Pointer reference token ([RFC 6901]): `~`
// becomes `~0` and `/` becomes `~1`, in that order. An attachment name is the
// one place in this format where a member name routinely contains a slash.
func escapePointerToken(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func pickRootDocument(entries []zipEntryHeader) *zipEntryHeader {
	var firstVNX *zipEntryHeader
	for i := range entries {
		f := &entries[i]
		name := strings.ToLower(f.name)
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
