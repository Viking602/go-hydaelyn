# go-hydaelyn-v0-10-stabilization - Work Plan

## TL;DR (For humans)
<!-- Fill this LAST, after the detailed plan below is written, so it summarizes the REAL plan. -->
<!-- Plain English for a non-engineer: NO file paths, NO todo numbers, NO wave/agent/tool names. -->

**What you'll get:** A release-ready v0.10.0 candidate whose MCP client interoperates with real servers, whose security and local CI gates are green, whose CLI reports the installed version, and whose documentation matches the shipped product.

**Why this approach:** Protocol and release correctness come before deferred features. The official MCP Go SDK replaces a custom wire implementation that already violates required lifecycle, stdio, and HTTP rules; the rest are deliberately small, direct fixes.

**What it will NOT do:** It will not add advanced schedulers, memory or artifact backends, observability products, or storage changes. It will prepare but not publish a release.

**Effort:** Large
**Risk:** Medium - the MCP transport is public and security-sensitive, but migration to the official SDK reduces long-term protocol risk.
**Decisions I made for you:** Treat the next release as v0.10.0; keep Go language level 1.25 and patch only the toolchain; pin official MCP Go SDK v1.6.1 behind Hydaelyn DTOs; accept `stream` as a documented neutral multiagent dependency; do not publish automatically.

Your next move: execute this plan with `$start-work`; publication remains a separate explicit action. Full execution detail follows below.

---

> TL;DR (machine): Large, medium-risk stabilization; patch Go/macOS gates, migrate MCP to official SDK, fix CLI/versioning, enforce architecture and release contracts, prepare v0.10.0 docs.

## Scope
### Must have

- Remove every known release blocker: GO-2026-5856, the macOS path-alias test failure, the incorrect CLI version, and the broken `@latest` install story.
- Make the MCP client conform through the official Go SDK while preserving Hydaelyn-owned DTOs and the common constructor/method call shapes.
- Turn the documented one-way architecture rules into executable CI checks.
- Prepare truthful v0.10.0 release notes and a release workflow that proves tag ancestry and installed behavior.

### Must NOT have (guardrails, anti-slop, scope boundaries)

- Do not implement deferred advanced schedulers, Memory/Artifact backends, OTel exporters, or production packs.
- Do not change storage schemas or the `api.StoreProvider` contract.
- Do not retain the custom MCP Content-Length framing as a fallback; a compatibility shim for a protocol-invalid wire format creates permanent ambiguity.
- Do not expose official SDK types from Hydaelyn's `Resource`, `Prompt`, `ContentBlock`, or tool result API.
- Do not add platform skips for macOS; compare filesystem identity correctly.
- Do not tag, push, publish, or mutate GitHub state as part of plan execution.

## Verification strategy
> Zero human intervention - all verification is agent-executed.
- Test decision: TDD for the MCP protocol and CLI/install regressions; tests-after for documentation/import-gate wiring; Go standard `testing` plus repository shell gates.
- Every todo captures its command output under `.omo/evidence/task-<N>-go-hydaelyn-v0-10-stabilization.txt`.
- The final gate runs on Go 1.25.12 and must not skip any test.

## Execution strategy
### Parallel execution waves
> Target 5-8 todos per wave. Fewer than 3 (except the final) means you under-split.

- Wave 1, independent: Todos 1, 4, and 5.
- Wave 2, MCP critical path: Todo 2, then Todo 3.
- Wave 3, integration and release preparation: Todos 6 and 7 after all code/API decisions are final.
- Final wave: F1-F4 run independently after Todos 1-7.

### Dependency matrix
| Todo | Depends on | Blocks | Can parallelize with |
| --- | --- | --- | --- |
| 1 | none | 6, 7 | 4, 5 |
| 2 | 1 | 3, 6, 7 | 4, 5 |
| 3 | 2 | 6, 7 | none on MCP files |
| 4 | none | 6, 7 | 1, 5 |
| 5 | none | 6, 7 | 1, 4 |
| 6 | 1, 2, 3, 4, 5 | 7 | none on workflow files |
| 7 | 1, 2, 3, 4, 5, 6 | final wave | none on docs |

