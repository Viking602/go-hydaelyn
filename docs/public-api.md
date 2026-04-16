# Public API Freeze

## v1 Public API

从 `v1.0.0` 开始，下列包视为稳定 public API：

- `agent`
- `blackboard`
- `capability`
- `host`
- `mcp`
- `observe`
- `planner`
- `plugin`
- `scheduler`
- `team`
- `tool`
- `toolkit`

这些包的兼容承诺受 [SemVer And Compatibility](semver.md) 约束。

## Internal Surface

以下包当前仍视为实现面，不承诺长期稳定：

- `providers/*`
- `transport/*`
- `tooltest`

## Frozen Contracts

### Runtime

- `host.Config`
- `host.StartTeamRequest`
- `host.PromptRequest`
- `host.ContinueRequest`
- `host.DumpConfigRequest`
- `host.Runtime` 的已公开方法

### Team Model

- `team.Profile`
- `team.AgentInstance`
- `team.Task`
- `team.RunState`
- `team.Result`

### Planner

- `planner.PlanRequest`
- `planner.Plan`
- `planner.TaskSpec`
- `planner.ReviewDecision`
- `planner.ReplanInput`
- `planner.Planner`

### Capability

- `capability.Call`
- `capability.Result`
- `capability.Error`
- `capability.Middleware`
- `capability.Type*`

### Plugin

- `plugin.Spec`
- `plugin.Ref`
- `plugin.Type*`
- `plugin.Registry`

## Change Rules

- 给 frozen struct 增加可选字段：允许
- 删除字段、改字段含义、改返回类型：需要 MAJOR
- 删除 public method：需要 MAJOR
- 新增 public method：允许
- CLI 新增命令：允许
- 删除 CLI 命令：需要 MAJOR
