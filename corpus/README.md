# The VNX conformance corpus

The corpus decides whether an implementation is correct: every implementation
runs these same files, and an implementation ships when it passes them.

## Layout

```
accept/    a document a reader must read, with zero diagnostics
reject/    a document a reader must refuse, with the refusal naming its cause
diagnose/  a document a reader must read, reporting exactly the listed diagnostics
write/     an input document, and the document a conforming writer produces from it
tools/     the generator for the committed .vnz archives
```

The four tiers hold fixtures and share the sidecar described below.

This list, the one in the repository root README and `SPEC §8` have to agree
with the tree and with each other.

## Terminology

| Term | Meaning |
|---|---|
| Fixture | The input file of a case: a `.vnx` document or a `.vnz` archive. |
| Sidecar | The `foo.expect.json` beside `foo.vnx` or `foo.vnz`, holding the expected result. |
| Case | One named unit of the corpus: a fixture and its sidecar in a tier. |
| Tier | A directory of cases sharing one operation and one requirement, listed above. |
| Runner | The program an implementation writes to walk the corpus and compare its library's behaviour against each sidecar. |

## The sidecar

Every fixture has a sidecar beside it. A fixture with no sidecar is a failure of
the run, not a skip.

| Field | Where | Meaning |
|---|---|---|
| `kind` | every case | `accept`, `reject`, `diagnose` or `write`; must match the directory |
| `note` | every case | what this case separates, and which of its values are load-bearing |
| `counts` | document tiers | the exact number of entries in each collection, and in each nested container |
| `positions` | document tiers | pinned coordinates, each addressed by an `at` JSON Pointer |
| `tracks` | document tiers | per track, looked up by name: `moving_time_ms`, or `null` for unknown |
| `root` | `.vnz` cases | the entry name of the root document |
| `attachments` | `.vnz` cases | the entry names the archive carries beside the root document |
| `attachment_contents` | `.vnz` cases | each attachment's bytes, as UTF-8 text, keyed by entry name |
| `refusal` | `reject/` | a substring of the expected refusal message |
| `diagnostics` | `diagnose/`, `write/` | the expected diagnostics, compared as a set; in `write/`, what decoding the input must report, and absent means none |
| `cannot_detect` | any case | why no diagnostic is possible, where a case expects none |
| `schema` | `.vnx` cases | `valid`, `invalid` or `not-json`: the verdict `schema/vnx-0.9.schema.json` reaches. Not a reader's business — a runner reads past it |

```jsonc
{
  "kind": "accept",                           // must match the directory
  "note": "why this case exists",             // required

  "counts":    {"waypoints": 1, "tracks": 1}, // exact, per collection
  "positions": [{"at": "/networks/0/arcs/arc-1/pips/0", "lon": -3.6, "lat": 57.1}],
  "tracks":    [{"name": "T", "moving_time_ms": 167000}],  // null = unknown
  "attachments": ["photos/summit.jpg"],       // .vnz only
  "attachment_contents": {"photos/summit.jpg": "…"},  // .vnz only

  "refusal":     "substring of the expected error",        // reject only
  "diagnostics": [{"path": "/waypoints/0/nmae", "code": "unknown-key",
                   "message_advisory": "unknown key"}],
  "cannot_detect": "why no diagnostic is possible",

  "schema": "valid"                           // the JSON Schema's verdict, not a reader's
}
```

`schema` is the one field an implementation runner does NOT compare. It records
what `schema/vnx-0.9.schema.json` says about the fixture, which is a claim about
the schema rather than about any library, and `go/internal/vnxschema` is what
asserts it. A runner that fails on fields it does not read — Python's and
Kotlin's both do, deliberately — must name this one as known and skip it rather
than drop the guard.

Three fields do work that is not evident from their names.

`counts` stops an accept case passing vacuously (`SPEC §8.1`). A decoder that
returned an empty document for every input satisfies "no error, no diagnostics"
on all of them.

