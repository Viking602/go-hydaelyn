package evalcase

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Viking602/go-hydaelyn/evaluation"
)

func LoadCase(path string) (evaluation.EvalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return evaluation.EvalCase{}, fmt.Errorf("read eval case: %w", err)
	}
	var c evaluation.EvalCase
	if err := json.Unmarshal(data, &c); err != nil {
		return evaluation.EvalCase{}, fmt.Errorf("decode eval case: %w", err)
	}
	if err := ValidateCase(c); err != nil {
		return evaluation.EvalCase{}, err
	}
	return c, nil
}

func DiscoverCasePaths(root string) ([]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("discover eval cases: root is required")
	}
	resolved, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve eval case root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat eval case root: %w", err)
	}
	if !info.IsDir() {
		likely, err := isLikelyCaseFile(resolved)
		if err != nil {
			return nil, err
		}
		if !likely {
			return nil, fmt.Errorf("eval case path is not a recognized case file: %s", resolved)
		}
		return []string{resolved}, nil
	}

	paths := make([]string, 0, 8)
	if err := filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch strings.ToLower(entry.Name()) {
			case "runs", "results", "_external":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".json") {
			return nil
		}
		likely, err := isLikelyCaseFile(path)
		if err != nil {
			return err
		}
		if likely {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk eval cases: %w", err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no eval case files found under %s", resolved)
	}
	return paths, nil
}

func isLikelyCaseFile(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read eval case probe %s: %w", path, err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return false, nil
	}
	required := []string{"id", "suite", "pattern"}
	for _, key := range required {
		if _, ok := envelope[key]; !ok {
			return false, nil
		}
	}
	return true, nil
}
