#!/usr/bin/env python3
"""Cross-implementation interop matrix: every writer against every reader.

Success criterion 5 of the simplification spec is that a document written by
any implementation reads back equal in every other. Each of the six
implementations has been certified against the shared corpus on its own, which
proves each one agrees with the corpus. It does not prove they agree with each
other, and until this script existed nothing measured that.

The measurement is a 6x6 matrix over `scripts/interop/source.vnx`:

  - Every implementation reads the source and writes it back out. That is the
    writer output, one per language, six documents.
  - Every implementation reads every writer output and writes THAT back out.
    Cell (writer, reader) compares the writer output against what the reader
    reproduced from it. Thirty-six cells, the diagonal being each
    implementation round-tripping its own output.

Comparison is over PARSED JSON TREES, by value, never over rendered text.
Section 8.1 of SPEC.md is the authority: key order, number spelling and
whitespace are the writer's own business. Three measured reasons a text
comparison would report false failures:

  - Python writes an integral double as `1.0` where Go writes `1`.
  - Swift's JSONEncoder writes `0.000001` as `1e-06` and `1e-7` as `1e-07`.
  - serde_json's `Value` compares `1` unequal to `1.0`; it carries the parsed
    type tag, so even a tree comparison in Rust's own types would be wrong.

Two normalisations are applied, the two Section 8.1 permits, and no others:

  - On a position, trailing `null` channels are dropped, down to arity 2.
  - An absent container and an empty one are the same value, so a key whose
    value is `[]` or `{}` equals that key being absent.

Neither applies to an application key (Section 3.9): a top-level key containing
a full stop, and everything inside its value, is compared exactly, so a writer
that dropped one holding `null`, `[]` or `{}` fails its cell.

No value is normalised here, and since Section 4.1 stopped constraining a
writer's precision, none should be.

⚠ THIS MATRIX DOES NOT GUARD PRECISION, and it is worth saying so because it
looks as though it should. Each cell compares a WRITER'S OUTPUT against a
reader's reproduction of that same output, so an implementation that rounded on
the way out would produce rounded bytes that every reader reproduces faithfully,
and all six of its cells would pass. What this matrix proves is that the six
agree with each other about what a document MEANS — not that any of them kept
the digits it was handed.

The guard for that is the ACCEPT TIER, and it is per-implementation: a runner
decodes a corpus file, re-encodes it, and compares against the ORIGINAL FILE, so
a writer that rounds differs from the file and reddens.
`corpus/accept/full-precision-coordinate.vnx` carries `-3.6712345678901235` for
exactly that purpose. Keep it, and do not let any runner reintroduce a precision
normalisation into that comparison — several did have one, applied to both
sides, which would have absorbed the very defect the fixture exists to catch.

No fallbacks. A missing toolchain, an unreadable file, a driver that cannot be
invoked: each is a named error and a non-zero exit. A cell is never skipped.

LIVENESS. A harness that silently failed to invoke five of the six drivers
would report a clean sweep, so the run asserts that thirty-six cells were
actually reached, and that each passing cell compared as many scalar leaves as
the writer output independently contains. Vacuous agreement between two empty
trees cannot pass. Beyond that the matrix is red-proofed by `--red-proof`,
which corrupts one implementation's output and checks that the cells which
should redden do and that the others do not; see that flag below.

CI. This repository has no CI of any kind: no `.github/workflows`, no
`.gitlab-ci.yml`, no Makefile, no scheduled job. This script therefore runs by
hand and nothing runs it automatically. That is a statement of fact at the time
of writing, not a recommendation.

Usage, from the repository root:

    python3 scripts/interop.py                 build everything, run the matrix
    python3 scripts/interop.py --no-build      use the drivers already built
    python3 scripts/interop.py --red-proof go  corrupt Go's output, expect red
"""

from __future__ import annotations

import argparse
import json
import math
import pathlib
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass, field

ROOT = pathlib.Path(__file__).resolve().parents[1]
INTEROP = ROOT / "scripts" / "interop"
SOURCE = INTEROP / "source.vnx"

# The order the matrix is printed in. Nothing depends on it beyond the report.
LANGS = ["go", "python", "js", "rust", "swift", "kotlin"]


