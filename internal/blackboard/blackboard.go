package blackboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

type Source struct {
	ID       string            `json:"id"`
	TaskID   string            `json:"taskId,omitempty"`
	Title    string            `json:"title,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Artifact struct {
	ID       string            `json:"id"`
	TaskID   string            `json:"taskId,omitempty"`
	Name     string            `json:"name,omitempty"`
	Content  string            `json:"content,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type EvidenceInput struct {
	Source  string `json:"source,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

type Evidence struct {
	ID         string  `json:"id"`
	TaskID     string  `json:"taskId,omitempty"`
	SourceID   string  `json:"sourceId,omitempty"`
	ArtifactID string  `json:"artifactId,omitempty"`
	Summary    string  `json:"summary,omitempty"`
	Snippet    string  `json:"snippet,omitempty"`
	Score      float64 `json:"score,omitempty"`
}

type Claim struct {
	ID          string   `json:"id"`
	TaskID      string   `json:"taskId,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	EvidenceIDs []string `json:"evidenceIds,omitempty"`
	Confidence  float64  `json:"confidence,omitempty"`
}

type Finding struct {
	ID          string   `json:"id"`
	TaskID      string   `json:"taskId,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	ClaimIDs    []string `json:"claimIds,omitempty"`
	EvidenceIDs []string `json:"evidenceIds,omitempty"`
	Confidence  float64  `json:"confidence,omitempty"`
}

type VerificationStatus string

const (
	VerificationStatusSupported    VerificationStatus = "supported"
	VerificationStatusContradicted VerificationStatus = "contradicted"
	VerificationStatusInsufficient VerificationStatus = "insufficient"
	DefaultVerificationConfidence  float64            = 0.7
)

type VerificationResult struct {
	ClaimID     string             `json:"claimId"`
	Status      VerificationStatus `json:"status"`
	Confidence  float64            `json:"confidence,omitempty"`
	EvidenceIDs []string           `json:"evidenceIds,omitempty"`
	Rationale   string             `json:"rationale,omitempty"`
}

func (r VerificationResult) SupportsClaim(confidenceThreshold float64) bool {
	if confidenceThreshold <= 0 {
		confidenceThreshold = DefaultVerificationConfidence
	}
	return r.Status == VerificationStatusSupported &&
		r.Confidence >= confidenceThreshold &&
		len(r.EvidenceIDs) > 0
}

type ExchangeValueType string

const (
	ExchangeValueTypeText        ExchangeValueType = "text"
	ExchangeValueTypeJSON        ExchangeValueType = "json"
	ExchangeValueTypeArtifactRef ExchangeValueType = "artifact_ref"
	ExchangeValueTypeClaimRef    ExchangeValueType = "claim_ref"
	ExchangeValueTypeFindingRef  ExchangeValueType = "finding_ref"
)

type Exchange struct {
	ID          string            `json:"id"`
	Key         string            `json:"key"`
	Namespace   string            `json:"namespace,omitempty"`
	TaskID      string            `json:"taskId,omitempty"`
	Version     int               `json:"version,omitempty"`
	ETag        string            `json:"etag,omitempty"`
	ValueType   ExchangeValueType `json:"valueType,omitempty"`
	Text        string            `json:"text,omitempty"`
	Structured  map[string]any    `json:"structured,omitempty"`
	ArtifactIDs []string          `json:"artifactIds,omitempty"`
	ClaimIDs    []string          `json:"claimIds,omitempty"`
	FindingIDs  []string          `json:"findingIds,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

var ErrExchangeConflict = errors.New("blackboard exchange conflict")

type State struct {
	Sources       []Source             `json:"sources,omitempty"`
	Artifacts     []Artifact           `json:"artifacts,omitempty"`
	Evidence      []Evidence           `json:"evidence,omitempty"`
	Claims        []Claim              `json:"claims,omitempty"`
	Findings      []Finding            `json:"findings,omitempty"`
	Verifications []VerificationResult `json:"verifications,omitempty"`
	Exchanges     []Exchange           `json:"exchanges,omitempty"`
}

type PublishRequest struct {
	TaskID     string          `json:"taskId"`
	Title      string          `json:"title,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	Confidence float64         `json:"confidence,omitempty"`
	Evidence   []EvidenceInput `json:"evidence,omitempty"`
}

type PublishResult struct {
	ClaimID     string   `json:"claimId,omitempty"`
	FindingID   string   `json:"findingId,omitempty"`
	EvidenceIDs []string `json:"evidenceIds,omitempty"`
}

type Pipeline struct{}

func NewPipeline() Pipeline {
	return Pipeline{}
}

var piiPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`),                                // API keys
	regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),    // email
	regexp.MustCompile(`\b\d{3}[-.]?\d{2}[-.]?\d{4}\b`),                       // SSN
	regexp.MustCompile(`\b(?:\d[ -]*?){13,16}\b`),                             // credit card
	regexp.MustCompile(`\b\+?1?[-.\s]?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`), // US phone
}

