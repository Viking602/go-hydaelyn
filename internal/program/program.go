package program

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrProgramNotFound = errors.New("program not found")

type Document struct {
	Name     string            `json:"name"`
	Body     string            `json:"body"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Loader interface {
	Load(ctx context.Context, name string) (Document, error)
}

type MemoryLoader struct {
	Documents map[string]Document
}

func (l MemoryLoader) Load(_ context.Context, name string) (Document, error) {
	if document, ok := l.Documents[name]; ok {
		return document, nil
	}
	return Document{}, ErrProgramNotFound
}

type FSLoader struct {
	Root string
}

func (l FSLoader) Load(_ context.Context, name string) (Document, error) {
	path := filepath.Join(l.Root, name)
	body, err := os.ReadFile(path)
	if err != nil {
		return Document{}, ErrProgramNotFound
	}
	return Document{
		Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Body: string(body),
	}, nil
}
