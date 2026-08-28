package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
)

// Publication is an external side effect: two callers racing to publish the
// same message must produce exactly one gateway call. The claim that makes
// that true is the Queued → Publishing transition, which commits before the
// gateway is invoked.
func TestPublishResponseCallsGatewayExactlyOnceUnderConcurrency(t *testing.T) {
	t.Run("second publisher is rejected while the gateway call is in flight", func(t *testing.T) {
		ctx := context.Background()
		rt := NewMemoryRuntime()
		run, message := seedQueuedResponse(ctx, t, rt, "run-publish-inflight")

		gateway := &blockingGateway{entered: make(chan struct{}), release: make(chan struct{})}
		rt.SetOutputGateway(gateway)

		first := make(chan error, 1)
		go func() {
			first <- rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID})
		}()

		select {
		case <-gateway.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("gateway was never called")
		}

		inFlight := mustResponseOutbox(ctx, t, rt, run.ID)[0]
		if inFlight.Status != UserMessagePublishing {
			t.Fatalf("message status during gateway call = %q, want %q", inFlight.Status, UserMessagePublishing)
		}

		err := rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID})
		if !errors.Is(err, ErrResponsePublishInFlight) {
			t.Fatalf("second PublishResponse() error = %v, want ErrResponsePublishInFlight", err)
		}
		if !errors.Is(err, api.ErrResponsePublishInFlight) {
			t.Fatalf("in-flight rejection is not detectable through the public api sentinel: %v", err)
		}

		close(gateway.release)
		if err := <-first; err != nil {
			t.Fatalf("first PublishResponse() error = %v", err)
		}
		if got := gateway.calls.Load(); got != 1 {
			t.Fatalf("gateway published %d times, want exactly 1", got)
		}
		if published := mustResponseOutbox(ctx, t, rt, run.ID)[0]; published.Status != UserMessagePublished {
			t.Fatalf("final message = %#v, want published", published)
		}
	})

	t.Run("concurrent publishers", func(t *testing.T) {
		for _, publishers := range []int{2, 8} {
			ctx := context.Background()
			rt := NewMemoryRuntime()
			run, message := seedQueuedResponse(ctx, t, rt, "run-publish-race")
			gateway := &countingGateway{}
			rt.SetOutputGateway(gateway)

			var wg sync.WaitGroup
			errs := make([]error, publishers)
			start := make(chan struct{})
			for i := range publishers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					errs[i] = rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID})
				}()
			}
			close(start)
			wg.Wait()

			if got := gateway.calls.Load(); got != 1 {
				t.Fatalf("%d concurrent publishers produced %d gateway calls, want exactly 1", publishers, got)
			}
			succeeded := 0
			for _, err := range errs {
				switch {
				case err == nil:
					succeeded++
				case errors.Is(err, ErrResponsePublishInFlight):
				default:
					t.Fatalf("PublishResponse() error = %v, want nil or ErrResponsePublishInFlight", err)
				}
			}
			if succeeded == 0 {
				t.Fatalf("%d concurrent publishers: none reported success", publishers)
			}
			if published := mustResponseOutbox(ctx, t, rt, run.ID)[0]; published.Status != UserMessagePublished {
				t.Fatalf("final message = %#v, want published", published)
			}
		}
	})
}

func TestPublishResponseReportsIdempotentNoop(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, message := seedQueuedResponse(ctx, t, rt, "run-publish-noop")
	gateway := &countingGateway{}
	rt.SetOutputGateway(gateway)

	didPublish, err := rt.publishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID})
	if err != nil || !didPublish {
		t.Fatalf("first publish = %t, %v, want true, nil", didPublish, err)
	}
	didPublish, err = rt.publishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID})
	if err != nil || didPublish {
		t.Fatalf("idempotent publish = %t, %v, want false, nil", didPublish, err)
	}
	if gateway.calls.Load() != 1 {
		t.Fatalf("gateway calls = %d, want 1", gateway.calls.Load())
	}
}

// A gateway error does not prove non-delivery: the external side effect may
// have happened before the error was returned. The claim therefore remains in
// Publishing until a host reconciles the delivery outcome.
func TestPublishResponseGatewayFailureLeavesMessageInDoubt(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, message := seedQueuedResponse(ctx, t, rt, "run-publish-unknown")
	rt.SetOutputGateway(failingGateway{})

	if err := rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID}); !errors.Is(err, errPublishFailed) {
		t.Fatalf("PublishResponse(failing gateway) error = %v, want %v", err, errPublishFailed)
	}
	after := mustResponseOutbox(ctx, t, rt, run.ID)[0]
	if after.Status != UserMessagePublishing {
		t.Fatalf("message after gateway failure = %#v, want publishing", after)
	}

	gateway := &countingGateway{}
	rt.SetOutputGateway(gateway)
	drained, err := rt.DrainResponseOutbox(ctx)
	if err != nil {
		t.Fatalf("DrainResponseOutbox() error = %v", err)
	}
	if drained != 0 || gateway.calls.Load() != 0 {
		t.Fatalf("drain published %d messages with %d gateway calls, want 0/0", drained, gateway.calls.Load())
	}
}

