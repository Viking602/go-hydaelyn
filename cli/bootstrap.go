package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/Viking602/go-hydaelyn/host"
)

func runInit(args []string, _ io.Writer) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	target := filepath.Join(dir, ".hydaelyn")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	config := map[string]any{
		"pattern":    "deepsearch",
		"supervisor": "supervisor",
		"workers":    []string{"researcher"},
	}
	return writeJSONFile(filepath.Join(target, "config.json"), config)
}

func runNew(args []string, _ io.Writer) error {
	if len(args) == 0 {
		return errors.New("new requires output path")
	}
	request := host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input: map[string]any{
			"query":      "example query",
			"subqueries": []string{"branch-a", "branch-b"},
		},
	}
	return writeJSONFile(args[0], request)
}
