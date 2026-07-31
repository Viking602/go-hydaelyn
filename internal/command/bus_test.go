package command

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
)

type testCommand struct{ Value string }

func (testCommand) CommandName() string { return "test.command" }

type otherCommand struct{}

func (otherCommand) CommandName() string { return "test.command" }

type testHandler struct{}

func (testHandler) Name() string { return "test.command" }

func (testHandler) Handle(_ context.Context, _ ports.UnitOfWork, cmd testCommand) (any, error) {
	return cmd.Value, nil
}

func TestBusExecutesTypedHandler(t *testing.T) {
	bus := NewBus()
	Register[testCommand](bus, testHandler{})
	got, err := bus.Execute(context.Background(), nil, testCommand{Value: "ok"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "ok" {
		t.Fatalf("Execute() = %v, want ok", got)
	}
}

func TestBusRejectsWrongCommandTypeWithoutPanic(t *testing.T) {
	bus := NewBus()
	Register[testCommand](bus, testHandler{})
	_, err := bus.Execute(context.Background(), nil, otherCommand{})
	if !errors.Is(err, model.ErrInvalidCommand) {
		t.Fatalf("Execute() error = %v, want ErrInvalidCommand", err)
	}
}
