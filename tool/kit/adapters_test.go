package kit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/tool"
)

func TestHTTPTool(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	driver := HTTPTool("remote", tool.Schema{Type: "object"}, HTTPToolConfig{URL: ts.URL}, Description("remote"))
	result, err := driver.Execute(context.Background(), tool.Call{
		ID:        "call-1",
		Name:      "remote",
		Arguments: json.RawMessage(`{"query":"hydaelyn"}`),
	}, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Content != `{"status":"ok"}` {
		t.Fatalf("unexpected result: %q", result.Content)
	}
}

func TestHTTPToolCarriesRuntimeGovernanceMetadata(t *testing.T) {
	driver := HTTPTool("remote", tool.Schema{Type: "object"}, HTTPToolConfig{URL: "https://example.test/tool"},
		Effect(tool.EffectExternalSideEffect),
		RequiresActionTask(),
		RequiresApproval(),
		RiskLevel("high"),
		Idempotent(false),
		Timeout(5*time.Second),
		Retry(tool.RetryPolicy{MaxAttempts: 2, Backoff: time.Second}),
		PolicyTags("prod", "approval"),
		RequiredPermissions("network:egress"),
	)

	def := driver.Definition()
	if def.Origin != "http" {
		t.Fatalf("expected http origin, got %q", def.Origin)
	}
	assertGovernanceMetadata(t, def, false, "network:egress")
}

func TestProcessToolCarriesRuntimeGovernanceMetadata(t *testing.T) {
	driver := ProcessTool("run", tool.Schema{Type: "object"}, ProcessToolConfig{Command: "printf"},
		Effect(tool.EffectExternalSideEffect),
		RequiresActionTask(),
		RequiresApproval(),
		RiskLevel("high"),
		Idempotent(true),
		Timeout(5*time.Second),
		Retry(tool.RetryPolicy{MaxAttempts: 2, Backoff: time.Second}),
		PolicyTags("prod", "approval"),
		RequiredPermissions("process:exec"),
	)

	def := driver.Definition()
	if def.Origin != "process" {
		t.Fatalf("expected process origin, got %q", def.Origin)
	}
	assertGovernanceMetadata(t, def, true, "process:exec")
}

func assertGovernanceMetadata(t *testing.T, def tool.Definition, idempotent bool, permission string) {
	t.Helper()

	if def.EffectType != tool.EffectExternalSideEffect {
		t.Fatalf("expected external side-effect metadata, got %#v", def)
	}
	if !def.RequiresActionTask {
		t.Fatalf("expected action task requirement, got %#v", def)
	}
	if !def.RequiresApproval || !def.Security.RequiresApproval {
		t.Fatalf("expected approval requirement in definition and security, got %#v", def)
	}
	if def.RiskLevel != "high" || def.Security.RiskLevel != "high" {
		t.Fatalf("expected high risk level in definition and security, got %#v", def)
	}
	if def.Idempotent != idempotent || def.Security.Idempotent != idempotent {
		t.Fatalf("expected idempotent %v in definition and security, got %#v", idempotent, def)
	}
	if def.Timeout != 5*time.Second {
		t.Fatalf("expected timeout metadata, got %#v", def)
	}
	if def.RetryPolicy.MaxAttempts != 2 || def.RetryPolicy.Backoff != time.Second {
		t.Fatalf("expected retry metadata, got %#v", def)
	}
	if len(def.PolicyTags) != 2 || def.PolicyTags[0] != "prod" || def.PolicyTags[1] != "approval" {
		t.Fatalf("expected policy tags, got %#v", def.PolicyTags)
	}
	if len(def.RequiredPermissions) != 1 || def.RequiredPermissions[0] != permission {
		t.Fatalf("expected required permission %q, got %#v", permission, def.RequiredPermissions)
	}
	if len(def.Security.RequiredPermissions) != 1 || def.Security.RequiredPermissions[0] != permission {
		t.Fatalf("expected security required permission %q, got %#v", permission, def.Security.RequiredPermissions)
	}
}
