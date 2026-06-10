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

// NewTeam creates an empty Team with the given name. Bind roles with
// AddRole and the coordination strategy with WithScheduler.
func NewTeam(name string) *Team {
	return &Team{Name: name}
}

// AddRole appends class to the Team roster and returns the Team for
// chaining.
func (t *Team) AddRole(class AgentClass) *Team {
	t.Agents = append(t.Agents, class)
	return t
}

// WithScheduler binds the Scheduler that drives this Team and returns
// the Team for chaining.
func (t *Team) WithScheduler(scheduler Scheduler) *Team {
	t.Scheduler = scheduler
	return t
}
