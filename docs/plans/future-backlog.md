# Future backlog

This backlog is non-normative. Items require demonstrated consumers and a decision-complete design before implementation.

## Provider and tool quality

- Expand adapter conformance fixtures for provider-specific streaming edge cases.
- Add measured performance baselines for long streams and large tool schemas.
- Publish separate ecosystem adapters only when ownership and maintenance are clear.

## Agent execution

- Improve context preparation benchmarks without adding hidden model calls.
- Expand continuation compatibility fixtures for future schema evolution.
- Add more output-policy schema coverage when real consumers need additional keywords.

## Orchestration

- Document application patterns for persisting `orchestration.State`.
- Add executor examples that combine stable dispatch IDs with `durable.ExecutionID`.
- Evaluate additional mechanical fold helpers only after two applications repeat the same implementation.

## Durability

- Support external backend repositories with shared conformance CI examples.
- Add fault-injection guidance for ambiguous database commit responses and lease expiry.
- Measure attempt payload size and checkpoint write amplification under realistic traces.
- Define versioning rules for attempt envelopes before a payload format change is needed.

## Explicit non-goals

The backlog does not reserve core types for application identity, routing strategy, approvals, quotas, deployment, domain records, or backend schema. Those remain application or ecosystem responsibilities.
