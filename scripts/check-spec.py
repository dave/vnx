#!/usr/bin/env python3
"""Check the published documents in this repository for internal leakage.

    ./scripts/check-spec.py

Exits non-zero and names every problem it found.

This replaced scripts/gen-spec.py, which generated SPEC.md from an internal
knowledge-base document. That arrangement kept one definition of the format, but
it forced the published specification to carry the register of an internal
working note. SPEC.md is now authored directly, in RFC register, as the single
normative definition; the knowledge-base document holds the rationale and cites
SPEC.md rather than restating it.

The property the generator protected — one definition, and no internal
references escaping into a published document — is what this script keeps. It
checks the two things a script can decide:

  * A published document must not reference anything a reader outside Viatrail
    cannot reach: a knowledge-base document, a path on somebody's machine, an
    internal spec number, or a note that the document is unfinished.
  * The rationale document must not issue requirements. RFC 2119 keywords
    belong in SPEC.md. A rationale that states rules is a second definition of
    the format, which is the failure the generator existed to prevent.

Widening a check here publishes the problem instead of solving it.
"""

import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
RATIONALE = pathlib.Path.home() / "src/knowledge/features/export/format-spec.md"

PUBLISHED = [
    "SPEC.md",
    "README.md",
    "corpus/README.md",
    "corpus/format/README.md",
    "go/pkg/vnx/schema/README.md",
]

# (pattern, what to do about it). The message has to stand alone: whoever trips
# one of these will not have this script's reasoning in front of them.
LEAKAGE = [
    (
        re.compile(r"\bvision §"),
        "a reference to the internal vision document, which is not published. "
        "State the ruling here, or drop the citation.",
    ),
    (
        re.compile(r"\bspec \d"),
        "a reference to an internal spec by number. A reader outside Viatrail "
        "has no roadmap to resolve it against.",
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
        "a note that the document is still unfinished. Either make the edit or "
        "record the gap as an open item.",
    ),
]

# A markdown link whose target is a relative path. The target has to exist in
# this repository, or the document points a reader at something they cannot
# open — which is how a knowledge-base link gets published.
LINK = re.compile(r"\]\((?!https?://|#|mailto:)([^)#]+)")

# Standalone RFC 2119 keywords. Only meaningful in the normative document.
KEYWORD = re.compile(r"\b(MUST NOT|MUST|SHOULD NOT|SHOULD|REQUIRED|MAY|OPTIONAL|SHALL)\b")

# The register tells the specification is not allowed to carry.
REGISTER = [
    (re.compile(r"⚠"), "a warning sigil used as emphasis. A specification uses 'Note:' or nothing."),
    (re.compile(r"\byou\b|\byour\b", re.IGNORECASE), "second person. A specification addresses a reader or a writer, not 'you'."),
]


def check_published(problems: list[str]) -> None:
    for name in PUBLISHED:
        path = ROOT / name
        if not path.exists():
            problems.append(f"{name}: missing")
            continue
        for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            for pattern, why in LEAKAGE:
                if match := pattern.search(line):
                    problems.append(f"{name}:{number}: {match.group(0)!r} — {why}\n    {line.strip()}")
            for match in LINK.finditer(line):
                target = match.group(1).strip()
                if not (path.parent / target).exists():
                    problems.append(
                        f"{name}:{number}: link to {target!r}, which is not in this repository — "
                        "a published document must not point at a file a reader cannot open.\n"
                        f"    {line.strip()}"
                    )


def check_register(problems: list[str]) -> None:
    path = ROOT / "SPEC.md"
    if not path.exists():
        return
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        for pattern, why in REGISTER:
            if match := pattern.search(line):
                problems.append(f"SPEC.md:{number}: {match.group(0)!r} — {why}\n    {line.strip()}")


def check_rationale(problems: list[str]) -> None:
    if not RATIONALE.exists():
        print(f"check-spec: no rationale document at {RATIONALE}; skipping that check")
        return
    for number, line in enumerate(RATIONALE.read_text(encoding="utf-8").splitlines(), 1):
        if line.lstrip().startswith(">"):
            continue  # a quotation keeps its source's wording
        if match := KEYWORD.search(line):
            problems.append(
                f"format-spec.md:{number}: {match.group(0)!r} — the rationale document states a "
                "requirement. Requirements belong in SPEC.md; this document explains why they "
                "were made. Two documents stating rules is two definitions of the format.\n"
                f"    {line.strip()}"
            )


def main() -> None:
    problems: list[str] = []
    check_published(problems)
    check_register(problems)
    check_rationale(problems)

    if problems:
        sys.exit(
            "check-spec: the published documents are not publishable as they stand.\n\n"
            + "\n".join(problems)
            + "\n\nFix the document. Widening a check in this script publishes the "
            "problem instead of solving it."
        )
    print(f"check-spec: {len(PUBLISHED)} published documents are clean")


if __name__ == "__main__":
    main()
