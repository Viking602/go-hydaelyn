package core

import (
	"context"

	"github.com/Viking602/venat/internal/core/model"
)

// AppendUsage persists one usage-metering record in its own unit of work.
// It is the write half of the UsageStore contract; the worker runtime
// calls it after every engine run so the metering ledger reflects real
// token consumption.
func (r *Runtime) AppendUsage(ctx context.Context, record model.UsageRecord) error {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	if err := uow.UsageRecords().AppendUsage(ctx, record); err != nil {
		return err
	}
	if err := uow.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

// QueryUsage returns the usage records matching sel.
func (r *Runtime) QueryUsage(ctx context.Context, sel model.UsageSelector) ([]model.UsageRecord, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return uow.UsageRecords().QueryUsage(ctx, sel)
}

// SumUsageCredits returns the credit sum over records matching sel.
func (r *Runtime) SumUsageCredits(ctx context.Context, sel model.UsageSelector) (int64, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return 0, err
	}
	defer done()
	return uow.UsageRecords().SumCredits(ctx, sel)
}
