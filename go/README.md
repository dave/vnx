# vnx for Go

`vnx` reads and writes VNX documents and VNZ containers in Go, and it is the
reference implementation of the format. Where [`SPEC.md`](../SPEC.md), the
[corpus](../corpus/) and an implementation disagree, this is the implementation
that gets corrected first.

Go 1.25 or later. No dependencies: the library, the command-line tool and the
three conversion writers import the standard library and nothing else.

## Install

This module implements VNX 0.11.0. From a checkout of the repository, in the
consuming module's `go.mod`:

```
require github.com/dave/vnx/go v0.0.0

replace github.com/dave/vnx/go => /path/to/vnx/go
```

Where the repository is reachable, `go get github.com/dave/vnx/go` fetches it
directly. The module sits in a subdirectory of the repository, so its version
tags carry that directory as a prefix: `go/v0.1.0`, not `v0.1.0`.

The import path is `github.com/dave/vnx/go/pkg/vnx`.

## Usage

Read a document, report what reading it turned up, and write it back.

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/dave/vnx/go/pkg/vnx"
)

func main() {
	data, err := os.ReadFile("trip.vnx")
	if err != nil {
		log.Fatal(err)
	}
	doc, diags, err := vnx.Decode(data)
	if err != nil {
		log.Fatal(err) // the document was refused; there is no document
	}
	for _, d := range diags {
		fmt.Fprintln(os.Stderr, d)
	}
	fmt.Println(len(doc.Networks), "network(s)")

	out, err := vnx.Encode(doc)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("trip.vnx", out, 0o644); err != nil {
		log.Fatal(err)
	}
}
```

## Command-line tool

`vnx` is the tool under [`cmd/vnx`](cmd/vnx). Run it from a checkout with
`go run ./cmd/vnx <command>`, or build a binary with
`go build -o vnx ./cmd/vnx`.

| Command | Flags | What it does |
|---|---|---|
| `vnx validate <file>` | `--json` | Reads the file and prints its diagnostics. |
| `vnx outline <file>` | `--json` | Prints the document's structure: names, folder count, and per-network node, arc, highlight, point and area counts. No coordinates. |
| `vnx convert <file>` | `--to gpx\|kml\|geojson`, `--out <file>` | Writes the document as GPX, KML or GeoJSON. |

Every command takes one file, and reads either a `.vnx` document or a `.vnz`
container, chosen by extension.

| Exit code | Meaning |
|---|---|
| 0 | The file was read. Diagnostics may have been printed. |
| 1 | The file was refused, or the chosen format could carry nothing the document holds. Nothing was written. |
| 2 | The command line was wrong: no command, an unknown command, or an undefined flag. |

`validate` and `outline` take `--json`, which prints a report carrying the
document path of everything it mentions. That is what makes them usable from a
script or an agent without parsing English prose.

```sh
vnx validate --json corpus/diagnose/walk-not-connected.vnx
```

```json
{
  "file": "corpus/diagnose/walk-not-connected.vnx",
  "ok": true,
  "diagnostics": [
    {
      "code": "walk-not-connected",
      "path": "/networks/0/highlights/1/walk/1",
      "message": "the walk is at node \"b\", which is not an end of arc \"cd\" — dropped"
    }
  ]
}
```

A diagnostic in that report carries `code`, `path` and `message`, and nothing
else. `code` is the stable token to match on; it is empty for the decode
diagnostics that have no assigned code. There is no `severity` field, because
[`SPEC §6.3`](../SPEC.md#63-diagnostics) says a diagnostic carries none.

`outline` exists because a day's recorded track holds tens of thousands of
positions. It reports a library's shape at a size a caller can hold, so that
renaming a plan or counting the networks in a file does not mean reading the
geometry.

`convert` writes to stdout unless `--out` names a file, so
`vnx convert --to gpx trip.vnx > trip.gpx` works. Its diagnostics — the items
the chosen format cannot carry — go to stderr, one per line, as
`severity code path: message`. With `--out`, the destination file appears only
once the whole document has been written.

GPX, KML and GeoJSON are conversion targets of this tool rather than part of the
format; see the root README's [Not in scope](../README.md#not-in-scope).

## API

Everything below is in `package vnx`.

| Call | Arguments | Returns |
|---|---|---|
| `Decode` | `data []byte` | `*Document`, `[]Diagnostic`, `error` |
| `DecodeReader` | `r io.Reader` | `*Document`, `[]Diagnostic`, `error` |
| `Encode` | `doc *Document` | `[]byte`, `error` |
| `EncodeTo` | `w io.Writer`, `doc *Document` | `error` |
| `DecodeContainer` | `r io.ReaderAt`, `size int64` | `*Container`, `[]Diagnostic`, `error` |
| `EncodeContainerTo` | `w io.Writer`, `doc *Document` | `error` |
| `WriteContainer` | `w io.Writer`, `name string`, `produce func(io.Writer) error` | `error` |
| `WriteContainerWithAttachments` | the above, plus `attachments []Attachment` | `error` |
| `DocumentIsEmpty` | `d *Document` | `bool` |
| `WriterCodes` | — | `[]string`, every `Code` the conversion writers emit |
| `IsAppKey` | `key string` | `bool`, whether a top-level key is an application key (`SPEC §3.9`) |
| `(*Document).GetAppKey` / `SetAppKey` / `DeleteAppKey` | `key string` (and `value any` to set) | read, store or remove one application key; `SetAppKey` keeps the list sorted and refuses what `EncodeTo` refuses |
| `AppKeyDiagnostics` | `d *Document`, `format string` | `[]Diagnostic`, one per application key a conversion writer drops |

`Document` holds the header fields, the item collections and `AppKeys`, the
application keys as a list of `AppKey{Key, Value}` sorted by key. `Value` is
the parsed JSON value, `nil` for a JSON `null`, and nothing inspects it.
`EncodeTo` refuses a key without a full stop, a key held twice, and a nil
slice or map as a value, before it writes anything. `Container` holds a
`*Document` and the files stored beside it, each a `ContainerAttachment` with a
`Path` and its `Bytes` — the same shape the JavaScript, Rust and Swift
implementations return. `EncodeTo` streams, so a document is never held in
memory twice. `DecodeReader` streams too: it reads token by token and keeps
only the `Document`, so a file is never held whole or as a JSON tree. `Decode`
is `DecodeReader` over its bytes, and `DecodeContainer` streams the root
document out of the archive.

`ContainerAttachment` (read) and `Attachment` (write) are separate types on
purpose. The reader has the whole file; the writer takes a `Name` and an
`io.Reader` and never holds one whole.

Each conversion writer — `pkg/vnx/gpx`, `pkg/vnx/kml`, `pkg/vnx/geojson` —
exports `Write(w io.Writer, d *vnx.Document) (vnx.WriteReport, error)`.
`WriteReport` carries the diagnostics raised while writing and `Emitted`, the
number of items the format kept.

A GeoJSON or KML feature needs a name, and a highlight's `Name` is optional
([`SPEC §3.3.3`](../SPEC.md#333-highlights)). Every item inside a network is
labelled by its own name, and one with no name by an integer unique within its
network (`vnx.LabelNetwork`): GeoJSON writes `"{network} / {label}"`, KML the
bare label inside the network's Folder.

## Errors and diagnostics

Refusal and diagnostics are separate channels ([`SPEC §6.2`](../SPEC.md#62-error-handling),
[`§6.3`](../SPEC.md#63-diagnostics)). An error from `Decode` means the document
was refused and none is returned; diagnostics mean it was read, with something
reported along the way. An error from the reader given to `DecodeReader` is returned
as it is, so a failed read or a cancelled context is not `ErrMalformed`.

Refusals are Go errors, matched with `errors.Is` against four sentinels:
`ErrMalformed` for bytes that are not JSON, `ErrNotVNX` for JSON carrying no
usable `vnx` key, `ErrUnsupportedVersion` for a version this reader does not
implement, and `ErrContainer` for an archive that yields no document at all.

A version refusal also matches one of three narrower sentinels, which say what
the holder of the file needs: `ErrNewerVersion` for a version above this
reader's, `ErrOlderVersion` for an earlier series
([`SPEC §2.2`](../SPEC.md#22-version)), and `ErrInvalidVersion` for a `vnx`
string that is not a version.

`ErrContainer` means what it says: an archive is refused only when no document
comes out of it. A fault in an ATTACHMENT is never one of those — whether its
compression method is one this reader does not have, or its bytes will not come
back from a method it does have, the attachment is dropped, an
`attachment-unreadable` diagnostic records it, and the document is returned
([`SPEC §7`](../SPEC.md#7-the-vnz-container)). Refusing a whole trail library
over one damaged photograph would be disproportionate. The root document is the
exception, because there is nothing left to return.

A `Diagnostic` carries `Path` — a JSON-pointer location such as
`/networks/0/arcs/arc-1` — a short English `Message`, and `Code`, the stable
token a caller matches on without reading the prose. `Code` is set on every
conversion-writer diagnostic (with its `Args`) and on the decode diagnostics the
conformance corpus pins; it is empty on the rest, which is a ruling rather than
a gap.

The Go type also carries a `Severity` (`Info` or `Warning`). That field is this
library's own and is no part of the format: `SPEC §6.3` says a diagnostic
carries no severity, so `--json` does not emit one and no conformance case
compares one.

## Run the tests

```sh
cd go && go test -count=1 ./...
```

This runs the unit tests, the corpus harness over [`corpus/`](../corpus/), and
the guards that keep the import set to the standard library. `-count=1` runs
them again each time: Go's test cache checks only the files inside the module,
so it does not see a change to `corpus/`.

## Layout

| Package | What it is |
|---|---|
| `pkg/vnx` | The document model, the reader, the writer and the VNZ container. |
| `pkg/vnx/gpx`, `pkg/vnx/kml`, `pkg/vnx/geojson` | One conversion writer each. |
| `pkg/detmath` | Sin, Cos, Atan, Atan2 and Hypot with the same bits on every architecture (from Go's `math`, BSD licence in its `LICENSE`). |
| `pkg/merge` | The network merge's geometry core: joins GPS tracks into a shared network of nodes and arcs. Imports only the standard library and `pkg/detmath`. API not stable yet. |
| `cmd/vnx` | The command-line tool. |

## Notes for porters

The ZIP mechanics in `pkg/vnx` are written by hand rather than taken from
`archive/zip`, because `SPEC §7` asks a reader to accept an entry whose declared
size exceeds the bytes it holds where the CRC agrees, and `archive/zip` refuses
it. See [`IMPLEMENTING.md §9.1`](../IMPLEMENTING.md).

A `.vnz` attachment comes back as a HANDLE — `Path`, plus `Bytes()` and
`Open()` — where the JavaScript, Rust and Swift implementations return the path
alongside the content. That is a deliberate difference and not drift. Go takes
an `io.ReaderAt`, which most of the others do not have, so it can offer the
content without deciding for the caller that they want it; materialising every
attachment made `DecodeContainer` retain 50 MB on a 52 MB archive and took peak
heap from 50 MB to 158 MB, all of it discarded by the one caller we have, which
wants the names. Nothing here crosses the wire, and the conformance corpus
cannot tell the two shapes apart because it asks for the content and gets it.
`DecodeContainer` still reads every attachment to its end and keeps none, so a
damaged one is found and diagnosed at decode time as `SPEC §7` requires.

`cmd/vnx` imports `pkg/vnx` and the three writer packages and nothing further, a
set a test pins one package at a time. That is what lets the tool cross-compile
and run anywhere the reference model is wanted without a database behind it.
