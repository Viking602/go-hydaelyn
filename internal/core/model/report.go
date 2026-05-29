package model

type TypedReport struct {
	Status        ReportStatus    `json:"status"`
	Summary       string          `json:"summary,omitempty"`
	Kind          string          `json:"kind,omitempty"`
	Retryable     bool            `json:"retryable,omitempty"`
	Escalatable   bool            `json:"escalatable,omitempty"`
	Structured    map[string]any  `json:"structured,omitempty"`
	ActionOutcome *ActionOutcome  `json:"actionOutcome,omitempty"`
	Handoff       *HandoffRequest `json:"handoff,omitempty"`
}
