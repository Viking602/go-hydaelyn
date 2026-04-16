# ADR-003 Plugin Registry 与生命周期

## 状态

已接受

## 背景

仓库原先的扩展点是分散注册：

- `RegisterProvider`
- `RegisterTool`
- `RegisterHook`
- `RegisterWorkflow`

这种方式的问题是扩展面不统一，也无法表达插件级配置与治理边界。

## 决策

- 引入 `plugin.Registry`
- 注册键固定为 `type/name`
- 首批统一插件类型：
  - `provider`
  - `tool`
  - `planner`
  - `verifier`
  - `storage`
  - `memory`
  - `observer`
  - `scheduler`
  - `mcp_gateway`
- `Runtime.RegisterPlugin` 成为统一入口
- 原有 `RegisterProvider` / `RegisterTool` 保留为兼容 API，但底层同步写入插件注册表

## 生命周期

- 注册时先进入 registry
- 对当前 runtime 已知的类型做接线：
  - `provider` 接到 provider map
  - `tool` 接到 tool bus
  - `storage` 接到 runtime storage
  - `observer` 接到 hook chain
- 其他类型先进入 registry，等待后续里程碑把真实执行面接通

## 影响

- 扩展面从散装注册转成统一控制面
- 后续 plugin 配置、观测、治理和生态拆分有了稳定挂点
