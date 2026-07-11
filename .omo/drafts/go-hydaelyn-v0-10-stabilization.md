---
slug: go-hydaelyn-v0-10-stabilization
status: approved
intent: unclear
pending-action: none
approach: Stabilize the next minor release by fixing protocol correctness and release blockers first, then align architecture enforcement and documentation. Do not implement deferred product features.
---

# Draft: go-hydaelyn-v0-10-stabilization

## Components (topology ledger)
<!-- Lock the SHAPE before depth. One row per top-level component that can succeed or fail independently. -->
<!-- id | outcome (one line) | status: active|deferred | evidence path -->

| id | outcome | status | evidence path |
| --- | --- | --- | --- |
| C1 | Supported Go toolchain with a green local gate on macOS | active | `go.mod:5`, `transport/mcp/client/client_test.go:312-340` |
| C2 | Spec-compliant MCP client lifecycle, stdio, and Streamable HTTP | active | `transport/mcp/client/client.go:76-85,188-235,280-347,511-537`; `transport/mcp/jsonrpc/jsonrpc.go:134-188` |
| C3 | CLI reports the installed module version | active | `internal/cli/cli.go:16-17,37-43` |
| C4 | Architecture claims match executable import-boundary gates | active | `.sentrux/rules.toml:6-25`; `docs/adr/ADR-016-explicit-multi-agent-scheduler.md:137-145` |
| C5 | Release workflow enforces main ancestry and exercises the installed surface | active | `.github/workflows/release.yml:20-79`; `RELEASING.md:5-13` |
| C6 | Main-branch docs and next release metadata describe what actually ships | active | `README.md:44,59-65`; `docs/product-spec/v0.9.0/README.md:1-19`; `docs/plans/active-plan.md:10-12` |

## Open assumptions (announced defaults)
<!-- Intent is UNCLEAR: research resolves ambiguity, defaults are adopted (not asked), and each is surfaced in the plan's human TL;DR for veto. -->
<!-- assumption | adopted default | rationale | reversible? -->

| assumption | adopted default | rationale | reversible? |
| --- | --- | --- | --- |
| Version scope | Prepare `v0.10.0`, not `v0.9.1` | `skill/` is a new public feature after `v0.9.0`; SemVer requires a minor release | yes, before tagging |
| Go upgrade | Keep language level `go 1.25.0`; bump only `toolchain` to `go1.25.12` | Smallest fix for GO-2026-5856; no language migration is needed | yes |
| MCP implementation | Pin official `github.com/modelcontextprotocol/go-sdk` `v1.6.1` behind the existing Hydaelyn facade | The custom client violates multiple protocol MUSTs; protocol ownership is not a product differentiator | yes, but expensive after release |
| MCP API compatibility | Preserve `New`, `Initialize`, `NewHTTPTransport`, `NewStreamTransport`, and `DialStdio` call shapes where possible; allow only the low-level `Transport` implementation contract to change | Limits downstream churn while allowing official session/lifecycle semantics | yes before v1.0 |
| Architecture | Treat `stream/` as an allowed neutral dependency of `multiagent/`, then enforce the actual one-way boundaries | Production code already depends on `stream`; pretending otherwise is worse than documenting the real rule | yes via ADR amendment |
| Release action | Produce a release-ready commit set and notes, but do not tag, push, or publish | Publishing is an external irreversible action not requested in this task | yes |

## Findings (cited - path:lines)

1. CRITICAL: `go.mod:5` pins Go 1.25.11. `govulncheck ./...` reaches GO-2026-5856 through `transport/mcp/client/client.go:216`; Go 1.25.12 removes the finding.
2. CRITICAL: the MCP client declares protocol `2025-06-18` at `transport/mcp/client/client.go:79`, but initialization omits required client capabilities and never sends `notifications/initialized`; stdio uses Content-Length framing at `transport/mcp/jsonrpc/jsonrpc.go:134-188` although MCP stdio requires newline-delimited JSON; HTTP omits required Accept/protocol/session behavior at `transport/mcp/client/client.go:210-231`.
3. HIGH: `TestDialStdioHonorsDir` performs a raw path-string comparison at `transport/mcp/client/client_test.go:338-340`, so macOS `/var` versus `/private/var` aliases fail `make verify` and `make ci-local` even though the child is in the correct directory.
4. HIGH: installed `v0.9.0` reports `v2.0.0-dev` because `internal/cli/cli.go:16-17` hard-codes a historical architecture label; release builds do not override it.
5. HIGH: `README.md:44` advertises `skill/` and `README.md:62` installs `@latest`, but `@latest` resolves to `v0.9.0`, which does not contain `skill/`; a clean `go get github.com/Viking602/go-hydaelyn/skill@latest` fails.
6. STRUCTURAL: `docs/adr/ADR-016-explicit-multi-agent-scheduler.md:139-141` says `multiagent` imports only api/agent/stdlib, while `multiagent/drive.go:11-12` and `multiagent/graph_exec.go:6-7` import `stream`; `.sentrux/rules.toml:6-10` explicitly says layer direction is not modeled, while README claims CI enforces it.
7. STRUCTURAL: `RELEASING.md:8` requires the tagged commit to be on main, but `.github/workflows/release.yml:36-55` validates only tag syntax/module major and never checks ancestry.
8. DOCUMENTATION: `docs/product-spec/v0.9.0/README.md:5-19` still says v0.9.0 is not started and provisional after the v0.9.0 release; `docs/plans/active-plan.md:10-12` still calls v0.8.x active.

## Decisions (with rationale)

- Replace, rather than repair, the custom MCP protocol engine. The official SDK already implements lifecycle negotiation, newline stdio, Streamable HTTP, sessions, SSE, cancellation, and conformance coverage.
- Keep Hydaelyn DTOs at the package boundary so the official SDK does not leak through the public API.
- Use `runtime/debug.ReadBuildInfo` as the CLI version source; do not add a second version file or require ldflags for normal `go install` users.
- Add one small import-boundary shell gate for the load-bearing directional rules that Sentrux 0.5.7 cannot express. Do not model every package relationship.
- Treat deferred scheduler/memory/observability features as backlog, not part of stabilization.

## Scope IN

- Go patch toolchain, macOS test portability, MCP client migration, CLI version, architecture import gate, release workflow hardening, install smoke, v0.10.0 release notes and documentation alignment.

## Scope OUT (Must NOT have)

- No Debate/MapReduce/Swarm scheduler, memory pipeline, artifact backend, OTel exporter, new storage schema, public storage-contract change, or general large-file refactor.
- No tag creation, push, GitHub Release, or issue/PR mutation.
- No pre-release MCP SDK version and no new abstraction layer beyond the facade required to isolate the official SDK.

## Open questions

None. The user explicitly asked to continue and provide the detailed solution after the evidence brief; reversible defaults above are adopted for the plan.

## Approval gate
status: approved-by-user-followup
<!-- When exploration is exhausted and unknowns are answered, set status: awaiting-approval. -->
<!-- That durable record is the loop guard: on a later turn read it and resume at the gate instead of re-running exploration. -->
