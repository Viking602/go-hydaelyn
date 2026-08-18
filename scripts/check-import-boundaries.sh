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
  local kind="$4"

  printf 'FAIL [%s]: %s imports %s (%s)\n' "$rule" "$package" "$imported" "$kind" >&2
  violations=$((violations + 1))
}

is_within() {
  local imported="$1"
  local prefix="$2"

  [[ "$imported" == "$prefix" || "$imported" == "$prefix/"* ]]
}

# Named exception: coding/eval_regression_test.go drives the real tool
# gate through the worker bridge and therefore imports the root façade
# and worker/. Production coding/ code and other coding tests cannot.
# go list aggregates TestImports for the whole package, so this only
# suppresses the package-level hit; check_coding_test_files enforces
# the file scope.
is_named_exception() {
  local package="$1"
  local imported="$2"
  local kind="$3"

  if [[ "$kind" == "test" && "$package" == "$module/coding" ]]; then
    if [[ "$imported" == "$module" || "$imported" == "$module/worker" ]]; then
      return 0
    fi
  fi
  return 1
}

extract_go_imports() {
  python3 - "$1" <<'PY'
import re
import sys

text = open(sys.argv[1], encoding="utf-8").read()
# Drop line comments so a quoted remark cannot steal the import path.
cleaned = []
for line in text.splitlines():
    in_raw = False
    in_str = False
    out = []
    i = 0
    while i < len(line):
        ch = line[i]
        if not in_str and not in_raw and ch == "/" and i + 1 < len(line) and line[i + 1] == "/":
            break
        out.append(ch)
        if ch == "`" and not in_str:
            in_raw = not in_raw
        elif ch == '"' and not in_raw:
            in_str = not in_str
        elif ch == "\\" and in_str:
            if i + 1 < len(line):
                out.append(line[i + 1])
                i += 1
        i += 1
    cleaned.append("".join(out))
text = "\n".join(cleaned)

paths = []
for match in re.finditer(r'(?m)^\s*import\s+(?:(?:[A-Za-z_][\w.]*|\.)\s+)?(["`])(.+?)\1', text):
    paths.append(match.group(2))
for match in re.finditer(r"(?s)import\s*\((.*?)\)", text):
    for inner in re.finditer(r'(["`])(.+?)\1', match.group(1)):
        paths.append(inner.group(2))
for path in paths:
    print(path)
PY
}

# File-level coding test scan: only eval_regression_test.go may import
# the root module or worker/. Other coding tests still cannot.
check_coding_test_files() {
  local file imported rel
  while IFS= read -r -d '' file; do
    rel="${file#./}"
    while IFS= read -r imported; do
      [[ -z "${imported:-}" ]] && continue
      if [[ "$rel" == "coding/eval_regression_test.go" ]]; then
        if is_within "$imported" "$module/packs"; then
          report "coding-no-worker-packs-root" "$rel" "$imported" "test-file"
        fi
        continue
      fi
      if [[ "$imported" == "$module" ]] ||
        is_within "$imported" "$module/worker" ||
        is_within "$imported" "$module/packs"; then
        report "coding-no-worker-packs-root" "$rel" "$imported" "test-file"
      fi
    done < <(extract_go_imports "$file")
  done < <(find ./coding -name '*_test.go' -print0)
}

check_import() {
  local scope="$1"
  local package="$2"
  local imported="$3"
  local kind="$4"

  if is_named_exception "$package" "$imported" "$kind"; then
    return 0
  fi
  # External tests (`package foo_test`) import the package under test.
  if [[ "$imported" == "$package" ]]; then
    return 0
  fi

  case "$scope" in
    api)
      if is_within "$imported" "$module"; then
        report "api-no-project-imports" "$package" "$imported" "$kind"
      fi
      ;;
    agent)
      if is_within "$imported" "$module/multiagent"; then
        report "agent-no-multiagent" "$package" "$imported" "$kind"
      fi
      ;;
    multiagent)
      if [[ "$imported" == "$module" ]] ||
        is_within "$imported" "$module/worker" ||
        is_within "$imported" "$module/internal"; then
        report "multiagent-no-runner-worker-internal" "$package" "$imported" "$kind"
      fi
      ;;
    root)
      if is_within "$imported" "$module/multiagent"; then
        report "root-facade-no-multiagent" "$package" "$imported" "$kind"
      fi
      ;;
    worker)
      if is_within "$imported" "$module/packs" ||
        is_within "$imported" "$module/coding"; then
        report "worker-no-packs-coding" "$package" "$imported" "$kind"
      fi
      ;;
    packs)
      if [[ "$imported" == "$module" ]] ||
        is_within "$imported" "$module/coding" ||
        is_within "$imported" "$module/worker"; then
        report "packs-no-coding-worker-root" "$package" "$imported" "$kind"
      fi
      ;;
    coding)
      if [[ "$imported" == "$module" ]] ||
        is_within "$imported" "$module/worker" ||
        is_within "$imported" "$module/packs"; then
        report "coding-no-worker-packs-root" "$package" "$imported" "$kind"
      fi
      ;;
  esac
}

check_package_set() {
  local scope="$1"
  local patterns="$2"
  local listing
  local package imported kind

  # Production, TestImports, and XTestImports are all part of the graph.
  # eval → worker is a declared harness bridge and is not in this ban list.
  listing="$(go list -e -f '{{$p := .ImportPath}}{{range .Imports}}{{printf "prod\t%s\t%s\n" $p .}}{{end}}{{range .TestImports}}{{printf "test\t%s\t%s\n" $p .}}{{end}}{{range .XTestImports}}{{printf "test\t%s\t%s\n" $p .}}{{end}}' $patterns)"
  while IFS=$'\t' read -r kind package imported; do
    [[ -z "${package:-}" || -z "${imported:-}" ]] && continue
    check_import "$scope" "$package" "$imported" "$kind"
  done <<< "$listing"
}

check_package_set api './api/...'
check_package_set agent './agent/...'
check_package_set multiagent './multiagent/...'
check_package_set root '.'
check_package_set worker './worker/...'
check_package_set packs './packs/...'
check_package_set coding './coding/...'
check_coding_test_files

if ((violations > 0)); then
  printf '\nFAIL: %d import boundary violation(s).\n' "$violations" >&2
  printf 'See docs/architecture-boundaries.md.\n' >&2
  exit 1
fi

go list './api/...' './agent/...' './multiagent/...' './worker/...' './packs/...' './coding/...' '.' >/dev/null

echo "OK: import boundaries preserved."
