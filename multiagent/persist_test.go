package multiagent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBlackboardEntryItem(t *testing.T) {
	entry := BlackboardEntry{
		Key:        "claim-1",
		Value:      json.RawMessage(`{"ok":true}`),
		WrittenBy:  "agent-a",
		EvidenceID: "ev-1",
		CreatedAt:  time.Unix(1, 0).UTC(),
	}
	item := entry.Item("run-1")
	if item.RunID != "run-1" || item.Source.ID != "agent-a" || item.Key != "claim-1" {
		t.Fatalf("Item() = %#v", item)
	}
}
