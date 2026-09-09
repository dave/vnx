#!/usr/bin/env python3
"""Fail while a ruling has been made and has not reached SPEC.md.

    ./scripts/check-ruling-backlog.py

DELETE THIS SCRIPT AND ITS LEDGER WHEN THE BACKLOG EMPTIES. Both are a work
list, not a document, and a permanent record of what the specification used to
be missing is drafting history that this repository does not keep. The script
says so itself when the ledger runs dry.

WHAT IT IS FOR. A ruling and the specification text are separate artefacts with
a lag between them, so a library can be correct against one and wrong against
the other, and neither is obviously the authority at the moment somebody looks.
Two seats lost a ruling that way on 2026-09-09: one implemented a reading, the
ruling landed by message, and SPEC.md still read as it had — so checking the
spec confirmed the wrong answer with a clear conscience. That is not a problem
anyone can be careful enough to avoid; it is a problem a check can hold.

WHAT IT CHECKS. For each entry: the named SPEC.md section exists, and the phrase
the ruling requires appears inside that section and not merely somewhere in the
file. Section scoping is the point — `backslash` appearing in section 4.5.5
would say nothing about whether section 7 carries the container rule.

WHAT IT DELIBERATELY DOES NOT DO. It does not check that the text is CORRECT,
only that something answering to the ruling is present. A phrase is a tripwire,
not a proof, and widening it into a proof would mean encoding the specification
twice. Choose a phrase that could only appear if the rule were really stated.
"""

import pathlib
import re
import subprocess
import sys
import tempfile
import tomllib

ROOT = pathlib.Path(__file__).resolve().parent.parent
SPEC = ROOT / "SPEC.md"
LEDGER = pathlib.Path(__file__).resolve().parent / "ruling-backlog.toml"

HEADING = re.compile(r"^(#{2,6})\s+(\d+(?:\.\d+)*)\.?\s+(.*)$")


def sections(text: str) -> dict[str, str]:
    """Map a heading number to that section's body, excluding its subsections.

    A section ends at the next heading of ANY level, so `7` holds the prose
    under `## 7.` and not the prose under a `### 7.1`. A rule stated in a
    subsection is stated in that subsection, and an entry naming the parent
    would otherwise pass on text its reader might never reach.
    """
    found: dict[str, str] = {}
    current: str | None = None
    body: list[str] = []
    for line in text.splitlines():
        if match := HEADING.match(line):
            if current is not None:
                found[current] = "\n".join(body)
            current = match.group(2)
            body = []
        elif current is not None:
            body.append(line)
    if current is not None:
        found[current] = "\n".join(body)
    return found


SELF_TEST_SPEC = """# Spec

## 3. Structure

### 3.9 Rulesets

Rulesets are not defined in version 0.1.

## 7. The VNZ container

Some prose about a backslash goes here.

### 7.1 A subsection

This subsection mentions every other entry.
"""

SELF_TEST_CASES = [
    (
        "a landed ruling passes and asks for its entry to be deleted",
        '[[ruling]]\nid = "landed"\nsection = "7"\nrequires = "backslash"\n',
        0,
        "have landed",
    ),
    (
        "a phrase present only in a SUBSECTION still fails",
        '[[ruling]]\nid = "sub"\nsection = "7"\nrequires = "every other entry"\n',
        1,
        "RULED AND UNWRITTEN",
    ),
    (
        "a section that does not exist is named as such",
        '[[ruling]]\nid = "gone"\nsection = "9.9"\nrequires = "anything"\n',
        1,
        "has no section 9.9",
    ),
    (
        "an empty ledger says to delete the file",
        "\n",
        1,
        "DELETE BOTH THIS SCRIPT AND THE LEDGER",
    ),
]


def self_test() -> None:
    """Prove the check can fail, and can pass, before anyone trusts it.

    The second case is the one that matters: a phrase living in a SUBSECTION of
    the named section must still fail, or this is a whole-file grep wearing a
    section number.
    """
    failures = []
    with tempfile.TemporaryDirectory() as tmp:
        root = pathlib.Path(tmp)
        (root / "scripts").mkdir()
        (root / "SPEC.md").write_text(SELF_TEST_SPEC, encoding="utf-8")
        script = root / "scripts" / pathlib.Path(__file__).name
        script.write_text(pathlib.Path(__file__).read_text(encoding="utf-8"), encoding="utf-8")
        for label, ledger, want_code, want_text in SELF_TEST_CASES:
            (root / "scripts" / "ruling-backlog.toml").write_text(ledger, encoding="utf-8")
            done = subprocess.run(
                [sys.executable, str(script)], capture_output=True, text=True
            )
            output = done.stdout + done.stderr
            if done.returncode != want_code:
                failures.append(f"{label}: exit {done.returncode}, expected {want_code}")
            elif want_text not in output:
                failures.append(f"{label}: output does not mention {want_text!r}\n{output}")
            else:
                print(f"  ok  {label}")
    if failures:
        sys.exit("check-ruling-backlog --self-test FAILED\n\n" + "\n".join(failures))
    print(f"check-ruling-backlog: {len(SELF_TEST_CASES)} self-test cases pass")


def main() -> None:
    if "--self-test" in sys.argv[1:]:
        self_test()
        return
    if not LEDGER.exists():
        print("check-ruling-backlog: no ledger; nothing outstanding")
        return
    ledger = tomllib.loads(LEDGER.read_text(encoding="utf-8"))
    rulings = ledger.get("ruling", [])

    if not rulings:
        sys.exit(
            f"check-ruling-backlog: {LEDGER.name} is empty.\n\n"
            "The backlog has emptied, so DELETE BOTH THIS SCRIPT AND THE LEDGER, "
            "and remove any invocation of it. They are a work list, and a "
            "permanent ledger of what the specification used to be missing is "
            "drafting history."
        )

    if not SPEC.exists():
        sys.exit(f"check-ruling-backlog: {SPEC} is missing")
    found = sections(SPEC.read_text(encoding="utf-8"))

    problems: list[str] = []
    landed: list[str] = []
    for ruling in rulings:
        rid = ruling.get("id", "<no id>")
        section = str(ruling.get("section", ""))
        phrase = ruling.get("requires", "")
        statement = " ".join(ruling.get("statement", "").split())
        if not section or not phrase:
            problems.append(f"{rid}: the entry names no section or no required phrase")
            continue
        if section not in found:
            problems.append(
                f"{rid}: SPEC.md has no section {section}. Either the ruling names the "
                "wrong section, or the section it was going into has been renumbered."
            )
            continue
        if phrase.lower() in found[section].lower():
            landed.append(f"  {rid}  -> section {section} now carries {phrase!r}")
        else:
            problems.append(
                f"{rid}: RULED AND UNWRITTEN. Section {section} does not contain "
                f"{phrase!r}.\n      {statement}"
            )

    if landed:
        print(
            "check-ruling-backlog: these rulings have landed and their entries should "
            "now be DELETED from the ledger:\n" + "\n".join(landed) + "\n"
        )

    if problems:
        sys.exit(
            f"check-ruling-backlog: {len(problems)} ruling(s) have been made and are not "
            "in SPEC.md.\n\n"
            + "\n\n".join(problems)
            + "\n\nWrite the text. Do not weaken a required phrase to make this pass — "
            "that publishes the problem instead of solving it. When the last entry "
            "goes, delete the ledger and this script."
        )
    print(
        f"check-ruling-backlog: all {len(rulings)} entries have landed. "
        "Empty the ledger, then delete it and this script."
    )


if __name__ == "__main__":
    main()
