#!/usr/bin/env python3
"""Generate SPEC.md from the authored format specification.

    ./scripts/gen-spec.py            write SPEC.md
    ./scripts/gen-spec.py --check    exit non-zero if SPEC.md is out of date

⚠ SPEC.md IS NOT EDITED BY HAND. Two hand-maintained copies of a format
definition is two definitions, and the one people implement against is whichever
they happened to open. The authored document lives in the knowledge base; this
turns it into the published one, and the transformation is small on purpose —
the published spec should differ from the authored one only where an internal
reference would be meaningless to a reader outside Viatrail.

⚠ IT FAILS RATHER THAN GUESSING. Two rules, both of them the same rule:

  * Every rewrite below must match. A rewrite that fires zero times means the
    source moved underneath it, and silently producing a SPEC.md that still
    carries the old text is exactly the failure this script exists to prevent.
  * Anything matching a FORBIDDEN pattern after rewriting stops the run. Those
    are references to documents and paths that do not exist in this repository.
    Publishing one is not a cosmetic defect: it points a reader implementing
    this format at something they cannot read.

So a new internal cross-reference in the authored spec breaks this script, and
that is the intended behaviour. The fix is a decision about what the published
spec should say — never a broader pattern here.
"""

import argparse
import pathlib
import re
import sys

SOURCE = pathlib.Path.home() / "src/knowledge/features/export/format-spec.md"
OUTPUT = pathlib.Path(__file__).resolve().parent.parent / "SPEC.md"

BANNER = """<!--
GENERATED FILE — DO NOT EDIT.

Produced by scripts/gen-spec.py from the authored specification. Edit that
source and re-run the script; an edit made here is lost the next time anybody
does. See the script's own comment for why there is only one definition.
-->
"""

# The authored document opens with its position in an internal roadmap: which
# spec number it is, which specs build on it, and a companion vision document
# holding the reasoning. None of that means anything to a reader outside
# Viatrail, and the vision document is not published. This is the one place the
# published spec says something the authored one does not.
PREAMBLE = """# VNX wire format specification

Version **0.1.0**. The format is 0.x and **unstable**: it changes without
compatibility guarantees until 1.0.

This document states the rules. **`corpus/` decides whether you have implemented
them** — a library conforms when it passes the corpus, and where prose and corpus
disagree, that disagreement is a finding rather than a matter of interpretation.

⚠ **The example document is not a specification.** Every place an example was
allowed to stand in for a stated rule produced a defect in the 2026-08-31 audit
of this document. If a rule you need is not written here, it is a gap — please
raise it rather than infer it from a file.
"""

# (description, old, new). Order matters: earlier rewrites see the original
# text. Each must fire at least once.
REWRITES = [
    (
        "the internal header block, down to the objective",
        """# VNX wire format specification

Status: **draft, 2026-08-31.** Spec 3 of the roadmap in
[format-vision.md](format-vision.md) §0, which is where to look for what the
other specs are and which have landed. It fixes the wire shape that spec 4 (Go
codec + conformance corpus), spec 5 (export engine) and spec 6 (export UI)
build against.

Every ruling here traces to [format-vision.md](format-vision.md) §7, which holds
the reasoning. This document states the rules; the vision says why. Where this
spec is silent, the vision is not a substitute — silence here is a gap to fill,
because **the example document is not a specification** and every place it was
allowed to stand in for one produced a defect in the 2026-08-31 audit.
""",
        PREAMBLE,
    ),
    (
        "the corpus location (§8.1) — it is at the repository root now",
        "It lives at `go/pkg/vnx/testdata/corpus/`, in three directories",
        "It lives at `corpus/`, at the root of this repository, in three directories",
    ),
    (
        "the canonical-form corpus reference (§4.5)",
        "`testdata/corpus/accept/` is stored in canonical form",
        "`corpus/accept/` is stored in canonical form",
    ),
    (
        "the formatter-case corpus reference (§8)",
        "**formatter cases** (`testdata/corpus/format/`)",
        "**formatter cases** (`corpus/format/`)",
    ),
    (
        "the schema-conformance test's package path (§8.2)",
        "`internal/vnxschema` holds a test",
        "`go/internal/vnxschema` holds a test",
    ),
]

# Patterns that must not survive into the published document. The message says
# what to do about each, because whoever trips one will not have this script's
# reasoning in front of them.
FORBIDDEN = [
    (
        re.compile(r"\bvision §"),
        "a reference to the vision document, which is not published. Either state "
        "the ruling in the spec itself or drop the citation.",
    ),
    (
        re.compile(r"\bspec \d"),
        "a reference to an internal spec by number. Readers outside Viatrail have "
        "no roadmap to resolve it against.",
    ),
    (
        re.compile(r"\]\((?!https?://|#)[^)]*\.md[^)]*\)"),
        "a link to another knowledge-base document, which does not exist here.",
    ),
    (
        re.compile(r"~/src/"),
        "a path on one person's machine.",
    ),
    (
        re.compile(r"\btestdata/corpus\b"),
        "the corpus's old home inside the Viatrail module; it is `corpus/` now.",
    ),
    (
        re.compile(r"before publication", re.IGNORECASE),
        "an edit the specification says it still needs. Publishing it as it "
        "stands would ship an admission that it is unfinished, or worse, would "
        "outlive the note and leave the gap unrecorded.",
    ),
    (
        re.compile(r"\*\*Open:"),
        "a question the specification marks as OPEN. A conformance corpus may "
        "already pin an answer to it, in which case six libraries will copy one "
        "implementation's choice as if it were the format. Settle it in the "
        "authored spec first.",
    ),
]


def generate(text: str) -> str:
    for description, old, new in REWRITES:
        if old not in text:
            sys.exit(
                f"gen-spec: the rewrite for {description} matched nothing.\n"
                "The authored specification has changed under this script. Read "
                "the source, decide what the published document should say, and "
                "update the rewrite — do not loosen it."
            )
        text = text.replace(old, new)

    problems = []
    for line_number, line in enumerate(text.splitlines(), start=1):
        for pattern, why in FORBIDDEN:
            if match := pattern.search(line):
                problems.append(f"  {line_number}: {match.group(0)!r} — {why}\n      {line.strip()}")
    if problems:
        sys.exit(
            "gen-spec: the specification is not publishable as it stands.\n"
            + "\n".join(problems)
            + "\n\nFix the authored source. Widening the checks in this script "
            "publishes the problem instead of solving it."
        )

    return BANNER + "\n" + text


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--check",
        action="store_true",
        help="verify SPEC.md matches the source; write nothing",
    )
    args = parser.parse_args()

    if not SOURCE.exists():
        sys.exit(f"gen-spec: no authored specification at {SOURCE}")

    generated = generate(SOURCE.read_text(encoding="utf-8"))

    if args.check:
        if not OUTPUT.exists():
            sys.exit(f"gen-spec: {OUTPUT.name} does not exist; run this script without --check")
        if OUTPUT.read_text(encoding="utf-8") != generated:
            sys.exit(f"gen-spec: {OUTPUT.name} is out of date; re-run this script")
        print(f"gen-spec: {OUTPUT.name} is up to date")
        return

    OUTPUT.write_text(generated, encoding="utf-8")
    print(f"gen-spec: wrote {OUTPUT.name} ({len(generated.splitlines())} lines)")


if __name__ == "__main__":
    main()
