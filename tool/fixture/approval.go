package fixture

import (
	"context"
	"fmt"
	"sync"

	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/tool"
)

type ApprovalTool struct {
	mu      sync.Mutex
	pending map[string]int
}

type approvalInput struct {
	Request  string `json:"request"`
	Approved bool   `json:"approved"`
}

type approvalOutput struct {
	Request  string `json:"request"`
	Approved bool   `json:"approved"`
}

func NewApprovalTool() *ApprovalTool {
	return &ApprovalTool{pending: map[string]int{}}
}

func (t *ApprovalTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "approval",
		Description: "Mock approval pause and resume behavior",
		InputSchema: tool.Schema{
			Type: "object",
			Properties: map[string]tool.Schema{
				"request":  {Type: "string"},
				"approved": {Type: "boolean"},
			},
			Required: []string{"request"},
		},
	}
}

func (t *ApprovalTool) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var input approvalInput
	if err := decodeArgs(call, &input); err != nil {
		return tool.Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !input.Approved {
		t.pending[input.Request]++
		return tool.Result{}, &capability.Error{Kind: capability.ErrorKindApproval, Message: fmt.Sprintf("approval required for %s", input.Request)}
	}
	delete(t.pending, input.Request)
	return jsonResult(call, t.Definition().Name, approvalOutput{Request: input.Request, Approved: true})
}

func (t *ApprovalTool) Pending(request string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pending[request]
}
