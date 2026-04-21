package host

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/mailbox"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

const (
	defaultMailboxFetchLimit = 8
	defaultMailboxLeaseTTL   = 60 * time.Second
)

// Mailbox returns the Runtime's mailbox. May be nil if not wired.
func (r *Runtime) Mailbox() mailbox.Mailbox {
	return r.mailbox
}

// resolveRecipientsState is the StateLoader the mailbox uses to fan out
// role/group addresses. It reads the current team run snapshot from storage.
func (r *Runtime) resolveRecipientsState(ctx context.Context, teamRunID string) (team.RunState, bool) {
	if r.storage == nil {
		return team.RunState{}, false
	}
	state, err := r.storage.Teams().Load(ctx, teamRunID)
	if err != nil {
		return team.RunState{}, false
	}
	if state.ID == "" {
		return team.RunState{}, false
	}
	return state, true
}

// drainMailboxForAgent fetches up to the configured batch of pending letters
// for the given agent and returns the rendered prompt fragment plus the
// envelope IDs (for later Ack/Nack). An empty string with no IDs means the
// mailbox is not wired, is empty, or failed silently.
func (r *Runtime) drainMailboxForAgent(ctx context.Context, teamRunID, agentID string, limit int, leaseTTL time.Duration) (string, []mailbox.Envelope) {
	if r.mailbox == nil || strings.TrimSpace(agentID) == "" {
		return "", nil
	}
	if limit <= 0 {
		limit = defaultMailboxFetchLimit
	}
	if leaseTTL <= 0 {
		leaseTTL = defaultMailboxLeaseTTL
	}
	envelopes, err := r.mailbox.Fetch(ctx, teamRunID, agentID, limit, leaseTTL)
	if err != nil || len(envelopes) == 0 {
		return "", nil
	}
	return renderInbox(envelopes, r.mailboxLimits.MaxInlineBodySize), envelopes
}

// renderInbox formats a set of delivered envelopes as a prompt-friendly block.
func renderInbox(envelopes []mailbox.Envelope, maxInlineBodySize int) string {
	if len(envelopes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Incoming messages]\n")
	for i, env := range envelopes {
		fromID := env.From.AgentID
		if fromID == "" {
			fromID = string(env.From.Kind)
		}
		subject := strings.TrimSpace(env.Letter.Subject)
		if subject == "" {
			subject = "(no subject)"
		}
		intent := env.Letter.Intent
		if intent == "" {
			intent = mailbox.IntentAsk
		}
		fmt.Fprintf(&b, "%d. from=%s intent=%s priority=%s id=%s\n",
			i+1, fromID, intent, env.Letter.Priority, env.ID)
		fmt.Fprintf(&b, "   subject: %s\n", subject)
		body := strings.TrimSpace(env.Letter.Body)
		if body != "" {
			preview := mailboxTruncateBody(body, maxInlineBodySize)
			fmt.Fprintf(&b, "   body: %s\n", preview)
		}
		if env.CorrelationID != "" {
			fmt.Fprintf(&b, "   correlationId: %s\n", env.CorrelationID)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// mailboxTruncateBody is a host-local copy of the mailbox truncator (unexported upstream).
func mailboxTruncateBody(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max < 16 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

// ackInbox acknowledges every envelope in `envelopes` with the given outcome.
// Errors are swallowed — ack is best-effort; stuck leases are cleaned up by
// RecoverExpiredLeases.
func (r *Runtime) ackInbox(ctx context.Context, envelopes []mailbox.Envelope, outcome string) {
	if r.mailbox == nil || len(envelopes) == 0 {
		return
	}
	now := time.Now().UTC()
	for _, env := range envelopes {
		_ = r.mailbox.Ack(ctx, mailbox.Receipt{
			EnvelopeID: env.ID,
			AgentID:    env.To.AgentID,
			AckedAt:    now,
			Outcome:    outcome,
		})
		r.recordMailboxEvent(ctx, storage.EventMailboxAcked, env, map[string]any{
			"outcome": outcome,
		})
	}
}

// nackInbox marks each envelope as not-yet-handled; the mailbox increments the
// attempt counter internally and promotes to DLQ if MaxAttempts is reached.
func (r *Runtime) nackInbox(ctx context.Context, envelopes []mailbox.Envelope, reason string) {
	if r.mailbox == nil || len(envelopes) == 0 {
		return
	}
	for _, env := range envelopes {
		_ = r.mailbox.Nack(ctx, env.ID, reason)
		r.recordMailboxEvent(ctx, storage.EventMailboxNacked, env, map[string]any{
			"reason": reason,
		})
	}
}

// recordMailboxEvent emits a storage event describing a mailbox state change.
// Best-effort — a failure here should not fail the calling task.
func (r *Runtime) recordMailboxEvent(ctx context.Context, eventType storage.EventType, env mailbox.Envelope, extra map[string]any) {
	payload := map[string]any{
		"envelopeId":    env.ID,
		"teamRunId":     env.TeamRunID,
		"fromAgentId":   env.From.AgentID,
		"toAgentId":     env.To.AgentID,
		"intent":        string(env.Letter.Intent),
		"priority":      string(env.Letter.Priority),
		"state":         string(env.State),
		"sequence":      env.Sequence,
		"correlationId": env.CorrelationID,
		"inReplyTo":     env.InReplyTo,
	}
	for k, v := range extra {
		payload[k] = v
	}
	_ = r.appendEvent(ctx, storage.Event{
		RunID:   env.TeamRunID,
		TeamID:  env.TeamRunID,
		Type:    eventType,
		Payload: payload,
	})
}
