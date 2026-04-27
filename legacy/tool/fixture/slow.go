package fixture

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/tool"
)

type SlowTool struct {
	Latency time.Duration
}

type slowInput struct {
	Message string `json:"message"`
}

type slowOutput struct {
	Message   string `json:"message"`
	LatencyMs int64  `json:"latencyMs"`
}

func NewSlowTool(latency time.Duration) *SlowTool {
	return &SlowTool{Latency: latency}
}

func (t *SlowTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "slow",
		Description: "Inject deterministic latency for timeout tests",
		InputSchema: tool.Schema{
			Type:       "object",
			Properties: map[string]tool.Schema{"message": {Type: "string"}},
		},
	}
}

func (t *SlowTool) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var input slowInput
	if err := decodeArgs(call, &input); err != nil {
		return tool.Result{}, err
	}
	if t.Latency > 0 {
		select {
		case <-ctx.Done():
			return tool.Result{}, ctx.Err()
		case <-time.After(t.Latency):
		}
	}
	return jsonResult(call, t.Definition().Name, slowOutput{Message: input.Message, LatencyMs: t.Latency.Milliseconds()})
}
