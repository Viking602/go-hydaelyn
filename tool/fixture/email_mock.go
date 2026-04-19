package fixture

import (
	"context"
	"sync"

	"github.com/Viking602/go-hydaelyn/tool"
)

type EmailMockTool struct {
	mu     sync.Mutex
	emails []EmailRecord
}

type EmailRecord struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func NewEmailMockTool() *EmailMockTool {
	return &EmailMockTool{}
}

func (t *EmailMockTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "email_mock",
		Description: "Track email side effects without sending messages",
		InputSchema: tool.Schema{
			Type: "object",
			Properties: map[string]tool.Schema{
				"to":      {Type: "string"},
				"subject": {Type: "string"},
				"body":    {Type: "string"},
			},
			Required: []string{"to", "subject", "body"},
		},
	}
}

func (t *EmailMockTool) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var input EmailRecord
	if err := decodeArgs(call, &input); err != nil {
		return tool.Result{}, err
	}
	t.mu.Lock()
	t.emails = append(t.emails, input)
	t.mu.Unlock()
	return jsonResult(call, t.Definition().Name, input)
}

func (t *EmailMockTool) Records() []EmailRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	cloned := make([]EmailRecord, len(t.emails))
	copy(cloned, t.emails)
	return cloned
}
