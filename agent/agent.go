package agent

import (
	"context"
	"errors"
	"io"
	"maps"

	"github.com/Viking602/go-hydaelyn/hook"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
)

var ErrToolBusMissing = errors.New("tool bus missing")

// LoopInput is the message-level input to Engine.RunMessages, the
// low-level loop entry preserved from v0.7. Most callers should use
// Engine.Run(ctx, api.Task, OutputPolicy) Result instead — that is the
// task-level entry the v0.8.0 multi-agent layer schedules against.
type LoopInput struct {
	Model         string
	Messages      []message.Message
	Metadata      map[string]string
	ToolMode      tool.Mode
	MaxIterations int
	OnEvent       func(provider.Event) error

	StopSequences  []string
	ThinkingBudget int
	ResponseFormat *provider.ResponseFormat
	ExtraBody      map[string]any

	OutputGuardrails []OutputGuardrail
	OutputRecorder   OutputGuardrailRecorder
}

// LoopOutput is the message-level result from Engine.RunMessages. The
// task-level Result type lives in result.go.
type LoopOutput struct {
	Messages   []message.Message
	Usage      provider.Usage
	StopReason provider.StopReason
	Iterations int
	Thinking   string
}

// Engine drives the bounded agent loop. Configure Provider/Tools/Hooks
// at construction; the v0.8.0 fields (Model, ToolMode, LoopPolicy,
// ContextBuilder) bind the defaults Engine.Run (the task-level entry)
// needs. Engine.RunMessages remains available as the low-level
// message-driven entry; it ignores the v0.8.0 fields and reads
// everything it needs from LoopInput.
type Engine struct {
	Provider provider.Driver
	Tools    *tool.Bus
	Hooks    hook.Chain

	Model          string
	ToolMode       tool.Mode
	LoopPolicy     LoopPolicy
	ContextBuilder ContextManager
}

// RunMessages is the low-level loop that drives one LoopInput to
// completion. Engine.Run is the task-level wrapper most callers want.
func (e Engine) RunMessages(ctx context.Context, input LoopInput) (LoopOutput, error) {
	if input.MaxIterations <= 0 {
		input.MaxIterations = 4
	}
	if input.ToolMode == "" {
		input.ToolMode = tool.ModeSequential
	}
	current := append([]message.Message{}, input.Messages...)
	totalUsage := provider.Usage{}
	for iteration := 0; iteration < input.MaxIterations; iteration++ {
		assistant, usage, stopReason, err := e.runTurn(ctx, current, input)
		if err != nil {
			return LoopOutput{}, err
		}
		totalUsage = totalUsage.Add(usage)
		if len(assistant.ToolCalls) == 0 {
			finalOutput, retryMessages, retryPolicy, err := e.applyOutputGuardrails(ctx, input, current, assistant, iteration+1, totalUsage, stopReason)
			if err != nil {
				return LoopOutput{}, err
			}
			if len(retryMessages) > 0 {
				if retryPolicy.IncludeRejectedOutput && (assistant.Text != "" || assistant.Thinking != "") {
					current = append(current, assistant)
				}
				if len(retryPolicy.ReplacementContext) > 0 {
					current = append(current, cloneMessages(retryPolicy.ReplacementContext)...)
				}
				current = append(current, retryMessages...)
				continue
			}
			if finalOutput.Text != "" || finalOutput.Thinking != "" {
				current = append(current, finalOutput)
			}
			return LoopOutput{
				Messages:   current,
				Usage:      totalUsage,
				StopReason: stopReason,
				Iterations: iteration + 1,
				Thinking:   finalOutput.Thinking,
			}, nil
		}
		if assistant.Text != "" || len(assistant.ToolCalls) > 0 {
			current = append(current, assistant)
		}
		if e.Tools == nil {
			return LoopOutput{}, ErrToolBusMissing
		}
		results, terminal, err := e.executeTools(ctx, assistant.ToolCalls, input.ToolMode)
		if err != nil {
			return LoopOutput{}, err
		}
		for _, result := range results {
			current = append(current, message.NewToolResult(result))
		}
		if terminal {
			return LoopOutput{
				Messages:   current,
				Usage:      totalUsage,
				StopReason: provider.StopReasonComplete,
				Iterations: iteration + 1,
				Thinking:   assistant.Thinking,
			}, nil
		}
	}
	return LoopOutput{
		Messages:   current,
		Usage:      totalUsage,
		StopReason: provider.StopReasonMaxTurns,
		Iterations: input.MaxIterations,
	}, nil
}

