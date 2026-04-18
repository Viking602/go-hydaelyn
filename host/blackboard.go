package host

import (
	"fmt"
	"strings"

	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

var publishPipeline = blackboard.NewPipeline()

const (
	verifierNamespacePrefix        = "verify."
	verifierGateExchangeKey        = "verify.gate"
	verifierGateDecisionField      = "synthesis_gate"
	verifierGateStatusField        = "verification_status"
	verifierGateSummaryField       = "verifier_summary"
	verifierGateEvidenceCountField = "published_input_count"
	verifierGatePassDecision       = "pass"
	verifierGateBlockDecision      = "block"
)

func (r *Runtime) applyBlackboardUpdate(state team.RunState, task team.Task) team.RunState {
	if task.Result == nil {
		return state
	}
	if state.Blackboard == nil && !needsBlackboard(task) {
		return state
	}
	if state.Blackboard == nil {
		state.Blackboard = &blackboard.State{}
	}
	claimIDs := []string{}
	findingIDs := []string{}
	switch task.Kind {
	case team.TaskKindResearch:
		request := blackboard.PublishRequest{
			TaskID:     task.ID,
			Title:      task.Title,
			Summary:    task.Result.Summary,
			Confidence: task.Result.Confidence,
			Evidence:   evidenceInputs(task.Result.Evidence),
		}
		published := publishPipeline.Publish(state.Blackboard, request)
		if published.ClaimID != "" {
			claimIDs = append(claimIDs, published.ClaimID)
		}
		if published.FindingID != "" {
			findingIDs = append(findingIDs, published.FindingID)
		}
	case team.TaskKindVerify:
		status := blackboard.InferVerificationStatus(task.Result.Summary)
		confidence := task.Result.Confidence
		if confidence <= 0 {
			confidence = 0.75
		}
		readExchanges := verifierPublishedInputs(state.Blackboard, task)
		for _, dependencyID := range task.DependsOn {
			for _, claim := range state.Blackboard.ClaimsForTask(dependencyID) {
				state.Blackboard.UpsertVerification(blackboard.VerificationResult{
					ClaimID:    claim.ID,
					Status:     status,
					Confidence: confidence,
					Rationale:  strings.TrimSpace(task.Result.Summary),
				})
				claimIDs = appendUnique(claimIDs, claim.ID)
				for _, finding := range state.Blackboard.FindingsForClaim(claim.ID) {
					findingIDs = appendUnique(findingIDs, finding.ID)
					if status == blackboard.VerificationStatusSupported {
						_, _ = state.Blackboard.UpsertExchangeCAS(blackboard.Exchange{
							Key:        "supported_findings",
							Namespace:  task.Namespace,
							TaskID:     task.ID,
							Version:    task.Version,
							ValueType:  blackboard.ExchangeValueTypeFindingRef,
							Text:       finding.Summary,
							ClaimIDs:   []string{claim.ID},
							FindingIDs: []string{finding.ID},
							Metadata: map[string]string{
								"status": string(status),
							},
						})
					}
				}
			}
		}
		_, _ = state.Blackboard.UpsertExchangeCAS(verifierGateExchange(task, status, readExchanges, claimIDs, findingIDs))
	}
	if task.PublishesTo(team.OutputVisibilityBlackboard) {
		for _, key := range task.Writes {
			_, _ = state.Blackboard.UpsertExchangeCAS(exchangeForTaskOutput(task, key, claimIDs, findingIDs))
		}
	}
	return state
}

func evidenceInputs(items []team.Evidence) []blackboard.EvidenceInput {
	result := make([]blackboard.EvidenceInput, 0, len(items))
	for _, item := range items {
		result = append(result, blackboard.EvidenceInput{
			Source:  item.Source,
			Snippet: item.Snippet,
		})
	}
	return result
}

func needsBlackboard(task team.Task) bool {
	return task.Kind == team.TaskKindResearch || task.Kind == team.TaskKindVerify || task.PublishesTo(team.OutputVisibilityBlackboard)
}

func exchangeForTaskOutput(task team.Task, key string, claimIDs, findingIDs []string) blackboard.Exchange {
	exchange := blackboard.Exchange{
		Key:         key,
		Namespace:   task.Namespace,
		TaskID:      task.ID,
		Version:     task.Version,
		Text:        task.Result.Summary,
		Structured:  cloneStructuredMap(task.Result.Structured),
		ArtifactIDs: append([]string{}, task.Result.ArtifactIDs...),
		ClaimIDs:    append([]string{}, claimIDs...),
		FindingIDs:  append([]string{}, findingIDs...),
		Metadata: map[string]string{
			"kind": string(task.Kind),
		},
	}
	if task.Kind == team.TaskKindVerify {
		status := blackboard.InferVerificationStatus(task.Result.Summary)
		decision := verifierGateDecision(status)
		exchange.Metadata[verifierGateStatusField] = string(status)
		exchange.Metadata[verifierGateDecisionField] = decision
		exchange.Structured = mergeStructuredMaps(exchange.Structured, map[string]any{
			verifierGateStatusField:   string(status),
			verifierGateDecisionField: decision,
			verifierGateSummaryField:  strings.TrimSpace(task.Result.Summary),
		})
	}
	switch {
	case len(findingIDs) > 0:
		exchange.ValueType = blackboard.ExchangeValueTypeFindingRef
	case len(claimIDs) > 0:
		exchange.ValueType = blackboard.ExchangeValueTypeClaimRef
	case len(task.Result.Structured) > 0:
		exchange.ValueType = blackboard.ExchangeValueTypeJSON
	case len(task.Result.ArtifactIDs) > 0:
		exchange.ValueType = blackboard.ExchangeValueTypeArtifactRef
	default:
		exchange.ValueType = blackboard.ExchangeValueTypeText
	}
	return exchange
}

func verifierPublishedInputs(board *blackboard.State, task team.Task) []blackboard.Exchange {
	if board == nil || len(task.Reads) == 0 {
		return nil
	}
	items := make([]blackboard.Exchange, 0, len(task.Reads))
	for _, key := range task.Reads {
		items = append(items, board.ExchangesForKey(key)...)
	}
	return items
}

func verifierGateExchange(task team.Task, status blackboard.VerificationStatus, publishedInputs []blackboard.Exchange, claimIDs, findingIDs []string) blackboard.Exchange {
	decision := verifierGateDecision(status)
	structured := map[string]any{
		verifierGateStatusField:        string(status),
		verifierGateDecisionField:      decision,
		verifierGateSummaryField:       strings.TrimSpace(task.Result.Summary),
		verifierGateEvidenceCountField: len(publishedInputs),
	}
	if len(task.Reads) > 0 {
		structured["read_keys"] = append([]string{}, task.Reads...)
	}
	if len(claimIDs) > 0 {
		structured["claim_ids"] = append([]string{}, claimIDs...)
	}
	if len(findingIDs) > 0 {
		structured["finding_ids"] = append([]string{}, findingIDs...)
	}
	text := strings.TrimSpace(task.Result.Summary)
	if text == "" {
		text = fmt.Sprintf("verifier %s", decision)
	}
	return blackboard.Exchange{
		Key:        verifierGateExchangeKey,
		Namespace:  task.Namespace,
		TaskID:     task.ID,
		Version:    task.Version,
		ValueType:  blackboard.ExchangeValueTypeJSON,
		Text:       text,
		Structured: structured,
		ClaimIDs:   append([]string{}, claimIDs...),
		FindingIDs: append([]string{}, findingIDs...),
		Metadata: map[string]string{
			"kind":                      string(task.Kind),
			verifierGateStatusField:     string(status),
			verifierGateDecisionField:   decision,
			"collaboration_namespace":   task.Namespace,
			"collaboration_gate_source": "verifier",
		},
	}
}

func verifierGateDecision(status blackboard.VerificationStatus) string {
	if status == blackboard.VerificationStatusSupported {
		return verifierGatePassDecision
	}
	return verifierGateBlockDecision
}

func mergeStructuredMaps(base map[string]any, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	merged := cloneStructuredMap(base)
	if merged == nil {
		merged = map[string]any{}
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func cloneStructuredMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]any, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func appendUnique(items []string, value string) []string {
	for _, current := range items {
		if current == value {
			return items
		}
	}
	return append(items, value)
}
