package blackboard

import (
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
)

type VerificationResult struct {
	ClaimID     string             `json:"claimId"`
	Status      VerificationStatus `json:"status"`
	Confidence  float64            `json:"confidence,omitempty"`
	EvidenceIDs []string           `json:"evidenceIds,omitempty"`
	Rationale   string             `json:"rationale,omitempty"`
}

type State struct {
	Sources       []Source             `json:"sources,omitempty"`
	Artifacts     []Artifact           `json:"artifacts,omitempty"`
	Evidence      []Evidence           `json:"evidence,omitempty"`
	Claims        []Claim              `json:"claims,omitempty"`
	Findings      []Finding            `json:"findings,omitempty"`
	Verifications []VerificationResult `json:"verifications,omitempty"`
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
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`),                                       // API keys
	regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),           // email
	regexp.MustCompile(`\b\d{3}[-.]?\d{2}[-.]?\d{4}\b`),                               // SSN
	regexp.MustCompile(`\b(?:\d[ -]*?){13,16}\b`),                                      // credit card
	regexp.MustCompile(`\b\+?1?[-.\s]?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`),         // US phone
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
		sourceTitle := title
		if source := normalize(item.Source); source != "" {
			sourceTitle = source
			sourceID = ensureSource(state, request.TaskID, sourceTitle)
		}
		evidenceID := ensureEvidence(state, request.TaskID, sourceID, artifactID, summary, snippet, score(request.Confidence))
		if evidenceID == "" {
			evidenceID = "evidence-" + request.TaskID + "-" + string(rune('a'+idx))
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

func (s State) ClaimsForTask(taskID string) []Claim {
	items := make([]Claim, 0, len(s.Claims))
	for _, claim := range s.Claims {
		if claim.TaskID == taskID {
			items = append(items, claim)
		}
	}
	return items
}

func (s State) SupportedFindings() []Finding {
	supported := map[string]struct{}{}
	for _, verification := range s.Verifications {
		if verification.Status == VerificationStatusSupported {
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

func InferVerificationStatus(text string) VerificationStatus {
	current := strings.ToLower(text)
	switch {
	case strings.Contains(current, "contradict"), strings.Contains(current, "unsupported"), strings.Contains(current, "false"):
		return VerificationStatusContradicted
	case strings.Contains(current, "insufficient"), strings.Contains(current, "unclear"), strings.Contains(current, "unknown"):
		return VerificationStatusInsufficient
	default:
		return VerificationStatusSupported
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
	id := "source-" + taskID
	state.Sources = append(state.Sources, Source{ID: id, TaskID: taskID, Title: title})
	return id
}

func ensureArtifact(state *State, taskID, title, content string) string {
	for _, artifact := range state.Artifacts {
		if artifact.TaskID == taskID && artifact.Content == content {
			return artifact.ID
		}
	}
	id := "artifact-" + taskID
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
	id := "evidence-" + taskID + "-" + normalizeID(snippet)
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
	id := "claim-" + taskID
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
	id := "finding-" + taskID
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

func normalizeID(value string) string {
	value = strings.ToLower(normalize(value))
	value = strings.ReplaceAll(value, " ", "-")
	if value == "" {
		return "summary"
	}
	if len(value) > 24 {
		return value[:24]
	}
	return value
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
