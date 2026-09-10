# VNX for JavaScript

A codec for the VNX format: read and write `.vnx` and `.vnz`, and report
diagnostics. Implemented from [`SPEC.md`](../SPEC.md) and
validated by [`corpus/`](../corpus).

Node 22+, ESM, **no runtime dependencies**. The ZIP container is written here
rather than imported: the format restricts compression to store and deflate, and
`node:zlib` supplies deflate.

```js
import {
  decodeText, encode,
  decodeContainer, encodeContainer, writeContainer,
} from '@viatrail/vnx';

const { document, diagnostics, positions } = decodeText(text);
for (const d of diagnostics) console.log(d.path, d.message);

encode(document); // a document written from the model: rounded, shortened
```

## One emit path

`encode` is `JSON.stringify` over a plain JSON value built from the model. The
bytes of a document are not constrained (`SPEC.md` 8.1) — key order, number
spelling and whitespace are this library's own business — so there is no
formatter and no canonical form. The output is compact: a document is mostly
coordinates, and indentation would be size for no reader.

Two rules ARE applied at the emit boundary (`SPEC.md` 4.1):

- longitude and latitude rounded to 6 decimal places, elevation to 1;
- the shortest spelling of a position — trailing `null` channels dropped, an
  interior `null` kept.

Neither happens in the model. `writeContainer` takes a decoded container back
out through the same path; `encodeContainer` takes document text the caller has
already produced.

The `write/` corpus tier exercises those two rules and is the only document tier
that does; see `corpus/write/README.md`. `corpus/numbers/` reaches the same
boundary at value level, pinning the rounded value for a given double by BIT
PATTERN — never as text, which would put a spelling rule back into the
comparison through the side door.

A rounding TIE rounds to even (`SPEC.md` 4.1): `340.25` to one place is `340.2`,
where JavaScript's `toFixed` gives `340.3`. Rounding is by FORMATTING, never by
scaling: `src/round.js` decides the tie on the double's exact value in BigInt and
reaches the result through decimal text, because scaling by a power of ten
manufactures ties the true decimal never had. The obvious round-trip tie check
finds phantoms and is not used.

## The container deflates

`SPEC.md` 7.1 lets a writer store or deflate per entry and constrains no other
field of the archive. This one deflates, so a `.vnz` is smaller than the document
it holds. The reader still takes both.

Archives written by two conforming implementations are not expected to agree byte
for byte, so the corpus's container case compares entry names, attachment bodies
and the root document's tree — not bytes.

## The accept tier compares TREES

`SPEC.md` 8.1: decode the file, encode the result, and compare the tree parsed
from the ORIGINAL FILE against the tree parsed from the encoded output. Exactly
two normalisations are permitted and no others — on a position, trailing `null`
channels dropped and §4.1's precision applied to both sides; and an absent
container reading the same as an empty one.

⚠ It is never model-versus-model. A reader that discarded every data record,
every ruleset, every style and every `notes` produces a model equal to ITSELF and
passes a tier built that way green — that is the whole extensibility mechanism,
all styling and all user prose. `test/redproof.test.js` runs all four of those
readers and asserts each one is caught.

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
this library, one defect at a time, and asserts which tier catches each. Several
of its cases assert that a defect is **not** caught, which records a gap in the
corpus rather than assuming there is none.

## What this library does not do

- **No GPX, KML or GeoJSON.** Those are conversion tools, not part of the format.
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
