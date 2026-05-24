package sqlbase

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
)

// ErrTxClosed is returned by store methods after the wrapping UnitOfWork
// has been committed or rolled back.
var ErrTxClosed = errors.New("sqlbase: transaction is closed")

// UnitOfWork is the dialect-agnostic api.UnitOfWork backed by a *sql.Tx.
// Providers construct it from their own Begin and return it from
// StoreProvider.Begin verbatim.
type UnitOfWork struct {
	tx      *sql.Tx
	dialect Dialect
}

// NewUnitOfWork constructs a UnitOfWork around an open *sql.Tx and the
// provider's Dialect. Ownership of the tx transfers in; UnitOfWork
// commits or rolls back on demand.
func NewUnitOfWork(tx *sql.Tx, d Dialect) *UnitOfWork {
	return &UnitOfWork{tx: tx, dialect: d}
}

func (u *UnitOfWork) txOrErr() (*sql.Tx, error) {
	if u.tx == nil {
		return nil, ErrTxClosed
	}
	return u.tx, nil
}

// Commit finalizes the transaction. Subsequent store calls return
// ErrTxClosed.
func (u *UnitOfWork) Commit(ctx context.Context) error {
	if u.tx == nil {
		return ErrTxClosed
	}
	tx := u.tx
	u.tx = nil
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", u.dialect.Name(), err)
	}
	return nil
}

// Rollback abandons the transaction. Safe to call multiple times;
// subsequent calls are no-ops once the tx is already done.
func (u *UnitOfWork) Rollback(ctx context.Context) error {
	if u.tx == nil {
		return nil
	}
	tx := u.tx
	u.tx = nil
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("%s: rollback: %w", u.dialect.Name(), err)
	}
	return nil
}

// Store accessors — each returns a thin handle bound to the same tx and
// dialect. They are cheap to call repeatedly.
func (u *UnitOfWork) Runs() api.RunStore                   { return &runStore{u: u} }
func (u *UnitOfWork) Tasks() api.TaskStore                 { return &taskStore{u: u} }
func (u *UnitOfWork) Events() api.EventStore               { return &eventStore{u: u} }
func (u *UnitOfWork) Trace() api.TraceStore                { return &traceStore{u: u} }
func (u *UnitOfWork) Blackboard() api.BlackboardReadWriter { return &blackboardStore{u: u} }
func (u *UnitOfWork) MailboxOutbox() api.MailboxOutboxStore {
	return &mailboxStore{u: u}
}
func (u *UnitOfWork) UserMessages() api.UserMessageStore { return &userMessageStore{u: u} }
func (u *UnitOfWork) Leases() api.LeaseStore             { return &leaseStore{u: u} }
func (u *UnitOfWork) Approvals() api.ApprovalStore       { return &approvalStore{u: u} }
func (u *UnitOfWork) ResumeTokens() api.ResumeTokenStore { return &resumeTokenStore{u: u} }
func (u *UnitOfWork) ActionAttempts() api.ActionAttemptStore {
	return &actionAttemptStore{u: u}
}
func (u *UnitOfWork) AgentProfiles() api.AgentProfileStore { return &agentProfileStore{u: u} }
func (u *UnitOfWork) CapabilityCatalog() api.CapabilityStore {
	return &capabilityStore{u: u}
}
func (u *UnitOfWork) UsageRecords() api.UsageStore     { return &usageStore{u: u} }
func (u *UnitOfWork) DeadLetters() api.DeadLetterStore { return &deadLetterStore{u: u} }

// exec wraps tx.ExecContext with Rebind so callers can write SQL with
// portable `?` placeholders.
func (u *UnitOfWork) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	tx, err := u.txOrErr()
	if err != nil {
		return nil, err
	}
	return tx.ExecContext(ctx, u.dialect.Rebind(query), args...)
}

func (u *UnitOfWork) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	tx, err := u.txOrErr()
	if err != nil {
		return nil, err
	}
	return tx.QueryContext(ctx, u.dialect.Rebind(query), args...)
}

func (u *UnitOfWork) upsert(table string, cols, pk, updateCols []string) string {
	phs := Placeholders(len(cols))
	colList := joinCols(cols)
	return fmt.Sprintf("INSERT INTO %s(%s) VALUES(%s) %s",
		table, colList, phs, u.dialect.UpsertClause(pk, updateCols))
}

func joinCols(cols []string) string {
	out := ""
	for i, c := range cols {
		if i > 0 {
			out += ","
		}
		out += c
	}
	return out
}

func marshalJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("sqlbase: marshal: %w", err)
	}
	return string(b), nil
}

func unmarshalJSON(s string, out any) error {
	if err := json.Unmarshal([]byte(s), out); err != nil {
		return fmt.Errorf("sqlbase: unmarshal: %w", err)
	}
	return nil
}

func unixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// ─── RunStore ───────────────────────────────────────────────────────────

type runStore struct{ u *UnitOfWork }

