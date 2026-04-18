package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/team"
)

func TestTeamStoreCompareAndSwap(t *testing.T) {
	store := NewMemoryDriver().Teams()
	base := team.RunState{ID: "team-1", Pattern: "linear", Status: team.StatusRunning, Phase: team.PhaseResearch}
	base.Normalize()
	if err := store.Save(context.Background(), base); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	persisted, err := store.Load(context.Background(), base.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if persisted.Version != 1 {
		t.Fatalf("expected initial version 1, got %d", persisted.Version)
	}
	updated := persisted
	updated.Metadata = map[string]string{"step": "fresh"}
	newVersion, err := store.SaveCAS(context.Background(), updated, persisted.Version)
	if err != nil {
		t.Fatalf("SaveCAS() error = %v", err)
	}
	if newVersion != 2 {
		t.Fatalf("expected CAS version 2, got %d", newVersion)
	}
	current, err := store.Load(context.Background(), base.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if current.Version != 2 {
		t.Fatalf("expected persisted version 2, got %d", current.Version)
	}
	if got := current.Metadata["step"]; got != "fresh" {
		t.Fatalf("expected fresh metadata, got %q", got)
	}
	stale := persisted
	stale.Metadata = map[string]string{"step": "stale"}
	if _, err := store.SaveCAS(context.Background(), stale, persisted.Version); !errors.Is(err, ErrStaleState) {
		t.Fatalf("expected ErrStaleState, got %v", err)
	}
	current, err = store.Load(context.Background(), base.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if current.Version != 2 || current.Metadata["step"] != "fresh" {
		t.Fatalf("expected fresh state to remain authoritative, got %#v", current)
	}
}
