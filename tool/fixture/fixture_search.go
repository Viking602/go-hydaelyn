package fixture

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Viking602/go-hydaelyn/eval/cases"
	"github.com/Viking602/go-hydaelyn/tool"
)

type SearchTool struct {
	documents []cases.CorpusDocument
}

type searchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type searchOutput struct {
	Matches []cases.CorpusDocument `json:"matches"`
}

func NewSearchTool(corpusPath string) (*SearchTool, error) {
	docs, err := cases.LoadCorpus(corpusPath)
	if err != nil {
		return nil, err
	}
	ordered := make([]cases.CorpusDocument, 0, len(docs))
	for _, doc := range docs {
		ordered = append(ordered, doc)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	return &SearchTool{documents: ordered}, nil
}

func (t *SearchTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "fixture_search",
		Description: "Search deterministic local fixture corpus",
		InputSchema: tool.Schema{
			Type: "object",
			Properties: map[string]tool.Schema{
				"query": {Type: "string"},
				"limit": {Type: "number"},
			},
			Required: []string{"query"},
		},
	}
}

func (t *SearchTool) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var input searchInput
	if err := decodeArgs(call, &input); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(input.Query) == "" {
		return tool.Result{}, fmt.Errorf("query is required")
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 5
	}
	query := strings.ToLower(input.Query)
	matches := make([]cases.CorpusDocument, 0, limit)
	for _, doc := range t.documents {
		if strings.Contains(strings.ToLower(doc.ID), query) || strings.Contains(strings.ToLower(doc.Text), query) {
			matches = append(matches, doc)
			if len(matches) == limit {
				break
			}
		}
	}
	return jsonResult(call, t.Definition().Name, searchOutput{Matches: matches})
}
