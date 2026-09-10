# Writer cases

Each `X.in.vnx` is a document, and `X.written.vnx` is the document a conforming
writer produces from it. The runner decodes the input, calls WRITE, and compares
the two as JSON trees. The bytes are not compared: nothing about a document's
whitespace, key order or number spelling is constrained.

Section 4.1 puts two rules at the emit boundary and this tier is the only one
that exercises them:

| Path | Rounds and shortens? | Tier |
|---|---|---|
| WRITE — a model into a document | yes | `write/` |
| RE-ENCODE — a decoded document written back out | applies them to values that already satisfy them | `accept/` |

## Why it exists

A tier that re-encodes what it decoded rounds values that are already rounded
and shortens positions that are already short, so it cannot see a defect in
either rule. Three things are therefore reachable only from here: a rounding
tie; a negative zero that no fixture can hand a writer, because the writer
produces it itself by rounding towards zero; and shortening a position.

"Rounding is unreachable because no tier rounds" reads as a fact about the format
and is a fact about which tiers exist. **An unreachability claim is a claim about
the current state of someone's ingenuity, not about the format.**

The input is a document rather than a model, which is what makes the tier
language-neutral: the operation is decode-then-write. No language-specific way
to express a model is needed.

## A container case, and the one shape extension it needs

`container-cross-language` is a writer case whose output is an archive rather
than a document, so it carries a third input file:

```
container-cross-language.in.vnx              the document
container-cross-language.attachments.json    {root, attachments: [{name, body}]}
container-cross-language.written.vnz         the archive a writer produces
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

A runner feeds the manifest's attachments in ANY order, and the entry names come
back in one order whichever it was. Section 7.1 requires only that the root
document is first; a writer that also orders its attachments is what makes two
exports of one library agree with each other.

The archive's own bytes are not compared. Section 7.1 constrains five rules and
leaves every other field of the archive to the writer, so what a runner compares
is the root document's tree, the entry names, and the attachment bodies.

## What a runner must assert

- The input is NOT already the written form. Without it, a writer that echoed its
  input would pass every case here.
- Only two normalisations, the ones `SPEC.md` 8.1 permits, and NOT the first of
  them here: rounding and the shortest spelling of a position are the rules this
  tier tests, so normalising them away would leave it proving nothing.
- The written form is a fixed point: writing it again reproduces it. Rounding and
  shortening are both idempotent, so a case that fails this is a defect in the
  fixture rather than in the writer.
- A case with no sidecar is a failure, not a skip, as everywhere else.

## Editing a case

The sidecar's note says which values are load-bearing and why. Read it before
changing a number: several of them look arbitrary and are not, and at least one
looks redundant beside its neighbour and is the only one that decides anything.
