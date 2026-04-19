package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
)

type OutputGuardrailAction string

const (
	OutputGuardrailActionAllow   OutputGuardrailAction = "allow"
	OutputGuardrailActionReplace OutputGuardrailAction = "replace"
	OutputGuardrailActionRetry   OutputGuardrailAction = "retry"
	OutputGuardrailActionBlock   OutputGuardrailAction = "block"
)

var ErrOutputGuardrailInvalidResult = errors.New("invalid output guardrail result")

type OutputGuardrail interface {
	Name() string
	Check(ctx context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error)
}

type OutputGuardrailFunc func(ctx context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error)

type outputGuardrail struct {
	name string
	fn   OutputGuardrailFunc
}

func NewOutputGuardrail(name string, fn OutputGuardrailFunc) OutputGuardrail {
	if fn == nil {
		return nil
	}
	return outputGuardrail{name: name, fn: fn}
}

func (g outputGuardrail) Name() string {
	return g.name
}

func (g outputGuardrail) Check(ctx context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error) {
	return g.fn(ctx, input)
}

type OutputGuardrailInput struct {
	Model         string
	Messages      []message.Message
	Output        message.Message
	Iteration     int
	MaxIterations int
	Usage         provider.Usage
	StopReason    provider.StopReason
	Metadata      map[string]string
}

type OutputGuardrailResult struct {
	Action        OutputGuardrailAction
	Replacement   *message.Message
	RetryMessages []message.Message
	Reason        string
	Metadata      map[string]string
}

func AllowOutput() OutputGuardrailResult {
	return OutputGuardrailResult{Action: OutputGuardrailActionAllow}
}

func ReplaceOutput(output message.Message) OutputGuardrailResult {
	return OutputGuardrailResult{
		Action:      OutputGuardrailActionReplace,
		Replacement: &output,
	}
}

func RetryOutput(messages ...message.Message) OutputGuardrailResult {
	return OutputGuardrailResult{
		Action:        OutputGuardrailActionRetry,
		RetryMessages: cloneMessages(messages),
	}
}

func BlockOutput(reason string) OutputGuardrailResult {
	return OutputGuardrailResult{
		Action: OutputGuardrailActionBlock,
		Reason: reason,
	}
}

type OutputGuardrailTripwireTriggeredError struct {
	Guardrail string
	Reason    string
	Output    message.Message
}

func (e *OutputGuardrailTripwireTriggeredError) Error() string {
	if e == nil {
		return "output guardrail triggered"
	}
	if e.Guardrail != "" && e.Reason != "" {
		return fmt.Sprintf("output guardrail %q triggered: %s", e.Guardrail, e.Reason)
	}
	if e.Guardrail != "" {
		return fmt.Sprintf("output guardrail %q triggered", e.Guardrail)
	}
	if e.Reason != "" {
		return fmt.Sprintf("output guardrail triggered: %s", e.Reason)
	}
	return "output guardrail triggered"
}

type OutputGuardrailRetryLimitExceededError struct {
	Guardrail string
	Output    message.Message
}

func (e *OutputGuardrailRetryLimitExceededError) Error() string {
	if e == nil || e.Guardrail == "" {
		return "output guardrail requested retry after max iterations reached"
	}
	return fmt.Sprintf("output guardrail %q requested retry after max iterations reached", e.Guardrail)
}

func normalizeOutputGuardrailResult(result OutputGuardrailResult) (OutputGuardrailResult, error) {
	if result.Action == "" {
		result.Action = OutputGuardrailActionAllow
	}
	switch result.Action {
	case OutputGuardrailActionAllow:
		return result, nil
	case OutputGuardrailActionReplace:
		if result.Replacement == nil {
			return OutputGuardrailResult{}, fmt.Errorf("%w: replace requires replacement output", ErrOutputGuardrailInvalidResult)
		}
		replacement := *result.Replacement
		if replacement.Role == "" {
			replacement.Role = message.RoleAssistant
		}
		if replacement.Kind == "" {
			replacement.Kind = message.KindStandard
		}
		if len(replacement.ToolCalls) > 0 || replacement.ToolResult != nil {
			return OutputGuardrailResult{}, fmt.Errorf("%w: replace output must remain terminal assistant output", ErrOutputGuardrailInvalidResult)
		}
		result.Replacement = &replacement
		return result, nil
	case OutputGuardrailActionRetry:
		if len(result.RetryMessages) == 0 {
			return OutputGuardrailResult{}, fmt.Errorf("%w: retry requires at least one retry message", ErrOutputGuardrailInvalidResult)
		}
		result.RetryMessages = cloneMessages(result.RetryMessages)
		return result, nil
	case OutputGuardrailActionBlock:
		return result, nil
	default:
		return OutputGuardrailResult{}, fmt.Errorf("%w: unknown action %q", ErrOutputGuardrailInvalidResult, result.Action)
	}
}

func cloneMessages(messages []message.Message) []message.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]message.Message, len(messages))
	copy(cloned, messages)
	return cloned
}
