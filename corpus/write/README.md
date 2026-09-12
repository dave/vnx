# The writer tier

Each `X.in.vnx` is a document and `X.written.vnx` is the document a conforming
writer produces from it, so this tier asserts the emit-boundary rules of
`SPEC §4.1`: rounding, and the shortest spelling of a position. The mechanics
common to every tier are in [`../README.md`](../README.md).

The runner decodes the input, calls WRITE, and compares the two as JSON trees.
Bytes are not compared: a document's whitespace, key order and number spelling
are unconstrained.

Three things are reachable only from here, because every other tier re-encodes
what it decoded and so applies both rules to values that satisfy them: a
rounding tie, a negative zero that a writer produces itself by rounding towards
zero, and shortening a position. The input is a document rather than a model,
which is what makes the tier language-neutral — the operation is
decode-then-write, and no language-specific way to express a model is needed.

## The cases

| Case | Files | What it asserts |
|---|---|---|
| `emit-boundary` | `.in.vnx`, `.written.vnx`, `.expect.json` | Rounding at both precisions, tie direction, and the shortest spelling of a position. |
| `container-cross-language` | `.in.vnx`, `.attachments.json`, `.written.vnz`, `.expect.json` | A document and its attachments through the WRITE path into an archive. |

`container-cross-language` writes an archive rather than a document, so it
carries a third input file: `container-cross-language.attachments.json`, a
manifest of `{root, attachments: [{name, body}]}`. A container write takes
something no other tier's input shape carries — a set of named attachments — and
the bodies are ASCII text, so the case holds no binary blob.

The attachments are in a manifest rather than in real files beside the sidecar,
and that is not tidiness. One attachment is named with U+1F600, and a filename
is subject to the checkout filesystem's Unicode normalisation and case folding,
so a real file could arrive under a name that is not the one the case means and
the fixture would test the filesystem instead of the writer. Inside JSON the
name is escaped text that nothing can alter.

Feed the manifest's attachments in any order; the entry names come back in one
order whichever it was. `SPEC §7.1` requires only that the root document is
first, and a writer that also orders its attachments is what makes two exports
of one library agree with each other.

The archive's own bytes are not compared. `SPEC §7.1` constrains five rules and
leaves every other field of the archive to the writer, so a runner compares the
root document's tree, the entry names and the attachment bodies.

## What a runner must assert

- The input is **not** the written form. Without this, a writer that echoed its
  input passes every case here.
- Only the two normalisations `SPEC §8.1` permits — and not the first of them
  here: rounding and the shortest spelling of a position are the rules this tier
  tests, so normalising them away would leave it proving nothing.
- The written form is a fixed point: writing it again reproduces it. Both rules
  are idempotent, so a case that fails this is a defect in the fixture rather
  than in the writer.
- A case with no sidecar is a failure, not a skip, as everywhere else.

## Edit a case

The sidecar's note says which values are load-bearing and why. Read it before
changing a number: several of them look arbitrary and are not, and at least one
looks redundant beside its neighbour and is the only one that decides anything.