// A message a crash left in Publishing is in doubt: the gateway may already
// have delivered it. The outbox must not republish it on its own.
func TestDrainResponseOutboxSkipsMessageLeftMidPublication(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, message := seedQueuedResponse(ctx, t, rt, "run-publish-indoubt")
	markMessagePublishing(ctx, t, rt, run.ID, message.ID)

	gateway := &countingGateway{}
	rt.SetOutputGateway(gateway)
	drained, err := rt.DrainResponseOutbox(ctx)
	if err != nil {
		t.Fatalf("DrainResponseOutbox() error = %v", err)
	}
	if drained != 0 || gateway.calls.Load() != 0 {
		t.Fatalf("drain published %d messages with %d gateway calls, want 0/0", drained, gateway.calls.Load())
	}
	if err := rt.PublishResponse(ctx, PublishResponseCommand{RunID: run.ID, MessageID: message.ID}); !errors.Is(err, ErrResponsePublishInFlight) {
		t.Fatalf("PublishResponse(in doubt) error = %v, want ErrResponsePublishInFlight", err)
	}
}

// A message stranded in publishing is the one state the runtime cannot
// resolve alone. ReconcileResponsePublication is how the host settles it.
func TestReconcileResponsePublication(t *testing.T) {
	t.Run("delivered marks published without recalling the gateway", func(t *testing.T) {
		ctx := context.Background()
		rt := NewMemoryRuntime()
		run, message := seedQueuedResponse(ctx, t, rt, "run-reconcile-delivered")
		markMessagePublishing(ctx, t, rt, run.ID, message.ID)
		gateway := &countingGateway{}
		rt.SetOutputGateway(gateway)

		settled, err := rt.ReconcileResponsePublication(ctx, ReconcileResponsePublicationCommand{
			RunID: run.ID, MessageID: message.ID, Delivered: true, Reason: "found in provider delivery log",
		})
		if err != nil {
			t.Fatalf("ReconcileResponsePublication(delivered) error = %v", err)
		}
		if settled.Status != UserMessagePublished {
			t.Fatalf("reconciled message = %#v, want published", settled)
		}
		if settled.PublishedAt.IsZero() {
			t.Fatal("reconciled message has no PublishedAt")
		}
		if gateway.calls.Load() != 0 {
			t.Fatalf("reconciliation called the gateway %d times, want 0", gateway.calls.Load())
		}
		if !collectEventTypes(rt.Events(ctx, run.ID)).Contains(EventResponsePublished) {
			t.Fatal("reconciliation did not record a ResponsePublished event")
		}
	})

	t.Run("undelivered returns the message to the outbox", func(t *testing.T) {
		ctx := context.Background()
		rt := NewMemoryRuntime()
		run, message := seedQueuedResponse(ctx, t, rt, "run-reconcile-undelivered")
		markMessagePublishing(ctx, t, rt, run.ID, message.ID)

		settled, err := rt.ReconcileResponsePublication(ctx, ReconcileResponsePublicationCommand{
			RunID: run.ID, MessageID: message.ID, Delivered: false, Reason: "absent from provider delivery log",
		})
		if err != nil {
			t.Fatalf("ReconcileResponsePublication(undelivered) error = %v", err)
		}
		if settled.Status != UserMessageQueued {
			t.Fatalf("reconciled message = %#v, want queued", settled)
		}
		if !collectEventTypes(rt.Events(ctx, run.ID)).Contains(EventResponsePublishFailed) {
			t.Fatal("reconciliation did not record a ResponsePublishFailed event")
		}

		gateway := &countingGateway{}
		rt.SetOutputGateway(gateway)
		drained, err := rt.DrainResponseOutbox(ctx)
		if err != nil {
			t.Fatalf("DrainResponseOutbox() error = %v", err)
		}
		if drained != 1 || gateway.calls.Load() != 1 {
			t.Fatalf("drain after reconciliation = %d messages / %d calls, want 1/1", drained, gateway.calls.Load())
		}
	})

	t.Run("repeating a determination is a no-op, reversing it conflicts", func(t *testing.T) {
		tests := []struct {
			name      string
			delivered bool
			want      UserMessageStatus
		}{
			{name: "delivered", delivered: true, want: UserMessagePublished},
			{name: "undelivered", delivered: false, want: UserMessageQueued},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				ctx := context.Background()
				rt := NewMemoryRuntime()
				run, message := seedQueuedResponse(ctx, t, rt, "run-reconcile-repeat")
				markMessagePublishing(ctx, t, rt, run.ID, message.ID)
				cmd := ReconcileResponsePublicationCommand{RunID: run.ID, MessageID: message.ID, Delivered: test.delivered}

				if _, err := rt.ReconcileResponsePublication(ctx, cmd); err != nil {
					t.Fatalf("first ReconcileResponsePublication() error = %v", err)
				}
				repeated, err := rt.ReconcileResponsePublication(ctx, cmd)
				if err != nil {
					t.Fatalf("repeated ReconcileResponsePublication() error = %v, want a no-op", err)
				}
				if repeated.Status != test.want {
					t.Fatalf("repeated reconciliation status = %q, want %q", repeated.Status, test.want)
				}

				reversed := cmd
				reversed.Delivered = !cmd.Delivered
				if _, err := rt.ReconcileResponsePublication(ctx, reversed); !errors.Is(err, api.ErrIdempotencyConflict) {
					t.Fatalf("reversed determination error = %v, want ErrIdempotencyConflict", err)
				}
			})
		}
	})

	t.Run("rejects a message that was never claimed", func(t *testing.T) {
		ctx := context.Background()
		rt := NewMemoryRuntime()
		run, message := seedQueuedResponse(ctx, t, rt, "run-reconcile-unclaimed")

		// The message is queued and no publish is in flight, so "delivered"
		// cannot be true of it.
		if _, err := rt.ReconcileResponsePublication(ctx, ReconcileResponsePublicationCommand{
			RunID: run.ID, MessageID: message.ID, Delivered: true,
		}); !errors.Is(err, api.ErrIdempotencyConflict) {
			t.Fatalf("reconciling an unclaimed message error = %v, want ErrIdempotencyConflict", err)
		}
	})

	t.Run("policy can deny reconciliation", func(t *testing.T) {
		ctx := context.Background()
		rt := NewMemoryRuntime()
		run, message := seedQueuedResponse(ctx, t, rt, "run-reconcile-policy")
		markMessagePublishing(ctx, t, rt, run.ID, message.ID)
		observed := false
		rt.SetPolicyEngine(obligationPolicyFunc(func(_ context.Context, request api.PolicyRequest) (api.PolicyDecision, error) {
			if request.Operation == api.PolicyOperationResponseReconcile {
				observed = true
				return api.PolicyDecision{Effect: api.PolicyEffectDeny, Reason: "manual settlement disabled"}, nil
			}
			return api.PolicyDecision{Effect: api.PolicyEffectAllow}, nil
		}))

		if _, err := rt.ReconcileResponsePublication(ctx, ReconcileResponsePublicationCommand{
			RunID: run.ID, MessageID: message.ID, Delivered: true,
		}); !errors.Is(err, api.ErrPolicyDenied) {
			t.Fatalf("ReconcileResponsePublication() error = %v, want ErrPolicyDenied", err)
		}
		if !observed {
			t.Fatal("policy engine did not observe response reconciliation")
		}
		if current := mustResponseOutbox(ctx, t, rt, run.ID)[0]; current.Status != UserMessagePublishing {
			t.Fatalf("denied reconciliation changed message status to %q", current.Status)
		}
	})

	t.Run("rejects an incomplete command", func(t *testing.T) {
		ctx := context.Background()
		rt := NewMemoryRuntime()
		if _, err := rt.ReconcileResponsePublication(ctx, ReconcileResponsePublicationCommand{MessageID: "msg-1"}); !errors.Is(err, api.ErrInvalidCommand) {
			t.Fatalf("ReconcileResponsePublication(no run) error = %v, want ErrInvalidCommand", err)
		}
	})
}

