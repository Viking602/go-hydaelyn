package coding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/Viking602/venat/coding/internal/hashline"
	"github.com/Viking602/venat/tool"
)

// editHashlineInput is the decoded argument shape for coding.edit_hashline.
type editHashlineInput struct {
	Input  string `json:"input"`
	DryRun bool   `json:"dryRun,omitempty"`
}

// EditSectionResult is the per-section outcome of an edit.
type EditSectionResult struct {
	Path             string   `json:"path"`
	Op               string   `json:"op"`
	OldTag           string   `json:"oldTag"`
	NewTag           string   `json:"newTag"`
	Header           string   `json:"header"`
	FirstChangedLine int      `json:"firstChangedLine"`
	Diff             string   `json:"diff"`
	Warnings         []string `json:"warnings,omitempty"`
	// Recovered reports that this section's ¶PATH#TAG was stale and the edit
	// replayed against current content after verifying/remapping target anchors
	// when present, or directly when it was a head/tail-only insertion.
	Recovered bool `json:"recovered,omitempty"`
}

// EditHashlineResult is the typed structured result of coding.edit_hashline.
// It carries the fresh headers the agent must use for its next edit plus the
// audit metadata surfaced to the run event stream.
type EditHashlineResult struct {
	DryRun   bool                `json:"dryRun"`
	Sections []EditSectionResult `json:"sections"`
	Content  string              `json:"content"`
	// OldTags/NewTags/FirstChangedLines/DiffHash are the audit dimensions
	// surfaced via the UpdateSink (see spec §7.2). They are duplicated here so
	// the durable tool Result is self-describing.
	OldTags           []string `json:"oldTags"`
	NewTags           []string `json:"newTags"`
	FirstChangedLines []int    `json:"firstChangedLines"`
	DiffHash          string   `json:"diffHash"`
	// Recovered reports that at least one stale section was safely replayed
	// against current content (see EditSectionResult).
	Recovered bool `json:"recovered,omitempty"`
}

// editHashlineDriver applies a line-anchored hashline patch all-or-nothing.
type editHashlineDriver struct {
	ws      Workspace
	patcher *hashline.Patcher
}

func (d editHashlineDriver) Definition() tool.Definition {
	return tool.Definition{
		Name:        ToolEditHashline,
		Description: "Apply a hashline patch to existing files; each section starts with ¶PATH#TAG and uses +final-content rows.",
		InputSchema: objectSchema(
			[]string{"input"},
			property{"input", stringSchema("The hashline patch: ¶PATH#TAG sections with replace/delete/insert ops and +body rows.")},
			property{"dryRun", boolSchema("Preview the diff without writing.")},
		),
		EffectType:         tool.EffectWrite,
		RequiresActionTask: true,
		RiskLevel:          riskMedium,
		PolicyTags:         []string{tagCoding, tagEdit, tagHashline, tagWorkspace},
	}
}

func (d editHashlineDriver) Execute(ctx context.Context, call tool.Call, sink tool.UpdateSink) (tool.Result, error) {
	var in editHashlineInput
	if err := decodeArgs(call.Arguments, &in); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(in.Input) == "" {
		return errorResult(call, "coding.edit_hashline failed: input is empty"), nil
	}

	patch, err := hashline.Parse(in.Input)
	if err != nil {
		return errorResult(call, recoveryMessage(err)), nil
	}

	prepared, err := d.patcher.Preflight(ctx, patch)
	if err != nil {
		return errorResult(call, recoveryMessage(err)), nil
	}

	if in.DryRun {
		result := buildEditResult(true, preparedToSections(prepared))
		emitEditAudit(sink, result)
		return successResult(call, result.Content, result)
	}

	applied, err := d.patcher.Commit(ctx, prepared)
	if err != nil {
		return errorResult(call, recoveryMessage(err)), nil
	}

	result := buildEditResult(false, appliedToSections(applied.Sections))
	emitEditAudit(sink, result)
	return successResult(call, result.Content, result)
}

// preparedToSections projects preflight output onto the result shape.
func preparedToSections(prepared []hashline.PreparedSection) []EditSectionResult {
	out := make([]EditSectionResult, 0, len(prepared))
	for _, ps := range prepared {
		out = append(out, EditSectionResult{
			Path:             ps.Path,
			Op:               ps.Op,
			OldTag:           ps.OldTag,
			NewTag:           ps.NewTag,
			Header:           hashline.FormatHeader(ps.Path, ps.NewTag),
			FirstChangedLine: ps.FirstChangedLine,
			Diff:             ps.Diff,
			Warnings:         ps.Warnings,
			Recovered:        ps.Recovered,
		})
	}
	return out
}

