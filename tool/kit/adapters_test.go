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

func TestProcessToolStreamsOutputBeforeReturning(t *testing.T) {
	if os.Getenv("VENAT_PROCESS_STREAM_HELPER") == "1" {
		_, _ = os.Stdout.WriteString("streamed")
		os.Exit(0)
	}

	driver := ProcessTool("run", tool.Schema{Type: "object"}, ProcessToolConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestProcessToolStreamsOutputBeforeReturning$"},
		Env:     append(os.Environ(), "VENAT_PROCESS_STREAM_HELPER=1"),
	})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	updates := make(chan tool.Update, 1)
	type outcome struct {
		result tool.Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := tool.NewBus(driver).Execute(context.Background(), tool.Call{
			ID:          "call-process-stream",
			Name:        "run",
			OperationID: "turn:0:call:0",
			Arguments:   json.RawMessage(`{}`),
		}, tool.ExecuteOptions{Sink: func(update tool.Update) error {
			updates <- tool.CloneUpdate(update)
			<-release
			return nil
		}})
		done <- outcome{result: result, err: err}
	}()

	var update tool.Update
	select {
	case update = <-updates:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process output update")
	}
	if update.Kind != tool.UpdateOutput || update.ToolCallID != "call-process-stream" ||
		update.OperationID != "turn:0:call:0" || update.Sequence != 1 ||
		len(update.Parts) != 1 || update.Parts[0].Text != "streamed" {
		t.Fatalf("process update = %#v", update)
	}
	select {
	case current := <-done:
		t.Fatalf("Execute() returned before sink released: %#v", current)
	default:
	}
	close(release)
	current := <-done
	if current.err != nil {
		t.Fatalf("Execute() error = %v", current.err)
	}
	if current.result.Content != "streamed" || len(current.result.Parts) != 1 || current.result.Parts[0].Text != "streamed" {
		t.Fatalf("process result = %#v", current.result)
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

func TestProcessToolCapturesOutputAfterLongRunningChild(t *testing.T) {
	const payloadSize = 64 << 10
	if os.Getenv("VENAT_PROCESS_LONG_HELPER") == "1" {
		time.Sleep(250 * time.Millisecond)
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("z"), payloadSize))
		os.Exit(0)
	}

	driver := ProcessTool("run", tool.Schema{Type: "object"}, ProcessToolConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestProcessToolCapturesOutputAfterLongRunningChild$"},
		Env:     append(os.Environ(), "VENAT_PROCESS_LONG_HELPER=1"),
	})
	result, err := driver.Execute(context.Background(), tool.Call{
		ID:        "call-process-long",
		Name:      "run",
		Arguments: json.RawMessage(`{}`),
	}, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := len(result.Content); got != payloadSize {
		t.Fatalf("captured %d bytes after long-running child, want %d", got, payloadSize)
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

func TestAdapterToolsCarryExecutionSettings(t *testing.T) {
	drivers := []tool.Driver{
		HTTPTool("remote", tool.Schema{Type: "object"}, HTTPToolConfig{URL: "https://example.test/tool"},
			Timeout(5*time.Second),
			Concurrency(tool.ConcurrencySequential),
			ConcurrencyGroup("adapters"),
			MaxConcurrency(1),
		),
		ProcessTool("run", tool.Schema{Type: "object"}, ProcessToolConfig{Command: "printf"},
			Timeout(5*time.Second),
			Concurrency(tool.ConcurrencySequential),
			ConcurrencyGroup("adapters"),
			MaxConcurrency(1),
		),
	}
	for _, driver := range drivers {
		def := driver.Definition()
		if def.Timeout != 5*time.Second {
			t.Fatalf("%s timeout = %s, want 5s", def.Name, def.Timeout)
		}
		if def.Concurrency != tool.ConcurrencySequential || def.ConcurrencyGroup != "adapters" || def.MaxConcurrency != 1 {
			t.Fatalf("%s concurrency settings = %#v", def.Name, def)
		}
	}
}
