package coding

import (
	"context"
	"errors"

	"github.com/Viking602/venat/coding/internal/hashline"
	"github.com/Viking602/venat/tool"
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
// is rejected and the agent is redirected to coding.edit_hashline. It records
// the new file's content into the shared snapshot store so the ¶PATH#TAG it
// returns is collision-guarded like a read/search/edit tag.
type writeFileDriver struct {
	ws    Workspace
	store hashline.SnapshotStore
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

	// Record the new file's content under its canonical path so the tag this
	// call mints is backed by recorded history. Without this, a later edit
	// keyed to that tag would fast-path on the 16-bit hash alone and could
	// apply stale line numbers if the file changed out of band. WriteFile mints
	// res.Tag from Normalize(in.Content), and Record normalizes identically, so
	// the recorded snapshot's hash equals res.Tag.
	if d.store != nil {
		d.store.Record(res.Path, in.Content)
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
