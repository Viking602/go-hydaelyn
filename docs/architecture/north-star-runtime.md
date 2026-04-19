# Hydaelyn 北极星架构

## 产品定位

Hydaelyn 的目标不是再做一个 Go 版 LLM 调用封装，而是做成一个 Go-native、可嵌入、可持久化、可观测、可治理、以 Supervisor 驱动 DAG 为核心的多 Agent Runtime。

## 保留项

- 保留 `agent.Engine` 作为单 agent 执行内核
- 保留 Go-native、embeddable 的宿主形态
- 保留 team、pattern、session 的基础分层
- 保留 worker 私有 session 隔离
- 保留 MCP 作为外部能力接入通道

## 强制重构项

- Pattern 不再承担全部调度逻辑，Supervisor 需要进入 `plan / review / replan` 主循环
- `Profile` 只保留为能力模板，运行中身份必须拆成 `AgentInstance`
- 调度器只执行 DAG runnable task，不能扫描全部 pending task
- 失败语义必须显式化，不能继续静默聚合
- Tool 默认权限必须改成 deny-by-default
- Provider、Tool、MCP、Search、VectorDB、Remote Agent 最终收敛到统一 capability 治理层
- Runtime 最终需要 event store、resume、approval、trace、metrics

## 当前仓库现状

基于 2026-04-16 的代码扫描（含补齐轮）：

- 依赖图当前是干净分层的，没有循环依赖
- v0.2 已修正 runtime DAG 语义、失败语义、agent identity 与工具默认权限（deny-by-default 通过 Bus.Subset 实现）
- v0.3 已引入 `plugin.Registry`、统一 `middleware.Chain` 和 runtime 配置合并/导出能力
- v0.4 已引入 `Planner` 接口、typed plan schema、budget-aware 调度选择、runtime review/replan loop，以及 planner-generated deepsearch 路径
- v0.5 已引入 `blackboard` 数据模型、publish pipeline（含 email/phone/SSN/credit-card PII redaction）、结构化 `VerificationResult`，以及只消费 supported claims 的综合路径
- v0.6 已引入 `capability` 包，把 LLM/tool 调用接入统一 invoker 和治理 middleware（含线程安全 RateLimit + 滑动窗口、指数退避 Retry、BudgetEnforcer），并补齐 OpenAI/Anthropic 的真实 SSE streaming provider
- v0.7 已引入 EventStore、event replay、pause/resume/abort、human-in-the-loop approval
- v0.8 已引入 OTel observer 适配器（回调注入零依赖）、全链路 trace、runtime control API、secret/PII redaction
- v0.9 已引入 TaskQueue（含 backpressure MaxPending/MaxInflight）、worker lease/heartbeat/recovery、MCP resources/prompts/tools 完整支持
- v1.0 CLI 支持 fake/openai/anthropic 三种 provider，4 个端到端示例（approval/durable/research/tooling），文档套齐全

后续重点为外置生态仓库实际拆分与分布式跨节点真实部署演示。

## 目标模块边界

```text
/agent        单 agent 执行内核
/team         profile、agent instance、task、result、run state
/planner      supervisor plan / review / replan
/scheduler    DAG 调度、lease、retry、queue
/session      私有 session、共享 session
/blackboard   finding、claim、evidence、artifact、source
/capability   统一外部能力调用入口
/provider     模型 provider 抽象
/providers    openai、anthropic、local、fake
/tool         tool driver、permission、execution
/mcp          MCP gateway / client
/storage      state store、session store、artifact store、event store
/observe      tracing、metrics、logs
/host         embeddable runtime
/admin        inspect / replay / pause / resume / abort API
/patterns     模板与样例，不再承载最终编排核心
```

## 执行策略

- 先修正 v0.2 的运行时语义
- 再落插件注册与中间件骨架
- 再把 Supervisor planner loop 拉进核心
- 最后逐步补齐 evidence、durability、observability、distributed worker

详细执行节奏见 [长期迭代 Active Plan](../plans/active-plan.md)。
