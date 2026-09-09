# vnx — the VNX format in Rust

A codec for [VNX](../SPEC.md): it reads and writes `.vnx` and `.vnz`, produces
canonical form (SPEC 4.5) and reports diagnostics (SPEC 6.3).

Written from `SPEC.md` alone and validated against [`corpus/`](../corpus), with
no reference to any other implementation. Where the two disagreed, or the spec
was silent, the decision is recorded in a comment at the code that makes it.

```sh
cd rust
cargo test              # unit tests, the corpus runner, and the doc example
cargo clippy --all-targets
python3 tools/redproof.py   # break the reader and require the runner to notice
```

## Zero dependencies

`Cargo.toml` has an empty `[dependencies]`, deliberately. The crate carries its
own JSON parser and renderer, ECMAScript number formatter, ZIP reader and
DEFLATE decompressor. `#![forbid(unsafe_code)]`.

Two of those are not the indulgence they look like. No stock JSON encoder in any
target language can emit VNX canonical form (SPEC 4.5.6), so a serialiser had to
be written regardless — and Rust's own float formatting is wrong for this format
in two separate ways (see below), so the number path had to be written too.
DEFLATE was the one genuinely optional piece, and every entry in every corpus
archive is deflated, so a reader without it cannot open a container at all.

| Module | What it is |
|---|---|
| `json` | a strict RFC 8259 parser, with no permissive mode |
| `json::number` | ECMAScript `Number::toString`, ties to even |
| `canonical` | the byte layout of SPEC 4.5 |
| `model` | the decoded document |
| `decode` | SPEC 6: tolerant of damage, silent about nothing |
| `render` | the two emit paths, `re_render` and `write` |
| `geo` | the 50 m projection tolerance of SPEC 3.3.3 |
| `zip`, `zip::inflate` | a read-only ZIP walker and an inflater |
| `vnz` | the container of SPEC 7 |
| `tests/corpus.rs` | the conformance runner, all five tiers |
| `tests/numbers.rs` | the number-conformance table |
| `tests/zip64.rs` | a hand-built ZIP64 container, because the corpus has none |

## Three operations, not one

```text
decode     bytes  -> a document, plus every diagnostic     SPEC 6
re_render  model  -> the bytes the document carried         SPEC 8.1
write      model  -> bytes, rounded and shortened           SPEC 4.5.4, 4.1
```

`re_render` and `write` are different operations, and a library with only one of
them cannot conform (SPEC 4.5.4). The `accept/` and `format/` tiers require
`re_render`; the `write/` tier requires `write`. Writing what you read is
legitimately not what you read, because a writer rounds.

They differ in exactly three places, all at the emit boundary: a position's
coordinates are rounded to 6 and 1 decimal places, a position's trailing
absences are dropped, and `"hidden": false` is not written. `vnz` splits the
same way, so repackaging a container cannot silently round it.

## Where Rust fights the format

Three things a Rust implementation gets wrong by default. The first two are in
`json::number` and the third is in `render`.

- **Exponent form.** `{}` for `f64` never uses scientific notation — it renders
  `1e21` as twenty-two digits and `1e-7` as `0.0000001`. `{:e}` always uses it,
  and spells the exponent `e21` where ECMAScript spells it `e+21`. Neither is
  the required rendering, so the form rules are applied here over the digits
  `{:e}` supplies.
- **A tie.** Rust resolves a shortest-spelling tie AWAY FROM ZERO, and SPEC
  4.5.3 requires round-to-even. `1497898211722073.25` is `…073.3` from Rust and
  `…073.2` from this crate. About one double in 4,200 is a tie, concentrated
  where `time_ms` lives. The detection is exact integer arithmetic on the
  binary64 mantissa, not a round-trip probe — a probe finds phantoms above
  2^49, where several one-decimal spellings map to one double.