## Todos
> Implementation + Test = ONE todo. Never separate.
<!-- APPEND TASK BATCHES BELOW THIS LINE WITH edit/apply_patch - never rewrite the headers above. -->
- [x] 1. Patch the supported Go toolchain and make the stdio directory test filesystem-correct
  What to do / Must NOT do: Change only the `toolchain` directive from `go1.25.11` to `go1.25.12`; keep the language directive at `go 1.25.0`. In `TestDialStdioHonorsDir`, compare `os.FileInfo` values with `os.SameFile` (or equivalently evaluate both existing paths) instead of raw strings. Add a failure assertion that two distinct directories are not accepted. Do not add `runtime.GOOS` conditionals or skip the test.
  Parallelization: Wave 1 | Blocked by: none | Blocks: 2, 6, 7
  References (executor has NO interview context - be exhaustive): `go.mod:3-5`; `transport/mcp/client/client_test.go:312-355`; Go 1.25.12 release note `https://go.dev/doc/devel/release#go1.25.12`; observed failure `/var/...` vs `/private/var/...`; observed GO-2026-5856 call trace through `transport/mcp/client/client.go:216`.
  Acceptance criteria (agent-executable): `go mod tidy` produces only the intended dependency/checksum changes; `GOTOOLCHAIN=go1.25.12 go test ./transport/mcp/client -run TestDialStdioHonorsDir -count=10`; `GOTOOLCHAIN=go1.25.12 govulncheck ./...` prints `No vulnerabilities found`; `git diff --check` passes.
  QA scenarios (name the exact tool + invocation): Happy: run the targeted test from a macOS temp directory whose logical and resolved paths differ. Failure: unit-test `os.SameFile` against a second real directory and require false. Evidence `.omo/evidence/task-1-go-hydaelyn-v0-10-stabilization.txt`.
  Commit: Y | `fix(build,test): patch Go toolchain and compare working directory identity`

- [x] 2. Replace the custom MCP wire engine with the official session lifecycle and stdio transport
  What to do / Must NOT do: Pin `github.com/modelcontextprotocol/go-sdk v1.6.1`. Refactor `Client` to create an official `mcp.Client`, call `Connect` during `Initialize`, retain the resulting `*mcp.ClientSession`, and map `InitializeResult` back into Hydaelyn's existing DTO. Preserve `New(transport)`, `Initialize`, `ListTools`, `CallTool`, `ListResources`, `ReadResource`, `ListPrompts`, `GetPrompt`, and `Close` call shapes. Define `ErrNotInitialized` and return it instead of panicking when operations precede initialization. Reimplement `NewStreamTransport` with official `mcp.IOTransport`. Because v1.6.1 `mcp.CommandTransport` cannot wrap stdout and has no inbound limit hook, implement `DialStdio` as a bounded command-process adapter feeding the official `mcp.IOTransport`; preserve environment scrub, optional inherited environment, working directory, args, bounded shutdown, and the historical 4 MiB per-message safety boundary. A standard-library `json.Valid` check may validate that each physical NDJSON line is one complete JSON value so embedded newlines cannot bypass the byte boundary; all deserialization and MCP semantics remain in the SDK. Delete `transport/mcp/jsonrpc/` after all production references disappear. Do not retain Content-Length framing, deserialize protocol messages locally, or implement a second MCP lifecycle state machine.
  Parallelization: Wave 2 | Blocked by: 1 | Blocks: 3, 6, 7
  References (executor has NO interview context - be exhaustive): `transport/mcp/client/client.go:34-179,238-579`; `transport/mcp/jsonrpc/jsonrpc.go:18-188`; `transport/mcp/client/client_test.go:171-706`; official SDK APIs `mcp.NewClient`, `Client.Connect`, `ClientSession.InitializeResult`, `mcp.IOTransport`, `mcp.CommandTransport`; MCP lifecycle `https://modelcontextprotocol.io/specification/2025-06-18/basic/lifecycle`; MCP stdio wire rule `https://modelcontextprotocol.io/specification/2025-06-18/basic/transports`.
  Acceptance criteria (agent-executable): A strict official SDK server observes `initialize` first, a non-empty capabilities object, negotiated supported protocol, and `notifications/initialized` before any list/call operation. Stdio messages are newline-delimited JSON. `go list -deps ./transport/mcp/client` contains the official SDK; `rg 'Content-Length' transport/mcp` returns no production framing code. `go test -race ./transport/mcp/client -count=20` passes.
  QA scenarios (name the exact tool + invocation): Happy: an in-test official SDK stdio server exposes one tool; Hydaelyn dials, initializes, lists, calls, and closes it. Failure: list before initialize returns `ErrNotInitialized`; an unsupported negotiated protocol closes the session and returns an error. Evidence `.omo/evidence/task-2-go-hydaelyn-v0-10-stabilization.txt`.
  Commit: Y | `fix(mcp): use official lifecycle and newline stdio transport`

