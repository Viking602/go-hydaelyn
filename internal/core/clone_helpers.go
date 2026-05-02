package core

import "github.com/Viking602/go-hydaelyn/internal/eventpayload"

func taskEventPayload(task Task) map[string]any {
	return eventpayload.Task(task)
}

func runPayload(run Run) map[string]any {
	return eventpayload.Run(run)
}
