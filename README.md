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

## Design direction

- Multi-agent execution is the primary abstraction.
- `agent.Engine` stays as the worker kernel, not the top-level runtime.
- Full subagents get their own private sessions and budgets.
- Supervisor-visible state stays structured and shared; worker scratchpads stay isolated.
- MCP is treated as a compatibility layer for external tools only.

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

## Current limits

- `providers/openai` and `providers/anthropic` are still scaffolds; their real remote streaming integrations are the next step.
- V1 runs multi-agent teams in a single process with goroutine-based parallelism.
- MCP resources/prompts are intentionally not part of the current core model; only external MCP tools are imported.
