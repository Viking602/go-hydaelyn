package coding

import (
	"context"
	"fmt"
	"strings"

	"github.com/Viking602/go-hydaelyn/coding/internal/hashline"
	"github.com/Viking602/go-hydaelyn/tool"
)

// searchInput is the decoded argument shape for coding.search.
type searchInput struct {
	Query      string `json:"query"`
	Regexp     bool   `json:"regexp,omitempty"`
	Glob       string `json:"glob,omitempty"`
	MaxResults int    `json:"maxResults,omitempty"`
}

// SearchToolResult is the typed structured result of coding.search. Each file
// is grouped under its ¶PATH#TAG header with its matched lines.
type SearchToolResult struct {
	Files     []SearchToolFile `json:"files"`
	Truncated bool             `json:"truncated"`
	Content   string           `json:"content"`
}

// SearchToolFile groups matches for one file under its minted tag/header.
type SearchToolFile struct {
	Path    string        `json:"path"`
	Tag     string        `json:"tag"`
	Header  string        `json:"header"`
	Matches []SearchMatch `json:"matches"`
}

// defaultSearchMaxResults caps matched lines returned by a single search.
const defaultSearchMaxResults = 200

// searchDriver scans workspace text files and returns grouped ¶PATH#TAG
// sections with the matched, numbered lines. Like read_file, it records each
// matched file's full normalized content into the shared snapshot store so a
// later edit can recover a stale tag.
type searchDriver struct {
	ws    Workspace
	store hashline.SnapshotStore
}

func (d searchDriver) Definition() tool.Definition {
	return tool.Definition{
		Name:        ToolSearch,
		Description: "Search workspace text files for a substring or regexp; returns ¶PATH#TAG grouped matches.",
		InputSchema: objectSchema(
			[]string{"query"},
			property{"query", stringSchema("Substring (default) or regular expression to match.")},
			property{"regexp", boolSchema("Treat query as a Go regular expression.")},
			property{"glob", stringSchema("Optional path glob to restrict the search.")},
			property{"maxResults", intSchema("Cap on matched lines returned (default 200).")},
		),
		EffectType:         tool.EffectReadOnly,
		RequiresActionTask: false,
		RiskLevel:          riskLow,
		PolicyTags:         []string{tagCoding, tagSearch},
	}
}

func (d searchDriver) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var in searchInput
	if err := decodeArgs(call.Arguments, &in); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(in.Query) == "" {
		return errorResult(call, "coding.search failed: query must not be empty"), nil
	}

	searchRes, err := d.ws.Search(ctx, SearchRequest{
		Query:      in.Query,
		Regexp:     in.Regexp,
		Glob:       in.Glob,
		MaxResults: in.MaxResults,
	})
	if err != nil {
		return errorResult(call, "coding.search failed: "+err.Error()), nil
	}

	result := SearchToolResult{Truncated: searchRes.Truncated}
	for _, f := range searchRes.Files {
		result.Files = append(result.Files, buildSearchFile(f.Path, f.Tag, f.Matches))
		// Record the matched file's full normalized content so a later edit can
		// recover a stale tag against this version (the same history read_file
		// records). Use the text from the SAME read that minted f.Tag and the
		// match line numbers — carried in f.Text — rather than a second ReadFile:
		// a follow-up read could observe a changed file and, if the new content
		// shared the 16-bit tag, record a snapshot whose tag matches the header
		// but whose lines do not, letting a later edit fast-path against the wrong
		// version. Recording the exact searched content keeps the snapshot's tag
		// equal to the header's tag.
		if d.store != nil {
			d.store.Record(f.Path, f.Text)
		}
	}
	result.Content = renderSearch(result.Files)
	return successResult(call, result.Content, result)
}

// buildSearchFile mints the header for a matched file.
func buildSearchFile(path, tag string, matches []SearchMatch) SearchToolFile {
	return SearchToolFile{
		Path:    path,
		Tag:     tag,
		Header:  hashline.FormatHeader(path, tag),
		Matches: matches,
	}
}

// renderSearch renders grouped files as ¶PATH#TAG headers followed by their
// matched N:TEXT lines, files separated by a blank line.
func renderSearch(files []SearchToolFile) string {
	var b strings.Builder
	for i, f := range files {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(f.Header)
		for _, m := range f.Matches {
			b.WriteString("\n")
			b.WriteString(hashline.FormatNumberedLine(m.LineNumber, m.Line))
		}
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// listFilesDriver lists workspace files. It shares the search file walker's
// sandbox and is included in the toolset for navigation.
type listFilesDriver struct {
	ws Workspace
}

func (d listFilesDriver) Definition() tool.Definition {
	return tool.Definition{
		Name:        ToolListFiles,
		Description: "List workspace-relative files matching an optional glob.",
		InputSchema: objectSchema(
			nil,
			property{"glob", stringSchema("Optional path glob (e.g. *.go).")},
			property{"limit", intSchema("Cap on returned paths.")},
		),
		EffectType:         tool.EffectReadOnly,
		RequiresActionTask: false,
		RiskLevel:          riskLow,
		PolicyTags:         []string{tagCoding, tagRead},
	}
}

// listFilesInput is the decoded argument shape for coding.list_files.
type listFilesInput struct {
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

func (d listFilesDriver) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var in listFilesInput
	if err := decodeArgs(call.Arguments, &in); err != nil {
		return tool.Result{}, err
	}
	res, err := d.ws.ListFiles(ctx, ListFilesRequest{Glob: in.Glob, Limit: in.Limit})
	if err != nil {
		return errorResult(call, "coding.list_files failed: "+err.Error()), nil
	}
	content := strings.Join(res.Files, "\n")
	if res.Truncated {
		content += fmt.Sprintf("\n[truncated; %d files shown]", len(res.Files))
	}
	return successResult(call, content, res)
}
