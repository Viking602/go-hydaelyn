package toolkit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
