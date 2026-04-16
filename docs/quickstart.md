# Quickstart

## 1. 最小运行

使用内置 fake provider 跑一个 `deepsearch` team：

```go
runtime := host.New(host.Config{})
runtime.RegisterProvider("fake", myProvider)
runtime.RegisterPattern(deepsearch.New())

runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "fake", Model: "test"})
runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "fake", Model: "test"})

state, err := runtime.StartTeam(context.Background(), host.StartTeamRequest{
	Pattern:           "deepsearch",
	SupervisorProfile: "supervisor",
	WorkerProfiles:    []string{"researcher", "researcher"},
	Input: map[string]any{
		"query":      "parallel research",
		"subqueries": []string{"architecture", "tooling"},
	},
})
```

## 2. Planner 驱动启动

如果注册了 planner plugin，可以直接在 `StartTeamRequest.Planner` 里指定：

```go
state, err := runtime.StartTeam(ctx, host.StartTeamRequest{
	Pattern:           "deepsearch",
	Planner:           "supervisor-planner",
	SupervisorProfile: "supervisor",
	WorkerProfiles:    []string{"researcher"},
	Input:             map[string]any{"query": "approval flow"},
})
```

## 3. CLI

当前 CLI 提供最小文件工作流：

```bash
hydaelyn init .
hydaelyn new team.json
hydaelyn run --request team.json --events events.json
hydaelyn inspect --events events.json
hydaelyn replay --events events.json
```

## 4. Durable / Observe

- `runtime.TeamEvents(ctx, teamID)` 可读取 team 事件流
- `runtime.ReplayTeamState(ctx, teamID)` 可按事件流重建 `RunState`
- `runtime.UseObserver(observer)` 可接 team/task/llm/tool 的 trace/counter/histogram/log
