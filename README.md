# Hydaelyn

Hydaelyn is a multi-agent parallel framework for Go.

It is designed around supervisor-controlled teams, full subagents, isolated worker sessions, and strong parallel search/research patterns. MCP is supported as an external tool integration path, not as Hydaelyn's core runtime model and not as a server surface exported by Hydaelyn.

## Current V1 baseline

- `agent`: single-agent worker kernel with model loop and tool orchestration.
- `team`: supervisor, worker profile, task, result, and run-state models.
- `host`: embeddable runtime with both low-level `Prompt()` and high-level `StartTeam()/ResumeTeam()/AbortTeam()`.
- `patterns/deepsearch`: first multi-agent pattern for parallel research, verification, and synthesis-style flows.
- `session`: isolated team sessions and worker sessions.
- `toolkit`: developer-facing APIs for local tools, HTTP/process tools, MCP tool import, and team/profile builders.
- `transport/mcp/client`: low-level MCP client transport for importing external MCP tools.
- `transport/http/admin`: optional control-plane HTTP handler.
- `plugin`: unified `type/name` plugin registry for provider, tool, storage, observer, planner, verifier, scheduler, memory, and MCP gateway slots.
- `middleware`: runtime interceptor chain spanning team, task, agent, llm, tool, memory, and phase-driven planner/verify/synthesize events.
- `planner`: typed plan schema plus `Plan / Review / Replan` interfaces for supervisor-driven orchestration.
- `blackboard`: shared Source / Artifact / Evidence / Claim / Finding / VerificationResult state for evidence-first collaboration.
- `capability`: unified LLM/tool/search/remote-agent invoker with typed usage/error results and governance middleware.
- `observe`: memory-backed tracing/metrics observer with runtime and capability middleware adapters.
- `scheduler`: task queue and lease primitives for future distributed worker execution.
- `mcp`: gateway adapters that can import external MCP tools into the runtime through the plugin surface.

## Design direction

- Multi-agent execution is the primary abstraction.
- `agent.Engine` stays as the worker kernel, not the top-level runtime.
- Full subagents get their own private sessions and budgets.
- Supervisor-visible state stays structured and shared; worker scratchpads stay isolated.
- MCP is treated as a compatibility layer for external tools only.
- Extension points are converging on `plugin.Registry` plus middleware-driven governance instead of one-off registration APIs.
- Planner-driven team startup and review/replan loops are available when a planner plugin is registered and selected on `StartTeamRequest`.
- Research tasks now publish normalized, deduped, redacted evidence into a blackboard, and verification-aware synthesis only uses supported claims.
- LLM and tool paths now route through a shared capability invoker, so governance middleware can observe both.
- Search and remote-agent capabilities can now be registered and invoked through the same runtime entrypoint.
- Team/task/llm/tool paths can now emit spans and counters through a shared observer interface.
- A memory-backed task queue and lease model now exists as the first distributed scheduling primitive.
- MCP gateway plugins can now import external MCP tools directly into runtime tool registration.

## Install

```bash
go get github.com/Viking602/go-hydaelyn@latest
```

## CLI

```bash
go run ./cmd/hydaelyn init .
go run ./cmd/hydaelyn new team.json
go run ./cmd/hydaelyn run --request team.json --events events.json
go run ./cmd/hydaelyn inspect --events events.json
go run ./cmd/hydaelyn replay --events events.json
```

## Quick example

```go
runtime := host.New(host.Config{})
runtime.RegisterProvider("fake", myProvider)
runtime.RegisterPattern(deepsearch.New())

runtime.RegisterProfile(toolkit.Profile(
	"supervisor",
	toolkit.WithRole(team.RoleSupervisor),
	toolkit.WithModel("fake", "test-model"),
))

runtime.RegisterProfile(toolkit.Profile(
	"researcher",
	toolkit.WithRole(team.RoleResearcher),
	toolkit.WithModel("fake", "test-model"),
	toolkit.WithToolNames("search"),
	toolkit.WithMaxConcurrency(2),
))

state, _ := runtime.StartTeam(context.Background(), host.StartTeamRequest{
	Pattern:           "deepsearch",
	Planner:           "supervisor-planner",
	SupervisorProfile: "supervisor",
	WorkerProfiles:    []string{"researcher", "researcher"},
	Input: map[string]any{
		"query":      "parallel agent search",
		"subqueries": []string{"architecture", "tooling"},
	},
})

fmt.Println(state.Result.Summary)
```

## MCP integration

Use MCP only to import external tools into worker profiles:

```go
client := mcpclient.New(mcpclient.NewHTTPTransport("http://localhost:8080/mcp", nil))
drivers, _ := toolkit.ImportMCPTools(context.Background(), client)
for _, driver := range drivers {
	runtime.RegisterTool(driver)
}
```

Those imported tools can then be attached to any worker profile via `ToolNames`.

## Official examples

- [examples/research](/Users/viking/GolandProjects/go-hydaelyn/examples/research/main.go)
- [examples/tooling](/Users/viking/GolandProjects/go-hydaelyn/examples/tooling/main.go)
- [examples/approval](/Users/viking/GolandProjects/go-hydaelyn/examples/approval/main.go)
- [examples/durable](/Users/viking/GolandProjects/go-hydaelyn/examples/durable/main.go)

## Benchmark

```bash
go test ./host -bench BenchmarkDeepsearchRuntime -run '^$'
```

## Current limits

- `providers/openai` and `providers/anthropic` are still scaffolds; their real remote streaming integrations are the next step.
- V1 runs multi-agent teams in a single process with goroutine-based parallelism.
- MCP resources/prompts are intentionally not part of the current core model; only external MCP tools are imported.
- Planner-driven startup/review/replan exists, task assignment honors role/capability/budget/concurrency, verification-aware synthesis consumes structured blackboard state, capability middleware governs LLM/tool calls, and the OpenAI/Anthropic providers now support real SSE streaming. MCP/search/remote-agent coverage and durable runtime are still pending.

## Releases

This repository uses tag-driven releases. Push a semver tag like `v0.1.0` and the `release` GitHub Action will run tests, validate module-version rules, and create the GitHub Release automatically. See [RELEASING.md](RELEASING.md).
