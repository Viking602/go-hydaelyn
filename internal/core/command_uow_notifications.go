package core

// notifyUoWCommandSubscribers emits commit-time notifications for blackboard
// writes produced by UoW command handlers. Used only for the external store
// path; the memory path is handled automatically by memory.Provider.Commit().
func (r *Runtime) notifyUoWCommandSubscribers(command RuntimeCommand, result any) {
	items := blackboardNotificationItems(command, result)
	if len(items) == 0 {
		return
	}
	r.memProvider.Notify(items)
}

func blackboardNotificationItems(command RuntimeCommand, result any) []BlackboardItem {
	switch command.(type) {
	case WriteBlackboardItemCommand:
		item, ok := result.(BlackboardItem)
		if !ok {
			return nil
		}
		return []BlackboardItem{item}
	case SubmitResponseOutputCommand:
		response, ok := result.(submitResponseOutputResult)
		if !ok {
			return nil
		}
		return []BlackboardItem{response.BlackboardItem}
	case SubmitUserInputCommand:
		input, ok := result.(submitUserInputResult)
		if !ok {
			return nil
		}
		return []BlackboardItem{input.Item}
	case HandoffCommand:
		handoff, ok := result.(handoffResult)
		if !ok || !handoff.HasContext {
			return nil
		}
		return []BlackboardItem{handoff.BlackboardItem}
	case SubmitTypedReportCommand:
		report, ok := result.(submitTypedReportResult)
		if !ok {
			return nil
		}
		return report.NotifyItems
	default:
		return nil
	}
}
