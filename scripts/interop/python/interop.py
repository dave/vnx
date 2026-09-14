"""Read a VNX document with the Python implementation and write it back out.

One of six drivers, one per language; scripts/interop.py runs each over each
other's output. No fallback: a refusal, a diagnostic, or an unwritable path
exits non-zero with the cause on stderr.
"""

from __future__ import annotations

import pathlib
import sys

HERE = pathlib.Path(__file__).resolve()
sys.path.insert(0, str(HERE.parents[3] / "python"))

import vnx  # noqa: E402


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: interop.py <in.vnx> <out.vnx>", file=sys.stderr)
        return 2
    try:
        result = vnx.read_file(sys.argv[1])
    except vnx.VNXRefusal as refusal:
        print(f"interop: refused: {refusal}", file=sys.stderr)
        return 1
    for diagnostic in result.diagnostics:
        print(
            f"interop: diagnostic {diagnostic.code} at {diagnostic.path}: {diagnostic.message}",
            file=sys.stderr,
        )
    if result.diagnostics:
        return 1
    vnx.encode_file(sys.argv[2], result.document)
    return 0


if __name__ == "__main__":
    sys.exit(main())
