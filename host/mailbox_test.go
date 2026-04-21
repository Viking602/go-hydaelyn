package host

import (
	"context"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/mailbox"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestDrainMailboxForAgent_UsesConfiguredInlineBodyLimit(t *testing.T) {
	driver := storage.NewMemoryDriver()
	rt := New(Config{
		Storage:       driver,
		WorkerID:      "rt-inline-limit",
		MailboxLimits: mailbox.Limits{MaxInlineBodySize: 16},
	})
	ctx := context.Background()
	const teamRunID = "team-inline-limit"

	if err := driver.Teams().Save(ctx, team.RunState{
		ID:      teamRunID,
		Pattern: "noop",
		Status:  team.StatusRunning,
		Workers: []team.AgentInstance{{ID: "worker-1", Role: team.RoleResearcher}},
	}); err != nil {
		t.Fatalf("Teams.Save error = %v", err)
	}

	body := "0123456789abcdefXYZ"
	if _, err := rt.Mailbox().Send(ctx, mailbox.SendInput{
		TeamRunID: teamRunID,
		From:      mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: teamRunID, AgentID: "boss"},
		To:        mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: teamRunID, AgentID: "worker-1"},
		Letter:    mailbox.Letter{Body: body},
	}); err != nil {
		t.Fatalf("Send error = %v", err)
	}

	inbox, envs := rt.drainMailboxForAgent(ctx, teamRunID, "worker-1", 1, 0)
	if len(envs) != 1 {
		t.Fatalf("expected 1 envelope, got %d", len(envs))
	}
	if !strings.Contains(inbox, "0123456789abc...") {
		t.Fatalf("expected configured truncation in inbox, got %q", inbox)
	}
	if strings.Contains(inbox, body) {
		t.Fatalf("expected full body to be truncated, got %q", inbox)
	}
}
