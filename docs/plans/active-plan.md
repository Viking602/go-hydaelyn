# Hydaelyn 长期迭代 Active Plan

## 状态说明

- `active`: 正在执行
- `next`: 下一批
- `queued`: 已确认方向，等待前置里程碑完成
- `done`: 已完成

## 当前结论

- 当前首要目标不是继续扩展 pattern，而是修正 runtime 语义
- v0.2 是后续所有里程碑的依赖地基
- 当前第一批执行范围限定在 P0: DAG runnable、身份拆分、失败语义、工具默认权限、测试补强

## 当前步骤

当前步骤：`complete`

### v0.2 运行时语义校正 `done`

- [x] 仅执行 `RunnableTasks()` 返回的 task
- [x] DAG 校验覆盖循环依赖、缺失依赖、重复 task ID
- [x] 引入 `AgentInstance`
- [x] `Task.Assignee` 迁移为 `RequiredRole + AssigneeAgentID`
- [x] 定型 `FailurePolicy`
- [x] task 失败后禁止静默 aggregate
- [x] Tool 默认权限改为 deny-by-default
- [x] 增加 task/session/profile 身份一致性测试
- [x] 跑通 `go test ./...`
- [x] 跑通 `go test ./... -race`

### v0.3 插件体系与中间件链 `done`

- [x] 引入 `plugin.Registry`
- [x] 统一插件类型与两级注册
- [x] 引入 middleware / interceptor 链
- [x] 定型配置优先级
- [x] 支持 runtime dump 生效配置

### v0.4 Supervisor Planning Loop `done`

- [x] 定义 `Planner` 接口：`Plan / Review / Replan`
- [x] 引入 typed plan schema
- [x] Scheduler 支持按 role/capability/budget/concurrency 选 agent
- [x] 支持 review based replan
- [x] 支持 `abort / ask-human / escalate`
- [x] Pattern 降级为 template，`deepsearch` 可走 planner-generated DAG 样例路径

### v0.5 Blackboard 与结构化验证 `done`

- [x] 引入 `Finding / Claim / Evidence / Artifact / Source`
- [x] 引入 publish pipeline
- [x] `Verifier` 输出结构化 `VerificationResult`
- [x] `Synthesizer` 只消费 verified claims

### v0.6 Capability 治理层 `done`

- [x] OpenAI streaming provider
- [x] Anthropic streaming provider
- [x] `CapabilityInvoker`
- [x] budget、timeout、retry、rate limit、approval 中间件

### v0.7 Durable Runtime `done`

- [x] EventStore
- [x] event replay 重建 RunState
- [x] pause / resume / replay / abort
- [x] human-in-the-loop approval

### v0.8 可观测性与治理 `done`

- [x] 全链路 trace
- [x] metrics、logs、trace correlation
- [x] Admin API inspect / pause / resume / abort
- [x] secret redaction、PII masking、deny audit

### v0.9 分布式执行与协议网关 `done`

- [x] `TaskQueue`
- [x] worker lease / heartbeat / recovery
- [x] distributed scheduler 本地/共享 queue 执行骨架
- [x] MCP gateway 扩展

### v1.0 稳定 API 与生态拆分 `done`

- [x] public API freeze
- [x] SemVer 兼容策略
- [x] 核心仓库与生态仓库拆分边界
- [x] Quickstart、architecture、plugin dev、migration guide
- [x] CLI 与 benchmark suite 基础版

## 当前已识别差距

- 当前长期路线图中的里程碑项已全部落地到仓库
- 2026-04-16 补齐轮修复项：
  - [x] capability `RateLimit` 修复为线程安全 + 滑动时间窗口 (`RateLimitPerWindow`)
  - [x] capability `Retry` 增加指数退避 + 非临时错误短路
  - [x] 新增 `BudgetEnforcer` 中间件（token/cost/tool-call 三维度拦截）
  - [x] scheduler 新增 backpressure（`MaxPending` / `MaxInflight` 限制 + `ErrQueueFull`）
  - [x] MCP client 扩展 `ListResources` / `ReadResource` / `ListPrompts` / `GetPrompt`
  - [x] PII masking 增强（email / phone / SSN / credit card 模式）
  - [x] OTel observer 适配器（回调注入模式，零外部依赖）
  - [x] CLI `run` 增加 `--provider` 参数支持 openai / anthropic 真实 provider
- 仍可继续优化的事项：
  - distributed scheduler 跨节点真实演示
  - 外置生态仓库（go-hydaelyn-observe-otel、go-hydaelyn-storage-postgres 等）实际拆分
  - 复杂函数数相对基线增加，需要作为后续结构收敛项跟踪

## 里程碑验收基线

v0.2 完成后必须满足：

- 线性、并行、diamond DAG 单测通过
- 失败依赖不再提前执行
- 同 profile 多 worker 不再共享 agent 身份
- race 检查通过

## 运行说明

当前 MCP 可用的 sentrux surface 以 `scan / session_start / rescan / health / dsm / test_gaps / check_rules / session_end` 为主。后续迭代按这些可用能力持续校验结构质量。
