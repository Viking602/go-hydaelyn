package memory

import (
	"maps"
	"slices"

	"github.com/Viking602/venat/api"
)

type State struct {
	Runs                  map[string]api.Run
	Tasks                 map[string]map[string]api.Task
	Events                map[string][]api.Event
	Blackboard            map[string][]api.BlackboardItem
	Envelopes             map[string]api.TaskEnvelope
	EnvelopesByRun        map[string][]string
	Messages              map[string]api.UserMessage
	MessagesByRun         map[string][]string
	TraceSpans            map[string][]api.TraceSpan
	Leases                map[string]api.TaskExecutionLease
	ActiveLeaseByTask     map[string]string
	Approvals             map[string]api.ApprovalRequest
	ResumeTokens          map[string]api.ResumeToken
	ActionAttempts        map[string]api.ActionAttempt
	AgentProfiles         map[string]api.AgentProfile
	Capabilities          map[string]api.Capability // key = name|agentID
	UsageRecords          map[string]api.UsageRecord
	DeadLetters           map[string]api.DeadLetterEntry
	Handoffs              map[string]api.HandoffRecord // key = runID|handoffID
	TeamStates            map[string]api.TeamStateRecord
	AgentInstances        map[string]api.AgentInstanceRecord
	AgentDefinitions      map[string]api.AgentDefinitionSnapshot
	AdmissionReservations map[string]api.AdmissionReservation
	ResourceClaims        map[string]api.ResourceClaim
	Seq                   map[string]uint64
	NextID                int
}

func NewState() *State {
	return &State{
		Runs:                  map[string]api.Run{},
		Tasks:                 map[string]map[string]api.Task{},
		Events:                map[string][]api.Event{},
		Blackboard:            map[string][]api.BlackboardItem{},
		Envelopes:             map[string]api.TaskEnvelope{},
		EnvelopesByRun:        map[string][]string{},
		Messages:              map[string]api.UserMessage{},
		MessagesByRun:         map[string][]string{},
		TraceSpans:            map[string][]api.TraceSpan{},
		Leases:                map[string]api.TaskExecutionLease{},
		ActiveLeaseByTask:     map[string]string{},
		Approvals:             map[string]api.ApprovalRequest{},
		ResumeTokens:          map[string]api.ResumeToken{},
		ActionAttempts:        map[string]api.ActionAttempt{},
		AgentProfiles:         map[string]api.AgentProfile{},
		Capabilities:          map[string]api.Capability{},
		UsageRecords:          map[string]api.UsageRecord{},
		DeadLetters:           map[string]api.DeadLetterEntry{},
		Handoffs:              map[string]api.HandoffRecord{},
		TeamStates:            map[string]api.TeamStateRecord{},
		AgentInstances:        map[string]api.AgentInstanceRecord{},
		AgentDefinitions:      map[string]api.AgentDefinitionSnapshot{},
		AdmissionReservations: map[string]api.AdmissionReservation{},
		ResourceClaims:        map[string]api.ResourceClaim{},
		Seq:                   map[string]uint64{},
	}
}

func (s *State) Clone() *State {
	if s == nil {
		return NewState()
	}
	clone := NewState()
	clone.Runs = make(map[string]api.Run, len(s.Runs))
	for id, run := range s.Runs {
		clone.Runs[id] = cloneRun(run)
	}
	clone.Tasks = make(map[string]map[string]api.Task, len(s.Tasks))
	for runID, tasks := range s.Tasks {
		cloned := make(map[string]api.Task, len(tasks))
		for taskID, task := range tasks {
			cloned[taskID] = cloneTask(task)
		}
		clone.Tasks[runID] = cloned
	}
	clone.Events = make(map[string][]api.Event, len(s.Events))
	for runID, events := range s.Events {
		cloned := make([]api.Event, len(events))
		for i, event := range events {
			cloned[i] = cloneEvent(event)
		}
		clone.Events[runID] = cloned
	}
	clone.Blackboard = make(map[string][]api.BlackboardItem, len(s.Blackboard))
	for runID, items := range s.Blackboard {
		cloned := make([]api.BlackboardItem, len(items))
		for i, item := range items {
			cloned[i] = cloneBlackboardItem(item)
		}
		clone.Blackboard[runID] = cloned
	}
	clone.Envelopes = make(map[string]api.TaskEnvelope, len(s.Envelopes))
	for id, envelope := range s.Envelopes {
		clone.Envelopes[id] = cloneEnvelope(envelope)
	}
	clone.EnvelopesByRun = cloneSliceMap(s.EnvelopesByRun)
	clone.Messages = maps.Clone(s.Messages)
	clone.MessagesByRun = cloneSliceMap(s.MessagesByRun)
	clone.TraceSpans = make(map[string][]api.TraceSpan, len(s.TraceSpans))
	for runID, spans := range s.TraceSpans {
		cloned := make([]api.TraceSpan, len(spans))
		for i, span := range spans {
			cloned[i] = cloneTraceSpan(span)
		}
		clone.TraceSpans[runID] = cloned
	}
	s.cloneGovernance(clone)
	s.cloneCatalog(clone)
	clone.Seq = maps.Clone(s.Seq)
	clone.NextID = s.NextID
	return clone
}

