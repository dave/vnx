# VNX for Kotlin

A VNX codec on the JVM: reads and writes `.vnx` and `.vnz`, produces canonical
form, and reports diagnostics. It was written from [`SPEC.md`](../SPEC.md) and
validated against [`corpus/`](../corpus/), without reference to any other
implementation.

```sh
./gradlew test
```

That runs the whole conformance corpus — all four tiers — plus the
number-conformance table and this library's own guards. It is a standalone
Gradle build and is not wired into any application.

The number table (`../number-conformance/`) is a fifth tier in everything but
its location. It exists because the corpus can reach neither of the two rules
that decide these bytes: a rendering tie is a value rather than a shape, and a
rounding tie happens only when a WRITER emits a model, while all four tiers
re-render an existing document. **The corpus's reach ends at the writer
boundary** — an implementation can round every coordinate the wrong way and pass
every tier green. Reverting this library's rounding tie rule reddens those two
columns and nothing else in the suite, which is that claim as a measurement.

## No dependencies

The library imports the standard library and nothing else. There is no JSON
library behind it: the parser, the serialiser and the ECMAScript number renderer
are all here, because the format's canonical form (`SPEC.md` section 4.5) is a
layout no stock encoder in any target language emits, and because a codec with
dependencies is a codec people cannot adopt. `PortabilityTest` asserts the
property rather than trusting the build file.

The one place this costs something visible is
[`EcmaNumber.kt`](src/main/kotlin/app/viatrail/vnx/EcmaNumber.kt). Kotlin's
`Double.toString` renders `340.0`, `1.0E21` and `1.0E-7` where canonical form
requires `340`, `1e+21` and `1e-7`, and Java only guarantees the SHORTEST
round-tripping decimal from JDK 19 — this build targets 17 — so the shortest
form is searched for directly over the exact value of the double.

## Using it

```kotlin
val result = Vnx.read(File("trip.vnx"))
result.document.tracks?.forEach { println("${it.name}: ${it.movingTimeMs()} ms") }
result.diagnostics.forEach { println("${it.path}: ${it.message}") }   // never ignore these

Vnx.render(result.document)   // the bytes this document had: no rounding, arity kept
Vnx.write(result.document)    // a writer's bytes: coordinates rounded, positions shortened
Vnx.canonicalise(text)        // decode and re-render — the `fmt` operation
```

A refusal arrives as `VnxRefusal`, separately from the diagnostics, so a caller
can tell a document that was refused from one that was read with elements
dropped (`SPEC.md` section 6.3). Every refusal carries a `RefusalCause` and
every diagnostic a `DiagnosticCause` beside its English text, so nothing has to
match on prose. `Diagnostic.code` is the format's reason code, which the corpus
sidecars started carrying on 2026-09-09; the runner pairs on it, and still
accepts a sidecar that has not migrated.

`Vnx.render` and `Vnx.write` are two different operations and the difference
matters: section 8.1 requires a RE-RENDER for the accept tier, because a writer
rounds and writing what you read is legitimately not what you read.

## What it does not do

- **No GPX, KML or GeoJSON.** Those are conversion tools, not part of the format,
  and no library is expected to implement them.
- **No JSON Schema validation.** Each corpus case carries a `schema` verdict; the
  runner checks the field's shape and does not act on it, because a draft 2020-12
  validator is a third-party dependency. The reference implementation keeps its
  schema test in a separate package for the same reason.
- **No duplicate-object-key detection.** `SPEC.md` section 6.4 documents this as
  a limitation of the format rather than a rule.
- **No resource bounds.** Section 9 says a reader taking documents from an
  untrusted source SHOULD bound what one document may consume, and this one does
  not.

## Layout

| File | What it holds |
|---|---|
| `Json.kt`, `JsonParser.kt` | the value model, and a strict RFC 8259 parser that refuses `NaN` and `Infinity` |
| `EcmaNumber.kt` | ECMAScript `Number::toString` for a binary64 |
| `Canonical.kt` | the canonical-form serialiser (section 4.5) |
| `Model.kt` | the decoded document; positions keep their arity, collections distinguish absent from empty |
| `Decoder.kt` | the reader of section 6 — tolerant of damage, silent about nothing |
| `Encoder.kt` | model to JSON, as either a RE-RENDER or a WRITE |
| `Geo.kt` | the 50 m projection tolerance of section 3.3.3 |
| `Zip.kt`, `Vnx.kt` | the VNZ container of section 7, and the public API |
| `Errors.kt`, `Diagnostics.kt` | the closed cause lists, and the table mapping each to the wording the corpus expects |