- **The writer's rounding.** `f64::round` rounds a tie away from zero and SPEC
  4.5.4 wants ties to even — but the obvious fix, `(v * 10.0).round_ties_even()
  / 10.0`, is a DIFFERENT RULE. Multiplying by the scale is itself a rounding
  step and moves values onto an exact half that were not on one: `0.15` is
  nearest `0.1` and comes out `0.2` that way. Six of ten ordinary
  elevation-shaped values disagree, in both directions. This crate takes the
  reading the prose states — the nearest value with that many decimal places to
  the double it was handed — which is what Rust's `{:.*}` computes, so `round_to`
  formats and parses back. `render::tests::scale_then_round_is_a_different_rule_
  and_this_pins_the_difference` measures the gap. Reported as a spec gap: SPEC
  4.5.4 does not say which of the two it means, and no corpus tier reaches
  either, because no tier asks a library to write.

Two things Rust makes easier than the other languages: `str` ordering is by
UTF-8 byte and therefore by code point, so canonical key order needs no custom
comparator (SPEC 4.5.1 warns JavaScript that it does); and a `String` cannot
hold a lone surrogate, so `"\ud83d"` alone is refused rather than substituted.

## What the type system wants, and why it is refused

A `Position` keeps its channels as `Vec<Option<f64>>`, not as a struct with
`Option<f64>` fields. `[lon, lat, null]` and `[lon, lat]` state the same thing,
and both must re-render AS WRITTEN, so a position's arity is model state. An
`Option<f64>` elevation field is the shape Rust pushes you towards and it cannot
pass the accept tier.

For the same reason a `data` record body, a `style` object and `rulesets` are
kept as raw `Json`. They are opaque by ruling (SPEC 5.1, 3.10, 3.9) and the only
thing that proves it is the accept tier's re-render.

## Geometry is continuous and structure is not

SPEC 6.2's sentence, and the one rule here that reads as its own opposite. A
position inside `pips` that will not parse drops the **arc or segment that holds
it**, not the position — while a link that cannot be used is dropped and its
chain survives.

They are the same rule. A missing vertex changes the shape of a line and leaves
nothing to indicate it: a shorter, plausible, wrong line. A missing link leaves a
gap the continuity rule of SPEC 3.3.3 can see and report, so nothing is lost
silently.

This crate kept the good pips until 2026-09-10, which is the reading SPEC 6.2
explicitly rules out. A ring vertex that will not parse drops the **item**, for
the same reason plus SPEC 3.7: a polygon is one closed ring of at least four
positions, so dropping a vertex both moves the boundary and can turn a
four-vertex ring into a three-vertex non-ring.

`position-unusable` therefore names *what was dropped* rather than only what was
wrong, because the remedy is not the same everywhere and a caller should not have
to derive it from the pointer.

## No fallbacks

A malformed value never becomes a default. It is dropped, and a diagnostic names
the cause and the JSON Pointer. Every refusal is one of SPEC 6.2's four causes or
SPEC 7's container causes; there is no catch-all variant in `Refusal`.

The only absences read as meanings are the two SPEC 3.1 names: an absent
`label-visible` is Auto, and an absent `hidden` is visible.

Every diagnostic carries a `Cause` with a stable `code()` alongside its English.
SPEC 6.3 makes the code the thing a conformance case compares and forbids
comparing the prose; six of the codes are pinned by `corpus/diagnose/` and are
wire commitments.

## What it does not do

- **No GPX, KML or GeoJSON.** Those are lossy conversions, not part of the
  format.
- **No `schema` verdict in the runner.** A sidecar's `schema` field needs a JSON
  Schema draft 2020-12 validator, which is a dependency. The reference
  implementation keeps its schema test out of `pkg/vnx` for the same reason
  (`corpus/schema/README.md`).
- **No DEFLATE compressor.** `vnz::write` stores its entries, which SPEC 7
  permits, and writes no ZIP64 records, which SPEC 7 also permits.
- **No ZIP64 writing.** Reading it is required and is now tested by a
  hand-built fixture (see below); writing it is optional and not done.
- **No streaming.** A document is parsed into memory. There is a nesting depth
  limit of 256 (SPEC 9 asks a reader to bound what one document may consume) and
  no limit on item count.

