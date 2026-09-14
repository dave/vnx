# VNX for JavaScript

`@viatrail/vnx` reads and writes VNX documents and VNZ containers in
JavaScript, and reports diagnostics. It implements
[`SPEC.md`](../SPEC.md) version 0.1 and passes [`corpus/`](../corpus).

Node 22 or later, ESM only, no runtime dependencies. The ZIP container is
written here rather than imported, over `node:zlib`.

## Install

Clone the repository and install from the package path:

```sh
npm install ../vnx/js
```

That writes a `file:` dependency, after which `@viatrail/vnx` resolves by name.
Import it from an ES module — a `package.json` with `"type": "module"`, or a `.mjs` file.

## Usage

Read a file, print its diagnostics, write the document back out:

```js
import { readFileSync } from 'node:fs';
import { decodeText, encode, VnxRefusal } from '@viatrail/vnx';

let reading;
try {
  reading = decodeText(readFileSync(process.argv[2], 'utf8'));
} catch (err) {
  if (err instanceof VnxRefusal) { console.error('refused:', err.message); process.exit(1); }
  throw err;
}

const { document, diagnostics } = reading;
for (const network of document.networks ?? []) {
  for (const chain of network.chains ?? []) console.log('chain', chain.name);
}
for (const d of diagnostics) console.log(d.code, d.path, d.message);

console.log(encode(document));
```

## API

| Call | Arguments | Returns | Throws |
|---|---|---|---|
| `decodeText` | document text | a reading | `VnxRefusal` |
| `decodeValue` | a parsed JSON value | a reading | `VnxRefusal` |
| `encode` | a `Document` | document text | |
| `emitDocument` | a `Document` | a plain JSON value | |
| `decodeContainer` | a `Buffer` of `.vnz` bytes | a container | `VnxRefusal` |
| `writeContainer` | a container | a `Buffer` | |
| `encodeContainer` | document text, `Iterable<[name, Buffer]>` | a `Buffer` | `VnxRefusal` |
| `indexPositions` | a `Document` | the `positions` map below | |

A **reading** is `{ document, diagnostics, positions }`. `positions` maps a
JSON Pointer to the `Position` at that pointer, which resolves a diagnostic's
`path` back to a coordinate.

A **container** is a reading plus `documentEntry`, the root document's archive
entry name, and `attachments`, a **`Map`** from entry name to `Buffer`. Its size
is `attachments.size`, not `.length`.

A `Document` carries `folders`, `networks`, `routes`, `tracks`, `waypoints`,
`areas`, `rulesets` and `counts`. A `Network` carries `nodes`, `arcs`, `chains`,
`points` and `areas`, and a chain's `links` traverse those arcs by reference.
Model classes are exported from the package root; `src/model.js` has the
fields.

## Errors and diagnostics

A refusal and a diagnostic are separate channels ([SPEC §6.2](../SPEC.md),
[§6.3](../SPEC.md)). A refused document throws `VnxRefusal`; everything else
returns a document alongside its diagnostics. There are no fallbacks: a
malformed value is dropped and a diagnostic names what and where. Five
consequences for a caller:

- Absent is `undefined` and is not a default. An absent `label-visible` is Auto,
  not `false`, and an absent collection is not an empty one, so a traversal
  needs `?? []`.
- A position keeps its arity. `[lon, lat, null]` and `[lon, lat]` are different
  spellings and the model holds whichever it read. `position.channels` is the
  array; `lon`, `lat`, `ele` and `time` are the channels.
- `Track#movingTimeMs()` returns `null` for unknown. Unknown is not zero.
- Opaque values come back whole. Data record bodies, `style` objects and
  `rulesets` are the parsed JSON value, and nothing inspects them.
- A backslash in an archive entry name is refused outright, which is stricter
  than the ZIP specification. An archive authored on Windows can carry one.

Every `Diagnostic` carries a `code`, a short stable token, beside its English
`message`, which is advisory and is never compared. Branch on `code`.
`DIAGNOSTIC_CODES` and `PINNED_CODES` in `src/errors.js` are the registers.

A `VnxRefusal` carries English only, matched as a substring, so a caller cannot
branch on a refusal cause. `REFUSAL_SUBSTRINGS` is the migration map for when
refusals gain codes.

## Run the tests

```sh
cd js && npm test
```

Unit tests, the red-proofs and the corpus harness. `node corpus/run.js
/path/to/corpus` runs the corpus alone.

## Layout

| Module | What it is |
|---|---|
| `src/decode.js` | reading a document (SPEC 3, 4 and 6) |
| `src/model.js` | the decoded model |
| `src/emit.js` | model to document text |
| `src/vnz.js` | the VNZ container (SPEC 7) |
| `src/zip.js` | the ZIP reader and writer |
| `src/errors.js` | `VnxRefusal`, `Diagnostic` and the code registers |
| `src/geo.js` | the projection tolerance model (SPEC 3.3.3) |

## Notes for porters

A coordinate is written at the precision the model holds it. SPEC 4.1 does not
constrain a writer's precision — its six-places paragraph is advice, mirroring
RFC 7946 Section 11.2, and states no requirement — so this library rounds nothing on
the way out. A reader MUST NOT round either. The one emit-boundary rule left is
the shortest spelling of a position, which is about ARITY and not precision:
`[lon, lat, null]` loses its trailing null, and `[lon, lat, null, time]` does
not. See [`IMPLEMENTING.md`](../IMPLEMENTING.md).

Version comparison is "not this reader's series", not "newer than this reader".
Through 0.x each MINOR is its own series ([SPEC §2.2](../SPEC.md)), so a 0.1
reader refuses 0.0.9 for the same reason it refuses 0.2.0, and only PATCH is
free. A reader written with `>` reads the older document under rules that were
never meant for it. `reject/version-too-old.vnx` is the case that catches it.

Archive entries are sorted by comparing UTF-8 bytes. `Array.prototype.sort()`
orders by UTF-16 code unit, which disagrees above U+FFFF, and an attachment name
may carry any character.
