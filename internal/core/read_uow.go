package core

import (
	"context"
	"errors"

	"github.com/Viking602/venat/internal/core/ports"
)

func (r *Runtime) beginReadUoW(ctx context.Context) (ports.UnitOfWork, func() error, error) {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return nil, nil, err
	}
	return uow, func() error { return uow.Rollback(ctx) }, nil
}

func joinReadCleanup(err *error, done func() error) {
	if done == nil {
		return
	}
	*err = errors.Join(*err, done())
}

func (r *Runtime) beginWriteUoW(ctx context.Context) (ports.UnitOfWork, error) {
	if r.storeProvider == nil {
		return r.memProvider.Begin(ctx)
	}
	return r.storeProvider.Begin(ctx)
}
