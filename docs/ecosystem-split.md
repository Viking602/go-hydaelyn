# Ecosystem Split Boundary

## Core Repo

`go-hydaelyn` 核心仓库保留：

- runtime 核心
- 统一模型
- 统一治理层
- planner / blackboard / capability / scheduler 抽象
- CLI 与官方示例

对应包：

- `agent`
- `blackboard`
- `capability`
- `cli`
- `host`
- `mcp`
- `observe`
- `planner`
- `plugin`
- `scheduler`
- `storage`
- `team`
- `tool`
- `toolkit`

## Ecosystem Repos

以下能力适合外置到生态仓库：

- `go-hydaelyn-provider-openai`
- `go-hydaelyn-provider-anthropic`
- `go-hydaelyn-storage-postgres`
- `go-hydaelyn-observe-otel`
- `go-hydaelyn-tool-mcp`
- `go-hydaelyn-pattern-deepsearch`
- `go-hydaelyn-pattern-debate`

## 当前仓库内的过渡实现

为了保持 examples、tests 和 quickstart 可用，当前仓库仍保留：

- `providers/openai`
- `providers/anthropic`
- `patterns/deepsearch`
- `transport/mcp/client`

这些目录在 v1 阶段视为“过渡内置实现”，后续迁出时：

- public API 不变
- import path 变化通过 migration note 说明
- 核心 runtime 只依赖抽象，不依赖具体商业 provider

## Split Criteria

当某个能力同时满足以下条件时，优先迁出：

- 依赖外部服务或外部基础设施
- 不属于 runtime 最小闭环
- 可以通过 plugin/capability 接口独立实现
- 不迁出也不会阻止用户构建最小可运行系统
