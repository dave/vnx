# VNX for JavaScript

`@viatrail/vnx` reads and writes VNX documents and VNZ containers in
JavaScript, and reports diagnostics. It implements
[`SPEC.md`](../SPEC.md) version 0.2 and passes [`corpus/`](../corpus).

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
  for (const highlight of network.highlights ?? []) console.log('highlight', highlight.name);
}
for (const d of diagnostics) console.log(d.code, d.path, d.message);

console.log(encode(document));
```

## Command-line tool

This library has none. The reference command-line tool is Go's; see
[`go/README.md`](../go/README.md#command-line-tool).

## API

| Call | Arguments | Returns | Throws |
|---|---|---|---|
| `decodeText` | document text | a reading | `VnxRefusal` |
| `decodeValue` | a parsed JSON value | a reading | `VnxRefusal` |
| `encode` | a `Document` | document text | `Error`: an application key with no full stop or listed twice, a value JSON cannot carry, or a document its own reader would repair or drop |
| `emitDocument` | a `Document` | a plain JSON value | |
| `decodeContainer` | a `Buffer` of `.vnz` bytes | a container | `VnxRefusal` |
| `writeContainer` | a container | a `Buffer` | |
| `encodeContainer` | document text, `Iterable<[name, Buffer]>` | a `Buffer` | `VnxRefusal` |
| `indexPositions` | a `Document` | the `positions` map below | |
| `walkLegs` | `nodes`, `arcs`, a walk | one `Position[]` per pass | `Error`: the walk does not resolve |
| `walkLine` | `nodes`, `arcs`, a walk | the legs joined into one `Position[]` | `Error`: the walk does not resolve |
| `arcLine` | `nodes`, an `Arc` | the arc's whole line, node to node | `Error`: the arc names an absent node |

A **reading** is `{ document, diagnostics, positions }`. `positions` maps a
JSON Pointer to the `Position` at that pointer, which resolves a diagnostic's
`path` back to a coordinate.

A **container** is a reading plus `documentEntry`, the root document's archive
entry name, and `attachments`, a **`Map`** from entry name to `Buffer`. Its size
is `attachments.size`, not `.length`.

A `Document` carries `folders`, `networks`, `routes`, `tracks`, `waypoints`,
`areas`, `appKeys` and `counts`. `appKeys` is a list of `AppKey` pairs, `key`
and `value`, sorted by key: the application keys of
[SPEC §3.9](../SPEC.md). A `null` value is a present key, and `encode` throws on
a key with no full stop or a key listed twice. `encode` also throws on
`undefined`, a function, a symbol, a bigint or a non-finite number anywhere in
the document, where `JSON.stringify` would drop the value or write `null`.

A `Network` carries `nodes` and `arcs`, each a `Map` keyed by id, and `highlights`,
`points` and `areas`. A `Route` carries its own `nodes`, `arcs` and one `walk`.
A walk is a list of `Pass` (`arc`, `from`, `to`, `turn`, `pinned`) over the
arcs of its owner, by reference. `from` and `to` are an `Endpoint`, a node key
or a `Position`, and are present only where [SPEC §3.3.3](../SPEC.md)'s rule for
`from` and `to` puts them. An `Arc`'s `pips` are its interior points only.

`encode` refuses what its own reader would repair or drop: an arc that is a
self-loop or names an absent node, a walk that breaks a rule, a time on a node
or an arc pip, an elevation or a time on a walk position, and an area ring that
is closed or holds fewer than three distinct positions. It does not repair any
of them.

`walkLegs` draws a walk the way it is walked: from each pass's entry, out to
its turn where it has one, and on to its exit. A walk position is cut at the
nearest point of its arc's line, at any distance. A walk that does not resolve
is an error, never the whole arc drawn forwards. Model classes are exported
from the package root; `src/model.js` has the fields.

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
  array; `lon`, `lat`, `ele` and `time` are the channels. A node and an arc pip
  keep only `lon`, `lat` and `ele`, and a walk position only `lon` and `lat`:
  the reader drops any other channel with a diagnostic.
- A walk is used whole or not at all. Any problem in a walk drops its highlight, or
  its route, and the diagnostic names the pass and the rule. A dropped arc drops
  every highlight that walks it, with a diagnostic of its own.
- `Track#movingTimeMs()` returns `null` for unknown. Unknown is not zero.
- Opaque values come back whole. Data record bodies, `style` objects and
  application-key values are the parsed JSON value, and nothing inspects them.
- A backslash in an archive entry name is refused outright, which is stricter
  than the ZIP specification. An archive authored on Windows can carry one.

Every `Diagnostic` carries a `code`, a short stable token, beside its English
`message`, which is advisory and is never compared. Branch on `code`.
`DIAGNOSTIC_CODES` and `PINNED_CODES` in `src/errors.js` are the registers.
Every problem [SPEC §6.2](../SPEC.md)'s remedy table names carries that table's
code. Four codes are this library's own, for shapes the table does not name:
`item-unusable`, `record-unusable`, `folder-absent` and `parent-folder-absent`.

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
| `src/walk.js` | the walk rules, the encoder's checks, and `walkLegs` (SPEC 3.3.3) |
| `src/geo.js` | the nearest point of an arc's line to a position (SPEC 3.3.3) |

## Notes for porters

A coordinate is written at the precision the model holds it. SPEC 4.1 does not
constrain a writer's precision — its six-places paragraph is advice, mirroring
RFC 7946 Section 11.2, and states no requirement — so this library rounds nothing on
the way out. A reader MUST NOT round either. The one emit-boundary rule left is
the shortest spelling of a position, which is about ARITY and not precision:
`[lon, lat, null]` loses its trailing null, and `[lon, lat, null, time]` does
not. See [`IMPLEMENTING.md`](../IMPLEMENTING.md).

Version comparison is "not this reader's series", not "newer than this reader".
Through 0.x each MINOR is its own series ([SPEC §2.2](../SPEC.md)), so a 0.2
reader refuses 0.1.9 for the same reason it refuses 0.3.0, and only PATCH is
free. A reader written with `>` reads the older document under rules that were
never meant for it. `reject/version-too-old.vnx` is the case that catches it.

Archive entries are sorted by comparing UTF-8 bytes. `Array.prototype.sort()`
orders by UTF-16 code unit, which disagrees above U+FFFF, and an attachment name
may carry any character.
