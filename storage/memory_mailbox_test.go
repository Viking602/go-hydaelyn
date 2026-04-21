package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/mailbox"
)

func TestMemoryMailboxStore_MarkDeliveredRejectsSecondClaim(t *testing.T) {
	store := newMemoryMailboxStore()
	ctx := context.Background()

	saved, err := store.Append(ctx, mailbox.Envelope{
		TeamRunID: "team-1",
		From:      mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: "team-1", AgentID: "sup"},
		To:        mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: "team-1", AgentID: "worker-1"},
		Letter:    mailbox.Letter{Body: "hello"},
	})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if _, err := store.MarkDelivered(ctx, saved.ID, "worker-a", time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("first MarkDelivered() error = %v", err)
	}
	if _, err := store.MarkDelivered(ctx, saved.ID, "worker-b", time.Now().UTC().Add(time.Minute)); !errors.Is(err, mailbox.ErrConflict) {
		t.Fatalf("expected ErrConflict on second claim, got %v", err)
	}
}
