# SemVer And Compatibility

## 版本策略

从 `v1.0.0` 开始，Hydaelyn 遵循 SemVer：

- `MAJOR`: 破坏性 API 变更
- `MINOR`: 向后兼容的新能力
- `PATCH`: 向后兼容的修复

## Public API 边界

当前默认视为 public 的包：

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

以下包默认视为内部实现面，不承诺稳定：

- `providers/*`
- `transport/*`
- `tooltest`

## 兼容原则

- 对 public struct 新增字段：允许
- 删除或重命名 public 字段/方法：需要 MAJOR
- public enum/const 语义变化：需要 MAJOR
- CLI 新增命令/flag：允许
- CLI 删除命令/flag：需要 MAJOR
- 事件类型新增字段：允许
- 事件类型重命名或删除：需要 MAJOR

## Release Gate

发布前至少满足：

- `go test ./...`
- `go test ./... -race`
- `check_rules()` 通过
- README、Quickstart、Migration、Plugin Dev 文档同步
