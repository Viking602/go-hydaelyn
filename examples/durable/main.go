package main

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/durable"
	"github.com/Viking602/venat/provider"
)

type uncertainStream struct {
	sent bool
}

func (stream *uncertainStream) Recv() (provider.Event, error) {
	if !stream.sent {
		stream.sent = true
		return provider.Event{Kind: provider.EventTextDelta, Text: "partial"}, nil
	}
	return provider.Event{}, errors.New("connection closed after request dispatch")
}

func (*uncertainStream) Close() error { return nil }

type recoveryProvider struct {
	calls atomic.Int32
}

func (*recoveryProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "durable-example"}
}

func (driver *recoveryProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	if driver.calls.Add(1) == 1 {
		return &uncertainStream{}, nil
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "resumed safely"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func main() {
	ctx := context.Background()
	store := newBackend()
	driver := &recoveryProvider{}
	engine := agent.Engine{Provider: driver, Model: "example-model"}

	first, err := durable.New(store, durable.Options{OwnerID: "process-one"})
	if err != nil {
		panic(err)
	}
	_, runErr := first.Start(ctx, "example-run", engine, agent.Request{Prompt: "recover this"}, agent.OutputPolicy{})
	var required *durable.ReconcileRequiredError
	if !errors.As(runErr, &required) || len(required.Attempts) != 1 {
		panic(fmt.Errorf("expected one unknown attempt: %w", runErr))
	}
	if err := first.Close(ctx); err != nil {
		panic(err)
	}

	// A new Backend handle and Runtime simulate a reopened process over the same
	// application-owned persistent state. The application explicitly chooses to
	// retry the ambiguous provider effect before resuming.
	second, err := durable.New(store.reopen(), durable.Options{OwnerID: "process-two"})
	if err != nil {
		panic(err)
	}
	attempt := required.Attempts[0]
	if err := second.Reconcile(ctx, "example-run", attempt.OperationID, durable.Reconciliation{
		AttemptNumber:  attempt.Number,
		AttemptVersion: attempt.Version,
		Resolution:     durable.ReconcileResolutionRetry,
	}); err != nil {
		panic(err)
	}
	result, err := second.Resume(ctx, "example-run", engine)
	if err != nil {
		panic(err)
	}
	if result.Failure != nil {
		panic(result.Failure)
	}
	fmt.Printf("durable: unknown->reconcile(retry)->resume; provider calls=%d; text=%q\n", driver.calls.Load(), result.Text)
	if err := runApprovalDemo(ctx); err != nil {
		panic(err)
	}
}
