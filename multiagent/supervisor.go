package multiagent

// SupervisorDecision is the typed output of a Scheduler implementation
// that reviews a finished AgentInstance Result before letting it commit
// to the Blackboard. v0.8.0 ships the contract; the reference
// SupervisorScheduler implementation lands in Phase 4.
type SupervisorDecision struct {
	Action    SupervisorAction `json:"action"`
	Reason    string           `json:"reason,omitempty"`
	HandoffTo string           `json:"handoffTo,omitempty"`
}

type SupervisorAction string

const (
	SupervisorActionAccept   SupervisorAction = "accept"
	SupervisorActionRetry    SupervisorAction = "retry"
	SupervisorActionHandoff  SupervisorAction = "handoff"
	SupervisorActionEscalate SupervisorAction = "escalate"
	SupervisorActionAbort    SupervisorAction = "abort"
)
