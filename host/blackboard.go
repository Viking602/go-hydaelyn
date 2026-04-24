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
		if report, ok := team.ExtractResearchReport(task.Result.Structured); ok {
			var scopedReport team.ResearchReport
			claimIDs, findingIDs, scopedReport = publishResearchReport(state.Blackboard, task, report)
			task = taskWithStructuredReport(task, scopedReport)
		} else {
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
	state = replaceTaskByID(state, task)
	return state
}

func publishResearchReport(board *blackboard.State, task team.Task, report team.ResearchReport) ([]string, []string, team.ResearchReport) {
	scopedReport := team.ResearchReport{
		Kind:       report.Kind,
		Confidence: report.Confidence,
		Notes:      blackboard.SanitizeText(report.Notes),
		Metadata:   sanitizeStringMap(report.Metadata),
	}
	if board == nil {
		return nil, nil, scopedReport
	}
	evidenceIDs := make([]string, 0, len(report.Evidence))
	evidenceIDByReportID := map[string]string{}
	for idx, item := range report.Evidence {
		reportID := strings.TrimSpace(item.ID)
		id := scopedReportID("evidence", task.ID, idx+1, reportID)
		if reportID != "" {
			evidenceIDByReportID[reportID] = id
		}
		evidenceIDs = appendUnique(evidenceIDs, id)
		snippet := blackboard.SanitizeText(item.Snippet)
		scopedReport.Evidence = append(scopedReport.Evidence, team.ReportEvidence{
			ID:      id,
			Source:  blackboard.SanitizeText(item.Source),
			Snippet: snippet,
			URL:     blackboard.SanitizeText(item.URL),
			Score:   item.Score,
		})
		upsertEvidence(board, blackboard.Evidence{
			ID:       id,
			TaskID:   task.ID,
			SourceID: blackboard.SanitizeText(item.Source),
			Summary:  snippet,
			Snippet:  snippet,
			Score:    item.Score,
		})
	}
	claimIDs := make([]string, 0, len(report.Claims))
	claimIDByReportID := map[string]string{}
	for idx, item := range report.Claims {
		reportID := strings.TrimSpace(item.ID)
		id := scopedReportID("claim", task.ID, idx+1, reportID)
		if reportID != "" {
			claimIDByReportID[reportID] = id
		}
		claimEvidenceIDs := remapReportIDs(item.EvidenceIDs, evidenceIDByReportID)
		if len(claimEvidenceIDs) == 0 && len(evidenceIDs) > 0 {
			claimEvidenceIDs = append([]string{}, evidenceIDs...)
		}
		claimIDs = appendUnique(claimIDs, id)
		scopedReport.Claims = append(scopedReport.Claims, team.ReportClaim{
			ID:          id,
			Summary:     blackboard.SanitizeText(item.Summary),
			EvidenceIDs: append([]string{}, claimEvidenceIDs...),
			Confidence:  item.Confidence,
		})
		upsertClaim(board, blackboard.Claim{
			ID:          id,
			TaskID:      task.ID,
			Summary:     blackboard.SanitizeText(item.Summary),
			EvidenceIDs: claimEvidenceIDs,
			Confidence:  item.Confidence,
		})
	}
	findingIDs := make([]string, 0, len(report.Findings))
	for idx, item := range report.Findings {
		id := scopedReportID("finding", task.ID, idx+1, item.ID)
		itemClaimIDs := remapReportIDs(item.ClaimIDs, claimIDByReportID)
		if len(itemClaimIDs) == 0 && len(claimIDs) > 0 {
			itemClaimIDs = append([]string{}, claimIDs...)
		}
		findingIDs = appendUnique(findingIDs, id)
		scopedReport.Findings = append(scopedReport.Findings, team.ReportFinding{
			ID:         id,
			Summary:    blackboard.SanitizeText(item.Summary),
			ClaimIDs:   append([]string{}, itemClaimIDs...),
			Confidence: item.Confidence,
		})
		upsertFinding(board, blackboard.Finding{
			ID:          id,
			TaskID:      task.ID,
			Summary:     blackboard.SanitizeText(item.Summary),
			ClaimIDs:    itemClaimIDs,
			EvidenceIDs: append([]string{}, evidenceIDs...),
			Confidence:  item.Confidence,
		})
	}
	if len(findingIDs) == 0 {
		for idx, claimID := range claimIDs {
			summary := ""
			confidence := report.Confidence
			if idx < len(report.Claims) {
				summary = report.Claims[idx].Summary
				if report.Claims[idx].Confidence > 0 {
					confidence = report.Claims[idx].Confidence
				}
			}
			id := scopedReportID("finding", task.ID, idx+1, "")
			findingIDs = appendUnique(findingIDs, id)
			scopedReport.Findings = append(scopedReport.Findings, team.ReportFinding{
				ID:         id,
				Summary:    blackboard.SanitizeText(summary),
				ClaimIDs:   []string{claimID},
				Confidence: confidence,
			})
			upsertFinding(board, blackboard.Finding{
				ID:          id,
				TaskID:      task.ID,
				Summary:     blackboard.SanitizeText(summary),
				ClaimIDs:    []string{claimID},
				EvidenceIDs: append([]string{}, evidenceIDs...),
				Confidence:  confidence,
			})
		}
	}
	return claimIDs, findingIDs, scopedReport
}

