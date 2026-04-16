package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/capability"
)

func TestRuntimeSupportsSearchAndRemoteAgentCapabilities(t *testing.T) {
	runtime := New(Config{})
	trace := make([]string, 0, 2)
	runtime.UseCapabilityMiddleware(capability.Func(func(ctx context.Context, call capability.Call, next capability.Next) (capability.Result, error) {
		trace = append(trace, string(call.Type)+":"+call.Name)
		return next(ctx, call)
	}))
	runtime.RegisterCapability(capability.TypeSearch, "web", func(ctx context.Context, call capability.Call) (capability.Result, error) {
		return capability.Result{Output: "search:" + call.Name}, nil
	})
	runtime.RegisterCapability(capability.TypeRemoteAgent, "delegate", func(ctx context.Context, call capability.Call) (capability.Result, error) {
		return capability.Result{Output: "agent:" + call.Name}, nil
	})

	searchResult, err := runtime.InvokeCapability(context.Background(), capability.Call{Type: capability.TypeSearch, Name: "web"})
	if err != nil {
		t.Fatalf("InvokeCapability(search) error = %v", err)
	}
	remoteResult, err := runtime.InvokeCapability(context.Background(), capability.Call{Type: capability.TypeRemoteAgent, Name: "delegate"})
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
