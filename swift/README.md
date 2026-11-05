# VNX for Swift

`VNX` reads and writes VNX documents and VNZ containers in Swift, and reports
diagnostics.

Swift 5.9 or later, on macOS 10.15, iOS 13, tvOS 13 or watchOS 6. Foundation and
zlib only — zlib is a system library, present in the macOS SDK and in every Linux
distribution, so nothing is fetched on macOS; on Debian and Ubuntu install the development package (`zlib1g-dev`) and no lock file. Implements VNX 0.10.0
— [`SPEC.md`](../SPEC.md) is the format definition.

## Install

Depend on the repository. In the consuming `Package.swift`:

```swift
dependencies: [
    .package(url: "https://github.com/dave/vnx.git", from: "0.1.0")
],
targets: [
    .executableTarget(name: "MyApp", dependencies: [
        .product(name: "VNX", package: "vnx")
    ])
]
```

The repository is private: that line resolves for someone with access to it, and
for everyone once it is public. Working from a clone instead, point at the
repository root, with `.package(path: "../vnx")`. The rest is identical, label
included.

**The `package:` label is `vnx`, the repository, not the package's declared
name and not the directory the sources sit in.** The declared name, the product
and the module are all spelled `VNX`, and the sources are under `swift/`, but
SwiftPM identifies a package by the last component of its URL or path. `package:
"swift"` fails with `unknown package 'swift' in dependencies of target 'MyApp';
valid packages are: 'vnx'`. The match ignores case, so `"VNX"` also resolves;
`"vnx"` is the spelling the toolchain prints and the one to write.

The manifest is at the repository root and its targets point into `swift/`,
which is what makes the URL form work. `swift/` holds sources and tests only.

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

## Command-line tool

This library has none. The reference command-line tool is Go's; see
[`go/README.md`](../go/README.md#command-line-tool).

## API

| Call | Takes | Returns | Throws |
|---|---|---|---|
| `VNX.read(_:)` | `Data` | `VNX.ReadResult` | `Refusal`, one of the four document causes |
| `VNX.read(contentsOf:)` | `URL`, read as a container where the extension is `.vnz` | `VNX.ReadResult` | the same, and whatever reading the file raises |
| `VNX.readContainer(_:)` | `Data` | `ContainerResult` | `Refusal`, a document or a container cause |
| `VNX.write(_:)` | `Document` | `Data` | `WriteError`, for a document its own reader would repair or drop; `ApplicationKeyError`; `NonFiniteNumberError`, for a number JSON cannot spell |
| `VNX.writeContainer(document:attachments:)` | `Document`, `[Attachment]` | `Data` | the same, from the document it carries |
| `VNX.version` | — | `String` | — |

| Type | Members |
|---|---|
| `VNX.ReadResult` | `document: Document`, `diagnostics: [Diagnostic]` |
| `ContainerResult` | `documentName: String`, `document`, `diagnostics`, `attachments: [Attachment]` |
| `Attachment` | `path: String`, `bytes: Data` |
| `Diagnostic` | `path: String`, `message: String`, `cause: Cause`, `code: String` |
| `Refusal` | `message: String`, `cause: Cause`; conforms to `Error` |
| `Document` | `vnx`, `generator`, `created`, and `folders`, `networks`, `plans`, `tracks`, `waypoints`, `areas`, `applicationKeys: [ApplicationKey]` |
| `ApplicationKey` | `key: String`, `value: JSONValue`; one top-level key containing a full stop (SPEC §3.9), `.null` for a JSON `null` |
| `ApplicationKeyError` | `key: String`, `message: String`; thrown by `VNX.write` for a key without a full stop or held twice; conforms to `Error` |
| `WriteError` | `message: String`; thrown by `VNX.write` for a closed ring or one of fewer than three distinct positions, an arc that is a self-loop or names an absent node, or a walk that is not valid; conforms to `Error` |
| `Position` | `lon`, `lat`, `elevation: Double?`, `timeMilliseconds: Double?`, `isSamePlace(as:)` |
| `Vertex` | `lon`, `lat`, `elevation: Double?`, `position`; a node or an arc pip, which carries no time |
| `LonLat` | `lon`, `lat`, `position`; a walk's `from`, `to` or `turn` |
| `Network` | `nodes: [JSONKey: Vertex]?`, `arcs: [JSONKey: Arc]?`, `highlights: [Highlight]?`, `points`, `areas`, and the item fields |
| `Plan` | `nodes`, `arcs` and `walk: [Pass]`, of a network's shape, and the item fields |
| `Arc` | `from: String`, `to: String`, `pips: [Vertex]` (interior points only), `data` |
| `Highlight` | `walk: [Pass]`, `name`, `hidden`, `pinned`, `data`: a lightweight container for data objects along a walk, with an optional name and no notes or style (a `notes` or `style` on one is an unknown key) |
| `Pass` | `arc: String`, `from: Endpoint?`, `to: Endpoint?`, `turn: LonLat?`, `pin: LonLat?` |
| `Endpoint` | `.node(String)` or `.position(LonLat)` |
| `Area` | `polygon: [Position]`, one open ring, and the item fields |
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
- **An absent collection is `nil`, not `[]`.** `Document` keeps the spelling the
  document used, though the two carry the same content (SPEC §3.1). Where the key
  is required — `Highlight.walk`, `Plan.walk`, `Track.segments`, an arc's `pips` —
  it is a plain array.
- **A walk is used whole or not at all.** Any fault in a walk drops the highlight,
  or the plan, that carries it, and only the first fault is reported, in the
  order of SPEC §6.2. A dropped node takes each arc naming it, and a dropped arc
  takes each highlight walking it, each with its own diagnostic.
- **`Pass.from` and `Pass.to` are `nil` where SPEC §3.3.3's rule leaves them
  out**, not where they are unknown. A pass is pinned when `pin` is set.
- **The writer refuses what its own reader would repair or drop**, with a
  `WriteError`: a closed ring, a ring of fewer than three distinct positions, a
  self-loop arc, an arc naming an absent node, or a walk that is not valid. It
  does not repair them for you.
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

The manifest is at the repository root, and `swift test` finds it from any
directory inside the repository, so this and `swift test` at the root run the
same suite.

Unit tests and the corpus runner over the accept, reject, diagnose and write
tiers. A green run is the conformance claim.

## Layout

| File | What it holds |
|---|---|
| `JSONValue.swift`, `JSONParser.swift` | a strict RFC 8259 parser and its tree |
| `Model.swift` | the decoded document (SPEC §3) |
| `Decoder.swift` | what is refused, dropped and reported (SPEC §6.2) |
| `Encoder.swift` | the emit boundary — the shortest spelling of a position (SPEC §4.1) — and the writer's refusals |
| `Walk.swift` | the valid-walk rules, stepped pass by pass; the reader and the writer share it (SPEC §3.3.3) |
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
