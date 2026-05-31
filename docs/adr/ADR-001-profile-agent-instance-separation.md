# ADR-001 Separating `Profile` and `AgentInstance`

## Status

Accepted

## Context

The repository originally wrote the task assignee directly as the profile name. This caused two problems:

- Runtime identity and the capability template were mixed together, making it impossible to express multiple concurrent workers under the same profile
- session, task, and shared messages could only be attached to a profile, so identity consistency could not be guaranteed

## Decision

- Keep `Profile` as the capability template
- Introduce `AgentInstance` as the runtime entity
- Change `RunState.Supervisor` and `RunState.Workers` to hold `AgentInstance`
- `Task` no longer carries the assignee directly via the profile name; instead it binds an `AssigneeAgentID`
- session and shared message are uniformly written with the real `AgentInstance.ID`

## Impact

- Multiple workers under the same profile can now have distinct agent identities and independent private sessions
- The scheduling logic can first pick an executor by agent, then resolve the profile capability template
- When later extending the scheduler/router by role, capability, or budget, the model does not need to be split a second time

## Costs

- The assembly code for the runtime and patterns becomes one layer more complex
- Existing data structures retain compatibility fields, so the old fields still need to be cleaned up later
