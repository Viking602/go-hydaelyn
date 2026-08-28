package venat

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
)

// countingGateway records how many times a message reached the outside world.
type countingGateway struct{ calls int }

func (g *countingGateway) Publish(context.Context, api.UserMessage) error {
	g.calls++
	return nil
}

type refusingGateway struct{ err error }

func (g refusingGateway) Publish(context.Context, api.UserMessage) error { return g.err }

// A publish interrupted after its claim leaves the message in
// api.UserMessagePublishing, which the runtime deliberately will not resolve
// on its own. ReconcileResponsePublication is the host's way out, and it must
// be reachable from the root Runner.
func TestRunnerReconcileResponsePublication(t *testing.T) {
	errGateway := errors.New("gateway unreachable")

	t.Run("host confirms delivery", func(t *testing.T) {
		ctx := context.Background()
		r := NewDevelopment()
		run, _, err := r.StartRun(ctx, api.StartRunCommand{Request: "reconcile"})
		if err != nil {
			t.Fatalf("StartRun() error = %v", err)
		}
		queueResponse(ctx, t, r, run.ID, "message-1")
		stranded := strandPublication(ctx, t, r, run.ID, "message-1", errGateway)

		gateway := &countingGateway{}
		r.SetOutputGateway(gateway)
		settled, err := r.ReconcileResponsePublication(ctx, api.ReconcileResponsePublicationCommand{
			RunID: run.ID, MessageID: "message-1", Delivered: true, Reason: "confirmed downstream",
		})
		if err != nil {
			t.Fatalf("ReconcileResponsePublication() error = %v", err)
		}
		if stranded.Status != api.UserMessagePublishing {
			t.Fatalf("precondition: stranded message = %#v, want publishing", stranded)
		}
		if settled.Status != api.UserMessagePublished {
			t.Fatalf("settled message = %#v, want published", settled)
		}
		if gateway.calls != 0 {
			t.Fatalf("reconciliation re-sent the message %d times, want 0", gateway.calls)
		}
	})

	t.Run("host reports non-delivery and the outbox retries", func(t *testing.T) {
		ctx := context.Background()
		r := NewDevelopment()
		run, _, err := r.StartRun(ctx, api.StartRunCommand{Request: "reconcile"})
		if err != nil {
			t.Fatalf("StartRun() error = %v", err)
		}
		queueResponse(ctx, t, r, run.ID, "message-1")
		strandPublication(ctx, t, r, run.ID, "message-1", errGateway)

		settled, err := r.ReconcileResponsePublication(ctx, api.ReconcileResponsePublicationCommand{
			RunID: run.ID, MessageID: "message-1", Delivered: false, Reason: "never left the queue",
		})
		if err != nil {
			t.Fatalf("ReconcileResponsePublication() error = %v", err)
		}
		if settled.Status != api.UserMessageQueued {
			t.Fatalf("settled message = %#v, want queued", settled)
		}

		gateway := &countingGateway{}
		r.SetOutputGateway(gateway)
		drained, err := r.DrainResponseOutbox(ctx)
		if err != nil {
			t.Fatalf("DrainResponseOutbox() error = %v", err)
		}
		if drained != 1 || gateway.calls != 1 {
			t.Fatalf("drain after reconciliation = %d messages / %d calls, want 1/1", drained, gateway.calls)
		}
	})
}

// strandPublication reproduces a publish whose gateway returned an error after
// the durable claim. The runtime keeps the message in publishing because the
// external delivery outcome is unknown.
func strandPublication(ctx context.Context, t *testing.T, r *Runner, runID, messageID string, gatewayErr error) api.UserMessage {
	t.Helper()
	r.SetOutputGateway(refusingGateway{err: gatewayErr})
	if err := r.PublishResponse(ctx, api.PublishResponseCommand{RunID: runID, MessageID: messageID}); !errors.Is(err, gatewayErr) {
		t.Fatalf("PublishResponse(refusing gateway) error = %v, want %v", err, gatewayErr)
	}
	stranded, err := r.LoadMessage(ctx, runID, messageID)
	if err != nil {
		t.Fatalf("LoadMessage(stranded) error = %v", err)
	}
	if stranded.Status != api.UserMessagePublishing {
		t.Fatalf("stranded message status = %q, want publishing", stranded.Status)
	}
	return stranded
}