## Three things the corpus cannot reach, and what covers them here

The corpus decides conformance, and it has three holes that are properties of
the corpus rather than of any implementation. Each is covered by a test in this
crate instead.

**Number ties, rendering and rounding.** `tests/numbers.rs` runs
`corpus/numbers/numbers.tsv` — 5,315 doubles as IEEE 754 hex with Go's
answers for the canonical spelling and for the spelling after rounding to 6 and
to 1 places. A rendering tie is a value rather than a shape, so no fixture holds
one usefully; and rounding happens only when a WRITER emits a model, while all
four corpus tiers re-render an existing document. An implementation can round
every coordinate in the format the wrong way and pass every corpus tier green.
The table also separates the two readings of SPEC 4.5.4's rounding rule, and
every differing row sides with rounding against the double. `cargo run --example
provenance` prints the counts and their breakdown by row kind rather than this
README quoting them — the table grew by fourteen rows between two runs of that
example while it was being written, so an embedded figure was stale within
minutes of being correct.

**ZIP64.** SPEC 7 requires a reader to accept it and all four `.vnz` fixtures
are plain archives, so a reader can omit the extra-field parser entirely and
pass. `tests/zip64.rs` assembles an archive byte by byte that masks both sizes
and the local-header offset in the CENTRAL DIRECTORY, and saturates every field
the end record can, so the locator has to be followed. A fixture built with
Python's `force_zip64` masks the local header only and would leave the parser
untested.

**A diagnostic's `code`.** Every `diagnose/` fixture carries at most one
diagnostic and its path identifies it, so no case can separate a matcher that
compares the code from one that ignores it — measured: weakening the matcher to
compare `path` alone leaves the whole tier green. `tests/corpus.rs` therefore
self-tests its own matcher: right path with the wrong code must fail, a sidecar
with no `code` must fail, and a sidecar carrying the pre-`67e1721` `message`
field must fail rather than be silently ignored.

## Every runner has been seen to fail

`tools/redproof.py` breaks the reader twenty-eight ways and requires each to reach
the verdict it should. Measured 2026-09-10; the script's own docstring carries the
table.

A mutation may carry an expected verdict of GREEN, meaning it documents a hole in
the CORPUS rather than in this crate, and it fails the script if it ever goes red.
None does today — and the one that did is why the mechanism is worth having.
Reaching inward to a segment's first and last TIMED pip survived on 2026-09-09,
because `accept/track-partly-timed.vnx` carried no times at all in its untimed
segment and both readings returned unknown. It was reported, the fixture gained a
timed middle pip, and the entry went red the next time the script ran — so the
promotion was noticed rather than remembered.

**Two naive rounding routes, not one.** `Math.round(x*10)/10` is half-UP and
`f64::round(x*10)/10` is half-AWAY-from-zero; they agree on every positive tie and
disagree on every negative one, so a suite catching one and not the other has a gap
that reads as closed. Both are separate mutations. A third, scaling before
rounding, is a different operation rather than a tie direction and is caught by
`corpus/numbers/` alone — the `write/` tier's own values cannot see it, which is
the same "looks complete" gap one level over. Without that, "the corpus passes" is a claim about a
runner nobody has watched fail.

Two cautions the script records in comments, both paid for here:

**A mutation that misses its target is indistinguishable from a runner that found
nothing wrong.** The first notes-discarding mutation came back green because it
patched only the first call site, and no accept fixture has a folder with `notes`.

**A mutation broader than the rule proves the corpus catches something, not that
it catches this.** The first attempt at the expected-green entry above also
skipped wholly-untimed segments — a second defect — and went red for the skipping
rather than for the reaching inward. Narrowing it to change only which pips are
read is what turned an argument into a measurement.

And one about the tests themselves: two earlier tests in `geo.rs` were named for
corpus fixtures and hard-coded a superseded pair of coordinates, so they would
have passed with both corpus files deleted, and one of them asserted — green — a
statement that was false of the corpus it named. They are gone. The claim they
made is now a `redproof.py` mutation that reads whatever is committed today.
