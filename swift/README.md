# VNX for Swift

A VNX codec: read and write `.vnx` and `.vnz`, report diagnostics. Swift 5.9+,
Foundation plus the system zlib. No package dependencies.

```sh
cd swift && swift test
```

## What it does

| Call | What it does |
|---|---|
| `VNX.read(_:)` | reads a `.vnx`, returning a `Document` and its diagnostics, or throwing a `Refusal` |
| `VNX.readContainer(_:)` | reads a `.vnz`, returning the same plus its attachments |
| `VNX.write(_:)` | writes a document, applying the emit-boundary rules of SPEC 4.1 |
| `VNX.writeContainer(document:attachments:)` | writes a `.vnz` with every entry deflated |

There is one emit path. There used to be two, because a formatter had to
reproduce the spelling of the document it was handed; SPEC 4.5 is gone and the
corpus compares parsed documents rather than bytes, so the only remaining
operation is the one that turns a model into a file.

`write` throws where the model holds a number JSON cannot spell (SPEC 4.2).

## What it does not do

- **No GPX, KML or GeoJSON.** The repository README calls those "conversion
  tools, not part of the format ... no library is expected to implement them".
- **No JSON Schema validation.** The corpus runner reads each case's `schema`
  verdict and asserts the one direction a reader can check without a validator:
  a case marked `not-json` must be refused.
- **No hand-written JSON serialiser.** `JSONEncoder` renders the tree.
  `JSONSerialization` would not do: it spells `-3.67` as `-3.6699999999999999`.
  The PARSER is still hand-written, because a reader has rules a stock parser
  does not enforce — `NaN` and `Infinity` are not tokens here.
- **No duplicate-object-key detection.** SPEC 6.4 documents this as a limitation
  of the format. `corpus/diagnose/duplicate-object-key.vnx` expects no
  diagnostic, and if a reader ever gains the ability to see one, that file goes
  red and the limitation gets revisited.

## zlib

`Package.swift` declares a `systemLibrary` target for zlib, with a module map
over `<zlib.h>`. It is the only build-system dependency this library has, and it
is a system one on purpose: zlib ships with the macOS SDK and with every Linux
distribution, so it costs no fetch and no lock file. A conformance library that
needed a package graph could not be dropped into an app that has none.

It buys both directions of DEFLATE — a `.vnz` writer that compresses, and the
inflate a reader needs for SPEC 7 — plus the CRC-32 a ZIP entry carries. The
alternative on Apple platforms is `Compression.framework`, which does not exist
on Linux.

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
| `JSONValue.swift`, `JSONParser.swift` | a strict RFC 8259 parser, and the tree it produces |
| `Model.swift` | the document model. A position holds four values, not the arity the file spelled |
| `Decoder.swift` | SPEC 6.2 in full: what is refused, what is dropped, what is reported |
| `Encoder.swift` | the model back to a JSON tree, with SPEC 4.1's emit boundary applied |
| `Projection.swift` | SPEC 3.3.3's equirectangular map and the 50 m tolerance |
| `Deflate.swift`, `Zip.swift`, `Container.swift` | zlib, ZIP with ZIP64, and SPEC 7 and 7.1 |
| `Rounding.swift` | SPEC 4.1's precision, from the exact decimal expansion, ties to even |
| `CodePointOrder.swift` | the one order this library chooses for itself; Swift's `<` normalises |
| `Version.swift` | `MAJOR.MINOR.PATCH`, compared as numbers of any length |
| `Cause.swift` | reason codes, and the tables tying them to the corpus |

## Tests

`Tests/VNXTests/CorpusTests.swift` runs the accept, reject, diagnose and write
tiers. The accept tier compares the tree parsed from the file against the tree
parsed from `write(read(file))`, applying the two normalisations SPEC 8.1
permits and no others.

`AcceptTierProofTests.swift` freezes why that comparison is against the FILE.
Four spoiled readers — one that drops every data record, every ruleset, every
style, every `notes` — are each asserted to be caught by the tree comparison,
and each asserted to pass a model-versus-model comparison green. That is the
whole extensibility mechanism, all styling and all user prose, silently
droppable by a "conforming" implementation.

`WriterArchiveRulesTests.swift` asserts SPEC 7.1's five rules against the
archive this writer produces, reading the local headers out of raw bytes rather
than through `Zip.entries`. That instrument choice is the point: every ZIP
library resolves an entry from the central directory on purpose, so a library
can never be the instrument for a local-header rule — it reports agreement
whatever the local header says. It no longer compares whole archives against
another implementation's bytes, because 7.1 no longer asks two writers to agree
on them.

`LibraryTests.swift` covers what the corpus does not reach — ZIP64 central
directories, the root-document rule at depth, version spellings, model-first
round trips.

`NumberConformanceTests.swift` runs the shared number-conformance table from
`corpus/numbers/`, generated by the Go reference. It compares the ROUNDED VALUE
by bit pattern and does not read the table's `render` column: how a number is
spelled is the writer's own business (SPEC 4.2), and comparing spellings across
implementations is the rule this format dropped.

`VersionRuleTests.swift` reads documents as a hypothetical 1.x reader, because
SPEC 2.2's rule that minor versions are additive from 1.0 onward is otherwise
unreachable from a library that implements 0.1 — and an unreachable branch is
where a mutation goes green and gets mistaken for a tested one.

`RoundingTests.swift` covers SPEC 4.1's precision rule in depth, against
`RoundingOracle` — 4,082 triples computed with rational arithmetic — because the
obvious implementation is wrong in a way that looks right: scaling by a power of
ten MANUFACTURES ties the exact decimal expansion does not have (`0.15` is
`0.1`, not `0.2`) and destroys ties it does (`0.45` is `0.5`, not `0.4`).
