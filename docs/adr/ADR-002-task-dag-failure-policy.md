# ADR-002 Task DAG 与 `FailurePolicy`

## 状态

已接受

## 背景

仓库原先存在三个语义缺口：

- `executeTasks` 会执行全部 pending task，而不是只执行 runnable task
- task 图没有系统化校验，循环依赖、缺失依赖、重复 ID 都可能悄悄进入运行态
- task 失败后 pattern 仍可能继续 aggregate，形成静默降级

## 决策

- `RunState.Validate()` 负责校验 task graph 的重复 ID、缺失依赖、循环依赖与 assignee 合法性
- `Runtime.executeTasks()` 只调度 `RunnableTasks()`
- `Task` 引入 `FailurePolicy`
- 当前支持四类策略：`fail_fast`、`retry`、`degrade`、`skip_optional`
- 对于阻塞依赖失败的 pending task，运行时会先解析成 `failed` 或 `skipped`，避免团队卡死
- 一旦出现 blocking failure，team 立即进入 failed，禁止继续 aggregate

## 影响

- 线性、并行、diamond DAG 的执行语义可预测
- 失败依赖不会被提前执行
- 失败从 pattern 拼接层回收到 runtime 语义层

## 后续

- `retry` 目前是基础能力，后续 v0.7 durable runtime 里要和 lease、idempotency、checkpoint 联动
- `degrade` 与 `skip_optional` 后续还需要配合 verifier/synthesizer 做更细粒度输出控制