- [x] 3. Move HTTP MCP onto official Streamable HTTP and prove session/SSE behavior
  What to do / Must NOT do: Implement `HTTPTransport.Connect` as a thin wrapper around `mcp.StreamableClientTransport`. Preserve same-origin custom headers with a small cloning `http.RoundTripper`, keep a 30-second response-header timeout without imposing a total body timeout on long-lived SSE, and let the official SDK own Accept negotiation, JSON versus SSE responses, `Mcp-Session-Id`, `MCP-Protocol-Version`, retries, cancellation, and DELETE close. Never forward configured credentials across origins or HTTPS downgrades. Convert official tool/resource/prompt result types into existing Hydaelyn DTOs. Normalize official JSON-RPC errors into the existing `RPCError` contract or document a single replacement type; HTTP and stdio must expose the same error semantics. A bounded reader may recognize only the empty-line SSE framing boundary needed to reset a per-event byte counter; do not parse SSE fields or payloads locally, and do not duplicate SDK JSON, retry, or session state.
  Parallelization: Wave 2 | Blocked by: 2 | Blocks: 6, 7
  References (executor has NO interview context - be exhaustive): `transport/mcp/client/client.go:181-235,262-278`; `transport/mcp/client/client_test.go:22-169`; official `mcp.StreamableClientTransport`; MCP Streamable HTTP requirements `https://modelcontextprotocol.io/specification/2025-06-18/basic/transports`; current missing headers/status handling at `client.go:210-231`.
  Acceptance criteria (agent-executable): Tests assert initial POST Accept includes both media types; later requests carry the negotiated protocol version and returned session ID; notification POST accepts 202 with an empty body; JSON and SSE responses both complete; Close issues session DELETE; HTTP and stdio return extractable typed RPC error codes. `go test -race ./transport/mcp/client -count=20` passes.
  QA scenarios (name the exact tool + invocation): Happy: drive an official SDK Streamable HTTP test server through Initialize/ListTools/CallTool/Close. Failure: return an unsupported protocol version, HTTP 400, HTTP 404 session expiry, and a JSON-RPC error; assert deterministic typed failures and no leaked goroutines. Evidence `.omo/evidence/task-3-go-hydaelyn-v0-10-stabilization.txt`.
  Commit: Y | `fix(mcp): adopt spec-compliant Streamable HTTP sessions`

- [x] 4. Derive the CLI version from Go build metadata and smoke the installed binary
  What to do / Must NOT do: Replace the hard-coded `v2.0.0-dev` with `runtime/debug.ReadBuildInfo`. Return the main module version for `go install module@version`; return a stable `devel` value for local builds. Make help use the same version source and remove historical `v2.0` wording. Keep commands and output destinations unchanged. Add an isolated GOBIN smoke that executes `version`, `--help`, and an invalid command. Do not introduce a VERSION file or require release-only ldflags.
  Parallelization: Wave 1 | Blocked by: none | Blocks: 6, 7
  References (executor has NO interview context - be exhaustive): `internal/cli/cli.go:1-45`; `internal/cli/cli_test.go:15-33`; `cmd/hydaelyn/main.go:11-15`; `cmd/hydaelyn/main_test.go:8-14`; observed `go run github.com/Viking602/go-hydaelyn/cmd/hydaelyn@v0.9.0 version` output `v2.0.0-dev`.
  Acceptance criteria (agent-executable): Local `go run ./cmd/hydaelyn version` prints `devel` (optionally plus VCS revision); an installed tag fixture prints its module version; help contains the same value; unknown command exits 1 on stderr. `go test ./internal/cli ./cmd/hydaelyn -count=10` passes.
  QA scenarios (name the exact tool + invocation): Happy: `GOBIN=$(mktemp -d) go install ./cmd/hydaelyn` then run version/help. Failure: run an unknown command and assert exit 1, empty stdout, non-empty stderr. Evidence `.omo/evidence/task-4-go-hydaelyn-v0-10-stabilization.txt`.
  Commit: Y | `fix(cli): report module version from build metadata`

