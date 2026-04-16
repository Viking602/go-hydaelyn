# ADR-001 `Profile` 与 `AgentInstance` 分离

## 状态

已接受

## 背景

仓库原先把 task assignee 直接写成 profile 名。这会导致两个问题：

- 运行中身份和能力模板混在一起，无法表达同 profile 多 worker 并发
- session、task、共享消息都只能挂在 profile 上，无法保证身份一致性

## 决策

- 保留 `Profile` 作为能力模板
- 引入 `AgentInstance` 作为运行时实体
- `RunState.Supervisor` 与 `RunState.Workers` 改为保存 `AgentInstance`
- `Task` 不再以 profile 名直接承载 assignee，而是绑定 `AssigneeAgentID`
- session 与 shared message 统一写入真实 `AgentInstance.ID`

## 影响

- 同 profile 多 worker 现在可以拥有不同的 agent identity 与独立私有 session
- 调度逻辑可以先按 agent 选执行体，再解析 profile 能力模板
- 后续按 role、capability、budget 做 scheduler/router 扩展时，不需要再拆第二次模型

## 代价

- 运行时与 pattern 的装配代码变复杂了一层
- 现有数据结构保留了兼容字段，后续还需要继续清理旧字段
