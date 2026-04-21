package host

import (
	"context"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/mailbox"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

// TestMailbox_RoundTrip_AgentToAgent covers the happy path: a coordinator seeds
// a letter into an agent's inbox before the agent is scheduled; when the worker
// picks up the task, the letter is injected into the prompt and acked after
// the agent finishes. The EventStore captures sent/delivered/acked.
func TestMailbox_RoundTrip_AgentToAgent(t *testing.T) {
	driver := storage.NewMemoryDriver()
	queue := scheduler.NewMemoryQueue()
	coordinator := newDistributedRuntime("coordinator", driver, queue)
	coordinator.RegisterPattern(singleTaskPattern{})

	state, err := coordinator.QueueTeam(context.Background(), StartTeamRequest{
		Pattern:           "single-task",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"task": "alpha-cohort"},
	})
	if err != nil {
		t.Fatalf("QueueTeam() error = %v", err)
	}

	ids, err := coordinator.Mailbox().Send(context.Background(), mailbox.SendInput{
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
			Subject:  "research brief",
			Body:     "please dig into the alpha cohort",
			Intent:   mailbox.IntentDelegate,
			Priority: mailbox.PriorityHigh,
		},
		CorrelationID: "thread-1",
	})
	if err != nil {
		t.Fatalf("Mailbox.Send() error = %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 envelope id, got %v", ids)
	}

	worker := newDistributedRuntime("worker-a", driver, queue)
	worker.RegisterPattern(singleTaskPattern{})

	processed, err := worker.RunQueueWorker(context.Background(), 5)
	if err != nil {
		t.Fatalf("RunQueueWorker() error = %v", err)
	}
	if processed == 0 {
		t.Fatalf("expected worker to pick up a task")
	}

	final, err := coordinator.GetTeam(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}
	if final.Status != team.StatusCompleted {
		t.Fatalf("expected team completed, got %s", final.Status)
	}

	pending, err := coordinator.Mailbox().Peek(context.Background(), state.ID, "worker-1", 16)
	if err != nil {
		t.Fatalf("Peek error = %v", err)
	}
	for _, env := range pending {
		if env.State == mailbox.EnvelopePending || env.State == mailbox.EnvelopeDelivered {
			t.Fatalf("expected no unacked envelopes, got %+v", env)
		}
	}

	events, err := driver.Events().List(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("Events.List error = %v", err)
	}
	seen := map[storage.EventType]int{}
	var acked storage.Event
	for _, e := range events {
		if strings.HasPrefix(string(e.Type), "Mailbox") {
			seen[e.Type]++
			if e.Type == storage.EventMailboxAcked {
				acked = e
			}
		}
	}
	for _, want := range []storage.EventType{
		storage.EventMailboxSent,
		storage.EventMailboxDelivered,
		storage.EventMailboxAcked,
	} {
		if seen[want] == 0 {
			t.Fatalf("expected at least one %s event, got %+v", want, seen)
		}
	}
	if acked.Payload["correlationId"] != "thread-1" {
		t.Fatalf("expected correlationId thread-1 on ack event, got %+v", acked.Payload)
	}
	if acked.Payload["outcome"] != "handled" {
		t.Fatalf("expected ack outcome=handled, got %+v", acked.Payload)
	}

	workerSnap, err := coordinator.GetSession(context.Background(), final.Tasks[0].SessionID)
	if err != nil {
		t.Fatalf("GetSession error = %v", err)
	}
	foundInbox := false
	for _, m := range workerSnap.Messages {
		if m.Metadata != nil && m.Metadata["mailboxInbox"] == "true" {
			foundInbox = true
			if !strings.Contains(m.Text, "please dig into the alpha cohort") {
				t.Fatalf("inbox body not injected, got %q", m.Text)
			}
			if !strings.Contains(m.Text, "intent=delegate") {
				t.Fatalf("inbox intent not rendered, got %q", m.Text)
			}
		}
	}
	if !foundInbox {
		t.Fatalf("expected inbox message in worker session, got %d messages", len(workerSnap.Messages))
	}
}

// TestMailbox_RoleFanout_DeliversPerRecipient verifies that a letter addressed
// by role fans out to every agent with that role, each getting an independent
// envelope.
func TestMailbox_RoleFanout_DeliversPerRecipient(t *testing.T) {
	driver := storage.NewMemoryDriver()
	ctx := context.Background()

	rt := New(Config{Storage: driver, WorkerID: "rt-1"})
	if rt.Mailbox() == nil {
		t.Fatalf("expected mailbox to be auto-wired")
	}

	teamRunID := "team-role-fanout"
	runState := team.RunState{
		ID:         teamRunID,
		Pattern:    "noop",
		Status:     team.StatusRunning,
		Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor},
		Workers: []team.AgentInstance{
			{ID: "verifier-1", Role: team.RoleVerifier},
			{ID: "verifier-2", Role: team.RoleVerifier},
			{ID: "researcher-1", Role: team.RoleResearcher},
		},
	}
	if err := driver.Teams().Save(ctx, runState); err != nil {
		t.Fatalf("Teams.Save error = %v", err)
	}

	ids, err := rt.Mailbox().Send(ctx, mailbox.SendInput{
		TeamRunID: teamRunID,
		From: mailbox.Address{
			Kind:      mailbox.AddressKindAgent,
			TeamRunID: teamRunID,
			AgentID:   "supervisor",
		},
		To: mailbox.Address{
			Kind:      mailbox.AddressKindRole,
			TeamRunID: teamRunID,
			Role:      team.RoleVerifier,
		},
		Letter: mailbox.Letter{
			Body:   "verify the draft report",
			Intent: mailbox.IntentAsk,
		},
	})
	if err != nil {
		t.Fatalf("Mailbox.Send() error = %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected fanout to 2 verifiers, got %d: %v", len(ids), ids)
	}

	for _, recipient := range []string{"verifier-1", "verifier-2"} {
		pending, err := rt.Mailbox().Peek(ctx, teamRunID, recipient, 16)
		if err != nil {
			t.Fatalf("Peek(%s) error = %v", recipient, err)
		}
		if len(pending) != 1 {
			t.Fatalf("expected 1 envelope for %s, got %d", recipient, len(pending))
		}
		if pending[0].To.AgentID != recipient {
			t.Fatalf("expected envelope To agent %s, got %+v", recipient, pending[0].To)
		}
		if pending[0].OriginalTo.Role != team.RoleVerifier {
			t.Fatalf("expected OriginalTo role preserved, got %+v", pending[0].OriginalTo)
		}
	}

	// Researcher must not receive role=verifier letters.
	pending, err := rt.Mailbox().Peek(ctx, teamRunID, "researcher-1", 16)
	if err != nil {
		t.Fatalf("Peek(researcher-1) error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("researcher received verifier-role letter: %+v", pending)
	}
}
