package multiagent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBlackboardEntryRoundTrip(t *testing.T) {
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
	got := EntryFromItem(item)
	if got.Key != entry.Key || got.WrittenBy != entry.WrittenBy || got.EvidenceID != entry.EvidenceID {
		t.Fatalf("EntryFromItem() = %#v", got)
	}
}

func TestHandoffRecordRoundTrip(t *testing.T) {
	handoff := Handoff{
		ID:          "h-1",
		RunID:       "run-1",
		From:        "a",
		To:          "b",
		Reason:      "next",
		Payload:     json.RawMessage(`{"n":1}`),
		EvidenceIDs: []string{"ev-1"},
		CreatedAt:   time.Unix(2, 0).UTC(),
	}
	record := handoff.Record()
	if record.ID != "h-1" || record.From != "a" || record.To != "b" {
		t.Fatalf("Record() = %#v", record)
	}
	got := HandoffFromRecord(record)
	if got.ID != handoff.ID || got.RunID != handoff.RunID || string(got.Payload) != string(handoff.Payload) {
		t.Fatalf("HandoffFromRecord() = %#v", got)
	}
}
