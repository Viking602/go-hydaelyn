package mailbox

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/team"
)

// fakeStore is a minimal in-file Store so mailbox tests don't import storage
// (storage imports mailbox; we must not create the reverse direction).
type fakeStore struct {
	envelopes     map[string]*Envelope
	byRecipient   map[string][]string
	byCorrelation map[string][]string
	seq           map[string]int64
	idSeq         int
	appendErrAt   int
	appendCalls   int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		envelopes:     map[string]*Envelope{},
		byRecipient:   map[string][]string{},
		byCorrelation: map[string][]string{},
		seq:           map[string]int64{},
	}
}

func recKey(t, a string) string { return t + "\x00" + a }

func (s *fakeStore) Append(_ context.Context, env Envelope) (Envelope, error) {
	s.appendCalls++
	if s.appendErrAt > 0 && s.appendCalls == s.appendErrAt {
		return Envelope{}, errors.New("append failed")
	}
	s.idSeq++
	if env.ID == "" {
		env.ID = "env-" + strconv.Itoa(s.idSeq)
	}
	if _, dup := s.envelopes[env.ID]; dup {
		return Envelope{}, ErrConflict
	}
	rkey := recKey(env.TeamRunID, env.To.AgentID)
	s.seq[rkey]++
	env.Sequence = s.seq[rkey]
	if env.CreatedAt.IsZero() {
		env.CreatedAt = time.Now().UTC()
	}
	if env.State == "" {
		env.State = EnvelopePending
	}
	stored := env
	s.envelopes[env.ID] = &stored
	s.byRecipient[rkey] = append(s.byRecipient[rkey], env.ID)
	if env.CorrelationID != "" {
		ck := env.TeamRunID + "\x00" + env.CorrelationID
		s.byCorrelation[ck] = append(s.byCorrelation[ck], env.ID)
	}
	return stored, nil
}

func (s *fakeStore) ListPending(_ context.Context, t, a string, limit int) ([]Envelope, error) {
	ids := s.byRecipient[recKey(t, a)]
	out := make([]Envelope, 0, len(ids))
	for _, id := range ids {
		e := s.envelopes[id]
		if e == nil || e.State != EnvelopePending {
			continue
		}
		out = append(out, *e)
	}
	// priority desc, sequence asc
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			pi, pj := priorityRank(out[j].Letter.Priority), priorityRank(out[j-1].Letter.Priority)
			swap := pi > pj || (pi == pj && out[j].Sequence < out[j-1].Sequence)
			if !swap {
				break
			}
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *fakeStore) NextSequence(_ context.Context, t, a string) (int64, error) {
	s.seq[recKey(t, a)]++
	return s.seq[recKey(t, a)], nil
}
func (s *fakeStore) MarkDelivered(_ context.Context, id, owner string, until time.Time) (Envelope, error) {
	e, ok := s.envelopes[id]
	if !ok {
		return Envelope{}, ErrNotFound
	}
	e.State = EnvelopeDelivered
	e.LeaseOwner = owner
	e.LeaseUntil = until
	e.DeliveredAt = time.Now().UTC()
	return *e, nil
}
func (s *fakeStore) MarkAcked(_ context.Context, id, a string, at time.Time, outcome string) error {
	e, ok := s.envelopes[id]
	if !ok {
		return ErrNotFound
	}
	if e.State == EnvelopeAcked {
		return nil
	}
	e.State = EnvelopeAcked
	e.AckedAt = at
	if outcome != "" {
		if e.Letter.Headers == nil {
			e.Letter.Headers = map[string]string{}
		}
		e.Letter.Headers["mailbox.ackOutcome"] = outcome
	}
	return nil
}
func (s *fakeStore) MarkDead(_ context.Context, id, reason string) error {
	e, ok := s.envelopes[id]
	if !ok {
		return ErrNotFound
	}
	e.State = EnvelopeDead
	e.DeadReason = reason
	return nil
}
func (s *fakeStore) IncrementAttempt(_ context.Context, id string) (int, error) {
	e, ok := s.envelopes[id]
	if !ok {
		return 0, ErrNotFound
	}
	e.Attempts++
	e.State = EnvelopePending
	return e.Attempts, nil
}
func (s *fakeStore) RecoverExpired(_ context.Context, now time.Time) (int, error) {
	n := 0
	for _, e := range s.envelopes {
		switch e.State {
		case EnvelopeDelivered:
			if !e.LeaseUntil.IsZero() && !e.LeaseUntil.After(now) {
				e.State = EnvelopePending
				e.LeaseOwner = ""
				e.LeaseUntil = time.Time{}
				n++
			}
		case EnvelopePending:
			if !e.ExpiresAt.IsZero() && !e.ExpiresAt.After(now) {
				e.State = EnvelopeExpired
				n++
			}
		}
	}
	return n, nil
}
func (s *fakeStore) ListByCorrelation(_ context.Context, t, c string) ([]Envelope, error) {
	ids := s.byCorrelation[t+"\x00"+c]
	out := make([]Envelope, 0, len(ids))
	for _, id := range ids {
		if e := s.envelopes[id]; e != nil {
			out = append(out, *e)
		}
	}
	return out, nil
}
func (s *fakeStore) Get(_ context.Context, id string) (Envelope, error) {
	e, ok := s.envelopes[id]
	if !ok {
		return Envelope{}, ErrNotFound
	}
	return *e, nil
}
func (s *fakeStore) CountPending(_ context.Context, t, a string) (int, error) {
	n := 0
	for _, id := range s.byRecipient[recKey(t, a)] {
		if e := s.envelopes[id]; e != nil && e.State == EnvelopePending {
			n++
		}
	}
	return n, nil
}

