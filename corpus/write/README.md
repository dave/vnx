# Writer cases

Each `X.in.vnx` is a document, and `X.written.vnx` is exactly the bytes a
conforming writer emits for it. The runner decodes the input, calls WRITE, and
byte-compares.

`SPEC.md` 4.5.4 names two emit paths and this tier is the only one that exercises
the first:

| Path | Rounds and shortens? | Tier |
|---|---|---|
| WRITE — a model into a document | yes | `write/` |
| RE-RENDER — a decoded document back into that document | no | `accept/`, `format/` |

## Why it exists

A tier that re-renders cannot reach section 4.5.4 at all, because a re-render
applies none of its rules. Three of them are therefore reachable only from here:
a rounding tie; a negative zero that no fixture can hand a writer, because the
writer produces it itself by rounding towards zero; and shortening a position.

"Rounding is unreachable because no tier rounds" reads as a fact about the format
and is a fact about which tiers exist. **An unreachability claim is a claim about
the current state of someone's ingenuity, not about the format.**

The input is a document rather than a model, which is what makes the tier
language-neutral: the operation is decode-then-write, exactly as `format/`'s is
decode-then-re-render. No language-specific way to express a model is needed.

## A container case, and the one shape extension it needs

`container-cross-language` is a writer case whose output is an archive rather
than a document, so it carries a third input file:

```
container-cross-language.in.vnx              the document
container-cross-language.attachments.json    {root, attachments: [{name, body}]}
container-cross-language.written.vnz         exactly the bytes a writer emits
```

The manifest exists because a container write takes something no other tier's
input shape carries: a set of named attachments. Bodies are ASCII text so the
case holds no binary blob.

**They are in a manifest rather than in real files beside the sidecar, and that
is not tidiness.** One attachment is named with U+1F600, and a filename is
subject to the checkout filesystem's Unicode normalisation and case folding — so
a real file could arrive under a name that is not the one the case means, and the
fixture would test the filesystem instead of the writer. Inside JSON the name is
escaped text that nothing can alter.

A runner feeds the manifest's attachments in ANY order. The writer sorts them,
which section 7.1 requires and which this case verifies by construction: the same
562 bytes come back when the three are supplied reversed.

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
