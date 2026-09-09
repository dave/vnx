# VNX for JavaScript

A codec for the VNX format: read and write `.vnx` and `.vnz`, produce canonical
form, and report diagnostics. Implemented from [`SPEC.md`](../SPEC.md) and
validated by [`corpus/`](../corpus).

Node 22+, ESM, **no runtime dependencies**. The ZIP container is written here
rather than imported: the format restricts compression to store and deflate, and
`node:zlib` supplies deflate.

```js
import { decodeText, rerender, write, canonicalise, decodeContainer } from '@viatrail/vnx';

const { document, diagnostics, positions } = decodeText(text);
for (const d of diagnostics) console.log(d.path, d.message);

rerender(document);      // the document as it was handed to the reader
write(document);         // a document written from the model: rounded, shortened
canonicalise(text).text; // decode, then re-render
```

## The two operations that both produce a document

`SPEC.md` 8.1 names them, and they are not interchangeable.

| | Rounding | Shortest spelling | Used for |
|---|---|---|---|
| `rerender(doc)` | no | no | canonical form, the accept tier |
| `write(doc)` | lon/lat to 6 places, elevation to 1 | yes | exporting a model |

`SPEC.md` 4.5.4 does not say which way a rounding TIE goes, and the languages
disagree: `340.25` to one place is `340.2` in Go and Python and `340.3` from
JavaScript's `toFixed`. `src/round.js` rounds half to even, matching the
reference implementation, and decides the tie on the double's exact value in
BigInt. **No corpus tier reaches the writer**, so nothing there can catch a
disagreement: `test/round.test.js` carries the guard for `SPEC.md` 4.5.4's tie
rule, red-proofed against a mutated copy of `src/round.js`, and
`test/redproof.test.js` records that the corpus stays green under the flip.
`SPEC.md` 4.5.3's rendering tie is free here — V8 rounds to even — and is
guarded in `test/canonical.test.js` all the same.

The corpus's accept tier is `rerender(decode(file)) === file`. A writer rounds,
so writing what you read is legitimately not what you read.

## Reading fails loudly or not at all

There are no fallbacks. A malformed value is never turned into a default: it is
dropped, and a diagnostic names what and where. Five things follow from that and
are worth knowing before you use the model:

- **Absent is `undefined`, and is not a default.** An absent `label-visible` is
  Auto, not `false`; an absent collection is not an empty one.
- **A position keeps its arity.** `[lon, lat, null]` and `[lon, lat]` are
  different spellings and the model holds whichever it read. Use
  `position.channels` for the array, `lon` / `lat` / `ele` / `time` for channels.
- **`Track#movingTimeMs()` returns `null` for unknown.** Unknown is not zero.
- **Opaque values come back whole.** Data record bodies, `style` objects and
  `rulesets` are the parsed JSON value; nothing inspects them.
- **A refusal is not a diagnostic.** A refused document throws `VnxRefusal`;
  everything else returns a document alongside its diagnostics.

Every `VnxRefusal` and every `Diagnostic` carries a stable `cause` code beside
its English text. `CAUSE_SUBSTRINGS` in `src/errors.js` maps each cause to the
substring the corpus expects today, so a move to reason codes is mechanical.

## Running the corpus

```sh
node corpus/run.js              # ../corpus
node corpus/run.js /path/to/corpus
npm test                        # unit tests, including the red-proofs
```

`corpus/run.js` reports `n/m cases passed` on its last line and sets an exit
code. A file with no sidecar is a failure, not a skip.

`test/redproof.test.js` runs the corpus against deliberately broken copies of
this library, one defect at a time, and asserts which tier catches each. Three
of its cases assert that a defect is **not** caught, which records a gap in the
corpus rather than assuming there is none.

## What this library does not do

- **No GPX, KML or GeoJSON.** Those are conversion tools, not part of the format.
- **No JSON Schema validation.** Every sidecar carries a `schema` verdict, but
  checking it needs a draft 2020-12 validator, which would be a dependency. The
  runner ignores the field; `corpus/schema/README.md` says the reference
  implementation checks it from a separate package for the same reason.
- **No vocabulary layer.** `app.viatrail.*` record bodies are carried, not
  interpreted, so nothing here knows that a `settlement`'s `size` is an
  enumeration.
- **No duplicate-key detection.** `JSON.parse` resolves duplicates last-wins
  before any VNX rule runs (`SPEC.md` 6.4).
- **No resource bounds.** `SPEC.md` 9 says a reader of untrusted documents
  SHOULD bound what one document may consume. This one does not.
- **ZIP64 is read, not written.** The writer emits a plain archive, which
  `SPEC.md` 7 permits.