func taskWithStructuredReport(task team.Task, report team.ResearchReport) team.Task {
	if task.Result == nil {
		return task
	}
	result := *task.Result
	result.Structured = cloneStructuredMap(result.Structured)
	result.Structured[team.ReportKey] = report
	task.Result = &result
	return task
}

func replaceTaskByID(state team.RunState, task team.Task) team.RunState {
	for idx := range state.Tasks {
		if state.Tasks[idx].ID == task.ID {
			state.Tasks[idx] = task
			return state
		}
	}
	return state
}

func scopedReportID(prefix, taskID string, idx int, reportID string) string {
	taskToken := reportIDToken(taskID)
	if taskToken == "" {
		taskToken = "task"
	}
	idToken := reportIDToken(reportID)
	if idToken == "" {
		return fmt.Sprintf("%s-%s-%d", prefix, taskToken, idx)
	}
	return fmt.Sprintf("%s-%s-%s", prefix, taskToken, idToken)
}

func reportIDToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	lastDash := false
	for _, current := range value {
		if isReportIDRune(current) {
			builder.WriteRune(current)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func isReportIDRune(current rune) bool {
	return current >= 'a' && current <= 'z' ||
		current >= 'A' && current <= 'Z' ||
		current >= '0' && current <= '9' ||
		current == '_' ||
		current == '-' ||
		current == '.'
}

func remapReportIDs(ids []string, mapping map[string]string) []string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if mapped, ok := mapping[id]; ok {
			items = appendUnique(items, mapped)
		}
	}
	return items
}

func upsertEvidence(board *blackboard.State, item blackboard.Evidence) {
	for idx := range board.Evidence {
		if board.Evidence[idx].ID == item.ID {
			board.Evidence[idx] = item
			return
		}
	}
	board.Evidence = append(board.Evidence, item)
}

func upsertClaim(board *blackboard.State, item blackboard.Claim) {
	for idx := range board.Claims {
		if board.Claims[idx].ID == item.ID {
			board.Claims[idx] = item
			return
		}
	}
	board.Claims = append(board.Claims, item)
}

func upsertFinding(board *blackboard.State, item blackboard.Finding) {
	for idx := range board.Findings {
		if board.Findings[idx].ID == item.ID {
			board.Findings[idx] = item
			return
		}
	}
	board.Findings = append(board.Findings, item)
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
	if typed, handled := typedVerificationResults(task.Result.Structured, claimsByID, fallbackEvidenceIDs, task); handled {
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
func typedVerificationResults(payload map[string]any, claimsByID map[string]blackboard.Claim, fallbackEvidenceIDs []string, task team.Task) ([]blackboard.VerificationResult, bool) {
	report, handled, valid := strictTypedVerificationReport(payload)
	if !handled {
		return nil, false
	}
	if !valid {
		return nil, true
	}
	perClaim := report.PerClaim
	results := make([]blackboard.VerificationResult, 0, len(perClaim))
	for _, claim := range perClaim {
		claimID := strings.TrimSpace(claim.ClaimID)
		if claimID == "" {
			continue
		}
		if _, ok := claimsByID[claimID]; !ok {
			return nil, true
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
	return results, true
}

func strictTypedVerificationReport(payload map[string]any) (team.VerificationReport, bool, bool) {
	rawReport, ok := payload[team.ReportKey]
	if !ok {
		return team.VerificationReport{}, false, false
	}
	report, ok := team.ExtractVerificationReport(map[string]any{team.ReportKey: rawReport})
	if !ok || team.ValidateVerificationReport(report) != nil || len(report.PerClaim) == 0 {
		return team.VerificationReport{}, true, false
	}
	return report, true, true
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
