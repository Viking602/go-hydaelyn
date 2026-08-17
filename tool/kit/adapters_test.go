package kit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/venat/tool"
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
		Arguments: json.RawMessage(`{"query":"venat"}`),
	}, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Content != `{"status":"ok"}` {
		t.Fatalf("unexpected result: %q", result.Content)
	}
}

func TestHTTPToolRejectsOversizedResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(bytes.Repeat([]byte("x"), 1<<20+1))
	}))
	defer ts.Close()

	driver := HTTPTool("remote", tool.Schema{Type: "object"}, HTTPToolConfig{URL: ts.URL})
	_, err := driver.Execute(context.Background(), tool.Call{
		ID:        "call-oversized",
		Name:      "remote",
		Arguments: json.RawMessage(`{}`),
	}, nil)
	if err == nil {
		t.Fatal("expected oversized response error")
	}
}

func TestProcessToolRejectsOversizedOutput(t *testing.T) {
	if os.Getenv("VENAT_PROCESS_OVERSIZED_OUTPUT_HELPER") == "1" {
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), 1<<20+1))
		os.Exit(0)
	}

	driver := ProcessTool("run", tool.Schema{Type: "object"}, ProcessToolConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestProcessToolRejectsOversizedOutput$"},
		Env:     append(os.Environ(), "VENAT_PROCESS_OVERSIZED_OUTPUT_HELPER=1"),
	})
	_, err := driver.Execute(context.Background(), tool.Call{
		ID:        "call-process-oversized",
		Name:      "run",
		Arguments: json.RawMessage(`{}`),
	}, nil)
	if err == nil {
		t.Fatal("expected oversized output error")
	}
}

func TestProcessToolCapturesStdoutAndStderr(t *testing.T) {
	if os.Getenv("VENAT_PROCESS_CAPTURE_HELPER") == "1" {
		_, _ = os.Stdout.WriteString("stdout-body")
		_, _ = os.Stderr.WriteString("stderr-body")
		os.Exit(0)
	}

	driver := ProcessTool("run", tool.Schema{Type: "object"}, ProcessToolConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestProcessToolCapturesStdoutAndStderr$"},
		Env:     append(os.Environ(), "VENAT_PROCESS_CAPTURE_HELPER=1"),
	})
	result, err := driver.Execute(context.Background(), tool.Call{
		ID:        "call-process-capture",
		Name:      "run",
		Arguments: json.RawMessage(`{}`),
	}, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !bytes.Contains([]byte(result.Content), []byte("stdout-body")) {
		t.Fatalf("missing stdout in %#q", result.Content)
	}
	if !bytes.Contains([]byte(result.Content), []byte("stderr-body")) {
		t.Fatalf("missing stderr in %#q", result.Content)
	}
}

func TestProcessToolDrainsPipeBeforeWait(t *testing.T) {
	const payloadSize = 256 << 10
	if os.Getenv("VENAT_PROCESS_DRAIN_HELPER") == "1" {
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("a"), payloadSize))
		_, _ = os.Stderr.Write(bytes.Repeat([]byte("b"), payloadSize))
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	driver := ProcessTool("run", tool.Schema{Type: "object"}, ProcessToolConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestProcessToolDrainsPipeBeforeWait$"},
		Env:     append(os.Environ(), "VENAT_PROCESS_DRAIN_HELPER=1"),
	})
	result, err := driver.Execute(ctx, tool.Call{
		ID:        "call-process-drain",
		Name:      "run",
		Arguments: json.RawMessage(`{}`),
	}, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := len(result.Content); got != payloadSize*2 {
		t.Fatalf("captured %d bytes, want %d", got, payloadSize*2)
	}
}

func TestProcessToolForwardsStdinJSON(t *testing.T) {
	if os.Getenv("VENAT_PROCESS_STDIN_HELPER") == "1" {
		payload, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(2)
		}
		_, _ = os.Stdout.Write(payload)
		os.Exit(0)
	}

	driver := ProcessTool("run", tool.Schema{Type: "object"}, ProcessToolConfig{
		Command:   os.Args[0],
		Args:      []string{"-test.run=^TestProcessToolForwardsStdinJSON$"},
		Env:       append(os.Environ(), "VENAT_PROCESS_STDIN_HELPER=1"),
		StdinJSON: true,
	})
	result, err := driver.Execute(context.Background(), tool.Call{
		ID:        "call-process-stdin",
		Name:      "run",
		Arguments: json.RawMessage(`{"query":"venat"}`),
	}, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Content != `{"query":"venat"}` {
		t.Fatalf("stdin round-trip = %#q", result.Content)
	}
}

func TestProcessToolReturnsAfterChildExitsWithInheritedPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inherited-pipe orphan test uses a Unix shell")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	driver := ProcessTool("run", tool.Schema{Type: "object"}, ProcessToolConfig{
		Command: "sh",
		Args:    []string{"-c", "printf ready; sleep 8 &"},
	})
	started := time.Now()
	result, err := driver.Execute(ctx, tool.Call{
		ID:        "call-process-orphan",
		Name:      "run",
		Arguments: json.RawMessage(`{}`),
	}, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Content, "ready") {
		t.Fatalf("missing child output in %#q", result.Content)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("blocked on inherited pipe for %s", elapsed)
	}
}

func TestProcessToolPreservesCommandContextCancel(t *testing.T) {
	if os.Getenv("VENAT_PROCESS_CANCEL_HELPER") == "1" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		time.Sleep(time.Hour)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	driver := ProcessTool("run", tool.Schema{Type: "object"}, ProcessToolConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestProcessToolPreservesCommandContextCancel$"},
		Env:     append(os.Environ(), "VENAT_PROCESS_CANCEL_HELPER=1"),
	})
	_, err := driver.Execute(ctx, tool.Call{
		ID:        "call-process-cancel",
		Name:      "run",
		Arguments: json.RawMessage(`{}`),
	}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline error, got %v", err)
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
