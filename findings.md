# go-hydaelyn 架构分析发现

**采集时间：** 2026-05-04  
**工具：** sentrux scan + health + dsm + git_stats + test_gaps + check_rules

---

## sentrux 基线数据

### 扫描概览
```
files:         317
import_edges:  406
lines:         32,323
quality_signal: 6,179
```

### 五维健康评分
```
Quality Signal: 6,179 / 10,000（约 C+）
瓶颈维度: modularity

acyclicity:  raw=0 cycles    score=10,000  ✅ 完美
redundancy:  raw=0.08        score=9,198   ✅ 优秀
equality:    raw=0.453       score=5,471   ⚠️ 一般
depth:       raw=7 levels    score=5,333   ⚠️ 偏深
modularity:  raw=0.003       score=3,355   🔴 瓶颈
```

### DSM（设计结构矩阵）
```
size:             209 节点
edge_count:       406
above_diagonal:   0    ← 零逆向依赖（完美下三角）
below_diagonal:   406  ← 全部向下流动
same_level:       0
level_breaks:     7
propagation_cost: 191
density:          93（稀疏，良好）
clusters:         0 个检测到的集群  ← Modularity 低的根因
```

**解读：** 所有 406 条 import 边向下流动，结构诚实；但 209 个节点未形成任何内聚集群，整体是一张稀薄的网。

### Git 统计（90 天）
```
commits_analyzed:       135
files_with_churn:       210
hotspot_count:          30
coupling_pairs_found:   50
single_author_ratio:    0.857（85.7%）
bus_factor_solo_files:  180
```

### 测试覆盖
```
source_files:    258
test_files:      59
tested:          25
untested:        233
coverage_ratio:  9.7%
```

### 架构规则检查
```
rules_checked:   2
violation_count: 0
pass:            true
```
规则：max_cycles=0，max_coupling=B，no_god_files=true

---

## 项目基本信息

```
语言：Go 1.25
外部依赖：零（纯 stdlib）
模块：github.com/Viking602/go-hydaelyn
架构模式：六边形架构（Ports & Adapters）+ Command Bus + Event Sourcing/Projection
```

---

## 层次结构

```
Public Facade:   hydaelyn.go · runner.go · api/ · errors.go
Agent/Worker:    agent/ · worker/ · flow/ · hook/ · message/ · policy/ · tool/
Transport:       transport/mcp/ · provider/(anthropic, openai, scripted)
Internal Core:   internal/core/ + ports/ + adapter/ + model/
Domain Services: internal/{action,approval,blackboard,execution,mailbox,
                           run,task,trace,response,report,toolgate,
                           userinput,handoff,lifecycle,...}（27 个子包）
Infrastructure:  internal/memory · internal/store · internal/errs
```

---

## 关键发现

### F1: runner.go 是 God Interface（P0 根因）
- 79 个公共方法，603 行
- 触达所有领域：run/task/mailbox/blackboard/approval/trace/action/response/admin
- 这是 Modularity=3,355 的直接原因（单个汇聚点让图无法形成集群）

### F2: model_aliases.go 是无效抽象
- 242 行，把 internal/core/model 的所有内容重导出到 core 命名空间
- 项目内 55 个文件直接 import internal/core/model（别名未能隐藏任何东西）
- 仅 3 个外部文件使用了别名：runner.go、hydaelyn.go、internal/cli/inspect.go
- **结论：完全删除，3 个文件加 import model 即可**

### F3: 双 blackboard 层（P4 根因）
- `internal/blackboard/service.go`：WriteItem 函数（写入+事件+Trace）
- `internal/core/blackboard/service.go`：Service struct（Subscribe+WaitForBlackboard）
- 职责不同但同包名，是 Depth=7 层的来源之一
- **结论：将 core/blackboard/Service 迁移到 internal/blackboard/subscribe.go**

### F4: 测试覆盖率严重不足（9.7%）
- 258 个源文件，仅 25 个有对应测试
- 高风险未测试路径：command_bus、state_transitions、mailbox/dispatch、execution/lease、approval
- 对框架型项目而言，这是最高风险点

### F5: Bus Factor 极高
- 85.7% 文件由单一作者维护
- 135 次提交中单人比例相同
- 知识孤岛风险大

### F6: 50 个隐式耦合对
- co-change pairs：在 import 图里不连接，但总是一起修改
- 表明存在未被接口捕获的隐式依赖
- 需要 git log 分析确认高频对后逐个处理

### F7: runner.go 使用 core.XXX 的两类来源（已确认）
- **命令类型**（来自 internal/core 本身）：AdvanceRunCommand、DispatchTaskCommand、StartRunResult 等 → 删别名后不用动
- **model 类型别名**：Run、Task、TaskEnvelope、ResumeToken、HolderType 等 → 删别名后改为 model.XXX
