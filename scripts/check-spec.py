#!/usr/bin/env python3
"""Check the published documents in this repository for internal leakage.

⚠ WHAT A CLEAN REPORT FROM THIS SCRIPT MEANS, AND WHAT IT DOES NOT.

It means: no forbidden spelling, no unbalanced emphasis marker, and acceptable
register densities. It has never meant the documents are correct, and four
classes of defect are outside it by construction. Every one below was found by a
person reading, after this script passed the same files clean:

  * A CONTRADICTION BETWEEN TWO DOCUMENTS. The guide said no tier reaches the
    writer while corpus/write/README.md said the opposite, at one commit.
  * A MISQUOTATION OF SPEC.md. The guide quoted a section as naming two
    normative anchors where it names one.
  * A PATH THAT DOES NOT EXIST in the tree.
  * A COUNT THAT DISAGREES WITH ITSELF. Three documents gave three different
    numbers of corpus directories and none matched the tree.

Checking any of those means encoding the documents' content a second time, which
is a second thing to get wrong. They need a reader, and saying so here is worth
more than implying a coverage this cannot have.


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

import json
import pathlib
import subprocess
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
RATIONALE = pathlib.Path.home() / "src/knowledge/features/export/format-spec.md"

# Every Markdown file a reader of this repository can open. The six language
# READMEs sit here because they are the pages most readers reach first, and
# because the leaks this script exists to catch — a date, a commit hash, a path
# into a private codebase — were found in four of them by a person reading.
PUBLISHED = [
    "SPEC.md",
    "IMPLEMENTING.md",
    "README.md",
    "CONTRIBUTING.md",
    "corpus/README.md",
    "corpus/numbers/README.md",
    "corpus/write/README.md",
    "go/README.md",
    "python/README.md",
    "js/README.md",
    "rust/README.md",
    "kotlin/README.md",
    "swift/README.md",
]

# (pattern, what to do about it). The message has to stand alone: whoever trips
# one of these will not have this script's reasoning in front of them.
# Named, because the sidecar scan excludes this one deliberately; see below.
SPEC_NUMBER = re.compile(r"\bspec \d")

LEAKAGE = [
    (
        re.compile(r"\bvision §"),
        "a reference to the internal vision document, which is not published. "
        "State the ruling here, or drop the citation.",
    ),
    (
        SPEC_NUMBER,
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
# A reader arrives today and has seen no earlier version of this repository.
# Prose that refers to one is addressed to somebody who is not reading it. This
# is a hard failure rather than a warning because every instance found so far
# reached a published document and had to be removed by hand afterwards.
HISTORY = [
    (
        re.compile(r"\bno longer\b|\bany more\b|(?<!be )(?<!is )(?<!are )(?<!was )"
                   r"(?<!were )(?<!been )\bused to\b|"
                   r"\bpreviously\b|\bearlier version\b|\bhas since\b|"
                   r"\bhave disagreed\b|\bhas disagreed\b|\bup until\b"),
        "a reference to a state of this project the reader has never seen. "
        "State what is true now. If the change itself matters to a reader, it "
        "belongs in a changelog, not in the prose.",
    ),
    (
        re.compile(r"\b20\d\d-\d\d-\d\d\b|\bRuled \d|\bMeasured 20\d\d"),
        "a date. A published document describes the format, not the day a "
        "decision was taken.",
    ),
]

REGISTER = [
    (re.compile(r"⚠"), "a warning sigil used as emphasis. A specification uses 'Note:' or nothing."),
    (re.compile(r"\byou\b|\byour\b", re.IGNORECASE), "second person. A specification addresses a reader or a writer, not 'you'."),
]


def check_markdown_structure(problems: list[str]) -> None:
    """Unbalanced emphasis markers, which the density warnings cannot see.

    ⚠ A CLEAN REPORT FROM THE REST OF THIS SCRIPT MEANS "NO FORBIDDEN SPELLINGS
    AND ACCEPTABLE DENSITIES". It has never meant "well-formed". A deletion that
    took the closing marker of a bold span with it left the orphaned opener
    swallowing the next heading, so a section existed with no heading and every
    span after it paired wrongly — and this script passed the file clean twice,
    reporting two numbers that were both true and both measured on a document
    with a missing heading.

    Three shapes, all structural rather than stylistic: an odd number of `**` in
    a paragraph, a heading carrying one, and a span crossing a blank line.
    """
    for name in PUBLISHED:
        path = ROOT / name
        if not path.exists():
            continue
        line_no = 1
        for para in path.read_text(encoding="utf-8").split("\n\n"):
            lines = para.split("\n")
            if para.count("**") % 2:
                problems.append(
                    f"{name}:{line_no}: an odd number of `**` in one paragraph, so a bold "
                    f"span is unclosed and will swallow whatever follows it\n"
                    f"    {lines[0].strip()[:90]}"
                )
            for offset, line in enumerate(lines):
                if line.lstrip().startswith("#") and "**" in line:
                    problems.append(
                        f"{name}:{line_no + offset}: a heading carrying `**`, which is what an "
                        f"unclosed span upstream looks like once it has eaten one\n    {line.strip()[:90]}"
                    )
            line_no += len(lines) + 1


def check_sidecar_notes(problems: list[str]) -> None:
    """The corpus ships with the format, so a `note` is published prose.

    ⚠ THESE ARE NOT IN PUBLISHED AND WERE SCANNED BY NOTHING. A sidecar note is
    read by every implementer running the corpus and by anyone reading the files
    to understand a case, which is more readers than some sections of the spec
    get. The leakage and history rules apply to them for the same reason.
    """
    for sidecar in sorted((ROOT / "corpus").rglob("*.expect.json")):
        try:
            note = json.loads(sidecar.read_text(encoding="utf-8")).get("note", "")
        except json.JSONDecodeError as err:
            problems.append(f"{sidecar.relative_to(ROOT)}: not valid JSON: {err}")
            continue
        # ⚠ THE SPEC-NUMBER PATTERN IS EXCLUDED HERE, and measuring before
        # shipping this scanner is why. In the published Markdown a bare
        # "spec 4" is a reference to an internal roadmap, which is what that
        # pattern was written for. In a sidecar note it is the established
        # shorthand for a SECTION of this specification -- "spec 4.1", "spec 8"
        # -- and 19 of the 21 hits on first run were that. A check that flags
        # nineteen correct references to flag two real ones is a check people
        # route around, which is worse than no check.
        for pattern, why in [r for r in LEAKAGE if r[0] is not SPEC_NUMBER]:
            if match := pattern.search(note):
                problems.append(
                    f"{sidecar.relative_to(ROOT)}: {match.group(0)!r} - {why}\n"
                    f"    in the case's `note`, which ships with the corpus"
                )


def check_published(problems: list[str]) -> None:
    for name in PUBLISHED:
        path = ROOT / name
        if not path.exists():
            problems.append(f"{name}: missing")
            continue
        for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            for pattern, why in LEAKAGE:
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


def check_history(problems: list[str]) -> None:
    """Every published document, not only the specification.

    CONTRIBUTING.md states this rule, so its own statement of the forbidden
    words would trip the scan. A fenced block is skipped for that reason, which
    is also why the rule is quoted inside one there.
    """
    for name in PUBLISHED:
        path = ROOT / name
        if not path.exists():
            continue  # check_published already reports it
        fenced = False
        for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            if line.lstrip().startswith("```"):
                fenced = not fenced
                continue
            if fenced:
                continue
            bare = re.sub(r"`[^`]*`", "", line)
            for pattern, why in HISTORY:
                if match := pattern.search(bare):
                    problems.append(
                        f"{name}:{number}: {match.group(0)!r} — {why}\n    {line.strip()}"
                    )


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


def check_ruling_backlog(problems: list[str]) -> None:
    """Fail while a ruling has been made and has not reached SPEC.md.

    A ruling and the spec text are separate artefacts with a lag between them, so
    a library can be correct against one and wrong against the other, and neither
    is obviously the authority at the moment somebody looks. Checking the spec
    then confirms the wrong answer with a clear conscience.

    ⚠ THE SCRIPT IS BUILT TO BE DELETED. It exits non-zero when the ledger is
    empty AND still present, so removing it is enforced rather than remembered —
    a permanent ledger of what this document used to be missing would be exactly
    the drafting history the repository forbids, arriving wearing a check, which
    is the last place anyone looks for a register violation.
    """
    script = ROOT / "scripts" / "check-ruling-backlog.py"
    if not script.exists():
        return
    done = subprocess.run(
        [sys.executable, str(script)], capture_output=True, text=True, cwd=ROOT
    )
    if done.returncode != 0:
        problems.append(done.stdout.strip() or done.stderr.strip())


def main() -> None:
    problems: list[str] = []
    check_ruling_backlog(problems)
    check_sidecar_notes(problems)
    check_markdown_structure(problems)
    check_published(problems)
    check_register(problems)
    check_history(problems)
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
