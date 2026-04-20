package fixture

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/tool/tooltest"
)

func TestSearchTool(t *testing.T) {
	driver, err := NewSearchTool(filepath.Join("..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatalf("NewSearchTool() error = %v", err)
	}
	result := tooltest.MustCall(t, driver, map[string]any{"query": "retain", "limit": 1})
	var output struct {
		Matches []map[string]any `json:"matches"`
	}
	if err := json.Unmarshal(result.Structured, &output); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(output.Matches) != 1 || output.Matches[0]["id"] != "policy-001" {
		t.Fatalf("unexpected matches %#v", output.Matches)
	}
}

func TestCalculatorTool(t *testing.T) {
	result := tooltest.MustCall(t, NewCalculatorTool(), map[string]any{"operation": "multiply", "operands": []float64{2, 3, 4}})
	var output struct {
		Result float64 `json:"result"`
	}
	if err := json.Unmarshal(result.Structured, &output); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if output.Result != 24 {
		t.Fatalf("result = %v, want 24", output.Result)
	}
}

func TestFlakyTool(t *testing.T) {
	driver := NewFlakyTool(2)
	call := tool.Call{ID: "call-1", Name: driver.Definition().Name, Arguments: []byte(`{"value":"ok"}`)}
	for attempt := range 2 {
		_, err := driver.Execute(context.Background(), call, nil)
		var capErr *capability.Error
		if !errors.As(err, &capErr) || capErr.Kind != capability.ErrorKindUpstream {
			t.Fatalf("attempt %d error = %v, want upstream capability error", attempt+1, err)
		}
	}
	result, err := driver.Execute(context.Background(), call, nil)
	if err != nil {
		t.Fatalf("third attempt error = %v", err)
	}
	var output struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(result.Structured, &output); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if output.Value != "ok" {
		t.Fatalf("value = %q, want ok", output.Value)
	}
}

func TestSlowToolTimeout(t *testing.T) {
	driver := NewSlowTool(20 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	_, err := driver.Execute(ctx, tool.Call{ID: "call-1", Name: driver.Definition().Name, Arguments: []byte(`{"message":"wait"}`)}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute() error = %v, want context deadline exceeded", err)
	}
}

func TestPermissionTool(t *testing.T) {
	driver := NewPermissionTool()
	_, err := driver.Execute(context.Background(), tool.Call{ID: "call-1", Name: driver.Definition().Name, Arguments: []byte(`{"permission":"write","granted":false}`)}, nil)
	var capErr *capability.Error
	if !errors.As(err, &capErr) || capErr.Kind != capability.ErrorKindPermission {
		t.Fatalf("Execute() error = %v, want permission error", err)
	}
	if _, err := driver.Execute(context.Background(), tool.Call{ID: "call-2", Name: driver.Definition().Name, Arguments: []byte(`{"permission":"write","granted":true}`)}, nil); err != nil {
		t.Fatalf("granted execute error = %v", err)
	}
}

func TestApprovalTool(t *testing.T) {
	driver := NewApprovalTool()
	_, err := driver.Execute(context.Background(), tool.Call{ID: "call-1", Name: driver.Definition().Name, Arguments: []byte(`{"request":"deploy"}`)}, nil)
	var capErr *capability.Error
	if !errors.As(err, &capErr) || capErr.Kind != capability.ErrorKindApproval {
		t.Fatalf("Execute() error = %v, want approval error", err)
	}
	if pending := driver.Pending("deploy"); pending != 1 {
		t.Fatalf("Pending() = %d, want 1", pending)
	}
	if _, err := driver.Execute(context.Background(), tool.Call{ID: "call-2", Name: driver.Definition().Name, Arguments: []byte(`{"request":"deploy","approved":true}`)}, nil); err != nil {
		t.Fatalf("approved execute error = %v", err)
	}
	if pending := driver.Pending("deploy"); pending != 0 {
		t.Fatalf("Pending() after approval = %d, want 0", pending)
	}
}

func TestWriteMockTool(t *testing.T) {
	driver := NewWriteMockTool()
	tmp := filepath.Join(t.TempDir(), "out.txt")
	if _, err := driver.Execute(context.Background(), tool.Call{ID: "call-1", Name: driver.Definition().Name, Arguments: []byte(`{"path":"` + filepath.ToSlash(tmp) + `","content":"hello"}`)}, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no file write, stat err = %v", err)
	}
	records := driver.Records()
	if len(records) != 1 || records[0].Content != "hello" {
		t.Fatalf("unexpected records %#v", records)
	}
}

func TestEmailMockTool(t *testing.T) {
	driver := NewEmailMockTool()
	if _, err := driver.Execute(context.Background(), tool.Call{ID: "call-1", Name: driver.Definition().Name, Arguments: []byte(`{"to":"user@example.com","subject":"subject","body":"body"}`)}, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	records := driver.Records()
	if len(records) != 1 || records[0].To != "user@example.com" {
		t.Fatalf("unexpected records %#v", records)
	}
}
