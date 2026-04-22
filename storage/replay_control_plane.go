package storage

import (
	"fmt"

	"github.com/Viking602/go-hydaelyn/team"
)

// validateControlPlaneChain enforces the Supervisor-Controlled Runtime
// happens-before chain:
//
//	SupervisorObserved(seq=S) → SupervisorDecision(seq=S, decision=D) → TaskRunGranted(seq=S, decision=D)
//
// Every grant must tie back to a decision, every decision to an observation.
// A grant that drops into the log without the two priors is either a replay
// whose control cursor is out of sync OR a bug where the runtime emitted
// grants without going through observeAndDecide — both are bad, both
// surface here as ReplayMismatchWrongOrder.
func validateControlPlaneChain(events []Event) []ReplayMismatch {
	observedSequences := map[int]int{}
	type decisionKey struct {
		sequence int
		number   int
	}
	decisionIndex := map[decisionKey]int{}
	grantedTasks := map[decisionKey]map[string]struct{}{}
	mismatches := make([]ReplayMismatch, 0)

	for idx, event := range events {
		switch event.Type {
		case EventSupervisorObserved:
			seq := controlPlaneInt(event.Payload, "sequence")
			if _, ok := observedSequences[seq]; !ok {
				observedSequences[seq] = idx
			}
		case EventSupervisorDecision:
			seq := controlPlaneInt(event.Payload, "digestSequence")
			number := controlPlaneInt(event.Payload, "decisionNumber")
			current := event
			observedIdx, ok := observedSequences[seq]
			if !ok || observedIdx >= idx {
				mismatches = append(mismatches, ReplayMismatch{
					Type:     ReplayMismatchWrongOrder,
					Event:    &current,
					Expected: string(EventSupervisorObserved),
					Actual:   string(EventSupervisorDecision),
					Message:  fmt.Sprintf("SupervisorDecision digestSequence=%d has no preceding SupervisorObserved", seq),
				})
				continue
			}
			key := decisionKey{sequence: seq, number: number}
			if _, seen := decisionIndex[key]; !seen {
				decisionIndex[key] = idx
			}
			kind := controlPlaneString(event.Payload, "kind")
			if kind == string(team.DecisionGrantRun) {
				taskIDs, _ := grantedTasks[key]
				if taskIDs == nil {
					taskIDs = map[string]struct{}{}
				}
				for _, id := range controlPlaneGrantTaskIDs(event.Payload) {
					taskIDs[id] = struct{}{}
				}
				grantedTasks[key] = taskIDs
			}
		case EventTaskRunGranted:
			seq := controlPlaneInt(event.Payload, "digestSequence")
			number := controlPlaneInt(event.Payload, "decisionNumber")
			taskID := event.TaskID
			if taskID == "" {
				taskID = controlPlaneString(event.Payload, "taskId")
			}
			current := event
			key := decisionKey{sequence: seq, number: number}
			decisionIdx, ok := decisionIndex[key]
			if !ok || decisionIdx >= idx {
				mismatches = append(mismatches, ReplayMismatch{
					Type:     ReplayMismatchWrongOrder,
					Event:    &current,
					Expected: string(EventSupervisorDecision),
					Actual:   string(EventTaskRunGranted),
					Message:  fmt.Sprintf("TaskRunGranted for task %s (digestSequence=%d, decisionNumber=%d) has no preceding SupervisorDecision", taskID, seq, number),
				})
				continue
			}
			if taskID == "" {
				mismatches = append(mismatches, ReplayMismatch{
					Type:    ReplayMismatchSemanticInconsistency,
					Event:   &current,
					Actual:  "missing taskId",
					Message: "TaskRunGranted must carry a taskId to trace back to its SupervisorDecision",
				})
				continue
			}
			if _, listed := grantedTasks[key][taskID]; !listed {
				mismatches = append(mismatches, ReplayMismatch{
					Type:     ReplayMismatchSemanticInconsistency,
					Event:    &current,
					Expected: "task listed in decision grants",
					Actual:   taskID,
					Message:  fmt.Sprintf("TaskRunGranted for task %s was not declared in SupervisorDecision (seq=%d, decision=%d)", taskID, seq, number),
				})
			}
		}
	}
	return mismatches
}

