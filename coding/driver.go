package coding

import (
	"encoding/json"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/tool"
)

// Tool name constants. They share the coding.* namespace and are referenced
// by AgentClass, PolicyEngine, and the toolset wiring.
const (
	ToolListFiles    = "coding.list_files"
	ToolReadFile     = "coding.read_file"
	ToolSearch       = "coding.search"
	ToolGitDiff      = "coding.git_diff"
	ToolEditHashline = "coding.edit_hashline"
	ToolWriteFile    = "coding.write_file"
	ToolGofmt        = "coding.gofmt"
	ToolGoTest       = "coding.go_test"
)

// Policy tags shared across tools. The PolicyEngine inspects PolicyTags to
// require approval for delete/run-tagged tools.
const (
	tagCoding    = "coding"
	tagRead      = "read"
	tagSearch    = "search"
	tagGit       = "git"
	tagDiff      = "diff"
	tagEdit      = "edit"
	tagHashline  = "hashline"
	tagWorkspace = "workspace-write"
	tagCreate    = "create-file"
	tagFormat    = "format"
	tagTest      = "test"
	tagRun       = "run"
	tagDelete    = "delete"
)

// Risk levels mirrored from the spec section-6 table.
const (
	riskLow    = "low"
	riskMedium = "medium"
)

// decodeArgs unmarshals a tool call's JSON arguments into v, tolerating an
// empty payload as the zero value.
func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// successResult builds a tool.Result whose Content is human-facing text and
// whose Structured payload is the JSON of v.
func successResult(call tool.Call, content string, v any) (tool.Result, error) {
	structured, err := json.Marshal(v)
	if err != nil {
		return tool.Result{}, err
	}
	return tool.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    content,
		Structured: structured,
	}, nil
}

// errorResult builds a non-fatal tool.Result that carries an agent-facing
// message and marks IsError. Tools return this (with a nil error) when the
// failure is one the agent should recover from in-loop (stale tag, parse
// error, file-exists) rather than abort the run.
func errorResult(call tool.Call, message string) tool.Result {
	return tool.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    message,
		IsError:    true,
	}
}

// stringSchema builds a string property schema with a description.
func stringSchema(description string) message.JSONSchema {
	return message.JSONSchema{Type: "string", Description: description}
}

// boolSchema builds a boolean property schema with a description.
func boolSchema(description string) message.JSONSchema {
	return message.JSONSchema{Type: "boolean", Description: description}
}

// intSchema builds an integer property schema with a description.
func intSchema(description string) message.JSONSchema {
	return message.JSONSchema{Type: "integer", Description: description}
}
