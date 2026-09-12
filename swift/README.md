# VNX for Swift

`VNX` reads and writes VNX documents and VNZ containers in Swift, and reports
diagnostics.

Swift 5.9 or later, on macOS 10.15, iOS 13, tvOS 13 or watchOS 6. Foundation and
zlib only — zlib is a system library, present in the macOS SDK and in every Linux
distribution, so there is nothing to fetch and no lock file. Implements VNX 0.1.0
— [`SPEC.md`](../SPEC.md) is the format definition.

## Install

Clone the repository and depend on the package by path. In the consuming
`Package.swift`:

```swift
dependencies: [
    .package(path: "../vnx/swift")
],
targets: [
    .executableTarget(name: "MyApp", dependencies: [
        .product(name: "VNX", package: "swift")
    ])
]
```

**The `package:` label is the name of the DIRECTORY the dependency points at,
not the package's declared name.** The declared name, the product and the module
are all spelled `VNX`, and `package: "VNX"` fails with `unknown package 'VNX' in
dependencies of target 'MyApp'; valid packages are: 'swift'`. With the dependency
at `../vnx/swift` the label is `swift`.

Set the consuming package's `platforms` floor at or above this one; a lower floor
fails to resolve.

## Usage

Read a file, walk a track to its coordinates, report every fault, write it back.

```swift
import Foundation
import VNX

let url = URL(fileURLWithPath: CommandLine.arguments[1])
let reading = try VNX.read(contentsOf: url)

for track in reading.document.tracks ?? [] {
    print("track \(track.name ?? "(unnamed)")")
    for segment in track.segments {
        for pip in segment.pips.prefix(2) {
            print("  \(pip.lon), \(pip.lat)")
        }
    }
}
for d in reading.diagnostics {
    print("\(d.code) \(d.path): \(d.message)")
}

try VNX.write(reading.document).write(to: URL(fileURLWithPath: "out.vnx"))
```

## API

| Call | Takes | Returns | Throws |
|---|---|---|---|
| `VNX.read(_:)` | `Data` | `VNX.ReadResult` | `Refusal`, one of the four document causes |
| `VNX.read(contentsOf:)` | `URL`, read as a container where the extension is `.vnz` | `VNX.ReadResult` | the same, and whatever reading the file raises |
| `VNX.readContainer(_:)` | `Data` | `ContainerResult` | `Refusal`, a document or a container cause |
| `VNX.write(_:)` | `Document` | `Data` | `Refusal`, for a number JSON cannot spell |
| `VNX.writeContainer(document:attachments:)` | `Document`, `[Attachment]` | `Data` | the same, from the document it carries |
| `VNX.version` | — | `String` | — |

| Type | Members |
|---|---|
| `VNX.ReadResult` | `document: Document`, `diagnostics: [Diagnostic]` |
| `ContainerResult` | `documentName: String`, `document`, `diagnostics`, `attachments: [Attachment]` |
| `Attachment` | `path: String`, `bytes: Data` |
| `Diagnostic` | `path: String`, `message: String`, `cause: Cause`, `code: String` |
| `Refusal` | `message: String`, `cause: Cause`; conforms to `Error` |
| `Document` | `vnx`, `profile`, `generator`, `created`, and `folders`, `networks`, `routes`, `tracks`, `waypoints`, `areas`, `rulesets` |
| `Position` | `lon`, `lat`, `elevation: Double?`, `timeMilliseconds: Double?`, `isSamePlace(as:)` |
| `Track` | `segments: [TrackSegment]`, `movingTimeMilliseconds: Double?` |

Every public type is `Sendable`. Writing what you read is legitimately not the
bytes you read, because a writer applies the emit-boundary rules of SPEC §4.1.

## Errors and diagnostics

A refusal and a diagnostic are separate channels (SPEC §6.2–§6.3), and in Swift
that is a thrown error against the `diagnostics` array returned beside a
document.

- **A refusal is a thrown `Refusal`.** `Refusal.cause` is a `Cause` and
  `cause.code` is its stable token. `Cause` is `CaseIterable`, so the whole set
  is reachable from code; `Sources/VNX/Cause.swift` documents each one.
- **A diagnostic comes back with the document.** Branch on `Diagnostic.code`.
  `message` is English prose, advisory, and never compared (SPEC §6.3). `path`
  is a JSON Pointer (RFC 6901) into the document, except `attachment-unreadable`,
  which points into the archive's entries.
- **An absent collection is `nil`, not `[]`.** `Document` holds the two apart, so
  a read-write-read is a fixed point. Inside an item — `Route.arcs`,
  `Chain.links`, `Track.segments`, an arc's `pips` — it is a plain array.
- **`Track.movingTimeMilliseconds` is `nil` for unknown.** Unknown is not zero.
- **A `Position` holds four values, not the arity the document spelled.**
  `elevation` and `timeMilliseconds` are `nil` where a channel was absent or
  `null`, and a writer emits the shortest spelling of the four.
- **This reader bounds nothing.** SPEC §9 has a reader taking documents from an
  untrusted source bound what one may consume; impose that limit before handing
  this library an upload.

## Run the tests

```sh
cd swift && swift test
```

Unit tests and the corpus runner over the accept, reject, diagnose, write and
numbers tiers. A green run is the conformance claim.

## Layout

| File | What it holds |
|---|---|
| `JSONValue.swift`, `JSONParser.swift` | a strict RFC 8259 parser and its tree |
| `Model.swift` | the decoded document (SPEC §3) |
| `Decoder.swift` | what is refused, dropped and reported (SPEC §6.2) |
| `Encoder.swift`, `Rounding.swift` | the emit boundary and its precision (SPEC §4.1) |
| `Projection.swift` | the equirectangular map and its tolerance (SPEC §3.3.3) |
| `Deflate.swift`, `Zip.swift`, `Container.swift` | zlib, ZIP with ZIP64, the container (SPEC §7) |
| `Version.swift` | `MAJOR.MINOR.PATCH`, compared as numbers of any length |
| `CodePointOrder.swift` | code-point ordering, which Swift's `<` does not give |
| `Cause.swift` | the reason codes, and the tables tying them to the corpus |

## Notes for porters

General form in [`IMPLEMENTING.md`](../IMPLEMENTING.md); these are the parts
that are shaped by Swift.

- `JSONEncoder` renders the tree, because `JSONSerialization` spells `-3.67` as
  `-3.6699999999999999`. The parser is hand-written: a reader enforces rules a
  stock parser does not, and `NaN` and `Infinity` are not JSON tokens.
- zlib arrives through a `systemLibrary` target with a module map over
  `<zlib.h>`, rather than `Compression.framework`, which has no Linux build.
- `withoutEscapingSlashes` sets the platform floor at macOS 10.15.
