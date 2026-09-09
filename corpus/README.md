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
numbers/   what every implementation must emit for a given double
schema/    the JSON Schema, and what it can and cannot check
tools/     the generator for the .vnz archives
```

The first four are directories of documents and share the sidecar described
below. `numbers/` is a table rather than a directory, because the properties it
pins are per-value rather than per-document; it has its own README and no
sidecars. It is a tier all the same, and it is the only one that reaches past
the boundary where a writer turns a model into a document.

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
  "diagnostics": [{"path": "/waypoints/0/nmae", "code": "unknown-key",
                   "message_advisory": "unknown key"}],
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

`accept/example-network.expect.json` pins one position of every kind, and that
breadth is the point. A list naming waypoints only leaves a transposition on
every arc pip, node, area vertex and track pip unchecked — including in
`accept/lonlat-transposition.vnx`, which holds a waypoint and nothing else, so
the file named for this defect cannot catch it anywhere else. Do not narrow the
list.

`moving_time_ms: null` means unknown, which is not zero. A reader that answers 0
for a track carrying no pip times has answered a question it cannot answer, and
its answer is indistinguishable from a track on which nobody moved.

Unknown also covers a track that is only partly timed. Moving time is a number
only when every segment carries a time on its first and last pip; otherwise it
is unknown, and a sum over the segments that happen to be timed is the worse
answer of the two the format forbids. Zero at least reads as suspicious. A
partial sum is indistinguishable from a complete one, and no caller can tell
which it was handed. `accept/track-partly-timed.vnx` is the only case that
separates the two readings — every other track here is wholly timed or wholly
untimed, and it was that uniformity that let three implementations ship the
partial sum with every tier green.

A diagnostic is matched on its `path` and its `code`. **The English is not
compared at all**, which is why the field is called `message_advisory` rather
than `message`: it is there so a person reading the sidecar can see what the
case is about, and a field named `message` that nothing compares reads as a
claim and is not one. Half-coupling is worse than none, because it looks like it
is there.

`refusal` is still matched as a substring. That is the remaining place where
this corpus compares English across implementations, and it is a known defect.

One of the six wordings shows the shape of it. `reject/nan-literal` and
`reject/infinity-literal` pin the substring `malformed JSON`, and that wording
is an artefact of a STRICT parser rather than of the format. A strict parser
refuses a `NaN` literal as a syntax error, before any VNX rule has run, and the
expected substring happens to match its message. A permissive parser accepts the
literal, reaches section 4.2's own rule and refuses for exactly the right
reason — in different words, which do not match, so the correct reader is the
one that fails.

Which seat this bites was measured by the JavaScript seat rather than assumed,
and the answer is not the one the wording suggests: Python's own `json.loads`
accepts `NaN`, `Infinity` and `1e400` by default, so Python is where a reader
reaches the format's rule and refuses in its own words. Two libraries currently
pass by wording their syntax error to match a substring, which is agreement
about a string rather than about the format.

Typed refusal codes are the fix and are not built yet. Until they are, do not
narrow a refusal message to make a case go green — that is the fixture editing
the implementation, backwards.

Diagnostics are compared as a set, not by position. `SPEC.md` section 6.3 makes
emission order insignificant: a reader that resolves cross-references in a second
pass emits its link diagnostics after its key diagnostics, and is correct. Each
expected diagnostic has to be paired with one distinct reported diagnostic, so a
runner that let one reported diagnostic satisfy two expectations would pass a
reader that found half the defects.

## Writing a runner

A matcher that checks less always passes. Every rule in this section exists
because a runner degraded quietly to a weaker comparison and stayed green, and
four implementations hit the same one on the same day.

When the sidecars moved from pinning English to pinning `code`, every runner
written against a `message` field stopped comparing anything but paths. None of
them went red. A runner that looks for a field, does not find it, and compares
what is left has not failed — it has narrowed, and a narrower comparison passes
more. So:

- **An expectation carrying no `code` is a failure of the case**, not a
  path-only match. Section 6.3 requires a `code` on any diagnostic a corpus case
  pins, so a missing one is a malformed sidecar and the run must say so.
- **A sidecar carrying a bare `message` is a failure**, not a field to ignore.
  The advisory field is `message_advisory` precisely so that nothing compares
  it; a key spelled `message` is either a sidecar written against the old shape
  or a runner about to compare English, and both need a person.
- **A case with the right path, the right English and the wrong `code` must go
  red.** If it does not, the runner is matching on path alone and every code in
  every sidecar is decoration.

**A nested expectation needs a nested sweep.** The guard that should have caught
the first failure looked only at the top level of each sidecar, so a `code`
inside `diagnostics` was invisible to it — the guard shared the defect it was
guarding against. Sweep the structure, not its first level.

Two more, from the same family:

- **Pair each expected diagnostic with one DISTINCT reported diagnostic.** A
  runner that lets one report satisfy two expectations passes a reader that
  found half the defects.
- **Assert that each `format/` input is not already canonical.** Without it a
  formatter that echoed its input passes the whole tier.

Red-proof the runner itself before trusting a green: break the library
deliberately and confirm the case you expect goes red, and that it is the ONLY
one. A mutation that reddens three cases cannot tell you which rule you broke —
`accept/track-partly-timed.expect.json` records a proof that had to be narrowed
twice before it meant anything.

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

Both arcs are meridians spanning thirteen degrees of latitude, and every part of
that is deliberate.

A meridian makes the projected segment vertical in every model, so the distance
is exactly the longitude offset times whatever scale the model uses — no model
can argue about the arithmetic, only about the scale, which is the thing under
test. Thirteen degrees is what makes a wrong scale flip a VERDICT rather than
move a number. At three degrees, a segment-start-cosine model reads the accept
case at 48.8 m against a 50 m tolerance: the number moves by 8.5% and the
verdict does not, so the pair discriminates against nothing. A corpus case is a
verdict.

The two arcs run in OPPOSITE directions, and that is the least obvious property
here. The accept case catches a model that over-reads and refuses a legal
document. A refusal case cannot catch that — over-reading also refuses — so the
diagnose case's arc descends, putting the segment start at high latitude and the
query at low, where a segment-start-cosine model under-reads and ACCEPTS a
document the format refuses. That is the more dangerous direction, and nothing
else in the corpus covers it.

Do not make the two files consistent with each other. The inconsistency is the
instrument.

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
the format.

### Saying that carefully

"No tier catches this" is a green-asserting claim, and it stops being true
without anything going red. Three such claims about this corpus were retired in
a single day, each by somebody building the thing the claim said was impossible:
that no fixture could carry a number tie, retired by `format/number-tie`, whose
input spells a value one way and whose expected output spells it the other; that
nothing past the writer boundary was reachable by a tier, retired by a tier that
decodes and then writes; and that a ZIP64 archive small enough to commit could
not exercise an extra-field parser, retired by masking the central directory
instead of the local header.

Every one of those was true of the corpus as it stood. None was a property of
the format.

> An unreachability claim is a claim about the current state of someone's
> ingenuity.

The working rule that survives is narrower and more useful: **a value-level
property is reachable by a re-render whenever the two readings produce different
TEXT for the same value.** Rounding is not, because a re-render never rounds.
A rendering tie is, because a re-render always re-spells.

So if a test in an implementation concludes that something is unreachable, that
test asserts nothing the moment the corpus grows, and nothing will tell it. Re-run
those tests when cases land, and prefer a proof naming what it expects to be RED
over one that expects everything to be green.

## Writing a case's note

A note earns its place by saying what a fixture separates. That is a different
claim from what the fixture contains, and the second is worth nothing on its
own: a construct present in a case that no expectation names is not covered by
it.

**A claim that part of a fixture is decorative is unmeasured until a table
exists.** Nobody writes that sentence after measuring; it is what gets written
while explaining why a value was added, and it reads to the next person as
permission to delete the value. `write/emit-boundary` carries two elevation
ties, one of which looks redundant beside the other. Against the five wrong tie
rules an implementation reaches for, they catch different pairs, and only both
together identify round-half-to-even.

So a note that says a value is `only` there for something, or is `not` the
point, or exists `merely` to prevent something, is a claim about discrimination
that nobody has checked. Either produce the table and put it in the note, or do not make the
claim.

The same applies to an absence. A fixture built carefully and missing an obvious
case reads as an oversight, and the next person adds it. Where a case is left out
deliberately, the note says why it adds nothing. `write/emit-boundary` carries
no negative rounding tie because every wrong rule is already separated by one of
its two positive values.

## Editing a case

Do not make a red corpus file green by editing the fixture. The fixture is the
instrument.