func (e Engine) applyOutputGuardrails(ctx context.Context, input LoopInput, current []message.Message, assistant message.Message, iteration int, usage provider.Usage, stopReason provider.StopReason) (message.Message, []message.Message, RetryPolicy, error) {
	if len(input.OutputGuardrails) == 0 {
		return assistant, nil, RetryPolicy{}, nil
	}
	candidate := assistant
	for _, guardrail := range input.OutputGuardrails {
		if guardrail == nil {
			continue
		}
		result, err := guardrail.Check(ctx, OutputGuardrailInput{
			Model:         input.Model,
			Messages:      cloneMessages(current),
			Output:        candidate,
			Iteration:     iteration,
			MaxIterations: input.MaxIterations,
			Usage:         usage,
			StopReason:    stopReason,
			Metadata:      cloneStringMap(input.Metadata),
		})
		if err != nil {
			return message.Message{}, nil, RetryPolicy{}, err
		}
		normalized, err := normalizeOutputGuardrailResult(result)
		if err != nil {
			return message.Message{}, nil, RetryPolicy{}, err
		}
		switch normalized.Action {
		case OutputGuardrailActionAllow:
			continue
		case OutputGuardrailActionReplace:
			e.recordOutputGuardrailDecision(ctx, input, guardrail.Name(), normalized.Action, normalized.Reason, iteration, normalized.Metadata)
			candidate = *normalized.Replacement
		case OutputGuardrailActionRetry:
			e.recordOutputGuardrailDecision(ctx, input, guardrail.Name(), normalized.Action, normalized.Reason, iteration, normalized.Metadata)
			if iteration >= input.MaxIterations {
				return message.Message{}, nil, RetryPolicy{}, &OutputGuardrailRetryLimitExceededError{
					Guardrail: guardrail.Name(),
					Output:    candidate,
				}
			}
			return candidate, normalized.RetryMessages, normalized.RetryPolicy, nil
		case OutputGuardrailActionBlock:
			e.recordOutputGuardrailDecision(ctx, input, guardrail.Name(), normalized.Action, normalized.Reason, iteration, normalized.Metadata)
			return message.Message{}, nil, RetryPolicy{}, &OutputGuardrailTripwireTriggeredError{
				Guardrail: guardrail.Name(),
				Reason:    normalized.Reason,
				Output:    candidate,
			}
		}
	}
	return candidate, nil, RetryPolicy{}, nil
}

func (e Engine) recordOutputGuardrailDecision(ctx context.Context, input LoopInput, name string, action OutputGuardrailAction, reason string, iteration int, metadata map[string]string) {
	if input.OutputRecorder == nil {
		return
	}
	merged := cloneStringMap(input.Metadata)
	if merged == nil {
		merged = map[string]string{}
	}
	for key, value := range metadata {
		merged[key] = value
	}
	input.OutputRecorder.RecordOutputGuardrailDecision(ctx, OutputGuardrailDecision{
		GuardrailName: name,
		Action:        action,
		Reason:        reason,
		Iteration:     iteration,
		Metadata:      merged,
	})
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneAnyMap(values map[string]any) map[string]any {
	return maps.Clone(values)
}

// runTurn executes a single model turn: context transform, request assembly,
// provider stream and event collection.
func (e Engine) runTurn(ctx context.Context, current []message.Message, input LoopInput) (message.Message, provider.Usage, provider.StopReason, error) {
	transformed, err := e.Hooks.TransformContext(ctx, current)
	if err != nil {
		return message.Message{}, provider.Usage{}, provider.StopReasonError, err
	}
	request := provider.Request{
		Model:          input.Model,
		Messages:       transformed,
		Metadata:       input.Metadata,
		StopSequences:  input.StopSequences,
		ThinkingBudget: input.ThinkingBudget,
		ResponseFormat: input.ResponseFormat,
		ExtraBody:      cloneAnyMap(input.ExtraBody),
	}
	if e.Tools != nil {
		request.Tools = e.Tools.Definitions()
	}
	if err := e.Hooks.BeforeModelCall(ctx, &request); err != nil {
		return message.Message{}, provider.Usage{}, provider.StopReasonError, err
	}
	stream, err := e.Provider.Stream(ctx, request)
	if err != nil {
		return message.Message{}, provider.Usage{}, provider.StopReasonError, err
	}
	return e.collect(ctx, stream, input.OnEvent)
}

func (e Engine) executeTools(ctx context.Context, calls []message.ToolCall, mode tool.Mode) ([]message.ToolResult, bool, error) {
	prepared := make([]tool.Call, 0, len(calls))
	terminal := false
	for _, call := range calls {
		item := call
		if err := e.Hooks.BeforeToolCall(ctx, &item); err != nil {
			return nil, false, err
		}
		if e.Tools != nil && e.Tools.IsTerminal(item.Name) {
			terminal = true
		}
		prepared = append(prepared, item)
	}
	results, err := e.Tools.ExecuteBatch(ctx, prepared, mode, nil)
	if err != nil {
		return nil, false, err
	}
	items := make([]message.ToolResult, 0, len(results))
	for _, current := range results {
		item := current
		if err := e.Hooks.AfterToolCall(ctx, &item); err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	return items, terminal, nil
}

func (e Engine) collect(ctx context.Context, stream provider.Stream, onEvent func(provider.Event) error) (message.Message, provider.Usage, provider.StopReason, error) {
	defer func() { _ = stream.Close() }()
	assistant := message.Message{Role: message.RoleAssistant, Kind: message.KindStandard}
	events := make([]provider.Event, 0, 8)
	for {
		event, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return message.Message{}, provider.Usage{}, provider.StopReasonError, err
		}
		if onEvent != nil {
			if err := onEvent(event); err != nil {
				return message.Message{}, provider.Usage{}, provider.StopReasonError, err
			}
		}
		if err := e.Hooks.OnEvent(ctx, event); err != nil {
			return message.Message{}, provider.Usage{}, provider.StopReasonError, err
		}
		events = append(events, event)
	}
	normalized, err := provider.NormalizeEvents(events)
	if err != nil {
		return message.Message{}, provider.Usage{}, provider.StopReasonError, err
	}
	assistant.Text = normalized.Text
	assistant.Thinking = normalized.Thinking
	assistant.ToolCalls = normalized.ToolCalls
	return assistant, normalized.Usage, normalized.StopReason, nil
}