func seedQueuedResponse(ctx context.Context, t *testing.T, rt *Runtime, runID string) (Run, UserMessage) {
	t.Helper()
	run := mustStartRun(ctx, t, rt, runID)
	response := mustCreateTask(ctx, t, rt, CreateTaskCommand{RunID: run.ID, TaskID: "response", Type: TaskTypeResponse, OwnerComponent: "response_composer"})
	lease := leaseTask(ctx, t, rt, run.ID, response.ID, HolderComponent, "response_composer")
	if err := rt.SubmitResponseOutput(ctx, SubmitResponseOutputCommand{
		RunID:       run.ID,
		TaskID:      response.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderComponent,
		HolderID:    "response_composer",
		TaskVersion: response.Version,
		Payload:     "hello",
	}); err != nil {
		t.Fatalf("SubmitResponseOutput() error = %v", err)
	}
	return run, mustResponseOutbox(ctx, t, rt, run.ID)[0]
}

// markMessagePublishing stands in for a process that died between claiming
// a message and recording the gateway outcome.
func markMessagePublishing(ctx context.Context, t *testing.T, rt *Runtime, runID, messageID string) {
	t.Helper()
	uow, err := rt.beginWriteUoW(ctx)
	if err != nil {
		t.Fatalf("beginWriteUoW() error = %v", err)
	}
	message, err := uow.UserMessages().LoadMessage(ctx, runID, messageID)
	if err != nil {
		_ = uow.Rollback(ctx)
		t.Fatalf("LoadMessage() error = %v", err)
	}
	message.Status = UserMessagePublishing
	if err := uow.UserMessages().UpdateMessage(ctx, message); err != nil {
		_ = uow.Rollback(ctx)
		t.Fatalf("UpdateMessage() error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
}

type countingGateway struct{ calls atomic.Int64 }

func (g *countingGateway) Publish(context.Context, api.UserMessage) error {
	g.calls.Add(1)
	return nil
}

type blockingGateway struct {
	calls   atomic.Int64
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (g *blockingGateway) Publish(context.Context, api.UserMessage) error {
	g.calls.Add(1)
	g.once.Do(func() { close(g.entered) })
	<-g.release
	return nil
}
