package host

import (
	"fmt"
	"strings"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
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
	var verificationStatus blackboard.VerificationStatus
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
		readExchanges := verifierPublishedInputs(state.Blackboard, task)
		verificationResults := verificationResultsForTask(state.Blackboard, task)
		verificationStatus = verificationGateStatus(task, verificationResults)
		for _, result := range verificationResults {
			state.Blackboard.UpsertVerification(result)
			claimIDs = appendUnique(claimIDs, result.ClaimID)
			for _, finding := range state.Blackboard.FindingsForClaim(result.ClaimID) {
				findingIDs = appendUnique(findingIDs, finding.ID)
				if !result.SupportsClaim(blackboard.DefaultVerificationConfidence) {
					continue
				}
				_, _ = state.Blackboard.UpsertExchangeCAS(blackboard.Exchange{
					Key:        "supported_findings",
					Namespace:  task.Namespace,
					TaskID:     task.ID,
					Version:    task.Version,
					ValueType:  blackboard.ExchangeValueTypeFindingRef,
					Text:       finding.Summary,
					ClaimIDs:   []string{result.ClaimID},
					FindingIDs: []string{finding.ID},
					Metadata: map[string]string{
						"status": string(result.Status),
					},
				})
			}
		}
		_, _ = state.Blackboard.UpsertExchangeCAS(verifierGateExchange(task, verificationStatus, readExchanges, claimIDs, findingIDs))
	}
	if task.PublishesTo(team.OutputVisibilityBlackboard) {
		for _, key := range task.Writes {
			_, _ = state.Blackboard.UpsertExchangeCAS(exchangeForTaskOutput(task, key, claimIDs, findingIDs, verificationStatus))
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

func exchangeForTaskOutput(task team.Task, key string, claimIDs, findingIDs []string, verificationStatus blackboard.VerificationStatus) blackboard.Exchange {
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
		status := verificationStatus
		if status == "" {
			status = blackboard.InferVerificationStatus(task.Result.Summary)
		}
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

func verificationResultsForTask(board *blackboard.State, task team.Task) []blackboard.VerificationResult {
	if board == nil || task.Result == nil {
		return nil
	}
	claims := claimsForVerifierTask(board, task)
	fallbackEvidenceIDs := verifierFallbackEvidenceIDs(board, task)
	claimsByID := make(map[string]blackboard.Claim, len(claims))
	for _, claim := range claims {
		claimsByID[claim.ID] = claim
	}
	if typed := typedVerificationResults(task.Result.Structured, claimsByID, fallbackEvidenceIDs, task); len(typed) > 0 {
		return typed
	}
	if structured := structuredVerificationResults(task.Result.Structured, claimsByID, fallbackEvidenceIDs, task); len(structured) > 0 {
		return structured
	}
	status := blackboard.InferVerificationStatus(task.Result.Summary)
	// We're in the text-inference branch — the worker did not ship a
	// structured verification report, so task.Result.Confidence is the
	// generic display floor set by buildTaskResult, NOT a signal about
	// verification strength. Use the verifier-specific default instead
	// so a bare-text "supported" still clears DefaultVerificationConfidence.
	confidence := 0.75
	results := make([]blackboard.VerificationResult, 0, len(claims))
	for _, claim := range claims {
		evidenceIDs := []string(nil)
		if status == blackboard.VerificationStatusSupported {
			evidenceIDs = append([]string{}, claim.EvidenceIDs...)
			if len(evidenceIDs) == 0 {
				evidenceIDs = append([]string{}, fallbackEvidenceIDs...)
			}
		}
		results = append(results, blackboard.VerificationResult{
			ClaimID:     claim.ID,
			Status:      status,
			Confidence:  confidence,
			EvidenceIDs: evidenceIDs,
			Rationale:   strings.TrimSpace(task.Result.Summary),
		})
	}
	return results
}

func claimsForVerifierTask(board *blackboard.State, task team.Task) []blackboard.Claim {
	if board == nil {
		return nil
	}
	claims := make([]blackboard.Claim, 0)
	seen := map[string]struct{}{}
	for _, dependencyID := range task.DependsOn {
		for _, claim := range board.ClaimsForTask(dependencyID) {
			if _, ok := seen[claim.ID]; ok {
				continue
			}
			seen[claim.ID] = struct{}{}
			claims = append(claims, claim)
		}
	}
	return claims
}

// typedVerificationResults decodes a team.VerificationReport out of the
// worker's structured payload and converts it into the internal
// blackboard verification-result shape. We prefer this path over the
// legacy untyped "claims": [...] decoder and over text inference: a
// well-formed typed report is the only place where the worker tells us
// exactly which claim received which verdict with what confidence, so
// whenever it is present and valid it wins.
func typedVerificationResults(payload map[string]any, claimsByID map[string]blackboard.Claim, fallbackEvidenceIDs []string, task team.Task) []blackboard.VerificationResult {
	report, ok := team.ExtractVerificationReport(payload)
	if !ok {
		return nil
	}
	if err := team.ValidateVerificationReport(report); err != nil {
		return nil
	}
	perClaim := report.PerClaim
	if len(perClaim) == 0 {
		// Overall-only verdict: fan it out across every claim the task
		// is supposed to adjudicate, so downstream SupportsClaim can
		// still match a supported overall against the pending claims.
		perClaim = make([]team.VerificationClaim, 0, len(claimsByID))
		for claimID := range claimsByID {
			perClaim = append(perClaim, team.VerificationClaim{
				ClaimID:    claimID,
				Status:     report.Status,
				Confidence: report.Confidence,
			})
		}
	}
	results := make([]blackboard.VerificationResult, 0, len(perClaim))
	for _, claim := range perClaim {
		claimID := strings.TrimSpace(claim.ClaimID)
		if claimID == "" {
			continue
		}
		status := blackboard.VerificationStatus(string(claim.Status))
		confidence := claim.Confidence
		if confidence <= 0 {
			confidence = report.Confidence
		}
		if confidence <= 0 {
			confidence = 0.75
		}
		evidenceIDs := append([]string{}, claim.EvidenceIDs...)
		if len(evidenceIDs) == 0 && status == blackboard.VerificationStatusSupported {
			if existing, ok := claimsByID[claimID]; ok {
				evidenceIDs = append([]string{}, existing.EvidenceIDs...)
			}
			if len(evidenceIDs) == 0 {
				evidenceIDs = append([]string{}, fallbackEvidenceIDs...)
			}
		}
		rationale := strings.TrimSpace(claim.Reason)
		if rationale == "" {
			rationale = strings.TrimSpace(report.Reason)
		}
		if rationale == "" {
			rationale = strings.TrimSpace(task.Result.Summary)
		}
		results = append(results, blackboard.VerificationResult{
			ClaimID:     claimID,
			Status:      status,
			Confidence:  confidence,
			EvidenceIDs: evidenceIDs,
			Rationale:   rationale,
		})
	}
	return results
}

func structuredVerificationResults(payload map[string]any, claimsByID map[string]blackboard.Claim, fallbackEvidenceIDs []string, task team.Task) []blackboard.VerificationResult {
	rawClaims, ok := payload["claims"]
	if !ok {
		return nil
	}
	items, ok := rawClaims.([]any)
	if !ok {
		return nil
	}
	results := make([]blackboard.VerificationResult, 0, len(items))
	for _, raw := range items {
		current, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		claimID := firstString(current, "claimId", "claim_id")
		if claimID == "" && len(claimsByID) == 1 {
			for onlyID := range claimsByID {
				claimID = onlyID
			}
		}
		if claimID == "" {
			continue
		}
		status := structuredVerificationStatus(current, task.Result.Summary)
		confidence := firstFloat(current, "confidence")
		if confidence <= 0 {
			confidence = task.Result.Confidence
		}
		if confidence <= 0 {
			confidence = 0.75
		}
		evidenceIDs := firstStringSlice(current, "evidenceIds", "evidence_ids", "evidenceRefs", "evidence_refs")
		if len(evidenceIDs) == 0 && status == blackboard.VerificationStatusSupported {
			if claim, ok := claimsByID[claimID]; ok {
				evidenceIDs = append([]string{}, claim.EvidenceIDs...)
			}
			if len(evidenceIDs) == 0 {
				evidenceIDs = append([]string{}, fallbackEvidenceIDs...)
			}
		}
		rationale := firstString(current, "rationale", "claim")
		if strings.TrimSpace(rationale) == "" {
			rationale = strings.TrimSpace(task.Result.Summary)
		}
		results = append(results, blackboard.VerificationResult{
			ClaimID:     claimID,
			Status:      status,
			Confidence:  confidence,
			EvidenceIDs: evidenceIDs,
			Rationale:   strings.TrimSpace(rationale),
		})
	}
	return results
}

func verifierFallbackEvidenceIDs(board *blackboard.State, task team.Task) []string {
	if board == nil {
		return nil
	}
	ids := make([]string, 0, len(task.Reads))
	for _, exchange := range verifierPublishedInputs(board, task) {
		if strings.TrimSpace(exchange.ID) == "" {
			continue
		}
		ids = appendUnique(ids, exchange.ID)
	}
	return ids
}

// verificationGateStatus derives the overall synthesis gate from claim-level
// verification results. Any contradicted claim short-circuits the whole gate
// to contradicted — we never let a supported sibling drown out a contradiction.
// Missing results degrade to insufficient rather than inferring from worker
// summary text, so a verifier that produces no structured evidence cannot
// implicitly approve synthesis.
func verificationGateStatus(_ team.Task, results []blackboard.VerificationResult) blackboard.VerificationStatus {
	if len(results) == 0 {
		return blackboard.VerificationStatusInsufficient
	}
	supported := 0
	required := 0
	for _, result := range results {
		if result.Status == blackboard.VerificationStatusContradicted {
			return blackboard.VerificationStatusContradicted
		}
		required++
		if result.SupportsClaim(blackboard.DefaultVerificationConfidence) {
			supported++
		}
	}
	if required > 0 && supported >= required {
		return blackboard.VerificationStatusSupported
	}
	return blackboard.VerificationStatusInsufficient
}

// structuredVerificationStatus reads the declared verifier decision from a
// structured claim payload. An absent or unrecognized decision degrades to
// insufficient — we never infer support from free-form worker text, because
// a verifier that cannot emit a machine-readable verdict has not produced
// evidence strong enough to gate synthesis.
func structuredVerificationStatus(payload map[string]any, _ string) blackboard.VerificationStatus {
	value := strings.ToLower(strings.TrimSpace(firstString(payload, "decision", "status")))
	switch value {
	case "supported", "pass", "passed", "approved":
		return blackboard.VerificationStatusSupported
	case "contradicted", "unsupported", "blocked", "rejected", "false":
		return blackboard.VerificationStatusContradicted
	default:
		return blackboard.VerificationStatusInsufficient
	}
}

func firstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, _ := payload[key].(string)
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstFloat(payload map[string]any, keys ...string) float64 {
	for _, key := range keys {
		switch value := payload[key].(type) {
		case float64:
			return value
		case float32:
			return float64(value)
		case int:
			return float64(value)
		}
	}
	return 0
}

func firstStringSlice(payload map[string]any, keys ...string) []string {
	for _, key := range keys {
		switch value := payload[key].(type) {
		case []string:
			return append([]string{}, value...)
		case []any:
			items := make([]string, 0, len(value))
			for _, item := range value {
				text, _ := item.(string)
				if strings.TrimSpace(text) != "" {
					items = append(items, text)
				}
			}
			if len(items) > 0 {
				return items
			}
		}
	}
	return nil
}