func (Pipeline) Publish(state *State, request PublishRequest) PublishResult {
	if state == nil {
		state = &State{}
	}
	title := normalize(request.Title)
	summary := redact(normalize(request.Summary))
	sourceID := ensureSource(state, request.TaskID, title)
	artifactID := ensureArtifact(state, request.TaskID, title, summary)
	evidenceIDs := make([]string, 0, len(request.Evidence))
	for idx, item := range request.Evidence {
		snippet := redact(normalize(item.Snippet))
		if source := normalize(item.Source); source != "" {
			sourceID = ensureSource(state, request.TaskID, source)
		}
		evidenceID := ensureEvidence(state, request.TaskID, sourceID, artifactID, summary, snippet, score(request.Confidence))
		if evidenceID == "" {
			evidenceID = fmt.Sprintf("evidence-%s-fallback-%d", request.TaskID, idx+1)
		}
		evidenceIDs = append(evidenceIDs, evidenceID)
	}
	claimID := ensureClaim(state, request.TaskID, summary, evidenceIDs, score(request.Confidence))
	findingID := ensureFinding(state, request.TaskID, summary, []string{claimID}, evidenceIDs, score(request.Confidence))
	return PublishResult{
		ClaimID:     claimID,
		FindingID:   findingID,
		EvidenceIDs: evidenceIDs,
	}
}

func (s *State) UpsertVerification(result VerificationResult) {
	result.Rationale = normalize(result.Rationale)
	for idx := range s.Verifications {
		if s.Verifications[idx].ClaimID == result.ClaimID {
			s.Verifications[idx] = result
			return
		}
	}
	s.Verifications = append(s.Verifications, result)
}

func (s *State) UpsertExchange(exchange Exchange) Exchange {
	if s == nil {
		return Exchange{}
	}
	exchange = normalizeExchange(exchange)
	for idx := range s.Exchanges {
		if sameExchange(s.Exchanges[idx], exchange) {
			exchange.ID = s.Exchanges[idx].ID
			s.Exchanges[idx] = exchange
			return exchange
		}
	}
	return s.appendExchange(exchange)
}

func (s *State) UpsertExchangeCAS(exchange Exchange) (Exchange, error) {
	if s == nil {
		return Exchange{}, nil
	}
	exchange = normalizeExchange(exchange)
	for idx := range s.Exchanges {
		current := s.Exchanges[idx]
		if !sameExchangeSlot(current, exchange) {
			continue
		}
		if !exchangeUsesCAS(current) && !exchangeUsesCAS(exchange) {
			if sameExchange(current, exchange) {
				exchange.ID = current.ID
				s.Exchanges[idx] = exchange
				return exchange, nil
			}
			return s.appendExchange(exchange), nil
		}
		exchange.ETag = effectiveExchangeETag(exchange)
		currentETag := effectiveExchangeETag(current)
		currentVersion := normalizedExchangeVersion(current)
		nextVersion := normalizedExchangeVersion(exchange)
		switch {
		case nextVersion < currentVersion:
			return Exchange{}, fmt.Errorf("%w: stale exchange write for %q in namespace %q", ErrExchangeConflict, exchange.Key, exchange.Namespace)
		case nextVersion == currentVersion && currentETag != exchange.ETag:
			return Exchange{}, fmt.Errorf("%w: conflicting exchange write for %q in namespace %q", ErrExchangeConflict, exchange.Key, exchange.Namespace)
		}
		exchange.ID = current.ID
		s.Exchanges[idx] = exchange
		return exchange, nil
	}
	if exchangeUsesCAS(exchange) {
		exchange.ETag = effectiveExchangeETag(exchange)
	}
	return s.appendExchange(exchange), nil
}

func (s State) ClaimsForTask(taskID string) []Claim {
	items := make([]Claim, 0, len(s.Claims))
	for _, claim := range s.Claims {
		if claim.TaskID == taskID {
			items = append(items, claim)
		}
	}
	return items
}

func (s State) ExchangesForKey(key string) []Exchange {
	items := make([]Exchange, 0, len(s.Exchanges))
	for _, exchange := range s.Exchanges {
		if exchange.Key == key {
			items = append(items, cloneExchange(exchange))
		}
	}
	return items
}

func (s State) ExchangesForTask(taskID string) []Exchange {
	items := make([]Exchange, 0, len(s.Exchanges))
	for _, exchange := range s.Exchanges {
		if exchange.TaskID == taskID {
			items = append(items, cloneExchange(exchange))
		}
	}
	return items
}

