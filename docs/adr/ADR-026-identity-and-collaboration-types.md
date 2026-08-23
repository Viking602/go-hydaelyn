# ADR-026 Agent Identity and Collaboration Type Unification

## Status

Accepted — 2026-08-15. Effective from v0.15.0.

## Context

Agent identity is split across `api.AgentProfile` (durable routing
identity), `api.AgentDefinition` (versioned deployment contract), and
`agent.AgentProfile` (loop-layer description with model, instructions,
and tool names). Multi-agent collaboration uses in-memory
`multiagent.BlackboardEntry` and `multiagent.Handoff` that are not
automatically persisted, while `api.BlackboardItem` and
`api.HandoffRecord` are the store rows.

Hosts re-implemented the mapping, so scheduler values and durable rows
drifted.

## Decision

1. **Durable identity is `api.AgentProfile` and `api.AgentDefinition`.**
   `agent.AgentProfile` remains the loop-layer descriptor and exposes
   `Identity()` / `ProfileFromIdentity` to project onto the durable
   profile (ID, Role, Metadata). It is not an alias: the extra loop
   fields do not belong on the store row.
2. **`multiagent.Handoff` persists as `api.HandoffRecord`.** Conversion
   helpers `Record()` and `HandoffFromRecord` are the only supported
   mapping. Schedulers that persist handoffs write through
   `HandoffStore`.
3. **`multiagent.BlackboardEntry` persists as `api.BlackboardItem`.**
   `Item(runID)` / `EntryFromItem` are the only supported mapping.
   WrittenBy maps to `Source` (`SourceAgent`); EvidenceID maps onto
   `EvidenceRefs`.
4. **Do not add a second store** for multi-agent-only rows.

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| Treating `agent.AgentProfile` as the store identity | Drops Groups and invents a second catalog |
| JSON-copying Handoff into Blackboard payload as the persistence path | Skips `HandoffStore` invariants |

## Impact

Schedulers and hosts share one mapping. Follow-up work can emit
handoffs automatically (historically reserved for v0.9) without new
types.

## References

- ADR-001, ADR-014, ADR-016
- `api/types.go`, `api/agent_definition.go`
- `agent/profile.go`
- `multiagent/handoff.go`, `multiagent/blackboard.go`
