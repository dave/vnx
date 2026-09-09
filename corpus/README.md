# The VNX conformance corpus

Every implementation runs these same files. The corpus is the only defence
against independent libraries quietly disagreeing with each other, and it cannot
work retroactively, which is why it exists before the second library does.

`SPEC.md` section 8 defines what the corpus requires.

## Layout

```
accept/    must be read, with zero diagnostics
reject/    must be refused, and the refusal must name its cause
diagnose/  must be read, reporting exactly the listed diagnostics
format/    messy input, plus exactly the bytes a formatter must emit for it
schema/    the JSON Schema, and what it can and cannot check
tools/     the generator for the .vnz archives
```

The schema lives here rather than beside the Go implementation because every
case carries a `schema` verdict in its sidecar. A sidecar field that only one
implementation can check is a corpus that cannot be run.

Each `foo.vnx` or `foo.vnz` has a `foo.expect.json` beside it. A file with no
sidecar is a failure, not a skip: the file everyone assumes is covered is
otherwise exactly the one that is not.

## The sidecar

```jsonc
{
  "kind": "accept" | "reject" | "diagnose",   // must match the directory
  "note": "why this case exists",             // required

  "counts":    {"waypoints": 1, "tracks": 1}, // exact, per collection
  "positions": [{"at": "/networks/0/arcs/arc-1/pips/0", "lon": -3.6, "lat": 57.1}],
  "tracks":    [{"name": "T", "moving_time_ms": 167000}],  // null = unknown
  "attachments": ["photos/summit.jpg"],       // .vnz only

  "refusal":     "substring of the expected error",        // reject only
  "diagnostics": [{"path": "/waypoints/0/nmae", "message": "unknown key"}],
  "cannot_detect": "why no diagnostic is possible"         // documented limitation
}
```

Three fields do work that is not evident from their names.

`counts` stops an accept file passing vacuously. A decoder that returned an empty
document for every input would otherwise satisfy "no error, no diagnostics" on
all of them.

`positions` is the only available defence against a longitude and latitude
transposition. Both orderings parse, both are in range, and nothing structural
can tell them apart while the point lands 8,000 km away. The values are JSON
numbers in the sidecar, so the comparison is between doubles rather than between
formatted text, which is also what makes the full-precision case work.

`at` is a JSON Pointer in the wire's own key names, and it addresses every kind
of position the format carries:

```
/waypoints/0/at                     /networks/0/nodes/node-1
/areas/0/polygon/0                  /networks/0/arcs/arc-1/pips/0
/routes/0/arcs/0/pips/0             /networks/0/chains/0/links/1/to
/tracks/0/segments/0/pips/0         /networks/0/points/0/at
                                    /networks/0/areas/0/polygon/0
```

Resolve it against your decoded model, never against the raw JSON. Resolving it
against the file compares the file to itself and cannot see a decoder that
swapped two channels, which is the entire point of the check.

Until 2026-09-09 this field named waypoints only, so a reader that transposed
longitude and latitude on every arc pip, node, area vertex and track pip passed
the whole corpus — `accept/lonlat-transposition.vnx` included, since it holds a
waypoint and nothing else. `accept/example-network.expect.json` now pins one
position of every kind. Do not narrow that list.

`moving_time_ms: null` means unknown, which is not zero. A reader that answers 0
for a track carrying no pip times has answered a question it cannot answer, and
its answer is indistinguishable from a track on which nobody moved.

`message` and `refusal` are matched as substrings. Exact wording is a reader's
own business; naming the cause is not.

Diagnostics are compared as a set, not by position. `SPEC.md` section 6.3 makes
emission order insignificant: a reader that resolves cross-references in a second
pass emits its link diagnostics after its key diagnostics, and is correct. Each
expected diagnostic has to be paired with one distinct reported diagnostic, so a
runner that let one reported diagnostic satisfy two expectations would pass a
reader that found half the defects.

## Accept files are canonical

Every `accept/*.vnx` is stored in canonical form (`SPEC.md` section 4.5). The
runner decodes it, RE-RENDERS the decoded document, and requires the original
bytes back.

Re-render, not write, and not reformat-the-bytes. A writer rounds coordinates at
the emit boundary and emits the shortest spelling of a position, so writing what
you read is legitimately not what you read. And reformatting bytes into bytes
never builds a model, so it cannot see a reader that threw away what it read:
measured 2026-09-09, a reader discarding every data record, every ruleset, every
style and every `notes` passed this tier green under a bytes-only check. Your
model has to keep a position's arity for this to work — see section 4.1.

That check is the byte-compatibility guarantee between the libraries. Every
conforming writer, in every language, emits these exact bytes for a given
document, so a Go export, a Swift export and a Python export of one library are
identical files or one of them is wrong. An implementation whose formatter
differs in any respect fails here, which means nobody has to trust anyone's
reading of the specification's prose.