It carries two kinds of key. A **bare name** is one of the six top-level
collections. An application key (`SPEC §3.9`) is not a collection and has no
entry in `counts`: the tree comparison of an accept case is what checks it. A name **starting with `/`** is a container nested inside one,
addressed by the same JSON Pointer vocabulary `positions` uses —
`/networks/0/nodes`, `/routes/0/arcs/arc-2/pips`, `/tracks/0/segments`,
`/networks/0/highlights/2/walk`, `/areas/0/polygon`.

⚠ **THE NESTED KEYS ARE WHAT THE `diagnose/` TIER HAS INSTEAD OF A ROUND TRIP.**
`SPEC §8.1` exempts `reject/` and `diagnose/` from the decode-then-encode
comparison, and `diagnose/` is exactly where a reader is required to drop
something in a SCOPED way: one area of two, one segment of two, one highlight of
two. Before these keys existed nothing pinned what SURVIVED, and an over-broad
drop, such as a dropped arc taking every highlight in its network with it, passed
the whole corpus green.

⚠ **AN ARC OR NODE CONTAINER IS KEYED BY ITS OWN KEY, NEVER BY AN INDEX**:
`/networks/0/arcs/arc-1/pips`. `SPEC §3.3` makes `arcs` and `nodes` objects,
and an object has no order, so an index there would export one implementation's
iteration order as a conformance requirement.

⚠ **A DIAGNOSE CASE RECORDS WHAT THE READER ENDS UP WITH, NOT WHAT THE FILE
HOLDS.** `diagnose/polygon-second-ring.vnx` carries two areas and its sidecar
records one, because the nested-ring area is dropped whole. Read the case's
`note` before changing a number: the note states the rule that decides it.
A count of `0` is legal and sometimes load-bearing:
`diagnose/self-loop-arc.expect.json` pins `/networks/0/highlights: 0` because the
network's only highlight walks the dropped arc.

`positions` is the only available defence against a longitude and latitude
transposition (`SPEC §8.1`). Both orderings parse, both are in range, and
nothing structural separates them while the point lands 8,000 km away. The
expected values are JSON numbers, so the comparison is between doubles rather
than between formatted text, which is also what makes the full-precision case
work.

`moving_time_ms: null` means unknown, which is not zero (`SPEC §3.5`). Unknown
covers a track that is partly timed as well: moving time is a number only when
every segment carries a time on its first and last pip.

### The `at` pointer

`at` is a JSON Pointer in the wire's own key names, and it addresses every kind
of position the format carries:

```
/waypoints/0/at                     /networks/0/nodes/node-1
/areas/0/polygon/0                  /networks/0/arcs/arc-1/pips/0
/routes/0/arcs/arc-1/pips/0         /networks/0/highlights/0/walk/0/from
/tracks/0/segments/0/pips/0         /networks/0/points/0/at
                                    /networks/0/areas/0/polygon/0
```

Resolve it against the decoded model, never against the raw JSON. Resolving it
against the file compares the file to itself, which cannot see a decoder that
swapped two channels.

`accept/example-network.expect.json` pins one position of every kind, and that
breadth is the point: a list naming waypoints only leaves a transposition on
every arc pip, node, area vertex and track pip unchecked. Keep the list wide.

## Write a runner

1.  Walk each tier directory and pair every fixture with its sidecar. A fixture
    with no sidecar fails the run.
2.  Perform the tier's operation: decode for `accept/`, `reject/` and
    `diagnose/`; decode-then-write for `write/`. Check the outcome against the
    tier's requirement.
3.  Resolve every sidecar expectation by something the document says, never by
    position. Look a `tracks` entry up by `name`, a `positions` entry up by its
    `at` pointer, and a `counts` entry up by its collection key or pointer. An
    index lookup
    finds *a* track at position 0 and compares against the wrong one, which
    passes a sidecar that has drifted from its fixture. An expectation naming
    something the document does not contain fails the case.
4.  Match a diagnostic on its `path` and its `code`, and on nothing else. The
    English is advisory and `SPEC §8.1` forbids comparing it, which is why the
    field is spelled `message_advisory`.
