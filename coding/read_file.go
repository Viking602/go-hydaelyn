package coding

import (
	"context"

	"github.com/Viking602/go-hydaelyn/coding/internal/hashline"
	"github.com/Viking602/go-hydaelyn/tool"
)

// readFileInput is the decoded argument shape for coding.read_file.
type readFileInput struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	MaxBytes  int    `json:"maxBytes,omitempty"`
}

// ReadFileToolResult is the typed structured result of coding.read_file.
type ReadFileToolResult struct {
	Path      string `json:"path"`
	Tag       string `json:"tag"`
	Header    string `json:"header"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	LineCount int    `json:"lineCount"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

// readFileDriver reads a workspace file and returns hashline-grounded
// numbered lines under a ¶PATH#TAG header. It records the full normalized file
// into the shared snapshot store so a later edit can recover a stale tag.
type readFileDriver struct {
	ws    Workspace
	store hashline.SnapshotStore
}

func (d readFileDriver) Definition() tool.Definition {
	return tool.Definition{
		Name:        ToolReadFile,
		Description: "Read a workspace file and return ¶PATH#TAG numbered lines for hashline editing.",
		InputSchema: objectSchema(
			[]string{"path"},
			property{"path", stringSchema("Workspace-relative path to read.")},
			property{"startLine", intSchema("1-based first line to return (default 1).")},
			property{"endLine", intSchema("1-based last line to return (default last line).")},
			property{"maxBytes", intSchema("Cap on the returned slice size in bytes.")},
		),
		EffectType:         tool.EffectReadOnly,
		RequiresActionTask: false,
		RiskLevel:          riskLow,
		PolicyTags:         []string{tagCoding, tagRead},
	}
}

func (d readFileDriver) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var in readFileInput
	if err := decodeArgs(call.Arguments, &in); err != nil {
		return tool.Result{}, err
	}
	res, err := d.ws.ReadFile(ctx, ReadFileRequest(in))
	if err != nil {
		return errorResult(call, "coding.read_file failed: "+err.Error()), nil
	}

	// Record the full normalized file under its canonical path so a later edit
	// whose tag is stale can recover via 3-way merge against this version.
	if d.store != nil {
		d.store.Record(res.Path, res.Text)
	}

	header := hashline.FormatHeader(res.Path, res.Tag)
	var content string
	if res.LineCount == 0 {
		content = header
	} else {
		content = header + "\n" + hashline.FormatNumberedLines(res.SliceText, res.StartLine)
	}

	structured := ReadFileToolResult{
		Path:      res.Path,
		Tag:       res.Tag,
		Header:    header,
		StartLine: res.StartLine,
		EndLine:   res.EndLine,
		LineCount: res.LineCount,
		Content:   content,
		Truncated: res.Truncated,
	}
	return successResult(call, content, structured)
}
