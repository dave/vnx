# VNX for Kotlin

A VNX codec on the JVM: reads and writes `.vnx` and `.vnz`, and reports
diagnostics. It was written from [`SPEC.md`](../SPEC.md) and validated against
[`corpus/`](../corpus/), without reference to any other implementation.

```sh
./gradlew test
```

That runs the whole conformance corpus — accept, reject, diagnose, write and
numbers — plus this library's own guards. It is a standalone Gradle build and is
not wired into any application.

## One dependency

The library imports the JVM standard library and
[`kotlinx-serialization-json`](https://github.com/Kotlin/kotlinx.serialization),
and nothing else. `PortabilityTest` asserts the property rather than trusting
the build file, and also asserts that the JSON library is confined to
[`Serialise.kt`](src/main/kotlin/app/viatrail/vnx/Serialise.kt).

The JVM standard library has no JSON API at all, so a JVM implementation either
takes a JSON library or writes one. Nothing about the bytes of a document is a
rule any more — `SPEC.md` section 4.2 defines round-trip at value level and
section 8.1 compares parsed documents — so writing one buys nothing.

**The reader is still this library's own.** Only the renderer went. What a
reader does with a bad document is specified in detail, and a stock parser
answers on its own terms instead: it stops at the first syntax error, decides
for itself what a number that overflows binary64 becomes, and offers permissive
modes that accept exactly what section 4.2 forbids.

## Using it

```kotlin
val result = Vnx.read(File("trip.vnx"))
result.document.tracks?.forEach { println("${it.name}: ${it.movingTimeMs()} ms") }
result.diagnostics.forEach { println("${it.path}: ${it.code} ${it.message}") }   // never ignore these

Vnx.write(result.document)                       // the document, compact JSON
Vnx.readContainer(bytes)                         // a .vnz and its attachments
Vnx.writeContainer(document, attachments)        // a .vnz, deflated
```

There is ONE emit operation, and it is a write: coordinates rounded to six
decimal places and elevations to one, and the shortest spelling of every
position (section 4.1). So the bytes a writer produces are legitimately not the
bytes it read, which is why section 8.1 compares documents as JSON trees.

A refusal arrives as `VnxRefusal`, separately from the diagnostics, so a caller
can tell a document that was refused from one that was read with elements
dropped (`SPEC.md` section 6.3). Every refusal carries a `RefusalCause` and
every diagnostic a `DiagnosticCause`, so nothing has to match on prose.
`Diagnostic.code` is the format's reason code and is what the corpus pins;
`Diagnostic.message` is advisory English that nothing compares. Refusals are
still matched as substrings until typed errors land.

## What it does not do

- **No GPX, KML or GeoJSON.** Those are conversion tools, not part of the format,
  and no library is expected to implement them.
- **No fixture for the empty-segment rule.** `SPEC.md` 3.5 says an empty segment
  contributes zero rather than making a track's moving time unknown, and no
  corpus case carries one — the rule is defended here by a local test only. It is
  offered rather than assumed: say the word and it is a two-line accept fixture.
- **No duplicate-object-key detection.** `SPEC.md` section 6.4 documents this as
  a limitation of the format rather than a rule.
- **No resource bounds.** Section 9 says a reader taking documents from an
  untrusted source SHOULD bound what one document may consume, and this one does
  not.

## Layout

| File | What it holds |
|---|---|
| `Json.kt`, `JsonParser.kt` | the value model, and a strict RFC 8259 parser that refuses `NaN` and `Infinity` |
| `Serialise.kt` | the value model to bytes, through `kotlinx-serialization-json` |
| `Model.kt` | the decoded document; collections distinguish absent from empty |
| `Decoder.kt` | the reader of section 6 — tolerant of damage, silent about nothing |
| `Encoder.kt` | model to JSON, applying section 4.1's emit-boundary rules |
| `Geo.kt` | the 50 m projection tolerance of section 3.3.3 |
| `Zip.kt`, `Vnx.kt` | the VNZ container of section 7, and the public API |
| `Errors.kt`, `Diagnostics.kt` | the closed cause lists, and the table mapping each to the wording the corpus expects |
