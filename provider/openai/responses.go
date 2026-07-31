package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/shared"
)

type responsesRequest struct {
	Model     string              `json:"model"`
	Input     []json.RawMessage   `json:"input"`
	Include   []string            `json:"include,omitempty"`
	Tools     []responsesTool     `json:"tools,omitempty"`
	Stream    bool                `json:"stream"`
	Reasoning *responsesReasoning `json:"reasoning,omitempty"`
	Text      *responsesText      `json:"text,omitempty"`
}

type responsesTool struct {
	Type        string             `json:"type"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Parameters  message.JSONSchema `json:"parameters"`
}

type responsesReasoning struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary"`
}

type responsesText struct {
	Format map[string]any `json:"format"`
}

type responsesStreamEvent struct {
	Type        string              `json:"type"`
	OutputIndex int                 `json:"output_index"`
	Delta       string              `json:"delta"`
	Item        responsesOutputItem `json:"item"`
	Response    responsesResponse   `json:"response"`
	Error       *responsesAPIError  `json:"error"`
	Code        string              `json:"code"`
	Message     string              `json:"message"`
}

type responsesOutputItem struct {
	ID        string             `json:"id"`
	Type      string             `json:"type"`
	CallID    string             `json:"call_id"`
	Name      string             `json:"name"`
	Arguments string             `json:"arguments"`
	Phase     provider.TextPhase `json:"phase"`
}

type responsesResponse struct {
	Output            json.RawMessage             `json:"output"`
	Usage             responsesUsage              `json:"usage"`
	IncompleteDetails *responsesIncompleteDetails `json:"incomplete_details"`
	Error             *responsesAPIError          `json:"error"`
}

type responsesUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	InputTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

type responsesIncompleteDetails struct {
	Reason string `json:"reason"`
}

type responsesAPIError struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type responsesOutputState struct {
	phase             provider.TextPhase
	callID            string
	name              string
	hadArgumentDeltas bool
}

type responsesStream struct {
	body     io.ReadCloser
	reader   *shared.Reader
	items    map[int]*responsesOutputState
	finished bool
}

func (d Driver) streamResponses(ctx context.Context, request provider.Request) (provider.Stream, error) {
	if len(request.StopSequences) > 0 {
		return nil, fmt.Errorf("openai responses API does not support stop sequences")
	}
	apiKey, err := d.apiKey()
	if err != nil {
		return nil, err
	}
	input, err := toResponsesInput(request.Messages)
	if err != nil {
		return nil, err
	}
	body, err := marshalResponsesRequest(responsesRequest{
		Model:     request.Model,
		Input:     input,
		Tools:     toResponsesTools(request.Tools),
		Stream:    true,
		Reasoning: responsesReasoningFromBudget(request.ThinkingBudget),
		Text:      responsesTextFromRequest(request.ResponseFormat),
	}, request.ExtraBody)
	if err != nil {
		return nil, err
	}
	bodyStream, err := d.postEventStream(ctx, "/responses", body, apiKey)
	if err != nil {
		return nil, err
	}
	return &responsesStream{
		body:   bodyStream,
		reader: shared.NewReader(bodyStream),
		items:  make(map[int]*responsesOutputState),
	}, nil
}

const responsesEncryptedReasoningInclude = "reasoning.encrypted_content"

func marshalResponsesRequest(payload responsesRequest, extraBody map[string]any) ([]byte, error) {
	include, err := responsesIncludes(extraBody)
	if err != nil {
		return nil, err
	}
	payload.Include = include
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	merged := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &merged); err != nil {
		return nil, err
	}
	for key, value := range extraResponsesBodyFields(extraBody) {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("marshal openai responses extra body field %q: %w", key, err)
		}
		merged[key] = encoded
	}
	return json.Marshal(merged)
}

