# The number tier

`numbers.tsv` asserts what every implementation must produce when it rounds a
given double to 6 and to 1 decimal places (`SPEC §4.1`). The mechanics common to
every tier are in [`../README.md`](../README.md).

A case here is a value rather than a document, so there is no sidecar: the two
answer columns are the expectation. One rule is reachable from nowhere else in
the corpus — a tree comparison is by value and `-0` equals `0`, so no document
tier can see a negative zero a writer created. This table compares bit patterns,
and it is the whole of the corpus's coverage for that rule. A writer meets
negative zero by rounding towards it: `-0.04` at one decimal place, `-4e-7` at
six, with `SPEC §4.1` requiring the sign to be normalised away.

## The format

Tab separated. Lines beginning `#` are comments; the header names the columns
and declares what the body holds.

```
bits              kind        round6     round1
3fd0000000000000  round1-tie  0.25       0.2
3f80000000000000  round6-tie  0.007812   0
```

| Column | Meaning |
|---|---|
| `bits` | The double, as 16 hexadecimal digits of its IEEE 754 binary64 encoding. A decimal literal would beg the question the table exists to answer. |
| `kind` | Which part of the sample the row is. Diagnosis only; a runner must not branch on it. |
| `round6` | The value rounded to 6 places (`SPEC §4.1`, longitude and latitude). |
| `round1` | The value rounded to 1 place (`SPEC §4.1`, elevation). |

Running the tier is about ten lines in any language: read the hex, bit-cast it
to a double, round it twice, and compare against the two columns.

**Compare values, not text.** The answer columns are decimal text carrying a
value, so parse them and compare bit patterns. Comparing the text would test how
a language spells a double, which is not a rule of this format: `SPEC §4.2`
constrains a number's value and says nothing about its spelling. Bit patterns
are also what makes the negative-zero rows mean anything, because `0` and `-0`
are equal as values and different as bit patterns.

## The header is a contract

Two header lines are machine-readable, and a runner is required to check them:

```
# rows=48
# counts: boundary=9 named=8 negative-zero=5 round1-tie=6 round6-tie=8 ...
```

Parse `rows=` and the `counts:` line, and assert that what the run observed
matches what they declare. Both are written by the generator, which refuses to
emit a table whose declared counts do not sum to its row count, so the header
cannot drift from the body. Quote these figures from the header rather than
from any other document.

Check the header rather than a hardcoded list of categories. A runner that names
the categories it depends on goes stale the moment a category is added, and
**the newest category is always the one nothing pins.** One runner asserted five
categories by name, none of them `scaling-route`, and separately that the table
held more than 5,000 rows — so the table could have lost its most discriminating
rows with that suite green. Checking the header instead means a runner learns
about a new category from the table, and a category that vanishes is a failure
rather than a silence.

## What the categories separate

| `kind` | What its rows separate |
|---|---|
| `named` | Ordinary coordinates and elevations of the kind a document carries. |
| `boundary` | The ends of each range, zero, and the two magnitudes either side of what six decimal places keep. |
| `round1-tie` | The direction a tie goes at 1 place — the odd quarters, so any quarter-metre elevation. |
| `round6-tie` | The direction a tie goes at 6 places — the odd multiples of `1/128`. |
| `scaling-route` | Exact-decimal rounding against scale-round-unscale, at both precisions. |
| `negative-zero` | A rounded result whose sign must be normalised away. |

Rounding to a decimal place has two independent choices, and a library can get
one right and the other wrong.

**Which way a tie goes.** At six places the ties are the odd multiples of
`1/128`, because `5e-7` is not dyadic and `j/128` is the only family whose exact
expansion ends in a 5 at the seventh place. The tie categories carry ties in
both directions: `340.25` rounds down to `340.2` and `340.75` rounds up to
`340.8`. It takes the pair — a table carrying one of them looks like tie
coverage and leaves two wrong rules passing.

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

Every one of those disagrees, and not one is a tie: the double nearest `0.15` is
`0.1499999999999999944…`, which is below its own halfway point. These are the
shape of an ordinary elevation, so a library taking the scaling route differs
from a conforming one on everyday data rather than on corner cases. The same
hazard applies at six places to every coordinate, and the `scaling-route`
category carries valid longitudes and latitudes that separate the two, found by
search, since no round number of degrees does.

**The two disagreements need disjoint data, and that is the durable fact about
this table.** The tie categories separate a tie's direction and do not separate
the choice of operation at all; ordinary values do that. A library can have the
tie rule right and the route wrong, pass every deliberately constructed tie row,
and be wrong about every coordinate it writes.

## Red-proof your rounding

Three mutations, each of which must redden this table:

- Change the tie rule from half-to-even to half-away-from-zero.
- Replace the rounding with scale-round-unscale. The tie mutation does not reach
  this one; the values in the table above are what separates them.
- Remove the collapse that turns a rounded `-0` into `0`. Nothing else in the
  corpus can see that.

Define a property as the thing under test, never approximate it. A tie decided
by asking whether a second equally short spelling round-trips is the tempting
proxy, and it labels far more rows than are ties: a category mined that way held
sixty rows, every one of which stayed green when the tie rule was reverted. The
generator decides a tie exactly, in rational arithmetic, and its guards are the
evidence rather than the category names.

## Where the answers come from

Go's `strconv`, which is correctly rounded and breaks a tie to even. Generating
them from an implementation under test would be that implementation marking its
own homework.

## Regenerate the table

```sh
cd corpus/numbers && go run ./makenumbers
```

Every row is chosen, and the selection is code rather than a hand edit: change
`makenumbers` and run it. The generator refuses to write a table in which any
category came out empty, in which a tie category does not differ from rounding
away from zero, in which a `scaling-route` row agrees under both routes, or in
which no row rounds to a negative zero. An undiscriminating table is the failure
that would not announce itself.
