package team

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ReportKind tags a typed worker report so consumers can dispatch by type
// without running reflection on a bare map[string]any. v1 lists the three
// worker roles recognized by the control plane; anything else is treated
// as free-form and does not satisfy the structured-report invariants.
type ReportKind string

const (
	ReportKindResearch     ReportKind = "research"
	ReportKindVerification ReportKind = "verification"
	ReportKindSynthesis    ReportKind = "synthesis"
)

// ReportKey is the structured-field key workers use to embed their typed
// report inside Result.Structured. Using a single well-known key keeps the
// guardrail lookup O(1) and avoids tripping on unrelated tool-result
// payloads that also happen to carry "findings"/"claims" fields.
const ReportKey = "report"

// ResearchReport is the data-plane contract for a research worker. A
// well-formed research worker publishes claims + evidence + findings
// together so the verifier can trace every claim back to its source. All
// three slices are optional individually, but a report that has zero
// claims AND zero findings is treated as empty — we do not synthesize
// defaults on the worker's behalf.
type ResearchReport struct {
	Kind       ReportKind        `json:"kind"`
	Claims     []ReportClaim     `json:"claims,omitempty"`
	Evidence   []ReportEvidence  `json:"evidence,omitempty"`
	Findings   []ReportFinding   `json:"findings,omitempty"`
	Confidence float64           `json:"confidence,omitempty"`
	Notes      string            `json:"notes,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type ReportClaim struct {
	ID          string   `json:"id,omitempty"`
	Summary     string   `json:"summary"`
	EvidenceIDs []string `json:"evidenceIds,omitempty"`
	Confidence  float64  `json:"confidence,omitempty"`
}

type ReportEvidence struct {
	ID      string  `json:"id,omitempty"`
	Source  string  `json:"source,omitempty"`
	Snippet string  `json:"snippet"`
	URL     string  `json:"url,omitempty"`
	Score   float64 `json:"score,omitempty"`
}

type ReportFinding struct {
	ID         string   `json:"id,omitempty"`
	Summary    string   `json:"summary"`
	ClaimIDs   []string `json:"claimIds,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
}

// VerificationReport is the data-plane contract for a verifier worker.
// Status is the overall verdict; PerClaim carries the decision for each
// claim the verifier actually adjudicated. A PerClaim entry without an
// explicit status is NOT inferred to pass — the guardrail rejects the
// report and the verifier must re-emit a fully-typed decision.
type VerificationReport struct {
	Kind       ReportKind          `json:"kind"`
	Status     VerificationStatus  `json:"status"`
	Confidence float64             `json:"confidence,omitempty"`
	PerClaim   []VerificationClaim `json:"perClaim,omitempty"`
	Reason     string              `json:"reason,omitempty"`
	Metadata   map[string]string   `json:"metadata,omitempty"`
}

type VerificationClaim struct {
	ClaimID     string             `json:"claimId"`
	Status      VerificationStatus `json:"status"`
	Confidence  float64            `json:"confidence,omitempty"`
	EvidenceIDs []string           `json:"evidenceIds,omitempty"`
	Reason      string             `json:"reason,omitempty"`
}

// VerificationStatus mirrors blackboard.VerificationStatus without
// importing it — reports are data-plane contracts living in team/, and
// the team package must stay free of internal/blackboard imports. The
// string values are identical so conversion at the boundary is lossless.
type VerificationStatus string

const (
	VerificationStatusSupported    VerificationStatus = "supported"
	VerificationStatusContradicted VerificationStatus = "contradicted"
	VerificationStatusInsufficient VerificationStatus = "insufficient"
)

