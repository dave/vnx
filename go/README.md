# vnx for Go

`vnx` reads and writes VNX documents and VNZ containers in Go, and it is the
reference implementation of the format. Where [`SPEC.md`](../SPEC.md), the
[corpus](../corpus/) and an implementation disagree, this is the implementation
that gets corrected first, and the number-conformance table every
implementation is measured against,
[`corpus/numbers/numbers.tsv`](../corpus/numbers/), is generated in Go.

Go 1.25 or later. No dependencies: the library, the command-line tool and the
three conversion writers import the standard library and nothing else.

## Install

This module implements VNX 0.1.0. From a checkout of the repository, in the
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
| `vnx outline <file>` | `--json` | Prints the document's structure: names, folder count, and per-network node, arc, chain, point and area counts. No coordinates. |
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
vnx validate --json corpus/diagnose/chain-does-not-join.vnx
```

```json
{
  "file": "corpus/diagnose/chain-does-not-join.vnx",
  "ok": true,
  "diagnostics": [
    {
      "severity": "warning",
      "path": "/networks/0/chains/0/links/1",
      "message": "this link starts at c but the previous one ends at b — the chain is not one connected walk"
    }
  ]
}
```

`outline` exists because a day's recorded track holds tens of thousands of
positions. It reports a library's shape at a size a caller can hold, so that
renaming a route or counting the networks in a file does not mean reading the
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
| `Encode` | `doc *Document` | `[]byte`, `error` |
| `EncodeTo` | `w io.Writer`, `doc *Document` | `error` |
| `DecodeContainer` | `r io.ReaderAt`, `size int64` | `*Container`, `[]Diagnostic`, `error` |
| `EncodeContainerTo` | `w io.Writer`, `doc *Document` | `error` |
| `WriteContainer` | `w io.Writer`, `name string`, `produce func(io.Writer) error` | `error` |
| `WriteContainerWithAttachments` | the above, plus `attachments []Attachment` | `error` |
| `DocumentIsEmpty` | `d *Document` | `bool` |
| `WriterCodes` | — | `[]string`, every `Code` the conversion writers emit |

`Document` holds the header fields and the item collections. `Container` holds a
`*Document` and the names of the files stored beside it. `EncodeTo` streams, so
a document is never held in memory twice.

Each conversion writer — `pkg/vnx/gpx`, `pkg/vnx/kml`, `pkg/vnx/geojson` —
exports one `Write(w io.Writer, d *vnx.Document) (vnx.WriteReport, error)`.
`WriteReport` carries the diagnostics raised while writing and `Emitted`, the
number of items the format kept.

## Errors and diagnostics

Refusal and diagnostics are separate channels ([`SPEC §6.2`](../SPEC.md#62-error-handling),
[`§6.3`](../SPEC.md#63-diagnostics)). An error from `Decode` means the document
was refused and none is returned; diagnostics mean it was read, with something
reported along the way.

Refusals are Go errors, matched with `errors.Is` against four sentinels:
`ErrMalformed` for bytes that are not JSON, `ErrNotVNX` for JSON carrying no
usable `vnx` key, `ErrUnsupportedVersion` for a version this reader does not
implement, and `ErrContainer` for an archive that yields no document at all.

A `Diagnostic` carries `Severity` (`Info` or `Warning`), `Path` — a
JSON-pointer location such as `/networks/0/arcs/arc-1` — and a short English
`Message`. Conversion-writer diagnostics also carry a stable `Code` and its
`Args`, so a caller can match one without reading its prose.

## Run the tests

```sh
cd go && go test ./...
```

This runs the unit tests, the corpus harness over [`corpus/`](../corpus/), and
the guards that keep the import set to the standard library.

## Layout

| Package | What it is |
|---|---|
| `pkg/vnx` | The document model, the reader, the writer and the VNZ container. |
| `pkg/vnx/gpx`, `pkg/vnx/kml`, `pkg/vnx/geojson` | One conversion writer each. |
| `cmd/vnx` | The command-line tool. |

## Notes for porters

The ZIP mechanics in `pkg/vnx` are written by hand rather than taken from
`archive/zip`, because `SPEC §7` asks a reader to accept an entry whose declared
size exceeds the bytes it holds where the CRC agrees, and `archive/zip` refuses
it. See [`IMPLEMENTING.md §9.1`](../IMPLEMENTING.md).

`cmd/vnx` imports `pkg/vnx` and the three writer packages and nothing further, a
set a test pins one package at a time. That is what lets the tool cross-compile
and run anywhere the reference model is wanted without a database behind it.
