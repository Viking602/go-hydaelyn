package model

type TypedReport struct {
	Status        ReportStatus    `json:"status"`
	Summary       string          `json:"summary,omitempty"`
	Structured    map[string]any  `json:"structured,omitempty"`
	ActionOutcome *ActionOutcome  `json:"actionOutcome,omitempty"`
	Handoff       *HandoffRequest `json:"handoff,omitempty"`
}