func (s *runStore) SaveRun(ctx context.Context, r api.Run) error {
	payload, err := marshalJSON(r)
	if err != nil {
		return err
	}
	agentID := r.Metadata["agentId"]
	q := s.u.upsert("runs",
		[]string{"id", "status", "agent_id", "payload", "created_at"},
		[]string{"id"},
		[]string{"status", "agent_id", "payload"})
	if _, err := s.u.exec(ctx, q, r.ID, string(r.Status), agentID, payload, unixNano(r.CreatedAt)); err != nil {
		return fmt.Errorf("%s: SaveRun: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *runStore) LoadRun(ctx context.Context, id string) (api.Run, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return api.Run{}, err
	}
	var payload string
	err = tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT payload FROM runs WHERE id=?`), id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return api.Run{}, fmt.Errorf("%s: LoadRun %q: not found", s.u.dialect.Name(), id)
	}
	if err != nil {
		return api.Run{}, fmt.Errorf("%s: LoadRun: %w", s.u.dialect.Name(), err)
	}
	var out api.Run
	if err := unmarshalJSON(payload, &out); err != nil {
		return api.Run{}, err
	}
	return out, nil
}

func (s *runStore) ListRuns(ctx context.Context, sel api.RunSelector) ([]api.Run, error) {
	q := `SELECT payload FROM runs WHERE 1=1`
	var args []any
	if len(sel.IDs) > 0 {
		q += ` AND id IN (` + Placeholders(len(sel.IDs)) + `)`
		for _, id := range sel.IDs {
			args = append(args, id)
		}
	}
	if sel.AgentID != "" {
		q += ` AND agent_id=?`
		args = append(args, sel.AgentID)
	}
	if len(sel.Statuses) > 0 {
		q += ` AND status IN (` + Placeholders(len(sel.Statuses)) + `)`
		for _, st := range sel.Statuses {
			args = append(args, string(st))
		}
	}
	if !sel.Since.IsZero() {
		q += ` AND created_at>=?`
		args = append(args, unixNano(sel.Since))
	}
	if !sel.Until.IsZero() {
		q += ` AND created_at<=?`
		args = append(args, unixNano(sel.Until))
	}
	q += ` ORDER BY created_at ASC`
	if sel.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, sel.Limit)
	}
	rows, err := s.u.query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: ListRuns: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	var out []api.Run
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var r api.Run
		if err := unmarshalJSON(payload, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ─── TaskStore ──────────────────────────────────────────────────────────

type taskStore struct{ u *UnitOfWork }

func (s *taskStore) SaveTask(ctx context.Context, t api.Task) error {
	payload, err := marshalJSON(t)
	if err != nil {
		return err
	}
	q := s.u.upsert("tasks",
		[]string{"run_id", "id", "status", "payload"},
		[]string{"run_id", "id"},
		[]string{"status", "payload"})
	if _, err := s.u.exec(ctx, q, t.RunID, t.ID, string(t.Status), payload); err != nil {
		return fmt.Errorf("%s: SaveTask: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *taskStore) LoadTask(ctx context.Context, runID, taskID string) (api.Task, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return api.Task{}, err
	}
	var payload string
	err = tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT payload FROM tasks WHERE run_id=? AND id=?`), runID, taskID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return api.Task{}, fmt.Errorf("%s: LoadTask: not found", s.u.dialect.Name())
	}
	if err != nil {
		return api.Task{}, fmt.Errorf("%s: LoadTask: %w", s.u.dialect.Name(), err)
	}
	var out api.Task
	if err := unmarshalJSON(payload, &out); err != nil {
		return api.Task{}, err
	}
	return out, nil
}

