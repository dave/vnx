# VNX for Kotlin

`vnx` reads and writes VNX documents and VNZ containers on the JVM, and reports
diagnostics.

Kotlin 2.1 and a JVM 17 toolchain, which the Gradle wrapper fetches. One
dependency,
[`kotlinx-serialization-json`](https://github.com/Kotlin/kotlinx.serialization)
1.8.0, which has to reach the runtime classpath of whatever consumes this.
Implements VNX 0.1.0; [`SPEC.md`](../SPEC.md) is the format definition.

## Install

Clone the repository and take this build in as a composite build. In the
consuming `settings.gradle.kts`:

```kotlin
includeBuild("../vnx/kotlin") {
    dependencySubstitution {
        substitute(module("app.viatrail:vnx")).using(project(":"))
    }
}
```

and in the consuming `build.gradle.kts`:

```kotlin
dependencies {
    implementation("app.viatrail:vnx")
}
```

**The coordinate `app.viatrail:vnx` is invented by the consumer.** It matches
nothing in this build and exists only to give the substitution rule something to
bind to; any string serves, so long as both lines spell it alike. Drop the
`dependencySubstitution` block and resolution fails with `Could not find
app.viatrail:vnx:`. This route puts kotlinx-serialization on the runtime
classpath without the consumer naming it.

A flat jar is the alternative: `./gradlew jar` here produces
`build/libs/vnx.jar`, which `implementation(files(".../vnx.jar"))` compiles
against. **Declare
`implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.8.0")` beside
it.** `files()` carries no POM, so nothing transitive arrives with the jar, and
its absence is not a build error: reading succeeds, because the parser is this
library's own, and writing throws
`NoClassDefFoundError: kotlinx/serialization/json/Json` at run time.

## Usage

Read a file, walk a track to its coordinates, report every fault, write it back.

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
    for (d in reading.diagnostics) println("${d.code} ${d.path}: ${d.message}")

    File("out.vnx").writeText(Vnx.write(reading.document))
}
```

## API

| Call | Takes | Returns | Throws |
|---|---|---|---|
| `Vnx.read` | `File`, read as a container where the extension is `.vnz` | `ReadResult` | `VnxRefusal`, and whatever reading the file raises |
| `Vnx.readDocument` | `String` or `ByteArray`, a byte order mark tolerated | `ReadResult` | `VnxRefusal`, one of the four document causes |
| `Vnx.readContainer` | `ByteArray` | `ReadResult` | `VnxRefusal`, a document or a container cause |
| `Vnx.write` | `VnxDocument` | `String` | `VnxRefusal`, for a number JSON cannot spell |
| `Vnx.writeContainer` | `VnxDocument`, `Map<String, ByteArray>` | `ByteArray` | the same, from the document it carries |
| `Vnx.version` | — | `Version` | — |

| Type | Members |
|---|---|
| `ReadResult` | `document: VnxDocument`, `diagnostics: List<Diagnostic>`, `attachments: List<String>?` |
| `Diagnostic` | `path: String`, `message: String`, `cause: DiagnosticCause`, `code: String?` |
| `VnxRefusal` | `message: String`, `refusal: RefusalCause`; extends `Exception` |
| `VnxDocument` | `vnx`, `profile`, `generator`, `created`, and `folders`, `networks`, `routes`, `tracks`, `waypoints`, `areas`, `rulesets` |
| `Position` | `lon`, `lat`, `ele: Double?`, `timeMs: Double?`, `meets()` |
| `Track` | `segments: List<Segment>`, `movingTimeMs(): Long?` |
| `Segment` | `pips: List<Position>` |

`ReadResult.attachments` holds entry names in archive order, not bytes, and is
null where the source was a plain `.vnx`. A writer applies the emit-boundary
rules of SPEC §4.1, so two conforming writers can produce different bytes for the
same document and SPEC §8.1 compares parsed documents.

## Errors and diagnostics

A refusal and a diagnostic are separate channels (SPEC §6.2–§6.3), and in Kotlin
that is a thrown exception against the list returned beside the document.

- **A refusal is a thrown `VnxRefusal`.** `refusal` is a `RefusalCause`, a closed
  enum, so the whole set is reachable from code; `Errors.kt` documents each one.
- **A diagnostic comes back with the document.** Branch on `Diagnostic.code`, the
  format's reason code; `message` is advisory English that nothing compares
  (SPEC §6.3). `path` is a JSON Pointer (RFC 6901) into the document, except
  `attachment-unreadable`, which points into the archive's entries.
- **An absent collection is `null`, not an empty list.** `VnxDocument` holds the
  two apart, so a read-write-read is a fixed point. Inside an item it is a plain
  list.
- **`Track.movingTimeMs()` returns null for unknown.** Unknown is not zero.
- **A `Position` holds four values, not the arity the document spelled.** `ele`
  and `timeMs` are null where a channel was absent or JSON `null`.
- **This reader bounds nothing.** SPEC §9 has a reader of untrusted documents
  bound what one may consume; impose that limit before handing it an upload.

## Run the tests

```sh
cd kotlin && ./gradlew test
```

Unit tests and the corpus runner over the accept, reject, diagnose, write and
numbers tiers. A green run is the conformance claim.

## Layout

| File | What it holds |
|---|---|
| `Json.kt`, `JsonParser.kt` | a strict RFC 8259 parser and its tree |
| `Serialise.kt` | the tree to bytes, through kotlinx-serialization |
| `Model.kt` | the decoded document (SPEC §3) |
| `Decoder.kt` | what is refused, dropped and reported (SPEC §6.2) |
| `Encoder.kt` | the emit boundary and its precision (SPEC §4.1) |
| `Geo.kt` | the equirectangular map and its tolerance (SPEC §3.3.3) |
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
