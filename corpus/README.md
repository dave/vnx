# The VNX conformance corpus

Spec §8. **Every implementation runs these same files.** The corpus is the only
real defence against independent libraries quietly disagreeing, and it cannot do
that retroactively — which is why it exists before the second library does.

## Layout

```
accept/    must be read, with ZERO diagnostics
reject/    must be refused, and the refusal must name its cause
diagnose/  must be READ, reporting exactly the listed diagnostics
format/    messy input + exactly the bytes a formatter must emit for it
tools/     the generator for the .vnz archives
```

Each `foo.vnx` (or `foo.vnz`) has a `foo.expect.json` beside it. **A file with
no sidecar is a failure, not a skip** — a corpus that silently ignores what it
cannot explain is worth nothing, because the file everyone assumes is covered is
exactly the one that is not.

## The sidecar

```jsonc
{
  "kind": "accept" | "reject" | "diagnose",   // must match the directory
  "note": "why this case exists",             // required; the thing that rots first

  "counts":    {"waypoints": 1, "tracks": 1}, // exact, per collection
  "positions": [{"waypoint": "X", "lon": -3.6, "lat": 57.1}],
  "tracks":    [{"name": "T", "moving_time_ms": 167000}],  // null = UNKNOWN
  "attachments": ["photos/summit.jpg"],       // .vnz only

  "refusal":     "substring of the expected error",        // reject only
  "diagnostics": [{"path": "/waypoints/0/nmae", "message": "unknown key"}],
  "cannot_detect": "why no diagnostic is possible"         // documented limitation
}
```

Three of these earn their place rather than being decoration:

- **`counts`** stops an accept file passing vacuously. A decoder that returned an
  empty document for every input would otherwise satisfy "no error, no
  diagnostics" on all of them.
- **`positions`** is the only defence against a lon/lat transposition. Both
  orderings parse, both are in range, and nothing structural can tell them
  apart — the point lands 8000 km away and every check still passes. The values
  are JSON numbers in the sidecar so the comparison is between doubles, not
  between formatted text, which is also what makes the full-precision case work.
- **`moving_time_ms: null`** means UNKNOWN, which is not zero. A reader that
  answers 0 for a track carrying no pip times has answered a question it cannot
  answer, and its answer is indistinguishable from a track where nobody moved.

`message` is matched as a substring and `refusal` likewise: exact wording is a
reader's business, but a refusal that does not name its cause is a bug.

## Accept files are canonical — this is the layout contract

Every `accept/*.vnx` is stored in canonical form (§4.5), and the runner asserts
`canonicalise(file) == file` byte-for-byte.

**That check IS the byte-compatibility guarantee between the libraries.** Every
conforming writer, in every language, must emit these exact bytes for a given
document — a Go export, a Swift export and a Python export of one library are
identical files or one of them is wrong. An implementation whose formatter
differs in any respect fails here, so nobody has to trust anyone's reading of the
specification's prose.

What the layout is, in one place:

- Two-space indent, UTF-8, no BOM, newline at end of file.
- Object keys: scalars first (lexicographic), then containers (lexicographic),
  classified by the value in *this* object.
- Arrays never reorder.
- **An array holding only numbers and `null` is written on ONE line**, elements
  separated by `, ` — every position in the format. `null` counts, so a position
  does not change shape when an elevation goes missing. Six lines per position
  would make a day's recorded track fifty thousand lines; this makes it ten.
- Numbers render with ECMAScript `Number::toString` semantics — what
  `JSON.stringify` emits and what Go's `encoding/json` reproduces.

⚠ **No stock JSON encoder can emit this layout in any language** — measured
2026-09-01. Every implementation writes its own serialiser; the reference one is
155 lines. The key ordering alone rules the stock encoders out (`sort_keys` /
`.sortedKeys` give plain lexicographic, which puts `networks` before `vnx`), so
the inline-position rule costs nothing extra. Numbers are the other trap: Python
AND Swift both emit `340.0`/`1e-06`/`1e-07` where the reference emits
`340`/`0.000001`/`1e-7`, and Apple's `JSONSerialization` renders `-3.67` as
`-3.6699999999999999` and must not be used. Only Go and JS can lean on their
built-in number formatting. Details in spec §4.5.1.

The canonical check also does more than police layout: it is what proves the
whole file — including data records of types no implementation knows, whose
bodies nothing else inspects — survives parse and re-render at value level.

Regenerate after editing one:

```sh
cd go && go run ./cmd/vnx fmt --write ../corpus/accept/<file>.vnx
```

(Both commands here are run from the repository root, and both need Go. The
corpus itself does not — that is the point of it.)

## Rebuilding the archives

The `.vnz` files are committed as binaries, because an implementation in another
language cannot be asked to build a Go program before it can run the corpus.
`tools/makevnz` is committed beside them so the bytes stay reproducible and
reviewable rather than mysterious:

```sh
cd corpus && go run ./tools/makevnz
```

It writes its output relative to the working directory, so that directory must
be `corpus/`. The `go.mod` beside this file exists only so the go tool will run
it at all — see the comment in it.

## Why `format/` exists separately

The accept files cannot catch a formatter bug. They are stored canonical, so
canonicalising one only ever asks the renderer about values already spelled
correctly — a writer that renders negative zero as `-0`, or `0.000001` as
`1e-06`, is never asked the question. The reference implementation had exactly
the negative-zero bug and its own corpus was blind to it. The `format/` inputs
are written in the WRONG spellings on purpose, and each case asserts its input is
not already canonical so it cannot pass vacuously.

## What the corpus cannot do

`diagnose/duplicate-object-key.vnx` expects **no** diagnostic. Two `n1` entries
in one `nodes` map resolve last-wins in every mainstream parser, before any VNX
code runs, so a node vanishes and a standard parser offers no way to know. This
is documented rather than papered over (§6.4). If a reader ever gains the ability
to see it, that file goes red and the limitation gets revisited instead of
quietly outliving its cause.

## Never

Do not make a red corpus file green by editing the fixture. The fixture is the
instrument.