@dataclass(frozen=True)
class Driver:
    """One language's read-then-write driver.

    `tool` is the executable the driver needs on PATH, checked before anything
    runs so that a missing toolchain is one clear error rather than six.
    `build` is the command that produces `run`; an interpreted driver has none.
    """

    name: str
    tool: str
    build: list[str] | None
    run: list[str]


DRIVERS = {
    "go": Driver(
        name="go",
        tool="go",
        build=["go", "build", "-o", "interop", "."],
        run=[str(INTEROP / "go" / "interop")],
    ),
    "python": Driver(
        name="python",
        tool="python3",
        build=None,
        run=[sys.executable, str(INTEROP / "python" / "interop.py")],
    ),
    "js": Driver(
        name="js",
        tool="node",
        build=None,
        run=["node", str(INTEROP / "js" / "interop.mjs")],
    ),
    "rust": Driver(
        name="rust",
        tool="cargo",
        build=["cargo", "build", "--quiet"],
        run=[str(INTEROP / "rust" / "target" / "debug" / "interop")],
    ),
    "swift": Driver(
        name="swift",
        tool="swift",
        build=["swift", "build"],
        run=[str(INTEROP / "swift" / ".build" / "debug" / "interop")],
    ),
    "kotlin": Driver(
        name="kotlin",
        tool="java",
        # The interop build has no wrapper of its own; it borrows the
        # library's, which is the wrapper version the library is built with.
        build=[str(ROOT / "kotlin" / "gradlew"), "installDist", "--console=plain", "--quiet"],
        run=[str(INTEROP / "kotlin" / "build" / "install" / "interop" / "bin" / "interop")],
    ),
}

BUILD_DIRS = {
    "go": INTEROP / "go",
    "rust": INTEROP / "rust",
    "swift": INTEROP / "swift",
    "kotlin": INTEROP / "kotlin",
}


class Failure(Exception):
    """A named, fatal condition. Never caught to substitute a default."""


# --------------------------------------------------------------------------
# Parsing and comparison
# --------------------------------------------------------------------------


def _reject_constant(name: str):
    # Python's json accepts NaN, Infinity and -Infinity by default. Section 4.2
    # forbids all three on the wire, so one appearing in a driver's output is a
    # defect in that driver, not something to parse quietly.
    raise Failure(f"non-finite JSON literal {name!r} in output; Section 4.2 forbids it")


def parse(path: pathlib.Path) -> object:
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as e:
        raise Failure(f"cannot read {path}: {e}") from e
    try:
        return json.loads(text, parse_constant=_reject_constant)
    except json.JSONDecodeError as e:
        raise Failure(f"{path} is not JSON: {e}") from e


def is_number(v: object) -> bool:
    # `True` is an `int` in Python. A boolean is not a number here.
    return isinstance(v, (int, float)) and not isinstance(v, bool)


def is_position(v: object) -> bool:
    """Is this array a position (Section 4.1)?

    A position is the only array in this format whose elements are numbers and
    `null`s, so the shape identifies it without a path table that would rot
    every time the format gains a position-bearing key. Arity 2 is the floor:
    `lon` and `lat` are mandatory.
    """
    return (
        isinstance(v, list)
        and len(v) >= 2
        and all(e is None or is_number(e) for e in v)
    )


def drop_trailing_nulls(v: list) -> list:
    n = len(v)
    while n > 2 and v[n - 1] is None:
        n -= 1
    return v[:n]


def is_empty_container(v: object) -> bool:
    return (isinstance(v, (list, dict))) and len(v) == 0


@dataclass
class Comparison:
    """The result of comparing two trees: what differed, and how much was looked at."""

    mismatches: list[tuple[str, str]] = field(default_factory=list)
    leaves: int = 0


