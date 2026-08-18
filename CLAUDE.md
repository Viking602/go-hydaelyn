# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository. `AGENTS.md` is a symlink to this file, so it is the single source of truth for all AI agents.

## Project overview

Venat is a durable, typed multi-agent framework for Go (`github.com/Viking602/venat`, Go 1.25). Documentation map: Packs → Worker integration (`worker/`) → Multi-Agent Layer (`multiagent/`) → Agent Loop Layer (`agent/`) → Durable Runner (root + `internal/`). The linear picture is documentation only; reverse-edge bans stay. The root package exposes the public `Runner` façade; public contracts live in `api/`; implementation details stay under `internal/`. Extension/runtime packages: `provider/`, `tool/`, `policy/`, `hook/`, `message/`, `transport/`, `coding/`, `memory/` (deprecated; use `api.Memory`), `packs/`, `eval/`. Storage conformance tests live in `contract/`, examples in `_examples/`, scripts in `scripts/`, docs (incl. ADRs) in `docs/`. `internal/memory` is the process-local development/test store, not a Position D reference backend. See `docs/architecture-boundaries.md`.

## Build, test, and development commands

- `make verify` — fast local gate; run before routine changes (fmt-check + vet + tidy-check + lint + test + architecture-check).
- `make ci-local` — full CI-parity gate; run before substantial changes (adds staticcheck, vulncheck, race).
- `make fmt` / `make fmt-check` — format with gofmt + goimports / fail if not clean.
- `make test` / `make test-race` — `go test ./...` / race-enabled with 10m timeout.
- `make architecture-check` — runs `sentrux check .`, `scripts/check-business-words.sh`, `scripts/check-public-any.sh`, `scripts/check-import-boundaries.sh`.
- Run an example with `go run ./_examples/<name>`.

## Critical gates

`make architecture-check` (and the four checks it runs) are hard gates — a violation fails CI even if tests pass. Run `make verify` before finishing routine work and `make ci-local` before substantial changes.

## Public any-field contract (ADR-009)

Exported functions in `api/`, `agent/`, `multiagent/`, and the root package must not return `[]any`, and exported fields must not be loose `any`. Add a typed result struct (e.g. `api.StartRunResult`) instead of returning `[]any`. Genuine exceptions (host payloads, provider bodies, JSON Schema objects) require an escape-hatch comment on the line immediately above:

- `//venat:allow-public-any` — above a function signature.
- `// godoc-allow-any` — above a struct field.

Enforced by `scripts/check-public-any.sh`. Test files are exempt.

## Coding style & naming

Use `gofmt` as the source of truth; goimports uses local prefix `github.com/Viking602/venat`. Go files and `Makefile` use tabs; Markdown uses two spaces, LF, final newlines (`.editorconfig`). Prefer package-context names that read naturally at call sites (`venat.New()`, `team.Profile`). Avoid package-name stutter, generic files (`types.go`, `helpers.go`, `utils.go`), package/directory renames, exported-symbol renames, and new linting stacks unless explicitly approved.

## Testing

Standard `testing` package; keep tests beside the package under test with `_test.go` suffixes and names like `TestThing_Behavior`. Mark helpers with `t.Helper()`, prefer table-driven cases, assert errors with `errors.Is`.

## Commit & PR conventions

Conventional Commits with scope, e.g. `feat(v0.8.0): ...`, `fix(lint,race,storage): ...`, `test(contract): ...`, `docs(readme): ...`. Keep commits focused and explain why API/behavior changes are needed. PRs should summarize scope, list validation commands run, link issues, and call out any public-API, storage-contract, or architecture-boundary impact.

## Gotchas

- CI lint (`golangci-lint`) runs with `only-new-issues: true` against the merge-base with `main` — there is a tracked baseline of pre-existing issues, so only NEW findings fail. Don't try to clear the baseline.
- `_examples/` is excluded from `go build ./...` by its leading underscore.
- Storage is Position D (ADR-012): the framework owns the contract verbs; applications own schema and implementation. `api.Memory[T]` is an optional plugin with no shipped reference implementation (ADR-013).
- All architecture docs and specs are written in English (chat may be Chinese).
