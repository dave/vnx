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
  deflate; it writes stored, which SPEC 7.1 now REQUIRES — deflate has no
  canonical output, so a deflated archive is byte-reproducible only by the
  language that wrote it.
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
| `Inflate.swift`, `Zip.swift`, `Container.swift` | RFC 1951 inflate, ZIP with ZIP64, and SPEC 7 and 7.1 |
| `Rounding.swift` | SPEC 4.5.4 rounding, from the exact decimal expansion, ties to even |
| `Version.swift` | `MAJOR.MINOR.PATCH`, compared as numbers of any length |
| `Cause.swift` | reason codes, and the tables tying them to the corpus |

## Tests

`Tests/VNXTests/CorpusTests.swift` runs all five corpus tiers, including
`write/`, which is the only one that exercises the emit boundary.
`LibraryTests.swift` covers what the corpus does not reach — ZIP64 central
directories, the root-document rule at depth, version spellings, model-first
round trips. `NumberRenderingTests.swift` pins twenty doubles that are exact
ties at their shortest decimal length; `format/number-tie` reaches that rule
too, since it carries one spelling in and the other out.

`NumberConformanceTests.swift` runs the shared number-conformance table —
5,315 doubles as IEEE 754 hex with their canonical spelling and their spelling
after rounding to 6 and to 1 places, generated from Go. It is a fifth
conformance tier in everything but its location.

`ContainerBytesTests.swift` checks the `.vnz` writer against an independent one
written from SPEC 7.1's table alone, in another language, sharing no code with
this library — 537 bytes for a document plus two attachments, one with a
non-ASCII entry name. `ZipHeaderTests.swift` checks the same writer against the
archive two seats agreed by measurement; the two catch different things, one
implementation drifting versus both drifting together.

`VersionRuleTests.swift` reads documents as a hypothetical 1.x reader, because
SPEC 2.2's rule that minor versions are additive from 1.0 onward is otherwise
unreachable from a library that implements 0.1 — and an unreachable branch is
where a mutation goes green and gets mistaken for a tested one.

`RoundingTests.swift` covers SPEC 4.5.4 in depth, against `RoundingOracle` —
4,082 triples computed with rational arithmetic — because the obvious
implementation is wrong in a way that looks right: scaling by a power of ten
MANUFACTURES ties the exact decimal expansion does not have (`0.15` is `0.1`,
not `0.2`) and destroys ties it does (`0.45` is `0.5`, not `0.4`). It was
written when that rule was beyond the corpus, and said so as though that were
permanent; `corpus/write/` reached it the same day. An unreachability claim is
a claim about the current state of someone's ingenuity, not about the format.