func responsesIncludes(extraBody map[string]any) ([]string, error) {
	include := []string{responsesEncryptedReasoningInclude}
	requested, ok := extraBody["include"]
	if !ok || requested == nil {
		return include, nil
	}
	encoded, err := json.Marshal(requested)
	if err != nil {
		return nil, fmt.Errorf("marshal openai responses include: %w", err)
	}
	var extra []string
	if err := json.Unmarshal(encoded, &extra); err != nil {
		return nil, fmt.Errorf("openai responses ExtraBody include must be an array of strings: %w", err)
	}
	seen := map[string]struct{}{responsesEncryptedReasoningInclude: {}}
	for _, item := range extra {
		if _, duplicate := seen[item]; duplicate {
			continue
		}
		seen[item] = struct{}{}
		include = append(include, item)
	}
	return include, nil
}

func extraResponsesBodyFields(extraBody map[string]any) map[string]any {
	fields := make(map[string]any, len(extraBody))
	for key, value := range extraBody {
		if _, managed := managedResponsesBodyFields[key]; !managed {
			fields[key] = value
		}
	}
	return fields
}

var managedResponsesBodyFields = map[string]struct{}{
	"model":     {},
	"include":   {},
	"input":     {},
	"tools":     {},
	"stream":    {},
	"reasoning": {},
	"text":      {},
}