func newTestMailbox(t *testing.T, store Store) *MemoryMailbox {
	t.Helper()
	loader := func(_ context.Context, id string) (team.RunState, bool) {
		state := fakeState()
		if state.ID != id {
			return team.RunState{}, false
		}
		return state, true
	}
	return NewMemoryMailbox(store, loader, "worker-1", Limits{})
}

func newSendInput(from, to, body string) SendInput {
	return SendInput{
		TeamRunID: "run-1",
		From:      Address{Kind: AddressKindAgent, TeamRunID: "run-1", AgentID: from},
		To:        Address{Kind: AddressKindAgent, TeamRunID: "run-1", AgentID: to},
		Letter:    Letter{Body: body},
	}
}

func TestMemoryMailbox_SendAndFetch(t *testing.T) {
	store := newFakeStore()
	mb := newTestMailbox(t, store)
	ctx := context.Background()

	ids, err := mb.Send(ctx, newSendInput("sup-1", "res-1", "hello"))
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("want 1 envelope, got %d", len(ids))
	}

	fetched, err := mb.Fetch(ctx, "run-1", "res-1", 10, time.Minute)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if len(fetched) != 1 || fetched[0].Letter.Body != "hello" {
		t.Fatalf("unexpected fetched: %+v", fetched)
	}
	if fetched[0].State != EnvelopeDelivered {
		t.Fatalf("expected Delivered, got %s", fetched[0].State)
	}

	if err := mb.Ack(ctx, Receipt{EnvelopeID: fetched[0].ID, AgentID: "res-1", Outcome: "handled"}); err != nil {
		t.Fatalf("Ack error: %v", err)
	}
	// Ack is idempotent.
	if err := mb.Ack(ctx, Receipt{EnvelopeID: fetched[0].ID, AgentID: "res-1"}); err != nil {
		t.Fatalf("second Ack error: %v", err)
	}
}

