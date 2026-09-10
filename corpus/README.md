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
write/     a document with a model to write, plus the document a writer produces
numbers/   the reference rounding of a given double to 6 and to 1 places
tools/     the generator for the .vnz archives
```

The first three hold documents and share the sidecar described below. `write/`
is a pair — `X.in.vnx` and `X.written.vnx` — and its operation is
decode-then-write, so it is the only document tier that applies the emit-boundary
rules of section 4.1: rounding, and the shortest spelling of a position.
`numbers/` is a table rather than a directory, because the properties it pins are
per-value; it has its own README and no sidecars.

This list, the one in the repository root README, and section 8 of `SPEC.md`
have to agree with the tree and with each other. They have disagreed before,
three ways at once, and nothing checks them.

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

**A `tracks` expectation resolves by NAME, never by index**, and so does every
other expectation that names the thing it is about. The reason is not style. A
sidecar that has drifted from its fixture — naming a track the document no
longer contains — is caught by a name lookup and sailed straight past by an
index lookup, which finds *a* track at position 0 and compares against the wrong
one. That has already happened here. An expectation naming something the
document does not contain is a failure of the case, not an expectation to skip.

`positions` resolves by its `at` pointer and `counts` by collection key, which
are the same rule: every expectation addresses its subject by something the
document says, so that a fixture and its sidecar cannot drift apart silently.

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

**And the corpus cannot tell an author whether they did any of this.** A green
`diagnose/` tier is not evidence that a runner compares codes. Weakening a
matcher to compare `path` alone leaves the whole tier green, because every case
here carries diagnostics whose paths already identify them, and a CORRECT reader
satisfies a path-only matcher and a code-comparing matcher alike. No fixture can
close that, because a fixture can only ever state a correct expectation; the
disagreement between the two matchers appears only against a reader that is
wrong, and the corpus contains no such reader.

So the self-test is the author's, and it is not optional: **break the library
deliberately and confirm the case you expect goes red, and that it is the only
one.** Change one reported `code` and require the run to fail. Four
implementations fixed the `message`/`code` failure above by hand, and the corpus
could not have told any of them whether they had succeeded.

**A path and a code together must identify one report.** No two diagnostics
about a single document may share both a path and a code, because
`path` and `code` are the only two fields a conforming runner may compare — so
two reports agreeing on both are, to a runner, the same report twice. A reader
that can emit two it cannot distinguish has a pointer that is not doing its job.

**And a diagnostic that shares a path with another MUST carry a code.** Without
that clause the rule passes the defect it was written for: two codeless
diagnostics at one path share `code` by both lacking it, so the invariant is
satisfied while the reports stay indistinguishable — which is precisely the shape
the reference implementation emitted for Section 3.3.2. Treating an absent code as
distinct from an absent code is not available either, because then every codeless
pair would violate and the rule would fire on documents where nothing is wrong.
So the pinning rule of Section 6.3 gains one condition: a code belongs to any
diagnostic a corpus case pins, AND to any diagnostic that shares a path with
another. A reader that always emits codes satisfies it for free.

Check it: group a run's reports by `path`; fail where one path carries more than
one distinct `code`, and fail where one path carries more than one report and any
of them has no code. That is mechanical, works over any document, and needs no
fixture.

**This is a different kind of check from the four above it, and a runner author
who has written those four will not think to write this one.** Every other check
in this file compares a reader against an expectation. This one asks whether the
reader's own output is well formed, with no expectation involved at all — the
sidecar is not consulted. Both kinds are required, and only the first kind is
what "running the corpus" sounds like.

**Which is fortunate, because no case here exercises it.** Measured over every
sidecar in the corpus: not one expects two diagnostics at a single path, so a
runner that skipped this check would never notice. The rule is a property of a
reader rather than of any document, and this is the second thing in this file
the corpus cannot check on a runner's behalf.

`diagnose/terminal-pip-off-node.vnx` is the case that comes closest, from the
wrong side: an arc with both terminals drifted produces two diagnostics, and a
reader naming the arc rather than the `from` and `to` keys emits them at one
identical path, separable only by their English — which nothing may compare. It
is why that case pins the field-level path.

Two more, from the same family:

- **Pair each expected diagnostic with one DISTINCT reported diagnostic.** A
  runner that lets one report satisfy two expectations passes a reader that
  found half the defects.
- **Assert that each `write/` input is not already the written form, and that
  the written form is a fixed point of the writer.** Without both, a writer that
  echoed its input passes the whole tier.

Red-proof the runner itself before trusting a green: break the library
deliberately and confirm the case you expect goes red, and that it is the ONLY
one. A mutation that reddens three cases cannot tell you which rule you broke —
`accept/track-partly-timed.expect.json` records a proof that had to be narrowed
twice before it meant anything.

## The accept tier's round-trip check

For every `accept/*.vnx` the runner decodes the file, encodes the decoded
document, and compares the two as JSON trees: the tree parsed from the original
file against the tree parsed from the encoded output. Only positions are
normalised before the comparison — trailing `null` channels dropped, and the
precision rules of section 4.1 applied to both sides. Nothing else may be
normalised away.

The comparison is over trees rather than bytes because the bytes of a document
are not constrained. Key order, number spelling and whitespace are each the
writer's own business, and two implementations using their own standard JSON
libraries will differ in all three while carrying the same document.

Compare against the tree parsed from the FILE, never one decoded model against
another. A reader that discarded every data record, every ruleset, every style
and every `notes` produces a model that compares equal to itself: measured
2026-09-09, such a reader passed this tier green on the sidecar assertions alone.
The tree comparison is what proves a reader kept what it read, including data
records of types no implementation recognises, whose bodies nothing else
inspects.

Do not reformat the file's bytes instead of decoding them. Reformatting bytes
into bytes never builds a model, so it cannot observe anything about what a model
kept.

## Rebuilding the archives

The `.vnz` files are committed as binaries, because an implementation in another
language cannot be asked to build a Go program before it can run the corpus.
`tools/makevnz` is committed beside them so the bytes stay reviewable:

```sh
cd corpus && go run ./tools/makevnz
```

It writes its output relative to the working directory, so that directory has to
be `corpus/`. The `go.mod` beside this file exists only so the go tool will run
it at all; see the comment in it.

**Reviewable, not reproducible byte for byte.** Deflate has no canonical
output: for the document in `accept/container-root-doc.vnz`, Go's
`compress/flate` emits 116 bytes and zlib emits 108, and no zlib level from 0 to
9 reproduces the committed bytes. Two conforming writers are not expected to
produce the same archive for the same document, so review these files by reading
`tools/makevnz` rather than by regenerating them and diffing.

These archives are input, and section 7 requires a reader to accept deflate, to
accept the data-descriptor bit, and to take its sizes from the central directory.
They are the only cases that test those rules. Regenerating them would delete
the corpus's only deflate coverage and its only data-descriptor coverage, and
every implementation would still pass.

## The ZIP64 case

`accept/container-zip64.vnz` is hand-built, and four of its properties look like
things to tidy. Each is the reason the case works.

Where the masking is applied. Python's `force_zip64` masks the local header only
and leaves the central directory holding real 32-bit values. Almost every reader
takes sizes and offsets from the central directory, so a fixture built that way
never asks the extra-field parser anything, and it stays green with that parser
disabled — which is how this gap was found, by red-proofing a ZIP64 test and
watching it pass. This archive does the reverse. Its local headers carry true
values, and its central directory masks compressed size, uncompressed size and
local-header offset to `0xFFFFFFFF`, moving all three into a ZIP64
extended-information extra field.

So do not simplify it by masking the local header instead, and do not regenerate
it with a stock zip library, which declines to emit ZIP64 for a 519-byte archive
and quietly hands back a plain one.

It catches two independent gaps and a red does not say which, so red-proof them
separately: disable the central-directory extra-field parser, and separately
ignore the ZIP64 end-of-central-directory record. That record's locator is the
only way to find the directory here, because the plain end record masks its
offset, its size and its entry counts.

Entries are stored rather than deflated. Deflate output differs between
compressors, and this archive is written by hand, so a deflated entry would make
its bytes reproducible in one language only. The archive was verified against
three independent readers — Python's `zipfile`, Go's `archive/zip` and `unzip` —
because a hand-built archive only its own author can read proves nothing.

This archive is input, not an example of what section 7.1 asks a writer to
produce. Section 7.1 says a conforming writer does not write ZIP64 records at
all, and section 7 says a reader must read them anyway. That asymmetry is why
the file has to be hand-built rather than produced by any library here.

The root document is deliberately not the first entry, and the reason is sharper
than "a lost value that happens to be correct is not a test". It is about which
defect survives review. A reader that never implements the extra field gets
`0xFFFFFFFF` and dies loudly, so it was never the risk. The dangerous reader is
the one that implements the extra field wrongly and produces `0` — and with the
document at local-header offset 0, that reader finds it anyway and looks
correct. `attachments/first.txt` exists only to push `doc.vnx` off zero, and
moving it after the document, or deleting it, removes the property. Measured:
with the extra field ignored and a lost offset defaulting to zero, `doc.vnx`
reads as 0 bytes rather than as itself.

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
everything else in a document reaches the runner only through the accept tier's
tree comparison, which asks whether a value survived the round trip and never
whether it is the right value. Two consequences are live today:

- A `data` record body is opaque to the decoder, so a record's fields are
  compared against nothing at all. `accept/example-network.vnx` writes
  `app.viatrail.camp` with integer fields, which is right — but nothing here
  could have told you so, and the internal design document said booleans until
  the fixture was ruled the accurate source.

That is not a defect in a fixture. It is a field the corpus does not look at,
and a second implementation reading a fixture infers its shape as though it were
the format.

### Where the coordinates are, and what that leaves untested

Measured over every sidecar at commit `c755069`: 19 pinned positions, longitude
-3.74 to -2.998942483187279, latitude 57.0704 to 67.5. Not one channel of one
pinned position is zero, every longitude is negative, and every latitude is
northern. So three things are outside this corpus. A decoder that turned `0.0`
into `-0.0` passes every tier, and negative zero has exactly one instrument
here, the number table in `numbers/`. A sign error that only shows on a positive
longitude or a southern latitude is untested. Nothing goes near the
antimeridian.

Recorded rather than closed, and deliberately. A coordinate moved or added to
give a test something to bite on is the failure this corpus keeps catching: a
case earns its place by what it separates, and none of these three has a
document behind it yet. One of them is also load-bearing where it stands.
`accept/lonlat-transposition.vnx` works because at 57 degrees north and 3
degrees west both numbers are in range for both channels, which is what makes
the transposition invisible to a range check. Move that fixture and the case
stops testing what it was built for.

The absence is frozen: the JavaScript suite asserts that the set of pinned zero
coordinates is empty, and fails with "a fixture now pins a zero coordinate;
assert the failure instead of the gap". So a later phase that closes this gap
with a real document is told to convert the absence into an assertion, rather
than leaving a green test that passes because something is missing.

### Saying that carefully

"No tier catches this" is a green-asserting claim, and it stops being true
without anything going red. Two such claims about this corpus were retired in a
single day, each by somebody building the thing the claim said was impossible:
that nothing past the writer boundary was reachable by a tier, retired by a tier
that decodes and then writes; and that a ZIP64 archive small enough to commit
could not exercise an extra-field parser, retired by masking the central
directory instead of the local header.

Every one of those was true of the corpus as it stood. None was a property of
the format.

> An unreachability claim is a claim about the current state of someone's
> ingenuity.

The working rule that survives is narrower and more useful: **a property is
reachable by the accept tier whenever the two readings put a different value in
the tree.** Rounding is not, because the accept tier's normalisation rounds both
sides alike; it needs the `write/` tier and `numbers/`.

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