5.  Compare the `diagnostics` list as a set, not by position: `SPEC §6.3` makes
    emission order insignificant. Pair each expected diagnostic with one
    **distinct** reported diagnostic, or a reader that found half the defects
    passes.
6.  Fail the case, rather than narrowing the comparison, on each of these:
    - an expectation carrying no `code` — `SPEC §6.3` requires one on any
      diagnostic a corpus case pins, so a missing one is a malformed sidecar;
    - a sidecar carrying a bare `message` key — it is a sidecar written against
      a shape this corpus does not use, or a runner about to compare English;
    - a reported diagnostic with the right path, the right English and the wrong
      `code`. If that passes, the runner is matching on path alone and every
      `code` in every sidecar is decoration.

    Sweep the whole sidecar structure for these, not its first level: a `code`
    inside `diagnostics` is invisible to a top-level scan.
7.  Check that the reader's own output is well formed, with no sidecar
    consulted. Group a run's reports by `path`; fail where one path carries more
    than one distinct `code`, and fail where one path carries more than one
    report and any of them has no code. `path` and `code` are the only two
    fields a conforming runner may compare, so two reports agreeing on both are
    the same report twice, and two codeless reports at one path are
    indistinguishable in the same way. A reader that always emits codes
    satisfies this for free.
8.  Red-proof the runner before trusting a green. Break the library
    deliberately, and confirm that the case you expect goes red and that it is
    the **only** one. Change one reported `code` and require the run to fail.
    Prefer a mutation that reddens exactly one case: a mutation that reddens
    three cannot say which rule it broke.

Step 7 is a different kind of check from steps 3 to 6, and a runner author who
has written those will not think to write it. Every other check compares a
reader against an expectation. This one asks whether the reader's own output is
well formed, and no fixture can prompt it.

## What each tier compares

### The accept tier's round-trip check

For every `accept/*.vnx` the runner decodes the file, encodes the decoded
document, and compares the two as JSON trees: the tree parsed from the original
file against the tree parsed from the encoded output. `SPEC §8.1` permits two
normalisations and no others.

The comparison is over trees rather than bytes because the bytes of a document
are not constrained. Key order, number spelling and whitespace are each the
writer's own business, and two implementations using their own standard JSON
libraries differ in all three while carrying the same document.

Compare against the tree parsed from the **file**, never one decoded model
against another. A reader that discarded every data record, every application key,
every style and every `notes` field produces a model that compares equal to itself and
passes this tier on the sidecar assertions alone. The tree comparison is what
proves a reader kept what it read, including data records of types no
implementation recognises.

Decode the bytes; do not reformat them. Reformatting bytes into bytes never
builds a model, so it cannot observe anything about what a model kept.

### The ZIP64 archive

`accept/container-zip64.vnz` is hand-built, and four of its properties look like
things to tidy. Each is the reason the case works.

**Where the masking is applied.** Its local headers carry true values, and its
central directory masks compressed size, uncompressed size and local-header
offset to `0xFFFFFFFF`, moving all three into a ZIP64 extended-information extra
field. Masking the local header instead — which is what a stock library's
`force_zip64` does — leaves the central directory holding real 32-bit values,
and almost every reader takes its sizes and offsets from there, so such a
fixture asks the extra-field parser nothing and stays green with that parser
disabled. Do not regenerate this archive with a stock zip library either: a
library that emits ZIP64 only when a value overflows 32 bits declines to emit it
at this size and hands back a plain archive. `tools/makevnz` assembles the file
field by field instead.

**Two independent gaps, and a red does not say which.** Red-proof them
separately: disable the central-directory extra-field parser, and separately
ignore the ZIP64 end-of-central-directory record. That record's locator is the
only way to find the directory here, because the plain end record masks its
offset, its size and its entry counts.

**Entries are stored rather than deflated.** Deflate output differs between
compressors, and this archive is written by hand, so a deflated entry would make
its bytes reproducible in one language only. The archive is verified against
three independent readers — Python's `zipfile`, Go's `archive/zip` and `unzip` —
because a hand-built archive that only its author can read proves nothing.

