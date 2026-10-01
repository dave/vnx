# vnx for Kotlin

`vnx` reads and writes VNX documents and VNZ containers on the JVM, and reports
diagnostics.

Kotlin 2.1.20, on a JDK 17 you have installed. The Gradle wrapper fetches Gradle
itself, not a JDK. One dependency,
[`kotlinx-serialization-json`](https://github.com/Kotlin/kotlinx.serialization)
1.8.0, which this library's POM declares, so it arrives on a consumer's runtime
classpath without the consumer naming it.
Implements VNX 0.8.0; [`SPEC.md`](../SPEC.md) is the format definition. Through
0.x the MINOR component is a breaking axis, so this reader reads the 0.8 series
and refuses every other, in both directions (SPEC §2.2).

## Install

```kotlin
dependencies {
    implementation("app.viatrail:vnx:0.8.0")
}
```

That coordinate is this build's own: `group` is `app.viatrail`, the artifact is
`vnx`, and the version is the VNX version implemented. Install it into your own
Maven local repository to resolve it. In `kotlin/`:

```sh
./gradlew publishToMavenLocal
```

and name that repository in the consuming build:

```kotlin
repositories {
    mavenLocal()
    mavenCentral()
}
```

`mavenCentral()` beside it resolves kotlinx-serialization, which the published
POM asks for. Nothing else is needed: the POM carries the dependency, so writing
a document works without the consumer declaring a JSON library of its own.

Developing against the library — where a change here should be picked up without
republishing — take it in as a composite build instead. In the consuming
`settings.gradle.kts`:

```kotlin
includeBuild("../vnx/kotlin")
```

and the same `implementation("app.viatrail:vnx:0.8.0")` line. Gradle substitutes
the included build for the coordinate because the two agree on `group` and name;
no `dependencySubstitution` block, and no `mavenLocal()`.

## Usage

Read a file, walk a track to its coordinates, save any attachments it carried,
report every fault, write it back.

```kotlin
import app.viatrail.vnx.Vnx
import java.io.File

fun main(args: Array<String>) {
    val reading = Vnx.read(File(args[0]))

    for (track in reading.document.tracks ?: emptyList()) {
        println("track ${track.name ?: "(unnamed)"}")
        for (segment in track.segments) {
            for (pip in segment.pips.take(2)) println("  ${pip.lon}, ${pip.lat}")
        }
    }

    // Null for a plain `.vnx`. For a `.vnz` it is the archive's other entries,
    // each a path AND its bytes, so a caller can write them out.
    for (attachment in reading.attachments ?: emptyList()) {
        println("attachment ${attachment.path}: ${attachment.bytes.size} bytes")
    }
    for (d in reading.diagnostics) println("${d.code} ${d.path}: ${d.message}")

    File("out.vnx").writeText(Vnx.write(reading.document))

    // And back into an archive. A writer takes the bytes it will carry.
    val carried = (reading.attachments ?: emptyList()).associate { it.path to it.bytes }
    File("out.vnz").writeBytes(Vnx.writeContainer(reading.document, carried))
}
```

## Command-line tool

This library has none. The reference command-line tool is Go's; see
[`go/README.md`](../go/README.md#command-line-tool).

## API

| Call | Takes | Returns | Throws |
|---|---|---|---|
| `Vnx.read` | `File`, read as a container where the extension is `.vnz` | `ReadResult` | `VnxRefusal`, and whatever reading the file raises |
| `Vnx.readDocument` | `String` or `ByteArray`, a byte order mark tolerated | `ReadResult` | `VnxRefusal`, one of the four document causes |
| `Vnx.readContainer` | `ByteArray` | `ReadResult` | `VnxRefusal`, a document or a container cause |
| `Vnx.write` | `VnxDocument` | `String` | `IllegalArgumentException`, for a number JSON cannot spell, an application key with no full stop or one listed twice (`SPEC §3.9`), an arc that is a self-loop or names an absent node (`SPEC §3.3.2`), a walk its reader would drop (`SPEC §3.3.3`, §3.4; the message names the pass and the code), or a ring that is closed or holds fewer than three distinct positions (`SPEC §3.7`) |
| `Vnx.writeContainer` | `VnxDocument`, `Map<String, ByteArray>` of entry name to bytes | `ByteArray` | the same, from the document it carries; `VnxRefusal`, for an attachment name a reader would refuse |
| `Vnx.version` | — | `Version` | — |

| Type | Members |
|---|---|
| `ReadResult` | `document: VnxDocument`, `diagnostics: List<Diagnostic>`, `attachments: List<Attachment>?` |
| `Attachment` | `path: String`, `bytes: ByteArray`; equality is by value |
| `Diagnostic` | `path: String`, `message: String`, `cause: DiagnosticCause`, `code: String?` |
| `VnxRefusal` | `message: String`, `refusal: RefusalCause`; extends `Exception` |
| `VnxDocument` | `vnx`, `generator`, `created`, and `folders`, `networks`, `routes`, `tracks`, `waypoints`, `areas`, and `appKeys: List<AppKey>` (`SPEC §3.9`) |
| `AppKey` | `key: String`, `value: JsonValue`; a JSON `null` is `JsonNull`; `AppKey.isAppKey(key)` |
| `Position` | `lon`, `lat`, `ele: Double?`, `timeMs: Double?` |
| `Vertex` | `lon`, `lat`, `ele: Double?`: a node or an arc pip, which carries no time |
| `LonLat` | `lon`, `lat`: a position in a walk |
| `Network` | `nodes: Map<String, Vertex>?`, `arcs: Map<String, Arc>?`, `highlights`, `points`, `areas`, and the item fields |
| `Arc` | `from: String`, `to: String`, `pips: List<Vertex>` (interior points only), `data` |
| `Highlight` | `walk: List<Pass>`, and the item fields |
| `Route` | `walk: List<Pass>`, `nodes`, `arcs`, and the item fields |
| `Pass` | `arc: String`, `from: Endpoint?`, `to: Endpoint?`, `turn: LonLat?`, `pinned: Boolean` |
| `Endpoint` | `Endpoint.Node(key)` or `Endpoint.At(position: LonLat)` |
| `Track` | `segments: List<Segment>`, `movingTimeMs(): Long?` |
| `Segment` | `pips: List<Position>` |

`ReadResult.attachments` holds each attachment as a path AND its bytes, in
archive order, and is null where the source was a plain `.vnx`. Archive order is
this library's choice rather than a rule: SPEC §7 fixes the order only for
picking the root document. An attachment whose bytes this reader cannot return —
a compression method it cannot use, a stream that will not decompress, or a CRC
that disagrees with the bytes present — is dropped with an
`attachment-unreadable` diagnostic and is not in the list, because an attachment
handed back empty cannot be told apart from one the archive never carried. The
document comes back either way; only the ROOT document being unrecoverable
refuses the archive, and then because there is nothing to return. A writer
applies the emit-boundary
rules of SPEC §4.1, so two conforming writers can produce different bytes for the
same document and SPEC §8.1 compares parsed documents.

**`Attachment` compares by value.** A Kotlin `data class` holding a `ByteArray`
gets `equals` by reference for that member, so this type writes its own: two
attachments with the same path and the same content are equal.

## Errors and diagnostics

A refusal and a diagnostic are separate channels (SPEC §6.2–§6.3), and in Kotlin
that is a thrown exception against the list returned beside the document.

- **A refusal is a thrown `VnxRefusal`.** `refusal` is a `RefusalCause`, a closed
  enum, so the whole set is reachable from code; `Errors.kt` documents each one.
- **Through 0.x a version refusal is about the SERIES, not about being newer.**
  Each MINOR is its own series (SPEC §2.2), so this 0.8 reader refuses `0.1.9`
  for the same reason it refuses `0.9.0`, and both raise
  `VERSION_SERIES_UNSUPPORTED`. Only `PATCH` is free: `0.8.5` reads.
- **Damage to one attachment is reported, not thrown.** SPEC §7 rules refusing a
  whole trail library over one damaged photograph disproportionate, so a
  corrupt or unreadable attachment leaves the document intact and raises an
  `attachment-unreadable` diagnostic. `CONTAINER_ROOT_UNREADABLE` is the same
  fault on the root document, where a refusal is the only answer left.
- **A diagnostic comes back with the document.** Branch on `Diagnostic.code`, the
  format's reason code; `message` is advisory English that nothing compares
  (SPEC §6.3). `path` is a JSON Pointer (RFC 6901) into the document, except
  `attachment-unreadable`, which points into the archive's entries.
- **An absent collection is `null`, not an empty list.** `VnxDocument` keeps the
  spelling the document used, though the two carry the same content (SPEC §3.1).
  Inside an item it is a plain list.
- **`Track.movingTimeMs()` returns null for unknown.** Unknown is not zero.
- **A `Position` holds four values, not the arity the document spelled.** `ele`
  and `timeMs` are null where a channel was absent or JSON `null`. A node and an
  arc pip are a `Vertex`, and a walk position is a `LonLat`, so a channel the
  format forbids there cannot be built. The reader drops it with an
  `extra-channel` or `arc-pip-time` diagnostic.
- **A walk is used whole or not at all.** Any problem in a walk drops the highlight,
  or the route, that carries it, and only the first problem is reported (SPEC
  §6.2). An arc that is dropped takes each highlight that walks it, and each drop
  has its own diagnostic.
- **This reader bounds nothing.** SPEC §9 has a reader of untrusted documents
  bound what one may consume; impose that limit before handing it an upload.

## Run the tests

```sh
cd kotlin && ./gradlew test
```

Unit tests and the corpus runner over the accept, reject, diagnose and write
tiers. A green run is the conformance claim.

## Layout

| File | What it holds |
|---|---|
| `Json.kt`, `JsonParser.kt` | a strict RFC 8259 parser and its tree |
| `Serialise.kt` | the tree to bytes, through kotlinx-serialization |
| `Model.kt` | the decoded document (SPEC §3) |
| `Decoder.kt` | what is refused, dropped and reported (SPEC §6.2) |
| `Encoder.kt` | the emit boundary (SPEC §4.1), and what a writer refuses |
| `Walk.kt` | the walk rules of SPEC §3.3.3, in the order §6.2 fixes, for the reader and the writer |
| `Zip.kt`, `Vnx.kt` | the VNZ container (SPEC §7), and the public API |
| `Version.kt` | `MAJOR.MINOR.PATCH`, compared as numbers of any length |
| `Errors.kt`, `Diagnostics.kt` | the reason codes, and the tables tying them to the corpus |

## Notes for porters

General form in [`IMPLEMENTING.md`](../IMPLEMENTING.md); these are the parts
shaped by Kotlin.

- The JVM standard library has no JSON API, so the renderer takes
  kotlinx-serialization. The parser is this library's own: a reader enforces rules
  a stock parser does not, and `NaN` and `Infinity` are not JSON tokens.
- `PortabilityTest` reads the dependency set off the compiled classes rather than
  the build file, and confines the JSON library to `Serialise.kt`.
- **`VnxRefusal` extends `RuntimeException`, and a port to another JVM language
  should keep it unchecked.** Kotlin has no checked exceptions, so it emits no
  `throws` clause naming a refusal. A checked `VnxRefusal` therefore does not
  oblige a Java caller to handle one, and `javac` rejects `catch (VnxRefusal e)`
  around a `Vnx.readDocument` call as an exception never thrown — leaving
  `catch (Exception)` and a downcast as the only route to the cause. Unchecked
  gives up a compulsion that was never enforced and buys back the catch.
