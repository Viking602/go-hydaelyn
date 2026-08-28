package agent

import (
	"time"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/session"
)

const (
	harnessKindRun          = "run"
	harnessPhaseCheckpoint  = "checkpoint"
	harnessPhaseAssistant   = "assistant"
	harnessNeedAssistant    = "need_assistant"
	harnessMayFinish        = "may_finish"
	harnessGenReady         = "ready"
	harnessGenEffectPending = "effect_pending"
	harnessGenRetryWait     = "retry_wait"
	harnessOutcomeCompleted = "completed"
	harnessOutcomeFailed    = "failed"
	harnessLaneMain         = "main"
	harnessCodeTools        = "tools_unsupported"
	harnessCodeInterrupted  = "interrupted"
	harnessCodeProvider     = "provider_error"
)

type OperationIntent struct {
	Kind           string   `json:"kind"`
	PromptEntryIDs []string `json:"promptEntryIds"`
}

type Operation struct {
	OperationID  string          `json:"operationId"`
	Lane         string          `json:"lane"`
	SourceLeafID string          `json:"sourceLeafId"`
	Intent       OperationIntent `json:"intent"`
}

type GenerationContext struct {
	Model       string `json:"model"`
	MaxAttempts int    `json:"maxAttempts"`
	BaseDelayMs int    `json:"baseDelayMs"`
}

type Generation struct {
	Status          string            `json:"status"`
	Context         GenerationContext `json:"context"`
	Attempt         int               `json:"attempt,omitempty"`
	NextAttempt     int               `json:"nextAttempt,omitempty"`
	ResponseEntryID string            `json:"responseEntryId,omitempty"`
	UsageID         string            `json:"usageId,omitempty"`
	NotBefore       int64             `json:"notBefore,omitempty"`
}

type RunPhase struct {
	Kind           string      `json:"kind"`
	Continuation   string      `json:"continuation,omitempty"`
	TriggerEntryID string      `json:"triggerEntryId,omitempty"`
	Generation     *Generation `json:"generation,omitempty"`
}

type RunState struct {
	Kind                   string   `json:"kind"`
	Phase                  RunPhase `json:"phase"`
	LatestAssistantEntryID string   `json:"latestAssistantEntryId,omitempty"`
}

// HarnessRetry bounds provider retries within one run. BaseDelayMs is the wait
// before the first retry; it doubles per attempt and is jittered. Zero disables
// waiting entirely.
type HarnessRetry struct {
	MaxAttempts int
	BaseDelayMs int
}

type RunOutcome struct {
	OperationID  string
	Kind         string
	LeafID       string
	FinalMessage *message.Message
	Error        *session.OperationError
}

type HarnessOptions struct {
	Provider provider.Driver
	Model    string
	Retry    HarnessRetry
	// LeaseTTL bounds how long another Harness must wait before recovering an
	// operation whose owner stopped renewing its durable lane lease.
	LeaseTTL time.Duration
}
