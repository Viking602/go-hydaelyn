package core

import "github.com/Viking602/go-hydaelyn/internal/core/model"

// BlackboardNotifier is implemented by command results that produced
// blackboard items needing commit-time fan-out to in-memory subscribers.
// Domain handlers opt in by adding NotifyBlackboard() to their public
// result type instead of having core type-switch on every new result.
type BlackboardNotifier interface {
	NotifyBlackboard() []model.BlackboardItem
}

// notifyUoWCommandSubscribers emits commit-time notifications for blackboard
// writes produced by UoW command handlers. Used only for the external store
// path; the memory path is handled automatically by memory.Provider.Commit().
func (r *Runtime) notifyUoWCommandSubscribers(_ RuntimeCommand, result any) {
	if item, ok := result.(model.BlackboardItem); ok {
		// WriteBlackboardItemCommand handlers return a bare BlackboardItem.
		r.memProvider.Notify([]model.BlackboardItem{item})
		return
	}
	if n, ok := result.(BlackboardNotifier); ok {
		if items := n.NotifyBlackboard(); len(items) > 0 {
			r.memProvider.Notify(items)
		}
	}
}
