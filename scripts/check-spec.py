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
    "IMPLEMENTING.md",
    "README.md",
    "corpus/README.md",
    "corpus/format/README.md",
    "corpus/numbers/README.md",
    "corpus/schema/README.md",
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

# A document in this repository never describes its own past.
#
# Nothing here has ever been public. There is no reader for whom a correction is
# news, no implementation built against an earlier wording, and no holder of a
# file written under one. A note like that exists only because the drafting was
# visible to whoever wrote it, and it reads as an apology to a reader who does
# not exist. State the rule, not the history of the rule.
#
# A rationale for why the FORMAT is this way is welcome and belongs in the spec.
# A rationale for why the DOCUMENT says it differently than it once did is not.
HISTORY = [
    (
        re.compile(r"\ban earlier (revision|draft|version)\b", re.IGNORECASE),
        "the document describing its own past.",
    ),
    (
        re.compile(r"\bformerly\b|\bpreviously\b|\bused to (be|say|read|have|do)\b", re.IGNORECASE),
        "the document describing its own past.",
    ),
    (
        re.compile(r"\bis now (consistent|correct|fixed)\b|\bwas corrected\b|\bhas been corrected\b", re.IGNORECASE),
        "a note whose subject is the document rather than the format.",
    ),
    (
        re.compile(r"\buntil 20\d\d-\d\d-\d\d\b|\bbefore 20\d\d-\d\d-\d\d\b", re.IGNORECASE),
        "a date marking when the document changed. A measured fact may carry a "
        "date; a change to this document may not.",
    ),
    (
        # A commit hash: 7 or more hex characters carrying BOTH a letter and a
        # digit, so a long decimal number is not mistaken for one. Every number
        # in SPEC.md's rendering tables would otherwise trip this.
        re.compile(
            # Not part of a longer token, and not part of a decimal number --
            # `1.4978982117220732e15` would otherwise match from after the dot,
            # because `e` is a hex digit.
            #
            # A FULL STOP MAY FOLLOW, and that is the whole point of the second
            # alternative: a hash is normally cited at the end of a sentence
            # ("deleted in 5dcf6ee."), and a blanket ban on a trailing `.` let
            # exactly that spelling through while catching the rarer mid-clause
            # one. A dot is only disqualifying when a digit or letter follows
            # it, which is what makes it part of a number rather than a
            # sentence.
            r"(?<![0-9a-zA-Z.])"
            r"(?=[0-9a-f]{7,40}(?![0-9a-zA-Z]|\.[0-9a-zA-Z]))"
            # A letter other than `e`, so scientific notation is not a hash.
            r"(?=[0-9a-f]*[abcdf])"
            r"(?=[0-9a-f]*[0-9])"
            r"[0-9a-f]{7,40}"
        ),
        "what looks like a commit hash. A published document does not cite the "
        "moment something changed.",
    ),
]

# Register markers. These are WARNINGS and not failures on purpose: every one has
# an innocent use, and it is the density that reads wrong rather than any single
# instance. A hard failure here would teach people to route around the check.
REGISTER_WARNINGS = [
    (
        re.compile(r"\*\*[^*\n]{1,80}\*\*"),
        "bold mid-sentence. Bold belongs on a defined term or on a line a "
        "skimming reader must not miss, not on a claim that wants shouting.",
    ),
    (
        re.compile(r" — |—"),
        "an em dash. Currently the most recognised marker of machine-written "
        "prose; most join two clauses a full stop or a colon joins better.",
    ),
    (
        # Capitals used to shout. RFC 2119 keywords, acronyms and the version
        # components are legitimate all-caps and are excluded by name, so the
        # count reflects emphasis rather than vocabulary.
        re.compile(
            r"\b(?!(?:MUST|SHOULD|SHALL|REQUIRED|OPTIONAL|"
            r"JSON|JSONC|ASCII|UTF|IEEE|RFC|BCP|STD|ZIP|APPNOTE|README|SPEC|"
            r"MAJOR|MINOR|PATCH|EOCD|NAN|VNX|VNZ|GPX|KML|GEOJSON|CLI|MVT|"
            r"TRUE|FALSE|NULL)\b)[A-Z]{4,}\b"
        ),
        "capitals used as emphasis.",
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
        fenced = False
        for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            if line.lstrip().startswith("```"):
                fenced = not fenced
                continue
            # HISTORY is a rule about PROSE. A fenced block is data -- a sample
            # of a file's own bytes, a command, a table of values -- and it says
            # nothing about how this document came to be. The exclusion is not a
            # convenience: a binary64 bit pattern is 16 hexadecimal digits, so
            # every row of corpus/numbers/numbers.tsv reads as a commit hash to
            # the pattern below, and the alternative to this line is either
            # weakening the hash rule for every document or forbidding the one
            # spelling in which that table can be quoted at all.
            #
            # LEAKAGE still applies inside a fence, because a home-directory
            # path or a knowledge-base reference leaks just as far in a command
            # as in a sentence.
            checks = LEAKAGE if fenced else LEAKAGE + HISTORY
            for pattern, why in checks:
                if match := pattern.search(line):
                    problems.append(f"{name}:{number}: {match.group(0)!r} - {why}\n    {line.strip()}")
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


def register_warnings() -> list[str]:
    """Density warnings, not failures.

    These do not stop a publish. Every pattern here has an innocent use, and it
    is the RATE that reads as machine-written rather than any single instance, so
    the report is per-document counts and a per-thousand-words rate. A hard
    failure would teach whoever is next in the file to route around the check.
    """
    lines: list[str] = []
    for name in PUBLISHED:
        path = ROOT / name
        if not path.exists():
            continue
        text = path.read_text(encoding="utf-8")
        words = max(len(text.split()), 1)
        hits = []
        for pattern, why in REGISTER_WARNINGS:
            n = len(pattern.findall(text))
            if n:
                hits.append((n, n * 1000 / words, why))
        if hits:
            lines.append(f"  {name} ({words} words)")
            for n, rate, why in sorted(hits, reverse=True):
                lines.append(f"    {n:4d}  {rate:5.1f} per 1000 words  {why}")
    return lines


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
    if warnings := register_warnings():
        print("\ncheck-spec: register warnings, which do not block a publish:")
        print("\n".join(warnings))


if __name__ == "__main__":
    main()
