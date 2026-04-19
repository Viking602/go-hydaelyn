# Naming and Format Review

## Scope of this pass

This pass records the naming cleanup and safe file-only splits that were already applied. It does **not** authorize package moves, layer reshuffles, `internal/` adoption, import-path changes, or a new framework layering model.

The outcome of this review is intentionally narrow:

- approve cosmetic filename cleanup where package context already carries meaning
- approve file splits that sharpen responsibilities **inside the same package**
- preserve existing package and layer boundaries unless there is evidence stronger than file size alone

## Applied naming rules

The applied decisions follow the existing repository rules captured in the rename matrix:

- avoid package-name stutter when the package already provides the missing context
- prefer filenames that describe the dominant responsibility in the file
- keep `runtime` in filenames only when it still disambiguates or marks the package composition root
- in `host` package tests, prefer `subsystem_test.go` and drop the redundant `runtime_` prefix
- treat repeated package/file names as acceptable when the file is the natural package entrypoint or protocol facade

## Applied rename outcomes

The rename pass approved **11 cosmetic file renames** with no package, import-path, or exported-symbol changes:

1. `host/runtime_blackboard.go` -> `host/blackboard.go`
2. `host/runtime_dataflow.go` -> `host/dataflow.go`
3. `host/runtime_events.go` -> `host/events.go`
4. `host/runtime_event_reasons.go` -> `host/event_reasons.go`
5. `host/runtime_planning.go` -> `host/planning.go`
6. `host/runtime_queue.go` -> `host/queue.go`
7. `host/runtime_manage.go` -> `host/team_ops.go`
8. `host/runtime_stage.go` -> `host/stages.go`
9. `host/runtime_capability.go` -> `host/capability_handlers.go`
10. `providers/openai/openai.go` -> `providers/openai/driver.go`
11. `providers/anthropic/anthropic.go` -> `providers/anthropic/driver.go`

These changes were approved because the new basenames name the subsystem directly while keeping the code in the same package and directory.

## Applied split outcomes

Two file-only splits were approved and completed.

### `storage`

`storage/storage.go` was split into:

- `storage/types.go` for shared storage model types and event enums
- `storage/interfaces.go` for store contracts and `Driver`
- `storage/memory.go` for the in-memory implementation

This preserved the existing `storage` package while clarifying the one-way dependency direction: contracts/types first, implementation second.

### `recipe`

`recipe/recipe.go` was split into:

- `recipe/spec.go` for schema and planner wrapper types
- `recipe/decode.go` for YAML/JSON/file decode entrypoints
- `recipe/compile.go` for compile flow, task expansion, and request assembly

This preserved the existing `recipe` package while separating schema, decode, and compile concerns along an already-present seam.

## No-change decisions

The following candidates were explicitly kept as-is in this pass:

### `host/runtime.go`

- kept as the main composition root for `host.Config`, `host.Runtime`, `host.New`, registration, and top-level runtime wiring
- protected by the repository's explicit runtime exception

### `host` test files

- host package tests now follow package-context naming such as `prompt_test.go`, `queue_test.go`, and `scheduler_test.go`
- the earlier exceptions for `host/runtime_test.go` and `host/runtime_queue_test.go` are retired
- keep `runtime` only on the production composition-root file `host/runtime.go`

### transport entrypoint files

- `transport/mcp/client/client.go`
- `transport/mcp/jsonrpc/jsonrpc.go`
- `transport/http/control/control.go`

These remained unchanged because repetition was judged intentional, not accidental. Each file acts as a package entrypoint or protocol/admin facade, so renaming would have been cosmetic without improving discoverability.

## Deferred decision: `host/runtime.go`

`host/runtime.go` remains **deferred**.

This pass found conceptual seams inside the file, but not extraction seams strong enough to justify further splitting or any package/layer change. The file still couples:

- runtime construction and configuration wiring
- provider/tool/workflow/plugin registration
- prompt and session execution
- team and workflow orchestration
- scheduler and lease behavior
- persistence, event emission, and runtime coordination through shared `Runtime` state

That coupling means file size alone is not enough evidence for a safe architecture move. A future pass must produce stronger evidence than size alone, such as narrower internal interfaces, reduced cross-calls, or a proven low-risk seam around composition, execution, or persistence/eventing boundaries.

**Conclusion for this item:** naming cleanup did **not** authorize package or layer changes, and `host/runtime.go` stays deferred until a future pass has stronger evidence than size alone.

## OSS comparison points

This outcome is consistent with common Go OSS naming practice:

- **Temporal SDK**: accepts package-entrypoint naming like `client/client.go`; repetition is fine when the file is the public package face.
- **Traefik**: uses responsibility-first filenames like `pkg/server/server.go` when a package has a dominant surface and no extra layer move is required.
- **Kubernetes client-go**: separates large systems by responsibility and generated surface area, but keeps stable package contracts and typed clients rather than treating filename cleanup as permission to redesign package structure.
- **chi**: keeps compact package-level entry files such as `chi.go`, `chain.go`, and `mux.go`, showing that discoverable facades and responsibility naming can coexist.
- **tRPC-Agent-Go**: documents architecture in terms of package responsibilities (`agent`, `runner`, `session`, `tool`, etc.), which reinforces the same principle used here: clarify responsibilities first, but do not infer a new layering model unless the architecture evidence is stronger.

The comparison point is not that Hydaelyn should copy any one project. The shared lesson is narrower: Go OSS usually treats filename cleanup and intra-package splits as local clarity work, not automatic authorization for package churn.

## Architecture conclusion for this pass

This review approves:

- the 11 applied cosmetic renames
- the `storage` split into `types.go`, `interfaces.go`, and `memory.go`
- the `recipe` split into `spec.go`, `decode.go`, and `compile.go`

This review explicitly does **not** approve:

- package moves
- package renames
- import-path changes
- `internal/` adoption
- new framework layering
- splitting `host/runtime.go` based on size alone

Final conclusion: the naming cleanup improved local clarity, but it did **not** authorize package/layer changes. `host/runtime.go` remains deferred because its current composition-root and shared-state coupling require stronger architectural evidence than size alone.
