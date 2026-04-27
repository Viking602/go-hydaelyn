package orchestrator

import "time"

func (r *Runtime) writeCriticalContextLocked(runID, taskID string, itemType BlackboardItemType, source SourceIdentity, key, payload string) BlackboardItem {
	if itemType == "" {
		itemType = BlackboardItemContext
	}
	if source.Type == "" {
		source = SourceIdentity{Type: SourceSystem, ID: "orchestrator"}
	}
	return r.writeBlackboardLocked(BlackboardItem{
		RunID:      runID,
		TaskID:     taskID,
		Type:       itemType,
		Source:     source,
		Visibility: BlackboardVisibilityAgentVisible,
		Key:        key,
		Content:    payload,
		Payload:    payload,
		CreatedAt:  time.Now().UTC(),
	})
}
