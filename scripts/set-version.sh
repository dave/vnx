#!/bin/bash
# Set the one version the format and all six libraries carry.
#
#     ./scripts/set-version.sh 0.10.1
#
# Writes the version to the SPEC.md Version line, table row and series
# sentence, to every package manifest, and to every library constant that
# names the format version it reads or writes. The list of places is
# VERSION_SITES in scripts/check-spec.py, the same list the drift check
# compares, so the two cannot disagree. Then it runs check-spec.py.
#
# It does not touch the version a corpus document or a test fixture declares.
# A new MINOR needs those swept too; CONTRIBUTING.md says how.
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [ $# -ne 1 ] || ! [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: scripts/set-version.sh MAJOR.MINOR.PATCH, three runs of digits, like 0.10.1" >&2
  exit 2
fi

python3 scripts/check-spec.py --write-version "$1"
python3 scripts/check-spec.py
