package venat

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
)

// denyOperationEngine denies exactly one operation and allows the rest, so a
// test can tell which of the two policy slots produced a decision.
type denyOperationEngine struct{ operation api.PolicyOperation }

func (e denyOperationEngine) Authorize(_ context.Context, request api.PolicyRequest) (api.PolicyDecision, error) {
	if request.Operation == e.operation {
		return api.PolicyDecision{Effect: api.PolicyEffectDeny, Reason: "engine denied"}, nil
	}
	return api.PolicyDecision{Effect: api.PolicyEffectAllow}, nil
}

func denyAllMessages(api.UserMessage) api.PolicyDecision {
	return api.PolicyDecision{Effect: api.PolicyEffectDeny, Reason: "message policy denied"}
}

func allowAllMessages(api.UserMessage) api.PolicyDecision {
	return api.PolicyDecision{Effect: api.PolicyEffectAllow}
}

// queueResponse seeds one queued outbox message that PublishResponse can pick
// up, which is the only operation carrying a PolicyRequest.Message.
func queueResponse(ctx context.Context, t *testing.T, r *Runner, runID, messageID string) {
	t.Helper()
	if err := r.QueueMessage(ctx, api.UserMessage{
		ID:      messageID,
		RunID:   runID,
		Status:  api.UserMessageQueued,
		Payload: "safe answer",
	}); err != nil {
		t.Fatalf("QueueMessage() error = %v", err)
	}
}

