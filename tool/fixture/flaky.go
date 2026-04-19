package fixture

import (
	"context"
	"sync"

	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/tool"
)

type FlakyTool struct {
	mu                sync.Mutex
	failuresRemaining int
}

type flakyInput struct {
	Value string `json:"value"`
}

type flakyOutput struct {
	Value string `json:"value"`
}

func NewFlakyTool(failures int) *FlakyTool {
	if failures < 0 {
		failures = 0
	}
	return &FlakyTool{failuresRemaining: failures}
}

func (t *FlakyTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "flaky",
		Description: "Fail deterministically before succeeding",
		InputSchema: tool.Schema{
			Type:       "object",
			Properties: map[string]tool.Schema{"value": {Type: "string"}},
			Required:   []string{"value"},
		},
	}
}

func (t *FlakyTool) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var input flakyInput
	if err := decodeArgs(call, &input); err != nil {
		return tool.Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failuresRemaining > 0 {
		t.failuresRemaining--
		return tool.Result{}, &capability.Error{Kind: capability.ErrorKindUpstream, Message: "flaky fixture failure", Temporary: true}
	}
	return jsonResult(call, t.Definition().Name, flakyOutput{Value: input.Value})
}
