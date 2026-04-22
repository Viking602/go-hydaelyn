package host

import (
	"time"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

// BuildSupervisorDigest produces a SupervisorDigest from a RunState and the
// supervisor's prior cursor. The function is pure — no side effects, no
// storage writes — so tests can call it against fixtures and replays can
// regenerate byte-identical digests from the same (state, cursor, now).
//
// The returned cursor advances past the data the digest captured:
// ExchangeIndex, VerificationIndex, FindingIndex all jump to the current
// blackboard length; TaskVersions records every task's current Version so
// the next tick can diff re-runs. Callers persist the new cursor on
// ControlState and pass it back on the next call.
func BuildSupervisorDigest(state team.RunState, cursor team.SupervisorCursor, now time.Time) (SupervisorDigest, team.SupervisorCursor) {
	normalized := state
	normalized.Normalize()

	sequence := 0
	if normalized.Control != nil {
		sequence = normalized.Control.DigestCount + 1
	} else {
		sequence = 1
	}

	digest := SupervisorDigest{
		RunID:      normalized.ID,
		TeamID:     normalized.ID,
		Sequence:   sequence,
		ObservedAt: now.UTC(),
		Phase:      normalized.Phase,
		Status:     normalized.Status,
	}

	candidates := normalized.RunnableCandidates()
	readinessByID := make(map[string]team.RunnableCandidate, len(candidates))
	for _, candidate := range candidates {
		readinessByID[candidate.Task.ID] = candidate
	}

	budget := BudgetDigest{}
	taskDigests := make([]TaskDigest, 0, len(normalized.Tasks))
	pendingReads := make([]PendingReadDigest, 0)
	for _, task := range normalized.Tasks {
		switch task.Status {
		case team.TaskStatusCompleted:
			budget.TasksCompleted++
		case team.TaskStatusFailed, team.TaskStatusAborted:
			budget.TasksFailed++
		case team.TaskStatusPending:
			budget.TasksPending++
		case team.TaskStatusRunning:
			budget.TasksRunning++
		}
		if task.Result != nil {
			budget.TokensUsed += task.Result.Usage.TotalTokens
			budget.ToolCallsUsed += task.Result.ToolCallCount
		}

		td := TaskDigest{
			ID:        task.ID,
			Kind:      task.Kind,
			Stage:     task.Stage,
			Status:    task.Status,
			Assignee:  task.EffectiveAssigneeAgentID(),
			DependsOn: append([]string{}, task.DependsOn...),
			Attempts:  task.Attempts,
			Error:     task.Error,
		}
		if task.Result != nil {
			td.Confidence = task.Result.Confidence
		}
		if candidate, ok := readinessByID[task.ID]; ok {
			td.Ready = candidate.Ready
			td.Blockers = append([]team.Blocker{}, candidate.Blockers...)
			for _, blocker := range candidate.Blockers {
				if blocker.Kind != team.BlockerReadMissing {
					continue
				}
				pendingReads = append(pendingReads, PendingReadDigest{
					TaskID: task.ID,
					Target: blocker.Target,
					Reason: blocker.Reason,
				})
			}
		}
		taskDigests = append(taskDigests, td)
	}
	digest.Tasks = taskDigests
	if len(pendingReads) > 0 {
		digest.PendingReads = pendingReads
	}
	digest.Budget = budget

	nextCursor := cursor.Clone()
	if nextCursor.TaskVersions == nil {
		nextCursor.TaskVersions = map[string]int{}
	}
	for _, task := range normalized.Tasks {
		nextCursor.TaskVersions[task.ID] = task.Version
	}

	if normalized.Blackboard != nil {
		board := normalized.Blackboard
		confidenceThreshold := blackboard.DefaultVerificationConfidence
		verificationByClaim := latestVerificationByClaim(board)

		if cursor.FindingIndex < len(board.Findings) {
			recent := board.Findings[cursor.FindingIndex:]
			findings := make([]FindingDigest, 0, len(recent))
			for _, finding := range recent {
				findings = append(findings, FindingDigest{
					ID:         finding.ID,
					TaskID:     finding.TaskID,
					Summary:    finding.Summary,
					ClaimIDs:   append([]string{}, finding.ClaimIDs...),
					Supported:  findingSupported(finding, verificationByClaim, confidenceThreshold),
					Confidence: finding.Confidence,
				})
			}
			digest.RecentFindings = findings
		}

		if cursor.VerificationIndex < len(board.Verifications) || len(board.Claims) > 0 {
			claims := make([]ClaimDigest, 0, len(board.Claims))
			for _, claim := range board.Claims {
				result, hasResult := verificationByClaim[claim.ID]
				cd := ClaimDigest{
					ID:         claim.ID,
					TaskID:     claim.TaskID,
					Summary:    claim.Summary,
					Confidence: claim.Confidence,
				}
				if hasResult {
					cd.Supported = result.SupportsClaim(confidenceThreshold)
					cd.VerificationStatus = result.Status
					if result.Confidence > 0 {
						cd.Confidence = result.Confidence
					}
				}
				claims = append(claims, cd)
			}
			digest.RecentClaims = claims
		}

		digest.Conflicts = board.DetectConflicts()

		nextCursor.ExchangeIndex = len(board.Exchanges)
		nextCursor.VerificationIndex = len(board.Verifications)
		nextCursor.FindingIndex = len(board.Findings)
	}

	digest.Cursor = nextCursor
	return digest, nextCursor
}

// latestVerificationByClaim collapses the verifications slice (which is
// upserted by ClaimID) into a lookup so each claim's digest reflects its
// most recent verification outcome without an O(N×M) scan.
func latestVerificationByClaim(board *blackboard.State) map[string]blackboard.VerificationResult {
	out := map[string]blackboard.VerificationResult{}
	if board == nil {
		return out
	}
	for _, result := range board.Verifications {
		out[result.ClaimID] = result
	}
	return out
}

// findingSupported returns true only if every backing claim has a
// SupportsClaim-qualifying verification — matching the contract
// SelectFindings enforces, so the supervisor and the selector pipeline
// never disagree on what counts as verified.
func findingSupported(finding blackboard.Finding, verifications map[string]blackboard.VerificationResult, threshold float64) bool {
	if len(finding.ClaimIDs) == 0 {
		return false
	}
	for _, claimID := range finding.ClaimIDs {
		result, ok := verifications[claimID]
		if !ok {
			return false
		}
		if !result.SupportsClaim(threshold) {
			return false
		}
	}
	return true
}
