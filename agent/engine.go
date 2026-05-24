package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
)

// Run drives the agent loop against one api.Task under the supplied
// OutputPolicy. The typed Failure on the returned Result is the only
// surface a failure crosses to the multi-agent layer (boundaries
// Principle 6); Run intentionally does not return a bare error.
//
// v0.8.0 ships the contract and a working scaffold. Schema repair (the
// full OutputPolicy.Repair loop with MaxRepairAttempts) lands in
// Phase 2; this scaffold validates that the model's terminal text is
// valid JSON when OutputPolicy.Schema is supplied, and surfaces
// FailureKindSchemaInvalid otherwise.
func (e Engine) Run(ctx context.Context, task api.Task, policy OutputPolicy) Result {
	messages, err := e.buildContext(ctx, task)
	if err != nil {
		return Result{
			Failure: (&AgentFailure{
				Kind:   FailureKindContextBuildFailed,
				Reason: err.Error(),
			}).WithCause(err),
		}
	}

	input := LoopInput{
		Model:         e.Model,
		Messages:      messages,
		ToolMode:      e.ToolMode,
		MaxIterations: e.LoopPolicy.MaxIterations,
	}

	output, runErr := e.RunMessages(ctx, input)
	if runErr != nil {
		return Result{
			Messages:   output.Messages,
			Usage:      output.Usage,
			StopReason: output.StopReason,
			Thinking:   output.Thinking,
			Failure: (&AgentFailure{
				Kind:   FailureKindEngineError,
				Reason: runErr.Error(),
			}).WithCause(runErr),
		}
	}

	text := finalAssistantTextFromMessages(output.Messages)
	result := Result{
		Text:       text,
		Thinking:   output.Thinking,
		Usage:      output.Usage,
		StopReason: output.StopReason,
		Messages:   output.Messages,
		Valid:      true,
	}

	if len(policy.Schema) > 0 && policy.Validate {
		if json.Valid([]byte(text)) {
			result.Structured = json.RawMessage(text)
			result.Valid = true
		} else {
			result.Valid = false
			result.Failure = &AgentFailure{
				Kind:      FailureKindSchemaInvalid,
				Reason:    "agent terminal output is not valid JSON",
				Retryable: policy.Repair,
			}
		}
	}

	return result
}

func (e Engine) buildContext(ctx context.Context, task api.Task) ([]message.Message, error) {
	if e.ContextBuilder != nil {
		return e.ContextBuilder.Build(ctx, task)
	}
	return defaultContextBuilder{}.Build(ctx, task)
}

type defaultContextBuilder struct{}

func (defaultContextBuilder) Build(_ context.Context, task api.Task) ([]message.Message, error) {
	goal := strings.TrimSpace(task.Goal)
	if goal == "" {
		goal = "Complete the assigned task and return a concise result."
	}
	return []message.Message{
		message.NewText(message.RoleSystem, "You are a Hydaelyn agent."),
		message.NewText(message.RoleUser, goal),
	}, nil
}

func (defaultContextBuilder) Compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	return history, nil
}

func finalAssistantTextFromMessages(messages []message.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role == message.RoleAssistant && strings.TrimSpace(m.Text) != "" {
			return m.Text
		}
	}
	return ""
}
