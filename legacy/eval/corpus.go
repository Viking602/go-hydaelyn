package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Corpus struct {
	Documents []CorpusDocument `json:"documents,omitempty" yaml:"documents,omitempty"`
}

type CorpusDocument struct {
	ID       string         `json:"id" yaml:"id"`
	Date     time.Time      `json:"date" yaml:"date"`
	Text     string         `json:"text" yaml:"text"`
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

func LoadCorpus(path string) (Corpus, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Corpus{}, err
	}

	var documents []CorpusDocument
	seen := map[string]struct{}{}
	appendDoc := func(doc CorpusDocument, source string) error {
		doc.ID = strings.TrimSpace(doc.ID)
		doc.Text = strings.TrimSpace(doc.Text)
		if doc.ID == "" {
			return fmt.Errorf("%s: corpus document missing id", source)
		}
		if doc.Date.IsZero() {
			return fmt.Errorf("%s: corpus document %q missing date", source, doc.ID)
		}
		if doc.Text == "" {
			return fmt.Errorf("%s: corpus document %q missing text", source, doc.ID)
		}
		if _, ok := seen[doc.ID]; ok {
			return fmt.Errorf("%s: duplicate corpus document id %q", source, doc.ID)
		}
		seen[doc.ID] = struct{}{}
		documents = append(documents, doc)
		return nil
	}

	if !info.IsDir() {
		loaded, err := loadCorpusFile(path)
		if err != nil {
			return Corpus{}, err
		}
		for _, doc := range loaded.Documents {
			if err := appendDoc(doc, path); err != nil {
				return Corpus{}, err
			}
		}
		return Corpus{Documents: documents}, nil
	}

	err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !isCorpusFile(current) {
			return nil
		}
		loaded, err := loadCorpusFile(current)
		if err != nil {
			return err
		}
		for _, doc := range loaded.Documents {
			if err := appendDoc(doc, current); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Corpus{}, err
	}

	sort.Slice(documents, func(i, j int) bool {
		if documents[i].Date.Equal(documents[j].Date) {
			return documents[i].ID < documents[j].ID
		}
		return documents[i].Date.Before(documents[j].Date)
	})

	return Corpus{Documents: documents}, nil
}

func loadCorpusFile(path string) (Corpus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Corpus{}, err
	}

	var bundle struct {
		Documents []corpusDocumentEnvelope `json:"documents" yaml:"documents"`
	}
	if err := yaml.Unmarshal(data, &bundle); err == nil && len(bundle.Documents) > 0 {
		return materializeCorpus(bundle.Documents)
	}

	var single corpusDocumentEnvelope
	if err := yaml.Unmarshal(data, &single); err == nil && !single.isZero() {
		return materializeCorpus([]corpusDocumentEnvelope{single})
	}

	var jsonBundle struct {
		Documents []corpusDocumentEnvelope `json:"documents"`
	}
	if err := json.Unmarshal(data, &jsonBundle); err == nil && len(jsonBundle.Documents) > 0 {
		return materializeCorpus(jsonBundle.Documents)
	}

	if err := json.Unmarshal(data, &single); err == nil && !single.isZero() {
		return materializeCorpus([]corpusDocumentEnvelope{single})
	}

	return Corpus{}, errors.New("unsupported corpus document format")
}

type corpusDocumentEnvelope struct {
	ID       string         `json:"id" yaml:"id"`
	Date     string         `json:"date" yaml:"date"`
	Text     string         `json:"text" yaml:"text"`
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

func (doc corpusDocumentEnvelope) isZero() bool {
	return strings.TrimSpace(doc.ID) == "" && strings.TrimSpace(doc.Date) == "" && strings.TrimSpace(doc.Text) == ""
}

func materializeCorpus(items []corpusDocumentEnvelope) (Corpus, error) {
	documents := make([]CorpusDocument, 0, len(items))
	for _, item := range items {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(item.Date))
		if err != nil {
			return Corpus{}, fmt.Errorf("parse corpus date for %q: %w", item.ID, err)
		}
		documents = append(documents, CorpusDocument{
			ID:       item.ID,
			Date:     parsed,
			Text:     item.Text,
			Metadata: item.Metadata,
		})
	}
	return Corpus{Documents: documents}, nil
}

func isCorpusFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".yaml", ".yml":
		return true
	default:
		return false
	}
}
