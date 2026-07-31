package memory

import (
	"maps"
	"slices"

	"github.com/Viking602/venat/internal/core/model"
)

type State struct {
	Runs              map[string]model.Run
	Tasks             map[string]map[string]model.Task
	Events            map[string][]model.Event
	Blackboard        map[string][]model.BlackboardItem
	Envelopes         map[string]model.TaskEnvelope
	EnvelopesByRun    map[string][]string
	Messages          map[string]model.UserMessage
	MessagesByRun     map[string][]string
	TraceSpans        map[string][]model.TraceSpan
	Leases            map[string]model.TaskExecutionLease
	ActiveLeaseByTask map[string]string
	Approvals         map[string]model.ApprovalRequest
	ResumeTokens      map[string]model.ResumeToken
	ActionAttempts    map[string]model.ActionAttempt
	AgentProfiles     map[string]model.AgentProfile
	Capabilities      map[string]model.Capability // key = name|agentID
	UsageRecords      map[string]model.UsageRecord
	DeadLetters       map[string]model.DeadLetterEntry
	Handoffs          map[string]model.HandoffRecord // key = runID|handoffID
	TeamStates        map[string]model.TeamStateRecord
	AgentInstances    map[string]model.AgentInstanceRecord
	Seq               map[string]int
	NextID            int
}

func NewState() *State {
	return &State{
		Runs:              map[string]model.Run{},
		Tasks:             map[string]map[string]model.Task{},
		Events:            map[string][]model.Event{},
		Blackboard:        map[string][]model.BlackboardItem{},
		Envelopes:         map[string]model.TaskEnvelope{},
		EnvelopesByRun:    map[string][]string{},
		Messages:          map[string]model.UserMessage{},
		MessagesByRun:     map[string][]string{},
		TraceSpans:        map[string][]model.TraceSpan{},
		Leases:            map[string]model.TaskExecutionLease{},
		ActiveLeaseByTask: map[string]string{},
		Approvals:         map[string]model.ApprovalRequest{},
		ResumeTokens:      map[string]model.ResumeToken{},
		ActionAttempts:    map[string]model.ActionAttempt{},
		AgentProfiles:     map[string]model.AgentProfile{},
		Capabilities:      map[string]model.Capability{},
		UsageRecords:      map[string]model.UsageRecord{},
		DeadLetters:       map[string]model.DeadLetterEntry{},
		Handoffs:          map[string]model.HandoffRecord{},
		TeamStates:        map[string]model.TeamStateRecord{},
		AgentInstances:    map[string]model.AgentInstanceRecord{},
		Seq:               map[string]int{},
	}
}

func (s *State) Clone() *State {
	if s == nil {
		return NewState()
	}
	clone := NewState()
	clone.Runs = maps.Clone(s.Runs)
	clone.Tasks = cloneNestedMap(s.Tasks)
	clone.Events = cloneSliceMap(s.Events)
	clone.Blackboard = cloneSliceMap(s.Blackboard)
	clone.Envelopes = maps.Clone(s.Envelopes)
	clone.EnvelopesByRun = cloneSliceMap(s.EnvelopesByRun)
	clone.Messages = maps.Clone(s.Messages)
	clone.MessagesByRun = cloneSliceMap(s.MessagesByRun)
	clone.TraceSpans = cloneSliceMap(s.TraceSpans)
	clone.Leases = maps.Clone(s.Leases)
	clone.ActiveLeaseByTask = maps.Clone(s.ActiveLeaseByTask)
	clone.Approvals = maps.Clone(s.Approvals)
	clone.ResumeTokens = maps.Clone(s.ResumeTokens)
	clone.ActionAttempts = make(map[string]model.ActionAttempt, len(s.ActionAttempts))
	for id, attempt := range s.ActionAttempts {
		clone.ActionAttempts[id] = cloneActionAttempt(attempt)
	}
	clone.AgentProfiles = maps.Clone(s.AgentProfiles)
	clone.Capabilities = maps.Clone(s.Capabilities)
	clone.UsageRecords = maps.Clone(s.UsageRecords)
	clone.DeadLetters = maps.Clone(s.DeadLetters)
	clone.Handoffs = maps.Clone(s.Handoffs)
	clone.TeamStates = maps.Clone(s.TeamStates)
	clone.AgentInstances = maps.Clone(s.AgentInstances)
	clone.Seq = maps.Clone(s.Seq)
	clone.NextID = s.NextID
	return clone
}

func cloneNestedMap[V any](in map[string]map[string]V) map[string]map[string]V {
	out := make(map[string]map[string]V, len(in))
	for key, value := range in {
		out[key] = maps.Clone(value)
	}
	return out
}

func cloneSliceMap[V any](in map[string][]V) map[string][]V {
	out := make(map[string][]V, len(in))
	for key, value := range in {
		out[key] = slices.Clone(value)
	}
	return out
}

func cloneActionAttempt(attempt model.ActionAttempt) model.ActionAttempt {
	attempt.ToolResult = append([]byte(nil), attempt.ToolResult...)
	return attempt
}
