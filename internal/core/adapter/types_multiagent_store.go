package adapter

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/model"
)

func HandoffRecordToModel(in api.HandoffRecord) model.HandoffRecord {
	return model.HandoffRecord{
		ID:                   in.ID,
		RunID:                in.RunID,
		From:                 in.From,
		To:                   in.To,
		Reason:               in.Reason,
		Payload:              cloneBytes(in.Payload),
		EvidenceIDs:          cloneStrings(in.EvidenceIDs),
		RequiredOutputSchema: cloneBytes(in.RequiredOutputSchema),
		CreatedAt:            in.CreatedAt,
	}
}

func HandoffRecordFromModel(in model.HandoffRecord) api.HandoffRecord {
	return api.HandoffRecord{
		ID:                   in.ID,
		RunID:                in.RunID,
		From:                 in.From,
		To:                   in.To,
		Reason:               in.Reason,
		Payload:              cloneBytes(in.Payload),
		EvidenceIDs:          cloneStrings(in.EvidenceIDs),
		RequiredOutputSchema: cloneBytes(in.RequiredOutputSchema),
		CreatedAt:            in.CreatedAt,
	}
}

func HandoffRecordsFromModel(in []model.HandoffRecord) []api.HandoffRecord {
	if in == nil {
		return nil
	}
	out := make([]api.HandoffRecord, 0, len(in))
	for _, record := range in {
		out = append(out, HandoffRecordFromModel(record))
	}
	return out
}

func HandoffRecordsToModel(in []api.HandoffRecord) []model.HandoffRecord {
	if in == nil {
		return nil
	}
	out := make([]model.HandoffRecord, 0, len(in))
	for _, record := range in {
		out = append(out, HandoffRecordToModel(record))
	}
	return out
}

func HandoffSelectorToModel(in api.HandoffSelector) model.HandoffSelector {
	return model.HandoffSelector{RunID: in.RunID, From: in.From, To: in.To, Since: in.Since}
}

func HandoffSelectorFromModel(in model.HandoffSelector) api.HandoffSelector {
	return api.HandoffSelector{RunID: in.RunID, From: in.From, To: in.To, Since: in.Since}
}

func TeamStateRecordToModel(in api.TeamStateRecord) model.TeamStateRecord {
	return model.TeamStateRecord{RunID: in.RunID, Tick: in.Tick, State: cloneBytes(in.State), UpdatedAt: in.UpdatedAt}
}

func TeamStateRecordFromModel(in model.TeamStateRecord) api.TeamStateRecord {
	return api.TeamStateRecord{RunID: in.RunID, Tick: in.Tick, State: cloneBytes(in.State), UpdatedAt: in.UpdatedAt}
}

func AgentInstanceRecordToModel(in api.AgentInstanceRecord) model.AgentInstanceRecord {
	return model.AgentInstanceRecord{
		ID:        in.ID,
		ClassName: in.ClassName,
		RunID:     in.RunID,
		TaskID:    in.TaskID,
		State:     in.State,
		CreatedAt: in.CreatedAt,
	}
}

func AgentInstanceRecordFromModel(in model.AgentInstanceRecord) api.AgentInstanceRecord {
	return api.AgentInstanceRecord{
		ID:        in.ID,
		ClassName: in.ClassName,
		RunID:     in.RunID,
		TaskID:    in.TaskID,
		State:     in.State,
		CreatedAt: in.CreatedAt,
	}
}

func AgentInstanceRecordsFromModel(in []model.AgentInstanceRecord) []api.AgentInstanceRecord {
	if in == nil {
		return nil
	}
	out := make([]api.AgentInstanceRecord, 0, len(in))
	for _, record := range in {
		out = append(out, AgentInstanceRecordFromModel(record))
	}
	return out
}

func AgentInstanceRecordsToModel(in []api.AgentInstanceRecord) []model.AgentInstanceRecord {
	if in == nil {
		return nil
	}
	out := make([]model.AgentInstanceRecord, 0, len(in))
	for _, record := range in {
		out = append(out, AgentInstanceRecordToModel(record))
	}
	return out
}

func AgentInstanceSelectorToModel(in api.AgentInstanceSelector) model.AgentInstanceSelector {
	return model.AgentInstanceSelector{RunID: in.RunID, ClassName: in.ClassName, State: in.State}
}

func AgentInstanceSelectorFromModel(in model.AgentInstanceSelector) api.AgentInstanceSelector {
	return api.AgentInstanceSelector{RunID: in.RunID, ClassName: in.ClassName, State: in.State}
}
