package worker

import (
	"context"
	"testing"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/api"
)

func TestStandardResourceClaimController_RecoverExpiredResourceClaims(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	now := time.Now().UTC()
	acquired, err := runner.AcquireResourceClaims(ctx, api.ResourceClaimRequest{
		RunID: "run", TaskID: "task", LeaseID: "lease", HolderID: "agent",
		Claims:      []api.ResourceClaimSpec{{ID: "claim", Key: "repo", Mode: api.ResourceClaimExclusive}},
		RequestedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil || !acquired.Acquired || len(acquired.Claims) != 1 {
		t.Fatalf("AcquireResourceClaims() decision=%#v error=%v", acquired, err)
	}
	controller := StandardResourceClaimController{Runner: runner}
	recovered, err := controller.RecoverExpiredResourceClaims(ctx, now.Add(2*time.Minute), 10)
	if err != nil {
		t.Fatalf("RecoverExpiredResourceClaims() error = %v", err)
	}
	if len(recovered) != 1 || recovered[0].State != api.ResourceClaimExpired || recovered[0].Version != 2 {
		t.Fatalf("recovered claims = %#v", recovered)
	}
	again, err := controller.RecoverExpiredResourceClaims(ctx, now.Add(3*time.Minute), 10)
	if err != nil || len(again) != 0 {
		t.Fatalf("second recovery claims=%#v error=%v", again, err)
	}
}

func TestStandardResourceClaimController_RequiresRunner(t *testing.T) {
	_, err := (StandardResourceClaimController{}).RecoverExpiredResourceClaims(context.Background(), time.Now(), 1)
	if err == nil {
		t.Fatal("RecoverExpiredResourceClaims() error = nil")
	}
}
