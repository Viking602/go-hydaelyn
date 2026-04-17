package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Viking602/go-hydaelyn/hook"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/middleware/formatter"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
)

type Input struct {
	Model         string
	Messages      []message.Message
	Metadata      map[string]string
	ToolMode      tool.Mode
	MaxIterations int
	OnEvent       func(provider.Event) error

	// OutputSpec, when non-nil, enables automatic format validation of
	// each terminal assistant message (turn without tool calls). Failed
	// outputs trigger a retry with a formatter.BuildRetryMessage appended
	// to the conversation, up to MaxRetries times.
	OutputSpec *formatter.OutputSpec
	// MaxRetries caps how many extra turns may be spent on format fixes.
	// Retry turns still count against MaxIterations.
	MaxRetries int
	// OnRetry is invoked (if non-nil) with the violations that triggered
	// a retry, before the retry message is appended.
	OnRetry func([]formatter.Violation)

	// StopSequences and ThinkingBudget are forwarded to provider.Request
	// so guardrails can be set per-run without crafting the request by
	// hand. Empty/zero values leave the provider default.
	StopSequences  []string
	ThinkingBudget int

	// Observer, when non-nil, receives rumination/retry metrics at the end
	// of a run. MetricPrefix namespaces them (defaults to "agent" when
	// empty). Any observe.Observer satisfies MetricSink structurally.
	Observer     formatter.MetricSink
	MetricPrefix string
}

type Result struct {
	Messages   []message.Message
	Usage      provider.Usage
	StopReason provider.StopReason
	Iterations int
	Retries    int
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
	lastUsage := provider.Usage{}
	lastStopReason := provider.StopReasonUnknown
	retriesUsed := 0
	retriesLeft := 0
	if input.OutputSpec != nil {
		retriesLeft = input.MaxRetries
	}
	for iteration := 0; iteration < input.MaxIterations; iteration++ {
		assistant, usage, stopReason, err := e.runTurn(ctx, current, input)
		if err != nil {
			return Result{}, err
		}
		lastUsage = usage
		lastStopReason = stopReason
		if assistant.Text != "" || len(assistant.ToolCalls) > 0 {
			current = append(current, assistant)
		}
		if len(assistant.ToolCalls) == 0 || e.Tools == nil {
			if retriesLeft > 0 {
				if retry := formatRetryMessage(assistant, input); retry != nil {
					current = append(current, *retry)
					retriesLeft--
					retriesUsed++
					continue
				}
			}
			reportMetrics(input, assistant, retriesUsed)
			return Result{
				Messages:   current,
				Usage:      lastUsage,
				StopReason: lastStopReason,
				Iterations: iteration + 1,
				Retries:    retriesUsed,
				Thinking:   assistant.Thinking,
			}, nil
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
		Usage:      lastUsage,
		StopReason: provider.StopReasonMaxTurns,
		Iterations: input.MaxIterations,
		Retries:    retriesUsed,
	}, nil
}

// reportMetrics emits retry counters and rumination histograms for the
// terminal assistant turn. No-op when Observer is nil, so callers can
// leave it unset without cost.
func reportMetrics(input Input, assistant message.Message, retries int) {
	if input.Observer == nil {
		return
	}
	prefix := input.MetricPrefix
	if prefix == "" {
		prefix = "agent"
	}
	attrs := map[string]string{"model": input.Model}
	input.Observer.IncCounter(prefix+".retries", int64(retries), attrs)
	if assistant.Text != "" {
		formatter.RuminationScore(assistant.Text).Report(input.Observer, prefix+".text", attrs)
	}
	if assistant.Thinking != "" {
		formatter.RuminationScore(assistant.Thinking).Report(input.Observer, prefix+".thinking", attrs)
	}
}

// runTurn executes a single model turn: context transform, request assembly,
// provider stream and event collection. It encapsulates the per-turn wiring
// so the main loop only has to reason about iteration/retry bookkeeping.
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

// formatRetryMessage validates the assistant text against input.OutputSpec
// and, when it fails, returns the retry message to append to the
// conversation. A nil return means either the feature is disabled or the
// output already passes — the caller should finish the run in that case.
func formatRetryMessage(assistant message.Message, input Input) *message.Message {
	if input.OutputSpec == nil {
		return nil
	}
	violations := formatter.Validate(assistant.Text, *input.OutputSpec)
	if len(violations) == 0 {
		return nil
	}
	if input.OnRetry != nil {
		input.OnRetry(violations)
	}
	msg := formatter.BuildRetryMessage(violations)
	return &msg
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
	assistant := message.Message{
		Role: message.RoleAssistant,
		Kind: message.KindStandard,
	}
	var text strings.Builder
	var thinking strings.Builder
	callBuilders := map[string]*toolCallBuilder{}
	lastUsage := provider.Usage{}
	stopReason := provider.StopReasonUnknown
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
		switch event.Kind {
		case provider.EventTextDelta:
			text.WriteString(event.Text)
		case provider.EventThinkingDelta:
			thinking.WriteString(event.Thinking)
		case provider.EventToolCall:
			if event.ToolCall != nil {
				assistant.ToolCalls = append(assistant.ToolCalls, *event.ToolCall)
			}
		case provider.EventToolCallDelta:
			if event.ToolCallDelta != nil {
				builder := callBuilders[event.ToolCallDelta.ID]
				if builder == nil {
					builder = &toolCallBuilder{id: event.ToolCallDelta.ID}
					callBuilders[event.ToolCallDelta.ID] = builder
				}
				if event.ToolCallDelta.Name != "" {
					builder.name = event.ToolCallDelta.Name
				}
				builder.args.WriteString(event.ToolCallDelta.ArgumentsDelta)
			}
		case provider.EventDone:
			lastUsage = event.Usage
			stopReason = event.StopReason
		case provider.EventError:
			if event.Err != nil {
				return message.Message{}, provider.Usage{}, provider.StopReasonError, event.Err
			}
			return message.Message{}, provider.Usage{}, provider.StopReasonError, fmt.Errorf("provider emitted error event")
		}
	}
	assistant.Text = text.String()
	assistant.Thinking = thinking.String()
	if len(assistant.ToolCalls) == 0 && len(callBuilders) > 0 {
		for _, builder := range callBuilders {
			assistant.ToolCalls = append(assistant.ToolCalls, builder.build())
		}
	}
	return assistant, lastUsage, stopReason, nil
}

type toolCallBuilder struct {
	id   string
	name string
	args strings.Builder
}

func (b *toolCallBuilder) build() message.ToolCall {
	raw := json.RawMessage(b.args.String())
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	return message.ToolCall{
		ID:        b.id,
		Name:      b.name,
		Arguments: raw,
	}
}