func (s *taskStore) ListTasks(ctx context.Context, runID string) ([]api.Task, error) {
	rows, err := s.u.query(ctx, `SELECT payload FROM tasks WHERE run_id=?`, runID)
	if err != nil {
		return nil, fmt.Errorf("%s: ListTasks: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	var out []api.Task
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var t api.Task
		if err := unmarshalJSON(payload, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ─── EventStore ─────────────────────────────────────────────────────────

type eventStore struct{ u *UnitOfWork }

func (s *eventStore) AppendEvent(ctx context.Context, e api.Event) error {
	payload, err := marshalJSON(e)
	if err != nil {
		return err
	}
	_, err = s.u.exec(ctx,
		`INSERT INTO events(run_id,sequence,type,payload,recorded_at) VALUES(?,?,?,?,?)`,
		e.RunID, e.Sequence, string(e.Type), payload, unixNano(e.RecordedAt))
	if err != nil {
		return fmt.Errorf("%s: AppendEvent: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *eventStore) ListEvents(ctx context.Context, runID string) ([]api.Event, error) {
	rows, err := s.u.query(ctx,
		`SELECT payload FROM events WHERE run_id=? ORDER BY sequence ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("%s: ListEvents: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	var out []api.Event
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var e api.Event
		if err := unmarshalJSON(payload, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *eventStore) ListAfter(ctx context.Context, runID string, afterSeq uint64) ([]api.Event, error) {
	rows, err := s.u.query(ctx,
		`SELECT payload FROM events WHERE run_id=? AND sequence>? ORDER BY sequence ASC`,
		runID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("%s: ListAfter: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	var out []api.Event
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var e api.Event
		if err := unmarshalJSON(payload, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ─── TraceStore ─────────────────────────────────────────────────────────

type traceStore struct{ u *UnitOfWork }

func (s *traceStore) SaveTraceSpan(ctx context.Context, sp api.TraceSpan) error {
	payload, err := marshalJSON(sp)
	if err != nil {
		return err
	}
	q := s.u.upsert("trace_spans",
		[]string{"id", "run_id", "payload"},
		[]string{"id"},
		[]string{"payload"})
	if _, err := s.u.exec(ctx, q, sp.ID, sp.RunID, payload); err != nil {
		return fmt.Errorf("%s: SaveTraceSpan: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *traceStore) ListTraceSpans(ctx context.Context, runID string) ([]api.TraceSpan, error) {
	rows, err := s.u.query(ctx, `SELECT payload FROM trace_spans WHERE run_id=?`, runID)
	if err != nil {
		return nil, fmt.Errorf("%s: ListTraceSpans: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	var out []api.TraceSpan
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var sp api.TraceSpan
		if err := unmarshalJSON(payload, &sp); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// ─── BlackboardReadWriter ──────────────────────────────────────────────

type blackboardStore struct{ u *UnitOfWork }

func (s *blackboardStore) WriteItem(ctx context.Context, item api.BlackboardItem) error {
	payload, err := marshalJSON(item)
	if err != nil {
		return err
	}
	createdAt := item.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	q := s.u.upsert("blackboard_items",
		[]string{"id", "run_id", "item_type", "payload", "created_at"},
		[]string{"id"},
		[]string{"payload"})
	if _, err := s.u.exec(ctx, q, item.ID, item.RunID, string(item.Type), payload, unixNano(createdAt)); err != nil {
		return fmt.Errorf("%s: WriteItem: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *blackboardStore) SelectItems(ctx context.Context, runID string, sel api.BlackboardSelector) ([]api.BlackboardItem, error) {
	q := `SELECT payload FROM blackboard_items WHERE run_id=?`
	args := []any{runID}
	if len(sel.ItemTypes) > 0 {
		q += ` AND item_type IN (` + Placeholders(len(sel.ItemTypes)) + `)`
		for _, it := range sel.ItemTypes {
			args = append(args, string(it))
		}
	}
	q += ` ORDER BY created_at ASC`
	if sel.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, sel.Limit)
	}
	rows, err := s.u.query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: SelectItems: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	var out []api.BlackboardItem
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var it api.BlackboardItem
		if err := unmarshalJSON(payload, &it); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ─── MailboxOutboxStore ────────────────────────────────────────────────

type mailboxStore struct{ u *UnitOfWork }

func (s *mailboxStore) QueueEnvelope(ctx context.Context, env api.TaskEnvelope) error {
	payload, err := marshalJSON(env)
	if err != nil {
		return err
	}
	createdAt := env.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	q := s.u.upsert("envelopes",
		[]string{"id", "run_id", "task_id", "payload", "created_at"},
		[]string{"id"},
		[]string{"payload"})
	if _, err := s.u.exec(ctx, q, env.ID, env.RunID, env.TaskID, payload, unixNano(createdAt)); err != nil {
		return fmt.Errorf("%s: QueueEnvelope: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *mailboxStore) LoadEnvelope(ctx context.Context, id string) (api.TaskEnvelope, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return api.TaskEnvelope{}, err
	}
	var payload string
	err = tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT payload FROM envelopes WHERE id=?`), id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return api.TaskEnvelope{}, fmt.Errorf("%s: LoadEnvelope: not found", s.u.dialect.Name())
	}
	if err != nil {
		return api.TaskEnvelope{}, fmt.Errorf("%s: LoadEnvelope: %w", s.u.dialect.Name(), err)
	}
	var out api.TaskEnvelope
	if err := unmarshalJSON(payload, &out); err != nil {
		return api.TaskEnvelope{}, err
	}
	return out, nil
}

func (s *mailboxStore) UpdateEnvelope(ctx context.Context, env api.TaskEnvelope) error {
	return s.QueueEnvelope(ctx, env)
}

func (s *mailboxStore) ListEnvelopes(ctx context.Context, runID string) ([]api.TaskEnvelope, error) {
	rows, err := s.u.query(ctx, `SELECT payload FROM envelopes WHERE run_id=? ORDER BY created_at ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("%s: ListEnvelopes: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	var out []api.TaskEnvelope
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var env api.TaskEnvelope
		if err := unmarshalJSON(payload, &env); err != nil {
			return nil, err
		}
		out = append(out, env)
	}
	return out, rows.Err()
}

// ─── UserMessageStore ───────────────────────────────────────────────────

type userMessageStore struct{ u *UnitOfWork }

func (s *userMessageStore) QueueMessage(ctx context.Context, m api.UserMessage) error {
	payload, err := marshalJSON(m)
	if err != nil {
		return err
	}
	createdAt := m.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	q := s.u.upsert("user_messages",
		[]string{"id", "run_id", "recipient", "status", "payload", "created_at"},
		[]string{"id"},
		[]string{"status", "payload"})
	if _, err := s.u.exec(ctx, q, m.ID, m.RunID, "", string(m.Status), payload, unixNano(createdAt)); err != nil {
		return fmt.Errorf("%s: QueueMessage: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *userMessageStore) LoadMessage(ctx context.Context, runID, id string) (api.UserMessage, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return api.UserMessage{}, err
	}
	var payload string
	err = tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT payload FROM user_messages WHERE run_id=? AND id=?`), runID, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return api.UserMessage{}, fmt.Errorf("%s: LoadMessage: not found", s.u.dialect.Name())
	}
	if err != nil {
		return api.UserMessage{}, fmt.Errorf("%s: LoadMessage: %w", s.u.dialect.Name(), err)
	}
	var out api.UserMessage
	if err := unmarshalJSON(payload, &out); err != nil {
		return api.UserMessage{}, err
	}
	return out, nil
}

func (s *userMessageStore) UpdateMessage(ctx context.Context, m api.UserMessage) error {
	return s.QueueMessage(ctx, m)
}

func (s *userMessageStore) ListMessages(ctx context.Context, runID string) ([]api.UserMessage, error) {
	rows, err := s.u.query(ctx, `SELECT payload FROM user_messages WHERE run_id=? ORDER BY created_at ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("%s: ListMessages: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	return scanUserMessages(rows)
}

func (s *userMessageStore) ListPendingFor(ctx context.Context, sel api.UserMessageSelector) ([]api.UserMessage, error) {
	q := `SELECT payload FROM user_messages WHERE 1=1`
	var args []any
	if sel.RunID != "" {
		q += ` AND run_id=?`
		args = append(args, sel.RunID)
	}
	if sel.Recipient != "" {
		q += ` AND recipient=?`
		args = append(args, sel.Recipient)
	}
	statuses := sel.Statuses
	if len(statuses) == 0 {
		statuses = []string{string(api.UserMessageQueued)}
	}
	q += ` AND status IN (` + Placeholders(len(statuses)) + `)`
	for _, st := range statuses {
		args = append(args, st)
	}
	if !sel.Since.IsZero() {
		q += ` AND created_at>=?`
		args = append(args, unixNano(sel.Since))
	}
	if !sel.Until.IsZero() {
		q += ` AND created_at<=?`
		args = append(args, unixNano(sel.Until))
	}
	q += ` ORDER BY created_at ASC, id ASC`
	if sel.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, sel.Limit)
	}
	rows, err := s.u.query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: ListPendingFor: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	return scanUserMessages(rows)
}

func scanUserMessages(rows *sql.Rows) ([]api.UserMessage, error) {
	var out []api.UserMessage
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var m api.UserMessage
		if err := unmarshalJSON(payload, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ─── LeaseStore ────────────────────────────────────────────────────────

type leaseStore struct{ u *UnitOfWork }

func (s *leaseStore) SaveLease(ctx context.Context, l api.TaskExecutionLease) error {
	payload, err := marshalJSON(l)
	if err != nil {
		return err
	}
	q := s.u.upsert("leases",
		[]string{"id", "run_id", "task_id", "holder_id", "status", "version", "payload", "expires_at"},
		[]string{"id"},
		[]string{"holder_id", "status", "version", "payload", "expires_at"})
	if _, err := s.u.exec(ctx, q,
		l.ID, l.RunID, l.TaskID, l.HolderID, string(l.Status),
		int64(l.Version), payload, unixNano(l.ExpiresAt)); err != nil {
		return fmt.Errorf("%s: SaveLease: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *leaseStore) LoadLease(ctx context.Context, id string) (api.TaskExecutionLease, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return api.TaskExecutionLease{}, err
	}
	var payload string
	err = tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT payload FROM leases WHERE id=?`), id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return api.TaskExecutionLease{}, fmt.Errorf("%s: LoadLease: not found", s.u.dialect.Name())
	}
	if err != nil {
		return api.TaskExecutionLease{}, fmt.Errorf("%s: LoadLease: %w", s.u.dialect.Name(), err)
	}
	var out api.TaskExecutionLease
	if err := unmarshalJSON(payload, &out); err != nil {
		return api.TaskExecutionLease{}, err
	}
	return out, nil
}

func (s *leaseStore) ActiveLeaseForTask(ctx context.Context, runID, taskID string) (api.TaskExecutionLease, bool, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return api.TaskExecutionLease{}, false, err
	}
	var payload string
	err = tx.QueryRowContext(ctx,
		s.u.dialect.Rebind(`SELECT payload FROM leases WHERE run_id=? AND task_id=? AND status=? LIMIT 1`),
		runID, taskID, string(api.LeaseStatusActive)).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return api.TaskExecutionLease{}, false, nil
	}
	if err != nil {
		return api.TaskExecutionLease{}, false, fmt.Errorf("%s: ActiveLeaseForTask: %w", s.u.dialect.Name(), err)
	}
	var out api.TaskExecutionLease
	if err := unmarshalJSON(payload, &out); err != nil {
		return api.TaskExecutionLease{}, false, err
	}
	return out, true, nil
}

func (s *leaseStore) AcquireWithExpectedVersion(ctx context.Context, l api.TaskExecutionLease, expectedVersion uint64) (bool, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return false, err
	}
	// Read current version (if any).
	var curVersion sql.NullInt64
	err = tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT version FROM leases WHERE id=?`), l.ID).Scan(&curVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("%s: AcquireWithExpectedVersion read: %w", s.u.dialect.Name(), err)
	}
	currently := uint64(0)
	exists := false
	if err == nil {
		exists = true
		currently = uint64(curVersion.Int64)
	}
	if currently != expectedVersion {
		return false, nil
	}
	newLease := l
	newLease.Version = expectedVersion + 1
	payload, mErr := marshalJSON(newLease)
	if mErr != nil {
		return false, mErr
	}
	if exists {
		res, err := s.u.exec(ctx,
			`UPDATE leases SET holder_id=?, status=?, version=?, payload=?, expires_at=? WHERE id=? AND version=?`,
			newLease.HolderID, string(newLease.Status), int64(newLease.Version), payload, unixNano(newLease.ExpiresAt),
			newLease.ID, int64(expectedVersion))
		if err != nil {
			return false, fmt.Errorf("%s: AcquireWithExpectedVersion update: %w", s.u.dialect.Name(), err)
		}
		n, _ := res.RowsAffected()
		return n == 1, nil
	}
	_, err = s.u.exec(ctx,
		`INSERT INTO leases(id,run_id,task_id,holder_id,status,version,payload,expires_at) VALUES(?,?,?,?,?,?,?,?)`,
		newLease.ID, newLease.RunID, newLease.TaskID, newLease.HolderID,
		string(newLease.Status), int64(newLease.Version), payload, unixNano(newLease.ExpiresAt))
	if err != nil {
		// Two writers racing on expectedVersion==0 both think the row is
		// absent; the loser hits a unique-key violation. Treat that as a
		// CAS miss (false, nil) to honor the LeaseStore contract.
		if s.u.dialect.IsDuplicateKey(err) {
			return false, nil
		}
		return false, fmt.Errorf("%s: AcquireWithExpectedVersion insert: %w", s.u.dialect.Name(), err)
	}
	return true, nil
}

func (s *leaseStore) ExtendLease(ctx context.Context, leaseID, workerID string, newExpiry time.Time) (bool, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return false, err
	}
	// Reject already-expired leases — once expires_at <= now, the lease is
	// transferable and the current holder no longer owns it. The UPDATE's
	// expires_at > ? clause makes this check atomic with the extension.
	res, err := s.u.exec(ctx,
		`UPDATE leases SET expires_at=? WHERE id=? AND holder_id=? AND status=? AND expires_at>?`,
		unixNano(newExpiry), leaseID, workerID, string(api.LeaseStatusActive), unixNano(time.Now().UTC()))
	if err != nil {
		return false, fmt.Errorf("%s: ExtendLease: %w", s.u.dialect.Name(), err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return false, nil
	}
	// Refresh in-payload Expiry too.
	var payload string
	if err := tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT payload FROM leases WHERE id=?`), leaseID).Scan(&payload); err != nil {
		return false, fmt.Errorf("%s: ExtendLease reload: %w", s.u.dialect.Name(), err)
	}
	var l api.TaskExecutionLease
	if err := unmarshalJSON(payload, &l); err != nil {
		return false, err
	}
	l.ExpiresAt = newExpiry
	newPayload, mErr := marshalJSON(l)
	if mErr != nil {
		return false, mErr
	}
	if _, err := s.u.exec(ctx, `UPDATE leases SET payload=? WHERE id=?`, newPayload, leaseID); err != nil {
		return false, fmt.Errorf("%s: ExtendLease payload: %w", s.u.dialect.Name(), err)
	}
	return true, nil
}