// validateSynthesisChain enforces:
//
//  1. Every SynthesisCommitted has a preceding SynthesisPacketBuilt. A
//     synthesizer that commits without the supervisor first publishing a
//     packet has bypassed the control plane — the final answer was not
//     grounded in the agreed evidence bundle.
//  2. If the authoritative state's final Result carries a typed
//     SynthesisReport with citations, every citation must reference an
//     ID listed in the most recent SynthesisPacketBuilt event's packet.
//     This closes the loop on PR 6's typed reports: the synthesizer
//     cannot smuggle citations that were never part of the verified
//     packet.
func validateSynthesisChain(events []Event, state TeamState) []ReplayMismatch {
	mismatches := make([]ReplayMismatch, 0)
	// The SynthesisCommitted→SynthesisPacketBuilt contract is a control-plane
	// invariant. In Legacy mode no supervisor is running, so no packet is ever
	// emitted — enforcing the ordering there would falsely flag every run
	// that never opted into the control plane. We detect engagement via
	// SupervisorObserved (the control loop's anchor event): if it never fires,
	// the run was legacy and the packet contract does not apply.
	controlPlaneEngaged := false
	for _, event := range events {
		if event.Type == EventSupervisorObserved {
			controlPlaneEngaged = true
			break
		}
	}
	var lastPacket map[string]struct{}
	var lastPacketPayload map[string]any
	for _, event := range events {
		switch event.Type {
		case EventSynthesisPacketBuilt:
			lastPacketPayload = event.Payload
			lastPacket = packetIDSet(event.Payload)
		case EventSynthesisCommitted:
			if !controlPlaneEngaged {
				continue
			}
			current := event
			if lastPacket == nil {
				mismatches = append(mismatches, ReplayMismatch{
					Type:     ReplayMismatchWrongOrder,
					Event:    &current,
					Expected: string(EventSynthesisPacketBuilt),
					Actual:   string(EventSynthesisCommitted),
					Message:  "SynthesisCommitted has no preceding SynthesisPacketBuilt — final answer is ungrounded",
				})
			}
		}
	}

	if state.Result == nil || len(state.Result.Structured) == 0 {
		return mismatches
	}
	report, ok := team.ExtractSynthesisReport(state.Result.Structured)
	if !ok || len(report.Citations) == 0 {
		return mismatches
	}
	if !controlPlaneEngaged {
		// Legacy run: citations are grounded against the state's own
		// findings/claims/exchanges, not against a packet the supervisor
		// never built. Skip packet-membership enforcement.
		return mismatches
	}
	if lastPacket == nil {
		mismatches = append(mismatches, ReplayMismatch{
			Type:    ReplayMismatchSemanticInconsistency,
			Actual:  "citations without packet",
			Message: "final SynthesisReport carries citations but no SynthesisPacketBuilt event exists",
		})
		return mismatches
	}
	for idx, citation := range report.Citations {
		for _, id := range citationReferencedIDs(citation) {
			if _, ok := lastPacket[id]; ok {
				continue
			}
			mismatches = append(mismatches, ReplayMismatch{
				Type:     ReplayMismatchSemanticInconsistency,
				Expected: fmt.Sprintf("citation id present in packet (exchange/finding/claim ids: %v)", lastPacketPayload),
				Actual:   fmt.Sprintf("citation[%d]=%s", idx, id),
				Message:  fmt.Sprintf("synthesis citation %s was not included in SynthesisPacketBuilt packet", id),
			})
		}
	}
	return mismatches
}

func packetIDSet(payload map[string]any) map[string]struct{} {
	set := map[string]struct{}{}
	packet, ok := payload["packet"].(map[string]any)
	if !ok {
		return set
	}
	for _, key := range []string{"findingIds", "claimIds", "exchangeIds"} {
		for _, id := range controlPlaneStringSlice(packet[key]) {
			set[id] = struct{}{}
		}
	}
	return set
}

func citationReferencedIDs(citation team.ReportCitation) []string {
	ids := make([]string, 0, 3)
	if citation.ExchangeID != "" {
		ids = append(ids, citation.ExchangeID)
	}
	if citation.FindingID != "" {
		ids = append(ids, citation.FindingID)
	}
	if citation.ClaimID != "" {
		ids = append(ids, citation.ClaimID)
	}
	return ids
}

func controlPlaneInt(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	return intValue(payload[key])
}

func controlPlaneString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	return stringValue(payload[key])
}

func controlPlaneStringSlice(value any) []string {
	switch current := value.(type) {
	case []string:
		return append([]string{}, current...)
	case []any:
		out := make([]string, 0, len(current))
		for _, item := range current {
			if text, ok := item.(string); ok && text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func controlPlaneGrantTaskIDs(payload map[string]any) []string {
	raw, ok := payload["grants"].([]any)
	if !ok {
		// Supervisor decision payload uses []map[string]any before any
		// JSON round-trip happens — handle both shapes so we don't miss
		// in-memory replays.
		typed, ok2 := payload["grants"].([]map[string]any)
		if !ok2 {
			return nil
		}
		ids := make([]string, 0, len(typed))
		for _, entry := range typed {
			if id, ok := entry["taskId"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	}
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := entry["taskId"].(string); ok && id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
