package host

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

// ValidateDecision checks a supervisor decision against the digest it was
// made for and the current blackboard. The validator is the only gate
// between "supervisor output" and "control-plane state transition", so it
// treats every field as untrusted input.
//
// Errors intentionally do not attempt to repair — an invalid decision is
// rejected and re-planned, never auto-corrected — so a drifting
// supervisor can't chain small mistakes into a large one.
func ValidateDecision(decision team.SupervisorDecision, digest SupervisorDigest, board *blackboard.State) error {
	if decision.DigestSequence != digest.Sequence {
		return fmt.Errorf("decision targets digest #%d but current digest is #%d — stale decision",
			decision.DigestSequence, digest.Sequence)
	}
	switch decision.Kind {
	case team.DecisionGrantRun:
		return validateGrantRun(decision, digest)
	case team.DecisionRequestVerify:
		return validateRequestVerify(decision, board)
	case team.DecisionSynthesize:
		return validateSynthesize(decision, board)
	case "":
		return errors.New("decision kind is required")
	default:
		return fmt.Errorf("unsupported decision kind %q (v1 allows grant_run, request_verify, synthesize only)", decision.Kind)
	}
}

func validateGrantRun(decision team.SupervisorDecision, digest SupervisorDigest) error {
	if len(decision.Grants) == 0 {
		return errors.New("grant_run decisions must include at least one TaskRunGrant")
	}
	if decision.Verify != nil || decision.Synthesis != nil {
		return errors.New("grant_run decisions must not carry verify or synthesis payloads")
	}
	tasksByID := make(map[string]TaskDigest, len(digest.Tasks))
	for _, td := range digest.Tasks {
		tasksByID[td.ID] = td
	}
	seen := map[string]struct{}{}
	for _, grant := range decision.Grants {
		id := strings.TrimSpace(grant.TaskID)
		if id == "" {
			return errors.New("grant is missing taskId")
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("grant_run references task %s twice in the same decision", id)
		}
		seen[id] = struct{}{}
		task, ok := tasksByID[id]
		if !ok {
			return fmt.Errorf("grant_run references unknown task %s (not in digest)", id)
		}
		if task.Status != team.TaskStatusPending {
			return fmt.Errorf("grant_run refuses task %s: status is %s, not pending", id, task.Status)
		}
		if !task.Ready {
			return fmt.Errorf("grant_run refuses task %s: task has outstanding blockers %v", id, task.Blockers)
		}
	}
	return nil
}

func validateRequestVerify(decision team.SupervisorDecision, board *blackboard.State) error {
	if decision.Verify == nil {
		return errors.New("request_verify decisions must carry a Verify payload")
	}
	if len(decision.Grants) > 0 || decision.Synthesis != nil {
		return errors.New("request_verify decisions must not carry grant or synthesis payloads")
	}
	if len(decision.Verify.ClaimIDs) == 0 {
		return errors.New("request_verify must name at least one claim id")
	}
	if board == nil {
		return errors.New("request_verify cannot be validated without a blackboard")
	}
	claims := map[string]struct{}{}
	for _, claim := range board.Claims {
		claims[claim.ID] = struct{}{}
	}
	for _, claimID := range decision.Verify.ClaimIDs {
		trimmed := strings.TrimSpace(claimID)
		if trimmed == "" {
			return errors.New("request_verify contains an empty claim id")
		}
		if _, ok := claims[trimmed]; !ok {
			return fmt.Errorf("request_verify references unknown claim %s", trimmed)
		}
	}
	return nil
}

func validateSynthesize(decision team.SupervisorDecision, board *blackboard.State) error {
	if decision.Synthesis == nil {
		return errors.New("synthesize decisions must carry a Synthesis payload")
	}
	if len(decision.Grants) > 0 || decision.Verify != nil {
		return errors.New("synthesize decisions must not carry grant or verify payloads")
	}
	packet := decision.Synthesis.Packet
	if len(packet.FindingIDs) == 0 && len(packet.ClaimIDs) == 0 && len(packet.ExchangeIDs) == 0 {
		return errors.New("synthesis packet must reference at least one piece of evidence")
	}
	if board == nil {
		return errors.New("synthesize cannot be validated without a blackboard")
	}
	confidenceThreshold := blackboard.DefaultVerificationConfidence
	verificationByClaim := latestVerificationByClaim(board)

	findingIndex := map[string]blackboard.Finding{}
	for _, f := range board.Findings {
		findingIndex[f.ID] = f
	}
	for _, id := range packet.FindingIDs {
		finding, ok := findingIndex[id]
		if !ok {
			return fmt.Errorf("synthesis packet references unknown finding %s", id)
		}
		if !findingSupported(finding, verificationByClaim, confidenceThreshold) {
			return fmt.Errorf("synthesis packet references unsupported finding %s — every claim must pass verification", id)
		}
	}

	claimIndex := map[string]struct{}{}
	for _, c := range board.Claims {
		claimIndex[c.ID] = struct{}{}
	}
	for _, id := range packet.ClaimIDs {
		if _, ok := claimIndex[id]; !ok {
			return fmt.Errorf("synthesis packet references unknown claim %s", id)
		}
		result, hasResult := verificationByClaim[id]
		if !hasResult || !result.SupportsClaim(confidenceThreshold) {
			return fmt.Errorf("synthesis packet references unsupported claim %s", id)
		}
	}

	exchangeIndex := map[string]blackboard.Exchange{}
	for _, ex := range board.Exchanges {
		if ex.ID != "" {
			exchangeIndex[ex.ID] = ex
		}
	}
	for _, id := range packet.ExchangeIDs {
		ex, ok := exchangeIndex[id]
		if !ok {
			return fmt.Errorf("synthesis packet references unknown exchange %s", id)
		}
		if !exchangeBackedByVerifiedClaim(ex, verificationByClaim, confidenceThreshold) {
			return fmt.Errorf("synthesis packet references exchange %s whose backing claim is not verified", id)
		}
	}
	return nil
}

func exchangeBackedByVerifiedClaim(ex blackboard.Exchange, verifications map[string]blackboard.VerificationResult, threshold float64) bool {
	if len(ex.ClaimIDs) == 0 {
		return false
	}
	for _, claimID := range ex.ClaimIDs {
		result, ok := verifications[claimID]
		if !ok || !result.SupportsClaim(threshold) {
			return false
		}
	}
	return true
}
