package host

import (
	"context"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/mailbox"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

// TestDistributedMailbox_CrossRuntimeDelivery verifies that a letter sent on
// one Runtime is picked up by another Runtime that shares the same storage
// driver, mirroring the coordinator+worker topology.
func TestDistributedMailbox_CrossRuntimeDelivery(t *testing.T) {
	driver := storage.NewMemoryDriver()
	queue := scheduler.NewMemoryQueue()
	coordinator := newDistributedRuntime("coordinator", driver, queue)
	coordinator.RegisterPattern(singleTaskPattern{})

	state, err := coordinator.QueueTeam(context.Background(), StartTeamRequest{
		Pattern:           "single-task",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"task": "distributed-mailbox"},
	})
	if err != nil {
		t.Fatalf("QueueTeam() error = %v", err)
	}

	// Coordinator sends; a different Runtime should deliver it.
	if _, err := coordinator.Mailbox().Send(context.Background(), mailbox.SendInput{
		TeamRunID: state.ID,
		From: mailbox.Address{
			Kind:      mailbox.AddressKindAgent,
			TeamRunID: state.ID,
			AgentID:   "supervisor",
		},
		To: mailbox.Address{
			Kind:      mailbox.AddressKindAgent,
			TeamRunID: state.ID,
			AgentID:   "worker-1",
		},
		Letter: mailbox.Letter{
			Body:   "cross-runtime payload",
			Intent: mailbox.IntentAsk,
		},
		CorrelationID: "cross-1",
	}); err != nil {
		t.Fatalf("coordinator Send error = %v", err)
	}

	worker := newDistributedRuntime("worker-b", driver, queue)
	worker.RegisterPattern(singleTaskPattern{})

	processed, err := worker.RunQueueWorker(context.Background(), 5)
	if err != nil {
		t.Fatalf("RunQueueWorker() error = %v", err)
	}
	if processed == 0 {
		t.Fatalf("expected worker to process the task")
	}

	final, err := coordinator.GetTeam(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}
	if final.Status != team.StatusCompleted {
		t.Fatalf("expected completed team, got %s", final.Status)
	}

	// Mailbox state must be consistent across both Runtimes.
	for _, rt := range []*Runtime{coordinator, worker} {
		pending, err := rt.Mailbox().Peek(context.Background(), state.ID, "worker-1", 16)
		if err != nil {
			t.Fatalf("Peek error = %v", err)
		}
		for _, env := range pending {
			if env.State == mailbox.EnvelopePending || env.State == mailbox.EnvelopeDelivered {
				t.Fatalf("expected no unacked envelopes from any runtime, got %+v", env)
			}
		}
	}
}

// TestDistributedMailbox_LeaseRecoveryPreventsDuplicateAck verifies that when
// a worker crashes after Fetch but before Ack, RecoverExpiredLeases returns
// the envelope to pending and a second worker can deliver it exactly once.
func TestDistributedMailbox_LeaseRecoveryPreventsDuplicateAck(t *testing.T) {
	driver := storage.NewMemoryDriver()
	ctx := context.Background()

	rt := New(Config{Storage: driver, WorkerID: "rt-recovery"})
	teamRunID := "team-recovery"
	if err := driver.Teams().Save(ctx, team.RunState{
		ID:      teamRunID,
		Pattern: "noop",
		Status:  team.StatusRunning,
		Workers: []team.AgentInstance{{ID: "worker-a", Role: team.RoleResearcher}},
	}); err != nil {
		t.Fatalf("Teams.Save error = %v", err)
	}

	if _, err := rt.Mailbox().Send(ctx, mailbox.SendInput{
		TeamRunID: teamRunID,
		From:      mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: teamRunID, AgentID: "boss"},
		To:        mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: teamRunID, AgentID: "worker-a"},
		Letter:    mailbox.Letter{Body: "do the thing", Intent: mailbox.IntentDelegate},
	}); err != nil {
		t.Fatalf("Send error = %v", err)
	}

	// First fetch claims the letter with a very short lease.
	first, err := rt.Mailbox().Fetch(ctx, teamRunID, "worker-a", 8, 20*time.Millisecond)
	if err != nil || len(first) != 1 {
		t.Fatalf("first Fetch: got %d envs, err=%v", len(first), err)
	}
	firstID := first[0].ID

	// Simulate crash: do not Ack. Let the lease expire and recover.
	time.Sleep(40 * time.Millisecond)
	if err := rt.Mailbox().RecoverExpiredLeases(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("RecoverExpiredLeases error = %v", err)
	}

	// Second fetch should see the same envelope available again.
	second, err := rt.Mailbox().Fetch(ctx, teamRunID, "worker-a", 8, time.Minute)
	if err != nil {
		t.Fatalf("second Fetch error = %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("expected 1 recovered envelope, got %d", len(second))
	}
	if second[0].ID != firstID {
		t.Fatalf("recovered envelope id mismatch: %s vs %s", second[0].ID, firstID)
	}

	// Second worker acks successfully.
	if err := rt.Mailbox().Ack(ctx, mailbox.Receipt{
		EnvelopeID: firstID,
		AgentID:    "worker-a",
		AckedAt:    time.Now().UTC(),
		Outcome:    "handled",
	}); err != nil {
		t.Fatalf("Ack error = %v", err)
	}

	// Third fetch must return nothing: exactly-once ack.
	third, err := rt.Mailbox().Fetch(ctx, teamRunID, "worker-a", 8, time.Minute)
	if err != nil {
		t.Fatalf("third Fetch error = %v", err)
	}
	if len(third) != 0 {
		t.Fatalf("expected no envelopes after ack, got %d", len(third))
	}
}
