package agent

import (
	"context"
	"errors"
	"io"

	"github.com/Viking602/go-hydaelyn/hook"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
)

var ErrToolBusMissing = errors.New("tool bus missing")

type Input struct {
	Model         string
	Messages      []message.Message
	Metadata      map[string]string
	ToolMode      tool.Mode
	MaxIterations int
	OnEvent       func(provider.Event) error

	// StopSequences and ThinkingBudget are forwarded to provider.Request
	// so guardrails can be set per-run without crafting the request by
	// hand. Empty/zero values leave the provider default.
	StopSequences  []string
	ThinkingBudget int

	// OutputGuardrails run only after a terminal assistant output is
	// collected. They are distinct from hooks/middleware/capability
	// policies because they operate on the final assistant answer rather
	// than prompt/tool/runtime stages.
	OutputGuardrails []OutputGuardrail
	OutputRecorder   OutputGuardrailRecorder
}

type Result struct {
	Messages   []message.Message
	Usage      provider.Usage
	StopReason provider.StopReason
	Iterations int
	// Thinking is the concatenated reasoning stream from the final turn,
	// when the provider emits EventThinkingDelta. Empty when the model
	// didn't surface any reasoning or the driver discards it.
	Thinking string
}

type Engine struct {
	Provider provider.Driver
	Tools    *tool.Bus
	Hooks    hook.Chain
}

func (e Engine) Run(ctx context.Context, input Input) (Result, error) {
	if input.MaxIterations <= 0 {
		input.MaxIterations = 4
	}
	if input.ToolMode == "" {
		input.ToolMode = tool.ModeSequential
	}
	current := append([]message.Message{}, input.Messages...)
	totalUsage := provider.Usage{}
	lastStopReason := provider.StopReasonUnknown
	for iteration := 0; iteration < input.MaxIterations; iteration++ {
		assistant, usage, stopReason, err := e.runTurn(ctx, current, input)
		if err != nil {
			return Result{}, err
		}
		totalUsage = totalUsage.Add(usage)
		lastStopReason = stopReason
		if len(assistant.ToolCalls) == 0 {
			finalOutput, retryMessages, retryPolicy, err := e.applyOutputGuardrails(ctx, input, current, assistant, iteration+1, totalUsage, lastStopReason)
			if err != nil {
				return Result{}, err
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
			return Result{
				Messages:   current,
				Usage:      totalUsage,
				StopReason: lastStopReason,
				Iterations: iteration + 1,
				Thinking:   finalOutput.Thinking,
			}, nil
		}
		if assistant.Text != "" || len(assistant.ToolCalls) > 0 {
			current = append(current, assistant)
		}
		if e.Tools == nil {
			return Result{}, ErrToolBusMissing
		}
		results, err := e.executeTools(ctx, assistant.ToolCalls, input.ToolMode)
		if err != nil {
			return Result{}, err
		}
		for _, result := range results {
			current = append(current, message.NewToolResult(result))
		}
	}
	return Result{
		Messages:   current,
		Usage:      totalUsage,
		StopReason: provider.StopReasonMaxTurns,
		Iterations: input.MaxIterations,
	}, nil
}

func (e Engine) applyOutputGuardrails(ctx context.Context, input Input, current []message.Message, assistant message.Message, iteration int, usage provider.Usage, stopReason provider.StopReason) (message.Message, []message.Message, RetryPolicy, error) {
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

func (e Engine) recordOutputGuardrailDecision(ctx context.Context, input Input, name string, action OutputGuardrailAction, reason string, iteration int, metadata map[string]string) {
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

// runTurn executes a single model turn: context transform, request assembly,
// provider stream and event collection.
func (e Engine) runTurn(ctx context.Context, current []message.Message, input Input) (message.Message, provider.Usage, provider.StopReason, error) {
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

func (e Engine) executeTools(ctx context.Context, calls []message.ToolCall, mode tool.Mode) ([]message.ToolResult, error) {
	prepared := make([]tool.Call, 0, len(calls))
	for _, call := range calls {
		item := tool.Call(call)
		if err := e.Hooks.BeforeToolCall(ctx, &item); err != nil {
			return nil, err
		}
		prepared = append(prepared, item)
	}
	results, err := e.Tools.ExecuteBatch(ctx, prepared, mode, nil)
	if err != nil {
		return nil, err
	}
	items := make([]message.ToolResult, 0, len(results))
	for _, current := range results {
		item := tool.Result(current)
		if err := e.Hooks.AfterToolCall(ctx, &item); err != nil {
			return nil, err
		}
		items = append(items, message.ToolResult(item))
	}
	return items, nil
}

func (e Engine) collect(ctx context.Context, stream provider.Stream, onEvent func(provider.Event) error) (message.Message, provider.Usage, provider.StopReason, error) {
	defer stream.Close()
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
