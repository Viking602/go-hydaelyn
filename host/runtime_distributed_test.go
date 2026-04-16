package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func newDistributedRuntime(workerID string, driver storage.Driver, queue scheduler.TaskQueue) *Runtime {
	runtime := New(Config{
		Storage:  driver,
		WorkerID: workerID,
	})
	_ = runtime.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeScheduler,
		Name:      "memory-queue",
		Component: queue,
	})
	runtime.RegisterProvider("team-fake", teamFakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})
	return runtime
}

func TestDistributedRuntimesCanShareQueueAndStorage(t *testing.T) {
	driver := storage.NewMemoryDriver()
	queue := scheduler.NewMemoryQueue()
	coordinator := newDistributedRuntime("coordinator", driver, queue)
	worker := newDistributedRuntime("worker-b", driver, queue)

	state, err := coordinator.QueueTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher"},
		Input: map[string]any{
			"query":      "distributed",
			"subqueries": []string{"branch-a", "branch-b"},
		},
	})
	if err != nil {
		t.Fatalf("QueueTeam() error = %v", err)
	}
	if state.Status != team.StatusRunning {
		t.Fatalf("expected queued team to be running/pending execution, got %#v", state)
	}
	if len(state.Tasks) != 2 || state.Tasks[0].Status != team.TaskStatusPending {
		t.Fatalf("expected queued pending tasks, got %#v", state.Tasks)
	}
	processed, err := worker.RunQueueWorker(context.Background(), 10)
	if err != nil {
		t.Fatalf("RunQueueWorker() error = %v", err)
	}
	if processed == 0 {
		t.Fatalf("expected worker to process queued tasks")
	}
	current, err := coordinator.GetTeam(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}
	if current.Status != team.StatusCompleted {
		t.Fatalf("expected worker to complete shared team, got %#v", current)
	}
}
