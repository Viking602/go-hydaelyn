# ADR-004 Middleware 执行顺序与短路机制

## 状态

已接受

## 背景

原有 `hook.Chain` 只覆盖模型调用和工具调用前后，无法表达 team、task、agent、memory 等更高层级的治理链路。

## 决策

- 引入统一 `middleware.Chain`
- 采用 onion 顺序：
  - 先注册的 middleware 先进入 before
  - 后注册的 middleware 先进入 after
- `next` 不调用即视为短路
- runtime 现阶段接入以下阶段：
  - `team`
  - `task`
  - `agent`
  - `llm`
  - `tool`
  - `memory`
  - phase 映射出来的 `planner` / `verify` / `synthesize`
- `llm` / `tool` 通过 hook adapter 复用现有 agent engine 接口

## 影响

- timeout、retry、logging、tracing、permission 等治理逻辑不再需要分别嵌进每个子模块
- 中间件已具备独立启停能力
- 后续 capability 层和 durable runtime 可继续复用同一条链路