// ─── ApprovalStore ─────────────────────────────────────────────────────

type approvalStore struct{ u *UnitOfWork }

func (s *approvalStore) SaveApproval(ctx context.Context, a api.ApprovalRequest) error {
	payload, err := marshalJSON(a)
	if err != nil {
		return err
	}
	q := s.u.upsert("approvals",
		[]string{"id", "status", "payload"},
		[]string{"id"},
		[]string{"status", "payload"})
	if _, err := s.u.exec(ctx, q, a.ApprovalID, a.Status, payload); err != nil {
		return fmt.Errorf("%s: SaveApproval: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *approvalStore) LoadApproval(ctx context.Context, id string) (api.ApprovalRequest, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return api.ApprovalRequest{}, err
	}
	var payload string
	err = tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT payload FROM approvals WHERE id=?`), id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return api.ApprovalRequest{}, fmt.Errorf("%s: LoadApproval: not found", s.u.dialect.Name())
	}
	if err != nil {
		return api.ApprovalRequest{}, fmt.Errorf("%s: LoadApproval: %w", s.u.dialect.Name(), err)
	}
	var out api.ApprovalRequest
	if err := unmarshalJSON(payload, &out); err != nil {
		return api.ApprovalRequest{}, err
	}
	return out, nil
}

// ─── ResumeTokenStore ──────────────────────────────────────────────────

type resumeTokenStore struct{ u *UnitOfWork }

func (s *resumeTokenStore) SaveResumeToken(ctx context.Context, t api.ResumeToken) error {
	payload, err := marshalJSON(t)
	if err != nil {
		return err
	}
	status := string(t.ResumeRunState)
	consumed := 0
	if t.Metadata["consumed"] == "true" {
		consumed = 1
	}
	q := s.u.upsert("resume_tokens",
		[]string{"id", "run_id", "task_id", "status", "consumed", "payload", "created_at"},
		[]string{"id"},
		[]string{"status", "consumed", "payload"})
	if _, err := s.u.exec(ctx, q, t.TokenID, t.RunID, t.TaskID, status, consumed, payload, time.Now().UTC().UnixNano()); err != nil {
		return fmt.Errorf("%s: SaveResumeToken: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *resumeTokenStore) LoadResumeToken(ctx context.Context, id string) (api.ResumeToken, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return api.ResumeToken{}, err
	}
	var payload string
	err = tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT payload FROM resume_tokens WHERE id=?`), id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return api.ResumeToken{}, fmt.Errorf("%s: LoadResumeToken: not found", s.u.dialect.Name())
	}
	if err != nil {
		return api.ResumeToken{}, fmt.Errorf("%s: LoadResumeToken: %w", s.u.dialect.Name(), err)
	}
	var out api.ResumeToken
	if err := unmarshalJSON(payload, &out); err != nil {
		return api.ResumeToken{}, err
	}
	return out, nil
}

