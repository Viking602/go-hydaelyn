# ADR-008 框架与业务的职责边界（P-1）

## 状态

已接受 — 自 v2.0 路线图（计划文件：`/Users/viking/.claude/plans/sunny-hugging-goose.md`）开始强制。

## 背景

Hydaelyn 的目标是做"Go 原生多智能体运行时"框架，把"如何并发调度任务、协调多 Agent、传递证据、做审批与处置"这套**能力**做厚做正确，让开发者在其上自由定义自己的业务架构（用户给出的事故响应参考架构只是一种可能性）。

但当前 `internal/core/types.go` 直接定义了：

- `TaskTypeSynthesis` / `TaskTypeReview` / `TaskTypeAction`
- `BlackboardItemSynthesis` / `BlackboardItemReviewResult` / `BlackboardItemActionResult`

并且 `internal/core/report.go` 用 `TaskTypeAction` 来分支判定行为、用 `BlackboardItemActionResult` 写黑板。这把"归因/评审/处置"这一套**业务语义**焊在了框架里。后果：

- 任何不做事故响应的领域被迫要么忽略这些常量、要么被它们语义干扰；
- 框架想增删一种业务流程都得改核心；
- 用户写新场景时不知道哪些 API 是"框架"哪些是"业务示例"。

## 决策

划分两条不可越界的红线：

### 1. 框架职责（保留 / 补完）

| 能力 | 形式 |
|------|------|
| Run / Task 状态机 | `Run`, `Task`, `RunStatus`, `TaskStatus`, `TaskType` 仅作为字符串别名（不绑定语义） |
| Blackboard | 读写 + 过滤 + Subscribe（M2.2）；item kind 由调用方任意命名 |
| Mailbox | 路由 + fan-out（M2.1）+ Lease + Ack/DeadLetter |
| Handoff 协议 | owner 历史 + 深度限制 + 环检测 |
| Approval 协议 | 请求 / 决策 / ResumeToken |
| Trace | Span 生命周期 |
| Tool 调用合约 | `Tool.RequiresActionTask`、参数/输出 schema、PolicyEngine 钩子 |
| 聚合屏障 | `AwaitMode{All,Any,Quorum}` + `OnDependencyFailed{Skip,Fail,Continue}`（M2.3） |

### 2. 业务职责（开发者侧 — 框架不可预设）

| 业务概念 | 实现方式（框架提供原料） |
|----------|--------------------------|
| 角色（Monitor / Reviewer / Hazard…） | `AgentProfile.Role` + `AgentProfile.Metadata` |
| 任务种类语义 | `Task.Tags []string` + `Task.Type`（开发者自定字符串） |
| 黑板条目种类 | 调用方传入 `BlackboardItemKind`（任意字符串） |
| 归因 / 评审 / 处置流程 | 由开发者用 Tool + Handoff + Approval 组合编排 |
| 是否触发审批 | `Tool.RequiresActionTask` 工具元数据驱动，框架不识别 "Action" 字面 |

### 3. 立即生效的硬约束

- 框架代码（`internal/core/**`、`orchestrator/**`、`agent/**`、`blackboard/**`、`mailbox/**`、`tool/**`、`flow/**`、`hook/**`、`message/**`、`policy/**`、`provider/**`）**不得**新增以下字面量：
  `Synthesis` / `Review` / `ReviewResult` / `Action`（作类型词时）/ `ActionResult` / `Hazard` / `Incident`
- 已有出现位置（M3 清理目标）由 `.sentrux/business-words.baseline` 锁定基线 = 45。CI 校验"实际计数 ≤ 基线"，仅允许下降。
- 框架代码**不得** import `legacy/**`。`.sentrux/rules.toml` 用 `[[boundaries]]` 对每个干净模块逐条锁定（`internal/core`、`orchestrator`、`agent`、`blackboard`、`mailbox`、`tool`、`flow`、`hook`、`message`、`policy`、`provider`），任何 PR 引入新依赖即 CI 失败。
- 不使用 sentrux 的 `[[layers]]` + `layer_direction`：在 0.5.7 中该规则太粗，会在过渡期内卡住合理的 façade→runtime 内部调用；用显式 `[[boundaries]]` 可以渐进收紧。
- `no_god_files` 暂时关闭：`archive/legacy-v1 host runtime`（fan-out=17）是合法残留，等 M6 删除 `legacy/` 后重启该规则。

### 4. 范围内的合理例外

- `_examples/`、`docs/`、`legacy/`、`pattern/`（M4.5 迁出前）**允许**自由使用业务词与领域类型。
- `internal/cli/`、`internal/slow/` 当前仍 import legacy，由 M4.2 / M4.3 切除；规则范围在 M4 结束后才扩展到全部 `internal/**`。

## 影响

- 任何 PR 在评审时引用本 ADR 即可作为拒绝业务词进入框架的依据。
- M3（业务词剥离）成为 v2.0 的硬性 break change：剥离 `TaskTypeSynthesis/Review/Action` 与 `BlackboardItem*Result`，由开发者自定。
- 评分回归：`sentrux session_end` 与 M0 baseline（quality_signal=6166, modularity=3233）对比；M5 拆分后 modularity 应回升。

## 引用

- 计划文件：`/Users/viking/.claude/plans/sunny-hugging-goose.md`
- 强制配置：`.sentrux/rules.toml`、`.sentrux/business-words.baseline`、`.github/workflows/ci.yml`（`architecture-gate` job）
