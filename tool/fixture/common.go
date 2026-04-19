package fixture

import (
	"encoding/json"
	"fmt"

	"github.com/Viking602/go-hydaelyn/tool"
)

func decodeArgs(call tool.Call, dest any) error {
	if len(call.Arguments) == 0 {
		return fmt.Errorf("tool %s requires arguments", call.Name)
	}
	if err := json.Unmarshal(call.Arguments, dest); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	return nil
}

func jsonResult(call tool.Call, name string, payload any) (tool.Result, error) {
	structured, err := json.Marshal(payload)
	if err != nil {
		return tool.Result{}, fmt.Errorf("marshal result payload: %w", err)
	}
	return tool.Result{ToolCallID: call.ID, Name: name, Structured: structured}, nil
}
