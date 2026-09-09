# Writer cases

Each `X.in.vnx` is a document, and `X.written.vnx` is exactly the bytes a
conforming WRITER emits for it. The runner decodes the input, calls WRITE, and
byte-compares.

`SPEC.md` 4.5.4 names two emit paths and this tier is the only one that exercises
the first:

| Path | Rounds and shortens? | Tier |
|---|---|---|
| WRITE — a model into a document | yes | `write/` |
| RE-RENDER — a decoded document back into that document | no | `accept/`, `format/` |

## Why it exists

Until this directory, every tier re-rendered, so nothing in the corpus ever asked
a library to write. The whole of `SPEC.md` 4.5.4 was therefore beyond the
corpus's reach, and three rules lived out there with only per-implementation
tests defending them: a rounding tie, a negative zero the writer CREATED, and
shortening a position.

That was stated for months as a permanent property — "rounding is unreachable
because no tier rounds" — and it was not a fact about the format. It was a fact
about which tiers had been built. **An unreachability claim is a claim about the
current state of someone's ingenuity, not about the format.**

The input is a document rather than a model, which is what makes the tier
language-neutral: the operation is decode-then-write, exactly as `format/`'s is
decode-then-re-render. No language-specific way to express a model is needed.

## What a runner must assert

- The input is NOT already the written form. Without it, a writer that echoed its
  input would pass every case here.
- The written form is a fixed point: writing it again reproduces it. Rounding and
  shortening are both idempotent, so a case that fails this is a defect in the
  fixture rather than in the writer.
- A case with no sidecar is a failure, not a skip, as everywhere else.

## Editing a case

The sidecar's note says which values are load-bearing and why. Read it before
changing a number: several of them look arbitrary and are not, and at least one
looks redundant beside its neighbour and is the only one that decides anything.
