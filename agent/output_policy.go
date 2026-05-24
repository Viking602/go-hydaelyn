package agent

import "encoding/json"

// OutputPolicy controls structured-output validation and schema repair
// after each agent loop completion. Engine.Run honors Validate; the
// Repair loop with MaxRepairAttempts is wired in Phase 2 (v0.8.0
// scaffold surfaces FailureKindSchemaInvalid when Validate fails).
type OutputPolicy struct {
	Schema            json.RawMessage `json:"schema,omitempty"`
	Validate          bool            `json:"validate,omitempty"`
	Repair            bool            `json:"repair,omitempty"`
	MaxRepairAttempts int             `json:"maxRepairAttempts,omitempty"`
}
