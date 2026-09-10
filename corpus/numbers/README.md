# The number-conformance table

`numbers.tsv` fixes what every VNX implementation must emit for a given double.
It is a conformance tier like the four beside it, and the only one that reaches
past the writer boundary.

It differs from those four in shape rather than in standing. A case there is a
document; a row here is a value, and there is no sidecar, because the three
answer columns are the expectation. That is why it is a table and not a
directory of files: the properties it pins are per-value, and a document that
carried five thousand of them would be a document nobody could read.

## Why it exists

A tier is defined by the operation it performs, and the four document tiers
perform two: decode, and decode-then-re-render. Neither rounds. Section 4.5.4's
rounding is applied only where a writer turns a model into a document, and
section 8.1 forbids a runner from using its writer for the accept check
precisely because a writer rounds. So an implementation can round every
coordinate and every elevation in the format the wrong way and take all four
tiers green. Measured in the Kotlin suite before the `write/` tier existed:
reverting the rounding tie rule reddened two tests out of ninety-one, and both
of them were this table. That gap is closed now — `write/` decodes and then
writes, so it applies the emit-boundary rules and a reverted tie rule reddens it
too. The measurement stands as what the four document tiers could not see, which
is why this table was built; it is no longer a live hole.

A re-render does re-spell, though, so section 4.5.3's rendering tie is reachable
by `format/number-tie`, whose input spells the value one way and whose expected
output spells it the other. The rule worth carrying:

> A value-level property is reachable by a re-render whenever the two readings
> produce different text for the same value.

What this table holds that no document tier reaches: the rounding tie, the
shortest-spelling rule across the whole double range, the exponent boundaries of
4.5.3, and a negative zero a writer CREATES by rounding towards it, which no
fixture can hand it because no fixture can contain it.

Say "no tier reaches this" carefully, and date it. Two such claims about this
table have been retired by somebody building the tier that reached them — the
rendering tie by `format/number-tie`, and part of the writer boundary by a tier
that performs decode-then-write. Both claims were true of the corpus as it stood
and neither was a property of the format. **An unreachability claim is a claim
about the current state of someone's ingenuity.** The practical consequence is
in the last section of this file: a test whose conclusion is an absence asserts
nothing once the absence ends, and nothing goes red to say so.

Both ties are live disagreements between real implementations, measured
2026-09-09:

| | value | Go | JavaScript | Python |
|---|---|---|---|---|
| render | `1.4978982117220732e15` | `1497898211722073.2` | `1497898211722073.2` | `1497898211722073.2` |
| round to 1 dp | `340.25` | `340.2` | `340.3` (`toFixed`) | `340.2` |