// appliedToSections projects commit output onto the result shape.
func appliedToSections(sections []hashline.SectionResult) []EditSectionResult {
	out := make([]EditSectionResult, 0, len(sections))
	for _, s := range sections {
		out = append(out, EditSectionResult{
			Path:             s.Path,
			Op:               s.Op,
			OldTag:           s.OldTag,
			NewTag:           s.NewTag,
			Header:           s.Header,
			FirstChangedLine: s.FirstChangedLine,
			Diff:             s.Diff,
			Warnings:         s.Warnings,
			Recovered:        s.Recovered,
		})
	}
	return out
}

// buildEditResult assembles the typed result and the human-facing content.
func buildEditResult(dryRun bool, sections []EditSectionResult) EditHashlineResult {
	var b strings.Builder
	oldTags := make([]string, 0, len(sections))
	newTags := make([]string, 0, len(sections))
	firstChanged := make([]int, 0, len(sections))
	recovered := false
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(s.Header)
		b.WriteString("\n")
		verb := "updated"
		if dryRun {
			verb = "would update"
		}
		b.WriteString(verb + " " + s.Path)
		if s.Recovered {
			b.WriteString(" (recovered stale tag against current content)")
		}
		if s.FirstChangedLine > 0 {
			b.WriteString("\nfirstChangedLine: ")
			b.WriteString(strconv.Itoa(s.FirstChangedLine))
		}
		if s.Diff != "" {
			b.WriteString("\n\n--- compact diff ---\n")
			b.WriteString(s.Diff)
		}
		oldTags = append(oldTags, s.OldTag)
		newTags = append(newTags, s.NewTag)
		firstChanged = append(firstChanged, s.FirstChangedLine)
		if s.Recovered {
			recovered = true
		}
	}
	content := b.String()
	return EditHashlineResult{
		DryRun:            dryRun,
		Sections:          sections,
		Content:           content,
		OldTags:           oldTags,
		NewTags:           newTags,
		FirstChangedLines: firstChanged,
		DiffHash:          diffHash(sections),
		Recovered:         recovered,
	}
}

// emitEditAudit surfaces the edit metadata to the run event stream. A nil
// sink (e.g. a direct unit-test call) is tolerated.
func emitEditAudit(sink tool.UpdateSink, result EditHashlineResult) {
	if sink == nil {
		return
	}
	data := map[string]string{
		"oldTags":           strings.Join(result.OldTags, ","),
		"newTags":           strings.Join(result.NewTags, ","),
		"firstChangedLines": joinInts(result.FirstChangedLines),
		"diffHash":          result.DiffHash,
	}
	if result.DryRun {
		data["dryRun"] = "true"
	}
	if result.Recovered {
		data["recovered"] = "true"
	}
	_ = sink(tool.Update{
		Kind:    "coding.edit_hashline",
		Message: "applied hashline edit",
		Data:    data,
	})
}

// diffHash returns a stable hash over all section diffs for the audit record.
func diffHash(sections []EditSectionResult) string {
	h := sha256.New()
	for _, s := range sections {
		h.Write([]byte(s.Path))
		h.Write([]byte{0})
		h.Write([]byte(s.Diff))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// recoveryMessage maps a hashline failure onto an agent-facing message that
// tells the agent how to recover (re-read on stale/parse errors).
func recoveryMessage(err error) string {
	switch {
	case errors.Is(err, hashline.ErrSnapshotMismatch):
		return "coding.edit_hashline rejected: the ¶PATH#TAG tag is stale (the file changed since you read it). " +
			"Re-read the file with coding.read_file to get the current tag and line numbers, then retry. Details: " + err.Error()
	case errors.Is(err, hashline.ErrParse):
		return "coding.edit_hashline rejected: the patch did not parse. Every section must start with ¶PATH#TAG, " +
			"body rows are final content only (+TEXT), and delete carries no body. Re-read the file and rebuild the patch. Details: " + err.Error()
	case errors.Is(err, hashline.ErrNoop):
		return "coding.edit_hashline rejected: the edit is a no-op (the result equals the current file). Details: " + err.Error()
	case errors.Is(err, hashline.ErrDuplicateSection):
		return "coding.edit_hashline rejected: multiple sections target the same file. Combine the operations into one ¶PATH#TAG section. Details: " + err.Error()
	default:
		return "coding.edit_hashline failed: " + err.Error() + ". Re-read the file with coding.read_file before retrying."
	}
}

// joinInts renders a slice of ints as a comma-separated string for the audit
// event data map.
func joinInts(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ",")
}
