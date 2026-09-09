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
| A version above the reader's | Reader-relative. The schema does not know what any reader implements. |
| A duplicate object key | Resolved last-wins by the parser before the schema runs (`SPEC.md` section 6.4). |
| `NaN` and `Infinity` | Not JSON. No validator ever sees the document. |
| Canonical form | About bytes, not structure. |
| Everything about `.vnz` | An archive is not a JSON document. |

## What it deliberately permits

`additionalProperties` is left open everywhere on purpose. `SPEC.md` section 6.2
makes an unrecognised key on an object a reader diagnostic that keeps the item,
so a schema refusing them would disagree with every conforming reader. The same
applies to `data` record bodies, which are opaque by design — constraining them
would break the extensibility the bag exists for — and to the `rulesets` block,
which a 0.1 reader preserves verbatim and interprets not at all (section 3.9).

## Where the schema and a reader disagree

The corpus records a `schema` verdict per case, so the disagreements are visible
rather than assumed. Two kinds exist and both are correct:

- `reject/version-too-new.vnx` is schema-valid and reader-refused. Only a reader
  knows what it implements.
- Most diagnose files are schema-valid and diagnosed: an unrecognised key, a
  dangling link, a chain that does not join. `diagnose/polygon-second-ring.vnx`
  and `diagnose/timestamp-with-offset.vnx` are the two the schema does catch.

## Running it

```sh
cd go && go test ./internal/vnxschema/
```

That test lives in `internal/vnxschema` rather than in `pkg/vnx` because a JSON
Schema validator is a third-party dependency, and `pkg/vnx` imports the standard
library and nothing else. It is the reference implementation, and a dependency-
free build is the property everyone reimplementing this format relies on.

The reason used to be sharper than the enforcement: the standard-library guard
skipped `_test.go` files, so putting this test in `pkg/vnx` would have compiled
and passed while quietly making the library undeliverable. Since 2026-09-06 the
guards read test files too, so the same mistake now goes red in the package that
made it.
