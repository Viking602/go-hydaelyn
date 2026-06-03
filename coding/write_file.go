package coding

import (
	"context"
	"errors"

	"github.com/Viking602/go-hydaelyn/coding/internal/hashline"
	"github.com/Viking602/go-hydaelyn/tool"
)

// writeFileInput is the decoded argument shape for coding.write_file.
type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteFileToolResult is the typed structured result of coding.write_file.
type WriteFileToolResult struct {
	Path    string `json:"path"`
	Tag     string `json:"tag"`
	Header  string `json:"header"`
	Content string `json:"content"`
}

// writeFileDriver creates a new workspace file. Writing over an existing file
// is rejected and the agent is redirected to coding.edit_hashline.
type writeFileDriver struct {
	ws Workspace
}

func (d writeFileDriver) Definition() tool.Definition {
	return tool.Definition{
		Name:        ToolWriteFile,
		Description: "Create a new workspace file. To modify an existing file, use coding.edit_hashline instead.",
		InputSchema: objectSchema(
			[]string{"path", "content"},
			property{"path", stringSchema("Workspace-relative path of the new file.")},
			property{"content", stringSchema("Full file content.")},
		),
		EffectType:         tool.EffectWrite,
		RequiresActionTask: true,
		RiskLevel:          riskMedium,
		PolicyTags:         []string{tagCoding, tagCreate},
	}
}

func (d writeFileDriver) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var in writeFileInput
	if err := decodeArgs(call.Arguments, &in); err != nil {
		return tool.Result{}, err
	}
	res, err := d.ws.WriteFile(ctx, WriteFileRequest{Path: in.Path, Content: in.Content})
	if err != nil {
		if errors.Is(err, ErrFileExists) {
			return errorResult(call,
				"coding.write_file rejected: "+err.Error()+
					". write_file only creates new files; to change an existing file, read it with coding.read_file and apply a patch with coding.edit_hashline."), nil
		}
		return errorResult(call, "coding.write_file failed: "+err.Error()), nil
	}
	header := hashline.FormatHeader(res.Path, res.Tag)
	content := header + "\ncreated " + res.Path
	structured := WriteFileToolResult{
		Path:    res.Path,
		Tag:     res.Tag,
		Header:  header,
		Content: content,
	}
	return successResult(call, content, structured)
}
