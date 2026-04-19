package scripted

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	"github.com/Viking602/go-hydaelyn/provider"
)

func TestLoadScriptAndReplay(t *testing.T) {
	path := filepath.Join("testdata", "script.json")
	events, err := LoadScript(path)
	if err != nil {
		t.Fatalf("LoadScript() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(events))
	}
	if events[1].ToolCall == nil || events[1].ToolCall.Name != "calculator" {
		t.Fatalf("unexpected tool call event %#v", events[1])
	}

	driver := New(events)
	stream, err := driver.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	defer stream.Close()

	var replayed []provider.Event
	for {
		event, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Recv() error = %v", err)
		}
		replayed = append(replayed, event)
	}
	if len(replayed) != len(events) {
		t.Fatalf("replayed %d events, want %d", len(replayed), len(events))
	}
	if replayed[0].Text != "Searching fixtures..." {
		t.Fatalf("first text = %q", replayed[0].Text)
	}
	var args map[string]any
	if err := json.Unmarshal(replayed[1].ToolCall.Arguments, &args); err != nil {
		t.Fatalf("unmarshal tool arguments: %v", err)
	}
	if args["operation"] != "add" {
		t.Fatalf("operation = %#v, want add", args["operation"])
	}
}

func TestLoadScriptRejectsUnsupportedKind(t *testing.T) {
	_, err := LoadScript(filepath.Join("testdata", "invalid.json"))
	if err == nil {
		t.Fatal("expected error")
	}
}
