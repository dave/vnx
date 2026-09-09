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

The rounding tie is the one case here that not even this tier reaches, because
rounding happens only when a WRITER emits a model and every tier re-renders. It
needs a test in each implementation rather than a fixture.
