#!/usr/bin/env bash
# Enforce ADR-009 / v0.8.0 spec 01-public-api §"Change 5":
# the public surface in api/, agent/, multiagent/, and the root package must
# not return []any from exported functions. Public fields that intentionally
# contain any (for host extension payloads, provider-specific bodies, JSON
# Schema objects, etc.) must be explicitly tagged `// godoc-allow-any`.
#
# Typed result structs (api.StartRunResult, api.RequestApprovalResult,
# api.AcquireTaskExecutionResult, ...) exist for every command that previously
# returned []any; new commands that need multi-value results MUST add a typed
# struct, never []any.
#
# Allowed exceptions are tagged `//hydaelyn:allow-public-any` on the line
# immediately above an offending signature. Public fields containing any are
# allowed only when tagged `// godoc-allow-any` on the line immediately above
# the field.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# Files in scope: api/ (the canonical surface), agent/ + multiagent/ +
# workflow/ (the v0.8.0 agent loop, multi-agent layer, and workflow modeling
# public surfaces per spec 01-public-api §Change 6 / Change 7), and
# root-package *.go. Test files are excluded — tests may legitimately need
# []any helpers.
mapfile -t files < <(
  {
    find api -maxdepth 2 -type f -name '*.go' -not -name '*_test.go' 2>/dev/null
    find agent -maxdepth 2 -type f -name '*.go' -not -name '*_test.go' 2>/dev/null
    find multiagent -maxdepth 2 -type f -name '*.go' -not -name '*_test.go' 2>/dev/null
    find workflow -maxdepth 2 -type f -name '*.go' -not -name '*_test.go' 2>/dev/null
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

  # Detect exported public fields such as:
  #   Payload map[string]any
  #   Input   any
  # Field-level exceptions are required because these shapes are part of the
  # public godoc contract and should stay visibly intentional.
  while IFS=: read -r lineno line; do
    [[ -z "$lineno" ]] && continue
    [[ "$line" == *"="* ]] && continue
    prev_lineno=$((lineno - 1))
    prev_line="$(sed -n "${prev_lineno}p" "$f")"
    if [[ "$prev_line" == *"// godoc-allow-any"* ]]; then
      continue
    fi
    output+="${f}:${lineno}: ${line}"$'\n'
    violations=$((violations + 1))
  done < <(grep -nE '^[[:space:]]*[A-Z][A-Za-z0-9_]*[[:space:]]+.*\bany\b' "$f" 2>/dev/null || true)
done

if (( violations > 0 )); then
  echo "FAIL: ${violations} public any contract violation(s)." >&2
  echo "See docs/product-spec/v0.8.0/01-public-api.md §Change 5." >&2
  echo >&2
  echo "$output" >&2
  exit 1
fi

echo "OK: public any contract preserved."
