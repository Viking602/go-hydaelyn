package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/capability"
)

func TestSupportsSearchAndRemoteAgentCapabilities(t *testing.T) {
	runner := New(Config{})
	trace := make([]string, 0, 2)
	runner.UseCapabilityMiddleware(capability.Func(func(ctx context.Context, call capability.Call, next capability.Next) (capability.Result, error) {
		trace = append(trace, string(call.Type)+":"+call.Name)
		return next(ctx, call)
	}))
	runner.RegisterCapability(capability.TypeSearch, "web", func(ctx context.Context, call capability.Call) (capability.Result, error) {
		return capability.Result{Output: "search:" + call.Name}, nil
	})
	runner.RegisterCapability(capability.TypeRemoteAgent, "delegate", func(ctx context.Context, call capability.Call) (capability.Result, error) {
		return capability.Result{Output: "agent:" + call.Name}, nil
	})

	searchResult, err := runner.InvokeCapability(context.Background(), capability.Call{Type: capability.TypeSearch, Name: "web"})
	if err != nil {
		t.Fatalf("InvokeCapability(search) error = %v", err)
	}
	remoteResult, err := runner.InvokeCapability(context.Background(), capability.Call{Type: capability.TypeRemoteAgent, Name: "delegate"})
	if err != nil {
		t.Fatalf("InvokeCapability(remote) error = %v", err)
	}
	if searchResult.Output != "search:web" || remoteResult.Output != "agent:delegate" {
		t.Fatalf("unexpected outputs: %#v %#v", searchResult, remoteResult)
	}
	if len(trace) != 2 {
		t.Fatalf("expected capability middleware trace, got %#v", trace)
	}
}
