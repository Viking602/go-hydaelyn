package venat

import (
	"context"
	"testing"

	"github.com/Viking602/venat/api"
)

func TestRunnerDomainFacadesDelegate(t *testing.T) {
	r := New()
	ctx := context.Background()
	r.Admin().RegisterAgent(api.AgentProfile{ID: "facade-agent", Role: "worker"})
	if len(r.Admin().Agents()) == 0 {
		t.Fatal("Admin().RegisterAgent did not persist the profile")
	}
	run, err := r.QueueRun(ctx, api.StartRunCommand{Request: "facade"})
	if err != nil {
		t.Fatalf("QueueRun: %v", err)
	}
	if _, err := r.Blackboard().SelectItems(ctx, run.ID, api.BlackboardSelector{}); err != nil {
		t.Fatalf("Blackboard().SelectItems: %v", err)
	}
	if count, err := r.Governance().ActiveLeaseCountContext(ctx, run.ID, run.RootTaskID); err != nil || count != 0 {
		t.Fatalf("Governance().ActiveLeaseCountContext = %d, %v on a fresh run", count, err)
	}
}