**The root document is deliberately not the first entry.** The dangerous reader
is the one that implements the extra field wrongly and produces `0`; with the
document at local-header offset 0, that reader finds it anyway and looks
correct. (A reader with no extra-field parser at all gets `0xFFFFFFFF` and dies
loudly, so it was never the risk.) `attachments/first.txt` exists to push
`doc.vnx` off zero, and moving it after the document, or deleting it, removes
the property: with the extra field ignored and a lost offset defaulting to zero,
`doc.vnx` reads as 0 bytes rather than as itself.

This archive is input rather than an example of what a writer here produces.
`SPEC §7` requires a reader to read ZIP64; `SPEC §7.1` leaves writing it to the
writer and no implementation in this repository writes it, which is why the file
has to be hand-built.

## Known limitations

A green run is not proof. These are the gaps, and a runner author who does not
know them will read a green run as more than it is.

**A green `diagnose/` tier is not evidence that a runner compares codes.**
Weakening a matcher to compare `path` alone leaves the whole tier green, because
every case here carries diagnostics whose paths identify them, and a correct
reader satisfies a path-only matcher and a code-comparing matcher alike. No
fixture can close that: a fixture can only state a correct expectation, and the
disagreement between the two matchers appears only against a reader that is
wrong. The self-test in step 8 is the author's own, and it is not optional.

**No case exercises the path-and-code uniqueness rule of step 7.** Measured over
every sidecar in the corpus, not one expects two diagnostics at a single path,
so a runner that skipped that check would notice nothing. The rule is a property
of a reader rather than of any document.

**`refusal` is matched as a substring, which compares English across
implementations.** `SPEC §8.1` states it, and it is a known defect.
`reject/nan-literal` and `reject/infinity-literal` show the shape of it: they
pin the substring `malformed JSON`, which is an artefact of a strict parser
rather than of the format.

A strict parser refuses a `NaN` literal as a syntax error before any VNX rule
runs, and its message happens to match. A permissive parser — Python's
`json.loads` accepts `NaN`, `Infinity` and `1e400` by default — reaches the rule
of `SPEC §4.2` and refuses for exactly the right reason, in different words,
which do not match. So the correct reader is the one that fails.

Typed refusal codes are the fix and are not built. Until they are, do not narrow
a refusal message to make a case go green: that is the fixture editing the
implementation, backwards.

**`diagnose/duplicate-object-key.vnx` expects no diagnostic.** Two `n1` entries
in one `nodes` object resolve last-wins in every mainstream parser before any
VNX code runs, so a node vanishes and a standard parser offers no way to see it
(`SPEC §6.4`). If a reader ever gains that ability, the file goes red and the
limitation is revisited rather than outliving its cause.

**The corpus cannot check a field it does not look at.** Its expectations are
`counts`, `positions`, `tracks`, `attachments`, `refusal` and `diagnostics`.
Everything else in a document reaches the runner only through the accept tier's
tree comparison, which asks whether a value survived the round trip and never
whether it is the right value. A `data` record body is opaque to the decoder, so
a record's fields are compared against nothing at all. The records in a fixture are
examples of what a record can hold, not a definition of any type. This format
defines no record type, so do not infer a record's shape from a case.

### Where the coordinates are

Fifty-one positions are pinned, longitude -3.74 to -3.574, latitude 57.0118 to
90. Measure it:

```sh
cd corpus && python3 -c "
import json,glob
ps=[p for f in glob.glob('*/*.expect.json') for p in json.load(open(f)).get('positions',[])]
print(len(ps), min(p['lon'] for p in ps), max(p['lon'] for p in ps), min(p['lat'] for p in ps), max(p['lat'] for p in ps))
"
```

Not one channel of one pinned position is zero, every longitude is negative and
every latitude is northern. Three things therefore sit outside this corpus: a
decoder that turned `0.0` into `-0.0` passes every tier, and no fixture pins a
zero in any channel, so nothing here can see it; a sign error that shows only on
a positive longitude or a southern latitude is untested; and nothing goes near
the antimeridian.

