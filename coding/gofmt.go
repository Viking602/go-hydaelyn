package coding

import (
	"context"
	"fmt"
	"go/format"
	"strings"

	"github.com/Viking602/venat/coding/internal/hashline"
	"github.com/Viking602/venat/tool"
)

// gofmtInput is the decoded argument shape for coding.gofmt.
type gofmtInput struct {
	Path string `json:"path"`
}

// GofmtToolResult is the typed structured result of coding.gofmt.
type GofmtToolResult struct {
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
	// Tag is the minted tag of the formatted file (its post-format content).
	Tag    string `json:"tag"`
	Header string `json:"header"`
	Diff   string `json:"diff"`
}

// gofmtDriver formats a Go file in-process using go/format.Source. No
// subprocess is launched and no import management is performed (goimports is
// out of scope for v1). It records the formatted content into the shared
// snapshot store so the ¶PATH#TAG it returns is collision-guarded like a
// read/search/edit tag.
type gofmtDriver struct {
	ws    Workspace
	store hashline.SnapshotStore
}

func (d gofmtDriver) Definition() tool.Definition {
	return tool.Definition{
		Name:        ToolGofmt,
		Description: "Format a Go file in-process with go/format. Use this for formatting, never hashline edits.",
		InputSchema: objectSchema(
			[]string{"path"},
			property{"path", stringSchema("Workspace-relative path of the .go file to format.")},
		),
		EffectType:         tool.EffectWrite,
		RequiresActionTask: true,
		RiskLevel:          riskLow,
		PolicyTags:         []string{tagCoding, tagFormat},
	}
}

func (d gofmtDriver) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var in gofmtInput
	if err := decodeArgs(call.Arguments, &in); err != nil {
		return tool.Result{}, err
	}
	if !strings.HasSuffix(in.Path, ".go") {
		return errorResult(call, "coding.gofmt rejected: only .go files can be formatted"), nil
	}

	raw, err := d.ws.ReadFile(ctx, ReadFileRequest{Path: in.Path})
	if err != nil {
		return errorResult(call, "coding.gofmt failed: "+err.Error()), nil
	}

	formatted, err := format.Source([]byte(raw.Text))
	if err != nil {
		return errorResult(call, "coding.gofmt rejected: the file does not parse as Go: "+err.Error()), nil
	}
	formattedText := string(formatted)

	if formattedText == raw.Text {
		// Already formatted: the returned tag is raw.Tag (over raw.Text). Record
		// it so the tag is backed by history even when no prior read recorded
		// this file, matching the collision guard's expectation.
		if d.store != nil {
			d.store.Record(raw.Path, raw.Text)
		}
		header := hashline.FormatHeader(raw.Path, raw.Tag)
		structured := GofmtToolResult{Path: raw.Path, Changed: false, Tag: raw.Tag, Header: header}
		return successResult(call, header+"\nalready formatted "+raw.Path, structured)
	}

	// gofmt rewrites an existing file in place, so it writes through the
	// hashline.Filesystem path (WriteFile only creates new files).
	if err := d.writeInPlace(ctx, raw.Path, formattedText); err != nil {
		return errorResult(call, "coding.gofmt failed to write: "+err.Error()), nil
	}

	// Record the post-format content under its canonical path so newTag is
	// backed by recorded history and a later edit keyed to it is collision-
	// guarded. Record normalizes identically to the newTag computation below.
	if d.store != nil {
		d.store.Record(raw.Path, formattedText)
	}

	newTag := hashline.ComputeFileHash(hashline.Normalize(formattedText).Text)
	header := hashline.FormatHeader(raw.Path, newTag)
	diff := hashline.CompactDiff(raw.Text, hashline.Normalize(formattedText).Text)
	structured := GofmtToolResult{
		Path:    raw.Path,
		Changed: true,
		Tag:     newTag,
		Header:  header,
		Diff:    diff,
	}
	content := header + "\nformatted " + raw.Path + "\n\n--- compact diff ---\n" + diff
	return successResult(call, content, structured)
}

// writeInPlace rewrites an existing file through the hashline.Filesystem write
// path, which (unlike WriteFile) does not reject an existing target.
func (d gofmtDriver) writeInPlace(ctx context.Context, path, text string) error {
	fs, ok := d.ws.(hashline.Filesystem)
	if !ok {
		return fmt.Errorf("coding: workspace does not support in-place writes")
	}
	if err := fs.PreflightWrite(ctx, path); err != nil {
		return err
	}
	return fs.WriteText(ctx, path, text)
}
