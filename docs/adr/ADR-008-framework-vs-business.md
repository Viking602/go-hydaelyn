# ADR-008 框架与业务的职责边界（P-1）

## 状态

已接受 — 自 v2.0 路线图（计划文件：`/Users/viking/.claude/plans/sunny-hugging-goose.md`）开始强制。

**Revised 2026-05-24:** v0.8.0 reconstruction (master spec `docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md`, ADR-016) introduces a first-class `multiagent/` kernel package whose vocabulary includes terms like `Scheduler`, `Supervisor`, `Voting`, `Debate`. These are *framework primitives* for multi-agent coordination, not business vocabulary, and are explicitly exempted from the ban below. See *Revised — multi-agent primitive exception list* at the bottom of this ADR.

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
| ---- | ---- |
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
| -------- | ------------------------ |
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

## Revised 2026-05-24 — multi-agent primitive exception list

The v0.8.0 reconstruction introduces a `multiagent/` top-level kernel package (see ADR-016 and `docs/product-spec/v0.8.0/05-multi-agent-layer.md`). Its public surface uses the following nouns and verbs. These are framework primitives for multi-agent coordination — they are **not** business vocabulary and are explicitly exempted from the §3 hard constraint:

```
Scheduler        (multiagent.Scheduler interface, multiagent.SequentialScheduler, ...)
Supervisor       (multiagent.SupervisorScheduler, multiagent/supervisor.go)
Voting           (multiagent/voting.go)
Debate           (reserved for v0.9.0 schedulers)
Handoff          (multiagent.Handoff, api.HandoffStore — already in use)
Dispatch         (multiagent.Dispatch)
Team             (multiagent.Team)
AgentClass       (multiagent.AgentClass)
AgentInstance    (multiagent.AgentInstance — ADR-014 revised)
TypedReport      (the Blackboard write of agent.Result.Structured)
TeamState        (multiagent.TeamState)
```

Permitted locations for these primitives: `api/`, `multiagent/**`, `agent/**` (when referencing the Scheduler boundary), `worker/**` (when adapting to Scheduler dispatches), `internal/**` mirrors of the above, `_examples/`, `examples/`, `packs/`, `docs/`.

What is NOT exempted (the §3 ban still applies in full to):

```
incident, change, ticket, customer, sales, deploy, repository, document,
synthesis, hazard, lead, agent_review,
review (as a TaskType), action (as a TaskType)
```

The exception list is *closed* — adding a new framework primitive requires an ADR amendment. Adding a business word still requires removal during code review.

### Baseline update

`.sentrux/business-words.baseline` and `scripts/check-business-words.sh` are updated alongside the v0.8.0 reconstruction to:

1. Remove `Scheduler`, `Supervisor`, `Voting`, `Debate`, `Handoff`, `Dispatch`, `Team`, `AgentClass`, `AgentInstance`, `TypedReport`, `TeamState` from the banned-word list if they were ever included (most never were; this is defensive).
2. Keep the existing business-word baseline = 45 ceiling. CI continues to enforce "≤ baseline; only allows decrease."
3. Add a comment header to `business-words.baseline` referencing this ADR revision so future contributors can trace why the exception list exists.

### Compatibility with the original decision

The original §1/§2/§3 split holds:

- §1 (framework responsibilities) — extended to include multi-agent coordination primitives. The ADR-016 surface lands here.
- §2 (business responsibilities) — unchanged. Domain concepts (incident response, code review workflows, customer support routing) still live in `packs/` and recipes.
- §3 (immediately-effective hard constraints) — narrowed only by the closed exception list above. All other bans hold.

The intent of ADR-008 — "framework primitives are mechanism, business concepts are policy" — is unchanged. The revision recognizes that multi-agent coordination *is* a framework mechanism, and gives it vocabulary accordingly.

## 引用

- 计划文件：`/Users/viking/.claude/plans/sunny-hugging-goose.md`
- 强制配置：`.sentrux/rules.toml`、`.sentrux/business-words.baseline`、`.github/workflows/ci.yml`（`architecture-gate` job）
- Master spec: `docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md`
- Multi-agent layer design: `docs/product-spec/v0.8.0/05-multi-agent-layer.md`
- Related ADRs: ADR-015 (Strong Bounded Agent Loop), ADR-016 (Explicit Multi-Agent Scheduler), ADR-017 (Durable Runner Boundary), ADR-014 (Agent Ontology Stance — revised)
