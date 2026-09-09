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
| `tests/corpus.rs` | the conformance runner, all four tiers |
| `tests/numbers.rs` | the number-conformance table, the fifth tier |
| `tests/zip64.rs` | a hand-built ZIP64 container, because the corpus has none |

## Three operations, not one

```text
decode     bytes  -> a document, plus every diagnostic     SPEC 6
re_render  model  -> the bytes the document carried         SPEC 8.1
write      model  -> bytes, rounded and shortened           SPEC 4.5.4, 4.1
```

`re_render` and `write` are different operations, and a library with only one of
them cannot conform (SPEC 4.5.4). The accept tier requires `re_render`: a writer
rounds coordinates and shortens positions, so writing what you read is
legitimately not what you read.

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
`number-conformance/numbers.tsv` — 5,315 doubles as IEEE 754 hex with Go's
answers for the canonical spelling and for the spelling after rounding to 6 and
to 1 places. A rendering tie is a value rather than a shape, so no fixture holds
one usefully; and rounding happens only when a WRITER emits a model, while all
four corpus tiers re-render an existing document. An implementation can round
every coordinate in the format the wrong way and pass every corpus tier green.
160 of the table's rows also separate the two readings of SPEC 4.5.4's rounding
rule, and all 160 side with rounding against the double.

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

`tools/redproof.py` breaks the reader twenty-two ways and requires a runner to go
red each time. All twenty-two were caught, measured 2026-09-09; the script's own
docstring carries the table. Without that, "the corpus passes" is a claim about a
runner nobody has watched fail.

One caution the script records in a comment: its first notes-discarding mutation
came back green because it patched only the first call site and no accept fixture
has a folder with `notes`. **A mutation that misses its target is
indistinguishable from a runner that found nothing wrong.**