That is recorded rather than closed, deliberately. A coordinate moved or added
to give a test something to bite on is the failure this corpus keeps catching: a
case earns its place by what it separates. One of the three is load-bearing
where it stands — `accept/lonlat-transposition.vnx` works because at 57 degrees
north and 3 degrees west both numbers are in range for both channels, which is
what makes the transposition invisible to a range check. Move that fixture and
the case stops testing what it was built for.

The absence is frozen: `js/test/redproof.test.js` asserts that the set of pinned
zero coordinates is empty, and fails with "a fixture now pins a zero coordinate;
assert the failure instead of the gap". A later phase that closes the gap with a
real document is told to convert the absence into an assertion.

### Stating a limitation carefully

"No tier catches this" is a green-asserting claim: it stops being true without
anything going red. Two such claims about this corpus were retired, each by
somebody building the thing the claim called impossible — that nothing past the
writer boundary was reachable by a tier, retired by a tier that decodes and then
writes; and that a ZIP64 archive small enough to commit could not exercise an
extra-field parser, retired by masking the central directory instead of the
local header. Each was true of the corpus as it stood. Neither was a property of
the format.

> An unreachability claim is a claim about the current state of someone's
> ingenuity.

The working rule is narrower and more useful: **a property is reachable by the
accept tier whenever the two readings put a different value in the tree.** The
shortest spelling of a position is not, because the accept tier's normalisation
shortens both sides alike; it needs the `write/` tier.

So a test in an implementation that concludes something is unreachable asserts
nothing the moment the corpus grows, and nothing will tell it. Re-run those
tests when cases land, and prefer a proof that names what it expects to be
**red** over one that expects everything to be green.

## Rebuild the archives

```sh
cd corpus && go run ./tools/makevnz
```

It writes its output relative to the working directory, so that directory has to
be `corpus/`. The `go.mod` beside this file exists so the go tool will run it at
all; see the comment in it.

Run it when a `.vnz` fixture's source document changes. It writes every `.vnz`
in the corpus, including `accept/container-zip64.vnz`, which it assembles field
by field rather than through `archive/zip`.

The `.vnz` files are committed as binaries, because an implementation in another
language cannot be asked to build a Go program before it can run the corpus.
`tools/makevnz` is committed beside them so the bytes stay reviewable — and
reviewable is the claim, not reproducible byte for byte. Deflate has no
canonical output, so review these files by reading `tools/makevnz` rather than
by regenerating them and diffing.

These archives are input, and `SPEC §7` requires a reader to accept deflate, to
accept the data-descriptor bit, and to take its sizes from the central
directory. They are the only cases that test those rules. Regenerating them with
another tool would delete the corpus's only deflate coverage and its only
data-descriptor coverage, and every implementation would pass.

## Add or edit a case

A note earns its place by saying what the fixture separates. That is a different
claim from what the fixture contains, and the second is worth nothing on its
own: a construct present in a case that no expectation names is not covered by
it.

**A claim that part of a fixture is decorative is unmeasured until a table
exists.** Such a sentence gets written while explaining why a value was added,
and it reads to the next person as permission to delete the value.
`write/emit-boundary` carries two elevation ties, one of which looks redundant
beside the other; against the five wrong tie rules an implementation reaches
for, they catch different pairs, and only both together identify
round-half-to-even. So a note saying a value is `only` there for something, or
is `not` the point, or exists `merely` to prevent something, is a claim about
discrimination that nobody has checked. Either produce the table and put it in
the note, or drop the claim.

The same applies to an absence. A fixture built carefully and missing an obvious
case reads as an oversight, and the next person adds it. Where a case is left
out deliberately, the note says why it adds nothing: `write/emit-boundary`
carries no negative rounding tie because every wrong rule is separated by one of
its two positive values.

When editing an existing case, do not make a red corpus file green by changing
the fixture. The fixture is the instrument. Read the note first: several values
that look arbitrary are not, and at least one that looks redundant beside its
neighbour is the only one that decides anything.
