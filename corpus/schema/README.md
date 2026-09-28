# The VNX JSON Schema

`vnx-0.1.schema.json`, draft 2020-12. It lets a tool validate a document with no
VNX implementation present, which is useful from a script or an agent.

It lives inside the corpus because every corpus case records a `schema` verdict
in its sidecar. A sidecar field that only one implementation can reach is a
corpus that cannot be run, so the schema ships with the cases that cite it.

A document that validates against this schema is not necessarily a valid VNX
document. Several of the format's load-bearing rules are not about shape, and a
schema can only check shape.

## What it checks

- The document is an object with a `vnx` version string.
- Every collection is an array of the right kind of object, and the required
  fields of each are present: a waypoint has an `at`, an area a `polygon`, a
  track its `segments`, a route its `arcs`, a network arc its `from`, `to` and
  `pips`, a link its `arc`, `from` and `to`.
- A position is an array of at least two numbers, longitude within ±180 and
  latitude within ±90, `null` permitted from index 2, extra trailing channels
  ignored.
- A ring holds at least four positions, and holds positions, so a GeoJSON-shaped
  polygon with nested rings is caught here.
- A record-level timestamp is UTC with a `Z` suffix, and nothing else.
- Every `data` record carries an `@type`.
- `importance`, inside a style object, is an integer from 1 to 5. It is the ONLY
  style value checked, because it is the only one `SPEC.md` gives a normative
  range (section 3.6). `icon-size` is deliberately not typed: section 3.6 states
  that no range is stated for it and that `"icon-size": "large"` passes
  unremarked, so a schema typing it would refuse a document every conforming
  reader accepts.

## What it cannot check

Each of these passes the schema and is wrong.
`internal/vnxschema`'s `TestSchemaCannotCatchTheseAndSaysSo` asserts that they
still pass, so if the schema ever grows the reach to catch one, that test goes
red and this list gets corrected rather than quietly going stale.

| Rule | Why a schema cannot reach it |
|---|---|
| lon/lat transposition | Both orderings are numbers in range. The point lands 8,000 km away and every check stays green. |
| A link naming an arc absent from the document | A cross-reference. JSON Schema has no way to say "this string must be a key of that object". |
| A ring that does not close | The last position must repeat the first, which is a relationship between two values. |
| An inline endpoint that is not on its arc | Arithmetic over two values, not a shape. |
| Container-scoped ids | A route's `node-1` and a network's `node-1` are different nodes. That is about scope, not shape. |
| A shared reversal endpoint | Two links in one chain may share an endpoint, and a reader compares inline positions by value. Nothing structural distinguishes it. |
| A version outside the reader's series, in EITHER direction | Reader-relative. The schema does not know what any reader implements. `0.0.9` and `9.9.9` satisfy the version pattern exactly as `0.1.0` does. The low side is the half that bit: through 0.x each minor is its own series (`SPEC.md` section 2.2), and five of the six implementations compared with "greater than" rather than "not equal" and read a 0.0 document. `reject/version-too-old.vnx` is what catches it. |
| A duplicate object key | Resolved last-wins by the parser before the schema runs (`SPEC.md` section 6.4). |
| `NaN` and `Infinity` | Not JSON. No validator ever sees the document. |
| Everything about `.vnz` | An archive is not a JSON document. |

## What it deliberately permits

`additionalProperties` is left open everywhere on purpose. `SPEC.md` section 6.2
makes an unrecognised key on an object a reader diagnostic that keeps the item,
so a schema refusing them would disagree with every conforming reader. The same
applies to `data` record bodies, which are opaque by design — constraining them
would break the extensibility the bag exists for — and to the value of an
application key, a top-level key containing a full stop, which a reader
preserves verbatim and interprets not at all (section 3.9).

## Where the schema and a reader disagree

The corpus records a `schema` verdict per case, so the disagreements are visible
rather than assumed. Two kinds exist and both are correct:

- `reject/version-too-new.vnx` and `reject/version-too-old.vnx` are both
  schema-valid and both reader-refused. Only a reader knows what it implements.
- Most diagnose files are schema-valid and diagnosed: an unrecognised key, a
  dangling link, a chain that does not join. Four are schema-invalid —
  `polygon-second-ring`, `timestamp-with-offset`, `position-unusable` and
  `wrong-typed-value` — because each is wrong in its SHAPE, which is the one
  thing a schema does reach.

## Running it

```sh
cd go && go test ./internal/vnxschema/
```

That test lives in `internal/vnxschema` rather than in `pkg/vnx` because a JSON
Schema validator is a third-party dependency, and `pkg/vnx` imports the standard
library and nothing else. It is the reference implementation, and a dependency-
free build is the property everyone reimplementing this format relies on.

The guard reads test files as well as production ones, which matters: a
third-party import reached only from a `_test.go` file still makes the library
undeliverable, and a guard that skipped tests would let that compile and pass.
