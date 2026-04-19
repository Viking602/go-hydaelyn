# SemVer And Compatibility

## Version Policy

Hydaelyn follows SemVer from `v1.0.0` onward:

- `MAJOR`: breaking public API or contract changes
- `MINOR`: backward-compatible features and additive fields
- `PATCH`: backward-compatible fixes

Runtime correctness has higher priority than benchmark breadth. When a change tightens correctness contracts for queue execution, replay, verifier evidence, or provider normalization, it must ship with tests and release-gate coverage in the same change.

## Public API Boundary

Stable packages:

- `agent`
- `blackboard`
- `capability`
- `host`
- `mcp`
- `observe`
- `planner`
- `plugin`
- `recipe`
- `scheduler`
- `team`
- `tool`
- `toolkit`
- `evaluation`

Implementation-detail packages:

- `providers/*`
- `transport/*`
- `tooltest`

## Compatibility Rules

- Adding optional fields to public structs is allowed.
- Removing public fields, renaming them, or changing their meaning requires a major version.
- Event payloads may add fields.
- Event type renames or removals require a major version.
- CLI may add flags such as `validate --strict-dataflow`.
- Removing CLI flags or changing their behavior incompatibly requires a major version.

## Release Gate

Every PR to `main` must pass:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `staticcheck ./...`
- `govulncheck ./...`

Main-branch soak:

- `go test ./... -count=20`
- `go test -race ./... -count=5`

These gates exist because the runtime contract is only as strong as the branch that carries it.
