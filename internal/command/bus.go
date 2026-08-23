package command

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
)

type TypedHandler[C ports.Command] interface {
	Name() string
	Handle(context.Context, ports.UnitOfWork, C) (any, error)
}

type erasedHandler interface {
	Name() string
	HandleAny(context.Context, ports.UnitOfWork, ports.Command) (any, error)
}

type Bus struct {
	handlers map[string]erasedHandler
}

func NewBus() *Bus {
	return &Bus{handlers: map[string]erasedHandler{}}
}

func Register[C ports.Command](bus *Bus, handler TypedHandler[C]) {
	if bus.handlers == nil {
		bus.handlers = map[string]erasedHandler{}
	}
	bus.handlers[handler.Name()] = erased[C]{handler: handler}
}

func (b *Bus) HasHandler(name string) bool {
	if b == nil {
		return false
	}
	_, ok := b.handlers[name]
	return ok
}

func (b *Bus) Execute(ctx context.Context, uow ports.UnitOfWork, cmd ports.Command) (any, error) {
	if b == nil || cmd == nil {
		return nil, api.ErrInvalidCommand
	}
	handler, ok := b.handlers[cmd.CommandName()]
	if !ok {
		return nil, api.ErrInvalidCommand
	}
	return handler.HandleAny(ctx, uow, cmd)
}

type erased[C ports.Command] struct {
	handler TypedHandler[C]
}

func (e erased[C]) Name() string { return e.handler.Name() }

func (e erased[C]) HandleAny(ctx context.Context, uow ports.UnitOfWork, cmd ports.Command) (any, error) {
	typed, ok := any(cmd).(C)
	if !ok {
		return nil, api.ErrInvalidCommand
	}
	return e.handler.Handle(ctx, uow, typed)
}
