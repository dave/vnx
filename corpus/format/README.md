# Formatter cases

Each `X.in.vnx` is a deliberately messy document, and `X.canonical.vnx` is
exactly what a conforming formatter must emit for it. The runner canonicalises
the input and byte-compares.

These exist because the `accept/` files cannot catch a formatter defect. Those
are stored canonical, so canonicalising one is a no-op over values that are
already spelled correctly: a renderer that gets `-0` wrong, or writes `1e-06`
for `0.000001`, is never asked the question, because no accept file contains the
input form that would trigger it. Found 2026-09-01, when a negative-zero defect
in the reference implementation turned out to be invisible to its own corpus.

The inputs here are written in the wrong spellings on purpose: keys out of order,
positions spread over several lines, `340.0`, `1e-06`, `1E21`, `-0.0`, a double
spelled with the longer of its two equivalent decimal forms, and — in
`escaping` — every string escaped where canonical form writes raw, spelled long
where canonical form uses a short escape, and in uppercase hex. A formatter that
passes these files has a layout that agrees with every other implementation's.

## Why this tier keeps earning its place

Three defects were found on 2026-09-09 by having more than one implementation,
and **the accept tier could not have seen any of them**:

- a number tie rendered away from zero rather than to even
  (`1497898211722073.3` against `…073.2`);
- a rounding tie doing the same at the writer's boundary (`340.25` to one place
  as `340.3` rather than `340.2`);
- a string escaped where the format says raw (`<`, `>`, `&`, U+2028, U+2029).

Each is two conforming libraries emitting different bytes for the same model,
with every test in every language staying green. The accept tier cannot see them
because it re-renders a file that is already canonical, so it only ever asks the
renderer about values already spelled correctly — the same blindness that hid the
negative-zero defect. Only an input written in the WRONG spelling asks the
question.

## Where the corpus's reach ends

Every tier here re-renders a document. **No tier ever asks a library to WRITE
one** — to turn a model into a document with the emit-boundary rules applied.
So the whole of `SPEC.md` section 4.5.4 is beyond this corpus, and three rules
live out there:

- **A rounding tie.** `340.25` to one place is `340.2` under round-to-even and
  `340.3` under round-half-away. Exact doubles, and a quarter-metre elevation
  grid produces them.
- **A negative zero a writer CREATED.** `format/` carries `-0.0` as an input
  literal, so it tests a renderer collapsing a negative zero it was GIVEN.
  Rounding `-0.04` to one place makes a fresh one, and nothing anywhere tests
  that — a different code path, and one that only exists once a library has a
  writer at all.
- **Shortening a position.** A writer emits the shortest spelling; a formatter
  reproduces what it was given. Only the second is ever exercised here.

**Each implementation carries its own tests for these.** A fixture cannot, and
saying so is better than leaving a gap that looks covered.

⚠ **Writing a tie case needs exact arithmetic, and the obvious detector is
wrong.** Asking "does a neighbouring one-decimal spelling also round-trip to
this double" finds phantoms: above 2^49 the spacing between doubles exceeds 0.1,
so several spellings map to one double and only one of them is nearest. That
test gave 400 candidates of which 91 were not ties at all. A true tie has its
two candidate spellings EXACTLY equidistant from the value, which needs exact
rational arithmetic rather than floating-point comparison. Get it wrong and the
case either chases phantoms or passes a genuinely broken renderer.

## A normative change the corpus could not see

`diagnose/chain-does-not-join.vnx` has a chain whose consecutive links differ in
BOTH their node keys and their positions. So a reader comparing keys only and a
reader comparing resolved positions return the same verdict, and the file passes
either way.

So this file cannot tell the two readings apart, and section 3.3.3 requires one
of them: two distinct node keys at the same position meet. An implementation that
compares keys only passes here and is wrong. The corpus is a set of cases, not a
set of rules, and a case only discriminates between the readings its own data can
separate.