def compare(a: object, b: object, path: str, out: Comparison, exact: bool = False) -> None:
    """Compare two parsed trees by value, recording every difference found.

    Traversal does not stop at the first mismatch, so `out.leaves` is a
    complete count for a tree that matches. That is what makes the leaf count
    usable as a vacuity guard on a passing cell.

    `exact` turns off both normalisations. It is set for a top-level
    application key and holds for everything beneath it (Section 3.9).
    """
    if isinstance(a, dict) or isinstance(b, dict):
        if not (isinstance(a, dict) and isinstance(b, dict)):
            out.mismatches.append((path, f"object vs {type_name(b if isinstance(a, dict) else a)}"))
            return
        for key in sorted(set(a) | set(b)):
            child = f"{path}/{escape(key)}"
            child_exact = exact or (path == "" and "." in key)
            if key not in a:
                # Section 3.1: an absent container and an empty one are the
                # same value. Anything else present on one side only differs.
                if child_exact or not is_empty_container(b[key]):
                    out.mismatches.append((child, f"absent vs {render(b[key])}"))
                continue
            if key not in b:
                if child_exact or not is_empty_container(a[key]):
                    out.mismatches.append((child, f"{render(a[key])} vs absent"))
                continue
            compare(a[key], b[key], child, out, child_exact)
        return

    if isinstance(a, list) or isinstance(b, list):
        if not (isinstance(a, list) and isinstance(b, list)):
            out.mismatches.append((path, f"array vs {type_name(b if isinstance(a, list) else a)}"))
            return
        if not exact and is_position(a) and is_position(b):
            a = drop_trailing_nulls(a)
            b = drop_trailing_nulls(b)
        if len(a) != len(b):
            out.mismatches.append((path, f"array of {len(a)} vs array of {len(b)}"))
            # Still compare the common prefix; a length difference is usually
            # one inserted or dropped element and the rest lines up.
        for i in range(min(len(a), len(b))):
            compare(a[i], b[i], f"{path}/{i}", out, exact)
        return

    out.leaves += 1

    if a is None or b is None:
        if a is not b:
            out.mismatches.append((path, f"{render(a)} vs {render(b)}"))
        return
    if isinstance(a, bool) or isinstance(b, bool):
        # A boolean never equals a number, whatever Python thinks of `True == 1`.
        if not (isinstance(a, bool) and isinstance(b, bool) and a == b):
            out.mismatches.append((path, f"{render(a)} vs {render(b)}"))
        return
    if is_number(a) and is_number(b):
        # By value. `1`, `1.0` and `1e0` are one number; the spelling is the
        # writer's business (Section 4.2).
        fa, fb = float(a), float(b)
        if not (fa == fb or (math.isnan(fa) and math.isnan(fb))):
            out.mismatches.append((path, f"{render(a)} vs {render(b)}"))
        return
    if isinstance(a, str) and isinstance(b, str):
        if a != b:
            out.mismatches.append((path, f"{render(a)} vs {render(b)}"))
        return
    out.mismatches.append((path, f"{type_name(a)} vs {type_name(b)}"))


def count_leaves(v: object) -> int:
    """Scalar leaves in a tree, counted the way `compare` counts them.

    Independent of `compare` only in that it looks at one tree. It exists so a
    passing cell can be asked how much it actually examined.
    """
    if isinstance(v, dict):
        return sum(count_leaves(x) for x in v.values())
    if isinstance(v, list):
        if is_position(v):
            v = drop_trailing_nulls(v)
        return sum(count_leaves(x) for x in v)
    return 1


def type_name(v: object) -> str:
    if v is None:
        return "null"
    if isinstance(v, bool):
        return "boolean"
    if is_number(v):
        return "number"
    if isinstance(v, str):
        return "string"
    if isinstance(v, list):
        return "array"
    return "object"


def render(v: object) -> str:
    text = json.dumps(v)
    return text if len(text) <= 70 else text[:67] + "..."


def escape(key: str) -> str:
    return key.replace("~", "~0").replace("/", "~1")


# --------------------------------------------------------------------------
# Perturbation, for the red proof
# --------------------------------------------------------------------------


