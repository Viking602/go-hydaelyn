# ADR-006 Blackboard / Evidence 数据模型

## 状态

已接受

## 背景

原先多 agent 协作只靠 `task.Result` 和最终 summary 拼接：

- research task 直接把 summary 暴露给最终聚合
- verify task 没有结构化输出
- synthesizer 无法区分已验证和未验证结论

这导致运行时无法表达 claim、evidence、verification 的真实关系。

## 决策

- 引入 `blackboard` 包，定义：
  - `Source`
  - `Artifact`
  - `Evidence`
  - `Claim`
  - `Finding`
  - `VerificationResult`
- runtime 在 research task 完成后通过 publish pipeline 发布到 blackboard
- publish pipeline 负责最小版：
  - `normalize`
  - `dedupe`
  - `redact`
  - `score`
- verify task 完成后产出结构化 `VerificationResult`
- synthesizer 只消费 `supported` claim 对应的 finding

## 当前语义

- research task 会发布 source、artifact、evidence、claim、finding
- verify task 会按依赖 research task 的 claim 写入 `VerificationResult`
- deepsearch 在 requireVerification 场景下只聚合 supported finding

## 影响

- 最终输出不再直接依赖原始 worker summary
- contradiction / insufficient 已进入可追踪状态模型
- 后续 verifier plugin、approval、event replay 都有了稳定数据落点
