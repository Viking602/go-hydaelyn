package multiagent

import "github.com/Viking602/venat/api"

// Multi-agent EventType extensions added in v0.8.0. These are valid
// api.EventType values for transport over the event store; the runner
// treats them opaquely.
//
// Spec anchor: docs/product-spec/v0.8.0/05-multi-agent-layer.md
// §"Multi-agent events".
const (
	EventAgentInstanceCreated   api.EventType = "AgentInstanceCreated"
	EventAgentInstanceFinished  api.EventType = "AgentInstanceFinished"
	EventAgentInstanceSuspended api.EventType = "AgentInstanceSuspended"
	EventSchedulerTick          api.EventType = "SchedulerTick"
	EventDispatchEmitted        api.EventType = "DispatchEmitted"
	EventTypedHandoff           api.EventType = "TypedHandoff"
	EventVotingResolved         api.EventType = "VotingResolved"
	EventSupervisorDecided      api.EventType = "SupervisorDecided"

	// EventSchedulerFailure records a Scheduler.Next error surfaced by
	// Drive as a SchedulerFailureError. Integrations map it to a typed
	// terminal Run status per ADR-016 §6.
	EventSchedulerFailure api.EventType = "SchedulerFailure"
)