func (s State) FindingsForClaim(claimID string) []Finding {
	items := make([]Finding, 0, len(s.Findings))
	for _, finding := range s.Findings {
		for _, current := range finding.ClaimIDs {
			if current == claimID {
				items = append(items, finding)
				break
			}
		}
	}
	return items
}

func (s State) SupportedFindings() []Finding {
	supported := map[string]struct{}{}
	for _, verification := range s.Verifications {
		if verification.SupportsClaim(DefaultVerificationConfidence) {
			supported[verification.ClaimID] = struct{}{}
		}
	}
	findings := make([]Finding, 0, len(s.Findings))
	for _, finding := range s.Findings {
		if len(finding.ClaimIDs) == 0 {
			continue
		}
		include := true
		for _, claimID := range finding.ClaimIDs {
			if _, ok := supported[claimID]; !ok {
				include = false
				break
			}
		}
		if include {
			findings = append(findings, finding)
		}
	}
	return findings
}

// InferVerificationStatus is the conservative, text-only fallback used
// when a verifier produced no structured VerificationReport. The default
// is deliberately Insufficient — prior to PR 6 this returned Supported
// for any text that lacked a negative keyword, which meant a verifier
// saying "I took a look" silently passed the gate. Now only explicit
// positive keywords (supported/approved/pass/verified/confirmed) upgrade
// the status; everything else defaults to Insufficient and the gate
// requires a structured report to actually pass.
func InferVerificationStatus(text string) VerificationStatus {
	current := strings.ToLower(text)
	switch {
	case strings.Contains(current, "contradict"), strings.Contains(current, "unsupported"), strings.Contains(current, "false"):
		return VerificationStatusContradicted
	case strings.Contains(current, "supported"), strings.Contains(current, "approved"), strings.Contains(current, "verified"), strings.Contains(current, "confirmed"), strings.Contains(current, "pass"):
		return VerificationStatusSupported
	default:
		return VerificationStatusInsufficient
	}
}

func ensureSource(state *State, taskID, title string) string {
	if title == "" {
		title = taskID
	}
	for _, source := range state.Sources {
		if source.TaskID == taskID && source.Title == title {
			return source.ID
		}
	}
	id := fmt.Sprintf("source-%s-%d", taskID, len(state.Sources)+1)
	state.Sources = append(state.Sources, Source{ID: id, TaskID: taskID, Title: title})
	return id
}

func ensureArtifact(state *State, taskID, title, content string) string {
	for _, artifact := range state.Artifacts {
		if artifact.TaskID == taskID && artifact.Content == content {
			return artifact.ID
		}
	}
	id := fmt.Sprintf("artifact-%s-%d", taskID, len(state.Artifacts)+1)
	state.Artifacts = append(state.Artifacts, Artifact{
		ID:      id,
		TaskID:  taskID,
		Name:    title,
		Content: content,
	})
	return id
}

func ensureEvidence(state *State, taskID, sourceID, artifactID, summary, snippet string, confidence float64) string {
	for _, evidence := range state.Evidence {
		if evidence.TaskID == taskID && evidence.Snippet == snippet {
			return evidence.ID
		}
	}
	id := fmt.Sprintf("evidence-%s-%d", taskID, len(state.Evidence)+1)
	state.Evidence = append(state.Evidence, Evidence{
		ID:         id,
		TaskID:     taskID,
		SourceID:   sourceID,
		ArtifactID: artifactID,
		Summary:    summary,
		Snippet:    snippet,
		Score:      confidence,
	})
	return id
}

func ensureClaim(state *State, taskID, summary string, evidenceIDs []string, confidence float64) string {
	for _, claim := range state.Claims {
		if claim.TaskID == taskID && claim.Summary == summary {
			return claim.ID
		}
	}
	id := fmt.Sprintf("claim-%s-%d", taskID, len(state.Claims)+1)
	state.Claims = append(state.Claims, Claim{
		ID:          id,
		TaskID:      taskID,
		Summary:     summary,
		EvidenceIDs: append([]string{}, evidenceIDs...),
		Confidence:  confidence,
	})
	return id
}

func ensureFinding(state *State, taskID, summary string, claimIDs, evidenceIDs []string, confidence float64) string {
	for _, finding := range state.Findings {
		if finding.TaskID == taskID && finding.Summary == summary {
			return finding.ID
		}
	}
	id := fmt.Sprintf("finding-%s-%d", taskID, len(state.Findings)+1)
	state.Findings = append(state.Findings, Finding{
		ID:          id,
		TaskID:      taskID,
		Summary:     summary,
		ClaimIDs:    append([]string{}, claimIDs...),
		EvidenceIDs: append([]string{}, evidenceIDs...),
		Confidence:  confidence,
	})
	return id
}

