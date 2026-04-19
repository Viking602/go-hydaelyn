# Migration Notes

## 从 v0.1 风格迁移到当前 runtime

### 1. Task assignee

旧模型：

- 直接用 profile 名承载 assignee

新模型：

- 运行时 identity 由 `AgentInstance` 承载
- task 绑定 `RequiredRole + AssigneeAgentID`

### 2. Pattern 语义

旧模型：

- pattern 直接承担拆分和调度

新模型：

- pattern 可以提供 template
- planner 可以生成 typed plan
- runtime 按 planner review/replan 驱动 team

### 3. Verification

旧模型：

- 直接拼 summary

新模型：

- research task 先发布到 `blackboard`
- verify task 产生结构化 `VerificationResult`
- synthesizer 只吃 supported claims

### 4. Capability 治理

旧模型：

- provider/tool 走各自调用路径

新模型：

- provider/tool 已接入统一 `CapabilityInvoker`
- timeout / retry / permission / approval / rate limit 已进入 capability policy

### 5. Durable runtime

当前版本已经支持：

- EventStore
- replay
- pause / resume / abort
- admin inspect/replay/events

仍待后续补齐：

- 分布式 worker 调度
- 更广义 capability 接入
- 外部 observability backend
