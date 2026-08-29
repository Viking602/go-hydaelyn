package agent

// LoopPolicy carries Engine defaults for one execution. A Request with a
// non-nil Budget replaces Budget as a whole.
type LoopPolicy struct {
	MaxIterations       int     `json:"maxIterations,omitempty"`
	UnlimitedIterations bool    `json:"unlimitedIterations,omitempty"`
	Budget              *Budget `json:"budget,omitempty"`
	ContextTokenTarget  int     `json:"contextTokenTarget,omitempty"`
}
