#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

module="$(go list -m -f '{{.Path}}')"
violations=0

report() {
  local rule="$1"
  local package="$2"
  local imported="$3"

  printf 'FAIL [%s]: %s imports %s\n' "$rule" "$package" "$imported" >&2
  violations=$((violations + 1))
}

is_within() {
  local imported="$1"
  local prefix="$2"

  [[ "$imported" == "$prefix" || "$imported" == "$prefix/"* ]]
}

check_package_set() {
  local scope="$1"
  local package
  local imported
  local listing
  local -a fields

  listing="$(go list -e -f '{{.ImportPath}}{{range .Imports}}{{printf "\t%s" .}}{{end}}' "$2")"
  while IFS=$'\t' read -r -a fields; do
    package="${fields[0]}"
    for imported in "${fields[@]:1}"; do
      case "$scope" in
        api)
          if is_within "$imported" "$module"; then
            report "api-no-project-imports" "$package" "$imported"
          fi
          ;;
        agent)
          if is_within "$imported" "$module/multiagent"; then
            report "agent-no-multiagent" "$package" "$imported"
          fi
          ;;
        multiagent)
          if [[ "$imported" == "$module" ]] ||
            is_within "$imported" "$module/worker" ||
            is_within "$imported" "$module/internal"; then
            report "multiagent-no-runner-worker-internal" "$package" "$imported"
          fi
          ;;
        root)
          if is_within "$imported" "$module/multiagent"; then
            report "root-facade-no-multiagent" "$package" "$imported"
          fi
          ;;
      esac
    done
  done <<< "$listing"
}

check_package_set api './api/...'
check_package_set agent './agent/...'
check_package_set multiagent './multiagent/...'
check_package_set root '.'

if ((violations > 0)); then
  printf '\nFAIL: %d import boundary violation(s).\n' "$violations" >&2
  printf 'See docs/product-spec/v0.8.0/11-boundaries.md.\n' >&2
  exit 1
fi

go list './api/...' './agent/...' './multiagent/...' '.' >/dev/null

echo "OK: import boundaries preserved."
