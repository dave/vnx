# The number-conformance table

`numbers.tsv` fixes what every VNX implementation must emit for a given double.
It is a fifth conformance tier in everything but its location, and it is here
rather than in `corpus/` only because `spec-rewrite` has not yet folded it in.
**Destination: `corpus/numbers/`.** Nothing about it depends on that path except
the one line in each implementation's runner that points at it.

## Why it exists

Two rules in `SPEC.md` decide bytes, and **the corpus can reach neither.**

**Rendering ties (4.5.3).** A tie is a value, not a shape. Nobody writes a
fixture containing a double whose shortest decimal spelling is exactly halfway
between two equal-length candidates — and if they did, both spellings parse to
the same double, so it would pass either way at value level and differ only in
bytes nothing compares.

**Rounding ties (4.5.4).** Stronger: rounding happens only when a **writer**
emits a model, and all four corpus tiers RE-RENDER an existing document —
which section 8.1 requires *precisely because a writer rounds*. **The corpus's
reach ends at the writer boundary.** An implementation can round every
coordinate in the format the wrong way and pass every tier green.

Both are live disagreements between real implementations, measured 2026-09-09:

| | value | Go | JavaScript | Python |
|---|---|---|---|---|
| render | `1.4978982117220732e15` | `1497898211722073.2` | `1497898211722073.2` | `1497898211722073.2` |
| round to 1 dp | `340.25` | `340.2` | `340.3` (`toFixed`) | `340.2` |

The first was found because the Kotlin and JavaScript libraries picked opposite
tie rules within an hour of each other, each from a defensible reading of an
ECMAScript clause that declines to decide ("the least significant digit of s is
not necessarily uniquely determined by these criteria"). Nothing anywhere went
red. **Ties round to even, in both columns.**

Ties are not exotic. Rendering ties are about one double in 4,200 of a uniform
sample. Rounding ties are worse, because they are structured: at 1 decimal place
every odd quarter is one, so **any quarter-metre elevation**; at 6 places every
odd multiple of 1/128.

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

The committed table is 5,315 rows, sized to be read and diffed. The `-sample`
flag is what settled the rendering rule: 460,007 doubles, zero disagreements
once the tie rule was corrected, and 78 disagreements when it was put back.

## Red-proofing your runner

A comparison that has never been seen to fail is not a comparison. Change your
tie rule from half-to-even to half-away-from-zero and confirm the table goes
red — in **both** the render column and the two round columns, because they are
different code paths and only one of them has an excuse for being right by
accident.
