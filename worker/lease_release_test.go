package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/memory"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/scripted"
)

var (
	errReportRejected  = errors.New("test: task write rejected")
	errReleaseRejected = errors.New("test: lease release rejected")
)

// unwindFaultProvider fails the report write and then the lease release,
// reproducing a store that becomes unavailable exactly while the worker is
// unwinding an execution.
type unwindFaultProvider struct {
	inner *memory.Provider
	armed atomic.Bool
}

func (p *unwindFaultProvider) arm() { p.armed.Store(true) }

func (p *unwindFaultProvider) Begin(ctx context.Context) (api.UnitOfWork, error) {
	uow, err := p.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if !p.armed.Load() {
		return uow, nil
	}
	return unwindFaultUnitOfWork{UnitOfWork: uow}, nil
}

func (p *unwindFaultProvider) Capabilities(ctx context.Context) (api.StoreCapabilities, error) {
	return p.inner.Capabilities(ctx)
}

type unwindFaultUnitOfWork struct {
	api.UnitOfWork
}

func (u unwindFaultUnitOfWork) Tasks() api.TaskStore {
	return unwindFaultTasks{TaskStore: u.UnitOfWork.Tasks()}
}

func (u unwindFaultUnitOfWork) Leases() api.LeaseStore {
	return unwindFaultLeases{LeaseStore: u.UnitOfWork.Leases()}
}

type unwindFaultTasks struct {
	api.TaskStore
}

// SaveTask rejects only the write that carries a report. Acquiring the report
// lease writes the task too, and that step has to succeed for the release path
// under test to be reached at all.
func (t unwindFaultTasks) SaveTask(ctx context.Context, task api.Task) error {
	if task.Result != nil {
		return errReportRejected
	}
	return t.TaskStore.SaveTask(ctx, task)
}

type unwindFaultLeases struct {
	api.LeaseStore
}

func (l unwindFaultLeases) SaveLease(ctx context.Context, lease api.TaskExecutionLease) error {
	if lease.Status == api.LeaseStatusReleased {
		return errReleaseRejected
	}
	return l.LeaseStore.SaveLease(ctx, lease)
}

func TestSingleRunnerReportSurfacesLeaseReleaseFailure(t *testing.T) {
	ctx := context.Background()
	provider := &unwindFaultProvider{inner: memory.NewProvider()}
	runner := venat.NewDevelopment(api.Config{StoreProvider: provider})
	if err := runner.RegisterAgent(api.AgentProfile{ID: "single-agent", Role: "test"}); err != nil {
		t.Fatalf("RegisterAgent() error = %v", err)
	}
	coordinator := &SingleRunner{
		Runner: runner,
		Worker: AgentWorker{
			Runner: runner, Engine: agent.Engine{Provider: scripted.New(nil), Model: "test-model"},
			AgentID: "single-agent", Model: "test-model", TTL: time.Minute,
		},
		AgentVersion: "v1",
	}
	started, err := coordinator.Start(ctx, StartSingleRunRequest{
		RunID: "single-report-release-failure", Request: "report from the host",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	provider.arm()

	_, err = coordinator.Report(ctx, started.Run.ID, api.TypedReport{
		Status: api.ReportStatusSuccess, Summary: "host authored",
	})
	if !errors.Is(err, errReportRejected) {
		t.Fatalf("Report() error = %v, want the rejected report write", err)
	}
	if !errors.Is(err, errReleaseRejected) {
		t.Fatalf("Report() error = %v, want the failed lease release to surface", err)
	}
}

func TestAgentWorkerSurfacesLeaseReleaseFailure(t *testing.T) {
	ctx := context.Background()
	store := &unwindFaultProvider{inner: memory.NewProvider()}
	runner := venat.NewDevelopment(api.Config{StoreProvider: store})
	if err := runner.RegisterAgent(api.AgentProfile{ID: "agent-a", Role: "test"}); err != nil {
		t.Fatalf("RegisterAgent() error = %v", err)
	}
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "worker-release-failure",
		RootTaskID: "root",
		Request:    "complete and unwind",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        run.RootTaskID,
		TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	store.arm()
	worker := AgentWorker{
		Runner: runner,
		Engine: agent.Engine{
			Provider: scripted.New([]provider.Event{
				{Kind: provider.EventTextDelta, Text: "done"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			}),
			Model: "test-model",
		},
		AgentID: "agent-a",
		Model:   "test-model",
		TTL:     time.Minute,
	}

	outcome, err := worker.ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: envelope})
	if outcome.State != ExecutionFailed {
		t.Fatalf("ExecuteEnvelope() state = %q, want failed", outcome.State)
	}
	if !errors.Is(err, errReportRejected) {
		t.Fatalf("ExecuteEnvelope() error = %v, want rejected report write", err)
	}
	if !errors.Is(err, errReleaseRejected) {
		t.Fatalf("ExecuteEnvelope() error = %v, want failed lease release", err)
	}
}