func (s *State) cloneGovernance(clone *State) {
	clone.Leases = maps.Clone(s.Leases)
	clone.ActiveLeaseByTask = maps.Clone(s.ActiveLeaseByTask)
	clone.Approvals = make(map[string]api.ApprovalRequest, len(s.Approvals))
	for id, approval := range s.Approvals {
		approval.Metadata = maps.Clone(approval.Metadata)
		clone.Approvals[id] = approval
	}
	clone.ResumeTokens = make(map[string]api.ResumeToken, len(s.ResumeTokens))
	for id, token := range s.ResumeTokens {
		token.Metadata = maps.Clone(token.Metadata)
		clone.ResumeTokens[id] = token
	}
	clone.ActionAttempts = make(map[string]api.ActionAttempt, len(s.ActionAttempts))
	for id, attempt := range s.ActionAttempts {
		clone.ActionAttempts[id] = cloneActionAttempt(attempt)
	}
	clone.UsageRecords = make(map[string]api.UsageRecord, len(s.UsageRecords))
	for id, record := range s.UsageRecords {
		record.Metadata = maps.Clone(record.Metadata)
		clone.UsageRecords[id] = record
	}
	clone.DeadLetters = make(map[string]api.DeadLetterEntry, len(s.DeadLetters))
	for id, entry := range s.DeadLetters {
		entry.Envelope = cloneEnvelope(entry.Envelope)
		entry.Payload = maps.Clone(entry.Payload)
		clone.DeadLetters[id] = entry
	}
}

func (s *State) cloneCatalog(clone *State) {
	clone.AgentProfiles = make(map[string]api.AgentProfile, len(s.AgentProfiles))
	for id, profile := range s.AgentProfiles {
		profile.Groups = slices.Clone(profile.Groups)
		profile.Metadata = maps.Clone(profile.Metadata)
		clone.AgentProfiles[id] = profile
	}
	clone.Capabilities = make(map[string]api.Capability, len(s.Capabilities))
	for id, capability := range s.Capabilities {
		capability.InputSchema = maps.Clone(capability.InputSchema)
		capability.OutputSchema = maps.Clone(capability.OutputSchema)
		capability.Tags = slices.Clone(capability.Tags)
		capability.Metadata = maps.Clone(capability.Metadata)
		clone.Capabilities[id] = capability
	}
	clone.Handoffs = make(map[string]api.HandoffRecord, len(s.Handoffs))
	for id, handoff := range s.Handoffs {
		handoff.Payload = append([]byte(nil), handoff.Payload...)
		handoff.EvidenceIDs = slices.Clone(handoff.EvidenceIDs)
		handoff.RequiredOutputSchema = append([]byte(nil), handoff.RequiredOutputSchema...)
		clone.Handoffs[id] = handoff
	}
	clone.TeamStates = make(map[string]api.TeamStateRecord, len(s.TeamStates))
	for id, state := range s.TeamStates {
		state.State = append([]byte(nil), state.State...)
		clone.TeamStates[id] = state
	}
	clone.AgentInstances = maps.Clone(s.AgentInstances)
	clone.AgentDefinitions = make(map[string]api.AgentDefinitionSnapshot, len(s.AgentDefinitions))
	for key, snapshot := range s.AgentDefinitions {
		snapshot.Definition = cloneAgentDefinition(snapshot.Definition)
		clone.AgentDefinitions[key] = snapshot
	}
	clone.AdmissionReservations = maps.Clone(s.AdmissionReservations)
	clone.ResourceClaims = maps.Clone(s.ResourceClaims)
}

