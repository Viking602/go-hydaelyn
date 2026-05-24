#!/usr/bin/env bash
# Enforce ADR-009 / v0.8.0 spec 01-public-api §"Change 5":
# the public surface in api/ and the root package must not return []any from
# exported functions. Typed result structs (api.StartRunResult,
# api.RequestApprovalResult, api.AcquireTaskExecutionResult, ...) exist for
# every command that previously returned []any; new commands that need
# multi-value results MUST add a typed struct, never []any.
#
# Allowed exceptions are tagged `//hydaelyn:allow-public-any` on the line
# immediately above the offending signature.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# Files in scope: api/ (the canonical surface) and root-package *.go.
# Test files are excluded — tests may legitimately need []any helpers.
mapfile -t files < <(
  {
    find api -maxdepth 2 -type f -name '*.go' -not -name '*_test.go' 2>/dev/null
    find . -maxdepth 1 -type f -name '*.go' -not -name '*_test.go' 2>/dev/null
  } | sort -u
)

violations=0
output=""

for f in "${files[@]}"; do
  # Detect lines like:
  #   func (r *Runner) Foo(...) ([]any, error)
  #   func Foo(...) []any
  # We use a single regex for both return-tuple and single-return shapes.
  while IFS=: read -r lineno line; do
    [[ -z "$lineno" ]] && continue
    # Allow if previous line is the escape-hatch tag.
    prev_lineno=$((lineno - 1))
    prev_line="$(sed -n "${prev_lineno}p" "$f")"
    if [[ "$prev_line" == *"//hydaelyn:allow-public-any"* ]]; then
      continue
    fi
    output+="${f}:${lineno}: ${line}"$'\n'
    violations=$((violations + 1))
  done < <(grep -nE '^func [^/]*\) +(\(.*\[\]any|\[\]any)' "$f" 2>/dev/null || true)
done

if (( violations > 0 )); then
  echo "FAIL: ${violations} exported function(s) return []any on the public surface." >&2
  echo "See docs/product-spec/v0.8.0/01-public-api.md §Change 5." >&2
  echo >&2
  echo "$output" >&2
  exit 1
fi

echo "OK: no public []any return types on api/ or root package."