- [x] 5. Make the load-bearing import boundaries executable and align the ADRs
  What to do / Must NOT do: Add `scripts/check-import-boundaries.sh` using `go list` to enforce only four rules: `api` imports no Hydaelyn package; `agent` never imports `multiagent`; `multiagent` never imports root, `worker`, or `internal`; root facade never imports `multiagent`. Explicitly allow neutral `stream` in `multiagent`, then update ADR-016, README, and `.sentrux/rules.toml` comments to state the real rule and Sentrux limitation. Keep the v0.8 tagged rules intact, but add an explicitly labeled post-release/current-main clarification to its boundaries/package-structure documents. Wire the script into `make architecture-check` and the existing CI architecture job. Do not model every package edge or add a graph framework.
  Parallelization: Wave 1 | Blocked by: none | Blocks: 6, 7
  References (executor has NO interview context - be exhaustive): `.sentrux/rules.toml:1-35`; `Makefile:70-74`; `.github/workflows/ci.yml` architecture-gate; `README.md:134-163`; `docs/adr/ADR-016-explicit-multi-agent-scheduler.md:137-145`; `docs/product-spec/v0.8.0/11-boundaries.md:61-68,148-189`; `docs/product-spec/v0.8.0/12-package-structure.md:205-225`; actual imports `multiagent/drive.go:11-12`, `multiagent/graph_exec.go:6-7`.
  Acceptance criteria (agent-executable): `./scripts/check-import-boundaries.sh` passes at HEAD. In a disposable copy, injecting each forbidden import makes the script exit non-zero and name the package/rule; adding an allowed `multiagent -> stream` import remains green. `make architecture-check` passes and output lists four checks including the new gate.
  QA scenarios (name the exact tool + invocation): Happy: run the script on the real graph. Failure: use a temporary copied package graph with `agent -> multiagent` and `multiagent -> internal/core` imports and assert rejection. Evidence `.omo/evidence/task-5-go-hydaelyn-v0-10-stabilization.txt`.
  Commit: Y | `build(architecture): enforce one-way package boundaries`

- [x] 6. Harden the tag release gate and exercise the install surface
  What to do / Must NOT do: In `release.yml`, verify `GITHUB_SHA` is an ancestor of `origin/main` before tests; install Sentrux at the same pinned version as CI and run `make architecture-check`; pin staticcheck and govulncheck versions instead of `@latest`; after the normal gate, install `cmd/hydaelyn` from the pushed tag into a temporary GOBIN with `GOPROXY=direct`, assert `version == GITHUB_REF_NAME`, run `--help`, and compile a temporary consumer importing `skill`. Update `RELEASING.md` to require `make ci-local` and the installed-runtime checks. Do not create a GitHub Release if any readback fails.
  Parallelization: Wave 3 | Blocked by: 1, 2, 3, 4, 5 | Blocks: 7
  References (executor has NO interview context - be exhaustive): `.github/workflows/release.yml:15-114`; `.github/workflows/ci.yml` tool and Sentrux setup; `RELEASING.md:5-46`; `README.md:59-65`; `skill/skill.go:1-33`; current tool versions from local verification: staticcheck `2026.1 (v0.7.0)`, govulncheck `v1.3.0`; tag ancestry rule `RELEASING.md:8`.
  Acceptance criteria (agent-executable): A workflow fixture with a non-main tag fails before release creation; a main-contained tag reaches verification. The isolated tagged install prints exactly the tag, help succeeds, invalid command exits 1, and a temp consumer can import and instantiate `skill.Skill`. Workflow YAML parses; `actionlint` passes if available.
  QA scenarios (name the exact tool + invocation): Happy: run the release script pieces against a local annotated test tag without pushing. Failure: create a disposable side-branch tag and assert the ancestry command fails. Evidence `.omo/evidence/task-6-go-hydaelyn-v0-10-stabilization.txt`.
  Commit: Y | `ci(release): verify ancestry and installed module surface`

