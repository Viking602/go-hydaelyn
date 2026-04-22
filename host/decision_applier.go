package host

import (
	"time"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

// ApplyDecision transforms a ControlState in response to a validated
// supervisor decision. It is pure: given the same inputs it produces the
// same output ControlState + event list. The caller is responsible for
// persisting events — we deliberately do not couple the applier to the
// storage layer so tests can exercise control-plane state transitions
// against in-memory fixtures.
//
// The returned events are ordered for replay: the SupervisorDecision
// event comes first, then any secondary events (TaskRunGranted per grant,
// SynthesisPacketBuilt for commit). Replay applying the same event chain
// must reconstruct the same ControlState.
func ApplyDecision(
	state team.ControlState,
	decision team.SupervisorDecision,
	digest SupervisorDigest,
	board *blackboard.State,
	runID, teamID string,
	now time.Time,
) (team.ControlState, []storage.Event, error) {
	if err := ValidateDecision(decision, digest, board); err != nil {
		return state, nil, err
	}
	next := cloneControlState(state)
	next.DecisionCount++
	next.LastDecision = &team.DecisionRecord{
		Kind:           decision.Kind,
		DigestSequence: decision.DigestSequence,
		AppliedAt:      now.UTC(),
		Rationale:      decision.Rationale,
	}

	events := []storage.Event{{
		RunID:   runID,
		TeamID:  teamID,
		Type:    storage.EventSupervisorDecision,
		Payload: decisionPayload(decision, next.DecisionCount),
	}}

	switch decision.Kind {
	case team.DecisionGrantRun:
		for _, grant := range decision.Grants {
			next.PendingGrants = appendUniqueGrant(next.PendingGrants, grant)
			events = append(events, storage.Event{
				RunID:   runID,
				TeamID:  teamID,
				TaskID:  grant.TaskID,
				Type:    storage.EventTaskRunGranted,
				Payload: grantPayload(grant, decision.DigestSequence, next.DecisionCount),
			})
		}
	case team.DecisionRequestVerify:
		// v1: the applier only records the decision + emits the event.
		// PR 5 will consume the event to inject / requeue verify tasks;
		// doing it here would couple the applier to the planner and
		// bypass the control-loop invariants we want to lock in PR 7.
	case team.DecisionSynthesize:
		packet := decision.Synthesis.Packet
		packetCopy := clonePacket(packet)
		next.Packet = &packetCopy
		events = append(events, storage.Event{
			RunID:   runID,
			TeamID:  teamID,
			Type:    storage.EventSynthesisPacketBuilt,
			Payload: packetPayload(packetCopy, decision.DigestSequence, next.DecisionCount),
		})
	}

	return next, events, nil
}

// BuildSynthesisPacket is the supervisor-side helper that picks the
// current set of Supported findings + their backing claims as a default
// synthesis bundle. Supervisors can still hand-craft a SynthesisPacket
// (e.g. restricting to a subset); this helper just provides the safe
// default so the common "synthesize everything verified" path doesn't
// reinvent the gate logic.
func BuildSynthesisPacket(board *blackboard.State, question string) team.SynthesisPacket {
	packet := team.SynthesisPacket{Question: question}
	if board == nil {
		return packet
	}
	verifications := latestVerificationByClaim(board)
	threshold := blackboard.DefaultVerificationConfidence

	for _, finding := range board.Findings {
		if !findingSupported(finding, verifications, threshold) {
			continue
		}
		packet.FindingIDs = append(packet.FindingIDs, finding.ID)
	}
	for _, claim := range board.Claims {
		result, ok := verifications[claim.ID]
		if !ok || !result.SupportsClaim(threshold) {
			continue
		}
		packet.ClaimIDs = append(packet.ClaimIDs, claim.ID)
	}
	for _, ex := range board.Exchanges {
		if ex.ID == "" {
			continue
		}
		if !exchangeBackedByVerifiedClaim(ex, verifications, threshold) {
			continue
		}
		packet.ExchangeIDs = append(packet.ExchangeIDs, ex.ID)
	}
	return packet
}

func cloneControlState(state team.ControlState) team.ControlState {
	next := state
	next.Cursor = state.Cursor.Clone()
	if len(state.PendingGrants) > 0 {
		next.PendingGrants = append([]team.TaskRunGrant{}, state.PendingGrants...)
	}
	if state.Packet != nil {
		packet := clonePacket(*state.Packet)
		next.Packet = &packet
	}
	if state.LastDecision != nil {
		record := *state.LastDecision
		next.LastDecision = &record
	}
	return next
}

func clonePacket(packet team.SynthesisPacket) team.SynthesisPacket {
	next := packet
	if len(packet.FindingIDs) > 0 {
		next.FindingIDs = append([]string{}, packet.FindingIDs...)
	}
	if len(packet.ClaimIDs) > 0 {
		next.ClaimIDs = append([]string{}, packet.ClaimIDs...)
	}
	if len(packet.ExchangeIDs) > 0 {
		next.ExchangeIDs = append([]string{}, packet.ExchangeIDs...)
	}
	return next
}

func appendUniqueGrant(existing []team.TaskRunGrant, grant team.TaskRunGrant) []team.TaskRunGrant {
	for idx, current := range existing {
		if current.TaskID == grant.TaskID {
			existing[idx] = grant
			return existing
		}
	}
	return append(existing, grant)
}

func decisionPayload(decision team.SupervisorDecision, decisionNumber int) map[string]any {
	payload := map[string]any{
		"kind":            string(decision.Kind),
		"digestSequence":  decision.DigestSequence,
		"decisionNumber":  decisionNumber,
		"rationale":       decision.Rationale,
	}
	switch decision.Kind {
	case team.DecisionGrantRun:
		grants := make([]map[string]any, 0, len(decision.Grants))
		for _, grant := range decision.Grants {
			grants = append(grants, map[string]any{
				"taskId":           grant.TaskID,
				"reason":           grant.Reason,
				"forceRefresh":     grant.ContextPolicy.ForceRefresh,
				"minExchangeIndex": grant.ContextPolicy.MinExchangeIndex,
			})
		}
		payload["grants"] = grants
	case team.DecisionRequestVerify:
		if decision.Verify != nil {
			payload["claimIds"] = decision.Verify.ClaimIDs
			payload["reason"] = decision.Verify.Reason
		}
	case team.DecisionSynthesize:
		if decision.Synthesis != nil {
			payload["packet"] = packetToMap(decision.Synthesis.Packet)
			payload["reason"] = decision.Synthesis.Reason
		}
	}
	return payload
}

func grantPayload(grant team.TaskRunGrant, digestSequence, decisionNumber int) map[string]any {
	return map[string]any{
		"taskId":           grant.TaskID,
		"reason":           grant.Reason,
		"forceRefresh":     grant.ContextPolicy.ForceRefresh,
		"minExchangeIndex": grant.ContextPolicy.MinExchangeIndex,
		"digestSequence":   digestSequence,
		"decisionNumber":   decisionNumber,
	}
}

func packetPayload(packet team.SynthesisPacket, digestSequence, decisionNumber int) map[string]any {
	return map[string]any{
		"packet":         packetToMap(packet),
		"digestSequence": digestSequence,
		"decisionNumber": decisionNumber,
	}
}

func packetToMap(packet team.SynthesisPacket) map[string]any {
	return map[string]any{
		"question":    packet.Question,
		"findingIds":  packet.FindingIDs,
		"claimIds":    packet.ClaimIDs,
		"exchangeIds": packet.ExchangeIDs,
	}
}
