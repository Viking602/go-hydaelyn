package run

import (
	"context"

	commandbus "github.com/Viking602/go-hydaelyn/internal/command"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

// StartRunResult is the typed result returned by StartRunCommand. Replacing the
// previous []any tuple keeps multi-value returns type-safe across the
// command-bus boundary.
type StartRunResult struct {
	Run  model.Run
	Root model.Task
}

func RegisterHandlers(bus *commandbus.Bus, newID IDGenerator) {
	commandbus.Register[StartRunCommand](bus, startRunHandler{newID: newID})
	commandbus.Register[CreateTaskCommand](bus, createTaskHandler{newID: newID})
}

type startRunHandler struct {
	newID IDGenerator
}

func (h startRunHandler) Name() string { return StartRunCommand{}.CommandName() }

func (h startRunHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd StartRunCommand) (any, error) {
	run, root, err := Start(ctx, uow, h.newID, StartInput(cmd))
	if err != nil {
		return nil, err
	}
	return StartRunResult{Run: run, Root: root}, nil
}

type createTaskHandler struct {
	newID IDGenerator
}

func (h createTaskHandler) Name() string { return CreateTaskCommand{}.CommandName() }

func (h createTaskHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd CreateTaskCommand) (any, error) {
	return CreateTask(ctx, uow, h.newID, CreateTaskInput(cmd))
}
