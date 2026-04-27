package cases

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CorpusDocument struct {
	ID   string    `json:"id"`
	Date time.Time `json:"date"`
	Text string    `json:"text"`
}

func LoadCorpus(corpusPath string) (map[string]CorpusDocument, error) {
	entries, err := os.ReadDir(corpusPath)
	if err != nil {
		return nil, fmt.Errorf("read corpus directory: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	docs := make(map[string]CorpusDocument)
	for _, entry := range entries {
		path := filepath.Join(corpusPath, entry.Name())
		if entry.IsDir() {
			nested, err := LoadCorpus(path)
			if err != nil {
				return nil, err
			}
			for id, doc := range nested {
				if _, exists := docs[id]; exists {
					return nil, fmt.Errorf("duplicate corpus document id %q", id)
				}
				docs[id] = doc
			}
			continue
		}
		if strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}
		fileDocs, err := loadCorpusFile(path)
		if err != nil {
			return nil, err
		}
		for _, doc := range fileDocs {
			if strings.TrimSpace(doc.ID) == "" {
				return nil, fmt.Errorf("corpus document id is required in %s", path)
			}
			if _, exists := docs[doc.ID]; exists {
				return nil, fmt.Errorf("duplicate corpus document id %q", doc.ID)
			}
			docs[doc.ID] = doc
		}
	}
	return docs, nil
}

func loadCorpusFile(path string) ([]CorpusDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus file %s: %w", path, err)
	}
	var list []CorpusDocument
	if err := json.Unmarshal(data, &list); err == nil {
		return list, nil
	}
	var doc CorpusDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode corpus file %s: %w", path, err)
	}
	return []CorpusDocument{doc}, nil
}
