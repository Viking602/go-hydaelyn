# Contributing to Venat

This document defines the coding standards for the Venat repository.

## Formatting

Use `gofmt`. It is the only sanctioned formatter.

```bash
gofmt -w .
```

No other formatters (gofumpt, goimports in write mode) are required or recommended.

## File Naming

### Anti-Stutter Rule

Avoid repeating the package name in file or symbol names. Rely on package context instead.

```go
// package deepsearch
// GOOD: task.go (not deepsearch_task.go)
// GOOD: registry.go (not deepsearch_registry.go)

// package worker
// GOOD: worker.go (the file contains worker execution glue)
```

### Package-Context Naming

Names should make sense at the call site. When a type or function is used through its package, the combination should read naturally.

```go
// GOOD: venat.New(), venat.Config, venat.Runner
// The root package provides the runner, not "venat.VenatRunner"

// GOOD: team.Profile, team.RoleSupervisor
// "team" context makes "Profile" and "Role" clear
```

### Responsibility-Based File Names

Name files after what they contain, not generic categories.

```go
// GOOD: registry.go (contains registry logic)
// GOOD: supervisor.go (contains supervisor orchestration)
// AVOID: types.go, utils.go, helpers.go (vague)
```

### When to Split Files

Split files only when there is a clean responsibility seam. Do not split by size alone.

```go
// GOOD: Separate files for distinct components
//   - registry.go (pattern registry)
//   - supervisor.go (supervisor orchestration)
//   - worker.go (worker task execution)

// AVOID: Splitting just because a file exceeds N lines
```

### Runner/Core Files

Prefer `runner` for public entrypoint naming and `core` for internal mechanism.
Use `runtime` only for historical compatibility aliases or when it is the
clearest domain term in surrounding context.

```go
// GOOD: venat.Runner, worker.AgentWorker, internal/core
// AVOID: NewRuntime-style names for new public APIs
```

## Verification Commands

Before submitting changes, run the local CI parity gate with Go 1.25.10:

```bash
make ci-local
```

This target runs formatting, module-tidy, `go vet`, `staticcheck`,
`govulncheck`, `golangci-lint`, normal tests, race tests, Sentrux, and
the framework boundary scripts.

## Guardrails

These constraints apply to all changes:

1. **No package/directory renames** - Current package structure is stable
2. **No exported symbol renames** - Public API changes require explicit approval
3. **No new linting stack** - Keep linting centralized in `.golangci.yml`, CI, and the existing Makefile targets

## References

- [Effective Go](https://go.dev/doc/effective_go) - Official Go style guide
- [Package Names](https://go.dev/blog/package-names) - Go blog on package naming conventions
- [Google Go Style Guide](https://google.github.io/styleguide/go/) - Comprehensive Go conventions
