# The number-conformance table

`numbers.tsv` fixes what every VNX implementation must produce when it rounds a
given double. It is a conformance tier like the four beside it, and the only one
that reaches the emit boundary per value rather than per document.

It differs from those four in shape rather than in standing. A case there is a
document; a row here is a value, and there is no sidecar, because the two answer
columns are the expectation.

## Why it exists

A tier is defined by the operation it performs, and the document tiers perform
two: decode, and decode-then-encode. Rounding is applied only where a writer
turns a model into a document, and section 8.1 forbids a runner from using its
writer for the accept comparison precisely because a writer rounds. `write/`
closes most of that gap: it decodes and then writes, so a wrong tie rule reddens
it. What `write/` cannot reach is a rule that only fails on particular doubles,
and that is what a table of chosen values is for.

One rule is reachable from nowhere else at all.

> A tree comparison is by value, and `-0` equals `0`. So no document tier can
> see a negative zero a writer created. This table compares bit patterns, and it
> is the whole of the corpus's coverage for that rule.

A writer meets negative zero by rounding towards it: `-0.04` at one decimal
place, `-4e-7` at six. Section 4.1 requires the sign to be normalised away.

## The format

Tab separated. Lines beginning `#` are comments; the header names the columns
and declares what the body holds.

```
bits              kind        round6     round1
3fd0000000000000  round1-tie  0.25       0.2
3f80000000000000  round6-tie  0.007812   0
```

- `bits` — the double as 16 hexadecimal digits of its IEEE 754 binary64
  encoding. A decimal literal would beg the question the table exists to answer.
- `kind` — which part of the sample the row is. Diagnosis only; do not branch on
  it.
- `round6` — the value after rounding to 6 places (section 4.1, longitude and
  latitude).
- `round1` — the same at 1 place (section 4.1, elevation).

Running it is about ten lines in any language: read the hex, bit-cast it to a
double, round it twice, and compare against the two columns.

**Compare values, not text.** The answer columns are decimal text carrying a
value, so parse them and compare bit patterns. Comparing the text instead would
test how your language spells a double, which is not a rule of this format —
section 4.2 constrains a number's value and says nothing about its spelling.
Bit patterns are also what makes the negative-zero row mean anything: `0` and
`-0` are equal as values and different as bit patterns.

## What the categories separate

Rounding to a decimal place has two independent choices, and a library can get
one right and the other wrong.

**Which way a tie goes.** At one place the exact halfway values are the odd
quarters, so any quarter-metre elevation; at six places they are the odd
multiples of `1/128`, because `5e-7` is not dyadic and `j/128` is the only
family whose exact expansion ends in a 5 at the seventh place. `round1-tie` and
`round6-tie` carry ties in both directions: `340.25` rounds down to `340.2` and
`340.75` rounds up to `340.8`. It takes the pair. A table carrying one of them
looks like tie coverage and leaves two wrong rules passing.

**Whether the value is rounded on its exact decimal expansion, or scaled by a
power of ten and rounded as an integer.** Scaling is itself a rounding step, and
it moves values onto an exact half that were never on one. Measured against the
nearest one-place value of the double each literal denotes:

| value | nearest 1 dp | scale, round to even, unscale |
|---|---|---|
| `0.15` | `0.1` | `0.2` |
| `0.05` | `0.1` | `0` |
| `1.05` | `1.1` | `1` |
| `0.45` | `0.5` | `0.4` |
| `1.85` | `1.9` | `1.8` |
| `0.95` | `0.9` | `1.0` |

Every one of those disagrees, and not one of them is a tie: the double nearest
`0.15` is `0.1499999999999999944…`, which is below its own halfway point. These
are the shape of an ordinary elevation, so a library taking the scaling route
differs from a conforming one on everyday data rather than on corner cases. The
same hazard applies at six places to every coordinate, and the `scaling-route`
category carries valid longitudes and latitudes that separate the two, found by
search, since no round number of degrees does.

**The two disagreements need disjoint data, and that is the durable fact about
this table.** The tie categories separate a tie's direction and do not separate
the choice of operation at all; ordinary values do that. A library can have the
tie rule right and the route wrong, pass every deliberately-constructed tie row,
and be wrong about every coordinate it writes.

`named` and `boundary` are the ordinary end: real coordinates and elevations,
the ends of each range, zero, and the two magnitudes either side of what six
decimal places keep.

## The header is a contract

Two header lines are machine-readable and a runner is required to check them:

```
# rows=48
# counts: boundary=9 named=8 negative-zero=5 round1-tie=6 round6-tie=8 ...
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
table could have lost its most discriminating rows and that suite would have
stayed green.

Checking the header instead means a runner learns about a new category from the
table rather than from a message, and a category that vanishes is a failure
rather than a silence.

A runner must not branch on `kind`. It is there for diagnosis, so a failure can
say which part of the sample broke, and the two answer columns are the whole
expectation. `counts` is the one thing to read it for.

## Where the answers come from

Go's `strconv`, which is correctly rounded and breaks a tie to even. Generating
them from any implementation under test would be that implementation marking its
own homework.

## Regenerating

```sh
go run ./makenumbers
```

Every row is chosen, and the selection is code rather than a hand edit: change
`makenumbers` and run it. The generator refuses to write a table in which any
category came out empty, in which a tie category does not actually differ from
rounding away from zero, in which a `scaling-route` row agrees under both
routes, or in which no row rounds to a negative zero. An undiscriminating table
is the failure that would not announce itself.

## Red-proofing your runner

A comparison that has never been seen to fail is not a comparison. Three
mutations, each of which must redden this table:

- Change your tie rule from half-to-even to half-away-from-zero.
- Replace your rounding with scale-round-unscale. The tie mutation does not
  reach this one, and the values in the table above are what separates them.
- Remove the collapse that turns a rounded `-0` into `0`. Nothing else in the
  corpus can see that.

Two ways a red proof over this table lies, both of which have happened here.

**A property must be defined as the thing under test, not approximated by it.**
A tie decided by asking whether a second equally short spelling round-trips is
the tempting proxy, and it labels far more rows than are ties. Those are
phantoms: a category once mined that way held sixty rows, every one of which
stayed green when the tie rule was reverted. The generator decides a tie exactly,
in rational arithmetic, and its guards are the evidence rather than the category
names.

**A proof whose conclusion is an absence stops asserting anything the moment the
absence ends.** "No tier catches this" is a green-asserting test: it passes
because something is missing, so it goes on passing after the thing arrives, and
nothing anywhere goes red. If a test here concludes that a property is
unreachable, re-run it whenever the corpus grows, and prefer a proof that names
what it expects to be red over one that expects everything to be green.

The repair for both is the same and it is the general one: **make the premise
checked.** A proof that a mutation reddens exactly one case should freeze why
that case and no other — otherwise a single red is indistinguishable from the
right red, and a proof that cannot tell you why it went red is measuring less
than it looks.
