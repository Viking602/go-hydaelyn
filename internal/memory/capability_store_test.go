package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/internal/core/model"
)

func TestCapabilityStore_RejectsReservedSelfNamespace(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	err = uow.CapabilityCatalog().SaveCapability(ctx, model.Capability{
		Name:    "hydaelyn.self.profile",
		AgentID: "agent-1",
	})
	if !errors.Is(err, model.ErrCapabilityNameReserved) {
		t.Fatalf("SaveCapability(reserved) = %v, want ErrCapabilityNameReserved", err)
	}
}

func TestCapabilityStore_SavesApplicationName(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	want := model.Capability{Name: "research.search", AgentID: "agent-1", RequiresLease: true, RequiresPolicy: true}
	if err := uow.CapabilityCatalog().SaveCapability(ctx, want); err != nil {
		t.Fatalf("SaveCapability(application) = %v", err)
	}
	got, err := uow.CapabilityCatalog().LoadCapability(ctx, want.Name, want.AgentID)
	if err != nil {
		t.Fatalf("LoadCapability() = %v", err)
	}
	if got.Name != want.Name || !got.RequiresLease || !got.RequiresPolicy {
		t.Fatalf("LoadCapability() = %#v", got)
	}
}

func TestListRuns_FiltersByAgentVersion(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	if err := uow.Runs().SaveRun(ctx, model.Run{ID: "run-v1", AgentVersion: "v1", Status: model.RunStatusCreated}); err != nil {
		t.Fatalf("SaveRun(v1) = %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, model.Run{ID: "run-v2", AgentVersion: "v2", Status: model.RunStatusCreated}); err != nil {
		t.Fatalf("SaveRun(v2) = %v", err)
	}

	got, err := uow.Runs().ListRuns(ctx, model.RunSelector{AgentVersion: "v2"})
	if err != nil {
		t.Fatalf("ListRuns() = %v", err)
	}
	if len(got) != 1 || got[0].ID != "run-v2" {
		t.Fatalf("ListRuns(AgentVersion=v2) = %#v", got)
	}
}