func (s *resumeTokenStore) ListPending(ctx context.Context, sel api.ResumeTokenSelector) ([]api.ResumeToken, error) {
	q := `SELECT payload FROM resume_tokens WHERE consumed=0`
	var args []any
	if sel.RunID != "" {
		q += ` AND run_id=?`
		args = append(args, sel.RunID)
	}
	if sel.TaskID != "" {
		q += ` AND task_id=?`
		args = append(args, sel.TaskID)
	}
	if len(sel.Statuses) > 0 {
		q += ` AND status IN (` + Placeholders(len(sel.Statuses)) + `)`
		for _, st := range sel.Statuses {
			args = append(args, st)
		}
	}
	q += ` ORDER BY created_at ASC, id ASC`
	if sel.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, sel.Limit)
	}
	rows, err := s.u.query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: ListPending: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	var out []api.ResumeToken
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var t api.ResumeToken
		if err := unmarshalJSON(payload, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ─── ActionAttemptStore ────────────────────────────────────────────────

type actionAttemptStore struct{ u *UnitOfWork }

func (s *actionAttemptStore) SaveActionAttempt(ctx context.Context, a api.ActionAttempt) error {
	payload, err := marshalJSON(a)
	if err != nil {
		return err
	}
	q := s.u.upsert("action_attempts",
		[]string{"id", "run_id", "task_id", "payload"},
		[]string{"id"},
		[]string{"payload"})
	if _, err := s.u.exec(ctx, q, a.AttemptID, a.RunID, a.TaskID, payload); err != nil {
		return fmt.Errorf("%s: SaveActionAttempt: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *actionAttemptStore) LoadActionAttempt(ctx context.Context, id string) (api.ActionAttempt, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return api.ActionAttempt{}, err
	}
	var payload string
	err = tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT payload FROM action_attempts WHERE id=?`), id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return api.ActionAttempt{}, fmt.Errorf("%s: LoadActionAttempt: not found", s.u.dialect.Name())
	}
	if err != nil {
		return api.ActionAttempt{}, fmt.Errorf("%s: LoadActionAttempt: %w", s.u.dialect.Name(), err)
	}
	var out api.ActionAttempt
	if err := unmarshalJSON(payload, &out); err != nil {
		return api.ActionAttempt{}, err
	}
	return out, nil
}

// ─── AgentProfileStore ─────────────────────────────────────────────────

type agentProfileStore struct{ u *UnitOfWork }

func (s *agentProfileStore) SaveAgentProfile(ctx context.Context, p api.AgentProfile) error {
	payload, err := marshalJSON(p)
	if err != nil {
		return err
	}
	q := s.u.upsert("agent_profiles",
		[]string{"id", "role", "payload"},
		[]string{"id"},
		[]string{"role", "payload"})
	if _, err := s.u.exec(ctx, q, p.ID, p.Role, payload); err != nil {
		return fmt.Errorf("%s: SaveAgentProfile: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *agentProfileStore) LoadAgentProfile(ctx context.Context, id string) (api.AgentProfile, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return api.AgentProfile{}, err
	}
	var payload string
	err = tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT payload FROM agent_profiles WHERE id=?`), id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return api.AgentProfile{}, fmt.Errorf("%s: LoadAgentProfile: not found", s.u.dialect.Name())
	}
	if err != nil {
		return api.AgentProfile{}, fmt.Errorf("%s: LoadAgentProfile: %w", s.u.dialect.Name(), err)
	}
	var out api.AgentProfile
	if err := unmarshalJSON(payload, &out); err != nil {
		return api.AgentProfile{}, err
	}
	return out, nil
}