def perturb_transpose(doc: object) -> str:
    """Swap longitude and latitude on the first INTERIOR pip of the first arc.

    Every other position in the document is cross-checked by some rule, and a
    corrupted one raises a diagnostic rather than a difference: a polygon ring
    stops closing, a chain's links stop meeting, and an arc's terminal pip
    stops meeting its node (`terminal-pip-off-node`, measured here by moving
    pip 0 and watching the driver refuse). A driver that exits on a diagnostic
    writes nothing, and the cell would go red for the wrong reason — the
    corruption would never reach the comparison the red proof is testing.

    An interior pip carries no such check. The corrupted document is still a
    clean VNX document that every reader accepts with no diagnostics, so the
    cell reddens on the tree comparison, which is the thing under test.
    """
    found = _first_pip(doc, "")
    if found is None:
        raise Failure("no pip position found to transpose; the source document changed shape")
    path, pos = found
    if pos[0] == pos[1]:
        raise Failure(f"pip at {path} has equal lon and lat; transposing it changes nothing")
    pos[0], pos[1] = pos[1], pos[0]
    return f"transposed lon/lat at {path}: {render(pos)}"


def _first_pip(v: object, path: str) -> tuple[str, list] | None:
    if isinstance(v, dict):
        for key in sorted(v):
            hit = _first_pip(v[key], f"{path}/{escape(key)}")
            if hit is not None:
                return hit
        return None
    if isinstance(v, list):
        if path.endswith("/pips"):
            # Interior only: the first and last pip of an arc must meet its
            # nodes, so moving either raises a diagnostic instead of a
            # difference. An arc of fewer than three pips has no interior.
            for i in range(1, len(v) - 1):
                if is_position(v[i]):
                    return (f"{path}/{i}", v[i])
            return None
        for i, e in enumerate(v):
            hit = _first_pip(e, f"{path}/{i}")
            if hit is not None:
                return hit
    return None


def perturb_drop_record(doc: object) -> str:
    """Drop the first data record found.

    A data record is an opaque extension (Section 5): nothing in the format
    cross-checks one, so removing it raises no diagnostic and the corrupted
    document stays clean. It is the loss the tree comparison exists to catch,
    the one a reader that discarded extensions would cause.
    """
    found = _first_data_list(doc, "")
    if found is None:
        raise Failure("no data record found to drop; the source document changed shape")
    path, records = found
    dropped = records.pop(0)
    return f"dropped data record at {path}/0: {render(dropped)}"


def _first_data_list(v: object, path: str) -> tuple[str, list] | None:
    if isinstance(v, dict):
        data = v.get("data")
        if isinstance(data, list) and data:
            return (f"{path}/data", data)
        for key in sorted(v):
            hit = _first_data_list(v[key], f"{path}/{escape(key)}")
            if hit is not None:
                return hit
        return None
    if isinstance(v, list):
        for i, e in enumerate(v):
            hit = _first_data_list(e, f"{path}/{i}")
            if hit is not None:
                return hit
    return None


PERTURBATIONS = {"transpose": perturb_transpose, "drop-record": perturb_drop_record}


# --------------------------------------------------------------------------
# Running the drivers
# --------------------------------------------------------------------------


def check_toolchains(langs: list[str]) -> None:
    missing = [(l, DRIVERS[l].tool) for l in langs if shutil.which(DRIVERS[l].tool) is None]
    if missing:
        lines = "\n".join(f"  {lang}: needs {tool!r} on PATH" for lang, tool in missing)
        raise Failure(f"toolchain missing, cannot measure these implementations:\n{lines}")


def build(langs: list[str]) -> None:
    """Build every compiled driver, so the matrix measures the working tree.

    Skipping the build would measure whatever binary happened to be lying
    around, which may predate the library source beside it.
    """
    for lang in langs:
        driver = DRIVERS[lang]
        if driver.build is None:
            continue
        cwd = BUILD_DIRS[lang]
        print(f"building {lang} ...", flush=True)
        proc = subprocess.run(driver.build, cwd=cwd, capture_output=True, text=True)
        if proc.returncode != 0:
            raise Failure(
                f"{lang}: build failed (exit {proc.returncode})\n"
                f"  command: {' '.join(driver.build)}\n"
                f"  in: {cwd}\n{indent(proc.stdout)}{indent(proc.stderr)}"
            )


