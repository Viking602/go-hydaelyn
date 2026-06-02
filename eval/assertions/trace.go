package assertions

import (
	"context"
	"fmt"
	"strings"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/eval"
)

// BlackboardHasItem asserts that at least one blackboard item matching a
// selector was written during the run. The selector is applied through the
// public Runner facade (SelectItems); its RunID is defaulted to the run under
// test when unset so callers can pass a selector scoped only by key, type, or
// source.
type BlackboardHasItem struct {
	// Selector filters the blackboard. At least one matching item must exist.
	Selector api.BlackboardSelector
}

// Name returns the assertion's stable identifier.
func (a BlackboardHasItem) Name() string { return "BlackboardHasItem" }

// Check reports whether the blackboard holds at least one item matching the
// selector.
func (a BlackboardHasItem) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	runner := harness.Runner()
	if runner == nil {
		return fmt.Errorf("harness returned a nil runner")
	}
	selector := a.Selector
	if selector.RunID == "" {
		selector.RunID = run.ID
	}
	items, err := runner.SelectItems(ctx, run.ID, selector)
	if err != nil {
		return fmt.Errorf("select blackboard items: %w", err)
	}
	if len(items) > 0 {
		return nil
	}
	return fmt.Errorf("no blackboard item matched the selector")
}

// ApprovalRequested asserts that the run requested at least one human approval.
// When Reason is set, the matching approval's reason must contain it
// (case-insensitive); an empty Reason matches any requested approval. Approvals
// are observed from the run's ApprovalRequested events.
type ApprovalRequested struct {
	// Reason, when non-empty, is a substring the approval reason must contain.
	Reason string
}

// Name returns the assertion's stable identifier.
func (a ApprovalRequested) Name() string { return "ApprovalRequested" }

// Check reports whether a matching approval was requested during the run.
func (a ApprovalRequested) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	runner := harness.Runner()
	if runner == nil {
		return fmt.Errorf("harness returned a nil runner")
	}
	events, err := runner.RunEvents(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("read run events: %w", err)
	}
	want := strings.ToLower(a.Reason)
	var sawApproval bool
	for _, ev := range events {
		if ev.Type != api.EventApprovalRequested {
			continue
		}
		sawApproval = true
		if want == "" {
			return nil
		}
		reason, _ := ev.Payload["reason"].(string)
		if strings.Contains(strings.ToLower(reason), want) {
			return nil
		}
	}
	if !sawApproval {
		return fmt.Errorf("no approval was requested during the run")
	}
	return fmt.Errorf("an approval was requested but none with a reason containing %q", a.Reason)
}