The first was found because the Kotlin and JavaScript libraries picked opposite
tie rules within an hour of each other, each from a defensible reading of an
ECMAScript clause that declines to decide ("the least significant digit of s is
not necessarily uniquely determined by these criteria"). Nothing anywhere went
red. **Ties round to even, in both columns.**

How often, and over which population — the rate alone decides nothing, because
the two populations disagree by more than three orders of magnitude. Measured
2026-09-09 over three million samples each:

| population | rendering ties |
|---|---|
| uniform over binary64 **bit patterns** | 1 in 4,225 |
| longitude, latitude, elevation, epoch ms | **0 in 1,000,000 each** |

Rendering ties exist only between about 1e10 and 1e16, overwhelmingly at 1e14 to
1e15, and only where the shortest spelling needs 16 or 17 significant digits.
Coordinates and elevations are far too small — their exact expansions run well
past 17 digits — and `time_ms` is an integer count, which never ties. So the
rendering tie does not threaten a coordinate; it threatens **an arbitrary number
in an opaque data record body**, which this format permits anywhere, and which is
where `format/number-tie` correctly put it.

Rounding ties are the opposite: rare in no sense at all, because they are
structured rather than accidental. At 1 decimal place the exact halfway values
are the odd quarters, so any quarter-metre elevation; at 6 places they are the
odd multiples of 1/128, because 5e-7 is not dyadic and `j/128` is therefore the
only family whose exact expansion ends in a 5 at the seventh place.

The `round6` and `round1` columns also separate the two ways of rounding to a
decimal place, which is a wider disagreement than the tie and the reason section
4.5.4 states the result rather than a procedure. Scaling by a power of ten,
rounding, and dividing back is itself a rounding step, and it moves values onto
an exact half that were never on one. Measured against the nearest one-place
value of the double each literal denotes:

| value | nearest 1 dp | scale, round to even, unscale |
|---|---|---|
| `0.15` | `0.1` | `0.2` |
| `0.05` | `0.1` | `0` |
| `1.05` | `1.1` | `1` |
| `0.45` | `0.5` | `0.4` |
| `1.85` | `1.9` | `1.8` |
| `0.95` | `0.9` | `1.0` |
| `340.25` | `340.2` | `340.2` |

Six of those seven disagree, and only the last is a genuine tie — `0.15` is
below its own halfway point, not on it, because the double nearest `0.15` is
`0.1499999999999999944…`. These are the shape of an ordinary elevation, so an
implementation taking the scaling route differs from a conforming one on
everyday data rather than on corner cases. The same hazard applies at six places
to every coordinate.

**The two disagreements need disjoint data, and that is the durable fact about
this table.** At one decimal place, not one row of the `round1-tie` category is
among the rows that separate the two routes. At six places, not one row of
`round6-tie` is. The tie categories are constructed to separate a tie's
DIRECTION, and they do not separate the choice of OPERATION at all — ordinary
values do that. So a library can have the tie rule right and the route wrong,
pass every deliberately-constructed tie row, and be wrong about every coordinate
it writes.

`0.15` is the example to put in front of anyone who thinks this is a corner
case: exact answer `0.1`, scaled route `0.2`, and it is not a tie in either
direction.

The `scaling-route` category exists because the table did not reach that at the
magnitudes the format carries. Measured over the table before the category
landed: every row separating the two routes was above 1e9, while the argument
above is about quarter-metre elevations and six-place coordinates. An
implementation whose scaling route was guarded at large magnitudes — a plausible
thing to write — passed the whole table and still emitted `0.2` for `0.15`. The
instrument caught the defect only where nobody meets it. Fourteen rows now sit
below 1e9, eight elevation-shaped and six valid longitudes and latitudes, found
by search because no round number of degrees separates the routes.

**And if a count comes from reconstructing the wrong behaviour, the
reconstruction is the thing to check first.** Breaking a disagreement down by
category means rebuilding the route that produced it, and a rebuild that is not
faithful answers a different question with the same confidence. Measured: an
attempt to break down 175 disagreeing rows gave 410 on its first run, because
the rebuild dropped a magnitude guard the shipped function had — so it was
measuring a route that never existed. **The only thing that caught it was having
a figure to disagree with.** Reconstructing something nobody had measured, 410
would have been published with nothing to contradict it.

> A reconstruction that is not faithful answers a different question.

That is an argument for deliberate overlap: two people measuring the same
quantity by different routes is not duplicated work, it is the only thing that
catches a sound method pointed at the wrong subject.

**Do not quote a row count for this from memory, and do not write one into a
document.** The table grew by fourteen rows between two runs of the script that
counts them, so a figure correct when written was stale within minutes. Compute
it. Two independently written counters agree exactly, at HEAD, on 168 rows at
one place and 206 at six — and the six-place figure is the one that describes
the damage, because six places is longitude and latitude and therefore every
coordinate rather than every elevation. Three rows at one place and 33 at six
overflow to infinity when scaled and are counted beside those figures rather
than inside them, because a non-finite product has no spelling to compare.

## The format

Tab separated. Lines beginning `#` are comments; the header names the columns
and records the seed.

```
bits              kind        render      round6     round1
3fd0000000000000  round1-tie  0.25        0.25       0.2
3f80000000000000  round6-tie  0.0078125   0.007812   0
```

- `bits` — the double as 16 hexadecimal digits of its IEEE 754 binary64
  encoding. A decimal literal would beg the question the table exists to answer.
- `kind` — which part of the sample the row is. Diagnosis only; do not branch on
  it.
- `render` — the canonical spelling (4.5.3).
- `round6` — the canonical spelling after rounding to 6 places (4.5.4, longitude
  and latitude).
- `round1` — the same at 1 place (4.5.4, elevation).

Running it is about ten lines in any language: read the hex, bit-cast it to a
double, and compare your three answers to the three columns.

## The header is a contract

Two header lines are machine-readable and a runner is REQUIRED to check them:

```
# seed=20260909 sample=2000 rows=5329
# counts: boundary=76 in-range=3000 named=14 negative-zero=5 render-tie=60 ...
```

Parse `rows=` and the `counts:` line, and assert that what the run observed
matches what they declare. Both are written by the generator, which refuses to
emit a table whose declared counts do not sum to its row count, so the header
cannot drift from the body.

**This exists because the obvious alternative ages badly.** A runner that names
the categories it depends on — a hardcoded list of what matters — goes stale the
moment a category is added, and **the newest category is always the one nothing
pins.** Measured: one runner asserted five categories by name, none of them
`scaling-route`, and separately that the table held more than 5,000 rows. So the
table could have lost all fourteen of its most discriminating rows, or three
hundred rows of any kind, and that suite would have stayed green. Five
hand-written lists age exactly as badly as the hardcoded path in each runner did
when this directory moved.

Checking the header instead means a runner learns about a new category from the
table rather than from a message, and a category that vanishes is a failure
rather than a silence.

A runner MUST NOT branch on `kind`. It is there for diagnosis — so a failure can
say which part of the sample broke — and the three answer columns are the whole
expectation. `counts` is the one thing to read it for.

## Where the answers come from

Go's `encoding/json` and `strconv`, which `SPEC.md` section 4.5.3 names as the
reference alongside JavaScript, with the one correction the spec names against
Go (negative zero renders as `0`). Generating them from any implementation under
test would be that implementation marking its own homework.

## Regenerating

```sh
go run ./makenumbers                            # the committed table
go run ./makenumbers -sample 300000 -out deep.tsv   # the deep check
```

The output is byte-reproducible from the seed in its header. The generator
refuses to write a table in which any category came out empty, or in which a
tie category does not actually differ from rounding away from zero — an
undiscriminating table is the failure that would not announce itself.

The committed table is 5,329 rows, sized to be read and diffed. The `-sample`
flag is what settles a rendering question at a scale the committed table cannot:
over 460,007 doubles, a library rounding rendering ties to even agrees with this
table everywhere, and one rounding them away from zero disagrees on 78 of them.

## Red-proofing your runner

A comparison that has never been seen to fail is not a comparison. Change your
tie rule from half-to-even to half-away-from-zero and confirm the table goes
red — in **both** the render column and the two round columns, because they are
different code paths and only one of them has an excuse for being right by
accident.

Then do the same for the rounding route, which the tie mutation does not reach.
Replace the rounding with scale-round-unscale and confirm the `round6` and
`round1` columns go red. A library can have the tie right and the route wrong,
and the seven values in the table above are what separates them.

Two ways a red proof over this table lies, both of which have happened here.

**A tie must be defined as the thing under test, not approximated by it.**
Asking whether a second equally short spelling round-trips is the tempting
proxy, and it labels far more rows than are ties: wherever the decimal step at
the shortest-round-trip length is finer than the double spacing, several
spellings map to one double and only one is nearest. Those are phantoms. The
proxy once produced sixty `render-tie` rows of which every one stayed green when
the tie rule was reverted — a category that discriminated nothing while looking
exactly as though it did.

The generator decides a tie exactly, in rational arithmetic, and
`assertDiscriminates` refuses to write a table in which a tie category fails to
separate rounding to even from rounding away from zero. `assertCatchesScaling
Route` does the same for the scaling category. Those two guards, not the
category names, are the evidence.

**A proof whose conclusion is an absence stops asserting anything the moment the
absence ends.** "No tier catches this" is a green-asserting test: it passes
because something is missing, so it goes on passing after the thing arrives, and
nothing anywhere goes red. Several proofs about this table went stale the day
`format/number-tie` landed. If a test here concludes that a property is
unreachable, re-run it whenever the corpus grows, and prefer a proof that names
what it expects to be red over one that expects everything to be green.

The repair for both is the same and it is the general one: **make the premise
checked.** A proof that a mutation reddens exactly one case should freeze why
that case and no other — otherwise a single red is indistinguishable from the
right red, and a proof that cannot tell you why it went red is measuring less
than it looks.