def check_runnable(langs: list[str]) -> None:
    for lang in langs:
        first = pathlib.Path(DRIVERS[lang].run[0])
        # An interpreter is resolved on PATH and already checked; only the
        # artefacts this repository produces are checked here.
        if first.is_absolute() and not first.exists():
            raise Failure(
                f"{lang}: driver {first} does not exist. "
                f"Run without --no-build, or build it by hand."
            )
        script = pathlib.Path(DRIVERS[lang].run[-1])
        if len(DRIVERS[lang].run) > 1 and not script.exists():
            raise Failure(f"{lang}: driver script {script} does not exist")


def invoke(lang: str, src: pathlib.Path, dst: pathlib.Path, perturb) -> None:
    """Run one driver over one file. Any non-zero exit is fatal and named."""
    cmd = DRIVERS[lang].run + [str(src), str(dst)]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode != 0:
        raise Failure(
            f"{lang}: driver failed on {src.name} (exit {proc.returncode})\n"
            f"  command: {' '.join(cmd)}\n{indent(proc.stderr)}"
        )
    if proc.stderr.strip():
        raise Failure(f"{lang}: driver wrote to stderr on {src.name}:\n{indent(proc.stderr)}")
    if not dst.exists():
        raise Failure(f"{lang}: driver exited 0 but wrote no {dst}")
    if perturb is not None:
        doc = parse(dst)
        note = perturb(doc)
        dst.write_text(json.dumps(doc, indent=2), encoding="utf-8")
        print(f"    perturbed {lang} output -> {note}", flush=True)


def indent(text: str) -> str:
    return "".join(f"    {line}\n" for line in text.rstrip().splitlines()) if text.strip() else ""


# --------------------------------------------------------------------------
# The matrix
# --------------------------------------------------------------------------


@dataclass
class Cell:
    writer: str
    reader: str
    passed: bool
    leaves: int
    expected_leaves: int
    mismatches: list[tuple[str, str]]


def run_matrix(work: pathlib.Path, langs: list[str], red: str | None, perturb) -> list[Cell]:
    if not SOURCE.exists():
        raise Failure(f"source document missing: {SOURCE}")

    written: dict[str, pathlib.Path] = {}
    for lang in langs:
        out = work / f"written-{lang}.vnx"
        invoke(lang, SOURCE, out, perturb if lang == red else None)
        written[lang] = out

    if len(written) != len(langs):
        raise Failure(f"expected {len(langs)} writer outputs, produced {len(written)}")

    cells: list[Cell] = []
    for writer in langs:
        src = written[writer]
        tree = parse(src)
        expected = count_leaves(tree)
        if expected <= 0:
            raise Failure(f"{writer}: its own output has no scalar leaves; nothing to compare")
        for reader in langs:
            back = work / f"read-{writer}-by-{reader}.vnx"
            invoke(reader, src, back, perturb if reader == red else None)
            result = Comparison()
            compare(tree, parse(back), "", result)
            cells.append(
                Cell(
                    writer=writer,
                    reader=reader,
                    passed=not result.mismatches,
                    leaves=result.leaves,
                    expected_leaves=expected,
                    mismatches=result.mismatches,
                )
            )
    return cells


def assert_liveness(cells: list[Cell], langs: list[str]) -> None:
    """The control that stops a silently-skipped runner reporting a clean sweep."""
    want = len(langs) ** 2
    if len(cells) != want:
        raise Failure(f"liveness: expected {want} cells, reached {len(cells)}")
    seen = {(c.writer, c.reader) for c in cells}
    if len(seen) != want:
        missing = [(w, r) for w in langs for r in langs if (w, r) not in seen]
        raise Failure(f"liveness: duplicate or missing cells; missing {missing}")
    for cell in cells:
        if cell.passed and cell.leaves != cell.expected_leaves:
            raise Failure(
                f"liveness: {cell.writer} -> {cell.reader} passed having compared "
                f"{cell.leaves} leaves, but its writer output holds "
                f"{cell.expected_leaves}. A pass over a partial tree is not a pass."
            )


