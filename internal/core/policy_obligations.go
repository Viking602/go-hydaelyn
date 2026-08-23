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

	"github.com/Viking602/venat/api"
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
	decision api.PolicyDecision,
	selector api.BlackboardSelector,
	items []api.BlackboardItem,
) (api.BlackboardSelector, []api.BlackboardItem, error) {
	enforcedSelector, enforcedItems, err := r.currentPolicyEnforcer().EnforceBlackboardRead(ctx, decision, selector, items)
	if err == nil {
		return enforcedSelector, enforcedItems, nil
	}
	if eventErr := appendPolicyObligationFailure(ctx, uow, selector.RunID, "", decision, err); eventErr != nil {
		return api.BlackboardSelector{}, nil, fmt.Errorf("%w: record obligation failure: %w", err, eventErr)
	}
	return api.BlackboardSelector{}, nil, commitWithError(err)
}

func (r *Runtime) enforceBlackboardWriteUoW(
	ctx context.Context,
	uow UnitOfWork,
	decision api.PolicyDecision,
	item api.BlackboardItem,
) (api.BlackboardItem, error) {
	enforced, err := r.currentPolicyEnforcer().EnforceBlackboardWrite(ctx, decision, item)
	if err == nil {
		return enforced, nil
	}
	if eventErr := appendPolicyObligationFailure(ctx, uow, item.RunID, item.TaskID, decision, err); eventErr != nil {
		return api.BlackboardItem{}, fmt.Errorf("%w: record obligation failure: %w", err, eventErr)
	}
	return api.BlackboardItem{}, commitWithError(err)
}

func (r *Runtime) EnforceToolResult(
	ctx context.Context,
	runID string,
	taskID string,
	decision api.PolicyDecision,
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
	decision api.PolicyDecision,
	handoff api.HandoffRequest,
) (api.HandoffRequest, error) {
	enforced, err := r.currentPolicyEnforcer().EnforceHandoff(ctx, decision, handoff)
	if err == nil {
		return enforced, nil
	}
	if eventErr := appendPolicyObligationFailure(ctx, uow, handoff.RunID, handoff.TaskID, decision, err); eventErr != nil {
		return api.HandoffRequest{}, fmt.Errorf("%w: record obligation failure: %w", err, eventErr)
	}
	return api.HandoffRequest{}, commitWithError(err)
}

func (r *Runtime) enforceResponseUoW(
	ctx context.Context,
	uow UnitOfWork,
	decision api.PolicyDecision,
	message api.UserMessage,
) (api.UserMessage, error) {
	enforced, err := r.currentPolicyEnforcer().EnforceResponse(ctx, decision, message)
	if err == nil {
		return enforced, nil
	}
	if eventErr := appendPolicyObligationFailure(ctx, uow, message.RunID, message.TaskID, decision, err); eventErr != nil {
		return api.UserMessage{}, fmt.Errorf("%w: record obligation failure: %w", err, eventErr)
	}
	return api.UserMessage{}, err
}

func (r *Runtime) enforceTraceSpansUoW(
	ctx context.Context,
	uow UnitOfWork,
	runID string,
	decision api.PolicyDecision,
	spans []api.TraceSpan,
) ([]api.TraceSpan, error) {
	out := make([]api.TraceSpan, 0, len(spans))
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
	decision api.PolicyDecision,
	enforcementErr error,
) error {
	return uow.Events().AppendEvent(ctx, api.Event{
		RunID:  runID,
		TaskID: taskID,
		Type:   api.EventPolicyObligationFailed,
		Payload: map[string]any{
			"decisionId":      decision.DecisionID,
			"reason":          enforcementErr.Error(),
			"effectiveEffect": string(api.PolicyEffectDeny),
		},
		RecordedAt: time.Now().UTC(),
	})
}

type defaultPolicyObligationEnforcer struct{}

