package fixture

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/tool"
)

type PermissionTool struct{}

type permissionInput struct {
	Permission string `json:"permission"`
	Granted    bool   `json:"granted"`
}

type permissionOutput struct {
	Permission string `json:"permission"`
	Granted    bool   `json:"granted"`
}

func NewPermissionTool() *PermissionTool {
	return &PermissionTool{}
}

func (t *PermissionTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "permission",
		Description: "Mock permission checks without side effects",
		InputSchema: tool.Schema{
			Type: "object",
			Properties: map[string]tool.Schema{
				"permission": {Type: "string"},
				"granted":    {Type: "boolean"},
			},
			Required: []string{"permission", "granted"},
		},
	}
}

func (t *PermissionTool) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var input permissionInput
	if err := decodeArgs(call, &input); err != nil {
		return tool.Result{}, err
	}
	if !input.Granted {
		return tool.Result{}, &capability.Error{Kind: capability.ErrorKindPermission, Message: fmt.Sprintf("permission denied: %s", input.Permission)}
	}
	return jsonResult(call, t.Definition().Name, permissionOutput{Permission: input.Permission, Granted: true})
}