def report(cells: list[Cell], langs: list[str]) -> None:
    index = {(c.writer, c.reader): c for c in cells}
    width = max(len(l) for l in langs)
    print()
    print("  matrix: rows are the writer, columns the reader; a cell is the")
    print("  reader's reproduction of that writer's document, compared by value.")
    print()
    header = " " * (width + 2) + "  ".join(f"{l:^{width}}" for l in langs)
    print(header)
    for writer in langs:
        row = [f"{writer:>{width}} |"]
        for reader in langs:
            mark = "pass" if index[(writer, reader)].passed else "FAIL"
            row.append(f"{mark:^{width}}")
        print("  ".join(row))
    print()

    failed = [c for c in cells if not c.passed]
    total_leaves = cells[0].expected_leaves if cells else 0
    print(f"  {len(cells) - len(failed)}/{len(cells)} cells agree "
          f"({total_leaves} scalar leaves compared per cell)")
    if not failed:
        return
    print()
    for cell in failed:
        print(f"  FAIL {cell.writer} written, {cell.reader} read back "
              f"({len(cell.mismatches)} difference"
              f"{'' if len(cell.mismatches) == 1 else 's'})")
        for path, detail in cell.mismatches[:10]:
            print(f"    {path or '/'}: {detail}")
        if len(cell.mismatches) > 10:
            print(f"    ... and {len(cell.mismatches) - 10} more")


def report_red_proof(cells: list[Cell], red: str, langs: list[str]) -> bool:
    """Check the reddening is where corrupting one implementation must put it.

    The perturbation is applied to everything the named implementation writes,
    which is the shape of a real defect in its writer. The consequence is a
    whole column, and only that column:

      - (w, red) for w other than red: a clean document in, a corrupted one
        out. Red.
      - (red, red): the corrupted document in, corrupted a second time. Red for
        `drop-record`, which loses a further record. For `transpose` the second
        swap undoes the first and the cell compares the corrupted document
        against the clean one. Red either way, but for different reasons.
      - (red, r) for r other than red: the corrupted document goes in and the
        reader reproduces it faithfully, corruption and all. GREEN, correctly:
        the pair agrees, and the row is not where a writer defect shows.
    """
    expected_red = {(w, red) for w in langs}
    actual_red = {(c.writer, c.reader) for c in cells if not c.passed}
    print()
    print(f"  red proof: {red}'s output perturbed on every write")
    print(f"  expected red: {len(expected_red)} cells, the {red} reader column")
    print(f"  actually red: {sorted(actual_red)}")
    surprise = actual_red - expected_red
    absent = expected_red - actual_red
    if surprise:
        print(f"  UNEXPECTED red: {sorted(surprise)}")
    if absent:
        print(f"  EXPECTED red but green: {sorted(absent)}")
    ok = not surprise and not absent
    print(f"  red proof: {'CONFIRMED' if ok else 'FAILED'}")
    return ok


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--no-build", action="store_true",
                    help="use the drivers already built; do not rebuild them")
    ap.add_argument("--red-proof", metavar="LANG", choices=LANGS,
                    help="corrupt this implementation's output on every write, "
                         "and check that exactly its reader column goes red")
    ap.add_argument("--perturbation", choices=sorted(PERTURBATIONS), default="transpose",
                    help="which corruption --red-proof applies (default: transpose)")
    ap.add_argument("--work", metavar="DIR",
                    help="keep the 42 intermediate documents here: 6 writer outputs "
                         "and 36 read-backs (default: a temporary directory, removed on exit)")
    args = ap.parse_args()

    langs = LANGS
    check_toolchains(langs)
    if not args.no_build:
        build(langs)
    check_runnable(langs)

    perturb = PERTURBATIONS[args.perturbation] if args.red_proof else None

    with tempfile.TemporaryDirectory(prefix="vnx-interop-") as tmp:
        work = pathlib.Path(args.work) if args.work else pathlib.Path(tmp)
        work.mkdir(parents=True, exist_ok=True)
        cells = run_matrix(work, langs, args.red_proof, perturb)
        assert_liveness(cells, langs)
        report(cells, langs)
        if args.red_proof:
            return 0 if report_red_proof(cells, args.red_proof, langs) else 1
        return 0 if all(c.passed for c in cells) else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Failure as failure:
        print(f"interop: {failure}", file=sys.stderr)
        sys.exit(1)