func TestMemoryMailbox_RoleFanout(t *testing.T) {
	store := newFakeStore()
	mb := newTestMailbox(t, store)
	ctx := context.Background()

	ids, err := mb.Send(ctx, SendInput{
		TeamRunID: "run-1",
		From:      Address{Kind: AddressKindAgent, TeamRunID: "run-1", AgentID: "sup-1"},
		To:        Address{Kind: AddressKindRole, TeamRunID: "run-1", Role: team.RoleResearcher},
		Letter:    Letter{Body: "broadcast"},
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("want fanout of 2, got %d", len(ids))
	}

	f1, _ := mb.Fetch(ctx, "run-1", "res-1", 10, time.Minute)
	f2, _ := mb.Fetch(ctx, "run-1", "res-2", 10, time.Minute)
	if len(f1) != 1 || len(f2) != 1 {
		t.Fatalf("each researcher should see 1 letter, got %d/%d", len(f1), len(f2))
	}
}

func TestMemoryMailbox_FanoutPreflightPreventsPartialWrite(t *testing.T) {
	store := newFakeStore()
	mb := NewMemoryMailbox(store, func(_ context.Context, _ string) (team.RunState, bool) {
		return fakeState(), true
	}, "worker", Limits{MaxPerRecipient: 1})
	ctx := context.Background()

	if _, err := mb.Send(ctx, newSendInput("sup-1", "res-2", "existing")); err != nil {
		t.Fatalf("seed send: %v", err)
	}
	_, err := mb.Send(ctx, SendInput{
		TeamRunID: "run-1",
		From:      Address{Kind: AddressKindAgent, TeamRunID: "run-1", AgentID: "sup-1"},
		To:        Address{Kind: AddressKindRole, TeamRunID: "run-1", Role: team.RoleResearcher},
		Letter:    Letter{Body: "fanout"},
	})
	if !errors.Is(err, ErrMailboxFull) {
		t.Fatalf("want ErrMailboxFull, got %v", err)
	}

	res1, _ := store.ListPending(ctx, "run-1", "res-1", 10)
	if len(res1) != 0 {
		t.Fatalf("expected no partial write for res-1, got %+v", res1)
	}
	res2, _ := store.ListPending(ctx, "run-1", "res-2", 10)
	if len(res2) != 1 {
		t.Fatalf("expected only seeded envelope for res-2, got %+v", res2)
	}
}

func TestMemoryMailbox_AppendFailureRollsBackFanout(t *testing.T) {
	store := newFakeStore()
	store.appendErrAt = 2
	mb := newTestMailbox(t, store)
	ctx := context.Background()

	ids, err := mb.Send(ctx, SendInput{
		TeamRunID: "run-1",
		From:      Address{Kind: AddressKindAgent, TeamRunID: "run-1", AgentID: "sup-1"},
		To:        Address{Kind: AddressKindRole, TeamRunID: "run-1", Role: team.RoleResearcher},
		Letter:    Letter{Body: "fanout"},
	})
	if err == nil {
		t.Fatalf("expected append failure")
	}
	if len(ids) != 0 {
		t.Fatalf("expected no ids on rollback, got %v", ids)
	}

	res1, _ := store.ListPending(ctx, "run-1", "res-1", 10)
	if len(res1) != 0 {
		t.Fatalf("expected rolled back envelope to stay out of pending inbox, got %+v", res1)
	}
	env, getErr := store.Get(ctx, "env-1")
	if getErr != nil {
		t.Fatalf("expected first append to exist for inspection, got %v", getErr)
	}
	if env.State != EnvelopeDead {
		t.Fatalf("expected rollback to dead-letter first envelope, got %s", env.State)
	}
}

func TestMemoryMailbox_PriorityOrdering(t *testing.T) {
	store := newFakeStore()
	mb := newTestMailbox(t, store)
	ctx := context.Background()

	_, _ = mb.Send(ctx, SendInput{
		TeamRunID: "run-1",
		From:      Address{Kind: AddressKindAgent, TeamRunID: "run-1", AgentID: "sup-1"},
		To:        Address{Kind: AddressKindAgent, TeamRunID: "run-1", AgentID: "res-1"},
		Letter:    Letter{Body: "low", Priority: PriorityLow},
	})
	_, _ = mb.Send(ctx, SendInput{
		TeamRunID: "run-1",
		From:      Address{Kind: AddressKindAgent, TeamRunID: "run-1", AgentID: "sup-1"},
		To:        Address{Kind: AddressKindAgent, TeamRunID: "run-1", AgentID: "res-1"},
		Letter:    Letter{Body: "urgent", Priority: PriorityUrgent},
	})

	fetched, err := mb.Fetch(ctx, "run-1", "res-1", 10, time.Minute)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if len(fetched) != 2 {
		t.Fatalf("want 2 letters, got %d", len(fetched))
	}
	if fetched[0].Letter.Body != "urgent" {
		t.Fatalf("expected urgent first, got %q", fetched[0].Letter.Body)
	}
}

func TestMemoryMailbox_NackToDLQ(t *testing.T) {
	store := newFakeStore()
	mb := NewMemoryMailbox(store, func(_ context.Context, id string) (team.RunState, bool) {
		return fakeState(), true
	}, "worker", Limits{MaxAttempts: 2})
	ctx := context.Background()

	ids, _ := mb.Send(ctx, newSendInput("sup-1", "res-1", "retry me"))
	if _, err := mb.Fetch(ctx, "run-1", "res-1", 1, time.Minute); err != nil {
		t.Fatalf("Fetch 1: %v", err)
	}
	if err := mb.Nack(ctx, ids[0], "try again"); err != nil {
		t.Fatalf("nack 1: %v", err)
	}
	if _, err := mb.Fetch(ctx, "run-1", "res-1", 1, time.Minute); err != nil {
		t.Fatalf("Fetch 2: %v", err)
	}
	if err := mb.Nack(ctx, ids[0], "give up"); err != nil {
		t.Fatalf("nack 2: %v", err)
	}
	env, _ := store.Get(ctx, ids[0])
	if env.State != EnvelopeDead {
		t.Fatalf("want EnvelopeDead, got %s", env.State)
	}
}

func TestMemoryMailbox_OversizeBody(t *testing.T) {
	store := newFakeStore()
	mb := NewMemoryMailbox(store, func(_ context.Context, _ string) (team.RunState, bool) {
		return fakeState(), true
	}, "w", Limits{MaxBodySize: 32})
	body := strings.Repeat("x", 200)
	_, err := mb.Send(context.Background(), newSendInput("sup-1", "res-1", body))
	if !errors.Is(err, ErrOverSize) {
		t.Fatalf("want ErrOverSize, got %v", err)
	}
}

func TestMemoryMailbox_RateLimit(t *testing.T) {
	store := newFakeStore()
	mb := NewMemoryMailbox(store, func(_ context.Context, _ string) (team.RunState, bool) {
		return fakeState(), true
	}, "w", Limits{SendRatePerMinute: 2})
	ctx := context.Background()
	if _, err := mb.Send(ctx, newSendInput("sup-1", "res-1", "1")); err != nil {
		t.Fatalf("1st: %v", err)
	}
	if _, err := mb.Send(ctx, newSendInput("sup-1", "res-1", "2")); err != nil {
		t.Fatalf("2nd: %v", err)
	}
	_, err := mb.Send(ctx, newSendInput("sup-1", "res-1", "3"))
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
}

func TestMemoryMailbox_HopCap(t *testing.T) {
	store := newFakeStore()
	mb := NewMemoryMailbox(store, func(_ context.Context, _ string) (team.RunState, bool) {
		return fakeState(), true
	}, "w", Limits{MaxHops: 3})
	ctx := context.Background()
	in := newSendInput("sup-1", "res-1", "x")
	in.Letter.Headers = map[string]string{HeaderHopCount: "3"}
	_, err := mb.Send(ctx, in)
	if !errors.Is(err, ErrHopLimit) {
		t.Fatalf("want ErrHopLimit, got %v", err)
	}
}

func TestMemoryMailbox_TTLExpiry(t *testing.T) {
	store := newFakeStore()
	mb := NewMemoryMailbox(store, func(_ context.Context, _ string) (team.RunState, bool) {
		return fakeState(), true
	}, "w", Limits{})
	ctx := context.Background()
	in := newSendInput("sup-1", "res-1", "quick")
	in.TTL = time.Millisecond
	ids, err := mb.Send(ctx, in)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	fetched, _ := mb.Fetch(ctx, "run-1", "res-1", 10, time.Minute)
	if len(fetched) != 0 {
		t.Fatalf("expected expired letter to be filtered, got %+v", fetched)
	}
	env, _ := store.Get(ctx, ids[0])
	if env.State != EnvelopeExpired {
		t.Fatalf("want EnvelopeExpired, got %s", env.State)
	}
}

func TestMemoryMailbox_Correlation(t *testing.T) {
	store := newFakeStore()
	mb := newTestMailbox(t, store)
	ctx := context.Background()
	in := newSendInput("sup-1", "res-1", "q")
	in.CorrelationID = "thread-x"
	_, _ = mb.Send(ctx, in)

	in2 := newSendInput("res-1", "sup-1", "a")
	in2.CorrelationID = "thread-x"
	_, _ = mb.Send(ctx, in2)

	chain, err := mb.ListByCorrelation(ctx, "run-1", "thread-x")
	if err != nil {
		t.Fatalf("ListByCorrelation: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("want 2 in chain, got %d", len(chain))
	}
}

func TestMemoryMailbox_DirectAgentMustExist(t *testing.T) {
	store := newFakeStore()
	mb := newTestMailbox(t, store)
	_, err := mb.Send(context.Background(), newSendInput("sup-1", "missing-agent", "hello"))
	if !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("want ErrNoRecipients, got %v", err)
	}
}
