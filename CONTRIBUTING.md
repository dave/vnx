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
| [`scripts/`](scripts/) | `check-spec.py`, which checks the published documents for dead links and register defects, and that every library carries the `SPEC.md` version. |

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
| Go | `cd go && go test -count=1 ./...` |
| Python | `cd python && python3 -m unittest discover -s tests -t .` |
| JavaScript | `cd js && npm test` |
| Rust | `cd rust && cargo test` |
| Kotlin | `cd kotlin && ./gradlew test` |
| Swift | `cd swift && swift test` |

Go needs no setup. Python needs 3.11 or later, JavaScript Node 22 or later,
Rust 1.77 or later, Swift 5.9 or later, and a JDK 17 for Kotlin. Each Gradle
wrapper fetches Gradle, not a JDK.

To check the documents:

```sh
python3 scripts/check-spec.py
```

`check-spec.py` also runs `scripts/check-format.sh`, so it fails when a source
file is not formatted.

## Format the code

Every source file is in its formatter's default style. After an edit, run the
formatter for that language:

| Language | Formatter | Command |
|---|---|---|
| Go | gofmt | `gofmt -w <file>` |
| Rust | rustfmt | `cd rust && cargo fmt` |
| Python | ruff | `ruff format <file>` |
| JavaScript | prettier, pinned in `js/package.json` | `js/node_modules/.bin/prettier --config js/.prettierrc.json --write <file>` |
| Kotlin | ktlint, pinned in `kotlin/build.gradle.kts` | `cd kotlin && ./gradlew ktlintFormat` |
| Swift | swift-format, from the Xcode toolchain | `xcrun swift-format format --in-place <file>` |

To check every file at once:

```sh
./scripts/check-format.sh
```

It changes nothing, names each file that is not formatted, and exits non-zero
if there is one. It needs every formatter above installed, prettier through
`cd js && npm ci`, and `JAVA_HOME` set to a JDK 17 for ktlint. A formatter
that is missing fails the check rather than skipping its language.

A commit that reformats code carries formatting only, under
`style(<scope>): format with <tool>`.

## Versions and releases

The format and the six libraries carry one version number, in the
`MAJOR.MINOR.PATCH` form of [Semantic Versioning](https://semver.org). The
`Version` line at the top of [`SPEC.md`](SPEC.md) is that number. Every package
manifest carries it, and so does every library constant that names the format
version the library reads or writes.

`scripts/set-version.sh` is the only way to change the version. It writes the
`SPEC.md` version line, table row and series sentence, every manifest and every
library constant, then runs `check-spec.py`:

```sh
./scripts/set-version.sh 0.9.1
```

It does not change the version that a corpus document or a test fixture
declares. Those are data, and they are a separate step:

- A PATCH bump needs no other change. Every fixture stays in its series, and a
  reader accepts any PATCH of its own series.
- A MINOR or MAJOR bump needs a sweep of every corpus document, every test
  fixture and every `SPEC.md` example, because a reader refuses a document from
  another series. A fixture that tests the version rule keeps its meaning: a
  newer minor stays one minor above, an older series stays older, and a
  malformed version stays a malformed spelling of the current series. The
  `.vnz` archives are rebuilt from `corpus/tools/`, or patched in place with
  their CRC-32 where no generator makes them.

A release bumps the version in one commit and tags that commit twice:

```sh
git tag -a v0.9.0 -m "VNX 0.9.0"      # the repository root, which SwiftPM resolves
git tag -a go/v0.9.0 -m "VNX 0.9.0"   # the Go module in go/, whose tags carry its directory
```

A change to one library alone, with no change to the format, is a PATCH bump of
everything: the format version moves with it, and a reader accepts any PATCH of
its own series ([`SPEC.md`](SPEC.md) section 2.2).

`python3 scripts/check-spec.py`, the check that also runs every formatter,
fails when a manifest or a library constant differs from the `SPEC.md` version.
`.githooks/pre-commit` runs the same version check on the staged files and
refuses a commit whose versions disagree; `git config core.hooksPath .githooks`
enables it, as for the commit-message hook.

## Commit messages

`type(scope): what the change makes true`, in the present tense, lower case, no
full stop.

The type is one of `feat`, `fix`, `test`, `refactor`, `style`, `docs` or `spec`. The
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

Commit messages name no tool and no assistant. `.githooks/commit-msg` rejects a
`Co-Authored-By`, `Claude-Session` or `Generated with` trailer; enable it once
per clone with:

```sh
git config core.hooksPath .githooks
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
