package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"hydaelyn/hook"
	"hydaelyn/message"
	"hydaelyn/provider"
	"hydaelyn/tool"
)

type Input struct {
	Model         string
	Messages      []message.Message
	Metadata      map[string]string
	ToolMode      tool.Mode
	MaxIterations int
	OnEvent       func(provider.Event) error
}

type Result struct {
	Messages   []message.Message
	Usage      provider.Usage
	StopReason provider.StopReason
	Iterations int
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
	for iteration := 0; iteration < input.MaxIterations; iteration++ {
		transformed, err := e.Hooks.TransformContext(ctx, current)
		if err != nil {
			return Result{}, err
		}
		request := provider.Request{
			Model:    input.Model,
			Messages: transformed,
			Metadata: input.Metadata,
		}
		if e.Tools != nil {
			request.Tools = e.Tools.Definitions()
		}
		if err := e.Hooks.BeforeModelCall(ctx, &request); err != nil {
			return Result{}, err
		}
		stream, err := e.Provider.Stream(ctx, request)
		if err != nil {
			return Result{}, err
		}
		assistant, usage, stopReason, err := e.collect(ctx, stream, input.OnEvent)
		if err != nil {
			return Result{}, err
		}
		lastUsage = usage
		lastStopReason = stopReason
		if assistant.Text != "" || len(assistant.ToolCalls) > 0 {
			current = append(current, assistant)
		}
		if len(assistant.ToolCalls) == 0 || e.Tools == nil {
			return Result{
				Messages:   current,
				Usage:      lastUsage,
				StopReason: lastStopReason,
				Iterations: iteration + 1,
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
	}, nil
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