- [x] 7. Reconcile v0.9 history and prepare truthful v0.10.0 release documentation
  What to do / Must NOT do: Rewrite `docs/product-spec/v0.9.0/README.md` as a historical record of the already released v0.9.0 scope; move unshipped scheduler/memory/observability items into an explicitly unversioned future backlog or a v0.10+ section without promising them in this stabilization release. Update `docs/product-spec/README.md`, `docs/plans/active-plan.md`, README package/status text, broken tracked links, and stale Position D contract/ADR explanations without changing storage behavior. Add `docs/release-notes/v0.10.0.md` covering Agent Skills, runtime hardening, MCP conformance, Go security patch, CLI version, and exact migration notes for low-level Transport, Gateway/kit interface signatures, and DTO definition ownership. Do not claim publication or availability until a tag exists.
  Parallelization: Wave 3 | Blocked by: 1, 2, 3, 4, 5, 6 | Blocks: final wave
  References (executor has NO interview context - be exhaustive): `docs/product-spec/v0.9.0/README.md:1-136`; `docs/product-spec/README.md`; `docs/plans/active-plan.md:1-113`; `README.md:39-65,134-174,203-218`; `docs/product-spec/v0.8.0/11-boundaries.md:13-15,170-177`; current release evidence `v0.9.0` at `78036a6`; unreleased range `v0.9.0..95e5110`; `RELEASING.md`.
  Acceptance criteria (agent-executable): `rg 'Not started|active product line is v0\.8|v2\.0\.0-dev|multiagent.*api/.*agent/.*stdlib only' README.md docs internal/cli -g '!docs/product-spec/v0.8.0/**'` returns no stale live claim; the v0.8 archive exclusion is documented. Every Markdown link passes the repository's link checker if present, otherwise a script verifies tracked and PR-candidate relative targets. Release notes say `Unreleased` and recommend `v0.10.0` without claiming it exists.
  QA scenarios (name the exact tool + invocation): Happy: a clean reader can trace latest release v0.9.0, unreleased v0.10 changes, and deferred backlog without contradiction. Failure: automated grep fixture detects a reintroduced `v0.9.0 Not started` or missing local link. Evidence `.omo/evidence/task-7-go-hydaelyn-v0-10-stabilization.txt`.
  Commit: Y | `docs(v0.10.0): align roadmap and release candidate scope`

## Final verification wave
> Runs in parallel after ALL todos. ALL must APPROVE. Surface results and wait for the user's explicit okay before declaring complete.
- [ ] F1. Plan compliance audit
  Verify every Must Have maps to a commit/test and every Must NOT Have has zero diff. Record `.omo/evidence/final-f1-plan-compliance.txt`.
- [ ] F2. Code quality review
  Review the full base diff for public API drift, official SDK type leakage, error consistency, resource cleanup, and redundant custom protocol code. Require zero HIGH findings. Record `.omo/evidence/final-f2-code-quality.txt`.
- [ ] F3. Real manual QA
  Run an official SDK stdio server and Streamable HTTP server through Hydaelyn; list and call a tool, read a resource, get a prompt, cancel one request, close both sessions. Install the CLI in isolated GOBIN and run version/help/bad input. Record `.omo/evidence/final-f3-manual-qa.txt`.
- [ ] F4. Scope fidelity
  Compare `git diff --name-status` against Todos 1-7, confirm no user-owned untracked files changed, no tag/push/release occurred, and deferred product features remain untouched. Record `.omo/evidence/final-f4-scope-fidelity.txt`.

## Commit strategy

- Seven focused commits in todo order; Todo 5 may land before MCP commits because it is independent.
- Keep implementation with its direct tests. Keep generated `go.sum` changes with the MCP dependency commit.
- Do not squash MCP stdio/lifecycle and HTTP sessions together; they are independently reviewable and revertible.
- Before each commit, re-read `git status --short --branch -uall` and stage only todo-owned paths. Existing `.omo/ulw-loop`, `AGENTS.md`, and `CLAUDE.md` remain untracked and untouched.

## Success criteria

- `go.mod` pins Go 1.25.12 and `govulncheck ./...` reports no reachable vulnerability.
- `make verify`, `make ci-local`, `make architecture-check`, full race tests, and `git diff --check` pass without skips on macOS.
- Official SDK-backed MCP stdio and HTTP both pass real lifecycle/manual QA; no production Content-Length MCP framing remains.
- Installed CLI reports its module tag, and an isolated consumer can import `skill` from the prepared v0.10.0 tag surface.
- CI enforces the real package-direction rules and release tags must belong to main.
- Docs consistently distinguish released v0.9.0, unreleased v0.10.0, and deferred backlog.
- No product code outside the enumerated components, no storage contract changes, and no external publication action.
