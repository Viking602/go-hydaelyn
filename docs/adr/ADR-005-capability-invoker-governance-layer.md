# ADR-005 `CapabilityInvoker` 统一治理层

## 状态

已接受

## 背景

在 v0.5 之前，LLM 和 Tool 虽然都能通过 runtime 执行，但它们走的是两条不同的调用路径：

- LLM 直接通过 `provider.Driver.Stream`
- Tool 直接通过 `tool.Driver.Execute`

这会让 timeout、retry、permission、approval、rate limit 这些治理能力无法在统一层收口。

## 决策

- 引入 `capability` 包
- 用 `CapabilityInvoker` 统一 capability 调用入口
- capability 当前覆盖：
  - `llm`
  - `tool`
- runtime 通过 adapter 把 provider/tool 调用接到 invoker：
  - `capabilityProviderDriver`
  - `capabilityToolDriver`
- capability 结果统一收敛为：
  - `Result`
  - `Usage`
  - `Error`

## 当前能力

- timeout
- retry
- permission
- approval
- rate limit

这些能力都通过 capability middleware 注入，而不是分散到 provider/tool 各自实现里。

## 影响

- runtime 已经可以在同一层观察和治理 llm/tool 调用
- 后续把 MCP、search、remote agent 接进来时，不需要再建第三套治理模型
- usage / timeout / error type 已经有统一结构，后续只需继续补 cost 和外部 capability 覆盖面