// SynthesisReport is the data-plane contract for a synthesizer worker.
// Answer is the final prose; Citations must every correspond to an
// exchange/finding id the verifier already certified — the guardrail
// cross-checks this against the SynthesisPacket.
type SynthesisReport struct {
	Kind      ReportKind        `json:"kind"`
	Answer    string            `json:"answer"`
	Citations []ReportCitation  `json:"citations,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type ReportCitation struct {
	ExchangeID string `json:"exchangeId,omitempty"`
	FindingID  string `json:"findingId,omitempty"`
	ClaimID    string `json:"claimId,omitempty"`
	Excerpt    string `json:"excerpt,omitempty"`
}

// ExtractResearchReport pulls a typed ResearchReport out of a worker's
// Result.Structured. The return flag is false when the key is absent OR
// when the embedded kind disagrees — we refuse to coerce an arbitrary
// map into a research report.
func ExtractResearchReport(structured map[string]any) (ResearchReport, bool) {
	var report ResearchReport
	if !extractTypedReport(structured, ReportKindResearch, &report) {
		return ResearchReport{}, false
	}
	return report, true
}

// ExtractVerificationReport mirrors ExtractResearchReport for the verifier
// report. When the structured payload carries a "status" field at top
// level (legacy v0 output) we convert it into a single-entry
// VerificationReport so the call site still receives a typed value — but
// only if the status string matches a known enum value. Unknown values
// force a false return so the caller falls back to conservative
// inference.
func ExtractVerificationReport(structured map[string]any) (VerificationReport, bool) {
	var report VerificationReport
	if extractTypedReport(structured, ReportKindVerification, &report) {
		return report, true
	}
	if status, ok := readStatus(structured["status"]); ok {
		return VerificationReport{Kind: ReportKindVerification, Status: status}, true
	}
	return VerificationReport{}, false
}

// ExtractSynthesisReport mirrors ExtractResearchReport for the synthesis
// report.
func ExtractSynthesisReport(structured map[string]any) (SynthesisReport, bool) {
	var report SynthesisReport
	if !extractTypedReport(structured, ReportKindSynthesis, &report) {
		return SynthesisReport{}, false
	}
	return report, true
}

// ValidateResearchReport checks the structural invariants the control
// plane relies on. A research report must have at least one claim, and every
// finding.ClaimIDs entry must reference a claim in the same report —
// otherwise the claim-supports-finding chain is already broken at the worker
// boundary and we do not want to paper over that in the host layer.
func ValidateResearchReport(report ResearchReport) error {
	if report.Kind != ReportKindResearch {
		return fmt.Errorf("research report must have kind=%s, got %q", ReportKindResearch, report.Kind)
	}
	if len(report.Claims) == 0 {
		return fmt.Errorf("research report must include at least one claim")
	}
	claimIDs, err := validateResearchClaims(report.Claims)
	if err != nil {
		return err
	}
	if err := validateResearchEvidence(report.Evidence); err != nil {
		return err
	}
	return validateResearchFindings(report.Findings, claimIDs)
}

func validateResearchClaims(claims []ReportClaim) (map[string]struct{}, error) {
	claimIDs := map[string]struct{}{}
	for idx, claim := range claims {
		if strings.TrimSpace(claim.Summary) == "" {
			return nil, fmt.Errorf("research report claim[%d] missing summary", idx)
		}
		if err := recordUniqueReportID("claim", idx, claim.ID, claimIDs); err != nil {
			return nil, err
		}
	}
	return claimIDs, nil
}

func validateResearchEvidence(evidence []ReportEvidence) error {
	evidenceIDs := map[string]struct{}{}
	for idx, item := range evidence {
		if err := recordUniqueReportID("evidence", idx, item.ID, evidenceIDs); err != nil {
			return err
		}
	}
	return nil
}

func validateResearchFindings(findings []ReportFinding, claimIDs map[string]struct{}) error {
	findingIDs := map[string]struct{}{}
	for idx, finding := range findings {
		if strings.TrimSpace(finding.Summary) == "" {
			return fmt.Errorf("research report finding[%d] missing summary", idx)
		}
		if err := recordUniqueReportID("finding", idx, finding.ID, findingIDs); err != nil {
			return err
		}
		for _, claimID := range finding.ClaimIDs {
			if claimID == "" {
				return fmt.Errorf("research report finding[%d] references empty claim id", idx)
			}
			if _, ok := claimIDs[claimID]; !ok && len(claimIDs) > 0 {
				return fmt.Errorf("research report finding[%d] references unknown claim %s", idx, claimID)
			}
		}
	}
	return nil
}

func recordUniqueReportID(kind string, idx int, id string, seen map[string]struct{}) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	key := canonicalReportIDToken(id)
	if key == "" {
		key = id
	}
	if _, exists := seen[key]; exists {
		return fmt.Errorf("research report %s[%d] duplicates id %s", kind, idx, id)
	}
	seen[key] = struct{}{}
	return nil
}

func canonicalReportIDToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	lastDash := false
	for _, current := range value {
		if isReportIDTokenRune(current) {
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

func isReportIDTokenRune(current rune) bool {
	return current >= 'a' && current <= 'z' ||
		current >= 'A' && current <= 'Z' ||
		current >= '0' && current <= '9' ||
		current == '_' ||
		current == '-' ||
		current == '.'
}

// ValidateVerificationReport enforces that the overall status is a
// recognized enum value. Per-claim entries, if present, must also carry
// explicit status strings — a verifier that wants to stay silent on a
// claim must omit it rather than emit an empty status.
func ValidateVerificationReport(report VerificationReport) error {
	if report.Kind != ReportKindVerification {
		return fmt.Errorf("verification report must have kind=%s, got %q", ReportKindVerification, report.Kind)
	}
	if !isKnownStatus(report.Status) {
		return fmt.Errorf("verification report status %q is not a recognized enum", report.Status)
	}
	for idx, claim := range report.PerClaim {
		if strings.TrimSpace(claim.ClaimID) == "" {
			return fmt.Errorf("verification report perClaim[%d] missing claimId", idx)
		}
		if !isKnownStatus(claim.Status) {
			return fmt.Errorf("verification report perClaim[%d] status %q is not a recognized enum", idx, claim.Status)
		}
	}
	return nil
}

// ValidateSynthesisReport enforces a non-empty answer. Citations are
// optional (not every synthesizer will cite) — the control-plane
// cross-check against the SynthesisPacket lives in PR 7, not here, so
// this layer only rejects structurally broken reports.
func ValidateSynthesisReport(report SynthesisReport) error {
	if report.Kind != ReportKindSynthesis {
		return fmt.Errorf("synthesis report must have kind=%s, got %q", ReportKindSynthesis, report.Kind)
	}
	if strings.TrimSpace(report.Answer) == "" {
		return fmt.Errorf("synthesis report must include an answer")
	}
	return nil
}

// extractTypedReport is the common JSON-roundtrip decoder the Extract*
// helpers share. We go through JSON rather than a hand-rolled map walk so
// every field tag (omitempty, etc.) is respected exactly once and schema
// changes to the report types automatically flow through every consumer.
func extractTypedReport(structured map[string]any, kind ReportKind, dest any) bool {
	if len(structured) == 0 {
		return false
	}
	raw, ok := structured[ReportKey]
	if !ok {
		return false
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return false
	}
	return kindOf(dest) == kind
}

// kindOf inspects a decoded report struct and returns its Kind field.
// Used by extractTypedReport to verify the embedded kind matches the
// destination type — a ResearchReport pointer that decoded kind="verification"
// must not be accepted.
func kindOf(dest any) ReportKind {
	switch v := dest.(type) {
	case *ResearchReport:
		return v.Kind
	case *VerificationReport:
		return v.Kind
	case *SynthesisReport:
		return v.Kind
	default:
		return ""
	}
}

func readStatus(value any) (VerificationStatus, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	status := VerificationStatus(strings.ToLower(strings.TrimSpace(text)))
	if isKnownStatus(status) {
		return status, true
	}
	return "", false
}

func isKnownStatus(status VerificationStatus) bool {
	switch status {
	case VerificationStatusSupported, VerificationStatusContradicted, VerificationStatusInsufficient:
		return true
	default:
		return false
	}
}
