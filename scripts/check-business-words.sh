#!/usr/bin/env bash
# Enforce ADR-008 framework-vs-business boundary.
# Counts business-domain literals leaked into framework Go code.
# Fails if the count exceeds the locked baseline.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BASELINE_FILE=".sentrux/business-words.baseline"
if [[ ! -f "$BASELINE_FILE" ]]; then
  echo "missing baseline file: $BASELINE_FILE" >&2
  exit 2
fi

baseline="$(tr -d '[:space:]' < "$BASELINE_FILE")"

# Pattern: business-domain words that must not leak into framework.
# Word boundaries on Hazard/Incident avoid false positives like "incident_response_example".
pattern='Synthesis|ReviewResult|ActionResult|TaskTypeAction|\bHazard\b|\bIncident\b'

count="$(
  {
    grep -rEc "$pattern" \
      --include="*.go" \
      --exclude-dir=legacy \
      --exclude-dir=_examples \
      --exclude-dir=docs \
      --exclude-dir=testdata \
      --exclude-dir=pattern \
      --exclude-dir=.git \
      . 2>/dev/null || true
  } \
  | awk -F: '$2 != 0 {s+=$2} END {print s+0}'
)"

echo "framework business-word count: $count (baseline: $baseline)"

if (( count > baseline )); then
  echo
  echo "FAIL: $((count - baseline)) new business-word occurrence(s) leaked into framework code." >&2
  echo "See docs/adr/ADR-008-framework-vs-business.md." >&2
  echo "Offending lines:" >&2
  grep -rnE "$pattern" \
    --include="*.go" \
    --exclude-dir=legacy \
    --exclude-dir=_examples \
    --exclude-dir=docs \
    --exclude-dir=testdata \
    --exclude-dir=pattern \
    --exclude-dir=.git \
    . >&2 || true
  exit 1
fi

if (( count < baseline )); then
  echo "tip: count dropped below baseline ($count < $baseline). Update $BASELINE_FILE to lock the win."
fi

echo "OK: framework boundary preserved."