func normalize(value string) string {
	return strings.TrimSpace(value)
}

func redact(value string) string {
	if value == "" {
		return value
	}
	for _, pattern := range piiPatterns {
		value = pattern.ReplaceAllString(value, "[REDACTED]")
	}
	return value
}

func score(confidence float64) float64 {
	if confidence > 0 {
		return confidence
	}
	return 0.5
}

func sameExchange(left, right Exchange) bool {
	return left.Key == right.Key &&
		left.Namespace == right.Namespace &&
		left.TaskID == right.TaskID &&
		left.Version == right.Version &&
		left.ETag == right.ETag &&
		left.ValueType == right.ValueType &&
		left.Text == right.Text &&
		reflect.DeepEqual(left.Structured, right.Structured) &&
		reflect.DeepEqual(left.ArtifactIDs, right.ArtifactIDs) &&
		reflect.DeepEqual(left.ClaimIDs, right.ClaimIDs) &&
		reflect.DeepEqual(left.FindingIDs, right.FindingIDs) &&
		reflect.DeepEqual(left.Metadata, right.Metadata)
}

func sameExchangeSlot(left, right Exchange) bool {
	if left.Key != right.Key || left.Namespace != right.Namespace || left.TaskID != right.TaskID || left.ValueType != right.ValueType {
		return false
	}
	if left.Key != "supported_findings" {
		return true
	}
	return reflect.DeepEqual(left.ClaimIDs, right.ClaimIDs) &&
		reflect.DeepEqual(left.FindingIDs, right.FindingIDs)
}

func cloneExchange(exchange Exchange) Exchange {
	exchange.Structured = cloneStructuredMap(exchange.Structured)
	exchange.ArtifactIDs = append([]string{}, exchange.ArtifactIDs...)
	exchange.ClaimIDs = append([]string{}, exchange.ClaimIDs...)
	exchange.FindingIDs = append([]string{}, exchange.FindingIDs...)
	exchange.Metadata = cloneStringMap(exchange.Metadata)
	return exchange
}

func (s *State) appendExchange(exchange Exchange) Exchange {
	exchange.ID = fmt.Sprintf("exchange-%d", len(s.Exchanges)+1)
	s.Exchanges = append(s.Exchanges, exchange)
	return exchange
}

func normalizeExchange(exchange Exchange) Exchange {
	exchange.Key = normalize(exchange.Key)
	exchange.Namespace = normalize(exchange.Namespace)
	exchange.ETag = normalize(exchange.ETag)
	exchange.Text = normalize(exchange.Text)
	exchange.Metadata = cloneStringMap(exchange.Metadata)
	exchange.Structured = cloneStructuredMap(exchange.Structured)
	exchange.ArtifactIDs = append([]string{}, exchange.ArtifactIDs...)
	exchange.ClaimIDs = append([]string{}, exchange.ClaimIDs...)
	exchange.FindingIDs = append([]string{}, exchange.FindingIDs...)
	return exchange
}

func exchangeUsesCAS(exchange Exchange) bool {
	return exchange.Version > 0 || exchange.ETag != ""
}

func normalizedExchangeVersion(exchange Exchange) int {
	if exchange.Version > 0 {
		return exchange.Version
	}
	return 0
}

func effectiveExchangeETag(exchange Exchange) string {
	if exchange.ETag != "" {
		return exchange.ETag
	}
	payload, err := json.Marshal(struct {
		Key         string            `json:"key,omitempty"`
		Namespace   string            `json:"namespace,omitempty"`
		TaskID      string            `json:"taskId,omitempty"`
		ValueType   ExchangeValueType `json:"valueType,omitempty"`
		Text        string            `json:"text,omitempty"`
		Structured  map[string]any    `json:"structured,omitempty"`
		ArtifactIDs []string          `json:"artifactIds,omitempty"`
		ClaimIDs    []string          `json:"claimIds,omitempty"`
		FindingIDs  []string          `json:"findingIds,omitempty"`
		Metadata    map[string]string `json:"metadata,omitempty"`
	}{
		Key:         exchange.Key,
		Namespace:   exchange.Namespace,
		TaskID:      exchange.TaskID,
		ValueType:   exchange.ValueType,
		Text:        exchange.Text,
		Structured:  exchange.Structured,
		ArtifactIDs: exchange.ArtifactIDs,
		ClaimIDs:    exchange.ClaimIDs,
		FindingIDs:  exchange.FindingIDs,
		Metadata:    exchange.Metadata,
	})
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
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

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}