func cloneAgentDefinition(definition api.AgentDefinition) api.AgentDefinition {
	definition.Skills = slices.Clone(definition.Skills)
	definition.AvailableSkills = slices.Clone(definition.AvailableSkills)
	definition.Tools = slices.Clone(definition.Tools)
	definition.InputSchema = append([]byte(nil), definition.InputSchema...)
	definition.OutputSchema = append([]byte(nil), definition.OutputSchema...)
	definition.Hooks = slices.Clone(definition.Hooks)
	definition.Capabilities = slices.Clone(definition.Capabilities)
	definition.Context = slices.Clone(definition.Context)
	for index := range definition.Context {
		definition.Context[index].Metadata = maps.Clone(definition.Context[index].Metadata)
	}
	definition.Triggers = slices.Clone(definition.Triggers)
	for index := range definition.Triggers {
		definition.Triggers[index].Config = maps.Clone(definition.Triggers[index].Config)
		definition.Triggers[index].Filter = maps.Clone(definition.Triggers[index].Filter)
	}
	definition.Governance.AllowedCapabilities = slices.Clone(definition.Governance.AllowedCapabilities)
	definition.Governance.DeniedCapabilities = slices.Clone(definition.Governance.DeniedCapabilities)
	definition.Governance.ApprovalRequiredFor = slices.Clone(definition.Governance.ApprovalRequiredFor)
	definition.Metadata = maps.Clone(definition.Metadata)
	return definition
}

func cloneSliceMap[V any](in map[string][]V) map[string][]V {
	out := make(map[string][]V, len(in))
	for key, value := range in {
		out[key] = slices.Clone(value)
	}
	return out
}

func cloneActionAttempt(attempt api.ActionAttempt) api.ActionAttempt {
	attempt.ToolResult = append([]byte(nil), attempt.ToolResult...)
	return attempt
}

func cloneRun(run api.Run) api.Run {
	run.Metadata = maps.Clone(run.Metadata)
	return run
}

func cloneEvent(event api.Event) api.Event {
	event.Payload = maps.Clone(event.Payload)
	return event
}

func cloneBlackboardItem(item api.BlackboardItem) api.BlackboardItem {
	item.EvidenceRefs = slices.Clone(item.EvidenceRefs)
	item.ArtifactRefs = slices.Clone(item.ArtifactRefs)
	return item
}

func cloneEnvelope(envelope api.TaskEnvelope) api.TaskEnvelope {
	envelope.Payload = maps.Clone(envelope.Payload)
	envelope.ReadSelectors = slices.Clone(envelope.ReadSelectors)
	envelope.WriteTargets = slices.Clone(envelope.WriteTargets)
	return envelope
}

func cloneTraceSpan(span api.TraceSpan) api.TraceSpan {
	span.Metadata = maps.Clone(span.Metadata)
	return span
}

func cloneTask(task api.Task) api.Task {
	task.Input = append([]byte(nil), task.Input...)
	task.OwnerHistory = slices.Clone(task.OwnerHistory)
	task.Tags = slices.Clone(task.Tags)
	task.CompletionCriteria = slices.Clone(task.CompletionCriteria)
	task.DependsOn = slices.Clone(task.DependsOn)
	task.ReadSelectors = slices.Clone(task.ReadSelectors)
	task.WriteTargets = slices.Clone(task.WriteTargets)
	task.PolicyDecisions = slices.Clone(task.PolicyDecisions)
	task.InputSchema = append([]byte(nil), task.InputSchema...)
	task.OutputSchema = append([]byte(nil), task.OutputSchema...)
	task.ResourceClaims = slices.Clone(task.ResourceClaims)
	if task.Budget != nil {
		budget := *task.Budget
		task.Budget = &budget
	}
	if task.Result != nil {
		result := *task.Result
		result.Structured = maps.Clone(result.Structured)
		if result.ActionOutcome != nil {
			outcome := *result.ActionOutcome
			outcome.ArtifactRefs = slices.Clone(outcome.ArtifactRefs)
			result.ActionOutcome = &outcome
		}
		if result.Handoff != nil {
			handoff := *result.Handoff
			handoff.ContextReferences = slices.Clone(handoff.ContextReferences)
			handoff.ContextSelectors = slices.Clone(handoff.ContextSelectors)
			handoff.Metadata = maps.Clone(handoff.Metadata)
			result.Handoff = &handoff
		}
		task.Result = &result
	}
	return task
}
