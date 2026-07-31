package model

import (
	"time"

	"github.com/Viking602/venat/message"
)

type ToolEffectType = message.ToolEffectType

const (
	ToolEffectReadOnly           = message.ToolEffectReadOnly
	ToolEffectWrite              = message.ToolEffectWrite
	ToolEffectExternalSideEffect = message.ToolEffectExternalSideEffect
)

// ActionOutcome is the structured outcome of an ActionAttempt. The framework
// owns the action-attempt protocol; the contents (Output, ArtifactRefs, etc.)
// are opaque domain payloads supplied by the caller.
type ActionOutcome struct {
	AttemptID         string              `json:"attemptId"`
	ResultID          string              `json:"resultId,omitempty"`
	ActionID          string              `json:"actionId,omitempty"`
	RunID             string              `json:"runId,omitempty"`
	TaskID            string              `json:"taskId,omitempty"`
	Status            ActionAttemptStatus `json:"status"`
	Summary           string              `json:"summary,omitempty"`
	Output            string              `json:"output,omitempty"`
	ArtifactRefs      []string            `json:"artifactRefs,omitempty"`
	RollbackAvailable bool                `json:"rollbackAvailable,omitempty"`
	ExternalResultRef string              `json:"externalResultRef,omitempty"`
	CreatedAt         time.Time           `json:"createdAt,omitempty"`
	Error             string              `json:"error,omitempty"`
}

type Tool struct {
	Name               string            `json:"name"`
	EffectType         ToolEffectType    `json:"effectType"`
	RequiresActionTask bool              `json:"requiresActionTask,omitempty"`
	RiskLevel          string            `json:"riskLevel,omitempty"`
	Idempotent         bool              `json:"idempotent,omitempty"`
	Timeout            time.Duration     `json:"timeout,omitempty"`
	RetryPolicy        RetryPolicy       `json:"retryPolicy,omitempty"`
	PolicyTags         []string          `json:"policyTags,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

const MaxRetryAttempts = 10

type RetryPolicy struct {
	MaxAttempts int           `json:"maxAttempts,omitempty"`
	Backoff     time.Duration `json:"backoff,omitempty"`
	MaxBackoff  time.Duration `json:"maxBackoff,omitempty"`
}
