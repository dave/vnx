# Contributing to VNX

How this repository is laid out, how to run each library's tests, and the
standard a change is held to. Start at [`README.md`](README.md) for what the
format is.

## Repository layout

| Path | What it is |
|---|---|
| [`SPEC.md`](SPEC.md) | The format definition. Normative. Nothing else states a rule. |
| [`corpus/`](corpus/) | The conformance corpus: the files every implementation must read, refuse or produce. |
| [`go/`](go/) | The reference implementation, and the `vnx` command-line tool under `go/cmd/vnx`. |
| [`python/`](python/), [`js/`](js/), [`rust/`](rust/), [`kotlin/`](kotlin/), [`swift/`](swift/) | The five other libraries. Each is a standalone build with no dependency on the others. |
| [`IMPLEMENTING.md`](IMPLEMENTING.md) | What a seventh implementation has to get right, and what it costs. |
| [`scripts/`](scripts/) | `check-spec.py`, which checks the published documents for dead links and register defects. |

## The corpus decides conformance

`SPEC.md` states the rules; [`corpus/`](corpus/) decides whether a library obeys
them. A library ships when it passes the corpus, and that is the whole bar.

A change to the format is therefore three changes together: `SPEC.md`, the
corpus case that pins the new behaviour, and the six libraries that have to pass
it. A corpus case added without a spec sentence behind it is a case no
implementer can act on.

## Run the tests

Each command runs that language's unit tests and its corpus harness.

| Language | Command |
|---|---|
| Go | `cd go && go test ./...` |
| Python | `cd python && python3 -m unittest discover -s tests -t .` |
| JavaScript | `cd js && npm test` |
| Rust | `cd rust && cargo test` |
| Kotlin | `cd kotlin && ./gradlew test` |
| Swift | `cd swift && swift test` |

Go needs no setup. Python needs 3.11 or later, JavaScript Node 22 or later,
Rust 1.77 or later, and Swift 5.9 or later; the Kotlin build fetches its own
toolchain through the wrapper.

To check the documents:

```sh
python3 scripts/check-spec.py
```

## Commit messages

`type(scope): what the change makes true`, in the present tense, lower case, no
full stop.

The type is one of `feat`, `fix`, `test`, `refactor`, `docs` or `spec`. The
scope is the directory the change lands in — `go`, `python`, `js`, `rust`,
`kotlin`, `swift`, `corpus` — and `spec:` takes no scope.

The subject states the fact the commit establishes, not the activity performed.
Prefer "the corpus positions check compares bit patterns, not values" over
"update positions check". Backticks around an identifier are welcome.

```
test(js): attachment order is not a rule, so the write tier stops checking it
fix(kotlin): the independent-reader test pinned the writer's entry order too
spec: negative zero collapses to positive zero, restored to 4.1
```

## Documentation

Documentation here is written for the person who wants to USE a library, not for
the person who built it. Most defects in these pages come from the second
audience displacing the first.

**The three-tier rule** decides what a page may say about the format.

- **Tier A — language-shaped.** Which type is raised, which call applies a rule,
  which dependency supplies a capability, what the manifest line is. Restate
  freely, and cite the section it implements.
- **Tier B — a bare normative quantity, threshold, keyword list or obligation.**
  "6 decimal places", "store and deflate only", "ties round to even". Cite
  `SPEC §x.y`; never restate. One correct value means a second copy is a second
  thing to get wrong.
- **Tier C — rationale, history, war story.** These belong in
  [`IMPLEMENTING.md`](IMPLEMENTING.md), once, in general form. A per-language
  page may carry a two-sentence pointer under `## Notes for porters`.

Two tests settle the tier. If a plausible change to `SPEC.md` or to the corpus
would make the sentence wrong, it is Tier B: make it a citation. If the same
sentence would be true in two per-language READMEs, it is not a per-language
fact: move it up to the root `README.md`.

**Fifteen checks. A page failing any of these is not finished.**

1. First sentence under 40 words: what this is, who it is for.
2. `grep '^## '` reads as a table of contents — sentence case, descriptive, no
   essay titles. ("Where Rust fights the format" is an essay title. "Rounding"
   is a heading.)
3. Heading order is non-increasing in reader need. For each heading in order,
   estimate the share of readers who need it; the sequence must not go up.
4. At most two Diátaxis modes on one page: tutorial, how-to, reference,
   explanation.
5. On a library page, the first runnable fenced block appears within 120 words
   of the top.
6. `## Install` is present on a library page, as its third element.
7. No `## What it does not do` section, and no statement of absence in the top
   30% of a page. The one legitimate boundary lives in the root README's
   `## Not in scope`.
8. No `any more`, `still`, `used to`, `no longer`, `originally`, `already`,
   `now`, `these days`. No dates, no commit hashes, no personal names, no paths
   into a private codebase. The reader arrived today and has seen no earlier
   version.
9. Every normative number is either cited to `SPEC §x.y` or is a fact about this
   language. Never both stated and owned.
10. No rationale in a reference section. Every "why" is a link.
11. No paragraph over six sentences.
12. No first person. `we`, `our`, `us` do not appear. The format has rules, the
    library has behaviour, neither has opinions.
13. Each top-level section's reader is obvious from its heading.
14. Contributor material on a library page is limited to `## Run the tests` and
    `## Notes for porters`.
15. All six per-language READMEs produce the same `grep '^## '` list.

**Register.** Say what is true, not what is not: "two conforming writers can
produce different bytes for the same document", never "the bytes are not
pinned". Never quote a number a file declares for itself — point at the file, or at
a command that prints it. A measured claim names what was measured and carries
the command that reproduces it, or it does not appear.

**Verify every command you write by running it.** A command in a document that
has not been run is a defect.
