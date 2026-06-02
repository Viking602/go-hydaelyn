// Package assertions ships the framework's built-in eval.Assertion
// implementations. Each assertion grades an executed api.Run through the
// public Runner façade exposed by eval.Harness. M1 ships the four carry-over
// single-agent assertions: OutputContains, OutputMatchesSchema,
// RunTerminatedWithStatus, and EventEmitted.
package assertions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/eval"
)

// OutputContains asserts that the run's textual output contains a substring.
// The output is the concatenation of every blackboard item written during the
// run (BlackboardItem.Payload, falling back to Content). Matching is
// case-insensitive unless CaseSensitive is set.
type OutputContains struct {
	// Substring is the text that must appear in the run output.
	Substring string
	// CaseSensitive switches matching from the default case-insensitive mode.
	CaseSensitive bool
}

// Name returns the assertion's stable identifier.
func (a OutputContains) Name() string { return "OutputContains" }

// Check gathers the run's output text and reports whether Substring appears in it.
func (a OutputContains) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	output, err := runOutput(ctx, run, harness)
	if err != nil {
		return err
	}
	hay, needle := output, a.Substring
	if !a.CaseSensitive {
		hay = strings.ToLower(hay)
		needle = strings.ToLower(needle)
	}
	if strings.Contains(hay, needle) {
		return nil
	}
	return fmt.Errorf("output does not contain %q (output: %q)", a.Substring, output)
}

// OutputMatchesSchema asserts that the run's structured output validates against
// a JSON Schema. M1 supports the same keyword subset as agent.OutputPolicy: an
// object "type" and "required" property names. The output is parsed from the
// run's blackboard items; the first item that parses as a JSON object is graded.
type OutputMatchesSchema struct {
	// Schema is the JSON Schema document the output must satisfy.
	Schema json.RawMessage
}

// Name returns the assertion's stable identifier.
func (a OutputMatchesSchema) Name() string { return "OutputMatchesSchema" }

// Check finds the first blackboard item that parses as a JSON object and
// validates it against Schema.
func (a OutputMatchesSchema) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	items, err := runOutputItems(ctx, run, harness)
	if err != nil {
		return err
	}
	for _, item := range items {
		var value map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(item)), &value) == nil {
			return validateSchema(a.Schema, value)
		}
	}
	return fmt.Errorf("no run output item parsed as a JSON object (output: %q)", strings.Join(items, "\n"))
}

// runOutputItems returns the non-empty textual output items produced on the
// run's blackboard, in store order. Each item contributes its Payload, falling
// back to Content when Payload is empty.
func runOutputItems(ctx context.Context, run api.Run, harness eval.Harness) ([]string, error) {
	runner := harness.Runner()
	if runner == nil {
		return nil, fmt.Errorf("harness returned a nil runner")
	}
	items, err := runner.SelectItems(ctx, run.ID, api.BlackboardSelector{RunID: run.ID})
	if err != nil {
		return nil, fmt.Errorf("select blackboard items: %w", err)
	}
	texts := make([]string, 0, len(items))
	for _, item := range items {
		text := item.Payload
		if text == "" {
			text = item.Content
		}
		if text == "" {
			continue
		}
		texts = append(texts, text)
	}
	return texts, nil
}

// runOutput concatenates the textual output produced on the run's blackboard,
// one item per line.
func runOutput(ctx context.Context, run api.Run, harness eval.Harness) (string, error) {
	items, err := runOutputItems(ctx, run, harness)
	if err != nil {
		return "", err
	}
	return strings.Join(items, "\n"), nil
}

// validateSchema applies the supported JSON Schema keyword subset (object type
// and required property names) to value. It deliberately mirrors the minimal
// validator the agent loop enforces; richer schema matching lands in M2's
// matcher package.
func validateSchema(schema json.RawMessage, value map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	var doc struct {
		Type     string   `json:"type"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil {
		return fmt.Errorf("invalid schema: %v", err)
	}
	if doc.Type != "" && doc.Type != "object" {
		return fmt.Errorf("unsupported schema type %q (only %q is supported)", doc.Type, "object")
	}
	var missing []string
	for _, key := range doc.Required {
		if _, ok := value[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("output missing required field(s): %s", strings.Join(missing, ", "))
	}
	return nil
}
