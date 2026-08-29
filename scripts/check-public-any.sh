#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [[ ! -f scripts/publicany/main.go ]]; then
  echo "FAIL: scripts/publicany command is missing." >&2
  exit 1
fi

go run ./scripts/publicany