func (defaultPolicyObligationEnforcer) EnforceBlackboardRead(
	_ context.Context,
	decision api.PolicyDecision,
	selector api.BlackboardSelector,
	items []api.BlackboardItem,
) (api.BlackboardSelector, []api.BlackboardItem, error) {
	obligations, err := policyObligationsForTarget(decision, api.PolicyTargetBlackboardRead)
	if err != nil {
		return api.BlackboardSelector{}, nil, err
	}
	out := append([]api.BlackboardItem(nil), items...)
	for _, obligation := range obligations {
		switch obligation.Kind {
		case api.ObligationSelectorOnly:
			if obligation.Selector == nil {
				return api.BlackboardSelector{}, nil, policyObligationError(obligation, "selector is required")
			}
			filtered := out[:0]
			for _, item := range out {
				if blackboardItemMatchesSelector(item, *obligation.Selector) {
					filtered = append(filtered, item)
				}
			}
			out = filtered
		case api.ObligationRedactFields:
			for index := range out {
				out[index], err = redactBlackboardItem(out[index], decision.Redactions)
				if err != nil {
					return api.BlackboardSelector{}, nil, err
				}
			}
		default:
			return api.BlackboardSelector{}, nil, policyObligationError(obligation, "obligation is not supported for blackboard reads")
		}
	}
	return selector, out, nil
}

func (defaultPolicyObligationEnforcer) EnforceBlackboardWrite(
	_ context.Context,
	decision api.PolicyDecision,
	item api.BlackboardItem,
) (api.BlackboardItem, error) {
	obligations, err := policyObligationsForTarget(decision, api.PolicyTargetBlackboardWrite)
	if err != nil {
		return api.BlackboardItem{}, err
	}
	out := item
	for _, obligation := range obligations {
		switch obligation.Kind {
		case api.ObligationSelectorOnly:
			if obligation.Selector == nil {
				return api.BlackboardItem{}, policyObligationError(obligation, "selector is required")
			}
			if !blackboardItemMatchesSelector(out, *obligation.Selector) {
				return api.BlackboardItem{}, policyObligationError(obligation, "item is outside the allowed selector")
			}
		case api.ObligationRedactFields:
			out, err = redactBlackboardItem(out, decision.Redactions)
			if err != nil {
				return api.BlackboardItem{}, err
			}
		default:
			return api.BlackboardItem{}, policyObligationError(obligation, "obligation is not supported for blackboard writes")
		}
	}
	return out, nil
}

func (defaultPolicyObligationEnforcer) EnforceToolResult(
	_ context.Context,
	decision api.PolicyDecision,
	result json.RawMessage,
) (json.RawMessage, error) {
	obligations, err := policyObligationsForTarget(decision, api.PolicyTargetToolResult)
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
		case api.ObligationMaskToolOutput:
			value.Content = "[masked]"
			delete(fields, "structured")
			if err := setPolicyToolResultString(fields, "content", value.Content); err != nil {
				return nil, err
			}
		case api.ObligationRedactFields:
			if err := redactPolicyToolResult(&value, fields, decision.Redactions, obligation); err != nil {
				return nil, err
			}
		case api.ObligationHideInternalTrace:
			value.Content = hidePolicyTraceLines(value.Content)
			if err := setPolicyToolResultString(fields, "content", value.Content); err != nil {
				return nil, err
			}
		default:
			return nil, policyObligationError(obligation, "obligation is not supported for tool results")
		}
		out, err = json.Marshal(fields)
		if err != nil {
			return nil, fmt.Errorf("%w: encode enforced tool result: %w", api.ErrPolicyObligationFailed, err)
		}
	}
	return out, nil
}

type policyToolResult struct {
	Content string `json:"content,omitempty"`
}

func decodePolicyToolResult(
	result json.RawMessage,
	obligation api.PolicyObligation,
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
	obligation api.PolicyObligation,
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
			return policyRedactionError(field, api.PolicyTargetToolResult)
		}
	}
	return nil
}

func (defaultPolicyObligationEnforcer) EnforceHandoff(
	_ context.Context,
	decision api.PolicyDecision,
	handoff api.HandoffRequest,
) (api.HandoffRequest, error) {
	obligations, err := policyObligationsForTarget(decision, api.PolicyTargetHandoff)
	if err != nil {
		return api.HandoffRequest{}, err
	}
	out := handoff
	for _, obligation := range obligations {
		switch obligation.Kind {
		case api.ObligationRestrictHandoffContext:
			out.ContextSummary = ""
			out.ContextReferences = nil
			out.ContextSelectors = nil
			out.Metadata = nil
		case api.ObligationRedactFields:
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
					return api.HandoffRequest{}, policyRedactionError(field, api.PolicyTargetHandoff)
				}
			}
		default:
			return api.HandoffRequest{}, policyObligationError(obligation, "obligation is not supported for handoff")
		}
	}
	return out, nil
}

