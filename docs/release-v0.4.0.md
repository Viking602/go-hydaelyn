# go-hydaelyn v0.4.0 — API surface convergence

v0.4.0 is a **breaking refactor** release. No runtime behavior changes; the
only thing that moved is the public package layout. 25 top-level packages
were either merged, moved under an internal/ tree, or grouped into a
single subtree. Downstream code only needs to update import paths and, in
a handful of cases, package-name identifiers.

If you want the one-liner: **prefer `github.com/Viking602/go-hydaelyn`
(the root façade) over `host`** for `New`, `Config`, `Runtime`, and
`StartTeamRequest`. Everything else is a straight rename.

## Why

Before this release the module exposed 39 top-level packages. That is
the surface every tagged version has to remain compatible with forever,
and it contained multiple pain points:

- Singular/plural collisions: `provider` **and** `providers`,
  `pattern` vs `patterns`, `auth` + `gate` + `security`, three `eval*`
  packages.
- Implementation detail leaking through public API: `blackboard`,
  `workflow`, `program`, `session`, `compact`, `middleware`, `plugin`,
  `slow` were all public yet mostly intended for internal composition.
- `errors` as a package name forced downstream callers to alias it away
  from `stdlib` `errors`.
- `examples/` got pulled into `go build ./...` of any downstream module.

After this refactor, the module presents **15 public top-level packages**
plus `cli/` and `cmd/`, one `_examples/` tree that Go's build rules
ignore, and a private `internal/` tree for the nine implementation-detail
packages.

## Full migration table

### Moved to `internal/` (no longer importable from outside the module)

| Before                                   | After                                  | Access from outside |
|------------------------------------------|----------------------------------------|---------------------|
| `github.com/.../auth`                    | `internal/auth`                        | via `host.AuthDriver` / `host.StaticAuth` aliases |
| `github.com/.../gate`                    | `internal/security`                    | via `capability.SecurityContext` + context helpers |
| `github.com/.../security`                | `internal/security`                    | via `capability.SecurityContext` + context helpers |
| `github.com/.../blackboard`              | `internal/blackboard`                  | via `team.Blackboard*` aliases |
| `github.com/.../workflow`                | `internal/workflow`                    | via `host.Workflow*` aliases |
| `github.com/.../program`                 | `internal/program`                     | via `pattern/research.Program*` aliases |
| `github.com/.../session`                 | `internal/session`                     | via `host.Session*` aliases |
| `github.com/.../middleware`              | `internal/middleware`                  | via `host.Middleware*` aliases |
| `github.com/.../compact`                 | `internal/compact`                     | via `host.Compactor` aliases |
| `github.com/.../plugin`                  | `internal/plugin`                      | via `host.Plugin*` aliases |
| `github.com/.../slow`                    | `internal/slow`                        | internal only |
| `github.com/.../errors`                  | `internal/errs`                        | internal only |

### Renamed at the top level

| Before                                   | After                                   |
|------------------------------------------|-----------------------------------------|
| `github.com/.../patterns` (tree)         | `github.com/.../pattern` (tree)         |
| `github.com/.../mcp`                     | `github.com/.../transport/mcp`          |
| `github.com/.../fixtures/` (JSON data)   | `github.com/.../testdata/`              |
| `github.com/.../examples/` (seven mains) | `github.com/.../_examples/` (skipped by `./...`) |

### Provider family unified

| Before                                   | After                                   |
|------------------------------------------|-----------------------------------------|
| `github.com/.../providers/anthropic`     | `github.com/.../provider/anthropic`     |
| `github.com/.../providers/openai`        | `github.com/.../provider/openai`        |
| `github.com/.../providers/shared`        | `github.com/.../provider/shared`        |

### Tool family unified

| Before                                   | After                                    | Package name |
|------------------------------------------|------------------------------------------|--------------|
| `github.com/.../toolkit`                 | `github.com/.../tool/kit`                | `kit`        |
| `github.com/.../tooltest`                | `github.com/.../tool/tooltest`           | `tooltest`   |

### Eval family unified

| Before                                   | After                                    | Package name |
|------------------------------------------|------------------------------------------|--------------|
| `github.com/.../evaluation`              | `github.com/.../eval`                    | `eval`       |
| `github.com/.../evalrun`                 | `github.com/.../eval/run`                | `run`        |
| `github.com/.../evalcase`                | `github.com/.../eval/cases`              | `cases`      |

> Note: `cases` (plural) because `case` is a Go keyword and cannot be
> used as a package name.

## New: root façade

```go
import "github.com/Viking602/go-hydaelyn"

runtime := hydaelyn.New(hydaelyn.Config{})
```

The following identifiers are now available on the root package as
type aliases — values are fully interchangeable with the subpackage
types:

- `hydaelyn.Runtime`          (= `host.Runtime`)
- `hydaelyn.Config`           (= `host.Config`)
- `hydaelyn.StartTeamRequest` (= `host.StartTeamRequest`)
- `hydaelyn.Profile`          (= `team.Profile`)
- `hydaelyn.Role`             (= `team.Role`)

Lower-level composition (registering middleware, plugins, storage
drivers, etc.) still goes through the subpackages — the façade covers
the common path only.

## What did **not** change

- Public capability / planner / scheduler / storage interfaces —
  signatures unchanged, only import path stability.
- Runtime behavior, wire format, blackboard semantics, event ordering.
- `go.mod` path is unchanged; this is still
  `github.com/Viking602/go-hydaelyn`.
- v1.0.0 is **not** being cut yet. v0.4 is a deliberate waypoint so we
  can iterate on the aliased surface a few more releases before freezing.

## Upgrade recipe

1. Update imports using the table above. For a quick repo-wide sweep:
   ```bash
   # macOS BSD sed; adjust for GNU if needed
   find . -name "*.go" -exec sed -i '' \
     -e 's|go-hydaelyn/providers/|go-hydaelyn/provider/|g' \
     -e 's|go-hydaelyn/toolkit"|go-hydaelyn/tool/kit"|g' \
     -e 's|go-hydaelyn/tooltest"|go-hydaelyn/tool/tooltest"|g' \
     -e 's|go-hydaelyn/patterns/|go-hydaelyn/pattern/|g' \
     -e 's|go-hydaelyn/mcp"|go-hydaelyn/transport/mcp"|g' \
     -e 's|go-hydaelyn/evaluation"|go-hydaelyn/eval"|g' \
     -e 's|go-hydaelyn/evalrun"|go-hydaelyn/eval/run"|g' \
     -e 's|go-hydaelyn/evalcase"|go-hydaelyn/eval/cases"|g' \
     {} +
   ```
2. Update identifiers where the package name changed:
   - `toolkit.` → `kit.`
   - `evaluation.` → `eval.`
   - `evalrun.` → `run.`
   - `evalcase.` → `cases.`
3. If you were importing `errors` from this module (rare), switch to
   whatever exported error sentinels you actually needed — most live on
   `capability`, `host`, or `team`.
4. Run `go build ./...` and `go test ./...` to confirm.

If something broke that isn't covered above, please open an issue —
this release is explicitly about tightening the surface ahead of v1,
and edge cases are worth fixing while we still have the flexibility.