func toResponsesInput(messages []message.Message) ([]json.RawMessage, error) {
	items := make([]json.RawMessage, 0, len(messages))
	for _, msg := range messages {
		var err error
		switch msg.Role {
		case message.RoleAssistant:
			items, err = appendResponsesAssistantInput(items, msg)
		case message.RoleTool:
			items, err = appendResponsesToolOutput(items, msg.ToolResult)
		default:
			items, err = appendResponsesInputItem(items, map[string]any{
				"role":    msg.Role,
				"content": msg.Text,
			})
		}
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func appendResponsesAssistantInput(items []json.RawMessage, msg message.Message) ([]json.RawMessage, error) {
	if len(msg.ProviderState) > 0 {
		stateItems, err := decodeResponsesProviderState(msg.ProviderState)
		if err != nil {
			return nil, err
		}
		return append(items, stateItems...), nil
	}
	var err error
	if msg.Text != "" {
		items, err = appendResponsesInputItem(items, map[string]any{
			"role":    message.RoleAssistant,
			"content": msg.Text,
		})
		if err != nil {
			return nil, err
		}
	}
	for _, call := range msg.ToolCalls {
		items, err = appendResponsesInputItem(items, map[string]any{
			"type":      "function_call",
			"call_id":   call.ID,
			"name":      call.Name,
			"arguments": string(call.Arguments),
		})
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func appendResponsesToolOutput(items []json.RawMessage, result *message.ToolResult) ([]json.RawMessage, error) {
	if result == nil {
		return items, nil
	}
	return appendResponsesInputItem(items, map[string]any{
		"type":    "function_call_output",
		"call_id": result.ToolCallID,
		"output":  result.Content,
	})
}

func appendResponsesInputItem(items []json.RawMessage, item map[string]any) ([]json.RawMessage, error) {
	encoded, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	return append(items, encoded), nil
}

func decodeResponsesProviderState(state json.RawMessage) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(state)
	if len(trimmed) < 2 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
		return nil, fmt.Errorf("openai responses provider state must be a JSON array")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, fmt.Errorf("decode openai responses provider state: %w", err)
	}
	return items, nil
}

func toResponsesTools(defs []message.ToolDefinition) []responsesTool {
	items := make([]responsesTool, 0, len(defs))
	for _, def := range defs {
		items = append(items, responsesTool{
			Type:        "function",
			Name:        def.Name,
			Description: def.Description,
			Parameters:  def.InputSchema,
		})
	}
	return items
}

func responsesReasoningFromBudget(budget int) *responsesReasoning {
	reasoning := reasoningFromBudget(budget)
	if reasoning == nil {
		return nil
	}
	return &responsesReasoning{Effort: reasoning.Effort, Summary: "auto"}
}

func responsesTextFromRequest(format *provider.ResponseFormat) *responsesText {
	if format == nil || format.Type == "" {
		return nil
	}
	payload := map[string]any{"type": format.Type}
	if format.Type == "json_schema" {
		payload["name"] = format.Name
		payload["strict"] = format.Strict
		if format.Schema != nil {
			payload["schema"] = format.Schema
		}
	}
	return &responsesText{Format: payload}
}

func (s *responsesStream) Recv() (provider.Event, error) {
	if s.finished {
		return provider.Event{}, io.EOF
	}
	for {
		frame, err := s.reader.Next()
		if err != nil {
			if err == io.EOF {
				return provider.Event{}, io.ErrUnexpectedEOF
			}
			return provider.Event{}, err
		}
		var event responsesStreamEvent
		if err := json.Unmarshal([]byte(frame.Data), &event); err != nil {
			return provider.Event{}, fmt.Errorf("decode openai responses stream event: %w", err)
		}
		if event.Type == "" {
			event.Type = frame.Name
		}
		result, emit, err := s.consume(event)
		if err != nil {
			return provider.Event{}, err
		}
		if emit {
			return result, nil
		}
	}
}

func (s *responsesStream) consume(event responsesStreamEvent) (provider.Event, bool, error) {
	switch event.Type {
	case "response.output_item.added":
		s.recordOutputItem(event.OutputIndex, event.Item)
	case "response.output_text.delta", "response.refusal.delta":
		return provider.Event{
			Kind:      provider.EventTextDelta,
			Text:      event.Delta,
			TextPhase: s.textPhase(event.OutputIndex),
		}, true, nil
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		return provider.Event{Kind: provider.EventThinkingDelta, Thinking: event.Delta}, true, nil
	case "response.function_call_arguments.delta":
		state := s.outputState(event.OutputIndex)
		state.hadArgumentDeltas = true
		index := event.OutputIndex
		return provider.Event{
			Kind: provider.EventToolCallDelta,
			ToolCallDelta: &provider.ToolCallDelta{
				Index:          &index,
				ID:             state.callID,
				Name:           state.name,
				ArgumentsDelta: event.Delta,
			},
		}, true, nil
	case "response.output_item.done":
		return s.outputItemDone(event.OutputIndex, event.Item), event.Item.Type == "function_call" && !s.outputState(event.OutputIndex).hadArgumentDeltas, nil
	case "response.completed":
		return s.completed(event.Response)
	case "response.incomplete":
		return s.incomplete(event.Response)
	case "response.failed":
		s.finished = true
		return provider.Event{Kind: provider.EventError, Err: responsesError(event.Response.Error)}, true, nil
	case "error":
		s.finished = true
		apiError := event.Error
		if apiError == nil {
			apiError = &responsesAPIError{Code: event.Code, Message: event.Message}
		}
		return provider.Event{Kind: provider.EventError, Err: responsesError(apiError)}, true, nil
	}
	return provider.Event{}, false, nil
}

func (s *responsesStream) outputItemDone(index int, item responsesOutputItem) provider.Event {
	state := s.outputState(index)
	hadArgumentDeltas := state.hadArgumentDeltas
	s.recordOutputItem(index, item)
	if item.Type != "function_call" || hadArgumentDeltas {
		return provider.Event{}
	}
	indexCopy := index
	return provider.Event{
		Kind: provider.EventToolCallDelta,
		ToolCallDelta: &provider.ToolCallDelta{
			Index:          &indexCopy,
			ID:             state.callID,
			Name:           state.name,
			ArgumentsDelta: item.Arguments,
		},
	}
}

func (s *responsesStream) completed(response responsesResponse) (provider.Event, bool, error) {
	providerState, output, err := responsesOutput(response.Output)
	if err != nil {
		return provider.Event{}, false, err
	}
	stopReason := provider.StopReasonComplete
	for _, item := range output {
		if item.Type == "function_call" {
			stopReason = provider.StopReasonToolUse
			break
		}
	}
	s.finished = true
	return responsesDoneEvent(response.Usage, stopReason, providerState), true, nil
}

func (s *responsesStream) incomplete(response responsesResponse) (provider.Event, bool, error) {
	providerState, _, err := responsesOutput(response.Output)
	if err != nil {
		return provider.Event{}, false, err
	}
	stopReason := provider.StopReasonUnknown
	if response.IncompleteDetails != nil && response.IncompleteDetails.Reason == "max_output_tokens" {
		stopReason = provider.StopReasonMaxTurns
	}
	s.finished = true
	return responsesDoneEvent(response.Usage, stopReason, providerState), true, nil
}

func responsesOutput(raw json.RawMessage) (json.RawMessage, []responsesOutputItem, error) {
	if len(raw) == 0 {
		return nil, nil, fmt.Errorf("openai responses terminal event omitted output")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
		return nil, nil, fmt.Errorf("openai responses terminal output must be a JSON array")
	}
	var output []responsesOutputItem
	if err := json.Unmarshal(trimmed, &output); err != nil {
		return nil, nil, fmt.Errorf("decode openai responses terminal output: %w", err)
	}
	providerState := append(json.RawMessage(nil), trimmed...)
	return providerState, output, nil
}

func responsesDoneEvent(usage responsesUsage, stopReason provider.StopReason, state json.RawMessage) provider.Event {
	return provider.Event{
		Kind: provider.EventDone,
		Usage: provider.Usage{
			InputTokens:       usage.InputTokens,
			CachedInputTokens: usage.InputTokensDetails.CachedTokens,
			OutputTokens:      usage.OutputTokens,
			TotalTokens:       usage.TotalTokens,
		},
		StopReason:    stopReason,
		ProviderState: state,
	}
}

func responsesError(apiError *responsesAPIError) error {
	if apiError == nil {
		return &provider.Error{Provider: "openai", Kind: provider.ErrorUnknown, Message: "responses API failed"}
	}
	return &provider.Error{
		Provider: "openai",
		Kind:     openAIErrorKind(apiError.Type, apiError.Code),
		Code:     apiError.Code,
		Message:  apiError.Message,
	}
}

func openAIErrorKind(errorType, code string) provider.ErrorKind {
	switch {
	case code == "rate_limit_exceeded" || errorType == "rate_limit_error":
		return provider.ErrorRateLimit
	case code == "server_error" || code == "server_is_overloaded" || code == "model_error" ||
		errorType == "api_error" || errorType == "overloaded_error":
		return provider.ErrorServer
	case code == "stream_error":
		return provider.ErrorStream
	case code == "invalid_request_error" || code == "invalid_request" ||
		code == "unsupported_parameter" || errorType == "invalid_request_error":
		return provider.ErrorInvalidRequest
	case code == "invalid_api_key" || errorType == "authentication_error":
		return provider.ErrorAuthentication
	case errorType == "permission_error":
		return provider.ErrorPermission
	case errorType == "not_found_error":
		return provider.ErrorNotFound
	default:
		return provider.ErrorUnknown
	}
}

func (s *responsesStream) recordOutputItem(index int, item responsesOutputItem) {
	state := s.outputState(index)
	state.phase = normalizeTextPhase(item.Phase)
	if item.CallID != "" {
		state.callID = item.CallID
	}
	if item.Name != "" {
		state.name = item.Name
	}
}

func (s *responsesStream) outputState(index int) *responsesOutputState {
	state, ok := s.items[index]
	if !ok {
		state = &responsesOutputState{}
		s.items[index] = state
	}
	return state
}

func (s *responsesStream) textPhase(index int) provider.TextPhase {
	return s.outputState(index).phase
}

func normalizeTextPhase(phase provider.TextPhase) provider.TextPhase {
	switch phase {
	case provider.TextPhaseCommentary, provider.TextPhaseFinalAnswer:
		return phase
	default:
		return ""
	}
}

func (s *responsesStream) Close() error {
	return s.body.Close()
}
