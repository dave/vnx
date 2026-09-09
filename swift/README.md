# VNX for Swift

A VNX codec: read and write `.vnx` and `.vnz`, render canonical form, report
diagnostics. Swift 5.9+, Foundation only, no package dependencies.

```sh
cd swift && swift test
```

## What it does

| Call | What it does |
|---|---|
| `VNX.read(_:)` | reads a `.vnx`, returning a `Document` and its diagnostics, or throwing a `Refusal` |
| `VNX.readContainer(_:)` | reads a `.vnz`, returning the same plus its attachments |
| `VNX.render(_:)` | RE-RENDER: canonical bytes with neither rounding nor position shortening |
| `VNX.write(_:)` | WRITE: canonical bytes with the emit-boundary rules of SPEC 4.5.4 and 4.1 |
| `VNX.canonicalise(_:)` | decode, then re-render |
| `VNX.writeContainer(document:attachments:)` | writes a `.vnz` with every entry stored |

`render` and `write` are two operations, not one with a flag, because SPEC 8.1
distinguishes them and the accept tier requires the first. A writer rounds
coordinates and emits the shortest spelling of a position, so writing what you
read is legitimately not what you read.

## What it does not do

- **No GPX, KML or GeoJSON.** The repository README calls those "conversion
  tools, not part of the format ... no library is expected to implement them".
- **No JSON Schema validation.** SPEC 8.2 publishes
  `corpus/schema/vnx-0.1.schema.json` so a document can be checked with *no* VNX
  implementation present. The corpus runner here reads each case's `schema`
  verdict and asserts the one direction a reader can check without a validator:
  a case marked `not-json` must be refused.
- **No deflate compressor.** It inflates, because SPEC 7 requires reading
  deflate; it writes stored, which SPEC 7 permits.
- **No duplicate-object-key detection.** SPEC 6.4 documents this as a limitation
  of the format. `corpus/diagnose/duplicate-object-key.vnx` expects no
  diagnostic, and if a reader ever gains the ability to see one, that file goes
  red and the limitation gets revisited.

## Reason codes

SPEC 6.3 gives a diagnostic a `code`, and the corpus compares codes rather than
English. Every `Diagnostic` and every `Refusal` here carries a `Cause`, whose
`code` is the corpus's own token for the six the corpus pins and this library's
own kebab-case token for the rest. `Cause.corpusCode` and
`Cause.corpusSubstring` are asserted against the corpus in both directions, so
neither table can go stale in silence.

A refusal has no code in the format yet — the corpus still matches `refusal` as
an English substring, which its README calls the remaining place it compares
English across implementations. `Cause` distinguishes the refusals the corpus
cannot: `malformedJSON` and `nonFiniteNumber` both concern SPEC 4.2 and only the
first has a corpus wording.

## Layout

| File | What it holds |
|---|---|
| `JSONValue.swift`, `JSONParser.swift` | a strict RFC 8259 parser; `NaN` and `Infinity` are not tokens |
| `NumberRendering.swift` | ECMAScript `Number::toString`, built on Swift's shortest-round-trip digits |
| `CanonicalWriter.swift` | SPEC 4.5: key groups, inline number arrays, escaping, two-space indent |
| `Model.swift` | the document model, including a position's arity as state |
| `Decoder.swift` | SPEC 6.2 in full: what is refused, what is dropped, what is reported |
| `Encoder.swift` | the model back to JSON, in either emit mode |
| `Projection.swift` | SPEC 3.3.3's equirectangular map and the 50 m tolerance |
| `Inflate.swift`, `Zip.swift`, `Container.swift` | RFC 1951 inflate, ZIP with ZIP64, and SPEC 7 |
| `Cause.swift` | reason codes, and the tables tying them to the corpus |

## Tests

`Tests/VNXTests/CorpusTests.swift` runs all four corpus tiers.
`LibraryTests.swift` covers what the corpus does not reach — ZIP64 central
directories, the root-document rule at depth, rounding ties, version spellings,
model-first round trips. `NumberRenderingTests.swift` pins twenty doubles that
are exact ties at their shortest decimal length; both spellings of each parse
back to the same double, so nothing but a pinned expectation can catch a
disagreement there.
