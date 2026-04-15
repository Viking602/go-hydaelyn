package session

import (
	"context"
	"testing"

	"hydaelyn/message"
)

func TestMemoryStoreBranchCopiesTranscript(t *testing.T) {
	store := NewMemoryStore()
	root, err := store.Create(context.Background(), CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := store.Append(context.Background(), root.ID, message.NewText(message.RoleUser, "hello")); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	branch, err := store.Branch(context.Background(), root.ID, "exp")
	if err != nil {
		t.Fatalf("Branch() error = %v", err)
	}
	snapshot, err := store.Load(context.Background(), branch.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(snapshot.Messages) != 1 {
		t.Fatalf("expected transcript copy, got %d messages", len(snapshot.Messages))
	}
}
