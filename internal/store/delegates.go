package store

import (
	"context"
	"fmt"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
)

// Options wires Delegates to the runtime's selected store provider without
// making this package depend on internal/core.
type Options struct {
	BeginWrite func(context.Context) (ports.UnitOfWork, error)
	BeginRead  func(context.Context) (ports.UnitOfWork, func() error, error)
}

// Delegates implements the store-facing runtime methods by delegating through
// the configured UnitOfWork boundary.
type Delegates struct {
	beginWrite func(context.Context) (ports.UnitOfWork, error)
	beginRead  func(context.Context) (ports.UnitOfWork, func() error, error)
}

func NewDelegates(options Options) *Delegates {
	return &Delegates{
		beginWrite: options.BeginWrite,
		beginRead:  options.BeginRead,
	}
}

func (d *Delegates) SaveRun(ctx context.Context, run model.Run) error {
	return d.withWrite(ctx, func(uow ports.UnitOfWork) error {
		return uow.Runs().SaveRun(ctx, run)
	})
}

func (d *Delegates) LoadRun(ctx context.Context, runID string) (model.Run, error) {
	uow, done, err := d.openRead(ctx)
	if err != nil {
		return model.Run{}, err
	}
	defer func() { _ = done() }()
	return uow.Runs().LoadRun(ctx, runID)
}

func (d *Delegates) SaveTask(ctx context.Context, task model.Task) error {
	return d.withWrite(ctx, func(uow ports.UnitOfWork) error {
		return uow.Tasks().SaveTask(ctx, task)
	})
}

func (d *Delegates) LoadTask(ctx context.Context, runID, taskID string) (model.Task, error) {
	uow, done, err := d.openRead(ctx)
	if err != nil {
		return model.Task{}, err
	}
	defer func() { _ = done() }()
	return uow.Tasks().LoadTask(ctx, runID, taskID)
}

func (d *Delegates) ListTasks(ctx context.Context, runID string) ([]model.Task, error) {
	uow, done, err := d.openRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = done() }()
	return uow.Tasks().ListTasks(ctx, runID)
}

func (d *Delegates) AppendEvent(ctx context.Context, event model.Event) error {
	return d.withWrite(ctx, func(uow ports.UnitOfWork) error {
		return uow.Events().AppendEvent(ctx, event)
	})
}

func (d *Delegates) ListEvents(ctx context.Context, runID string) ([]model.Event, error) {
	uow, done, err := d.openRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = done() }()
	return uow.Events().ListEvents(ctx, runID)
}

func (d *Delegates) SaveTraceSpan(ctx context.Context, span model.TraceSpan) error {
	return d.withWrite(ctx, func(uow ports.UnitOfWork) error {
		return uow.Trace().SaveTraceSpan(ctx, span)
	})
}

func (d *Delegates) ListTraceSpans(ctx context.Context, runID string) ([]model.TraceSpan, error) {
	uow, done, err := d.openRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = done() }()
	return uow.Trace().ListTraceSpans(ctx, runID)
}

func (d *Delegates) QueueMessage(ctx context.Context, message model.UserMessage) error {
	return d.withWrite(ctx, func(uow ports.UnitOfWork) error {
		return uow.UserMessages().QueueMessage(ctx, message)
	})
}

func (d *Delegates) LoadMessage(ctx context.Context, runID, messageID string) (model.UserMessage, error) {
	uow, done, err := d.openRead(ctx)
	if err != nil {
		return model.UserMessage{}, err
	}
	defer func() { _ = done() }()
	return uow.UserMessages().LoadMessage(ctx, runID, messageID)
}

func (d *Delegates) UpdateMessage(ctx context.Context, message model.UserMessage) error {
	return d.withWrite(ctx, func(uow ports.UnitOfWork) error {
		return uow.UserMessages().UpdateMessage(ctx, message)
	})
}

func (d *Delegates) ListMessages(ctx context.Context, runID string) ([]model.UserMessage, error) {
	uow, done, err := d.openRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = done() }()
	return uow.UserMessages().ListMessages(ctx, runID)
}

func (d *Delegates) ListQueuedMessages(ctx context.Context) ([]model.UserMessage, error) {
	uow, done, err := d.openRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = done() }()
	scanner, ok := uow.UserMessages().(ports.UserMessageOutboxScanner)
	if !ok {
		return nil, fmt.Errorf("user message store does not support queued outbox scanning: %w", model.ErrInvalidConfiguration)
	}
	return scanner.ListQueuedMessages(ctx)
}

func (d *Delegates) QueueEnvelope(ctx context.Context, env model.TaskEnvelope) error {
	return d.withWrite(ctx, func(uow ports.UnitOfWork) error {
		return uow.MailboxOutbox().QueueEnvelope(ctx, env)
	})
}

func (d *Delegates) LoadEnvelope(ctx context.Context, envelopeID string) (model.TaskEnvelope, error) {
	uow, done, err := d.openRead(ctx)
	if err != nil {
		return model.TaskEnvelope{}, err
	}
	defer func() { _ = done() }()
	return uow.MailboxOutbox().LoadEnvelope(ctx, envelopeID)
}

func (d *Delegates) UpdateEnvelope(ctx context.Context, env model.TaskEnvelope) error {
	return d.withWrite(ctx, func(uow ports.UnitOfWork) error {
		return uow.MailboxOutbox().UpdateEnvelope(ctx, env)
	})
}

func (d *Delegates) ListEnvelopes(ctx context.Context, runID string) ([]model.TaskEnvelope, error) {
	uow, done, err := d.openRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = done() }()
	return uow.MailboxOutbox().ListEnvelopes(ctx, runID)
}

func (d *Delegates) withWrite(ctx context.Context, fn func(ports.UnitOfWork) error) error {
	uow, err := d.openWrite(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	if err := fn(uow); err != nil {
		return err
	}
	if err := uow.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (d *Delegates) openWrite(ctx context.Context) (ports.UnitOfWork, error) {
	if d == nil || d.beginWrite == nil {
		return nil, fmt.Errorf("store delegates missing write unit of work: %w", model.ErrInvalidConfiguration)
	}
	return d.beginWrite(ctx)
}

func (d *Delegates) openRead(ctx context.Context) (ports.UnitOfWork, func() error, error) {
	if d == nil {
		return nil, nil, fmt.Errorf("store delegates missing read unit of work: %w", model.ErrInvalidConfiguration)
	}
	if d.beginRead != nil {
		return d.beginRead(ctx)
	}
	uow, err := d.openWrite(ctx)
	if err != nil {
		return nil, nil, err
	}
	return uow, func() error { return uow.Rollback(ctx) }, nil
}
