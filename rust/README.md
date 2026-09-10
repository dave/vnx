# vnx — the VNX format in Rust

A codec for [VNX](../SPEC.md): it reads and writes `.vnx` and `.vnz` and reports
diagnostics (SPEC 6.3).

Written from `SPEC.md` alone and validated against [`corpus/`](../corpus), with
no reference to any other implementation. Where the two disagreed, or the spec
was silent, the decision is recorded in a comment at the code that makes it.

```sh
cd rust
cargo test              # unit tests, the corpus runner, and the doc example
cargo clippy --all-targets
python3 tools/redproof.py   # break the reader and require the runner to notice
```

## Two dependencies

`serde_json` parses and serialises; `flate2` compresses and decompresses.
`#![forbid(unsafe_code)]`.

`serde_json` is taken with **`features = ["float_roundtrip"]`**, and that is
not a tuning choice. Without it `-3.6712345678901235` parses one bit low —
`c00d5eb03aa8431d` where `str::parse` gives `…431e`. That literal is the
longitude in `corpus/accept/full-precision-coordinate`, and the last bit of a
coordinate is what that case exists to protect.

The parse goes to `serde_json::Value` and a hand-written walk turns that into
the model. **Not a derive.** A derive stops at the first field it cannot read,
and SPEC 6.2 requires one diagnostic per bad path with the rest of the document
kept.

The ZIP READER is still written out. Three of SPEC 7's reader rules are ones a
general-purpose ZIP library does not offer: an over-declared entry size must be
ACCEPTED where the CRC agrees, an unusable compression method takes a different
remedy for the root document than for an attachment, and a duplicate entry name
must be seen twice rather than resolved.

| Module | What it is |
|---|---|
| `json` | a JSON value, parsed and serialised by `serde_json` |
| `model` | the decoded document |
| `decode` | SPEC 6: tolerant of damage, silent about nothing |
| `encode` | the emit boundary of SPEC 4.1, and the rounding |
| `geo` | the 50 m projection tolerance of SPEC 3.3.3 |
| `zip` | a ZIP reader and writer, with `flate2` under both |
| `vnz` | the container of SPEC 7 |
| `tests/corpus.rs` | the conformance runner, all four tiers |
| `tests/support/tree.rs` | the JSON-tree comparison of SPEC 8.1 |
| `tests/numbers.rs` | the number-conformance table |
| `tests/zip64.rs` | a hand-built ZIP64 container, because the corpus has none |
| `tests/hostile.rs` | truncation and corruption sweeps: refuse, never panic |

## Two operations

```text
decode  bytes -> a document, plus every diagnostic     SPEC 6
encode  model -> bytes, rounded and shortened          SPEC 4.1
```

**Encoding what you decoded is legitimately not what you read**, because a
writer rounds coordinates to 6 and 1 decimal places, drops a position's trailing
absences, and does not write `"hidden": false`.

Nothing constrains a document's bytes (SPEC 8.1) — key order, number spelling
and whitespace are the writer's own business — so the corpus compares JSON
TREES. The accept tier applies those emit-boundary rules to BOTH sides, because
it compares a document against a re-encoding of itself; the write tier applies
neither, because there they are the rules under test.

⚠ **And never one model against another.** A reader that discarded every data
record, every ruleset, every style and every `notes` produces a model that
compares equal to itself and passes such a tier green — that is the whole
extensibility mechanism, all styling and all user prose.
`a_reader_that_discards_what_it_read_is_caught` freezes the proof.

## Where Rust fights the format

One thing a Rust implementation gets wrong by default, and it is in `encode`.

- **The writer's rounding.** `f64::round` rounds a tie away from zero and SPEC
  4.1 wants ties to even — but the obvious fix, `(v * 10.0).round_ties_even()
  / 10.0`, is a DIFFERENT RULE. Multiplying by the scale is itself a rounding
  step and moves values onto an exact half that were not on one: `0.15` is
  nearest `0.1` and comes out `0.2` that way. Six of ten ordinary
  elevation-shaped values disagree, in both directions. This crate takes the
  reading the prose states — the nearest value with that many decimal places to
  the double it was handed — which is what Rust's `{:.*}` computes, so `round_to`
  formats and parses back. `encode::tests::scale_then_round_is_a_different_rule_
  and_this_pins_the_difference` measures the gap, and
  `corpus/numbers/numbers.tsv` decides it: 239 of its 5,329 rows separate the
  two operations at six decimal places, and every one sides with rounding
  against the double.

  `round_to` also collapses a negative zero. Rounding CREATES ones that were
  never in the model — `-4e-08` to six places, `-0.04` to one — and the table
  gives `0` on all 993 rows that reach it.

## What the type system wants, and why it is refused

