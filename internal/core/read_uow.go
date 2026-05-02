package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func (r *Runtime) beginReadUoW(ctx context.Context) (ports.UnitOfWork, func(), error) {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return nil, nil, err
	}
	return uow, func() { _ = uow.Rollback(ctx) }, nil
}

func (r *Runtime) beginWriteUoW(ctx context.Context) (ports.UnitOfWork, error) {
	if r.storeProvider == nil {
		return r.memProvider.Begin(ctx)
	}
	return r.storeProvider.Begin(ctx)
}
