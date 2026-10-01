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

It also checks that every package manifest and every library's format-version
constant carries the `SPEC.md` version, because CONTRIBUTING.md makes them one
number.

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
    "corpus/write/README.md",
    "corpus/schema/README.md",
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
KEYWORD = re.compile(
    r"\b(MUST NOT|MUST|SHOULD NOT|SHOULD|REQUIRED|MAY|OPTIONAL|SHALL)\b"
)

# The register tells the specification is not allowed to carry.
# A reader arrives today and has seen no earlier version of this repository.
# Prose that refers to one is addressed to somebody who is not reading it. This
# is a hard failure rather than a warning because every instance found so far
# reached a published document and had to be removed by hand afterwards.
HISTORY = [
    (
        re.compile(
            r"\bno longer\b|\bany more\b|(?<!be )(?<!is )(?<!are )(?<!was )"
            r"(?<!were )(?<!been )\bused to\b|"
            r"\bpreviously\b|\bearlier version\b|\bhas since\b|"
            r"\bhave disagreed\b|\bhas disagreed\b|\bup until\b"
        ),
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
    (
        re.compile(r"⚠"),
        "a warning sigil used as emphasis. A specification uses 'Note:' or nothing.",
    ),
    (
        re.compile(r"\byou\b|\byour\b", re.IGNORECASE),
        "second person. A specification addresses a reader or a writer, not 'you'.",
    ),
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
                    problems.append(
                        f"{name}:{number}: {match.group(0)!r} - {why}\n    {line.strip()}"
                    )
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
                problems.append(
                    f"SPEC.md:{number}: {match.group(0)!r} — {why}\n    {line.strip()}"
                )


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
    for number, line in enumerate(
        RATIONALE.read_text(encoding="utf-8").splitlines(), 1
    ):
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


def check_diagnose_codes(problems: list[str]) -> None:
    """Every code a diagnose or write case pins is a row of SPEC section 6.2's table.

    Section 6.2 lists every problem a document can have, with the code a reader
    MUST put on it. A corpus case pinning a code that table does not name is a
    case no implementer can act on from the specification, which is the
    failure CONTRIBUTING.md names: a corpus case without a spec sentence
    behind it.

    The table is found by its header row, and a table that is not found is
    itself a problem: an empty set of codes would pass nothing silently, it
    would fail every case, and the message would blame the cases.
    """
    spec = (ROOT / "SPEC.md").read_text(encoding="utf-8")
    start = spec.find("### 6.2 Error handling")
    end = spec.find("### 6.3", start)
    if start < 0 or end < 0:
        problems.append(
            "SPEC.md: section 6.2 not found, so its codes could not be checked"
        )
        return
    codes = set()
    in_table = False
    for line in spec[start:end].splitlines():
        if line.startswith("| Construct | Remedy | Code |"):
            in_table = True
            continue
        if in_table:
            if not line.startswith("|"):
                break
            cells = [c.strip() for c in line.strip("|").split("|")]
            codes.update(re.findall(r"`([a-z-]+)`", cells[-1]))
    if not codes:
        problems.append("SPEC.md: no remedy table with a Code column in section 6.2")
        return
    checked = 0
    sidecars = sorted((ROOT / "corpus" / "diagnose").glob("*.expect.json"))
    sidecars += sorted((ROOT / "corpus" / "write").glob("*.expect.json"))
    for sidecar in sidecars:
        for d in json.loads(sidecar.read_text(encoding="utf-8")).get("diagnostics", []):
            checked += 1
            if d.get("code") not in codes:
                problems.append(
                    f"{sidecar.relative_to(ROOT)}: pins code {d.get('code')!r}, which is not a row of "
                    "SPEC.md section 6.2's remedy table"
                )
    if checked == 0:
        problems.append(
            "corpus/diagnose: no pinned diagnostic was found, so the code check measured nothing"
        )


# Every place that states the version. CONTRIBUTING.md: the format and all six
# libraries carry ONE version number. Each entry is (file, pattern, what, kind).
# The pattern's groups hold the version, and the kind says how:
#
#   version  one group, MAJOR.MINOR.PATCH
#   series   one group, MAJOR.MINOR
#   pair     two groups, MAJOR and MINOR as bare integers
#   bigint   three groups, Kotlin BigInteger expressions
#
# check_versions() compares every entry after the first with the first, the
# SPEC.md Version line. scripts/set-version.sh writes every entry, the first
# included, through write_versions(), so the script and the check read one list
# and cannot disagree. An anchor that matches nothing is a failure, not a pass:
# a check that found no version has compared nothing.
VERSION_SITES = [
    ("SPEC.md", r"^Version (\d+\.\d+\.\d+)$", "Version line", "version"),
    (
        "SPEC.md",
        r"^\| Current version \| `([^`]+)` \|$",
        "Current version table row",
        "version",
    ),
    (
        "SPEC.md",
        r"^A reader of this version supports the (\d+\.\d+) series only\.$",
        "supported-series sentence",
        "series",
    ),
    ("js/package.json", r'^  "version": "([^"]+)"', "npm package version", "version"),
    (
        "js/package-lock.json",
        r'^  "version": "([^"]+)"',
        "npm lock, top level",
        "version",
    ),
    (
        "js/package-lock.json",
        r'^    "": \{\n      "name": "@viatrail/vnx",\n      "version": "([^"]+)"',
        "npm lock, root package",
        "version",
    ),
    (
        "js/src/decode.js",
        r"^export const READER_MAJOR = (\d+);\nexport const READER_MINOR = (\d+);",
        "JS reader MAJOR.MINOR",
        "pair",
    ),
    (
        "python/pyproject.toml",
        r'^version = "([^"]+)"',
        "Python package version",
        "version",
    ),
    (
        "python/vnx/__init__.py",
        r'^__version__ = "([^"]+)"',
        "Python __version__",
        "version",
    ),
    (
        "python/vnx/decode.py",
        r"^SUPPORTED_VERSION = \((\d+), (\d+)\)",
        "Python reader MAJOR.MINOR",
        "pair",
    ),
    (
        "python/vnx/model.py",
        r'^    vnx: str = "([^"]+)"',
        "Python Document default version",
        "version",
    ),
    (
        "kotlin/build.gradle.kts",
        r'^version = "([^"]+)"',
        "Kotlin artifact version",
        "version",
    ),
    (
        "kotlin/src/main/kotlin/app/viatrail/vnx/Version.kt",
        r"val SUPPORTED = Version\((BigInteger\.\w+(?:\(\d+\))?), (BigInteger\.\w+(?:\(\d+\))?), (BigInteger\.\w+(?:\(\d+\))?)\)",
        "Kotlin reader version",
        "bigint",
    ),
    (
        "scripts/interop/kotlin/build.gradle.kts",
        r'implementation\("app\.viatrail:vnx:([^"]+)"\)',
        "interop dependency on the Kotlin library",
        "version",
    ),
    (
        "rust/Cargo.toml",
        r'^\[package\]\nname = "vnx"\nversion = "([^"]+)"',
        "Rust crate version",
        "version",
    ),
    (
        "rust/src/model.rs",
        r"^pub const SUPPORTED: \(u64, u64\) = \((\d+), (\d+)\);",
        "Rust reader MAJOR.MINOR",
        "pair",
    ),
    (
        "go/pkg/vnx/vnx.go",
        r'^const Version = "([^"]+)"',
        "Go package Version",
        "version",
    ),
    (
        "swift/Sources/VNX/VNX.swift",
        r'^  public static let version = "([^"]+)"',
        "Swift VNX.version",
        "version",
    ),
]

VERSION = re.compile(r"\d+\.\d+\.\d+")
BIGINTEGER = re.compile(r"BigInteger\.(?:(ZERO|ONE|TWO|TEN)|valueOf\((\d+)\))")
BIGINTEGER_NAMED = {"ZERO": "0", "ONE": "1", "TWO": "2", "TEN": "10"}


def read_site(groups: tuple[str, ...], kind: str) -> str:
    """The version a site states, spelled as its kind spells it."""
    if kind == "bigint":
        numbers = []
        for g in groups:
            named, value = BIGINTEGER.fullmatch(g).groups()
            numbers.append(BIGINTEGER_NAMED[named] if named else value)
        return ".".join(numbers)
    return ".".join(groups)


def render_site(version: str, kind: str) -> list[str]:
    """The group values that make a site of this kind state `version`."""
    major, minor, patch = version.split(".")
    if kind == "version":
        return [version]
    if kind == "series":
        return [f"{major}.{minor}"]
    if kind == "pair":
        return [major, minor]
    if kind == "bigint":
        named = {v: k for k, v in BIGINTEGER_NAMED.items()}
        return [
            f"BigInteger.{named[n]}" if n in named else f"BigInteger.valueOf({n})"
            for n in (major, minor, patch)
        ]
    raise ValueError(f"unknown version site kind {kind!r}")


def want_for(version: str, kind: str) -> str:
    return read_site(tuple(render_site(version, kind)), kind)


def check_versions(problems: list[str]) -> None:
    """The format and every library carry one version (CONTRIBUTING.md)."""
    found_at = []
    for rel, pattern, what, kind in VERSION_SITES:
        text = (ROOT / rel).read_text(encoding="utf-8")
        found = re.search(pattern, text, re.MULTILINE)
        if not found:
            problems.append(
                f"{rel}: the {what} was not found, so it was not compared with SPEC.md"
            )
            if not found_at:
                return
            continue
        found_at.append((rel, what, kind, read_site(found.groups(), kind)))
    (_, _, _, version), *rest = found_at
    for rel, what, kind, got in rest:
        if got != want_for(version, kind):
            problems.append(
                f"{rel}: the {what} is {got} and SPEC.md is {version}. The format and all "
                "six libraries carry one version; change it with scripts/set-version.sh."
            )


def write_versions(version: str) -> None:
    """Write `version` to every site. scripts/set-version.sh calls this."""
    if not VERSION.fullmatch(version):
        sys.exit(f"set-version: {version!r} is not MAJOR.MINOR.PATCH")
    for rel, pattern, what, kind in VERSION_SITES:
        path = ROOT / rel
        text = path.read_text(encoding="utf-8")
        found = list(re.finditer(pattern, text, re.MULTILINE))
        if len(found) != 1:
            sys.exit(
                f"set-version: {rel}: the {what} matched {len(found)} times, not once"
            )
        match, values = found[0], render_site(version, kind)
        for i in reversed(range(len(values))):
            text = text[: match.start(i + 1)] + values[i] + text[match.end(i + 1) :]
        path.write_text(text, encoding="utf-8")


def check_format(problems: list[str]) -> None:
    """Every source file is in its formatter's style (CONTRIBUTING.md).

    The formatters are not Python and not this script's to reimplement, so
    this runs scripts/check-format.sh and reports what it reports.
    """
    done = subprocess.run(
        [str(ROOT / "scripts" / "check-format.sh")],
        capture_output=True,
        text=True,
        cwd=ROOT,
    )
    if done.returncode != 0:
        problems.append((done.stderr.strip() or done.stdout.strip()))


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
    if sys.argv[1:2] == ["--write-version"] and len(sys.argv) == 3:
        write_versions(sys.argv[2])
        return
    problems: list[str] = []
    check_ruling_backlog(problems)
    check_diagnose_codes(problems)
    check_versions(problems)
    check_format(problems)
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
