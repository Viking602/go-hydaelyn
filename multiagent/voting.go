package multiagent

// VotingResult aggregates votes cast by AgentInstances on a candidate
// Dispatch or candidate agent.Result. v0.8.0 ships the type; the
// vote-collection scheduler (DebateScheduler) lands in v0.9.0.
//
// Tally is the count per candidate; Quorum is the minimum vote count
// for the result to count as resolved.
type VotingResult struct {
	Candidate string            `json:"candidate"`
	Votes     map[string]string `json:"votes,omitempty"`
	Tally     map[string]int    `json:"tally,omitempty"`
	Winner    string            `json:"winner,omitempty"`
	Quorum    int               `json:"quorum,omitempty"`
}
