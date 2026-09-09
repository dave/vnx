# VNX for JavaScript

A codec for the VNX format: read and write `.vnx` and `.vnz`, produce canonical
form, and report diagnostics. Implemented from [`SPEC.md`](../SPEC.md) and
validated by [`corpus/`](../corpus).

Node 22+, ESM, **no runtime dependencies**. The ZIP container is written here
rather than imported: the format restricts compression to store and deflate, and
`node:zlib` supplies deflate.

```js
import {
  decodeText, rerender, write, canonicalise,
  decodeContainer, rerenderContainer, writeContainer,
} from '@viatrail/vnx';

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

A conforming library offers both (`SPEC.md` 4.5.4). The container path splits the
same way — `rerenderContainer` and `writeContainer` — so repackaging a container
unchanged cannot silently round it. `encodeContainer` takes document text the
caller has already produced, so the choice is the caller's.

The `write/` corpus tier exercises the first path and is the only document tier
that does; see `corpus/write/README.md`. `corpus/numbers/` reaches the same
boundary at value level, pinning what every implementation must emit for a given
double before and after rounding to 6 and to 1 places.

A rounding TIE rounds to even (`SPEC.md` 4.5.4): `340.25` to one place is `340.2`,
where JavaScript's `toFixed` gives `340.3`. `src/round.js` decides the tie on the
double's exact value in BigInt, because the obvious round-trip check finds
phantoms. `write/emit-boundary` now catches a wrong tie direction; the local
guard in `test/round.test.js` predates that tier and stays, red-proofed against a
mutated copy of `src/round.js`.
`SPEC.md` 4.5.3's rendering TIE is free here (V8 rounds to even) but IS reachable
by a re-render, because a re-render always re-spells, so `format/number-tie`
catches it and a red-proof shows it would still catch a hand-written renderer in
this seat.

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

Every `Diagnostic` carries a `code` — a short stable token (`SPEC.md` 6.3) —
beside its English `message`, which is advisory and is never compared. The corpus
pins six codes today; `PINNED_CODES` and `DIAGNOSTIC_CODES` in `src/errors.js`
are checked against the corpus in both directions, so neither register can drift
as diagnose cases are added.

A `VnxRefusal` still carries only English, matched as a substring — a known
defect. `REFUSAL_SUBSTRINGS` is the migration map for when refusals gain codes,
and a test keeps it honest against `reject/`.

## Running the corpus

```sh
node corpus/run.js              # ../corpus
node corpus/run.js /path/to/corpus
npm test                        # unit tests, including the red-proofs
VNX_CORPUS=/path/to/corpus npm test
```

`corpus/` is a shared tree and is routinely mid-edit, so measure against a
`git archive HEAD corpus` extract as a second, separately labelled run:

```sh
mkdir -p /tmp/vnx-head && git -C .. archive HEAD corpus | tar -x -C /tmp/vnx-head
node corpus/run.js /tmp/vnx-head/corpus
VNX_CORPUS=/tmp/vnx-head/corpus npm test
```

A red-proof that depends on a particular fixture SHAPE asserts that shape first,
so running against an older corpus fails by naming the reason rather than by a
bare inequality.

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
- **A backslash in an archive entry name is refused outright**, which is
  stricter than [APPNOTE]. Ruled 2026-09-09; no corpus case covers it, so
  `test/vnz.test.js` holds the guard and a red-proof records the gap.
