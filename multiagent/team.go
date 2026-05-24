package multiagent

// Team binds a roster of AgentClasses to a Scheduler. The Scheduler
// drives one Team across one Run; multiple Teams may coexist in a
// single Run when the application splits work across domains (e.g. a
// research team and a writing team coordinating via Blackboard).
type Team struct {
	Name      string       `json:"name"`
	Agents    []AgentClass `json:"agents,omitempty"`
	Scheduler Scheduler    `json:"-"`
}