// requestHandoff drives a handoff, whose PolicyRequest carries no Message and is
// therefore decided by the policy engine alone.
func requestHandoff(ctx context.Context, t *testing.T, r *Runner, runID string) error {
	t.Helper()
	task, err := r.CreateTask(ctx, api.CreateTaskCommand{
		RunID: runID, TaskID: "worker", OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return r.RequestHandoff(ctx, api.HandoffCommand{
		RunID: runID, TaskID: task.ID, FromAgentID: "agent-a", ToAgentID: "agent-b",
		TaskVersion: task.Version,
	})
}

func TestPolicySetters_ComposeRegardlessOfOrder(t *testing.T) {
	engine := denyOperationEngine{operation: api.PolicyOperationHandoff}
	tests := []struct {
		name    string
		install func(*Runner)
	}{
		{
			name: "engine then message policy",
			install: func(r *Runner) {
				r.SetPolicyEngine(engine)
				r.SetMessagePolicy(denyAllMessages)
			},
		},
		{
			name: "message policy then engine",
			install: func(r *Runner) {
				r.SetMessagePolicy(denyAllMessages)
				r.SetPolicyEngine(engine)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			r := NewDevelopment()
			test.install(r)

			run, _, err := r.StartRun(ctx, api.StartRunCommand{Request: "compose"})
			if err != nil {
				t.Fatalf("StartRun() error = %v", err)
			}
			if err := requestHandoff(ctx, t, r, run.ID); !errors.Is(err, api.ErrPolicyDenied) {
				t.Fatalf("RequestHandoff() error = %v, want ErrPolicyDenied from the engine", err)
			}
			queueResponse(ctx, t, r, run.ID, "message-1")
			err = r.PublishResponse(ctx, api.PublishResponseCommand{RunID: run.ID, MessageID: "message-1"})
			if !errors.Is(err, api.ErrPolicyDenied) {
				t.Fatalf("PublishResponse() error = %v, want ErrPolicyDenied from the message policy", err)
			}
		})
	}
}

func TestPolicySetters_MessageRequestNeedsBothToAllow(t *testing.T) {
	tests := []struct {
		name      string
		engine    api.PolicyEngine
		message   api.MessagePolicyChecker
		wantDenal bool
	}{
		{name: "both allow", engine: denyOperationEngine{operation: api.PolicyOperationHandoff}, message: allowAllMessages},
		{
			name:      "engine denies the publish",
			engine:    denyOperationEngine{operation: api.PolicyOperationResponsePublish},
			message:   allowAllMessages,
			wantDenal: true,
		},
		{
			name:      "message policy denies the publish",
			engine:    denyOperationEngine{operation: api.PolicyOperationHandoff},
			message:   denyAllMessages,
			wantDenal: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			r := NewDevelopment()
			r.SetPolicyEngine(test.engine)
			r.SetMessagePolicy(test.message)

			run, _, err := r.StartRun(ctx, api.StartRunCommand{Request: "publish"})
			if err != nil {
				t.Fatalf("StartRun() error = %v", err)
			}
			queueResponse(ctx, t, r, run.ID, "message-1")
			err = r.PublishResponse(ctx, api.PublishResponseCommand{RunID: run.ID, MessageID: "message-1"})
			if test.wantDenal {
				if !errors.Is(err, api.ErrPolicyDenied) {
					t.Fatalf("PublishResponse() error = %v, want ErrPolicyDenied", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("PublishResponse() error = %v, want nil", err)
			}
		})
	}
}

func TestSetPolicyEngine_NilLeavesMessagePolicyInForce(t *testing.T) {
	ctx := context.Background()
	r := NewDevelopment()
	r.SetMessagePolicy(denyAllMessages)
	r.SetPolicyEngine(nil)

	run, _, err := r.StartRun(ctx, api.StartRunCommand{Request: "clear engine"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	queueResponse(ctx, t, r, run.ID, "message-1")
	if err := r.PublishResponse(ctx, api.PublishResponseCommand{RunID: run.ID, MessageID: "message-1"}); !errors.Is(err, api.ErrPolicyDenied) {
		t.Fatalf("PublishResponse() error = %v, want the message policy to survive SetPolicyEngine(nil)", err)
	}
}

func TestSetMessagePolicy_NilLeavesPolicyEngineInForce(t *testing.T) {
	ctx := context.Background()
	r := NewDevelopment()
	r.SetPolicyEngine(denyOperationEngine{operation: api.PolicyOperationHandoff})
	r.SetMessagePolicy(nil)

	run, _, err := r.StartRun(ctx, api.StartRunCommand{Request: "clear message policy"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if err := requestHandoff(ctx, t, r, run.ID); !errors.Is(err, api.ErrPolicyDenied) {
		t.Fatalf("RequestHandoff() error = %v, want the engine to survive SetMessagePolicy(nil)", err)
	}
}

func TestSetPolicyEngine_NilRespectsRuntimeMode(t *testing.T) {
	engine := denyOperationEngine{operation: api.PolicyOperationHandoff}
	tests := []struct {
		name       string
		newRunner  func(*testing.T) *Runner
		wantDenied bool
	}{
		{
			name: "development clears the engine",
			newRunner: func(*testing.T) *Runner {
				return NewDevelopment(api.Config{PolicyEngine: engine})
			},
		},
		{
			name: "production keeps the engine",
			newRunner: func(t *testing.T) *Runner {
				t.Helper()
				r, err := NewProduction(api.Config{
					StoreProvider: NewDevelopment().StoreProvider(),
					PolicyEngine:  engine,
				})
				if err != nil {
					t.Fatalf("NewProduction() error = %v", err)
				}
				return r
			},
			wantDenied: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			r := test.newRunner(t)
			r.SetPolicyEngine(nil)

			run, _, err := r.StartRun(ctx, api.StartRunCommand{Request: "clear engine"})
			if err != nil {
				t.Fatalf("StartRun() error = %v", err)
			}
			err = requestHandoff(ctx, t, r, run.ID)
			if test.wantDenied {
				if !errors.Is(err, api.ErrPolicyDenied) {
					t.Fatalf("RequestHandoff() error = %v, want a production runner to reject SetPolicyEngine(nil)", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("RequestHandoff() error = %v, want nil after the engine was cleared", err)
			}
		})
	}
}