A `Position` keeps its channels as `Vec<Option<f64>>`, not as a struct with
`Option<f64>` fields. `[lon, lat, null]` and `[lon, lat]` state the same thing,
and a reader must take either, so a position's arity is model state. An
`Option<f64>` elevation field is the shape Rust pushes you towards, and it
cannot express the difference the write tier is about.

For the same reason a `data` record body, a `style` object and `rulesets` are
kept as raw `Json`. They are opaque by ruling (SPEC 5.1, 3.10, 3.9), and the
only thing that proves they survived is the accept tier's tree comparison
against the ORIGINAL FILE.

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
- **No ZIP64 writing.** Reading it is required and is tested by a hand-built
  fixture (see below); writing it is optional and not done.
- **No streaming, and no resource policy.** A document is parsed into memory.
  Nothing bounds the number of items, the depth of the folder tree, or the size
  of an opaque data record, so a ten-million-waypoint document is read in full.

  That is deliberate. SPEC 9 says a reader SHOULD bound what one document may
  consume, and **the library cannot know the bound while the caller can**: a
  hiking application exporting its own library legitimately produces that
  document, and a server accepting an upload from a stranger does not. Any number
  picked here is wrong for one of them, and refusing a large legitimate export
  would be worse than reading it.

  **So the answer, if anyone implements it, is a lever rather than a limit**: an
  optional cap on the reader, defaulting to none, refusing beyond it with a
  cause. That is the no-fallbacks rule applied to a resource policy — do not
  invent one, expose the decision. Reach for the parameter, not for a constant.

- **One exception: nesting depth is capped at 256**, and it belongs in the
  library where the others do not. Recursion depth is the one resource whose
  failure is a stack overflow rather than slowness, and **a stack overflow is not
  a refusal a caller can handle.** That makes it a correctness bound rather than
  a policy one.

## Three things the corpus cannot reach, and what covers them here

The corpus decides conformance, and it has three holes that are properties of
the corpus rather than of any implementation. Each is covered by a test in this
crate instead.

**Rounding.** `tests/numbers.rs` runs `corpus/numbers/numbers.tsv` — doubles as
IEEE 754 hex, with Go's answers after rounding to 6 and to 1 places. Rounding
happens only when a WRITER emits a model, and every other corpus tier
re-encodes what it decoded, which applies rounding to values that already
satisfy it. An implementation can round every coordinate in the format the wrong
way and pass every corpus tier green.

The comparison is **by bit pattern, never by text**. The table's `render` column
is not read at all: how a number is spelled is the writer's own business (SPEC
4.2), and comparing the rounded value as text would put a rendering rule this
format no longer has back into the measurement through the side door.

The table also separates the two readings of SPEC 4.1's rounding rule, and every
differing row sides with rounding against the double. `cargo run --example
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

**Hostile input.** Every corpus fixture is well-formed, including the ones that
must be refused — they are refused for FORMAT reasons rather than for being
damaged. So no tier asks what happens to a truncated download or a corrupt
archive. `tests/hostile.rs` sweeps every prefix and every single-byte flip of a
container, a deflated one and a document, and requires a refusal or a
clean read — never a panic. `#![forbid(unsafe_code)]` makes the bad case a panic
rather than memory corruption, which is the good version of getting it wrong and
is still not a refusal.

That sweep found a real defect, and it took a hand-built archive to reach it: a
ZIP64 extended information field can name a local-header offset anywhere in a
`u64`, so `at + len` overflowed a `usize` before any bounds check ran. In a
release build it wrapped and the archive was refused, so nobody would have
noticed; in a debug build it panicked. No truncation or bit-flip of a committed
fixture can produce it, because none of them carries a ZIP64 extra field.

Three hazards in the same family, each with a mutation behind it. **A declared
uncompressed size is never reserved up front** — in Rust that would abort on
allocation failure rather than return an error, which no caller can intercept and
`forbid(unsafe_code)` does not help. It is used as a CEILING instead: the
decompressor is given one byte more than the declared size and no more, so a
stream that over-produces is stopped there rather than run to completion and
measured afterwards. **A CRC is checked**, because a corrupt ATTACHMENT is
caught by nothing else: the document keeps parsing, so the archive reads clean.
**A stored entry's two declared sizes must agree**, or a truncated entry reads as
whole.

**A diagnostic's `code`.** Every `diagnose/` fixture carries at most one
diagnostic and its path identifies it, so no case can separate a matcher that
compares the code from one that ignores it — measured: weakening the matcher to
compare `path` alone leaves the whole tier green. `tests/corpus.rs` therefore
self-tests its own matcher: right path with the wrong code must fail, a sidecar
with no `code` must fail, and a sidecar carrying the pre-`67e1721` `message`
field must fail rather than be silently ignored.

## Every runner has been seen to fail

`tools/redproof.py` breaks the reader thirty-five ways and requires each to reach
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
