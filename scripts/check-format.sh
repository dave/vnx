#!/bin/bash
# Check that every source file in the repository is in its formatter's style.
#
#     ./scripts/check-format.sh
#
# One formatter per language, at its default style, each run in check mode. The
# script exits non-zero if any file is not formatted, and names the file. It
# changes nothing: to fix a file, run the formatter's own write command, which
# CONTRIBUTING.md lists.
#
# A formatter that is not installed is a failure, not a skip: a check that
# quietly skipped one language would report that language clean.
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

failed=0
fail() {
  echo "check-format: $1" >&2
  failed=1
}
need() {
  command -v "$1" > /dev/null 2>&1 || fail "$1 is not installed, so $2 was not checked"
}

# Go: gofmt.
need gofmt Go
if command -v gofmt > /dev/null 2>&1; then
  out="$(git ls-files -z '*.go' | xargs -0 gofmt -l)"
  [ -z "$out" ] || fail "gofmt would change: $(echo $out)"
fi

# Rust: rustfmt, through cargo, for each crate.
need cargo Rust
if command -v cargo > /dev/null 2>&1; then
  for crate in rust scripts/interop/rust; do
    out="$(cd "$crate" && cargo fmt -- --check -l 2>&1)" || fail "rustfmt would change (in $crate): $(echo $out)"
  done
fi

# Python: ruff format, black style.
need ruff Python
if command -v ruff > /dev/null 2>&1; then
  out="$(git ls-files -z '*.py' | xargs -0 ruff format --check 2>&1)" || fail "ruff format would change: $(echo "$out" | sed -n 's/^ *--> \([^:]*\):.*/\1/p; s/^Would reformat: //p' | sort -u | tr '\n' ' ')"
fi

# JavaScript: prettier, pinned in js/package.json.
need npx JavaScript
if command -v npx > /dev/null 2>&1; then
  if [ ! -x js/node_modules/.bin/prettier ]; then
    fail "prettier is not installed in js/node_modules; run \`cd js && npm ci\`"
  else
    out="$(js/node_modules/.bin/prettier --config js/.prettierrc.json --check $(git ls-files '*.js' '*.mjs') 2>&1)" || fail "prettier would change: $(echo "$out" | grep '^\[warn\] ' | grep -v 'Code style issues' | sed 's/^\[warn\] //' | tr '\n' ' ')"
  fi
fi

# Kotlin: ktlint, pinned in each Gradle build.
if [ -z "${JAVA_HOME:-}" ]; then
  fail "JAVA_HOME is not set, so Kotlin was not checked (see CONTRIBUTING.md)"
else
  for build in kotlin scripts/interop/kotlin; do
    rm -rf "$build/build/reports/ktlint"
    (cd "$build" && "$ROOT/kotlin/gradlew" -q -p . ktlintCheck --rerun-tasks > /dev/null 2>&1) ||
      fail "ktlint would change (in $build): $(cat "$build"/build/reports/ktlint/*/*.txt 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g' | grep -o '^/[^:]*' | sed "s|$ROOT/||" | sort -u | tr '\n' ' ')"
  done
fi

# Swift: swift-format, from the Xcode toolchain.
need xcrun Swift
if command -v xcrun > /dev/null 2>&1; then
  out="$(xcrun swift-format lint --strict --recursive swift Package.swift scripts/interop/swift 2>&1)"
  [ -z "$out" ] || fail "swift-format would change: $(echo "$out" | cut -d: -f1 | sort -u | sed "s|$ROOT/||" | tr '\n' ' ')"
fi

if [ "$failed" -ne 0 ]; then
  exit 1
fi
echo "check-format: every file is formatted"
