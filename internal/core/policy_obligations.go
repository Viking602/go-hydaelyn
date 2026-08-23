package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Viking602/venat/internal/core/model"
)

var policyEmailRE = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

func (r *Runtime) currentPolicyEnforcer() PolicyObligationEnforcer {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	if r.policyEnforcer == nil {
		return defaultPolicyObligationEnforcer{}
	}
	return r.policyEnforcer
}

func (r *Runtime) enforceBlackboardReadUoW(
	ctx context.Context,
	uow UnitOfWork,
	decision model.PolicyDecision,
	selector model.BlackboardSelector,
	items []model.BlackboardItem,
) (model.BlackboardSelector, []model.BlackboardItem, error) {
	enforcedSelector, enforcedItems, err := r.currentPolicyEnforcer().EnforceBlackboardRead(ctx, decision, selector, items)
	if err == nil {
		return enforcedSelector, enforcedItems, nil
	}
	if eventErr := appendPolicyObligationFailure(ctx, uow, selector.RunID, "", decision, err); eventErr != nil {
		return model.BlackboardSelector{}, nil, fmt.Errorf("%w: record obligation failure: %w", err, eventErr)
	}
	return model.BlackboardSelector{}, nil, commitWithError(err)
}

func (r *Runtime) enforceBlackboardWriteUoW(
	ctx context.Context,
	uow UnitOfWork,
	decision model.PolicyDecision,
	item model.BlackboardItem,
) (model.BlackboardItem, error) {
	enforced, err := r.currentPolicyEnforcer().EnforceBlackboardWrite(ctx, decision, item)
	if err == nil {
		return enforced, nil
	}
	if eventErr := appendPolicyObligationFailure(ctx, uow, item.RunID, item.TaskID, decision, err); eventErr != nil {
		return model.BlackboardItem{}, fmt.Errorf("%w: record obligation failure: %w", err, eventErr)
	}
	return model.BlackboardItem{}, commitWithError(err)
}