func (s *agentProfileStore) ListAgentProfiles(ctx context.Context, sel api.AgentSelector) ([]api.AgentProfile, error) {
	q := `SELECT payload FROM agent_profiles WHERE 1=1`
	var args []any
	if len(sel.IDs) > 0 {
		q += ` AND id IN (` + Placeholders(len(sel.IDs)) + `)`
		for _, id := range sel.IDs {
			args = append(args, id)
		}
	}
	if len(sel.Roles) > 0 {
		q += ` AND role IN (` + Placeholders(len(sel.Roles)) + `)`
		for _, r := range sel.Roles {
			args = append(args, r)
		}
	}
	q += ` ORDER BY id ASC`
	if sel.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, sel.Limit)
	}
	rows, err := s.u.query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: ListAgentProfiles: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	var out []api.AgentProfile
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var p api.AgentProfile
		if err := unmarshalJSON(payload, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ─── CapabilityStore ───────────────────────────────────────────────────

type capabilityStore struct{ u *UnitOfWork }

func (s *capabilityStore) SaveCapability(ctx context.Context, c api.Capability) error {
	payload, err := marshalJSON(c)
	if err != nil {
		return err
	}
	q := s.u.upsert("capabilities",
		[]string{"name", "agent_id", "payload"},
		[]string{"name", "agent_id"},
		[]string{"payload"})
	if _, err := s.u.exec(ctx, q, c.Name, c.AgentID, payload); err != nil {
		return fmt.Errorf("%s: SaveCapability: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *capabilityStore) LoadCapability(ctx context.Context, name, agentID string) (api.Capability, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return api.Capability{}, err
	}
	var payload string
	err = tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT payload FROM capabilities WHERE name=? AND agent_id=?`), name, agentID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return api.Capability{}, fmt.Errorf("%s: LoadCapability: not found", s.u.dialect.Name())
	}
	if err != nil {
		return api.Capability{}, fmt.Errorf("%s: LoadCapability: %w", s.u.dialect.Name(), err)
	}
	var out api.Capability
	if err := unmarshalJSON(payload, &out); err != nil {
		return api.Capability{}, err
	}
	return out, nil
}

func (s *capabilityStore) ListCapabilities(ctx context.Context, sel api.CapabilitySelector) ([]api.Capability, error) {
	q := `SELECT payload FROM capabilities WHERE 1=1`
	var args []any
	if len(sel.Names) > 0 {
		q += ` AND name IN (` + Placeholders(len(sel.Names)) + `)`
		for _, n := range sel.Names {
			args = append(args, n)
		}
	}
	if len(sel.AgentIDs) > 0 {
		q += ` AND agent_id IN (` + Placeholders(len(sel.AgentIDs)) + `)`
		for _, id := range sel.AgentIDs {
			args = append(args, id)
		}
	}
	q += ` ORDER BY name ASC, agent_id ASC`
	if sel.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, sel.Limit)
	}
	rows, err := s.u.query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: ListCapabilities: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	var out []api.Capability
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var c api.Capability
		if err := unmarshalJSON(payload, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ─── UsageStore ────────────────────────────────────────────────────────

type usageStore struct{ u *UnitOfWork }

func (s *usageStore) AppendUsage(ctx context.Context, r api.UsageRecord) error {
	if r.ID == "" {
		// Use a transient placeholder length; the actual marshaled payload
		// reflects whatever id is assigned here, so QueryUsage sees the
		// same value when it unmarshals payload back into a UsageRecord.
		r.ID = fmt.Sprintf("usage-%d", time.Now().UnixNano())
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	payload, err := marshalJSON(r)
	if err != nil {
		return err
	}
	q := s.u.upsert("usage_records",
		[]string{"id", "run_id", "task_id", "agent_id", "provider", "credits", "payload", "created_at"},
		[]string{"id"},
		[]string{"payload"})
	if _, err := s.u.exec(ctx, q, r.ID, r.RunID, r.TaskID, r.AgentID, r.Provider, r.Credits, payload, unixNano(r.CreatedAt)); err != nil {
		return fmt.Errorf("%s: AppendUsage: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *usageStore) QueryUsage(ctx context.Context, sel api.UsageSelector) ([]api.UsageRecord, error) {
	q := `SELECT payload FROM usage_records WHERE 1=1`
	args := buildUsageFilters(&q, sel)
	q += ` ORDER BY created_at ASC`
	if sel.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, sel.Limit)
	}
	rows, err := s.u.query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: QueryUsage: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	var out []api.UsageRecord
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var r api.UsageRecord
		if err := unmarshalJSON(payload, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *usageStore) SumCredits(ctx context.Context, sel api.UsageSelector) (int64, error) {
	tx, err := s.u.txOrErr()
	if err != nil {
		return 0, err
	}
	q := `SELECT COALESCE(SUM(credits),0) FROM usage_records WHERE 1=1`
	args := buildUsageFilters(&q, sel)
	var total sql.NullInt64
	if err := tx.QueryRowContext(ctx, s.u.dialect.Rebind(q), args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("%s: SumCredits: %w", s.u.dialect.Name(), err)
	}
	return total.Int64, nil
}

func buildUsageFilters(q *string, sel api.UsageSelector) []any {
	var args []any
	if sel.RunID != "" {
		*q += ` AND run_id=?`
		args = append(args, sel.RunID)
	}
	if sel.TaskID != "" {
		*q += ` AND task_id=?`
		args = append(args, sel.TaskID)
	}
	if sel.AgentID != "" {
		*q += ` AND agent_id=?`
		args = append(args, sel.AgentID)
	}
	if sel.Provider != "" {
		*q += ` AND provider=?`
		args = append(args, sel.Provider)
	}
	if !sel.Since.IsZero() {
		*q += ` AND created_at>=?`
		args = append(args, unixNano(sel.Since))
	}
	if !sel.Until.IsZero() {
		*q += ` AND created_at<=?`
		args = append(args, unixNano(sel.Until))
	}
	return args
}

// ─── DeadLetterStore ───────────────────────────────────────────────────

type deadLetterStore struct{ u *UnitOfWork }

func (s *deadLetterStore) AppendDeadLetter(ctx context.Context, e api.DeadLetterEntry) error {
	payload, err := marshalJSON(e)
	if err != nil {
		return err
	}
	id := e.ID
	if id == "" {
		id = fmt.Sprintf("dl-%s-%d", e.EnvelopeID, time.Now().UnixNano())
	}
	createdAt := e.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	q := s.u.upsert("dead_letters",
		[]string{"id", "envelope_id", "run_id", "task_id", "payload", "created_at"},
		[]string{"id"},
		[]string{"payload"})
	if _, err := s.u.exec(ctx, q, id, e.EnvelopeID, e.RunID, e.TaskID, payload, unixNano(createdAt)); err != nil {
		return fmt.Errorf("%s: AppendDeadLetter: %w", s.u.dialect.Name(), err)
	}
	return nil
}

func (s *deadLetterStore) ListDeadLetters(ctx context.Context, sel api.DeadLetterSelector) ([]api.DeadLetterEntry, error) {
	q := `SELECT payload FROM dead_letters WHERE 1=1`
	var args []any
	if sel.RunID != "" {
		q += ` AND run_id=?`
		args = append(args, sel.RunID)
	}
	if sel.TaskID != "" {
		q += ` AND task_id=?`
		args = append(args, sel.TaskID)
	}
	if !sel.Since.IsZero() {
		q += ` AND created_at>=?`
		args = append(args, unixNano(sel.Since))
	}
	if !sel.Until.IsZero() {
		q += ` AND created_at<=?`
		args = append(args, unixNano(sel.Until))
	}
	q += ` ORDER BY created_at ASC`
	if sel.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, sel.Limit)
	}
	rows, err := s.u.query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: ListDeadLetters: %w", s.u.dialect.Name(), err)
	}
	defer rows.Close()
	var out []api.DeadLetterEntry
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var e api.DeadLetterEntry
		if err := unmarshalJSON(payload, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *deadLetterStore) Requeue(ctx context.Context, deadLetterID string) error {
	tx, err := s.u.txOrErr()
	if err != nil {
		return err
	}
	var payload string
	err = tx.QueryRowContext(ctx, s.u.dialect.Rebind(`SELECT payload FROM dead_letters WHERE id=?`), deadLetterID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: Requeue %q: not found", s.u.dialect.Name(), deadLetterID)
	}
	if err != nil {
		return fmt.Errorf("%s: Requeue: %w", s.u.dialect.Name(), err)
	}
	var e api.DeadLetterEntry
	if err := unmarshalJSON(payload, &e); err != nil {
		return err
	}
	if err := (&mailboxStore{u: s.u}).QueueEnvelope(ctx, e.Envelope); err != nil {
		return fmt.Errorf("%s: Requeue mailbox: %w", s.u.dialect.Name(), err)
	}
	if _, err := s.u.exec(ctx, `DELETE FROM dead_letters WHERE id=?`, deadLetterID); err != nil {
		return fmt.Errorf("%s: Requeue delete: %w", s.u.dialect.Name(), err)
	}
	return nil
}
