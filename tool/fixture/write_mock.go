package fixture

import (
	"context"
	"sync"

	"github.com/Viking602/go-hydaelyn/tool"
)

type WriteMockTool struct {
	mu     sync.Mutex
	writes []WriteRecord
}

type WriteRecord struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func NewWriteMockTool() *WriteMockTool {
	return &WriteMockTool{}
}

func (t *WriteMockTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "write_mock",
		Description: "Track write side effects without touching disk",
		InputSchema: tool.Schema{
			Type: "object",
			Properties: map[string]tool.Schema{
				"path":    {Type: "string"},
				"content": {Type: "string"},
			},
			Required: []string{"path", "content"},
		},
	}
}

func (t *WriteMockTool) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var input WriteRecord
	if err := decodeArgs(call, &input); err != nil {
		return tool.Result{}, err
	}
	t.mu.Lock()
	t.writes = append(t.writes, input)
	t.mu.Unlock()
	return jsonResult(call, t.Definition().Name, input)
}

func (t *WriteMockTool) Records() []WriteRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	cloned := make([]WriteRecord, len(t.writes))
	copy(cloned, t.writes)
	return cloned
}
