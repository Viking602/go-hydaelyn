package host

import (
	"context"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/mailbox"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

// TestMailboxGuardrail_RedactsPIIInBody verifies that secret tokens, emails,
// and phone numbers in a letter body are scrubbed at Send time so the
// downstream recipient never sees the raw values.
func TestMailboxGuardrail_RedactsPIIInBody(t *testing.T) {
	driver := storage.NewMemoryDriver()
	ctx := context.Background()
	rt := New(Config{Storage: driver, WorkerID: "rt-guardrail"})

	teamRunID := "team-guardrail"
	if err := driver.Teams().Save(ctx, team.RunState{
		ID:      teamRunID,
		Pattern: "noop",
		Status:  team.StatusRunning,
		Workers: []team.AgentInstance{{ID: "verifier-1", Role: team.RoleVerifier}},
	}); err != nil {
		t.Fatalf("Teams.Save error = %v", err)
	}

	rawBody := "Please validate key sk-abcd1234efgh5678 from user@example.com phone 415-555-0199"
	ids, err := rt.Mailbox().Send(ctx, mailbox.SendInput{
		TeamRunID: teamRunID,
		From:      mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: teamRunID, AgentID: "boss"},
		To:        mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: teamRunID, AgentID: "verifier-1"},
		Letter: mailbox.Letter{
			Subject: "token to validate: sk-secretSUBJECT",
			Body:    rawBody,
		},
	})
	if err != nil {
		t.Fatalf("Send error = %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 envelope, got %d", len(ids))
	}

	envs, err := rt.Mailbox().Peek(ctx, teamRunID, "verifier-1", 8)
	if err != nil {
		t.Fatalf("Peek error = %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(envs))
	}

	body := envs[0].Letter.Body
	subject := envs[0].Letter.Subject
	for _, needle := range []string{"sk-abcd1234efgh5678", "user@example.com", "415-555-0199", "sk-secretSUBJECT"} {
		if strings.Contains(body+subject, needle) {
			t.Fatalf("raw PII %q leaked through: subject=%q body=%q", needle, subject, body)
		}
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("expected body to contain [REDACTED], got %q", body)
	}
	if !strings.Contains(subject, "[REDACTED]") {
		t.Fatalf("expected subject to contain [REDACTED], got %q", subject)
	}
}