func (defaultPolicyObligationEnforcer) EnforceResponse(
	_ context.Context,
	decision api.PolicyDecision,
	message api.UserMessage,
) (api.UserMessage, error) {
	obligations, err := policyObligationsForTarget(decision, api.PolicyTargetResponse)
	if err != nil {
		return api.UserMessage{}, err
	}
	out := message
	for _, obligation := range obligations {
		switch obligation.Kind {
		case api.ObligationRedactFields:
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
					return api.UserMessage{}, policyRedactionError(field, api.PolicyTargetResponse)
				}
			}
		case api.ObligationHideInternalTrace:
			out.Payload = hidePolicyTraceLines(out.Payload)
		case api.ObligationMaskToolOutput:
			out.Payload = maskPolicyToolOutputLines(out.Payload)
		default:
			return api.UserMessage{}, policyObligationError(obligation, "obligation is not supported for responses")
		}
	}
	return out, nil
}

func (defaultPolicyObligationEnforcer) EnforceTrace(
	_ context.Context,
	decision api.PolicyDecision,
	span api.TraceSpan,
) (api.TraceSpan, bool, error) {
	obligations, err := policyObligationsForTarget(decision, api.PolicyTargetTrace)
	if err != nil {
		return api.TraceSpan{}, false, err
	}
	out := span
	out.Metadata = maps.Clone(span.Metadata)
	visible := true
	for _, obligation := range obligations {
		switch obligation.Kind {
		case api.ObligationHideInternalTrace:
			visible = false
		case api.ObligationRedactFields:
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
					return api.TraceSpan{}, false, policyRedactionError(field, api.PolicyTargetTrace)
				}
			}
		default:
			return api.TraceSpan{}, false, policyObligationError(obligation, "obligation is not supported for traces")
		}
	}
	return out, visible, nil
}

func policyObligationsForTarget(decision api.PolicyDecision, target api.PolicyObligationTarget) ([]api.PolicyObligation, error) {
	out := make([]api.PolicyObligation, 0, len(decision.Obligations))
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

func knownPolicyObligation(kind api.ObligationKind) bool {
	switch kind {
	case api.ObligationRedactFields,
		api.ObligationSelectorOnly,
		api.ObligationRequireHumanApproval,
		api.ObligationHideInternalTrace,
		api.ObligationMaskToolOutput,
		api.ObligationRestrictHandoffContext:
		return true
	default:
		return false
	}
}

func knownPolicyTarget(target api.PolicyObligationTarget) bool {
	switch target {
	case api.PolicyTargetBlackboardRead,
		api.PolicyTargetBlackboardWrite,
		api.PolicyTargetToolResult,
		api.PolicyTargetHandoff,
		api.PolicyTargetResponse,
		api.PolicyTargetTrace:
		return true
	default:
		return false
	}
}

func defaultPolicyTargetApplies(kind api.ObligationKind, target api.PolicyObligationTarget) bool {
	switch kind {
	case api.ObligationRedactFields:
		return target == api.PolicyTargetResponse
	case api.ObligationSelectorOnly:
		return target == api.PolicyTargetBlackboardRead || target == api.PolicyTargetBlackboardWrite
	case api.ObligationHideInternalTrace:
		return target == api.PolicyTargetResponse || target == api.PolicyTargetTrace
	case api.ObligationMaskToolOutput:
		return target == api.PolicyTargetToolResult
	case api.ObligationRestrictHandoffContext:
		return target == api.PolicyTargetHandoff
	default:
		return false
	}
}

func policyObligationError(obligation api.PolicyObligation, reason string) error {
	return fmt.Errorf("%w: %s %q for target %q", api.ErrPolicyObligationFailed, reason, obligation.Kind, obligation.Target)
}

func policyRedactionError(field string, target api.PolicyObligationTarget) error {
	return fmt.Errorf("%w: unsupported redaction %q for target %q", api.ErrPolicyObligationFailed, field, target)
}

func defaultRedactions(redactions []string) []string {
	if len(redactions) == 0 {
		return []string{"email"}
	}
	return redactions
}

func redactBlackboardItem(item api.BlackboardItem, redactions []string) (api.BlackboardItem, error) {
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
			return api.BlackboardItem{}, policyRedactionError(field, api.PolicyTargetBlackboardRead)
		}
	}
	return out, nil
}

func blackboardItemMatchesSelector(item api.BlackboardItem, selector api.BlackboardSelector) bool {
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
		return fmt.Errorf("%w: encode enforced tool result field %q: %w", api.ErrPolicyObligationFailed, name, err)
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