The layout, in one place:

- Two-space indent, UTF-8, no BOM, newline at end of file.
- Object keys: scalars first in lexicographic order, then containers in
  lexicographic order, classified by the value in this object.
- Arrays are never reordered.
- An array holding only numbers and `null` is written on one line, elements
  separated by `, `. That is every position in the format. `null` counts, so a
  position does not change shape when an elevation goes missing. Six lines per
  position would make a day's recorded track fifty thousand lines; this makes it
  ten.
- Numbers render with ECMAScript `Number::toString` semantics — what
  `JSON.stringify` emits and what Go's `encoding/json` reproduces — with
  negative zero collapsed to `0`.

No stock JSON encoder can emit this layout in any of the target languages,
measured 2026-09-01, so every implementation writes its own serialiser. The key
ordering alone rules the stock encoders out: `sort_keys` and `.sortedKeys` give
plain lexicographic order, which puts `networks` before `vnx`. The
inline-position rule therefore costs nothing extra. Numbers are the other trap:
Python and Swift both emit `340.0`, `1e-06` and `1e-07` where the reference emits
`340`, `0.000001` and `1e-7`, and Apple's `JSONSerialization` renders `-3.67` as
`-3.6699999999999999` and must not be used. Only Go and JavaScript can lean on
their built-in number formatting. `SPEC.md` section 4.5.6 has the table.

The canonical check does more than police layout. It is what proves that a whole
document — including data records of types no implementation recognises, whose
bodies nothing else inspects — survives parse and re-render at value level.

Regenerate a file after editing it:

```sh
cd go && go run ./cmd/vnx fmt --write ../corpus/accept/<file>.vnx
```

That command is run from the repository root and needs Go. The corpus itself
does not, which is the point of it.

## Rebuilding the archives

The `.vnz` files are committed as binaries, because an implementation in another
language cannot be asked to build a Go program before it can run the corpus.
`tools/makevnz` is committed beside them so the bytes stay reproducible and
reviewable:

```sh
cd corpus && go run ./tools/makevnz
```

It writes its output relative to the working directory, so that directory has to
be `corpus/`. The `go.mod` beside this file exists only so the go tool will run
it at all; see the comment in it.

## Why `format/` exists separately

The accept files cannot catch a formatter defect. They are stored canonical, so
canonicalising one only ever asks the renderer about values already spelled
correctly. A writer that renders negative zero as `-0`, or `0.000001` as
`1e-06`, is never asked the question. The reference implementation had exactly
the negative-zero defect and its own corpus was blind to it.

The `format/` inputs are therefore written in the wrong spellings on purpose, and
each case asserts that its input is not already canonical, so it cannot pass
vacuously.

## The tolerance cases

`accept/inline-endpoint-within-tolerance.vnx` and
`diagnose/inline-endpoint-beyond-tolerance.vnx` are the only cases that exercise
the 50 m projection tolerance at any distance. Until they existed the corpus
could not distinguish four plausible distance models from each other: the worked
example's inline endpoint sits about 20 m from its arc, where those four models
read 19.81 m, 20.34 m, 25.52 m and 36.47 m and all four pass.

Both arcs run NORTH-SOUTH over three degrees, and that shape is the point.
`SPEC.md` section 3.3.3 takes the cosine of the QUERY POINT's latitude and holds
it constant for every vertex. A model that instead takes the cosine of each
segment's start latitude reads these endpoints 8.5% further away. On an
east-west arc the two agree to within 0.06%, so the obvious fixture shape would
have discriminated nothing.

## What the corpus cannot check

`diagnose/duplicate-object-key.vnx` expects no diagnostic. Two `n1` entries in
one `nodes` object resolve last-wins in every mainstream parser, before any VNX
code runs, so a node vanishes and a standard parser offers no way to know
(`SPEC.md` section 6.4). The limitation is documented rather than papered over.
If a reader ever gains the ability to see it, that file goes red and the
limitation gets revisited instead of quietly outliving its cause.

The corpus also cannot check a field it does not look at. Its expectations are
`counts`, `positions`, `tracks`, `attachments`, `refusal` and `diagnostics`;
everything else in a document is compared only through the canonical-form check,
which compares bytes to bytes and therefore agrees with whatever the fixture
already says. Two consequences are live today:

- A `data` record body is opaque to the decoder, so a record's fields are
  compared against nothing at all. `accept/example-network.vnx` writes
  `app.viatrail.camp` with integer fields, which is right — but nothing here
  could have told you so, and the internal design document said booleans until
  the fixture was ruled the accurate source.

That is not a defect in a fixture. It is a field the corpus does not look at,
and a second implementation reading a fixture infers its shape as though it were
the format. Coordinates outside a waypoint were the same hole until 2026-09-09;
`positions` now reaches all of them.

## Editing a case

Do not make a red corpus file green by editing the fixture. The fixture is the
instrument.