func (r *Runtime) EnforceToolResult(
	ctx context.Context,
	runID string,
	taskID string,
	decision model.PolicyDecision,
	result json.RawMessage,
) (enforced json.RawMessage, err error) {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackIfNotCommitted(ctx, uow, &committed, &err)
	enforced, enforcementErr := r.currentPolicyEnforcer().EnforceToolResult(ctx, decision, result)
	if enforcementErr != nil {
		if eventErr := appendPolicyObligationFailure(ctx, uow, runID, taskID, decision, enforcementErr); eventErr != nil {
			return nil, fmt.Errorf("%w: record obligation failure: %w", enforcementErr, eventErr)
		}
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	if enforcementErr != nil {
		return nil, enforcementErr
	}
	return enforced, nil
}

func (r *Runtime) enforceHandoffUoW(
	ctx context.Context,
	uow UnitOfWork,
	decision model.PolicyDecision,
	handoff model.HandoffRequest,
) (model.HandoffRequest, error) {
	enforced, err := r.currentPolicyEnforcer().EnforceHandoff(ctx, decision, handoff)
	if err == nil {
		return enforced, nil
	}
	if eventErr := appendPolicyObligationFailure(ctx, uow, handoff.RunID, handoff.TaskID, decision, err); eventErr != nil {
		return model.HandoffRequest{}, fmt.Errorf("%w: record obligation failure: %w", err, eventErr)
	}
	return model.HandoffRequest{}, commitWithError(err)
}

func (r *Runtime) enforceResponseUoW(
	ctx context.Context,
	uow UnitOfWork,
	decision model.PolicyDecision,
	message model.UserMessage,
) (model.UserMessage, error) {
	enforced, err := r.currentPolicyEnforcer().EnforceResponse(ctx, decision, message)
	if err == nil {
		return enforced, nil
	}
	if eventErr := appendPolicyObligationFailure(ctx, uow, message.RunID, message.TaskID, decision, err); eventErr != nil {
		return model.UserMessage{}, fmt.Errorf("%w: record obligation failure: %w", err, eventErr)
	}
	return model.UserMessage{}, err
}

func (r *Runtime) enforceTraceSpansUoW(
	ctx context.Context,
	uow UnitOfWork,
	runID string,
	decision model.PolicyDecision,
	spans []model.TraceSpan,
) ([]model.TraceSpan, error) {
	out := make([]model.TraceSpan, 0, len(spans))
	for _, span := range spans {
		enforced, visible, err := r.currentPolicyEnforcer().EnforceTrace(ctx, decision, span)
		if err != nil {
			if eventErr := appendPolicyObligationFailure(ctx, uow, runID, span.TaskID, decision, err); eventErr != nil {
				return nil, fmt.Errorf("%w: record obligation failure: %w", err, eventErr)
			}
			return nil, commitWithError(err)
		}
		if visible {
			out = append(out, enforced)
		}
	}
	return out, nil
}

func appendPolicyObligationFailure(
	ctx context.Context,
	uow UnitOfWork,
	runID string,
	taskID string,
	decision model.PolicyDecision,
	enforcementErr error,
) error {
	return uow.Events().AppendEvent(ctx, model.Event{
		RunID:  runID,
		TaskID: taskID,
		Type:   model.EventPolicyObligationFailed,
		Payload: map[string]any{
			"decisionId":      decision.DecisionID,
			"reason":          enforcementErr.Error(),
			"effectiveEffect": string(model.PolicyEffectDeny),
		},
		RecordedAt: time.Now().UTC(),
	})
}

type defaultPolicyObligationEnforcer struct{}

func (defaultPolicyObligationEnforcer) EnforceBlackboardRead(
	_ context.Context,
	decision model.PolicyDecision,
	selector model.BlackboardSelector,
	items []model.BlackboardItem,
) (model.BlackboardSelector, []model.BlackboardItem, error) {
	obligations, err := policyObligationsForTarget(decision, model.PolicyTargetBlackboardRead)
	if err != nil {
		return model.BlackboardSelector{}, nil, err
	}
	out := append([]model.BlackboardItem(nil), items...)
	for _, obligation := range obligations {
		switch obligation.Kind {
		case model.ObligationSelectorOnly:
			if obligation.Selector == nil {
				return model.BlackboardSelector{}, nil, policyObligationError(obligation, "selector is required")
			}
			filtered := out[:0]
			for _, item := range out {
				if blackboardItemMatchesSelector(item, *obligation.Selector) {
					filtered = append(filtered, item)
				}
			}
			out = filtered
		case model.ObligationRedactFields:
			for index := range out {
				out[index], err = redactBlackboardItem(out[index], decision.Redactions)
				if err != nil {
					return model.BlackboardSelector{}, nil, err
				}
			}
		default:
			return model.BlackboardSelector{}, nil, policyObligationError(obligation, "obligation is not supported for blackboard reads")
		}
	}
	return selector, out, nil
}

func (defaultPolicyObligationEnforcer) EnforceBlackboardWrite(
	_ context.Context,
	decision model.PolicyDecision,
	item model.BlackboardItem,
) (model.BlackboardItem, error) {
	obligations, err := policyObligationsForTarget(decision, model.PolicyTargetBlackboardWrite)
	if err != nil {
		return model.BlackboardItem{}, err
	}
	out := item
	for _, obligation := range obligations {
		switch obligation.Kind {
		case model.ObligationSelectorOnly:
			if obligation.Selector == nil {
				return model.BlackboardItem{}, policyObligationError(obligation, "selector is required")
			}
			if !blackboardItemMatchesSelector(out, *obligation.Selector) {
				return model.BlackboardItem{}, policyObligationError(obligation, "item is outside the allowed selector")
			}
		case model.ObligationRedactFields:
			out, err = redactBlackboardItem(out, decision.Redactions)
			if err != nil {
				return model.BlackboardItem{}, err
			}
		default:
			return model.BlackboardItem{}, policyObligationError(obligation, "obligation is not supported for blackboard writes")
		}
	}
	return out, nil
}

func (defaultPolicyObligationEnforcer) EnforceToolResult(
	_ context.Context,
	decision model.PolicyDecision,
	result json.RawMessage,
) (json.RawMessage, error) {
	obligations, err := policyObligationsForTarget(decision, model.PolicyTargetToolResult)
	if err != nil {
		return nil, err
	}
	out := append(json.RawMessage(nil), result...)
	for _, obligation := range obligations {
		value, fields, err := decodePolicyToolResult(out, obligation)
		if err != nil {
			return nil, err
		}
		switch obligation.Kind {
		case model.ObligationMaskToolOutput:
			value.Content = "[masked]"
			delete(fields, "structured")
			if err := setPolicyToolResultString(fields, "content", value.Content); err != nil {
				return nil, err
			}
		case model.ObligationRedactFields:
			if err := redactPolicyToolResult(&value, fields, decision.Redactions, obligation); err != nil {
				return nil, err
			}
		case model.ObligationHideInternalTrace:
			value.Content = hidePolicyTraceLines(value.Content)
			if err := setPolicyToolResultString(fields, "content", value.Content); err != nil {
				return nil, err
			}
		default:
			return nil, policyObligationError(obligation, "obligation is not supported for tool results")
		}
		out, err = json.Marshal(fields)
		if err != nil {
			return nil, fmt.Errorf("%w: encode enforced tool result: %w", model.ErrPolicyObligationFailed, err)
		}
	}
	return out, nil
}

type policyToolResult struct {
	Content string `json:"content,omitempty"`
}

func decodePolicyToolResult(
	result json.RawMessage,
	obligation model.PolicyObligation,
) (policyToolResult, map[string]json.RawMessage, error) {
	var value policyToolResult
	fields := make(map[string]json.RawMessage)
	if len(result) == 0 {
		return value, fields, nil
	}
	if err := json.Unmarshal(result, &value); err != nil {
		return policyToolResult{}, nil, policyObligationError(obligation, "tool result is not valid JSON")
	}
	if err := json.Unmarshal(result, &fields); err != nil {
		return policyToolResult{}, nil, policyObligationError(obligation, "tool result is not a JSON object")
	}
	if fields == nil {
		fields = make(map[string]json.RawMessage)
	}
	return value, fields, nil
}

func redactPolicyToolResult(
	value *policyToolResult,
	fields map[string]json.RawMessage,
	redactions []string,
	obligation model.PolicyObligation,
) error {
	for _, field := range defaultRedactions(redactions) {
		switch field {
		case "email":
			value.Content = redactPolicyEmail(value.Content)
			if err := setPolicyToolResultString(fields, "content", value.Content); err != nil {
				return err
			}
			if structured, ok := fields["structured"]; ok {
				redacted, err := redactPolicyStructuredEmails(structured)
				if err != nil {
					return policyObligationError(obligation, "structured tool result is not valid JSON")
				}
				fields["structured"] = redacted
			}
		case "content":
			value.Content = "[redacted]"
			if err := setPolicyToolResultString(fields, "content", value.Content); err != nil {
				return err
			}
		case "structured":
			delete(fields, "structured")
		default:
			return policyRedactionError(field, model.PolicyTargetToolResult)
		}
	}
	return nil
}

func (defaultPolicyObligationEnforcer) EnforceHandoff(
	_ context.Context,
	decision model.PolicyDecision,
	handoff model.HandoffRequest,
) (model.HandoffRequest, error) {
	obligations, err := policyObligationsForTarget(decision, model.PolicyTargetHandoff)
	if err != nil {
		return model.HandoffRequest{}, err
	}
	out := handoff
	for _, obligation := range obligations {
		switch obligation.Kind {
		case model.ObligationRestrictHandoffContext:
			out.ContextSummary = ""
			out.ContextReferences = nil
			out.ContextSelectors = nil
			out.Metadata = nil
		case model.ObligationRedactFields:
			for _, field := range defaultRedactions(decision.Redactions) {
				switch field {
				case "email":
					out.Reason = redactPolicyEmail(out.Reason)
					out.ContextSummary = redactPolicyEmail(out.ContextSummary)
				case "reason":
					out.Reason = "[redacted]"
				case "contextSummary":
					out.ContextSummary = "[redacted]"
				case "contextReferences":
					out.ContextReferences = nil
				case "contextSelectors":
					out.ContextSelectors = nil
				case "metadata":
					out.Metadata = nil
				default:
					return model.HandoffRequest{}, policyRedactionError(field, model.PolicyTargetHandoff)
				}
			}
		default:
			return model.HandoffRequest{}, policyObligationError(obligation, "obligation is not supported for handoff")
		}
	}
	return out, nil
}

func (defaultPolicyObligationEnforcer) EnforceResponse(
	_ context.Context,
	decision model.PolicyDecision,
	message model.UserMessage,
) (model.UserMessage, error) {
	obligations, err := policyObligationsForTarget(decision, model.PolicyTargetResponse)
	if err != nil {
		return model.UserMessage{}, err
	}
	out := message
	for _, obligation := range obligations {
		switch obligation.Kind {
		case model.ObligationRedactFields:
			for _, field := range defaultRedactions(decision.Redactions) {
				switch field {
				case "email":
					out.Title = redactPolicyEmail(out.Title)
					out.Payload = redactPolicyEmail(out.Payload)
				case "title":
					out.Title = "[redacted]"
				case "payload":
					out.Payload = "[redacted]"
				default:
					return model.UserMessage{}, policyRedactionError(field, model.PolicyTargetResponse)
				}
			}
		case model.ObligationHideInternalTrace:
			out.Payload = hidePolicyTraceLines(out.Payload)
		case model.ObligationMaskToolOutput:
			out.Payload = maskPolicyToolOutputLines(out.Payload)
		default:
			return model.UserMessage{}, policyObligationError(obligation, "obligation is not supported for responses")
		}
	}
	return out, nil
}

func (defaultPolicyObligationEnforcer) EnforceTrace(
	_ context.Context,
	decision model.PolicyDecision,
	span model.TraceSpan,
) (model.TraceSpan, bool, error) {
	obligations, err := policyObligationsForTarget(decision, model.PolicyTargetTrace)
	if err != nil {
		return model.TraceSpan{}, false, err
	}
	out := span
	out.Metadata = maps.Clone(span.Metadata)
	visible := true
	for _, obligation := range obligations {
		switch obligation.Kind {
		case model.ObligationHideInternalTrace:
			visible = false
		case model.ObligationRedactFields:
			for _, field := range defaultRedactions(decision.Redactions) {
				switch {
				case field == "email":
					out.Error = redactPolicyEmail(out.Error)
					for key, value := range out.Metadata {
						out.Metadata[key] = redactPolicyEmail(value)
					}
				case field == "error":
					out.Error = "[redacted]"
				case field == "metadata":
					out.Metadata = nil
				case strings.HasPrefix(field, "metadata."):
					delete(out.Metadata, strings.TrimPrefix(field, "metadata."))
				default:
					return model.TraceSpan{}, false, policyRedactionError(field, model.PolicyTargetTrace)
				}
			}
		default:
			return model.TraceSpan{}, false, policyObligationError(obligation, "obligation is not supported for traces")
		}
	}
	return out, visible, nil
}

func policyObligationsForTarget(decision model.PolicyDecision, target model.PolicyObligationTarget) ([]model.PolicyObligation, error) {
	out := make([]model.PolicyObligation, 0, len(decision.Obligations))
	for _, obligation := range decision.Obligations {
		if !knownPolicyObligation(obligation.Kind) {
			return nil, policyObligationError(obligation, "unknown obligation")
		}
		if obligation.Target != "" && !knownPolicyTarget(obligation.Target) {
			return nil, policyObligationError(obligation, "unknown target")
		}
		if obligation.Target != "" {
			if obligation.Target == target {
				out = append(out, obligation)
			}
			continue
		}
		if defaultPolicyTargetApplies(obligation.Kind, target) {
			out = append(out, obligation)
		}
	}
	return out, nil
}

func knownPolicyObligation(kind model.ObligationKind) bool {
	switch kind {
	case model.ObligationRedactFields,
		model.ObligationSelectorOnly,
		model.ObligationRequireHumanApproval,
		model.ObligationHideInternalTrace,
		model.ObligationMaskToolOutput,
		model.ObligationRestrictHandoffContext:
		return true
	default:
		return false
	}
}

func knownPolicyTarget(target model.PolicyObligationTarget) bool {
	switch target {
	case model.PolicyTargetBlackboardRead,
		model.PolicyTargetBlackboardWrite,
		model.PolicyTargetToolResult,
		model.PolicyTargetHandoff,
		model.PolicyTargetResponse,
		model.PolicyTargetTrace:
		return true
	default:
		return false
	}
}

func defaultPolicyTargetApplies(kind model.ObligationKind, target model.PolicyObligationTarget) bool {
	switch kind {
	case model.ObligationRedactFields:
		return target == model.PolicyTargetResponse
	case model.ObligationSelectorOnly:
		return target == model.PolicyTargetBlackboardRead || target == model.PolicyTargetBlackboardWrite
	case model.ObligationHideInternalTrace:
		return target == model.PolicyTargetResponse || target == model.PolicyTargetTrace
	case model.ObligationMaskToolOutput:
		return target == model.PolicyTargetToolResult
	case model.ObligationRestrictHandoffContext:
		return target == model.PolicyTargetHandoff
	default:
		return false
	}
}

func policyObligationError(obligation model.PolicyObligation, reason string) error {
	return fmt.Errorf("%w: %s %q for target %q", model.ErrPolicyObligationFailed, reason, obligation.Kind, obligation.Target)
}

func policyRedactionError(field string, target model.PolicyObligationTarget) error {
	return fmt.Errorf("%w: unsupported redaction %q for target %q", model.ErrPolicyObligationFailed, field, target)
}

func defaultRedactions(redactions []string) []string {
	if len(redactions) == 0 {
		return []string{"email"}
	}
	return redactions
}

func redactBlackboardItem(item model.BlackboardItem, redactions []string) (model.BlackboardItem, error) {
	out := item
	for _, field := range defaultRedactions(redactions) {
		switch field {
		case "email":
			out.Content = redactPolicyEmail(out.Content)
			out.Payload = redactPolicyEmail(out.Payload)
		case "content":
			out.Content = "[redacted]"
		case "payload":
			out.Payload = "[redacted]"
		case "evidenceRefs":
			out.EvidenceRefs = nil
		case "artifactRefs":
			out.ArtifactRefs = nil
		default:
			return model.BlackboardItem{}, policyRedactionError(field, model.PolicyTargetBlackboardRead)
		}
	}
	return out, nil
}

func blackboardItemMatchesSelector(item model.BlackboardItem, selector model.BlackboardSelector) bool {
	if selector.RunID != "" && selector.RunID != item.RunID {
		return false
	}
	if selector.TaskID != "" && selector.TaskID != item.TaskID {
		return false
	}
	if len(selector.ItemTypes) > 0 && !slices.Contains(selector.ItemTypes, item.Type) {
		return false
	}
	if len(selector.SourceTypes) > 0 && !slices.Contains(selector.SourceTypes, item.Source.Type) {
		return false
	}
	if len(selector.SourceIDs) > 0 && !slices.Contains(selector.SourceIDs, item.Source.ID) {
		return false
	}
	if selector.Visibility != "" && selector.Visibility != item.Visibility {
		return false
	}
	if selector.SinceVersion > 0 && item.Version < selector.SinceVersion {
		return false
	}
	if len(selector.Keys) > 0 && !slices.Contains(selector.Keys, item.Key) {
		return false
	}
	return true
}

func setPolicyToolResultString(fields map[string]json.RawMessage, name, value string) error {
	if value == "" {
		delete(fields, name)
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("%w: encode enforced tool result field %q: %w", model.ErrPolicyObligationFailed, name, err)
	}
	fields[name] = encoded
	return nil
}

func redactPolicyStructuredEmails(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("structured JSON contains multiple values")
		}
		return nil, err
	}

	value = redactPolicyStructuredValue(value)
	return json.Marshal(value)
}

func redactPolicyStructuredValue(value any) any {
	switch current := value.(type) {
	case string:
		return redactPolicyEmail(current)
	case map[string]any:
		for key, nested := range current {
			current[key] = redactPolicyStructuredValue(nested)
		}
	case []any:
		for index, nested := range current {
			current[index] = redactPolicyStructuredValue(nested)
		}
	}
	return value
}

func redactPolicyEmail(value string) string {
	return policyEmailRE.ReplaceAllString(value, "[redacted-email]")
}

func hidePolicyTraceLines(value string) string {
	lines := strings.Split(value, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if !strings.Contains(strings.ToLower(line), "internal trace") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func maskPolicyToolOutputLines(value string) string {
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		if marker := strings.Index(strings.ToLower(line), "tool output:"); marker >= 0 {
			lines[index] = line[:marker] + "tool output: [masked]"
		}
	}
	return strings.Join(lines, "\n")
}
